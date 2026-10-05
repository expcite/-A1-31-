# M5A-R2 里程碑报告：测量结果校验收口

**项目**：A1-31 园区边缘设备国密与后量子混合安全通信软件设计实现
**本轮范围**：审查包 `EdgeHybrid_M5A_R1_Audit_Next` 的三个验收门（R2-1 结果契约、R2-2 真实业务探测、R2-3 终态与退出码）
**本轮不做**：M4 重做、H1–H4 改动、密码组件重写、正式 1000 次采样、性能结论

**实际读取的审查目录**：`C:\Users\20477\WorkBuddy\密码技术竞赛\EdgeHybrid_M5A_R1_Audit_Next`
（`AGENT_NEXT_M5A_R2.md`、`REVIEW.md`、`SOURCE_EVIDENCE.md`、`PUBLIC_REFERENCES.md`、`evidence/`、`regression/` 两个 Go 模板、`isolated_noncrypto/`）。`isolated_noncrypto` 未复制进工程。

**run_id**：`m5a_r2-20260929T003740Z`（父 run：`m5a_r1-20260928T112228Z`）
**源码清单**：144 文件，哈希 `bcc1c8b83476cb93af964c0a3c35d16bce9b1b45ecefe57bda18307acfdbcb64`
**验收等级**：`M5A_R2_ACCEPTANCE_CLOSURE_VERIFIED_PENDING`

> 历史缺口如实保留：**`-race` ENVIRONMENT_BLOCKED**、**SM2 密钥交换无独立实现对照**。

---

## R2-1：一次定义完整的结果契约

**审查复现的 12 类漏洞**（对真实 H 结果副本逐项修改）：失败仍 PASS/退出 0、重复 trial_id、0 请求空运行 PASS、未知 status 被接受、`suites=[]` panic、短 SID panic、删字段后变零值、错身份被接受、事件坏行被跳过、负资源值被接受、`success_pct=999`与 probe 分位数错误被接受、验证器不读真实退出码。

**修复**：新增 `internal/bench/schema.go` —— **契约以表驱动定义一次**，验证器消费这些表，不再逐条补 if：
- `manifestFields` / `trialFields` / `summaryFields` / `statusFields` 四个必填键表，含类型（string/int/uint/bool/null-or-int/array/float）、最小值范围、允许空字符串的字段白名单。
- 枚举：`run_kind`（仅 smoke，formal 故意不在白名单内）、`mode`、`status`（仅 PASS/FAIL/INCOMPLETE）、`success_pct_denominator`。
- `objectAsMap` **拒绝重复键**（不再让后者静默覆盖前者）。
- 所有下标访问（`suites[0]`、`sid[:8]`）前均有结构校验 —— 空 suites、短 SID 现在返回结构化问题，**不 panic**（有专门测试）。

**状态与统计分离**：新增 `JudgeResults` 返回 `ResultJudgement`，含
`run_status`（PASS/FAIL/INCOMPLETE）与 `evidence_status`（VALID/INVALID）两个独立判定，
外加 `acceptance_passed` 与 `expected_exit_code`。
**完整记录的真实失败 = VALID 证据 + FAIL 运行**，绝不改写成成功（有专门测试）。
PASS 一致性要求：请求为正、请求/开始/完成一致、失败=0、not_started=0、每条成功都有阶段与远端确认证据；FAIL/INCOMPLETE 各有自身一致性要求，不只是核退出码。

**全字段重算**：`success_pct`（浮点，1e-9 容差）、probe mean/median/P95、握手 mean/median/P95、
单次与合计字节、样本量、网关确认数、模式/schema/run_kind 全部逐项核对。
身份用 **`authenticated_peer_id` 与 manifest 期望网关比对**，不信任 `peer_id_ok` 布尔。
网关事件坏行被**诊断**而不是跳过；确认必须同 run/device/SID/operation 且不得重复匹配虚增成功。
新增系统性缺键/错类型测试（8 个键逐个删除）、重复键、畸形结构、身份不符、事件坏行、全字段重算（9 个字段逐个改错）。

## R2-2：真实业务探测

**修复**：
- **删除** bench 手填 `applied:true, duplicate:false` 的路径。
- 新增 `app.HandleControl`：解析 → operation 校验 → **100..60000 ms 生产范围** → 会话内 operation 幂等 → **实际 interval 状态更新** → ACK 由真实结果生成。
  `app.ControlSession` 持有真实状态（`IntervalMS`、`AppliedCount`）与已处理 operation 集合。
- **生产 Device 与该处理器共用同一函数**（`internal/app/service.go` 业务循环改为调用它），因此两者不可能对"已执行"产生分歧。
- bench 的 trial 记录 `control_applied` / `control_duplicate` / `applied_interval_ms`（来自处理器真实返回）。
- 非法控制（1 ms、60001 ms）返回错误，**绝不 ACK 为 applied**；正常 5000 ms 实际更新；重复 operation 不重复副作用（`AppliedCount` 保持 1）。
- `EventSink` 改为**通知驱动**（容量 1 的有界信号 + 先扫缓冲），替代 10 ms 轮询；先到事件不丢失，仍受统一 trial deadline 约束。文档明确"轮询值不代表网络延迟"。
- 握手完成、业务未确认时：`handshake_success=true`、`probe_success=false`（有专门测试）。

## R2-3：终态、真实退出码、真实运行测试

**修复**：
- RUNNING 创建后，**所有程序可处理的后续错误统一走 `finalize()`**（临时目录失败、provisioning、events 文件、gateway/device runtime、listen、trials 文件、Encode、summary/重算）。`finalize` 写终态；**状态本身写不了时把根因打到 stderr 并返回真实非零码，绝不伪造文件存在**。
- 检查 Write/Sync/Close 错误；`trials.jsonl` 每行 Encode 后 Sync。
- **先停止并等待事件生产者**（cancelGW + `<-gwDone`）**再关闭事件文件**，最后重算验收。
- CLI **使用 `Run` 返回的真实 code** 做内部核对（删除了从 summary 反推退出码的 `exitCodeFor`）；内部操作码、验证器码、最终进程退出码分列；由外层脚本在进程结束后立刻保存真实退出码。
- 信号（SIGINT/SIGTERM）转 context 取消；文档注明 kill -9/掉电等**不可捕获终止只能由外层/下次扫描标 ABORTED**，不宣称所有异常都能写终态。
- 敏感临时目录在结果目录之外（`os.MkdirTemp`），结束 best-effort 清理。
- 新增**真实调用 `Run`/`runTrialCtx`** 的集成测试：setup 失败（临时目录故障注入，带注入有效性守卫）、**取消 → INCOMPLETE 且 requested 不缩小**、以及 cmd/bench 测试（formal 拒绝、缺参、未知 mode、坏结果验收失败）。

**审查回归**：`review_measurement_acceptance_test.go` 与 `review_run_failure_test.go` 放入 `internal/bench` 在真实工程运行；`ackFor` 夹具按新签名适配（`ControlSession`），断言数值未削弱。两个 setup-failure 测试现在通过（此前会留下 RUNNING）。

---

## 实际测试结果

| 项 | 结果 |
|---|---|
| `go mod verify` / `go vet ./...` / `go build ./cmd/...` | 全部 exit 0 |
| **全量 `go test -count=1 -json ./...`** | **291 顶层 / 218 子测试 / 0 失败**，16 包（12 个有测试），真实退出码 0 |
| 新增测量器与 CLI 测试 | internal/bench 全通过；**cmd/bench 4/4 通过**（该包此前无测试） |
| 四套自测 | 17 / 45 / 23 / 9 全过（bench 自测调用生产 Go 验证器） |
| Suite 1 / Suite 2 E2E | PASS / PASS |
| 20×3 fleet | PASS — 60 会话 / 60 唯一 SID / 180 遥测 / 180 控制 |
| 19 good + 1 bad | PASS — 19 台、57 会话，D010 失败 |
| **T smoke** | 10 请求 / 10 开始 / 10 完成 / 10 成功 / 0 失败，PASS，median 1.00 ms，单次 308/272 B，网关确认 10/10 |
| **H smoke** | 10 请求 / 10 开始 / 10 完成 / 10 成功 / 0 失败，PASS，median 1.00 ms，单次 1492/1360 B，网关确认 10/10 |
| **受控真实失败** | 3 请求 / 3 开始 / 0 成功 / 3 失败，**status=FAIL、进程退出码 3**；失败握手保留部分字节（1350 B）；验证器：`problems=[]` → 有效失败证据，不被接受 |
| **受控中断（取消）** | Go 集成测试：status=INCOMPLETE、退出码 4、requested 不缩小、验证器判为有效证据但 `acceptance_passed=false` |
| 独立验收输出 | 正常 T/H 两个 smoke 目录离线重验 `problems=[]` |
| `-race` / SM2 独立验证 | **未执行 / 不可得，pending** |

smoke 均带 `run_kind=smoke`、`formal_results_eligible=false`、父 run_id 与源码清单哈希，
每条成功 trial 可在 `gateway_events.jsonl` 中找到同 run/device/SID/operation 的 `control-confirmed`。
**失败不替换、不重试补齐**；历史数据未修改。

---

## 声明

- 未执行正式 1000 次采样，未生成性能排名；smoke 数字不构成正式性能结论；
- 单次 CPU 与峰值 RSS 继续 **null + 原因**；`go_heap` 标注为进程快照而非单次试验独占内存；
- 审查用坏结果夹具均为 synthetic/negative，未并入性能样本；
- 修复不构成安全审计或合规认证，也不意味着不存在其他缺陷。

**完成 M5A-R2，停止，不进入 M5B。**

---

## 集中自查补充项

本节记录"集中自查与统一收口"建议中与前三门重叠部分的核实结论，以及本轮新增的
取消回调交错检查。与前文不重复实现：验收器、业务处理器、结果写入器各只有一套。

### 与统一收口建议的重合项（已由前三门覆盖，仅作回归）

| 建议项 | 对应实现 | 状态 |
|---|---|---|
| 测量结果 schema、状态、统计与关联校验 | `internal/bench/schema.go` + `JudgeResults`（R2-1） | 已实现，本轮回归通过 |
| Device 与 bench 共用真实控制处理 | `app.HandleControl` + `app.ControlSession`（R2-2） | 已实现，本轮回归通过 |
| 异常收尾、事件文件关闭、持久化、真实退出码 | `finalize()` + CLI 真实 code（R2-3） | 已实现，本轮回归通过 |

未新增第二套验收器/业务处理器/结果写入器。"运行是否成功"与"失败证据是否完整有效"
分别由 `run_status` 与 `evidence_status` 判定；缺失字段与显式 null 由 schema 区分
（`null-or-int` 类型 + 原因字段）；失败样本不替换、不重试。

### 取消回调交错检查（本轮新增）

**源码位置**：`internal/handshake/timeout.go` —— `armIO`、`ioGuard`；
调用处：`internal/handshake/channel.go`（`sendGuard`/`recvGuard`，Send/Receive 各自
的 `stop`）、`internal/handshake/params.go`（`frameIO.gen`，H1..H4 固定预算）。

**核实结论：审查针对旧基线提出的风险在当前代码中真实存在，已修复。**

- **旧基线（generation 计数器）确实存在该竞态**：`context.AfterFunc` 的 `Stop()`
  不会等待已开始运行的回调。确定性回归 `TestCancelCallbackDoesNotClobberNextOperationDeadline`
  用屏障 setter 把旧回调精确停在"已通过 generation 检查、尚未应用 deadline"的位置，
  期间结束旧操作并让新操作安装自己的 deadline（now+600s）；随后放行旧回调 ——
  修复前实测旧回调把 deadline 覆盖为"now"（`m=+30.003s`），新操作的 `m=+600s` 被覆盖。
- **修复**：`ioGuard`（互斥 + 序号）覆盖三个临界区 —— 安装操作 deadline、取消回调应用
  "now"、结束操作（作废回调）。三条路径互斥后，旧回调只可能在其操作仍为当前操作时
  完成，或看到序号已推进而什么都不做；不可能与新操作的安装交错。
  **无死锁**：互斥只横跨 `conn.Set{Read,Write}Deadline`（即使连接关闭也立即返回），
  `stop()` 等待的是有界快速 setter，绝不等待网络 I/O。
- **测试方式**（`internal/handshake/timeout_deadline_interleave_test.go`，全部用屏障与
  明确事件，不用 sleep 定时）：
  1. `TestCancelCallbackDoesNotClobberNextOperationDeadline` —— 上述交错序列，
     断言最终生效 deadline 是新操作的、且回调在操作结束后不再有应用；
  2. `TestStopWaitsForAnInFlightCallback` —— 回调停在半途时 `stop()` 等待其完成，
     不死锁、不泄漏；
  3. `TestRepeatedArmAndStopDoesNotLeakGenerations` —— 25 轮 arm/cancel/stop，
     每轮断言生效 deadline 不超过已安装值（回调在 cancel 时把 deadline 设为 now
     是其职责，属合法观测），循环后再装一个最终 deadline 并断言无陈旧回调覆盖；
  4. `TestArmIOSetsTheFixedDeadline` —— 固定预算安装的正对照。
- **稳定性**：上述 4 个测试独立连跑 **15 轮全部通过（0 失败）**；全量回归中
  `cancel_test.go` 的 10 个既有取消/超时测试全部通过。
- **既有行为不倒退**：`cancel_test.go` 全部 10 个既有取消/超时测试在全量回归中通过
  （含等待发送槽、写阻塞、读阻塞、调用方 deadline、握手预算、上下文过期不杀已建立
  通道、幂等、goroutine 泄漏检查）。
- **测试过程中的自我修正（如实记录）**：初版夹具先后有四处缺陷，均已修正——
  ①在持锁状态下阻塞造成夹具自死锁；②门闩挡住了新操作自身的安装造成假通过；
  ③完成事件未随迭代排空导致断言读到上一轮事件；④对"回调在 cancel 时应用 now"
  这一合法行为做了错误断言（曾在一次运行中于第 17 轮报出，实为断言语义错误而非
  竞态残留）。最终版本：无锁等待 + 单次门闩 + 每轮排空事件队列 + 正确的断言语义，
  并以 15 轮独立连跑验证稳定。文件名曾用 `_arm` 后缀被 Go 视为 GOARCH 约束而
  未编译，已改为 `timeout_deadline_interleave_test.go`。

### 未完成事项

- `-race` 仍 ENVIRONMENT_BLOCKED（取消回调修复的并发正确性目前由功能交错测试覆盖，
  **不是** race 检测结果）；
- SM2 密钥交换独立实现对照仍不可得；
- 单次 CPU / 峰值 RSS 维持 null + 原因（正式方案在 docs/BENCHMARK_PLAN.md）；
- P 对照 NOT_IMPLEMENTED；正式 1000 次保持锁定。
