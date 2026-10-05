#!/usr/bin/env python3
"""Self-test for scripts/summarize-go-test-json.py.

The M2 review ran the old summarizer against the fixtures in
testdata/summary_selftest/ and got exit 0 for empty, garbage and truncated input.
That is the defect these tests pin shut.

Each case asserts the EXACT exit code, because the distinction matters:
  0 = verified complete pass
  1 = the run failed
  2 = the evidence is unusable

Run:  python3 scripts/summarize_selftest.py
Exit: 0 if every case behaved as required, 1 otherwise.
"""
import json
import os
import subprocess
import sys
import tempfile

HERE = os.path.dirname(os.path.abspath(__file__))
SUMMARIZER = os.path.join(HERE, "summarize-go-test-json.py")
FIXTURES = os.path.join(HERE, "testdata", "summary_selftest")

PASS_CODE = 0
RUN_FAILED = 1
EVIDENCE_UNUSABLE = 2


def run(args):
    proc = subprocess.run(
        [sys.executable, SUMMARIZER] + args,
        capture_output=True, text=True)
    return proc.returncode, proc.stdout, proc.stderr


def write(tmp, name, content):
    path = os.path.join(tmp, name)
    with open(path, "w", encoding="utf-8") as fh:
        fh.write(content)
    return path


def good_stream():
    """A small, complete, passing `go test -json` stream for two packages."""
    lines = [
        {"Action": "start", "Package": "example.com/x/a"},
        {"Action": "run", "Package": "example.com/x/a", "Test": "TestOne"},
        {"Action": "output", "Package": "example.com/x/a", "Test": "TestOne", "Output": "ok\n"},
        {"Action": "pass", "Package": "example.com/x/a", "Test": "TestOne"},
        {"Action": "run", "Package": "example.com/x/a", "Test": "TestOne/sub"},
        {"Action": "pass", "Package": "example.com/x/a", "Test": "TestOne/sub"},
        {"Action": "pass", "Package": "example.com/x/a"},
        {"Action": "start", "Package": "example.com/x/b"},
        {"Action": "skip", "Package": "example.com/x/b"},
    ]
    return "".join(json.dumps(e) + "\n" for e in lines)


def unclosed_test_stream():
    """A stream that runs a test and never reports its result."""
    lines = [
        {"Action": "start", "Package": "example.com/x/a"},
        {"Action": "run", "Package": "example.com/x/a", "Test": "TestOne"},
        {"Action": "output", "Package": "example.com/x/a", "Test": "TestOne", "Output": "partial\n"},
    ]
    return "".join(json.dumps(e) + "\n" for e in lines)


def unclosed_package_stream():
    """A stream whose package never reaches a terminal action."""
    lines = [
        {"Action": "start", "Package": "example.com/x/a"},
        {"Action": "run", "Package": "example.com/x/a", "Test": "TestOne"},
        {"Action": "pass", "Package": "example.com/x/a", "Test": "TestOne"},
    ]
    return "".join(json.dumps(e) + "\n" for e in lines)


def failing_stream():
    lines = [
        {"Action": "start", "Package": "example.com/x/a"},
        {"Action": "run", "Package": "example.com/x/a", "Test": "TestOne"},
        {"Action": "fail", "Package": "example.com/x/a", "Test": "TestOne"},
        {"Action": "fail", "Package": "example.com/x/a"},
    ]
    return "".join(json.dumps(e) + "\n" for e in lines)


def wrong_type_stream():
    """A log whose Package is not a string: structurally invalid evidence."""
    return json.dumps({"Action": "start", "Package": 123}) + "\n"


CASES = []
GO_A = "example.com/x/a"
GO_B = "example.com/x/b"


def case(name, want_code, args):
    CASES.append((name, want_code, args))


def main():
    tmp = tempfile.mkdtemp(prefix="summary-selftest-")

    good = write(tmp, "good.jsonl", good_stream())
    unclosed_t = write(tmp, "unclosed_test.jsonl", unclosed_test_stream())
    unclosed_p = write(tmp, "unclosed_package.jsonl", unclosed_package_stream())
    failing = write(tmp, "failing.jsonl", failing_stream())
    badtype = write(tmp, "badtype.jsonl", wrong_type_stream())
    empty_ok = write(tmp, "empty_ok.jsonl", good_stream())

    # --- the fixtures the review used ---------------------------------------
    fx = lambda n: os.path.join(FIXTURES, n)
    case("empty input is unusable, not a pass", EVIDENCE_UNUSABLE,
         [fx("empty.jsonl"), "--go-exit-code", "0"])
    case("garbage input is unusable, not a pass", EVIDENCE_UNUSABLE,
         [fx("garbage.jsonl"), "--go-exit-code", "0"])
    case("truncated input is unusable, not a pass", EVIDENCE_UNUSABLE,
         [fx("truncated.jsonl"), "--go-exit-code", "0"])
    case("a real failure is reported as a failed run", RUN_FAILED,
         [fx("failure_control.jsonl"), "--go-exit-code", "1"])

    # --- the normal path ----------------------------------------------------
    case("a complete passing log with go exit 0 passes", PASS_CODE,
         [good, "--go-exit-code", "0", "--expect-package", GO_A, "--expect-package", GO_B])

    # --- the cases the request listed explicitly ----------------------------
    case("a missing expected package is unusable", EVIDENCE_UNUSABLE,
         [good, "--go-exit-code", "0", "--expect-package", GO_A, "--expect-package", "example.com/x/missing"])
    case("an unclosed test is unusable", EVIDENCE_UNUSABLE,
         [unclosed_t, "--go-exit-code", "0"])
    case("an unclosed package is unusable", EVIDENCE_UNUSABLE,
         [unclosed_p, "--go-exit-code", "0"])
    case("a failing test is a failed run", RUN_FAILED,
         [failing, "--go-exit-code", "1"])
    case("go exit != 0 with no fail event is still a failed run", RUN_FAILED,
         [good, "--go-exit-code", "1"])
    case("a non-zero go exit with no fail event and a build failure is a failed run", RUN_FAILED,
         [empty_ok, "--go-exit-code", "2"])
    case("wrong field types are unusable", EVIDENCE_UNUSABLE,
         [badtype, "--go-exit-code", "0"])
    case("a missing file is unusable", EVIDENCE_UNUSABLE,
         [os.path.join(tmp, "does-not-exist.jsonl"), "--go-exit-code", "0"])
    case("omitting the go exit code is unusable", EVIDENCE_UNUSABLE,
         [good, "--expect-package", GO_A, "--expect-package", GO_B])
    case("--require-tests with only skipped packages is unusable", EVIDENCE_UNUSABLE,
         [write(tmp, "only_skip.jsonl",
                json.dumps({"Action": "start", "Package": GO_A}) + "\n"
                + json.dumps({"Action": "skip", "Package": GO_A}) + "\n"),
          "--go-exit-code", "0", "--require-tests"])
    case("adding a package outside the expected set is unusable by default", EVIDENCE_UNUSABLE,
         [good, "--go-exit-code", "0", "--expect-package", GO_A])
    case("an extra package is allowed when explicitly permitted", PASS_CODE,
         [good, "--go-exit-code", "0", "--expect-package", GO_A, "--allow-extra-packages"])

    # --- negative-case extraction must not silently yield zero --------------
    #
    # Regression guard: the M2NEG regex lives in the summarizer and the emitter
    # lives in Go test code. If the emitter's field set changes and the regex is
    # not updated, extraction returns ZERO cases and an evidence file comes out
    # empty while the run still reports PASS. That is precisely the failure mode
    # this project is not allowed to have, so a synthetic M2NEG line is checked
    # against the real summarizer.
    neg_input = write(tmp, "negcase.jsonl",
        json.dumps({"Action": "start", "Package": GO_A}) + "\n"
        + json.dumps({"Action": "run", "Package": GO_A, "Test": "TestNeg"}) + "\n"
        + json.dumps({"Action": "output", "Package": GO_A, "Test": "TestNeg", "Output":
            'M2NEG name="TestNeg" scenario="synthetic attack" expected_stage="finished_confirmation" '
            'actual_stage="finished_confirmation" device_channel_created=false '
            'gateway_channel_created=true plaintext_delivered=null business_accepted=null '
            'target="H2" mutation="kem_ct substituted" detail="synthetic"\n'}) + "\n"
        + json.dumps({"Action": "pass", "Package": GO_A, "Test": "TestNeg"}) + "\n"
        + json.dumps({"Action": "pass", "Package": GO_A}) + "\n")
    neg_json = os.path.join(tmp, "neg.json")
    neg_dg = os.path.join(tmp, "neg_dg.json")
    code, out, err = run([neg_input, "--go-exit-code", "0", "--expect-package", GO_A,
                          "--json", neg_json, "--negatives", neg_json + "l",
                          "--downgrades", neg_dg])
    if code != 0:
        print(f"FAIL: negative-case run returned {code}")
        print("  stdout:", out.strip().replace("\n", " | ")[:400])
        print("  stderr:", err.strip().replace("\n", " | ")[:400])
        return 1
    with open(neg_json, encoding="utf-8") as fh:
        payload = json.load(fh)
    if payload.get("negative_cases") != 1:
        print(f"FAIL: M2NEG extraction found {payload.get('negative_cases')} cases, expected 1")
        return 1
    with open(neg_dg, encoding="utf-8") as fh:
        attacks = [json.loads(l) for l in fh if l.strip()]
    if len(attacks) != 1 or attacks[0].get("target") != "H2":
        print(f"FAIL: downgrade extraction wrong: {attacks}")
        return 1
    tri_states = attacks[0]
    if tri_states.get("gateway_channel_created") is not True or tri_states.get("plaintext_delivered") is not None:
        print(f"FAIL: tri-state fields not decoded: {tri_states}")
        return 1
    print("ok   M2NEG extraction and tri-state decoding work on the current field set")

    # --- machine-readable output -------------------------------------------
    json_out = os.path.join(tmp, "summary.json")
    code, out, err = run([good, "--go-exit-code", "0", "--json", json_out])
    if code != PASS_CODE:
        print(f"FAIL: --json run returned {code}")
        return 1
    with open(json_out, encoding="utf-8") as fh:
        payload = json.load(fh)
    if payload.get("verdict") != "PASS" or payload.get("exit_code") != PASS_CODE:
        print(f"FAIL: --json payload is wrong: {payload}")
        return 1
    print("ok   --json emits a machine-readable verdict")

    # --- run every case -----------------------------------------------------
    failures = []
    for name, want, args in CASES:
        code, out, err = run(args)
        if code == want:
            print(f"ok   [{code}] {name}")
        else:
            failures.append((name, want, code, (out + err).strip().splitlines()[:3]))
            print(f"FAIL [got {code}, want {want}] {name}")

    # The self-test must itself be trustworthy: a case that "passes" because the
    # summarizer crashed for an unrelated reason would be worthless.
    code, out, err = run([good, "--go-exit-code", "0"])
    if "Traceback" in (out + err):
        print("FAIL: the summarizer raised an unhandled exception on valid input")
        failures.append(("no traceback on valid input", "no crash", "crash", []))

    print()
    if failures:
        print(f"{len(failures)} of {len(CASES)} cases FAILED")
        for name, want, got, sample in failures:
            print(f"  {name}: want {want}, got {got} {sample}")
        return 1
    print(f"all {len(CASES)} summarizer self-test cases behaved as required")
    return 0


if __name__ == "__main__":
    sys.exit(main())
