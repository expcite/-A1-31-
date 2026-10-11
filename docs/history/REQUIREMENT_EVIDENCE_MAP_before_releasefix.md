# 需求到源码 测试 原始证据对应表

完整路径、hash和限制见 FINAL_DELIVERY_INDEX.json。表内目录项是来源集合，不能将目录hash当文件hash。

|要求|源码|测试|原始证据|
|---|---|---|---|
|1 密钥生命周期|internal/identity, cmd/secctl, internal/app/sessions.go|internal/identity/identity_test.go, internal/app|results/raw/m4_verification.txt, results/raw/m5_formal_20261001/freeze/commands/go_test_json/stdout.txt|
|2 双向认证与两份秘密KDF|internal/cryptosuite, internal/handshake/keyschedule.go, internal/handshake/client.go, internal/handshake/server.go|internal/handshake/keyschedule_hybrid_test.go, internal/handshake/kem_implicit_test.go, internal/handshake/tamper_test.go|results/raw/m5_formal_20261001/data/results_index.json, results/raw/m6_20261001/comparison_v2/commands/normal_B_1/observations.json|
|3 SM4记录与重放/重连|internal/record, internal/app|internal/handshake/wire_replay_test.go, internal/handshake/channel_test.go|results/raw/m6_20261001/comparison_v2/commands/normal_A_1/stdout.jsonl, results/raw/m6_20261001/comparison_v2/commands/normal_B_1/stdout.jsonl|
|4 传统/混合迁移|internal/policy, internal/handshake|internal/handshake/downgrade_test.go, internal/handshake/no_fallback_test.go|results/raw/m6_20261001/comparison_v2/commands/normal_A_1/observations.json|
|5 20设备与正式性能|cmd/fleet, internal/bench, internal/pbench, internal/metrics|internal/bench, internal/pbench|results/raw/m5_formal_20261001/data/results_index.json, results/raw/m5_formal_20261001/report_verified/recomputed_report.json, results/raw/demo_windows_20260930|
|6 模型/来源/许可|docs/FINAL_DESIGN_REPORT.md, docs/DEPENDENCIES.md, THIRD_PARTY_NOTICES.md|results/raw/m6_20261001/regression/go_test_json/stdout.txt|results/raw/m6_20261001/freeze/audit/REVIEW.md, results/raw/m6_20261001/regression/m6_verified.json|
|innovation 可审计结构化篡改测试|internal/handshake/m6_structured_test.go, scripts/m6_compare.py|TestM6Structured, 隔离D1/D2/D3|results/raw/m6_20261001/comparison_v2/comparison.json, results/raw/m6_20261001/comparison_v2/measurement_source_manifest.sha256|


## interactive-v1.2 需求增量（不回填旧M5/M6）

|当前要求|源码|测试|新证据根 results/raw/interactive_v12_20261002/|
|---|---|---|---|
|网关生成/配置/使用|identity/init.go、app/wiring.go；live/manager.go启动预检|TestGatewayRotationRealTCPGenerations、新世代正例|gateway_events、all_go_final、gateway_installed_one/twenty|
|网关维护/轮换/吊销/退役/清理|management/gateway.go、live/gateway_lifecycle.go、console/assets/keys.js|TestGatewayRevocationRetirementPersistsOnRestart、TestGatewayMaintenanceFailureSafety、TestGatewaySecondConfirmationAPI|all_go_final、gateway_installed_one/twenty；失败和pending保留|
|旧请求/队列隔离|live/request_scope.go、device.go、commands.go；dispatch.go网关快照|既有session_queue_test.go保持；新旧网关请求回归|all_go_final、gateway_events|
|新手温度/周期真实主流程|assets/intro.js、workbench.js、usability.js；live共享业务|TestInteractiveSingleClosure、TestActualSamplingIntervals、Node及真实Browser|installed_one、browser_final、screenshots|
|全20及首中末异常|live/manager.go既有逐设备准入|TestStartAllAttemptsHealthyRealTCP、actual新exe|installed_twenty、installed_bad_D001/D010/D020|
|有界容量/授权/退出/导出|console/schema/auth/handlers_live/handlers_export、exports；assets/logs|TestAuthorizationExpiryFailsClosed、TestCapacityErrorsDistinguishLedgerAndQueue、TestFilteredExportMatchesFrozenEventRange、TestExitRequiresOwnerAndCurrentRunConfirmation|all_go_final、frontend_final、installed_one、gateway_installed_one/twenty、browser_final|

旧表是历史M6材料来源，原正式性能只证明冻结原测量版本。新增版本不获得新正式性能、race或独立SM2验证结论。新回归各run精确源码和binary关联见build_final与VERIFICATION_REPORT。
