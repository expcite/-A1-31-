# M5A 里程碑报告：前置回归修复 + 测量工具 + smoke

**项目**：A1-31 园区边缘设备国密与后量子混合安全通信软件设计实现
**本轮范围**：审查包 `EdgeHybrid_R1_Review_Next` 的 F1–F4 前置修复 + M5A 测量器 + T/H 各 10 次 smoke
**本轮不做**：正式 1000 次采样、最终性能结论、cmd/bench formal 模式、新生命周期功能

**run_id**：`m5a-20260928T085056Z`（父 run：`m4_2_r1-20260927T131136Z`）
**审查包**：`EdgeHybrid_R1_Review_Next`（AGENT_NEXT_M5A.md、REVIEW.md、SOURCE_EVIDENCE.md、evidence/、regression/）
**源码清单**：137 文件，哈希 `4d995d6d0f514bd65759b65f8a7a52dfc91a624297dfb1403d090ae4cfa96a1d`
**验收等级**：`M5A_HARNESS_SMOKE_VERIFIED_PENDING`

> 历史缺口如实保留：**`-race` ENVIRONMENT_BLOCKED**、**SM2 密钥交换无独立实现对照**。

---

## F1：业务准入必须检查最新迁移策略

**复现**：审查模板 `review_admission_and_terminal_test.go` 放入 `internal/app` 用真实依赖运行，
`upgrade=true` 子测试失败："Suite 1 admitted after trusted minimum changed to hybrid without
changing keys"。`PeerSnapshot.Matches` 收了 revision 参数却不用；`admitSession` 只查
lifecycle/key_id/generation。

**修复**：
- `policy.ModeOf(suite)` / `policy.Satisfies(suite, minimum)`（`internal/policy/policy.go`）：
  显式把"协商套件"映射到策略层级并与最低模式比较。
- `PeerSnapshot.Matches` 现在**比较 per-entry revision**（是设备自身条目的 revision，
  不是全局 registry revision，避免其他设备更新牵连无关会话）。
- `admitSession` 新增策略门：协商套件不满足**最新**可信 MinimumMode → `policy-rejected` 并关闭，
  不开始业务。Suite 1 遇 hybrid 最低要求必须拒绝。

**测试**：`upgrade=true` 拒绝且未注册任何会话 ✅；`upgrade=false` 无更新对照成功 ✅；
原 N→N+1 拒绝与 N+1 新握手成功测试保留 ✅。真实 Suite 1 握手 + 真实注册表，无 mock。

**生效窗口（已文档化）**：本项覆盖"握手已完成、新业务尚未准入"的窗口——准入时重读注册表并
核对策略；已建立会话由 250ms watcher 处理，其语义不变。

## F2：截断与未确认控制不能记成正常完成

**复现**：`pending_control_no_ack` 与 `partial_record` 两个子测试失败，终态事件为
`peer-closed Incomplete:false`。

**修复**（`Gateway.serve` 终态 switch，三分支不再合并）：
- `ErrTruncated` → `outcome=terminated`、`ErrorStage=record`、`Incomplete=true`，
  detail 为真实错误（不再用 "between frames" 描述）；底层 `wire.ErrTruncated` 分类未削弱。
- `ErrPeerClosed` / err==nil → `peer-closed`，但 **`Incomplete = SentControls > ConfirmedControls`**：
  有控制已发出而未确认时关闭即不完整，detail 附加 "N control(s) sent but never acknowledged"。
- 干净关闭（无 pending）保持 `Incomplete=false`，正常对照不被误伤。

**测试**：三个场景全过 ✅，其中 `pending_control_no_ack` 正向观测
`SentControls=1, ConfirmedControls=0, Incomplete=true`；`partial_record` 终止于 record 阶段且
incomplete=true；`clean_idle_close` 对照保持非 incomplete ✅。
E2E verifier 语义未削弱：正常完成的会话仍不被标记 incomplete，Suite1/2 E2E 继续 PASS。

## F3：M4CASE 必填三态键必须存在

**复现**：审查实测删除 `business_accepted`/`plaintext_delivered`/两端 channel_created 仍 PASS。
我的解析器问题：`REQUIRED_TRI` 循环仅在 key 存在时校验值，缺键直接漏过。

**修复**（`scripts/summarize-go-test-json.py`）：抽出通用 `parse_kv_case`，
M4CASE 与 M2NEG **共用**同一严格规则：必需字符串键必须存在且非空；必需三态键**必须存在**
（显式 null 允许，缺键不允许）；**重复键拒绝**（不再最后一个值静默覆盖）；未知新键保留兼容。
M2NEG 同时加了"marker 存在但解析失败不得静默消失"的守卫（并入 problems → 非零退出）。

**这条守卫立刻在真实数据上抓到缺陷**：`internal/handshake` 的 5 处 M2NEG 发射
（`TestDefaultHybridConfigurationRunsSuiteTwo`、`TestUnusablePeerNeverYieldsAChannel` 三个子例、
`TestTamperedGatewaySignatureRejected` 等）的 `target`/`mutation` 为空或缺失 —— 旧解析器全部
静默放过。发射器已补全（`record`/`assert*` 签名显式要求 target/mutation，逐调用点填写真实
攻击对象与方式），完整流从 INCOMPLETE 恢复为 PASS。

**自测**：`m4case_extractor_selftest.py` 扩至 **45 项**（逐字段删除 ×4、显式 null、字段重排、
额外字段、重复键、M2NEG 正反例等），45/45 ✅。计数来自数据（负例 37、M4CASE 14），未硬编码。

## F4：Fleet 按关系精确验收

**复现**（`evidence/perturbations/`）：A. operation_id 改成含 device_id 的任意串；B. 确认的
sample_id 改 99999；C. 一台设备三个会话的业务全挪到第一个 SID；D. summary run_id 改掉 ——
原验收器全部误判 PASS。另有口径问题：原始流 `control-sent` 事件为 0，但输出 `controls_sent=180`
（实为 len(ctrl) 代填）。

**修复**：
- **生产侧**（`internal/app/service.go`）：`ch.Send` 成功后立即发 `control-sent` 事件
  （含 device/session/operation_id/sample_id 公开标识，无业务秘密）；
  `controls_sent` 从发送事件计数，`controls_confirmed` 从确认事件计数，二者不再互代。
- **验收器**（`scripts/verify_fleet_events.py`）按
  `(run_id, device_id, SID, sample_id, operation_id)` 精确关联：
  1. run_id 校验覆盖 gateway 流与 fleet summary（D 变体拒绝）；
  2. 每个已建立会话**分别**满足 samples/session 的遥测与控制数量（C 变体拒绝）；
  3. 会话内 sample_id 集合必须恰为 1..samples（重复/缺口拒绝），跨 SID 可复用；
  4. 每条确认控制必须对应**同一会话**内的一条遥测（B 变体拒绝）；
  5. operation_id 必须是 `set-interval-<device>-<sample>` 的 emitter 格式 ——
     **归属由发射格式证明，不是子串巧合**（A 变体拒绝）；
  6. 每条确认控制必须有对应 `control-sent` 事件（删除发送事件 → `controls_sent=null`，
     不代填）；
  7. 每个预期 device_id（含好设备）恰好一条结果记录，坏设备 ok 为布尔 false 且有失败信息。

**自测**：`fleet_verifier_selftest.py` 扩至 **19 项**（未篡改 PASS + A–D + 删发送事件 +
重复 sample + 缺正常设备行），19/19 ✅。**原始正常数据对照**：20×3 真实证据用新验收器重跑
仍 `PASS — 60 unique session ids` ✅。

---

## M5A 测量器与 smoke

**测量器**：`internal/bench` + `cmd/bench`。调用**真实**握手组件（`handshake.Client` 全流程：
SM2 签名、SM2 协商、Suite 2 的 ML-KEM、HKDF-SM3、SM4-GCM），无简化握手；每次试验独立
TCP 连接与独立临时材料，不降级、不复用会话。

- `bench -mode traditional|hybrid -run-kind smoke -trials N -out DIR`
- `formal` 请求**显式拒绝**（退出码 2），不会降级成较小的 formal 运行
- 输出 `manifest.json` / `trials.jsonl` / `summary.json` / `status.json`；
  状态先写 `RUNNING`，不可能残留旧 PASS
- 校验两端身份（ExpectedPeerID + PeerSnapshot）、实际 suite、Finished（Client 返回即已验证）、
  SID、业务探测 operation_id/ACK

**测量口径（已冻结进 docs/BENCHMARK_SPEC.md，BENCHMARK_PLAN.md 承载正式 1000 次设计）**：
- `tcp_connect_ns`：Dial 前后；`handshake_total_ns`：**临时材料生成之前** → 客户端验完
  H4/FinishedB；`probe_ns`：完整业务往返。三者分开列，不重复相加，不做跨进程时钟相减。
- wire bytes：客户端连接上的计数包装器记录实际 n（含部分写），C→S / S→C 分列，每字节只计一次；
  含 4 字节帧前缀，不含 IP/TCP 内核头（用户态不可测 → null）。
- CPU（单次）与峰值 RSS：**本机 API 不支持 → null + 原因**，绝不用 0 伪装；
  `go_heap_alloc_bytes`/`go_sys_bytes` 为**实测**但明确标注是与 RSS 不同的指标。
- 正式采样 CPU/RSS 方案已设计（逐次隔离子进程 + ProcessState / 平台峰值工作集）。

**smoke 实际结果**（`results/bench/m5a-*/`，`run_kind=smoke`，`formal_results_eligible=false`）：

| 模式 | trials | 成功 | 失败 | status | 握手 median | P95 | C→S bytes | S→C bytes |
|---|---|---|---|---|---|---|---|---|
| T traditional | 10 | 10 | 0 | PASS | 1.70 ms | 2.18 ms | 3 080 B | 2 720 B |
| H hybrid | 10 | 10 | 0 | PASS | 1.60 ms | 3.08 ms | 14 920 B | 13 600 B |

Suite 2 的 wire bytes 显著大于 Suite 1（KEM 公开数据实测可见），与规范预期一致；
单机单进程结果只代表本实验环境，不推断真实低功耗硬件，也不构成正式性能结论。

**测量器自测**：`bench_result_selftest.py` **12/12** —— 失败保留不重试、缺 trial/重复 trial_id、
错误身份、suite 不匹配、握手成功但缺 ACK ≠ overall success、0 假装实测、null 无原因、
截断 JSON、缺终态、formal-eligible 拒绝。

---

## 回归与证据

| 项 | 结果 |
|---|---|
| `go mod verify` / `go vet ./...` / `go build ./cmd/...` | 全部 exit 0 |
| **全量 `go test -count=1 -json ./...`** | **241 顶层 / 199 子测试 / 0 失败**，16 包全覆盖，真实退出码 0 |
| Suite 1 / Suite 2 E2E | PASS / PASS |
| 20×3 fleet | PASS — 60 会话 / 60 唯一 SID / 180 遥测 / 180 控制 |
| 19 good + 1 bad | PASS — 19 台、57 会话，D010 失败 |
| 四类自测 | 17 / 45 / 19 / 12 全过 |
| race | **ENVIRONMENT_BLOCKED，未执行** |
| SM2 交换独立验证 | **不可得，pending** |

**过程记录（不隐瞒）**：一次全量运行曾出现 4 个包同时 600s 超时；排查为更早被中断的前台
`go test` 进程仍存活造成资源争用（PowerShell 确认无残留后，干净重跑 1m43s 全过），
非代码缺陷。失败与超时的中间输出保留在任务日志中未删除。

---

## 声明

- 本轮未执行正式 1000 次采样，未生成最终性能排名；smoke 结果带
  `run_kind=smoke`、`formal_results_eligible=false`，不得作为竞赛性能数据引用；
- `cmd/bench` 仅开放 smoke；formal 显式拒绝；
- 审查包 `isolated_snapshot_method` 未复制进项目；`regression/` 模板已在本地真实依赖下
  编译运行（无需适配夹具）；
- 修复不构成安全审计或合规认证，也不意味着不存在其他缺陷。

**完成 M5A，停止，不进入 M5B。**
