#!/usr/bin/env bash
#
# M4 multi-device integration harness: one gateway plus K real Suite 2 devices.
#
# The device count is a PARAMETER (-devices / DEVICES), never a constant. The
# same cmd/fleet binary runs 1 device or 20 or K, each with its own trusted
# identity, its own keys and its own session state.
#
# Every device performs a real Suite 2 session: SM2 mutual authentication, a full
# SM2 key agreement, a real ML-KEM-768 exchange, HKDF-SM3 and SM4-GCM records.
#
# Usage:  scripts/m4-fleet-e2e.sh [OUTPUT_ROOT]
# Env:    GO=/path/to/go
#         PYTHON=python3
#         DEVICES=20        number of devices
#         SAMPLES=3         telemetry samples per device
#         SESSIONS=1        successive sessions per device (>1 exercises rekey)
#         REKEY_RECORDS=0   rekey after N records per session. The counter counts
#                           every ProtectedRecord the device writes or reads, so one
#                           complete telemetry->control->ACK transaction is THREE
#                           records. To let every device send all SAMPLES and still
#                           rekey, set this to SAMPLES*3 (e.g. 9 for 3 samples); a
#                           smaller value makes each session stop early at a
#                           transaction boundary, which the verifier then reports.
#         REKEY_AGE=0       rekey after this session age
#         BAD_DEVICE=       a device id to run with an untrusted identity
#
# This is an integration check. The duration it reports is wall time, NOT a
# benchmark number.

set -uo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
OUT_ROOT="${1:-$ROOT/results/raw/e2e}"
GO="${GO:-go}"
PYTHON="${PYTHON:-python3}"
DEVICES="${DEVICES:-20}"
SAMPLES="${SAMPLES:-3}"
SESSIONS="${SESSIONS:-1}"
REKEY_RECORDS="${REKEY_RECORDS:-0}"
REKEY_AGE="${REKEY_AGE:-0}"
BAD_DEVICE="${BAD_DEVICE:-}"

RUN_ID="m4-$(date -u +%Y%m%dT%H%M%SZ)-$$"
WORK="$(mktemp -d)"
BIN="$WORK/bin"
RUNTIME="$WORK/runtime"
RUN_DIR="$OUT_ROOT/$RUN_ID"

mkdir -p "$BIN" "$RUNTIME" "$RUN_DIR"

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
FLEET_CODE="not-run"
VERIFY_CODE="not-run"

finish() {
  local code=$?
  if [ -n "$GW_PID" ] && kill -0 "$GW_PID" 2>/dev/null; then
    kill "$GW_PID" 2>/dev/null || true
    wait "$GW_PID" 2>/dev/null || true
    GW_CODE="killed"
  fi
  {
    echo "status=$STATUS"
    echo "run_id=$RUN_ID"
    echo "devices=$DEVICES"
    echo "samples=$SAMPLES"
    echo "sessions=$SESSIONS"
    echo "gateway_exit_code=$GW_CODE"
    echo "fleet_exit_code=$FLEET_CODE"
    echo "verifier_exit_code=$VERIFY_CODE"
    echo "script_exit_code=$code"
  } > "$RUN_DIR/status" 2>/dev/null || true
  echo "$STATUS" > "$RUN_DIR/result" 2>/dev/null || true
  rm -rf "$WORK" 2>/dev/null || true
  exit $code
}
trap finish EXIT
trap 'STATUS="INTERRUPTED"; exit 130' INT
trap 'STATUS="INTERRUPTED"; exit 143' TERM

echo "RUNNING" > "$RUN_DIR/result"
{ echo "status=RUNNING"; echo "run_id=$RUN_ID"; echo "devices=$DEVICES"; } > "$RUN_DIR/status"

fail() {
  STATUS="FAIL"
  echo "FAIL: $*" >&2
  echo "FAIL: $*" >> "$RUN_DIR/diagnostics.txt"
  exit 1
}

echo "== run_id : $RUN_ID"
echo "== devices: $DEVICES (samples=$SAMPLES sessions=$SESSIONS)"
echo "== output : $RUN_DIR"

echo "== 1. build"
(cd "$ROOT" && "$GO" build -o "$BIN/" ./cmd/...) || fail "go build failed"

echo "== 2. provision $DEVICES device identities"
"$BIN/secctl" init-demo -dir "$RUNTIME" -gateway GW001 \
  -devices "$DEVICES" -id-pattern 'D%03d' > "$RUN_DIR/init.json" 2>"$RUN_DIR/init.err" \
  || fail "init-demo failed: $(cat "$RUN_DIR/init.err")"

echo "== 3. write the gateway configuration"
cat > "$WORK/gateway.json" <<EOF
{
  "role": "gateway",
  "id": "GW001",
  "address": "127.0.0.1:0",
  "minimum_mode": "hybrid",
  "allow_legacy": false,
  "supported_suites": [2],
  "handshake_timeout_ms": 15000,
  "max_frame_bytes": 16384,
  "trust_store": "$RUNTIME_NATIVE/trust.json",
  "key_directory": "$RUNTIME_NATIVE/keys"
}
EOF

echo "== 4. start the gateway"
"$BIN/gateway" -config "$WORK/gateway.json" -run-for 180s \
  -run-id "$RUN_ID" -events "$RUN_DIR_NATIVE/gateway.jsonl" \
  > "$RUN_DIR/gateway.stdout" 2> "$RUN_DIR/gateway.stderr" &
GW_PID=$!

# Readiness comes from the gateway's OWN event, never from a fixed sleep.
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

echo "== 5. write the base device configuration"
cat > "$WORK/device.json" <<EOF
{
  "role": "device",
  "id": "D001",
  "address": "$ADDR",
  "minimum_mode": "hybrid",
  "allow_legacy": false,
  "supported_suites": [2],
  "handshake_timeout_ms": 15000,
  "max_frame_bytes": 16384,
  "trust_store": "$RUNTIME_NATIVE/trust.json",
  "key_directory": "$RUNTIME_NATIVE/keys",
  "expected_peer_id": "GW001"
}
EOF

echo "== 6. run $DEVICES devices concurrently"
"$BIN/fleet" -config "$WORK/device.json" -devices "$DEVICES" \
  -first 1 -id-pattern 'D%03d' -gateway GW001 \
  -samples "$SAMPLES" -sessions "$SESSIONS" \
  -rekey-records "$REKEY_RECORDS" -rekey-age "$REKEY_AGE" \
  -bad-device "$BAD_DEVICE" \
  -run-id "$RUN_ID" -events "$RUN_DIR_NATIVE/device.jsonl" \
  -summary "$RUN_DIR_NATIVE/fleet_summary.json" \
  > "$RUN_DIR/fleet.stdout" 2> "$RUN_DIR/fleet.stderr"
FLEET_CODE=$?

echo "== 7. verify (shared strict verifier)"
"$PYTHON" "$ROOT/scripts/verify_fleet_events.py" \
  --gateway "$RUN_DIR/gateway.jsonl" \
  --summary "$RUN_DIR/fleet_summary.json" \
  --run-id "$RUN_ID" --expect-devices "$DEVICES" \
  --expect-suite 2 --expect-samples "$SAMPLES" --expect-sessions "$SESSIONS" \
  --expect-bad-device "$BAD_DEVICE" \
  --out "$RUN_DIR/verify.json"
VERIFY_CODE=$?

# Convenience copies, written for EVERY run so a failure cannot leave a stale
# PASS behind. They always describe THIS run.
cp "$RUN_DIR/gateway.jsonl" "$OUT_ROOT/../m4_20device_gateway.jsonl" 2>/dev/null || true
cp "$RUN_DIR/device.jsonl" "$OUT_ROOT/../m4_20device_device.jsonl" 2>/dev/null || true
cp "$RUN_DIR/fleet_summary.json" "$OUT_ROOT/../m4_20device_fleet_summary.json" 2>/dev/null || true
cp "$RUN_DIR/verify.json" "$OUT_ROOT/../m4_20device_summary.json" 2>/dev/null || true
cat "$RUN_DIR/gateway.jsonl" "$RUN_DIR/device.jsonl" > "$OUT_ROOT/../m4_20device_events.jsonl" 2>/dev/null || true

if [ "$VERIFY_CODE" -ne 0 ]; then
  cat "$RUN_DIR/fleet.stderr" >> "$RUN_DIR/diagnostics.txt" 2>/dev/null || true
  fail "the shared verifier rejected this run (exit $VERIFY_CODE)"
fi
if [ -n "$BAD_DEVICE" ] && [ "$FLEET_CODE" -eq 0 ]; then
  fail "the fleet reported success although $BAD_DEVICE was supposed to fail"
fi
if [ -z "$BAD_DEVICE" ] && [ "$FLEET_CODE" -ne 0 ]; then
  fail "the fleet failed with exit code $FLEET_CODE"
fi

STATUS="PASS"
echo
echo "PASS: $DEVICES devices completed real Suite 2 sessions against one gateway."
echo "      Events, fleet summary and the verifier result are in $RUN_DIR"
exit 0
