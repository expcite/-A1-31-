# M5A-R2 review patch notes

Status: **PATCHED / FULL GO>=1.25 REVALIDATION REQUIRED**

This patch was applied to the submitted `EdgeHybrid_M5A_R2_Review_Package.zip` after source review. It deliberately does **not** change H1-H4, the cryptographic key schedule, lifecycle rules, or the Suite 1/Suite 2 protocol implementation.

## Definite defects fixed

1. **Evidence producer shutdown order**
   - The submitted tree closed `gateway_events.jsonl` before cancelling and awaiting the gateway.
   - The patched `Run` stops/awaits the gateway first, then checks the event sink, syncs and closes the event file.

2. **Persistence failures were ignored**
   - Initial/final manifest writes, per-row trial sync, summary writes and several file closes used ignored errors.
   - These errors now propagate as non-zero run failures.
   - `EventSink` records the first persistence error so `app.Logger`'s best-effort logging cannot hide a failed benchmark evidence write.
   - `cmd/bench` now treats failure to write `validation.json` as validation failure.

3. **Strict JSON validation gaps**
   - Duplicate keys in `summary.json` / `status.json` could be detected by the parser but the error was discarded.
   - Event/result objects could contain trailing data after a valid JSON object.
   - `objectAsMap` now accepts exactly one top-level JSON object, rejects duplicate keys and rejects trailing data; parser errors are propagated for all artifacts.

4. **Mode / suite binding gap**
   - A result could label itself `traditional` while consistently carrying Suite 2.
   - The validator now binds `traditional -> Suite 1` and `hybrid -> Suite 2` in both the manifest and successful trials.

5. **Session-id syntax**
   - Validation only rejected IDs shorter than 32 characters.
   - Session IDs now have to be exactly 32 hexadecimal characters.

6. **Orphan/extra confirmation events**
   - The validator tracked matched confirmation events but did not reject unmatched `control-confirmed` records.
   - Every same-run confirmation must now belong to exactly one successful trial, and `gateway_confirmed_count` must equal the number of successful trials.

7. **Result contract completeness**
   - Final manifest/status/summary schemas now require key terminal-evidence fields such as timestamps, config hash, metric-support metadata and the smoke ineligibility reason.
   - A null CPU/RSS measurement must carry the corresponding unavailable reason.
   - `success` is cross-checked against the recorded phase facts.

8. **Stale project status documentation**
   - README / IMPLEMENTATION_STATUS / VERSION were still describing the current tree as M4.2 and claimed Suite 2/bench were unavailable.
   - They now describe the M5A-R2 smoke-measurement state and keep the formal 1000-trial mode locked.

## Added regressions

`internal/bench/assistant_closure_test.go` adds tests for:

- event-sink persistence error recording;
- duplicate summary keys;
- trailing garbage in gateway event lines;
- public mode to protocol-suite binding;
- exact hexadecimal SID syntax;
- unmatched gateway confirmations.

Existing fixture helpers were updated to include the terminal fields now required by the result contract.

## Verification performed in this review environment

Performed:

- outer submitted-package manifest verification before editing: all submitted files matched;
- `gofmt` on all modified Go files;
- Python summarizer self-test: 17/17 passed;
- M4CASE extractor self-test: 45/45 passed;
- fleet verifier self-test: 23/23 passed;
- `bash -n scripts/m2-e2e.sh`: passed;
- `bash -n scripts/m4-fleet-e2e.sh`: passed.

Not performed:

- full Go build/test/vet of the patched tree;
- Suite 1 / Suite 2 E2E on the patched tree;
- fresh smoke runs using the patched tree;
- race detector;
- independent SM2 key-agreement cross-check.

Reason: this review environment has Go 1.23.2 while the project requires Go >=1.25.0, and network access is unavailable for automatic toolchain download. `GOTOOLCHAIN=local go test ./...` stops at the Go-version check.

Therefore this package must **not** be represented as a new fully verified milestone. On the original development machine, run the full validation sequence documented in `PATCH_REVALIDATION.md` before using these changes as the formal M5 baseline.
