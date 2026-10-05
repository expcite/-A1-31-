# 当前需求→源码→测试→原始证据

当前版本interactive-v1.2-backend2-status1-releasefix。活动FINAL_DELIVERY_INDEX.json的所有证据路径以审查包根为准，与包根RELEASE_INDEX.json相同。source_root是source/edgehybrid-agent-starter；历史源historical_sources用于采样与M6复核，不能作为当前程序源。

|需求|当前源码/测试（项目根相对）|包根历史证据与当前核对|
|---|---|---|
|密钥生命周期|internal/identity、internal/management、cmd/secctl、cmd/gateway及对应测试|source/edgehybrid-agent-starter/results/raw/backend_friend_20261002；本轮普通回归|
|双向认证/混合两份秘密|internal/cryptosuite、internal/handshake及tamper/kem/keyschedule测试|原M5与backend1共32批，采样source hash各自保持；当前结果验收器只读复核|
|记录/重放/新会话|internal/record、internal/app、internal/live；对应Channel/queue测试|backend_friend原保护消息与截断/会话回归；本轮Friend/Recheck|
|传统/混合迁移|internal/policy、internal/handshake；downgrade/no_fallback|historical_sources/M6_resolved/results/raw/m6_20261001/comparison_v2|
|20设备和性能|cmd/fleet、bench、pbench、internal/metrics|source/edgehybrid-agent-starter/results/raw/m5_formal_20261001/data、results/raw/backend_20261002/formal；每个批次在活动索引有包根完整路径|
|模型/来源/许可证|docs/REQUIREMENTS.md、DEPENDENCIES.md、THIRD_PARTY_NOTICES.md|历史source清单、当前source/EXE/build_info以及来源索引|
|可审计创新|internal/handshake/m6_structured_test.go、scripts/m6_compare.py|M6原AB流、public观测、隔离变体、源归档均在historical_sources/M6_resolved；A/B覆盖与检出相同|
|F07|internal/console/assets/logs.js、scripts/logs_refresh_test.mjs|source/edgehybrid-agent-starter/results/raw/release_five_20261003修前4/7、修后同例及真实浏览器/API导出；测试结论以原日志为准|

旧映射原字节见history/REQUIREMENT_EVIDENCE_MAP_before_releasefix.md。活动索引目录具有逐文件manifest，必需文件丢失/错hash/依赖缺失会阻止验收。历史race/独立SM2核心14/OpenSSL已执行但非全认证；S1/S2未执行。
