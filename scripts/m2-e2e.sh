#!/usr/bin/env bash
#
# M2.1 end-to-end acceptance harness.
#
# Changes since M2, all of which the review asked for:
#   * the file is LF-only (see .gitattributes: *.sh text eol=lf). The M2 file
#     shipped with CRLF and `bash -n` failed on Linux with exit 2.
#   * every run gets its OWN output directory under results/raw/e2e/<run_id>/.
#     A run starts by writing RUNNING and ends by writing PASS, FAIL or
#     INTERRUPTED, so a failed run can never leave a stale PASS behind.
#   * acceptance is delegated to scripts/verify_e2e_events.py, which is shared
#     with the PowerShell harness. The M2 script decided with grep/sed counts and
#     only checked one endpoint's peer id.
#   * failures keep their logs, and the trap does not report success.
#
# Usage:  scripts/m2-e2e.sh [OUTPUT_ROOT]
# Env:    GO=/path/to/go   use a toolchain that is not on PATH
#         PYTHON=python3   interpreter for the shared verifier
#         SAMPLES=N        telemetry samples (default 3)
#
# This is an END-TO-END FUNCTIONAL check. It is NOT a benchmark.

set -uo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
OUT_ROOT="${1:-$ROOT/results/raw/e2e}"
GO="${GO:-go}"
PYTHON="${PYTHON:-python3}"
SAMPLES="${SAMPLES:-3}"
# MODE=traditional  uses an explicit suite-1 demo configuration
# MODE=hybrid       mirrors the SHIPPED default configuration, which declares
#                   minimum_mode=hybrid; only the address is replaced with an
#                   OS-chosen port so runs never collide.
MODE="${MODE:-traditional}"
case "$MODE" in
  traditional)
    CFG_MINMODE=traditional; CFG_SUITES=1; CFG_LEGACY=true
    EXPECT_SUITE=1; EXPECT_KEM=off
    EVENTS_PREFIX=m2_e2e
    ;;
  hybrid)
    CFG_MINMODE=hybrid; CFG_SUITES=2; CFG_LEGACY=false
    EXPECT_SUITE=2; EXPECT_KEM=on
    EVENTS_PREFIX=m3_hybrid_e2e
    ;;
  *)
    echo "unknown MODE: $MODE (use traditional or hybrid)" >&2
    exit 2
    ;;
esac

RUN_ID="m21-$(date -u +%Y%m%dT%H%M%SZ)-$$"
WORK="$(mktemp -d)"
BIN="$WORK/bin"
RUNTIME="$WORK/runtime"
RUN_DIR="$OUT_ROOT/$RUN_ID"

mkdir -p "$BIN" "$RUNTIME" "$RUN_DIR"

# Native Windows binaries do not understand a Git-Bash "/c/..." path, and JSON
# does not escape backslashes for us, so use the mixed form.
if command -v cygpath >/dev/null 2>&1; then
  RUNTIME_NATIVE="$(cygpath -m "$RUNTIME")"
  RUN_DIR_NATIVE="$(cygpath -m "$RUN_DIR")"
else
  RUNTIME_NATIVE="$RUNTIME"
  RUN_DIR_NATIVE="$RUN_DIR"
fi

GW_PID=""
STATUS="INTERRUPTED"
GW_CODE="not-run"
DEV_CODE="not-run"
VERIFY_CODE="not-run"

finish() {
  local code=$?
  if [ -n "$GW_PID" ] && kill -0 "$GW_PID" 2>/dev/null; then
    kill "$GW_PID" 2>/dev/null || true
    wait "$GW_PID" 2>/dev/null || true
    GW_CODE="killed"
  fi
  # The status file is written LAST and holds only THIS run's result, so a
  # failure can never be mistaken for a previous PASS.
  {
    echo "status=$STATUS"
    echo "run_id=$RUN_ID"
    echo "gateway_exit_code=$GW_CODE"
    echo "device_exit_code=$DEV_CODE"
    echo "verifier_exit_code=$VERIFY_CODE"
    echo "script_exit_code=$code"
    echo "samples=$SAMPLES"
  } > "$RUN_DIR/status" 2>/dev/null || true
  echo "$STATUS" > "$RUN_DIR/result" 2>/dev/null || true
  rm -rf "$WORK" 2>/dev/null || true
  exit $code
}
trap finish EXIT
# A signal must never be reported as success.
trap 'STATUS="INTERRUPTED"; exit 130' INT
trap 'STATUS="INTERRUPTED"; exit 143' TERM

echo "RUNNING" > "$RUN_DIR/result"
{ echo "status=RUNNING"; echo "run_id=$RUN_ID"; } > "$RUN_DIR/status"

fail() {
  STATUS="FAIL"
  echo "FAIL: $*" >&2
  echo "FAIL: $*" >> "$RUN_DIR/diagnostics.txt"
  exit 1
}

echo "== run_id: $RUN_ID"
echo "== mode  : $MODE (expecting suite $EXPECT_SUITE, kem $EXPECT_KEM)"
echo "== output: $RUN_DIR"

echo "== 1. build"
(cd "$ROOT" && "$GO" build -o "$BIN/" ./cmd/...) || fail "go build failed"

echo "== 2. create fresh test identities"
"$BIN/secctl" init-demo -dir "$RUNTIME" > "$RUN_DIR/init.json" 2>"$RUN_DIR/init.err" \
  || fail "init-demo failed"

echo "== 3. write the gateway configuration"
cat > "$WORK/gateway.json" <<EOF
{
  "role": "gateway",
  "id": "GW001",
  "address": "127.0.0.1:0",
  "minimum_mode": "$CFG_MINMODE",
  "allow_legacy": $CFG_LEGACY,
  "supported_suites": [$CFG_SUITES],
  "handshake_timeout_ms": 5000,
  "max_frame_bytes": 16384,
  "trust_store": "$RUNTIME_NATIVE/trust.json",
  "key_directory": "$RUNTIME_NATIVE/keys"
}
EOF

echo "== 4. start the gateway"
"$BIN/gateway" -config "$WORK/gateway.json" -max-sessions 1 -run-for 60s \
  -run-id "$RUN_ID" -events "$RUN_DIR_NATIVE/gateway.jsonl" \
  > "$RUN_DIR/gateway.stdout" 2> "$RUN_DIR/gateway.stderr" &
GW_PID=$!

# Readiness comes from the gateway's OWN event, never from a fixed sleep. The
# poll is bounded so a stuck gateway still fails.
ADDR=""
for _ in $(seq 1 200); do
  if [ -s "$RUN_DIR/gateway.jsonl" ]; then
    ADDR="$(sed -n 's/.*"stage":"listen","outcome":"ready","detail":"\([^"]*\)".*/\1/p' \
            "$RUN_DIR/gateway.jsonl" | head -1)"
    [ -n "$ADDR" ] && break
  fi
  if ! kill -0 "$GW_PID" 2>/dev/null; then
    cat "$RUN_DIR/gateway.stderr" >> "$RUN_DIR/diagnostics.txt" 2>/dev/null || true
    fail "gateway did not start"
  fi
  sleep 0.1
done
[ -n "$ADDR" ] || fail "the gateway never reported a listening address"
echo "   gateway is listening on $ADDR"

echo "== 5. write the device configuration and run the device"
cat > "$WORK/device.json" <<EOF
{
  "role": "device",
  "id": "D001",
  "address": "$ADDR",
  "minimum_mode": "$CFG_MINMODE",
  "allow_legacy": $CFG_LEGACY,
  "supported_suites": [$CFG_SUITES],
  "handshake_timeout_ms": 5000,
  "max_frame_bytes": 16384,
  "trust_store": "$RUNTIME_NATIVE/trust.json",
  "key_directory": "$RUNTIME_NATIVE/keys",
  "expected_peer_id": "GW001"
}
EOF

"$BIN/device" -config "$WORK/device.json" -samples "$SAMPLES" -run-for 30s \
  -run-id "$RUN_ID" -events "$RUN_DIR_NATIVE/device.jsonl" \
  > "$RUN_DIR/device.stdout" 2> "$RUN_DIR/device.stderr"
DEV_CODE=$?
if [ "$DEV_CODE" -ne 0 ]; then
  cat "$RUN_DIR/device.stderr" >> "$RUN_DIR/diagnostics.txt" 2>/dev/null || true
  fail "the device run failed with exit code $DEV_CODE"
fi

echo "== 6. wait for the gateway"
wait "$GW_PID"
GW_CODE=$?
GW_PID=""
if [ "$GW_CODE" -ne 0 ]; then
  cat "$RUN_DIR/gateway.stderr" >> "$RUN_DIR/diagnostics.txt" 2>/dev/null || true
  fail "the gateway exited with code $GW_CODE"
fi

echo "== 7. verify the events (shared strict verifier)"
"$PYTHON" "$ROOT/scripts/verify_e2e_events.py" \
  --gateway "$RUN_DIR/gateway.jsonl" \
  --device "$RUN_DIR/device.jsonl" \
  --run-id "$RUN_ID" \
  --samples "$SAMPLES" \
  --device-id D001 --gateway-id GW001 \
  --expect-suite "$EXPECT_SUITE" --expect-kem "$EXPECT_KEM" \
  --out "$RUN_DIR/verify.json"
VERIFY_CODE=$?
if [ "$VERIFY_CODE" -ne 0 ]; then
  fail "the shared event verifier rejected this run (exit $VERIFY_CODE)"
fi

# Convenience copies under the M2 file names. They are written only on success
# and always from THIS run, so they cannot be a stale PASS.
cp "$RUN_DIR/gateway.jsonl" "$OUT_ROOT/../${EVENTS_PREFIX}_gateway.jsonl" 2>/dev/null || true
cp "$RUN_DIR/device.jsonl" "$OUT_ROOT/../${EVENTS_PREFIX}_device.jsonl" 2>/dev/null || true
cat "$RUN_DIR/gateway.jsonl" "$RUN_DIR/device.jsonl" > "$OUT_ROOT/../${EVENTS_PREFIX}_events.jsonl" 2>/dev/null || true
cp "$RUN_DIR/verify.json" "$OUT_ROOT/../${EVENTS_PREFIX}_summary.json" 2>/dev/null || true

STATUS="PASS"
echo
echo "PASS: real TCP handshake, telemetry, control and acknowledgement verified on both sides."
echo "      Event streams and the verifier result are in $RUN_DIR"
exit 0
