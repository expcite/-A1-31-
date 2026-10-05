# M5A-R1 里程碑报告：测量器定向修订

**项目**：A1-31 园区边缘设备国密与后量子混合安全通信软件设计实现
**本轮范围**：审查包 `EdgeHybrid_M5A_Audit_Next`（REVIEW.md B1–B4）的测量与验收可靠性修订
**本轮不做**：M4 重做、H1–H4 改动、密码组件重写、正式 1000 次采样、性能结论

**run_id**：`m5a_r1-20260928T112228Z`（父 run：`m5a-20260928T085056Z`）
**审查包**：`EdgeHybrid_M5A_Audit_Next`（AGENT_NEXT_M5A_R1.md、REVIEW.md、SOURCE_EVIDENCE.md、evidence/、regression/review_bench_state_test.go）
**源码清单**：139 文件，哈希 `f3919cd59d1e87eea7f207e24e9532127fb942ac1fa39397c88b3b9f8e34fe7d`
**验收等级**：`M5A_R1_MEASUREMENT_RELIABILITY_VERIFIED_PENDING`

> 历史缺口如实保留：**`-race` ENVIRONMENT_BLOCKED**、**SM2 密钥交换无独立实现对照**。

---

## B1：计数与统计

**审查确认的缺陷（已复现）**：`runTrial` 把 `appW, appR = hsW, hsR` 当成应用计数，而后续
连接继续累加 hsW/hsR —— 40/40 条 trial 的应用字节都是握手字节的**复制品**；
握手失败在赋值前返回，已写出的 H1 部分字节被丢掉；median 用上中位数却冒充常规定义；
握手分位数按 overall success 过滤，业务失败悄悄删掉了已完成的握手延迟。

**修复**（`internal/bench/bench.go`）：
- **一对累计计数器 + 阶段边界差值**：握手字节 = 握手结束时刻的累计值；
  应用字节 = 探测结束累计 − 握手边界累计。一个字节不会被计两次，
  应用阶段也不可能继承握手阶段的数字。
- **失败路径保存真实部分读写**：`HandshakeClientToServerBytes` 在 `Client` 返回前（无论成败）
  就落字段；`fail()` 不再清空它们。
- **单次与合计分列**：`handshake_c2s_bytes_first_trial` / `handshake_c2s_bytes_total` 等字段名
  明确区分。实测当前构建单次 T = 308/272 B、H = 1492/1360 B；10 次合计 3080/2720、14920/13600。
  **BENCHMARK_SPEC 增补已修正**为"单次 X、10 次合计 Y"，消除十倍误读。
- **median 冻结为常规定义**（偶数取两中间值均值），**P95 冻结为 nearest-rank
  （ceil(0.95·n)−1）**，`percentile_definition` 字段写进 summary。
- **分阶段统计**：握手分布按 `handshake_success`，探测分布按 `probe_success` ——
  探测失败的已完成握手仍留在握手分布里。
- **summary 从磁盘重算**：`SummariseFromDisk` 逐行解析 trials.jsonl 后聚合，网关确认数从
  事件文件独立计数；空集合分位数为 **null**（指针字段），不是 0。

**新增 Go 测试**（`bench_test.go`）：countingConn 部分写/读与阶段差值、失败保留部分字节、
奇偶 median、单/双样本、nearest-rank P95（n=1/2/20/100）、混合失败汇总、
`TestSummariseMedianIsConventional`。**审查回归 4/4 通过**（夹具按新签名适配，
断言数值未削弱）。

---

## B2：成功、失败与退出状态

**修复**：
- **阶段事实不互相覆盖**：`fail()` 只置后续阶段状态；`HandshakeSuccess` 在真实认证、
  套件、Finished 满足后设置一次，探测失败不会把它抹掉（审查回归
  `TestReviewProbeFailurePreservesHandshakeSuccess` ✅）。
- **ACK 发出 ≠ 网关确认**：新增 `EventSink`（同步、线程安全、网关启动前就绪、先到事件缓冲、
  原始行持久化到 `gateway_events.jsonl`）。探测在本地 `Send(Ack)` 之后**等待网关的
  `control-confirmed` 事件**，且必须匹配同一 run_id、设备、SID、operation_id；
  超时（同一试验预算内）→ `probe_confirm` 失败，`gateway_confirmation_timeout`。
  `ackFor` 的注释也已改为"不声称设备业务副作用已执行"。
- **同一预算**：`DialContext` 使用 trialCtx（不再固定 5s），握手、读写、确认等待共享
  同一个单次 deadline；`probe_confirm_wait_ns` 单独记录。
- **计数分立**：`requested_trials` / `started_trials` / `completed_trials` / `successes` /
  `failures` / `not_started` 分别记录；requested 不因中断缩小，中断不变成 10/10
  也不变成 0/0 PASS。
- **退出码文档化并强制**：0=PASS、2=配置/执行错误、3=试验失败、4=中断、5=结果验收失败。
  `Run` 对 FAIL/INCOMPLETE 返回非 nil error；CLI 相应非零退出。
- **终态必写、目录独占**：每个离开 `Run` 的路径都写 `status.json`（RUNNING → PASS/FAIL/
  INCOMPLETE）；输出目录**独占创建**（已存在即拒绝，绝不覆盖旧证据）；参数不合法在
  建目录前拒绝；运行中所有 goroutine/连接在返回前关闭清理。
- **临时密钥与证据分离**：provisioning 使用 `os.MkdirTemp` 的私有目录（证据目录之外），
  结束时 best-effort 删除；`gateway_events.jsonl` 只含公开元数据。

---

## B3：结果验收接入真实执行链路

**修复**：
- **正式验证器 `internal/bench.VerifyResults`**：从磁盘读取 manifest/trials/summary/status +
  网关事件 + 命令退出码，**独立重算**全部计数、字节合计与分位数并与 summary 逐项核对；
  校验 run_id/schema_version 一致、SID 唯一、成功必须有全部阶段事实、suite 必须等于
  manifest 请求值（不信 `suite_ok=true`）、负值拒绝、CPU/RSS 为 0 或无理由 null 拒绝、
  每个成功 trial 必须有对应网关确认事件、`PASS` 必须 started==requested、
  RUNNING 不算终态、FAIL/INCOMPLETE 不得退出 0。
- **cmd/bench 在每次运行后调用同一验证器**（ problems 非零 → 退出码 5 并写 validation.json）；
  `bench -verify -dir ...` 子命令暴露同一判据。
- **Python 自测重写**：`bench_result_selftest.py` 不再自带判据，改为构造 9 个夹具目录
  （正对照 + 审查列出的全部反例：RUNNING、汇总错误、负值、suite 不匹配、无支撑成功、
  重复 SID、删行缩 summary、缺网关确认）并调用 `bench -verify` —— **同一验收判据**。9/9 ✅。
- **关联性**：trial/事件/summary/status 均带 `schema_version` 与 `run_id`；manifest 带
  `parent_run_id`、`source_manifest_sha256`、`config_hash`、期望身份与套件、
  `timeout_per_trial_ns`（数值纳秒，不再是 "30s" 字符串）。
- **真实 Go 测试**：`internal/bench` 新增 18 个测试（验证器 7 变体 + 正对照 +
  计数器 + 分位数 + 混合失败）；cmd/bench 编译通过并实现退出码映射。
  全量流中 internal/bench 不再是 `[no test files]`。

---

## B4：收紧 Fleet 发送证据

**复现**（审查 evidence/）：删除全部 180 条 control-sent 后原验收器仍 exit 0（controls_sent=null）。

**修复**（`scripts/verify_fleet_events.py`）：
- **当前 schema 默认严格**：存在确认控制却没有任何 control-sent 事件 → **FAIL**
  （"every confirmed control needs an independent control-sent event"）。
- **多重集一一对应**：sent 与 confirmed 按
  `(run_id, device, SID, sample_id, operation_id)` 做 Counter 比较，
  重复确认掩盖缺失发送、重复发送无确认都被拒绝。
- **legacy 显式降级**：`--legacy` 时历史流输出 `result_kind=legacy_incomplete`、
  `controls_sent=null`、`result=FAIL`（exit 1）—— 可以复查，但**永远不会通过当前验收门**。

**自测**：`fleet_verifier_selftest.py` 扩至 **23/23**（含删全部发送事件、删一条、
重复确认替代、legacy 模式仍不通过）。**正常对照**：真实 20×3 证据（含 control-sent 事件）
用新验收器重跑仍 `PASS — 60 unique session ids` ✅。

---

## 回归与证据

| 项 | 结果 |
|---|---|
| `go mod verify` / `go vet ./...` / `go build ./cmd/...` | 全部 exit 0 |
| **全量 `go test -count=1 -json ./...`** | **263 顶层 / 199 子测试 / 0 失败**，16 包（11 ok），真实退出码 0 |
| Suite 1 / Suite 2 E2E | PASS / PASS |
| 20×3 fleet | PASS — 60 会话 / 60 唯一 SID / 180 遥测 / 180 控制 |
| 19 good + 1 bad | PASS — 19 台、57 会话，D010 失败 |
| 四类自测 | 17 / 45 / 23 / 9 全过（bench 自测改为调用生产验证器） |
| T smoke | 10 请求 / 10 开始 / 10 完成 / 10 成功 / 0 失败 / 0 未开始，PASS，验收通过 |
| H smoke | 10 请求 / 10 开始 / 10 完成 / 10 成功 / 0 失败 / 0 未开始，PASS，验收通过 |
| race | **ENVIRONMENT_BLOCKED，未执行** |
| SM2 交换独立验证 | **不可得，pending** |

**smoke 实测数字**（`run_kind=smoke`，`formal_results_eligible=false`，含网关确认事件）：

| 模式 | 握手 median | P95 (nearest-rank) | 单次 wire C→S / S→C | 网关确认 |
|---|---|---|---|---|
| T | 1.11 ms | 1.75 ms | 308 B / 272 B | 10/10 |
| H | 1.64 ms | 2.18 ms | 1 492 B / 1 360 B | 10/10 |

（单机单进程小规模，不构成正式性能结论。）

**审查回归**：`regression/review_bench_state_test.go` 放入 `internal/bench` 用真实依赖运行，
修复前 2 个失败（probe 失败抹掉握手成功、偶数 median），修复后 4/4 通过；
夹具因 `summarise` 签名变化做了适配（传入 Manifest），断言数值未削弱。
审查包 `isolated_noncrypto` 未复制进项目。

---

## 声明

- 本轮未执行正式 1000 次采样，未生成最终性能排名；smoke 结果不得作为竞赛性能数据引用；
- `cmd/bench` 仅开放 smoke；formal 显式拒绝（exit 2）；
- CPU/RSS 单次口径继续 null + 原因（正式方案见 docs/BENCHMARK_PLAN.md）；
- 修复不构成安全审计或合规认证，也不意味着不存在其他缺陷。

**完成 M5A-R1，停止，正式 1000 次保持锁定。**
