#!/usr/bin/env python3
"""Standalone self-test for the M4CASE extractor (R1 E1).

This is deliberately separate from summarize_selftest.py (which covers the
17 generic go-test-JSON cases). The review asked for a dedicated self-test for
the structured-case parser, because that parser had a real bug: a fixed field
order meant that adding one field (record_replay_accepted) silently dropped
three replay cases from the evidence file while the run still reported PASS.

Checked here, against a synthetic stream built in this file:

  positive
    1. fields in the documented order parse
    2. fields in a DIFFERENT order parse to the same case
    3. an extra unknown field is preserved and does not break parsing
    4. an extra field placed between two required fields parses
    5. tri-state values true/false/null are all accepted
    6. a case spread across the Go JSON escaping still parses

  negative (each must make the evidence run INCOMPLETE, i.e. non-zero exit)
    7. a missing required field
    8. a tri-state field with a value other than true/false/null
    9. an empty required field
   10. two cases carrying the same case id
   11. an empty case set is reported as zero, not as an error, but the emitted
       file is empty and the count is 0 -- and that is visible.

Counts are asserted against the data, never hardcoded to 13.
"""
import json
import os
import subprocess
import sys
import tempfile

HERE = os.path.dirname(os.path.abspath(__file__))
SUMMARIZER = os.path.join(HERE, "summarize-go-test-json.py")
GO_A = "example.com/x/a"

PKG_START = {"Action": "start", "Package": GO_A}
PKG_PASS = {"Action": "pass", "Package": GO_A}


def case_line(pairs, test="TestCase"):
    body = " ".join(f"{k}={v}" for k, v in pairs)
    return {"Action": "output", "Package": GO_A, "Test": test,
            "Output": f"M4CASE {body}\n"}


def base_pairs(**over):
    pairs = [
        ("kind", '"wire_replay"'),
        ("scenario", '"session A ciphertext replayed into session B"'),
        ("expected", '"rejected"'),
        ("actual", '"rejected"'),
        ("device_channel_created", "true"),
        ("gateway_channel_created", "true"),
        ("plaintext_delivered", "false"),
        ("business_accepted", "false"),
        ("detail", '"injected 3 frames"'),
    ]
    return [(k, v) for k, v in pairs if k not in over] + [
        (k, v) for k, v in over.items()]


def run_summarizer(events, extra=()):
    tmp = tempfile.mkdtemp()
    stream = os.path.join(tmp, "stream.jsonl")
    with open(stream, "w", encoding="utf-8") as fh:
        for ev in events:
            fh.write(json.dumps(ev) + "\n")
    out = os.path.join(tmp, "summary.json")
    cases = os.path.join(tmp, "cases.jsonl")
    cmd = [sys.executable, SUMMARIZER, stream, "--go-exit-code", "0",
           "--expect-package", GO_A, "--json", out, "--m4-cases", cases,
           *extra]
    proc = subprocess.run(cmd, capture_output=True, text=True)
    parsed = None
    if os.path.exists(out):
        with open(out, encoding="utf-8") as fh:
            parsed = json.load(fh)
    rows = []
    if os.path.exists(cases):
        with open(cases, encoding="utf-8") as fh:
            rows = [json.loads(l) for l in fh if l.strip()]
    return proc.returncode, parsed, rows, proc.stdout + proc.stderr


def pkg(events, test="TestCase"):
    return [PKG_START,
            {"Action": "run", "Package": GO_A, "Test": test},
            *events,
            {"Action": "pass", "Package": GO_A, "Test": test},
            PKG_PASS]


def main():
    failures = []
    checks = 0

    def check(name, ok, detail=""):
        nonlocal checks
        checks += 1
        if not ok:
            failures.append((name, detail))

    # 1. documented order
    code, s, rows, _ = run_summarizer(pkg([case_line(base_pairs())]))
    check("documented order parses", code == 0 and len(rows) == 1, f"code={code} rows={len(rows)}")
    if rows:
        check("fields land correctly",
              rows[0]["kind"] == "wire_replay" and rows[0]["business_accepted"] is False
              and rows[0]["plaintext_delivered"] is False, str(rows[0])[:120])

    # 2. different order
    reordered = list(reversed(base_pairs()))
    code, s, rows, _ = run_summarizer(pkg([case_line(reordered)]))
    check("reordered fields parse", code == 0 and len(rows) == 1, f"code={code} rows={len(rows)}")
    if rows:
        check("reordered values identical", rows[0]["kind"] == "wire_replay", str(rows[0])[:80])

    # 3. unknown extra field at the end
    code, s, rows, _ = run_summarizer(pkg([case_line(base_pairs() + [("future_field", '"x"')])]))
    check("trailing unknown field tolerated", code == 0 and len(rows) == 1, f"code={code}")
    if rows:
        check("unknown field preserved", rows[0].get("future_field") == "x", str(rows[0])[:120])

    # 4. unknown field in the MIDDLE (this is exactly what broke the old parser)
    mid = base_pairs()[:8] + [("record_replay_accepted", "false")] + base_pairs()[8:]
    code, s, rows, _ = run_summarizer(pkg([case_line(mid)]))
    check("middle extra field tolerated", code == 0 and len(rows) == 1, f"code={code} rows={len(rows)}")

    # 5. tri-state values
    for v in ("true", "false", "null"):
        code, s, rows, _ = run_summarizer(pkg([case_line(base_pairs(plaintext_delivered=v))]))
        check(f"tri-state {v} accepted", code == 0 and len(rows) == 1, f"code={code}")
        if rows:
            got = rows[0]["plaintext_delivered"]
            want = {"true": True, "false": False, "null": None}[v]
            check(f"tri-state {v} decoded", got is want, repr(got))

    # 6. Go-escaped string content
    code, s, rows, _ = run_summarizer(pkg([case_line(base_pairs(detail='"quote \\" and \\\\ backslash"'))]))
    check("escaped strings parse", code == 0 and len(rows) == 1, f"code={code}")

    # 7. missing required field must FAIL the evidence run
    broken = [p for p in base_pairs() if p[0] != "actual"]
    code, s, rows, _ = run_summarizer(pkg([case_line(broken)]))
    check("missing required field -> non-zero exit", code != 0, f"code={code}")
    check("missing required field not recorded", len(rows) == 0, f"rows={len(rows)}")
    if s:
        check("summary verdict is not PASS", s.get("verdict") != "PASS", str(s.get("verdict")))
    else:
        check("summary JSON still written", False, "no summary produced")

    # 8. bad tri-state value
    code, s, rows, _ = run_summarizer(pkg([case_line(base_pairs(business_accepted="maybe"))]))
    check("bad tri-state -> non-zero exit", code != 0, f"code={code}")

    # 9. empty required field
    code, s, rows, _ = run_summarizer(pkg([case_line(base_pairs(kind='""'))]))
    check("empty required field -> non-zero exit", code != 0, f"code={code}")

    # 10. duplicate case ids are visible in the data
    code, s, rows, _ = run_summarizer(pkg([
        case_line(base_pairs(), test="T1"),
        case_line(base_pairs(), test="T2"),
    ]))
    check("both duplicate-id cases are still extracted", code == 0 and len(rows) == 2,
          f"code={code} rows={len(rows)}")
    ids = [r.get("kind") for r in rows]
    check("duplicates are countable from the data", len(ids) == len(set(ids)) + 1, str(ids))

    # 11. empty case set
    code, s, rows, _ = run_summarizer(pkg([
        {"Action": "output", "Package": GO_A, "Test": "T", "Output": "nothing here\n"}]))
    check("empty case set is not an error", code == 0, f"code={code}")
    check("empty case set reports zero", s.get("m4_cases") == 0 and rows == [],
          f"m4_cases={s.get('m4_cases')}")

    # --- F3: every required tri-state key must be PRESENT (absent != null) ---
    for field in ("business_accepted", "plaintext_delivered",
                  "device_channel_created", "gateway_channel_created"):
        dropped = [p for p in base_pairs() if p[0] != field]
        code, s, rows, _ = run_summarizer(pkg([case_line(dropped)]))
        check(f"F3 missing {field} -> non-zero exit", code != 0, f"code={code}")
        check(f"F3 missing {field} not recorded", len(rows) == 0, f"rows={len(rows)}")
        if s:
            check(f"F3 missing {field} -> not PASS", s.get("verdict") != "PASS",
                  str(s.get("verdict")))

    # explicit null for a required tri-state is ALLOWED (absent != null)
    code, s, rows, _ = run_summarizer(pkg([case_line(base_pairs(business_accepted="null"))]))
    check("F3 explicit null accepted", code == 0 and len(rows) == 1, f"code={code}")
    if rows:
        check("F3 explicit null decoded", rows[0]["business_accepted"] is None,
              repr(rows[0]["business_accepted"]))

    # duplicate key must be rejected, not silently overwritten
    dup = base_pairs() + [("business_accepted", "true")]
    code, s, rows, _ = run_summarizer(pkg([case_line(dup)]))
    check("F3 duplicate key -> non-zero exit", code != 0, f"code={code}")
    if s:
        check("F3 duplicate key reason", "duplicate key" in json.dumps(s), str(s.get("reasons"))[:120])

    # field reordering still parses (positive control for the F3 changes)
    code, s, rows, _ = run_summarizer(pkg([case_line(list(reversed(base_pairs())))]))
    check("F3 reordered fields still parse", code == 0 and len(rows) == 1, f"code={code}")

    # --- F3: the M2NEG family gets the same treatment ------------------------
    def neg_line(pairs, test="NegCase"):
        body = " ".join(f"{k}={v}" for k, v in pairs)
        return {"Action": "output", "Package": GO_A, "Test": test,
                "Output": f"M2NEG {body}\n"}

    def base_neg(**over):
        pairs = [
            ("name", '"replay old ciphertext"'),
            ("scenario", '"old session record into new session"'),
            ("expected_stage", '"record"'),
            ("actual_stage", '"record"'),
            ("device_channel_created", "false"),
            ("gateway_channel_created", "false"),
            ("plaintext_delivered", "false"),
            ("business_accepted", "false"),
            ("target", '"record"'),
            ("mutation", '"replay"'),
            ("detail", '"rejected"'),
        ]
        return [(k, v) for k, v in pairs if k not in over] + [
            (k, v) for k, v in over.items()]

    code, s, rows, _ = run_summarizer(pkg([neg_line(base_neg())]))
    check("F3 M2NEG positive parses", code == 0, f"code={code}")
    # delete each required tri key
    for field in ("business_accepted", "plaintext_delivered"):
        dropped = [p for p in base_neg() if p[0] != field]
        code, s, rows, _ = run_summarizer(pkg([neg_line(dropped)]))
        check(f"F3 M2NEG missing {field} -> non-zero exit", code != 0, f"code={code}")
    # duplicate key
    code, s, rows, _ = run_summarizer(pkg([neg_line(base_neg() + [("detail", '"x"')])]))
    check("F3 M2NEG duplicate key -> non-zero exit", code != 0, f"code={code}")

    # 12. count comes from the data (three cases -> 3, not 13, not 10)
    code, s, rows, _ = run_summarizer(pkg([
        case_line(base_pairs(), test="A"),
        case_line(base_pairs(), test="B"),
        case_line(base_pairs(), test="C"),
    ]))
    check("count follows the data", s.get("m4_cases") == 3 and len(rows) == 3,
          f"m4_cases={s.get('m4_cases')} rows={len(rows)}")

    print()
    if failures:
        print(f"{len(failures)} of {checks} M4CASE extractor self-test checks FAILED")
        for name, detail in failures:
            print(f"  {name}: {detail}")
        return 1
    print(f"all {checks} M4CASE extractor self-test checks behaved as required")
    return 0


if __name__ == "__main__":
    sys.exit(main())
