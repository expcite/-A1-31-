# Revalidation required after the M5A-R2 review patch

Run these commands on the original supported Go >=1.25 environment before proceeding to M5B/formal sampling.

```bash
go version
go mod verify
go vet ./...
go build ./cmd/...

go test -count=1 -json ./... > results/raw/m5a_r2_patch_tests.jsonl
printf '%s\n' "$?" > results/raw/m5a_r2_patch_tests.exit
```

Then rerun the existing evidence/self-test suites and actual system regressions:

```bash
python scripts/summarize_selftest.py
python scripts/m4case_extractor_selftest.py
python scripts/fleet_verifier_selftest.py
python scripts/bench_result_selftest.py

MODE=traditional bash scripts/m2-e2e.sh
MODE=hybrid bash scripts/m2-e2e.sh
DEVICES=20 SESSIONS=3 REKEY_RECORDS=4 bash scripts/m4-fleet-e2e.sh
DEVICES=20 BAD_DEVICE=D010 bash scripts/m4-fleet-e2e.sh
```

Finally run **new** traditional and hybrid smoke measurements with the patched source and verify them with `cmd/bench -verify`. Do not reuse the pre-patch smoke numbers as evidence for the patched tree.

Acceptance before M5B:

- all full-project tests pass on the patched source;
- the new `assistant_closure_test.go` regressions pass;
- Suite 1 and Suite 2 E2E pass;
- 20x3 and 19+1 fleet regressions pass;
- new T/H smoke result directories pass the same formal validator;
- no result file contains duplicate keys, trailing data, mode/suite mismatch, malformed SID or orphan confirmation;
- no evidence persistence error is ignored;
- `-race` remains `ENVIRONMENT_BLOCKED` unless actually executed successfully;
- SM2 key-agreement independent cross-check remains pending unless actually performed.
