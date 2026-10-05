#!/usr/bin/env python3
"""Standalone self-test for the fleet verifier (R1 E2).

This is deliberately separate from summarize_selftest.py. The review required a
dedicated self-test because the fleet verifier accepted three kinds of tampered
evidence: a business event whose operation_id had been removed, business events
re-pointed at a session that was never established, and a missing (or duplicated)
bad-device result row.

The fixture is built in this file: a synthetic but internally consistent
gateway event stream plus a matching fleet summary, shaped exactly like the
real 20x3 run. Positive cases must PASS; each tampered variant must FAIL with a
naming reason.
"""
import json
import os
import subprocess
import sys
import tempfile

HERE = os.path.dirname(os.path.abspath(__file__))
VERIFIER = os.path.join(HERE, "verify_fleet_events.py")

RUN_ID = "m4_2-selftest"
GW = "GW001"
SUITE = 2
SAMPLES = 2
SESSIONS = 2
DEVICES = 3
BAD = "D003"          # the third device is the deliberate failure
GOOD_DEVICES = 2      # D001, D002


def sid(seed):
    return f"{seed:032x}"


def build_good():
    """An internally consistent 3-device run: 2 good + 1 bad."""
    gw, summary_rows = [], []
    peer = 0
    for i in range(1, DEVICES + 1):
        dev = f"D{i:03d}"
        if dev == BAD:
            # The bad device uses an identity the registry does not trust, so its
            # handshake fails: it produces NO established session at all.
            summary_rows.append({"device_id": dev, "ok": False,
                                 "error": "handshake: untrusted identity"})
            continue
        for s in range(1, SESSIONS + 1):
            session = sid(i * 100 + s)
            peer += 1
            gw.append({"run_id": RUN_ID, "role": "gateway", "stage": "handshake",
                       "outcome": "established", "peer_id": dev,
                       "session_id": session, "suite": SUITE, "kem_used": True})
            for sample in range(1, SAMPLES + 1):
                op = f"set-interval-{dev}-{sample}"
                gw.append({"run_id": RUN_ID, "role": "gateway", "stage": "business",
                           "outcome": "telemetry-accepted", "peer_id": dev,
                           "session_id": session, "sample_id": sample, "suite": SUITE})
                gw.append({"run_id": RUN_ID, "role": "gateway", "stage": "business",
                           "outcome": "control-sent", "peer_id": dev,
                           "session_id": session, "sample_id": sample,
                           "operation_id": op, "suite": SUITE})
                gw.append({"run_id": RUN_ID, "role": "gateway", "stage": "business",
                           "outcome": "control-confirmed", "peer_id": dev,
                           "session_id": session, "sample_id": sample,
                           "operation_id": op, "suite": SUITE})
        summary_rows.append({"device_id": dev, "ok": True})
    summary = {"run_id": RUN_ID, "expected_devices": DEVICES,
               "successful_devices": GOOD_DEVICES, "results": summary_rows}
    return gw, summary


def write_and_run_legacy(gw, summary, expect_devices=DEVICES, bad=BAD, sessions=SESSIONS):
    """Same as write_and_run but passes --legacy for historical re-examination."""
    import tempfile as _tf
    tmp = _tf.mkdtemp()
    gp = os.path.join(tmp, "gw.jsonl"); sp = os.path.join(tmp, "summary.json")
    out = os.path.join(tmp, "verify.json")
    with open(gp, "w", encoding="utf-8") as fh:
        for e in gw: fh.write(json.dumps(e) + "\n")
    with open(sp, "w", encoding="utf-8") as fh:
        json.dump(summary, fh)
    cmd = [sys.executable, VERIFIER, "--gateway", gp, "--summary", sp,
           "--run-id", RUN_ID, "--expect-devices", str(expect_devices),
           "--expect-suite", str(SUITE), "--expect-samples", str(SAMPLES),
           "--expect-sessions", str(sessions), "--out", out, "--legacy"]
    if bad: cmd += ["--expect-bad-device", bad]
    proc = subprocess.run(cmd, capture_output=True, text=True)
    verdict = None
    if os.path.exists(out):
        with open(out, encoding="utf-8") as fh: verdict = json.load(fh)
    return proc.returncode, verdict, proc.stdout + proc.stderr


def write_and_run(gw, summary, expect_devices=DEVICES, bad=BAD, sessions=SESSIONS):
    tmp = tempfile.mkdtemp()
    gp = os.path.join(tmp, "gw.jsonl")
    sp = os.path.join(tmp, "summary.json")
    out = os.path.join(tmp, "verify.json")
    with open(gp, "w", encoding="utf-8") as fh:
        for e in gw:
            fh.write(json.dumps(e) + "\n")
    with open(sp, "w", encoding="utf-8") as fh:
        json.dump(summary, fh)
    cmd = [sys.executable, VERIFIER, "--gateway", gp, "--summary", sp,
           "--run-id", RUN_ID, "--expect-devices", str(expect_devices),
           "--expect-suite", str(SUITE), "--expect-samples", str(SAMPLES),
           "--expect-sessions", str(sessions), "--out", out]
    if bad:
        cmd += ["--expect-bad-device", bad]
    proc = subprocess.run(cmd, capture_output=True, text=True)
    verdict = None
    if os.path.exists(out):
        with open(out, encoding="utf-8") as fh:
            verdict = json.load(fh)
    return proc.returncode, verdict, proc.stdout + proc.stderr


def strip_field(gw, outcome, field):
    out = []
    for e in gw:
        if e.get("outcome") == outcome and field in e:
            e = {k: v for k, v in e.items() if k != field}
        out.append(e)
    return out


def main():
    failures = []
    checks = 0

    def check(name, ok, detail=""):
        nonlocal checks
        checks += 1
        if not ok:
            failures.append((name, detail))

    gw, summary = build_good()

    # positive control: the untampered run must pass
    code, v, _ = write_and_run(gw, summary)
    check("untampered run passes", code == 0 and v and v["result"] == "PASS",
          f"code={code} failures={v.get('failures') if v else None}")

    # tamper 1: remove operation_id from confirmed controls
    code, v, _ = write_and_run(strip_field(gw, "control-confirmed", "operation_id"), summary)
    check("missing operation_id is rejected", code != 0 and v and v["result"] == "FAIL",
          f"code={code}")
    if v:
        check("rejection names the operation_id rule",
              any("operation_id" in f for f in v["failures"]), str(v["failures"])[:200])

    # tamper 2: point business events at a session that was never established
    tampered = []
    for e in gw:
        if e.get("outcome") in ("telemetry-accepted", "control-confirmed"):
            e = dict(e)
            e["session_id"] = sid(999999)
        tampered.append(e)
    code, v, _ = write_and_run(tampered, summary)
    check("business on an unknown session is rejected", code != 0 and v and v["result"] == "FAIL",
          f"code={code}")
    if v:
        check("rejection names the unknown-session rule",
              any("established session" in f for f in v["failures"]), str(v["failures"])[:200])

    # tamper 3a: drop the bad device's result row
    dropped = dict(summary)
    dropped["results"] = [r for r in summary["results"] if r["device_id"] != BAD]
    dropped["successful_devices"] = DEVICES
    code, v, _ = write_and_run(gw, dropped)
    check("a missing bad-device row is rejected", code != 0 and v and v["result"] == "FAIL",
          f"code={code}")
    if v:
        check("rejection names the missing bad-device row",
              any("exactly one result row" in f for f in v["failures"]),
              str(v["failures"])[:200])

    # tamper 3b: duplicate the bad device's row
    duplicated = dict(summary)
    duplicated["results"] = summary["results"] + [
        r for r in summary["results"] if r["device_id"] == BAD]
    code, v, _ = write_and_run(gw, duplicated)
    check("a duplicated bad-device row is rejected", code != 0 and v and v["result"] == "FAIL",
          f"code={code}")

    # tamper 3c: bad device reported ok=true
    flipped = dict(summary)
    flipped["results"] = [dict(r, ok=True, error="")
                          if r["device_id"] == BAD else r for r in summary["results"]]
    code, v, _ = write_and_run(gw, flipped)
    check("a bad device reported as ok=true is rejected", code != 0 and v and v["result"] == "FAIL",
          f"code={code}")

    # tamper 3d: bad device row with a non-boolean ok
    typed = dict(summary)
    typed["results"] = [dict(r, ok="false") if r["device_id"] == BAD else r
                        for r in summary["results"]]
    code, v, _ = write_and_run(gw, typed)
    check("a non-boolean ok field is rejected", code != 0 and v and v["result"] == "FAIL",
          f"code={code}")

    # tamper 4: telemetry without a sample_id
    code, v, _ = write_and_run(strip_field(gw, "telemetry-accepted", "sample_id"), summary)
    check("telemetry without sample_id is rejected", code != 0 and v and v["result"] == "FAIL",
          f"code={code}")

    # --- F4: exact-relationship variants ------------------------------------
    def tamper(fn):
        g2 = [dict(e) for e in gw]
        fn(g2)
        return g2

    # A. operation_id replaced by an arbitrary string that still CONTAINS the
    #    device id. Ownership must come from the emitter's format, not a substring.
    def A(events):
        for e in events:
            if e.get("outcome") == "control-confirmed" and e.get("peer_id") == "D001":
                e["operation_id"] = f"whatever-contains-D001-inside-{e.get('sample_id')}"
    code, v, _ = write_and_run(tamper(A), summary)
    check("F4-A foreign operation_id is rejected", code != 0 and v and v["result"] == "FAIL",
          f"code={code}")
    if v:
        check("F4-A rejection names ownership",
              any("operation_id belongs" in f for f in v["failures"]), str(v["failures"])[:160])

    # B. confirmed sample_id moved to 99999, which no telemetry in that session has
    def B(events):
        for e in events:
            if e.get("outcome") == "control-confirmed" and e.get("peer_id") == "D001":
                e["sample_id"] = 99999
    code, v, _ = write_and_run(tamper(B), summary)
    check("F4-B control sample not matching telemetry is rejected", code != 0, f"code={code}")

    # C. all of one device's business moved into its FIRST session
    first_sid = None
    for e in gw:
        if e.get("peer_id") == "D001" and e.get("outcome") == "established":
            first_sid = e["session_id"]
            break
    def C(events):
        for e in events:
            if e.get("peer_id") == "D001" and e.get("outcome") in ("telemetry-accepted",
                                                                   "control-sent", "control-confirmed"):
                e["session_id"] = first_sid
    code, v, _ = write_and_run(tamper(C), summary)
    check("F4-C business collapsed into one session is rejected", code != 0, f"code={code}")

    # D. fleet summary run_id changed
    other = dict(summary)
    other["run_id"] = "SOME-OTHER-RUN"
    code, v, _ = write_and_run(gw, other)
    check("F4-D mismatched summary run_id is rejected", code != 0, f"code={code}")

    # E/B4: control-sent events removed entirely -> the CURRENT gate must FAIL.
    no_sent = [e for e in gw if e.get("outcome") != "control-sent"]
    code, v, _ = write_and_run(no_sent, summary)
    check("B4-E deleting all control-sent events fails the current gate",
          code != 0 and v and v.get("result") == "FAIL", f"code={code}")
    if v:
        named = any("control-sent" in str(p)
                    for p in list(v.get("problems", [])) +
                    [c.get("name", "") + c.get("detail", "") for c in v.get("checks", [])])
        check("B4-E the plain gate names the missing send evidence", named,
              json.dumps(v.get("checks"))[:160])

    # B4: a legacy stream can be re-examined with --legacy, but the verdict is
    # still FAIL with controls_sent=null (never a pass).
    code, v, _ = write_and_run_legacy(no_sent, summary)
    check("B4-E legacy re-examination still does not pass",
          code != 0 and v and v.get("controls_sent") is None
          and v.get("result_kind") == "legacy_incomplete",
          f"code={code} kind={v.get('result_kind') if v else None}")

    # B4: one send event removed -> multiset mismatch must fail
    def drop_one_send(events):
        removed = False
        out = []
        for e in events:
            if e.get("outcome") == "control-sent" and not removed:
                removed = True
                continue
            out.append(e)
        return out
    code, v, _ = write_and_run(drop_one_send(gw), summary)
    check("B4 deleting ONE control-sent event fails (multiset, not set)",
          code != 0, f"code={code}")

    # B4: a duplicated confirmation cannot cover a missing send
    def dup_confirm_drop_send(events):
        out, removed = [], False
        extra = None
        for e in events:
            if e.get("outcome") == "control-sent" and not removed:
                removed = True
                extra = dict(e)
                extra.pop("outcome")
                extra["outcome"] = "control-confirmed"
                continue
            out.append(e)
        out.append(extra)
        return out
    code, v, _ = write_and_run(dup_confirm_drop_send(gw), summary)
    check("B4 a duplicated confirmation cannot cover a missing send", code != 0, f"code={code}")

    # F. duplicate sample_id inside one session
    def F(events):
        dup = None
        for e in events:
            if e.get("peer_id") == "D001" and e.get("outcome") == "telemetry-accepted":
                dup = dict(e)
                break
        if dup is not None:
            events.append(dup)
    code, v, _ = write_and_run(tamper(F), summary)
    check("F4-F duplicated sample in one session is rejected", code != 0, f"code={code}")

    # G. a good device's result row removed
    missing_row = dict(summary)
    missing_row["results"] = [r for r in summary["results"] if r["device_id"] != "D002"]
    code, v, _ = write_and_run(gw, missing_row)
    check("F4-G missing good-device row is rejected", code != 0, f"code={code}")

    print()
    if failures:
        print(f"{len(failures)} of {checks} fleet verifier self-test checks FAILED")
        for name, detail in failures:
            print(f"  {name}: {detail}")
        return 1
    print(f"all {checks} fleet verifier self-test checks behaved as required")
    return 0


if __name__ == "__main__":
    sys.exit(main())
