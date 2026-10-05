#!/usr/bin/env python3
"""Summarize and VERIFY a `go test -json` stream.

This replaces an earlier version that reported success for empty, garbage and
truncated input. The review reproduced that (exit 0 for all three) and it is a
real defect: a verifier that cannot tell "no failures were found" from "the
evidence was complete and passed" will certify a broken run.

What this version insists on
----------------------------
1. NON-EMPTY input. An empty stream is unusable evidence, not a pass.
2. STRICT JSON on every line. A line that is not a JSON object is reported with
   its line number and makes the evidence unusable. Malformed input is never
   silently skipped.
3. FIELD TYPES are checked, and the event vocabulary matches the Go toolchain's
   `test2json` semantics for the Go version in use, including the `build-output`
   and `build-fail` actions added in Go 1.24.
4. LIFECYCLE COMPLETENESS. Every package `start` must reach a terminal action and
   every test `run` must end. A log that stops mid-run is truncated, not passing.
5. The EXPECTED PACKAGE SET must be covered, supplied by the caller from the
   command actually run. No test count is hardcoded: adding or removing tests
   must not change whether this verifier passes.
6. The REAL `go test` EXIT CODE is correlated. A non-zero exit with no `fail`
   event (build failure, panic, timeout, cancellation) must still fail. When the
   exit code is not supplied the result is "evidence incomplete", never a pass.

Exit codes
----------
  0  verified complete pass
  1  the run failed (a fail event, a build failure, or go's exit code != 0)
  2  the evidence is unusable (empty, malformed, unclosed, incomplete coverage,
     or the go exit code was not supplied)

Keeping 1 and 2 apart matters: "the tests failed" and "I cannot tell whether the
tests passed" are different answers and must not be collapsed.

Usage
-----
  summarize-go-test-json.py TESTS.jsonl --go-exit-code 0 \
      --expect-package ./... [--out SUMMARY.txt] [--json SUMMARY.json] \
      [--negatives NEG.jsonl] [--require-tests]
"""
import argparse
import json
import re
import sys

EXIT_PASS = 0
EXIT_RUN_FAILED = 1
EXIT_EVIDENCE_UNUSABLE = 2

# Actions emitted by `go test -json`. `build-output` and `build-fail` exist from
# Go 1.24; treating them as unknown would make valid modern logs unusable.
PACKAGE_ACTIONS = {"start", "pass", "fail", "skip", "build-output", "build-fail"}
TEST_ACTIONS = {"run", "pause", "cont", "output", "pass", "fail", "skip"}
TERMINAL = {"pass", "fail", "skip"}

# Structured M4 lifecycle / rekey / isolation case lines.
#
# E1 (R1): the emitter writes `M4CASE key=value ...` pairs. Older versions of
# this parser matched a FIXED field order, so adding one field (for example
# record_replay_accepted) silently dropped every such line from the evidence
# file while the run still reported PASS. The parser is now order-independent:
#
#   - a line is a case line if it contains the M4CASE marker;
#   - every `key=value` pair is collected, in any order, and unknown extra
#     fields are preserved rather than rejected;
#   - a case line that is missing a REQUIRED field, or whose tri-state fields
#     are not true/false/null, is a PARSE FAILURE and makes the whole evidence
#     run incomplete (non-zero exit) instead of being silently dropped.
#
# Counts always come from the data; nothing here hardcodes how many cases a run
# should have.
M4CASE_MARKER = "M4CASE"

# Fields every case must carry. Anything else is kept as an extra.
M4CASE_REQUIRED_STR = ("kind", "scenario", "expected", "actual", "detail")
M4CASE_REQUIRED_TRI = (
    "device_channel_created",
    "gateway_channel_created",
    "plaintext_delivered",
    "business_accepted",
)
# Optional fields understood by later emitters; they are parsed when present and
# keep the tri-state rule.
M4CASE_OPTIONAL_TRI = ("record_replay_accepted",)

# key=value, where value is a quoted Go string or a bare token.
M4_KV_RE = re.compile(r'(\w+)=("(?:[^"\\]|\\.)*"|[^\s]+)')

def parse_kv_case(text, marker, required_str, required_tri, optional_tri=()):
    """Parse one `MARKER key=value ...` line, order-independently and strictly.

    F3 rules, applied to BOTH case families:
      - every required string key must be PRESENT and non-empty;
      - every required tri-state key must be PRESENT (absent != null) and must be
        exactly true/false/null;
      - a key appearing twice is rejected rather than letting the last value
        silently overwrite the earlier one;
      - unknown extra keys are preserved (forward compatible) but never satisfy
        a required key.
    """
    fields = {}
    for m in M4_KV_RE.finditer(text):
        key, raw = m.group(1), m.group(2)
        if key == marker:
            continue
        if key in fields:
            return None, f"duplicate key {key!r}"
        if raw.startswith('"'):
            fields[key] = go_string(raw)
        else:
            fields[key] = raw
    for key in required_str:
        if key not in fields:
            return None, f"missing required field {key!r}"
        if not isinstance(fields[key], str) or not fields[key]:
            return None, f"field {key!r} must be a non-empty string"
    for key in required_tri + tuple(optional_tri):
        # PRESENCE is checked for required and optional alike; an omitted
        # optional field stays absent, an omitted REQUIRED field is an error.
        if key in fields:
            v = fields[key]
            if v not in ("true", "false", "null"):
                return None, f"field {key!r} must be true/false/null, got {v!r}"
            fields[key] = {"true": True, "false": False, "null": None}[v]
    for key in required_tri:
        if key not in fields:
            return None, f"missing required field {key!r}"
    ordered = {}
    for key in required_str + required_tri + tuple(optional_tri):
        if key in fields:
            ordered[key] = fields[key]
    for key, val in fields.items():
        if key not in ordered:
            ordered[key] = val
    return ordered, None


def parse_m4_case(text):
    """Parse one M4CASE line. See parse_kv_case for the rules."""
    return parse_kv_case(text, M4CASE_MARKER, M4CASE_REQUIRED_STR,
                         M4CASE_REQUIRED_TRI, M4CASE_OPTIONAL_TRI)


def parse_neg_case(text):
    """Parse one M2NEG line. See parse_kv_case for the rules."""
    return parse_kv_case(text, "M2NEG", NEG_REQUIRED_STR, NEG_REQUIRED_TRI)


# Structured negative-case lines from the tamper and downgrade tests.
#
# The tri-state fields accept true / false / null, because "not observed" is a
# distinct answer from "false".
#
# M3 added business_accepted, target and mutation between plaintext_delivered and
# detail. The regex below matches the CURRENT emitted order; when the emitter's
# field set changes, this regex must change with it — otherwise extraction
# silently yields zero cases and an evidence file comes out empty without the
# verifier noticing. The self-test guards against exactly that regression.
# F3: the negative-case family uses the SAME order-independent, strict parser as
# M4CASE, so adding a field can no longer make records disappear silently.
NEG_REQUIRED_STR = ("name", "scenario", "expected_stage", "actual_stage",
                    "target", "mutation", "detail")
NEG_REQUIRED_TRI = ("device_channel_created", "gateway_channel_created",
                    "plaintext_delivered", "business_accepted")


class Unusable(Exception):
    """The evidence cannot support any conclusion."""


def go_string(raw):
    return json.loads(raw)


def tri(raw):
    return None if raw == "null" else raw == "true"


def parse_stream(path):
    """Parse the stream, raising Unusable on anything that is not valid input."""
    events = []
    bad_lines = []
    non_object = []
    try:
        with open(path, encoding="utf-8") as fh:
            raw_lines = fh.readlines()
    except FileNotFoundError:
        raise Unusable(f"input file does not exist: {path}")
    except OSError as exc:
        raise Unusable(f"cannot read {path}: {exc}")

    if not raw_lines:
        raise Unusable("input is empty: zero bytes is not a passing test run")

    non_blank = 0
    for n, line in enumerate(raw_lines, start=1):
        if not line.strip():
            continue
        non_blank += 1
        try:
            ev = json.loads(line)
        except json.JSONDecodeError as exc:
            bad_lines.append((n, str(exc)))
            continue
        if not isinstance(ev, dict):
            non_object.append((n, type(ev).__name__))
            continue
        action = ev.get("Action")
        if not isinstance(action, str):
            bad_lines.append((n, f"Action is {type(action).__name__}, expected string"))
            continue
        pkg = ev.get("Package")
        if action in PACKAGE_ACTIONS and not isinstance(pkg, str):
            bad_lines.append((n, f"action {action!r} needs a string Package"))
            continue
        test = ev.get("Test")
        if test is not None and not isinstance(test, str):
            bad_lines.append((n, f"Test is {type(test).__name__}, expected string"))
            continue
        out = ev.get("Output")
        if out is not None and not isinstance(out, str):
            bad_lines.append((n, f"Output is {type(out).__name__}, expected string"))
            continue
        if action == "run" and not test:
            bad_lines.append((n, "action 'run' needs a Test name"))
            continue
        events.append(ev)

    if non_blank == 0:
        raise Unusable("input has no non-blank lines")
    if bad_lines:
        detail = "; ".join(f"line {n}: {why}" for n, why in bad_lines[:10])
        more = "" if len(bad_lines) <= 10 else f" (and {len(bad_lines) - 10} more)"
        raise Unusable(f"{len(bad_lines)} malformed event line(s): {detail}{more}")
    if non_object:
        detail = "; ".join(f"line {n}: JSON {kind}" for n, kind in non_object[:10])
        raise Unusable(f"{len(non_object)} line(s) are not JSON objects: {detail}")
    return events


def analyse(events):
    """Walk the lifecycle.

    Lifecycle problems (an unclosed package, a test with no result, a terminal
    action with no start event) are collected into `problems` rather than raised,
    because a DEFINITE failure must still be reported as a failed run even when
    the log is also incomplete. Only structurally unparseable input is raised.
    """
    packages = {}          # pkg -> terminal action
    started = set()
    terminals_without_start = set()
    open_tests = {}        # (pkg, test) -> True
    top = {"pass": 0, "fail": 0, "skip": 0}
    sub = {"pass": 0, "fail": 0, "skip": 0}
    negatives = []
    m4cases = []
    build_failed = set()
    test_seen = set()
    problems = []

    for ev in events:
        action = ev.get("Action")
        pkg = ev.get("Package") or ""
        test = ev.get("Test") or ""

        if action == "output":
            text = ev.get("Output") or ""
            # M4 lifecycle/rekey cases: order-independent, strict on required
            # fields. A line carrying the marker that cannot be parsed is an
            # evidence failure, not a silent gap.
            if M4CASE_MARKER in text:
                case, why = parse_m4_case(text)
                if case is None:
                    problems.append(
                        f"unparseable M4CASE in {test}: {why}: {text.strip()[:200]}")
                else:
                    case["test"] = test
                    m4cases.append(case)
            if "M2NEG" in text:
                case, why = parse_neg_case(text)
                if case is None:
                    problems.append(
                        f"unparseable M2NEG in {test}: {why}: {text.strip()[:200]}")
                else:
                    case["test"] = test
                    negatives.append(case)
            continue

        if action == "build-fail":
            build_failed.add(pkg)
            continue

        if action == "build-output":
            continue

        if action == "start":
            started.add(pkg)
            continue

        if action in TERMINAL and test == "":
            if pkg not in started:
                terminals_without_start.add(pkg)
            packages[pkg] = action
            continue

        if action == "run":
            open_tests[(pkg, test)] = True
            test_seen.add((pkg, test))
            continue

        if action == "pause":
            continue

        if action == "cont":
            open_tests.setdefault((pkg, test), True)
            continue

        if action in TERMINAL:
            key = (pkg, test)
            if key not in open_tests and key not in test_seen:
                problems.append(f"test {pkg}/{test} reported {action} without a run event")
            open_tests.pop(key, None)
            bucket = sub if "/" in test else top
            bucket[action] += 1
            continue

    if not started:
        problems.append("no package start event: this does not look like a `go test -json` stream")
    unclosed_pkgs = sorted(started - set(packages))
    if unclosed_pkgs:
        problems.append("the stream ends with package(s) still running (truncated log): "
                        + ", ".join(unclosed_pkgs[:5]))
    if open_tests:
        sample = ", ".join(f"{p}/{t}" for p, t in list(open_tests)[:5])
        problems.append(f"the stream ends with test(s) still running: {sample}")
    if terminals_without_start:
        problems.append("terminal action without a start event for package(s): "
                        + ", ".join(sorted(terminals_without_start)[:5]))

    # A failure is self-evidencing: if any fail event or build failure is present,
    # the run failed, and that conclusion does not depend on the log being
    # complete. Completeness is only required to certify a PASS.
    any_failure = bool(build_failed) or top["fail"] > 0 or sub["fail"] > 0 \
        or any(a == "fail" for a in packages.values())

    return {
        "packages": packages,
        "top_level": top,
        "subtests": sub,
        "negatives": negatives,
        "m4_cases": m4cases,
        "build_failed": sorted(build_failed),
        "problems": problems,
        "any_failure": any_failure,
    }


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("tests_jsonl")
    ap.add_argument("--go-exit-code", type=int, default=None,
                    help="the REAL exit code of the `go test` command. Required for a pass.")
    ap.add_argument("--expect-package", action="append", default=[],
                    help="a package the command was expected to cover; repeatable.")
    ap.add_argument("--allow-extra-packages", action="store_true",
                    help="do not fail when the log covers packages outside the expected set")
    ap.add_argument("--require-tests", action="store_true",
                    help="fail if no test was executed at all")
    ap.add_argument("--out")
    ap.add_argument("--json")
    ap.add_argument("--negatives")
    ap.add_argument("--m4-cases",
                    help="write all structured M4 lifecycle/rekey/isolation cases to this JSON Lines file")
    ap.add_argument("--m4-kind", action="append", default=[],
                    help="with --m4-cases, restrict to these kinds (repeatable)")
    ap.add_argument("--downgrades",
                    help="also write the subset of negative cases that describe a wire-level attack "
                         "(those carrying a target and a mutation) to this JSON Lines file")
    args = ap.parse_args()

    verdict = "UNUSABLE"
    reasons = []
    summary = {}

    try:
        events = parse_stream(args.tests_jsonl)
        result = analyse(events)
    except Unusable as exc:
        print(f"VERDICT: UNUSABLE EVIDENCE (exit {EXIT_EVIDENCE_UNUSABLE})", file=sys.stderr)
        print(f"reason: {exc}", file=sys.stderr)
        payload = {"verdict": "UNUSABLE", "exit_code": EXIT_EVIDENCE_UNUSABLE,
                   "reason": str(exc), "input": args.tests_jsonl}
        if args.json:
            with open(args.json, "w", encoding="utf-8") as fh:
                json.dump(payload, fh, indent=2)
        return EXIT_EVIDENCE_UNUSABLE

    packages = result["packages"]
    top = result["top_level"]
    sub = result["subtests"]
    negatives = result["negatives"]
    m4cases = result["m4_cases"]

    failed_pkgs = sorted(p for p, a in packages.items() if a == "fail")
    ok_pkgs = sorted(p for p, a in packages.items() if a == "pass")
    skip_pkgs = sorted(p for p, a in packages.items() if a == "skip")

    # --- what could not be established --------------------------------------
    #
    # Two questions are answered separately:
    #   run_failed  : is there positive evidence that the run failed?
    #   incomplete  : is the evidence good enough to certify a PASS?
    # A definite failure outweighs incompleteness, because "something failed" is a
    # conclusion the evidence already supports. Only a PASS needs the evidence to
    # be complete.
    run_failed = result["any_failure"] or (args.go_exit_code not in (None, 0))
    incomplete = bool(result["problems"])

    if result["build_failed"]:
        reasons.append("build failed for package(s): " + ", ".join(result["build_failed"]))
    if failed_pkgs:
        reasons.append("package(s) failed: " + ", ".join(failed_pkgs))
    if top["fail"] or sub["fail"]:
        reasons.append(f"failing tests: top={top['fail']} sub={sub['fail']}")
    if args.go_exit_code is None:
        reasons.append(
            "the real `go test` exit code was not supplied, so a failure with no fail event "
            "(build error, panic, timeout, cancellation) cannot be excluded; pass --go-exit-code")
        incomplete = True
    elif args.go_exit_code != 0:
        reasons.append(f"`go test` exited {args.go_exit_code}, which is not success")

    if result["problems"]:
        for problem in result["problems"]:
            reasons.append(problem)

    if args.expect_package:
        expected = set(args.expect_package)
        missing = sorted(expected - set(packages))
        extra = sorted(set(packages) - expected)
        if missing:
            reasons.append("expected package(s) absent from the log: " + ", ".join(missing))
            incomplete = True
        if extra and not args.allow_extra_packages:
            reasons.append("log covers package(s) outside the expected set: " + ", ".join(extra))
            incomplete = True

    if args.require_tests and not (top["pass"] or top["fail"] or top["skip"]):
        reasons.append("no test was executed at all")
        incomplete = True

    lines = [
        f"packages        : {len(packages)} total, {len(ok_pkgs)} ok, "
        f"{len(failed_pkgs)} failed, {len(skip_pkgs)} without tests",
    ]
    for p in failed_pkgs:
        lines.append(f"  FAILED PACKAGE: {p}")
    lines += [
        f"top-level tests : pass={top['pass']} fail={top['fail']} skip={top['skip']}",
        f"subtests        : pass={sub['pass']} fail={sub['fail']} skip={sub['skip']}",
        f"negative cases  : {len(negatives)}",
        f"m4 cases        : {len(m4cases)}",
    ]

    if run_failed:
        code = EXIT_RUN_FAILED
        verdict = "FAIL"
    elif incomplete:
        code = EXIT_EVIDENCE_UNUSABLE
        verdict = "INCOMPLETE"
    else:
        code = EXIT_PASS
        verdict = "PASS"

    if verdict == "PASS":
        lines.append("VERDICT: PASS (exit 0) "
                     "— evidence complete, expected packages covered, go exit code 0")
    else:
        lines.append(f"VERDICT: {verdict} (exit {code})")
        for r in reasons:
            lines.append(f"  reason: {r}")

    text = "\n".join(lines)
    print(text)

    summary = {
        "verdict": verdict,
        "exit_code": code,
        "run_failed": run_failed,
        "evidence_incomplete": incomplete,
        "reasons": reasons,
        "input": args.tests_jsonl,
        "go_exit_code": args.go_exit_code,
        "packages_total": len(packages),
        "packages_ok": len(ok_pkgs),
        "packages_failed": failed_pkgs,
        "packages_without_tests": skip_pkgs,
        "expected_packages": sorted(args.expect_package),
        "top_level_tests": top,
        "subtests": sub,
        "negative_cases": len(negatives),
        "m4_cases": len(m4cases),
    }
    if args.out:
        with open(args.out, "w", encoding="utf-8") as fh:
            fh.write(text + "\n")
            fh.write(json.dumps(summary, indent=2) + "\n")
    if args.json:
        with open(args.json, "w", encoding="utf-8") as fh:
            json.dump(summary, fh, indent=2)
    if args.negatives:
        with open(args.negatives, "w", encoding="utf-8") as fh:
            for n in negatives:
                fh.write(json.dumps(n, ensure_ascii=False) + "\n")
        print(f"wrote {len(negatives)} negative cases to {args.negatives}")
    if args.m4_cases:
        chosen = [c for c in m4cases if not args.m4_kind or c.get("kind") in args.m4_kind]
        with open(args.m4_cases, "w", encoding="utf-8") as fh:
            for c in chosen:
                fh.write(json.dumps(c, ensure_ascii=False) + "\n")
        print(f"wrote {len(chosen)} M4 cases to {args.m4_cases}")
    if args.downgrades:
        # A downgrade/attack case is one that records WHAT was changed. Plain
        # tampering cases from earlier milestones do not carry target/mutation.
        attacks = [n for n in negatives if n.get("target") or n.get("mutation")]
        with open(args.downgrades, "w", encoding="utf-8") as fh:
            for n in attacks:
                fh.write(json.dumps(n, ensure_ascii=False) + "\n")
        print(f"wrote {len(attacks)} wire-level attack cases to {args.downgrades}")

    return code


if __name__ == "__main__":
    sys.exit(main())
