#!/usr/bin/env python3
"""Strict verifier for the M4 multi-device (fleet) integration run.

It checks the gateway event stream AND the fleet summary together, because
neither alone is sufficient: the events show what the gateway authenticated, the
summary shows what each device thought happened.

Assertions:
  * every event line is a JSON object with a run id matching the run
  * devices_expected == devices_authenticated == hybrid_sessions == kem_used
  * every device: unique id, unique 32-hex session id, suite == 2, kem_used
  * no duplicate session ids
  * telemetry / control / ACK ownership: each operation_id belongs to exactly
    one device, and each device's controls are confirmed by that device
  * no session cross-talk: no device appears in another device's session
  * the expected bad device fails, and every other device succeeds

Exit: 0 verified, 1 not verified.
"""
import argparse
import json
import re
import sys

HEX32 = re.compile(r"^[0-9a-f]{32}$")


def load(path, problems, label):
    if not path:
        problems.append(f"{label}: no file supplied")
        return []
    try:
        with open(path, encoding="utf-8") as fh:
            raw_lines = fh.readlines()
    except FileNotFoundError:
        problems.append(f"{label} event file does not exist: {path}")
        return []
    if not raw_lines:
        problems.append(f"{label} event file is empty: {path}")
        return []
    events = []
    for n, line in enumerate(raw_lines, start=1):
        if not line.strip():
            continue
        try:
            ev = json.loads(line)
        except json.JSONDecodeError as exc:
            problems.append(f"{label} line {n} is not JSON: {exc}")
            continue
        if not isinstance(ev, dict):
            problems.append(f"{label} line {n} is not a JSON object")
            continue
        if not isinstance(ev.get("role"), str) or not isinstance(ev.get("stage"), str) \
                or not isinstance(ev.get("outcome"), str):
            problems.append(f"{label} line {n} has no role/stage/outcome")
            continue
        events.append(ev)
    return events


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--gateway", required=True)
    ap.add_argument("--summary", required=True)
    ap.add_argument("--run-id", default=None)
    ap.add_argument("--expect-devices", type=int, default=20)
    ap.add_argument("--expect-suite", type=int, default=2)
    ap.add_argument("--expect-samples", type=int, default=3)
    ap.add_argument("--expect-sessions", type=int, default=None,
                    help="exact number of established sessions per device; with --expect-devices this "
                         "makes every count exact rather than a lower bound")
    ap.add_argument("--expect-bad-device", default="")
    # Historical streams predate control-sent events. They can be re-examined
    # with --legacy (result FAIL, controls_sent=null) but never pass the current
    # acceptance gate.
    ap.add_argument("--legacy", action="store_true",
                    help="mark a pre-control-sent stream as legacy/incomplete instead of failing it")
    ap.add_argument("--out")
    args = ap.parse_args()

    problems = []
    checks = []

    def check(name, ok, detail=""):
        checks.append({"name": name, "ok": bool(ok), "detail": detail})
        if not ok:
            problems.append(f"{name}: {detail}" if detail else name)

    gw = load(args.gateway, problems, "gateway")

    try:
        with open(args.summary, encoding="utf-8") as fh:
            summary = json.load(fh)
    except (FileNotFoundError, json.JSONDecodeError) as exc:
        problems.append(f"fleet summary unusable: {exc}")
        summary = None

    # --- run identity -------------------------------------------------------
    if gw:
        run_ids = {e.get("run_id") for e in gw}
        if args.run_id:
            check("every gateway event carries the expected run_id", run_ids == {args.run_id},
                  f"run_ids present: {sorted(run_ids)}")
        else:
            check("all gateway events share one run_id", len(run_ids) == 1, f"run_ids: {sorted(run_ids)}")

    # --- gateway-side handshake observations ---------------------------------
    established = [e for e in gw if e.get("stage") == "handshake" and e.get("outcome") == "established"]
    by_peer = {}
    sids = []
    for e in established:
        by_peer.setdefault(e.get("peer_id"), []).append(e)
        sids.append((e.get("peer_id"), e.get("session_id")))

    expected = args.expect_devices
    if args.expect_bad_device:
        expected = args.expect_devices - 1
    check(f"gateway authenticated {expected} devices", len(by_peer) == expected,
          f"authenticated {len(by_peer)}: {sorted(by_peer)}")

    bad_suite = [p for p, es in by_peer.items() if any(e.get("suite") != args.expect_suite for e in es)]
    check(f"every session negotiated suite {args.expect_suite}", not bad_suite, f"offending peers: {bad_suite}")

    bad_kem = [p for p, es in by_peer.items() if any(e.get("kem_used") is not True for e in es)]
    check("every session reports that the KEM was used", not bad_kem, f"peers without a KEM: {bad_kem}")

    # EXACT session accounting. When --expect-sessions is given, the run must be
    # exactly right: total sessions and per-device sessions, not merely "at least".
    total_sessions = len(established)
    sessions_per_device = args.expect_sessions
    if sessions_per_device is not None:
        expected_total = expected * sessions_per_device
        check(f"the gateway established exactly {expected_total} sessions",
              total_sessions == expected_total,
              f"established {total_sessions}, expected {expected_total}")
        wrong = {p: len(es) for p, es in by_peer.items() if len(es) != sessions_per_device}
        check(f"every device established exactly {sessions_per_device} sessions", not wrong,
              f"devices with a different count: {wrong}")
        missing = [d for d in (f"D{i:03d}" for i in range(1, args.expect_devices + 1))
                   if d not in by_peer and d != args.expect_bad_device]
        check("every expected device appeared", not missing, f"missing devices: {missing}")

    # --- session id uniqueness ----------------------------------------------
    full = [s for _, s in sids if isinstance(s, str) and HEX32.match(s)]
    check("every session id is a full 32-hex value", len(full) == len(sids),
          f"{len(sids) - len(full)} session ids are not 32-hex")
    check("all session ids are unique", len(set(full)) == len(full),
          f"{len(full) - len(set(full))} duplicates")

    # --- fleet summary ------------------------------------------------------
    if summary is not None:
        check("fleet expected the requested number of devices",
              summary.get("expected_devices") == args.expect_devices,
              f"expected_devices={summary.get('expected_devices')}")
        ok = summary.get("successful_devices")
        check("fleet reports the expected number of successful devices", ok == expected,
              f"successful_devices={ok}, expected {expected}")
        if args.expect_bad_device:
            bad = [r for r in summary.get("results", []) if r.get("device_id") == args.expect_bad_device]
            check(f"the deliberately bad device {args.expect_bad_device} failed",
                  len(bad) != 1 or not bad[0].get("ok"),
                  f"bad device result: {bad}")

    # --- telemetry / control ownership --------------------------------------
    telem = [e for e in gw if e.get("stage") == "business" and e.get("outcome") == "telemetry-accepted"]
    ctrl = [e for e in gw if e.get("stage") == "business" and e.get("outcome") == "control-confirmed"]
    per_device_telem = {}
    for e in telem:
        per_device_telem.setdefault(e.get("peer_id"), 0)
        if e.get("sample_id") is not None:
            per_device_telem[e.get("peer_id")] += 1
    telemetry_expected = expected * (sessions_per_device or 1) * args.expect_samples
    if sessions_per_device is not None:
        # Exact: the fleet must have delivered exactly the configured volume.
        short = [p for p, n in per_device_telem.items() if n != sessions_per_device * args.expect_samples]
        check(f"each device delivered exactly {sessions_per_device * args.expect_samples} telemetry samples",
              not short, f"devices with a different count: {short}")
        check(f"the gateway accepted exactly {telemetry_expected} telemetry records",
              len(telem) == telemetry_expected, f"accepted {len(telem)}")
    elif per_device_telem:
        short = [p for p, n in per_device_telem.items() if n < args.expect_samples]
        check(f"each device delivered at least {args.expect_samples} telemetry samples", not short,
              f"devices below the sample count: {short}")

    # An operation_id is derived from the device id, so checking the prefix
    # proves the control belonged to that device and no other.
    misowned = []
    for e in ctrl:
        op = e.get("operation_id")
        peer = e.get("peer_id")
        if isinstance(op, str) and isinstance(peer, str) and peer not in op:
            misowned.append((peer, op))
    check("every control operation_id belongs to the device it was sent to", not misowned,
          f"mismatched: {misowned}")

    per_device_ctrl = {}
    for e in ctrl:
        per_device_ctrl[e.get("peer_id")] = per_device_ctrl.get(e.get("peer_id"), 0) + 1
    if sessions_per_device is not None:
        want_ctrl = sessions_per_device * args.expect_samples
        wrongctrl = {p: n for p, n in per_device_ctrl.items() if n != want_ctrl}
        check(f"each device received exactly {want_ctrl} confirmed controls", not wrongctrl,
              f"devices with a different count: {wrongctrl}")
        check(f"the gateway confirmed exactly {telemetry_expected} controls",
              len(ctrl) == telemetry_expected, f"confirmed {len(ctrl)}")
    elif per_device_ctrl:
        nocontrol = [p for p, n in per_device_ctrl.items() if n < 1]
        check("each device received at least one confirmed control", not nocontrol, f"{nocontrol}")

    # --- no cross-talk ------------------------------------------------------
    terminated = [e for e in gw if e.get("outcome") == "terminated"]
    abnormal = [e for e in terminated if e.get("peer_id") not in by_peer and e.get("peer_id")]
    check("no unauthenticated device produced a business termination", not abnormal,
          f"{[(e.get('peer_id'), e.get('detail')) for e in abnormal]}")

    # --- F4: exact per-session / per-operation correlation -------------------
    # Identity is (run_id, device_id, SID, sample_id, operation_id). A device id
    # appearing inside an operation_id string is NOT proof of ownership, and
    # per-device totals are not enough: every single session must carry its own
    # expected business volume, and every control must match the telemetry that
    # triggered it within the SAME session.

    established_keys = {(e.get("run_id"), e.get("peer_id"), e.get("session_id"))
                        for e in established}

    def biz_key(e):
        return (e.get("run_id"), e.get("peer_id"), e.get("session_id"))

    # 0. run_id consistency across every stream we were given.
    stream_ids = set()
    for e in gw:
        rid = e.get("run_id")
        if rid:
            stream_ids.add(rid)
    if (summary or {}).get("run_id"):
        stream_ids.add(summary["run_id"])
    if args.run_id and stream_ids:
        wrong = sorted(x for x in stream_ids if x != args.run_id)
        check("every stream carries the same run_id (gateway and summary)", not wrong,
              f"unexpected run ids: {wrong[:3]}")

    # 1. every business event belongs to a session that was really established
    unlinked = sorted({biz_key(e) for e in telem + ctrl if biz_key(e) not in established_keys})
    check("every business event belongs to an established session", not unlinked,
          f"events with an unknown session: {unlinked[:5]}")

    # 2. per-session volume: each established session must carry exactly the
    #    expected number of telemetry samples and confirmed controls.
    sessions_per_device = args.expect_sessions
    want_per_session = args.expect_samples
    per_sess_t, per_sess_c = {}, {}
    for e in telem:
        per_sess_t[biz_key(e)] = per_sess_t.get(biz_key(e), 0) + 1
    for e in ctrl:
        per_sess_c[biz_key(e)] = per_sess_c.get(biz_key(e), 0) + 1
    wrong_vol = []
    for key in sorted(established_keys):
        got_t, got_c = per_sess_t.get(key, 0), per_sess_c.get(key, 0)
        if got_t != want_per_session or got_c != want_per_session:
            wrong_vol.append((key[1], str(key[2])[:8], got_t, got_c))
    check(f"every session carried exactly {want_per_session} telemetry and {want_per_session} controls",
          not wrong_vol, f"mismatched sessions: {wrong_vol[:5]}")

    # 3. sample ids inside a session must be the expected distinct set
    #    (1..samples). Cross-session reuse is allowed; intra-session duplicates
    #    and gaps are not.
    bad_samples = []
    for key in sorted(established_keys):
        got = sorted({e.get("sample_id") for e in telem if biz_key(e) == key})
        if got != list(range(1, want_per_session + 1)):
            bad_samples.append((key[1], str(key[2])[:8], got))
    check(f"every session's telemetry sample ids are exactly 1..{want_per_session}",
          not bad_samples, f"bad sample sets: {bad_samples[:5]}")

    # 4. every confirmed control carries an operation_id and a sample_id, and the
    #    sample_id must match a telemetry sample IN THE SAME SESSION (variant B).
    missing_op = [(e.get("peer_id"), e.get("sample_id")) for e in ctrl if not e.get("operation_id")]
    check("every confirmed control carries an operation_id", not missing_op,
          f"controls without one: {sorted(set(missing_op))[:5]}")
    telemetry_keys = {(biz_key(e), e.get("sample_id")) for e in telem}
    orphan_controls = sorted({(e.get("peer_id"), str(e.get("session_id"))[:8], e.get("sample_id"))
                              for e in ctrl
                              if (biz_key(e), e.get("sample_id")) not in telemetry_keys})
    check("every confirmed control matches a telemetry sample in the same session",
          not orphan_controls, f"controls without a matching telemetry: {orphan_controls[:5]}")

    # 5. operation_id must be tied to the session it appeared in, not merely
    #    contain the device id (variant A).
    op_by_session = {}
    for e in ctrl:
        op_by_session.setdefault((biz_key(e), e.get("sample_id")), set()).add(e.get("operation_id"))
    inconsistent = sorted({(k[0][1], str(k[0][2])[:8], k[1], sorted(v))
                           for k, v in op_by_session.items() if len(v) != 1})
    check("each (session, sample) maps to exactly one operation_id", not inconsistent,
          f"inconsistent operation ids: {inconsistent[:5]}")

    # 6. the operation_id must be the one the gateway generated for that device
    #    and sample: set-interval-<device>-<sample>. This is a REAL ownership
    #    check against the emitter's format, not a substring coincidence.
    wrong_owner = sorted({(e.get("peer_id"), e.get("sample_id"), e.get("operation_id"))
                          for e in ctrl
                          if e.get("operation_id") != f"set-interval-{e.get('peer_id')}-{e.get('sample_id')}"})
    check("every control's operation_id belongs to its own device and sample",
          not wrong_owner, f"foreign operation ids: {wrong_owner[:5]}")

    # 7. sent vs confirmed are counted from their OWN events and must agree.
    # B4: the CURRENT schema requires independent send evidence for every
    # confirmed control. A stream with confirmations but no send events is
    # legacy / evidence-incomplete and FAILS unless --legacy is passed, so a
    # historical run can be re-examined without its incompleteness being mistaken
    # for acceptance.
    sent_ev = [e for e in gw if e.get("stage") == "business" and e.get("outcome") == "control-sent"]
    if not sent_ev and ctrl:
        if args.legacy:
            result = {
                "result": "FAIL",
                "result_kind": "legacy_incomplete",
                "controls_sent": None,
                "controls_confirmed": len(ctrl),
                "problems": ["this stream has no control-sent events and cannot satisfy the current acceptance gate"],
            }
            out = json.dumps(result, indent=2, ensure_ascii=False)
            with open(args.out, "w", encoding="utf-8", newline="\n") as fh:
                fh.write(out + "\n")
            print(out)
            print("VERDICT: FAIL — legacy stream without send evidence (controls_sent=null)")
            sys.exit(1)
        check("every confirmed control needs an independent control-sent event", False,
              f"confirmed={len(ctrl)} but control-sent events=0")
    else:
        check("controls_sent is counted from control-sent events",
              len(sent_ev) == len(ctrl),
              f"sent events={len(sent_ev)} confirmed events={len(ctrl)}")
        # Multiset comparison: an event counts once per occurrence, so a
        # duplicated confirmation cannot cover a missing send.
        from collections import Counter
        sent_multi = Counter((e.get("run_id"), e.get("peer_id"), e.get("session_id"),
                              e.get("sample_id"), e.get("operation_id")) for e in sent_ev)
        confirmed_multi = Counter((e.get("run_id"), e.get("peer_id"), e.get("session_id"),
                                   e.get("sample_id"), e.get("operation_id")) for e in ctrl
                                  if e.get("operation_id"))
        missing = confirmed_multi - sent_multi
        extra = sent_multi - confirmed_multi
        check("every confirmed control has a matching control-sent event (one-to-one)",
              not missing, f"confirmed without sent: {sorted(missing)[:3]}")
        check("no duplicate or unmatched control-sent events",
              not extra, f"sent without confirmation: {sorted(extra)[:3]}")

    # 8. bad-device accounting must be exact.
    if args.expect_bad_device:
        rows_bad = [r for r in (summary or {}).get("results", [])
                    if r.get("device_id") == args.expect_bad_device]
        check(f"exactly one result row for {args.expect_bad_device}", len(rows_bad) == 1,
              f"found {len(rows_bad)}")
        if len(rows_bad) == 1:
            row = rows_bad[0]
            check("the bad device's ok field is a boolean", isinstance(row.get("ok"), bool),
                  f"type={type(row.get('ok')).__name__}")
            check("the bad device's ok is false", row.get("ok") is False, f"ok={row.get('ok')!r}")
            check("the bad device has a failure message",
                  isinstance(row.get("error"), str) and bool(row.get("error").strip()),
                  f"error={row.get('error')!r}")
        check("the bad device was not authenticated by the gateway",
              args.expect_bad_device not in by_peer,
              f"authenticated peers included {args.expect_bad_device}")
        # F4: every expected GOOD device must have exactly one result row too.
        good_missing = [d for d in (f"D{i:03d}" for i in range(1, args.expect_devices + 1))
                        if d != args.expect_bad_device
                        and len([r for r in (summary or {}).get("results", [])
                                 if r.get("device_id") == d]) != 1]
        check("every expected good device has exactly one result row", not good_missing,
              f"missing or duplicated rows: {good_missing[:5]}")

    result = {
        "result": "PASS" if not problems else "FAIL",
        "run_id": args.run_id,
        "expected_devices": args.expect_devices,
        "devices_with_hybrid": sum(
            1 for p in by_peer if any(e.get("suite") == args.expect_suite for e in by_peer[p])),
        "hybrid_sessions_total": sum(
            1 for p in by_peer for e in by_peer[p] if e.get("suite") == args.expect_suite),
        "sessions_per_device_expected": sessions_per_device,
        "unique_sid_count": len(set(full)),
        "telemetry_expected": telemetry_expected,
        "telemetry_accepted": sum(per_device_telem.values()) if per_device_telem else 0,
        # F4: sent and confirmed are counted from their OWN events. Older streams
        # without control-sent events report null rather than a number copied
        # from the confirmed count.
        "controls_sent": len(sent_ev) if sent_ev else None,
        "controls_confirmed": len(ctrl),
        "selected_suite": args.expect_suite,
        "kem_used_count": sum(
            1 for p in by_peer if any(e.get("kem_used") is True for e in by_peer[p])),
        # Backwards-compatible aliases retained so older readers do not break.
        "devices_authenticated": len(by_peer),
        "hybrid_sessions": total_sessions,
        "checks": checks,
        "failures": problems,
    }

    for c in checks:
        print(f"{'ok  ' if c['ok'] else 'FAIL'} {c['name']}" + (f" — {c['detail']}" if c['detail'] and not c['ok'] else ""))
    if problems:
        print()
        print(f"VERDICT: FAIL — {len(problems)} problem(s)")
        for p in problems:
            print(f"  {p}")
    else:
        print()
        print(f"VERDICT: PASS — {len(by_peer)} devices, suite {args.expect_suite}, "
              f"all with the KEM, {len(set(full))} unique session ids")

    if args.out:
        with open(args.out, "w", encoding="utf-8") as fh:
            json.dump(result, fh, indent=2)
    return 0 if not problems else 1


if __name__ == "__main__":
    sys.exit(main())
