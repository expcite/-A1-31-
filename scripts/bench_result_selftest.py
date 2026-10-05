#!/usr/bin/env python3
"""Self-test for the M5A-R1 bench result validator.

R1 change: this self-test no longer carries its own acceptance logic. It builds
fixture result directories on disk (one positive control and one defective
variant per rejection the review listed) and invokes the SAME formal validator
the production harness uses -- `bench -verify`, which calls
internal/bench.VerifyResults. There is exactly one acceptance judgement.

Rejected variants, each of which must produce problems and a non-zero exit:
  1. status=RUNNING as a final state
  2. raw rows say 10 successes, summary says 0
  3. negative latency and negative wire bytes
  4. a hybrid run whose trial negotiated suite 1 with suite_ok=true
  5. success=true while handshake_success=false
  6. two trials sharing one session id
  7. a deleted trial row whose summary was shrunk to match

Plus the positive control: a complete, consistent run must pass.
"""
import json
import math
import os
import subprocess
import sys
import tempfile

HERE = os.path.dirname(os.path.abspath(__file__))
ROOT = os.path.dirname(HERE)
GO = os.environ.get("GO", "go")


def build_validator():
    out = os.path.join(tempfile.mkdtemp(), "bench.exe")
    proc = subprocess.run([GO, "build", "-o", out, "./cmd/bench"],
                          cwd=ROOT, capture_output=True, text=True)
    if proc.returncode != 0:
        print("building the validator failed:", proc.stderr)
        sys.exit(1)
    return out


def trial(i, **over):
    t = {
        "schema_version": "m5a-r1/1", "run_id": "run-1", "trial_id": i,
        "mode": "hybrid", "started_at_utc": "2026-09-28T00:00:00Z",
        "success": True, "handshake_success": True, "probe_success": True,
        "gateway_confirmed": True,
        "suite": 2, "suite_ok": True, "peer_id_ok": True, "finished_ok": True,
        "session_id": f"{i:032x}",
        "operation_id": f"set-interval-D001-{i % 3 + 1}",
        "authenticated_peer_id": "GW001", "not_started_stage": "",
        "tcp_connect_ns": 100000, "handshake_total_ns": 2000000 + i, "probe_ns": 300000,
        "probe_confirm_wait_ns": 1000,
        "handshake_client_to_server_bytes": 1492,
        "handshake_server_to_client_bytes": 1360,
        "app_client_to_server_bytes": 200, "app_server_to_client_bytes": 180,
        "cpu_total_ns": None,
        "cpu_unavailable_reason": "per-trial CPU time requires an isolated subprocess harness",
        "peak_rss_bytes": None,
        "peak_rss_unavailable_reason": "not exposed by the Go standard library here",
        "go_heap_alloc_bytes": 123456, "go_sys_bytes": 6543210,
        "control_applied": True, "control_duplicate": False,
        "applied_interval_ms": 5000,
    }
    t.update(over)
    return t


def config_hash(mode, trials, timeout_ns, suites, schema_version="m5a-r1/1"):
    """Recompute Go configHash: fnv1a over the alphabetically sorted map."""
    payload = json.dumps(
        {"mode": mode, "schema_version": schema_version, "suites": suites,
         "timeout_ns": timeout_ns, "trials": trials}, sort_keys=True,
        separators=(",", ":")).encode()
    h = 1469598103934665603
    for b in payload:
        h ^= b
        h = (h * 1099511628211) & 0xFFFFFFFFFFFFFFFF
    return "fnv1a-%016x" % h


def manifest(trials):
    return {
        "schema_version": "m5a-r1/1", "run_id": "run-1", "run_kind": "smoke",
        "mode": "hybrid", "requested_trials": trials, "timeout_per_trial_ns": 30000000000,
        "started_at_utc": "2026-09-30T00:00:00Z", "finished_at_utc": "2026-09-30T00:01:00Z",
        "config_hash": config_hash("hybrid", trials, 30000000000, [2]),
        "source_manifest_sha256": "fixture",
        "go_version": "go1.27.1", "goos": "windows", "goarch": "amd64",
        "suites": [2], "expected_device_id": "D001", "expected_gateway_id": "GW001",
        "formal_results_eligible": False,
        "formal_not_eligible_reason": "smoke run: small scale",
        "metric_support": {"cpu_total_ns": None, "peak_rss_bytes": None},
    }


def summary(trial_rows, gateway_confirmed):
    ok = sum(1 for t in trial_rows if t.get("success"))
    hs = sorted(t["handshake_total_ns"] for t in trial_rows if t.get("handshake_success"))
    med = (hs[len(hs)//2] if len(hs) % 2 else (hs[len(hs)//2 - 1] + hs[len(hs)//2]) // 2) if hs else None
    p95 = hs[max(0, int(math.ceil(0.95 * len(hs))) - 1)] if hs else None
    mean = sum(hs) // len(hs) if hs else None
    return {
        "schema_version": "m5a-r1/1", "run_id": "run-1", "run_kind": "smoke",
        "mode": "hybrid", "requested_trials": 10,
        "started_trials": len(trial_rows), "completed_trials": ok,
        "successes": ok, "failures": len(trial_rows) - ok,
        "not_started": 10 - len(trial_rows),
        "success_pct": 100.0 * ok / 10 if trial_rows else 0.0,
        "success_pct_denominator": "requested_trials",
        "handshake_samples": len(hs),
        "handshake_median_ns": med,
        "handshake_p95_ns": p95, "handshake_mean_ns": mean,
        "probe_samples": ok, "probe_median_ns": 300000,
        "probe_p95_ns": 300000, "probe_mean_ns": 300000,
        "percentile_definition": "median = conventional; P95 = nearest-rank",
        "handshake_c2s_bytes_first_trial": 1492, "handshake_s2c_bytes_first_trial": 1360,
        "handshake_c2s_bytes_total": 1492 * len(trial_rows),
        "handshake_s2c_bytes_total": 1360 * len(trial_rows),
        "app_c2s_bytes_total": 200 * ok, "app_s2c_bytes_total": 180 * ok,
        "gateway_confirmed_count": gateway_confirmed,
        "formal_results_eligible": False, "formal_not_eligible_reason": "smoke",
    }


def gateway_events(rows):
    ev = []
    for t in rows:
        if t.get("success"):
            ev.append({"stage": "business", "outcome": "control-confirmed",
                       "run_id": "run-1", "peer_id": "D001",
                       "session_id": t["session_id"], "operation_id": t["operation_id"],
                       "sample_id": 1})
    return ev


def write_dir(root, name, manifest_obj, rows, sum_obj, status, events):
    d = os.path.join(root, name)
    os.makedirs(d, exist_ok=True)
    def w(fn, obj):
        with open(os.path.join(d, fn), "w", encoding="utf-8", newline="\n") as fh:
            if fn.endswith("trials.jsonl") or fn.endswith("gateway_events.jsonl"):
                for e in obj:
                    fh.write(json.dumps(e) + "\n")
            else:
                fh.write(json.dumps(obj, indent=2) + "\n")
    w("manifest.json", manifest_obj)
    w("trials.jsonl", rows)
    if sum_obj is not None:
        w("summary.json", sum_obj)
    w("status.json", status)
    w("gateway_events.jsonl", events)
    return d


def main():
    validator = build_validator()

    def run_verify(d, exit_code=0):
        proc = subprocess.run([validator, "-verify", "-dir", d,
                               "-exit-code", str(exit_code)],
                              capture_output=True, text=True)
        problems = []
        try:
            problems = json.loads(proc.stdout).get("problems", [])
        except json.JSONDecodeError:
            problems = ["validator output was not JSON: " + proc.stdout[:200]]
        return proc.returncode, problems

    good_rows = [trial(i) for i in range(1, 11)]
    good_events = gateway_events(good_rows)
    good_summary = summary(good_rows, len(good_events))
    good_status = {"schema_version": "m5a-r1/1", "status": "PASS",
                   "reason": "all trials completed", "run_id": "run-1",
                   "written_at_utc": "2026-09-30T00:01:00Z"}

    checks = []
    def check(name, ok, detail=""):
        checks.append((name, ok, detail))

    # positive control
    root = tempfile.mkdtemp()
    d = write_dir(root, "good", manifest(10), good_rows, good_summary, good_status, good_events)
    code, problems = run_verify(d, 0)
    check("a complete consistent run passes", code == 0 and not problems,
          f"code={code} problems={problems[:2]}")

    def variant(name, rows=None, sum_obj=None, status=None, events=None):
        r = rows if rows is not None else good_rows
        e = events if events is not None else gateway_events(r)
        so = sum_obj if sum_obj is not None else summary(r, len(e))
        st = dict(status) if status is not None else dict(good_status)
        st.setdefault("written_at_utc", "2026-09-30T00:01:00Z")
        return write_dir(root, name, manifest(10), r, so, st, e)

    # 1. RUNNING as a final state
    code, problems = run_verify(variant("running", status={"schema_version": "m5a-r1/1",
        "status": "RUNNING", "reason": "still going", "run_id": "run-1",
        "written_at_utc": "2026-09-30T00:01:00Z"}), 0)
    check("RUNNING final state rejected", code != 0 and any("RUNNING" in p for p in problems),
          f"code={code} problems={problems[:1]}")

    # 2. raw rows say 10 successes, summary says 0
    bad = summary(good_rows, len(good_events))
    bad["successes"], bad["failures"] = 0, 10
    code, problems = run_verify(variant("badsummary", sum_obj=bad), 3)
    check("summary contradicting the rows rejected",
          code != 0 and any("successes" in p for p in problems),
          f"code={code} problems={problems[:1]}")

    # 3. negative latency and bytes
    neg = [dict(t) for t in good_rows]
    neg[0]["handshake_total_ns"] = -7
    neg[0]["handshake_client_to_server_bytes"] = -100
    code, problems = run_verify(variant("negative", rows=neg), 0)
    check("negative latency/bytes rejected",
          code != 0 and any("below the minimum" in p or "negative" in p for p in problems),
          f"code={code} problems={problems[:1]}")

    # 4. suite mismatch with suite_ok=true
    mis = [dict(t) for t in good_rows]
    mis[0]["suite"] = 1
    code, problems = run_verify(variant("suite", rows=mis), 0)
    check("suite mismatch rejected", code != 0 and any("negotiated suite" in p for p in problems),
          f"code={code} problems={problems[:1]}")

    # 5. success=true with handshake_success=false
    unb = [dict(t) for t in good_rows]
    unb[0]["handshake_success"] = False
    unb[0]["gateway_confirmed"] = False
    code, problems = run_verify(variant("unbacked", rows=unb), 0)
    check("unbacked success rejected", code != 0 and any("claims success" in p for p in problems),
          f"code={code} problems={problems[:1]}")

    # 6. duplicate session id
    dup = [dict(t) for t in good_rows]
    dup[1]["session_id"] = dup[0]["session_id"]
    code, problems = run_verify(variant("dupsid", rows=dup), 0)
    check("duplicate SID rejected",
          code != 0 and any("reused" in p or "reuses session id" in p for p in problems),
          f"code={code} problems={problems[:1]}")

    # 7. deleted trial with a shrunk summary
    shrunk_rows = good_rows[:9]
    shrunk_events = gateway_events(shrunk_rows)
    shrunk = summary(shrunk_rows, len(shrunk_events))
    shrunk["requested_trials"] = 10
    code, problems = run_verify(variant("deleted", rows=shrunk_rows, sum_obj=shrunk,
                                        events=shrunk_events), 0)
    check("deleted trial row rejected",
          code != 0 and any("requested trials were started" in p or "not_started" in p
                            for p in problems),
          f"code={code} problems={problems[:1]}")

    # missing gateway confirmation evidence
    code, problems = run_verify(variant("noconfirm", events=[]), 0)
    check("success without gateway confirmation rejected",
          code != 0 and any("control-confirmed" in p for p in problems),
          f"code={code} problems={problems[:1]}")

    failed = [c for c in checks if not c[1]]
    print()
    if failed:
        print(f"{len(failed)} of {len(checks)} bench validator self-test checks FAILED")
        for name, ok, detail in failed:
            print(f"  {name}: {detail}")
        return 1
    print(f"all {len(checks)} bench validator self-test checks behaved as required "
          f"(against the production Go validator)")
    return 0


if __name__ == "__main__":
    sys.exit(main())
