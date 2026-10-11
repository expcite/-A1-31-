# M5B 本轮源码改动

基线：EdgeHybrid_M5B_Baseline_20260930T163316/source_before.zip。完整统一 diff 见 M5B_CORRECTED.diff；每文件前后 SHA-256 见 raw/m5b_corrected_20260930/source_changes.json。备份中生成的 BASELINE_MANIFEST.sha256 不作为源文件删除项。

AGENTS.md 原字节保持；既有 handshake/cryptosuite（含 KDF）/record/policy 的 50 个原文件原字节保持。旧结果保持，新增报告仅追加说明。

- added: CODEX_HANDOFF_M5B.md
- modified: IMPLEMENTATION_STATUS.json
- modified: PACKAGE_INFO.md
- modified: README.md
- modified: cmd/bench/main.go
- modified: cmd/gateway/main.go
- modified: cmd/pbench/main.go
- added: cmd/pbench/main_test.go
- modified: docs/ARCHITECTURE.md
- added: docs/M5B_CORRECTED_CONTRACT.md
- modified: docs/MEASUREMENT_CONTRACT_M5B.md
- modified: internal/app/events.go
- modified: internal/app/gateway_business.go
- added: internal/app/logger_error_test.go
- modified: internal/app/messages.go
- modified: internal/bench/bench_r2_test.go
- modified: internal/bench/judge.go
- added: internal/bench/m5b_boundary_test.go
- modified: internal/bench/m5b_common.go
- added: internal/bench/m5b_controller_test.go
- added: internal/bench/m5b_process.go
- modified: internal/bench/m5b_resource.go
- added: internal/bench/m5b_resource_validate.go
- modified: internal/bench/m5b_throughput.go
- modified: internal/bench/m5b_validate_test.go
- modified: internal/bench/trial.go
- added: internal/evidence/business.go
- added: internal/evidence/json.go
- added: internal/evidence/json_test.go
- added: internal/evidence/run.go
- added: internal/evidence/throughput.go
- added: internal/evidence/throughput_types.go
- added: internal/evidence/tls.go
- added: internal/metrics/conversion_windows_test.go
- modified: internal/metrics/metrics_windows.go
- added: internal/metrics/row.go
- added: internal/metrics/snapshot.go
- added: internal/metrics/snapshot_test.go
- added: internal/pbench/measurement.go
- modified: internal/pbench/pbench.go
- added: internal/pbench/rejections.go
- added: internal/pbench/server.go
- added: internal/pbench/server_test.go
- added: internal/pbench/validate.go
- added: scripts/m5b_corrected_smoke.py
- added: scripts/package_review.py
- added: scripts/run_checked.py
- modified: scripts/source_manifest.py
- added: scripts/windows_integration.py
