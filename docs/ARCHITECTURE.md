# 架构与模块职责（M5B 拆分后）

## 依赖方向
cmd/* → internal/* → 标准库/已固定依赖。internal 包之间：bench/pbench 依赖 app（共用
HandleControl 与运行时装配）与 metrics；app 不依赖 bench/pbench；record/policy/wire/cryptosuite
不依赖 app。cmd 只做参数、配置、装配与退出码，不实现握手、密码算法、业务或统计验收。

## internal/app（生产协议栈）
| 文件 | 职责 |
|---|---|
| events.go | Event 结构、Logger、boolPtr、businessStats、fullSession/shortSession |
| gateway.go | Gateway：Run 主循环、serve、handshakeConn、admitSession（会话准入）、watchLifecycle、track/closeActive |
| gateway_business.go | 网关业务循环：遥测接受、控制下发、control-confirmed 事件 |
| device.go | Device：Run、runSession、设备业务循环（含 rekey 计数） |
| sessions.go | 会话注册表、Admit 名额、SweepRevoked（未改动） |
| rekey.go / wiring.go / messages.go | 换钥策略、运行时装配、Control/Ack/HandleControl（未改动） |

Device 与 bench 共用 messages.go 的 HandleControl（单一业务判定来源）。

## internal/bench（测量与验收）
| 文件 | 职责 |
|---|---|
| types.go | RunKind/Mode/Status、Options、Trial、Summary、Manifest |
| run.go | Run 编排（provision→运行→汇总→终态）、finalize、provision、countingConn |
| trial.go | runTrialCtx 单次试验、ackFor、fail、classifyHandshakeError |
| events.go | EventSink（同步收集+持久化+Err）、eventStr |
| stats.go | median（常规定义）、p95（nearest-rank）、mean、statsOf、summarise、SummariseFromDisk |
| persist.go | writeJSON/writeStatus/configHash |
| judge.go | VerifyResults/JudgeResults（验收，唯一一套规则）、EvidenceStatus、ResultJudgement、objectAsMap |
| schema.go | m5a 契约表（manifest/trial/summary/status 字段、类型、枚举） |
| m5b_common.go | M5B 共享：Schema 常量、WorkerOptions、JSON 写辅助、finalizeM5B |
| m5b_resource.go | 资源采集（分进程网关/设备自报 + 握手窗口快照差）与验收 |
| m5b_throughput.go | 吞吐（3×200 真实事务）与验收 |

## internal/pbench、internal/metrics、cmd
- pbench：P 对照（证书生成、TLS1.3+X25519MLKEM768 握手/吞吐/拒绝测试、ValidatePResults）。
- metrics：Windows 进程 CPU（GetProcessTimes）与内存（GetProcessMemoryInfo）；其他平台 null+原因。
- cmd/bench：smoke/worker/资源/吞吐调度入口 + verify 子命令；cmd/gateway：生产网关（-metrics 自报）；
  cmd/pbench：P 入口；cmd/secctl：身份与注册表管理。

## 新增代码放哪里
- 握手/协议/业务语义 → internal/app 对应文件；测量与验收 → internal/bench 对应文件；
  P 相关 → internal/pbench；OS 资源 → internal/metrics；入口只做装配。

## 旧文件/函数 → 新文件/函数 映射（M5B 拆分）
| 旧位置 | 新位置 |
|---|---|
| bench.go: types/Options/Trial/Summary/Manifest | bench/types.go |
| bench.go: EventSink/waitFor、eventStr | bench/events.go |
| bench.go: Run/finalize/provision/countingConn | bench/run.go |
| bench.go: runTrialCtx/ackFor/fail/classifyHandshakeError | bench/trial.go |
| bench.go: median/p95/mean/statsOf/summarise/SummariseFromDisk/countGatewayConfirmations | bench/stats.go |
| bench.go: writeJSON/writeStatus/configHash | bench/persist.go |
| bench.go: VerifyResults/JudgeResults/objectAsMap/suiteForMode/validSessionID/deref/shortSID | bench/judge.go |
| m5b.go: Schema 常量/WorkerOptions/写辅助/finalizeM5B | bench/m5b_common.go |
| m5b.go: ResourceRow/ResourceSummary/RunDeviceResourceWorker/runResourceTrial/RunResourceSmoke/ValidateResourceResults | bench/m5b_resource.go |
| m5b.go: Throughput*/runThroughputRound/RunThroughputSmoke/acceptedPerRoundFromEvents/ValidateThroughputResults | bench/m5b_throughput.go |
| service.go: Event/Logger/businessStats/fullSession/shortSession | app/events.go |
| service.go: Gateway/Run/serve/handshakeConn/admitSession/watchLifecycle | app/gateway.go |
| service.go: (g *Gateway) business | app/gateway_business.go |
| service.go: Device/deviceSession/Run/runSession/business | app/device.go |

`JudgeResults` 拆分说明：验收仍是唯一一套规则（schema.go 表 + judge.go 流程），按
manifest→trials→summary→status→身份/SID→事件→重算→状态/退出码 的段落顺序执行，
未复制第二套校验规则。

## M5B 本轮校正增量

- internal/evidence：所有新结果共用严格 JSON、字段存在性、durable write、manifest/status、精确业务关联、确认观察和吞吐重算；不含密码实现。
- internal/metrics：ProcessRow 与 CPUSnapshot 共用角色/原始采样契约、单位和回退检查。
- internal/bench/m5b_process.go：受限子进程、正常会话完成停止、异常 cleanup 与真实 Wait；m5b_resource_validate.go 完整资源验收。
- internal/pbench/server.go：有界 JSON 行、整连接预算、身份/业务/ACK/EOF 检查；measurement.go 负责真实试验与 join；validate.go/rejections.go 分担验收及拒绝实验。
- cmd/pbench 仅参数、信号、装配及退出码；cmd/bench -verify 分派对应完整验收器。
- scripts/windows_integration.py 是 Windows 原生联调入口，复用既有 Python 验证器；run_checked.py 保存实际命令、平台、退出码和原始流。

H1–H4、KDF、密码组件与 record 序号逻辑本轮未修改。app 只提取既有遥测校验、追加公共长度事实及传播事件写入错误。
