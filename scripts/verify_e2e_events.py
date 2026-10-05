#!/usr/bin/env python3
"""Strict verifier for the traditional end-to-end event streams.

Shared by scripts/m2-e2e.sh and scripts/m2-e2e.ps1 so both platforms apply
EXACTLY the same acceptance rules. The M2 script decided with grep and sed
counts on one side only; this checks the structured event fields on both sides
and verifies the telemetry -> control -> acknowledgement correspondence.

What it refuses:
  * a line that is not a JSON object                  (line number reported)
  * an event missing role/stage/outcome, or a run_id that disagrees with the rest
  * a wrong role, a wrong peer identity, or suite != 1
  * a session id that is not the FULL 32 lowercase hex characters, or that the
    two endpoints disagree about
  * a telemetry sample or a control/ack pair that does not correspond
  * any 'terminated' outcome, or an endpoint that marks the session incomplete
  * a gateway that still has an unacknowledged control when the session ends

Usage
  verify_e2e_events.py --gateway GW.jsonl --device DEV.jsonl \
      [--run-id ID] [--samples N] [--device-id D001] [--gateway-id GW001] \
      [--expect-suite 1] [--out RESULT.json]

Exit: 0 verified, 1 not verified.
"""
import argparse
import json
import os
import re
import sys

HEX32 = re.compile(r"^[0-9a-f]{32}$")


def load(path, problems, label):
    if not os.path.exists(path):
        problems.append(f"{label} event file does not exist: {path}")
        return []
    events = []
    with open(path, encoding="utf-8") as fh:
        raw_lines = fh.readlines()
    if not raw_lines:
        problems.append(f"{label} event file is empty: {path}")
        return []
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
        for required in ("role", "stage", "outcome"):
            if not isinstance(ev.get(required), str):
                problems.append(f"{label} line {n} has no string {required!r}")
        if not isinstance(ev.get("run_id"), str) or not ev.get("run_id"):
            problems.append(f"{label} line {n} has no run_id")
        events.append((n, ev))
    if not events:
        problems.append(f"{label} produced no usable events")
    return events


def pick(events, role, stage, outcome):
    return [ev for _, ev in events
            if ev.get("role") == role and ev.get("stage") == stage and ev.get("outcome") == outcome]


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--gateway", required=True)
    ap.add_argument("--device", required=True)
    ap.add_argument("--run-id", default=None)
    ap.add_argument("--samples", type=int, default=3)
    ap.add_argument("--device-id", default="D001")
    ap.add_argument("--gateway-id", default="GW001")
    ap.add_argument("--expect-suite", type=int, default=1)
    ap.add_argument("--expect-kem", choices=["on", "off"], default=None,
                    help="require the KEM to have been used (on) or not used (off)")
    ap.add_argument("--out")
    args = ap.parse_args()

    problems = []
    checks = []

    def check(name, ok, detail=""):
        checks.append({"name": name, "ok": bool(ok), "detail": detail})
        if not ok:
            problems.append(f"{name}: {detail}" if detail else name)
        return ok

    gw = load(args.gateway, problems, "gateway")
    dev = load(args.device, problems, "device")
    all_events = [ev for _, ev in gw] + [ev for _, ev in dev]

    # --- run identity --------------------------------------------------------
    if all_events:
        run_ids = {ev.get("run_id") for ev in all_events}
        if args.run_id:
            check("every event carries the expected run_id", run_ids == {args.run_id},
                  f"run_ids present: {sorted(run_ids)}")
        else:
            check("all events share one run_id", len(run_ids) == 1, f"run_ids: {sorted(run_ids)}")

    # --- handshake on both sides --------------------------------------------
    gw_hs = pick(gw, "gateway", "handshake", "established")
    dev_hs = pick(dev, "device", "handshake", "established")
    if not check("exactly one gateway handshake", len(gw_hs) == 1, f"found {len(gw_hs)}"):
        gw_hs = gw_hs[:1]
    if not check("exactly one device handshake", len(dev_hs) == 1, f"found {len(dev_hs)}"):
        dev_hs = dev_hs[:1]

    sid = None
    if gw_hs and dev_hs:
        g, d = gw_hs[0], dev_hs[0]
        check("gateway authenticated the device", g.get("peer_id") == args.device_id,
              f"gateway peer_id={g.get('peer_id')!r}, expected {args.device_id!r}")
        check("device authenticated the gateway", d.get("peer_id") == args.gateway_id,
              f"device peer_id={d.get('peer_id')!r}, expected {args.gateway_id!r}")
        check(f"both endpoints negotiated suite {args.expect_suite}",
              g.get("suite") == args.expect_suite and d.get("suite") == args.expect_suite,
              f"gateway suite={g.get('suite')}, device suite={d.get('suite')}")
        # The negotiated suite is not enough on its own: also verify the KEM flag,
        # so "suite 2 was selected" and "a KEM secret actually entered the key
        # schedule" cannot be conflated in the acceptance record.
        if args.expect_kem == "on":
            check("both endpoints report that the KEM was used",
                  g.get("kem_used") is True and d.get("kem_used") is True,
                  f"gateway kem_used={g.get('kem_used')}, device kem_used={d.get('kem_used')}")
        elif args.expect_kem == "off":
            check("neither endpoint reports a KEM secret",
                  not g.get("kem_used") and not d.get("kem_used"),
                  f"gateway kem_used={g.get('kem_used')}, device kem_used={d.get('kem_used')}")
        g_sid, d_sid = g.get("session_id"), d.get("session_id")
        check("gateway reports a full 32-hex session id",
              isinstance(g_sid, str) and bool(HEX32.match(g_sid)), f"got {g_sid!r}")
        check("device reports a full 32-hex session id",
              isinstance(d_sid, str) and bool(HEX32.match(d_sid)), f"got {d_sid!r}")
        check("both endpoints agree on the session id", g_sid == d_sid,
              f"gateway {g_sid!r} vs device {d_sid!r}")
        check("the gateway built a channel", g.get("channel_created") is True)
        check("the device built a channel", d.get("channel_created") is True)
        if isinstance(g_sid, str) and HEX32.match(g_sid):
            sid = g_sid

    # --- business correspondence --------------------------------------------
    gw_telem = pick(gw, "gateway", "business", "telemetry-accepted")
    dev_telem = pick(dev, "device", "business", "telemetry-sent")
    gw_ctrl = pick(gw, "gateway", "business", "control-confirmed")
    dev_ctrl = pick(dev, "device", "business", "control-applied")

    n = args.samples
    check(f"gateway accepted {n} telemetry samples", len(gw_telem) == n, f"found {len(gw_telem)}")
    check(f"device sent {n} telemetry samples", len(dev_telem) == n, f"found {len(dev_telem)}")
    check(f"gateway confirmed {n} controls", len(gw_ctrl) == n, f"found {len(gw_ctrl)}")
    check(f"device applied {n} controls", len(dev_ctrl) == n, f"found {len(dev_ctrl)}")

    gw_telem_ids = sorted(e.get("sample_id") for e in gw_telem)
    dev_telem_ids = sorted(e.get("sample_id") for e in dev_telem)
    want_ids = list(range(1, n + 1))
    check("telemetry sample ids are exactly 1..N on both sides",
          gw_telem_ids == want_ids and dev_telem_ids == want_ids,
          f"gateway {gw_telem_ids}, device {dev_telem_ids}")

    # Each sample must have a matching control on the gateway and an applied
    # control on the device, with the SAME operation id.
    gw_ops = {e.get("operation_id") for e in gw_ctrl}
    dev_ops = {e.get("operation_id") for e in dev_ctrl}
    expected_ops = {f"set-interval-{args.device_id}-{i}" for i in want_ids}
    check("gateway control operation ids match the expected set", gw_ops == expected_ops,
          f"got {sorted(gw_ops)}")
    check("device acknowledged exactly those operation ids", dev_ops == gw_ops,
          f"gateway {sorted(gw_ops)} vs device {sorted(dev_ops)}")

    gw_pairs = sorted((e.get("sample_id"), e.get("operation_id")) for e in gw_ctrl)
    dev_pairs = sorted((e.get("sample_id"), e.get("operation_id")) for e in dev_ctrl)
    check("each sample maps to its own control on both sides", gw_pairs == dev_pairs,
          f"gateway {gw_pairs} vs device {dev_pairs}")

    # --- outcomes ------------------------------------------------------------
    terminated = [f"{ev.get('role')}:{ev.get('detail')}" for _, ev in gw + dev
                  if ev.get("outcome") == "terminated"]
    check("no endpoint reported an abnormal termination", not terminated, "; ".join(terminated))

    incomplete = [f"{ev.get('role')}:{ev.get('stage')}:{ev.get('detail')}" for _, ev in gw + dev
                  if ev.get("incomplete") is True]
    check("no endpoint marked the session incomplete", not incomplete, "; ".join(incomplete))

    gw_terminal = [ev for _, ev in gw
                   if ev.get("stage") == "business"
                   and ev.get("outcome") in ("completed", "peer-closed", "terminated")]
    dev_terminal = [ev for _, ev in dev
                    if ev.get("stage") == "business"
                    and ev.get("outcome") in ("completed", "peer-closed", "terminated")]
    check("the gateway reported a terminal business outcome", len(gw_terminal) >= 1,
          f"found {len(gw_terminal)}")
    if gw_terminal:
        t = gw_terminal[-1]
        check("the gateway has no unacknowledged control", not t.get("pending_controls"),
              f"pending_controls={t.get('pending_controls')}")
        check("the gateway's terminal outcome is not a failure",
              t.get("outcome") in ("completed", "peer-closed"), f"outcome={t.get('outcome')!r}")
    check("the device reported a completed session",
          any(ev.get("outcome") == "completed" for ev in dev_terminal),
          f"outcomes: {[ev.get('outcome') for ev in dev_terminal]}")

    # --- result --------------------------------------------------------------
    result = {
        "result": "PASS" if not problems else "FAIL",
        "run_id": args.run_id,
        "session_id": sid,
        "selected_suite": args.expect_suite,
        "kem_expected": args.expect_kem,
        "samples": n,
        "gateway_events": len(gw),
        "device_events": len(dev),
        "checks": checks,
        "failures": problems,
        "note": "single functional end-to-end run; NOT a benchmark and NOT a security proof",
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
        print(f"VERDICT: PASS — session {sid}")

    if args.out:
        with open(args.out, "w", encoding="utf-8") as fh:
            json.dump(result, fh, indent=2)
    return 0 if not problems else 1


if __name__ == "__main__":
    sys.exit(main())
