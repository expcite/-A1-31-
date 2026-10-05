# M4.2-R1 里程碑报告：生命周期原子性、认证移交与证据验收

**项目**：A1-31 园区边缘设备国密与后量子混合安全通信软件设计实现
**本轮范围**：只做审查包 `EdgeHybrid_M4_2_Audit`（REVIEW.md B1–B4/E1–E2）的定向修订
**本轮不做**：M5 采样、cmd/bench 开放、新功能、网页/数据库/新协议

**run_id**：`m4_2_r1-20260927T131136Z`
**审查包**：`C:\Users\20477\WorkBuddy\密码技术竞赛\EdgeHybrid_M4_2_Audit`
（AGENT_NEXT_M4_2_R1.md、REVIEW.md、SOURCE_EVIDENCE.md、regression/ 两个测试模板、evidence/）
**验收等级**：`M4_2_R1_REVISION_VERIFIED_PENDING`

> 两个历史缺口如实保留：**`-race` ENVIRONMENT_BLOCKED**、**SM2 密钥交换无独立实现对照**。

---

## B1：统一生命周期原子更新

**审查复现**（已复现）：CAS 只比较 generation + 两个 key_id，不比较 peer revision / Lifecycle，
且 Revoke/Retire 是"Lookup 后无条件 Replace"。因此
①轮换准备后发生吊销/退役 → 轮换提交把设备**重新激活**为 ACTIVE（generation 还升到 2）；
②revoke 读到 generation 1 → 轮换提交 generation 2 → revoke 用**整份旧 Peer** 写回 →
密钥世代和 key_id **倒退回 1**。审查包 `regression/review_lifecycle_interleavings_test.go`
在修复前确实失败（revoke/retire 两例 + 反向回滚一例）。

**修复**（`internal/identity/identity.go`、`lifecycle.go`、`contracts.go`）：

| 项 | 做法 |
|---|---|
| 前置条件扩展 | `RotationPrecondition` 增加 `Revision` 与 `Lifecycle`；新增通用 `SnapshotPrecondition` |
| **CAS 无条件比较 revision 与 lifecycle** | `CompareAndReplace` 在写锁临界区内检查 `cur.Revision != expect.Revision`、`cur.EffectiveLifecycle() != expect.Lifecycle` —— 任何一次提交都会 bump revision，因此**任何**交错（吊销/退役/策略修订）都会被拒绝 |
| Revoke/Retire | 不再"读整份 Peer → 改 → 写回"，改为 `setLifecycle`：读快照 → `snapshotOf(before)` → **只改 lifecycle 字段** → `CompareAndReplaceSnapshot`。锁外旧 Peer 不可能整份写回 |
| 准备快照 | `Rotation.PreparedRevision`/`PreparedLifecycle` 由**真正准备阶段**的同一次读取填入（`secctl rotate` 已填）；`ApplyRotation` 携带时用之，未携带时用其内部读取并**同样无条件比较** —— 两种窗口都覆盖，绝不"用最新读取冒充准备时前置条件" |

**交错测试**（`lifecycle_atomic_test.go` + 审查模板）：
- 旧快照 → 吊销提交 → 轮换提交被拒 → 世代仍 2、新 key_id 仍在 ✅
- 旧快照 → 退役提交 → 轮换提交被拒 ✅
- 准备轮换 → 最低模式/策略 revision 更新 → 旧轮换提交被拒（**这是 revision 检查的核心价值**：策略修订不改 generation/key_id，旧检查抓不到）✅
- 吊销/退役读旧条目 → 轮换提交新世代 → 生命周期操作不能把 generation/key_id 写回旧值 ✅
- 无交错顺序对照：rotate→revoke 顺序成功、revoked 后再 Revoke 明确报错、retire 后轮换被拒 ✅
- 原有多轮换恰好一个成功、提交失败不损坏旧材料、Reload 不回退 继续通过 ✅
- **磁盘 read-back 已补**：`TestRegistryFailureLeavesOldGenerationUsable` 现在重新 `LoadTrustStore` 打开 trust 文件，断言磁盘世代/key_id 仍为 N ✅

**夹具适配声明**：`reviewReplaceInterposer` 原本 hook `Replace`；Revoke/Retire 现在提交走
`CompareAndReplaceSnapshot`，因此夹具改为 hook 该方法（审查允许）。为满足"必须正向证明交错确实发生"，
hook 内**断言轮换已提交**（generation == 2）并记录 `sawInterleave`；若调度入口不再触发，测试**失败**而非静默通过。

---

## B2：保留实际认证的身份快照

**审查确认**：握手用最初查到的 Peer 校验 H1/H3，之后 `serve` 再 Lookup **当前** Peer，
把**当前**的 key_id/generation 填进 SessionState —— 旧密钥认证的会话被登记成新世代。

**修复**（`internal/handshake/channel.go`、`server.go`、`client.go`、`internal/app/service.go`）：

1. **握手结果携带不可变认证元数据**：`Channel.PeerSnapshot()` 返回
   `PeerSnapshot{PeerID, SignKeyID, AgreementKeyID, KeyGeneration, RegistryRevision}`，
   来源是**本次验证实际使用的那份 Peer**（在签名与 Finished 校验处读取），不是之后的 Lookup，
   也不采信对端自报。`Matches()` 在没有记录 key_id 时返回 false（fail-closed，不当作匹配）。
2. **`serve` 拆分为三个可独立调用的阶段**：`handshakeConn` → `admitSession` → `business`。
   这是审查建议的"拆分真实准入函数、从测试控制调用时序"路径。
3. **`admitSession` 读取最新注册表**（先 `Reload` 再 `Lookup`），并做三道门：
   lifecycle 可用性、`UsableForNewSessions`、**认证快照与当前条目比对** —— 不匹配即
   `stale-generation-rejected` 并关闭，不开始业务。
4. **SessionState 保存真实认证结果**：`SignKeyID/AgreementKeyID/KeyGeneration` 全部来自
   `snap.*`，不再是当前注册表条目的值。Sweep 仍对同一捕获的 `SessionState` 用 `TerminateIfCurrent`。

**确定性真实协议回归**（`interleave_test.go`，无 mock Channel、无故障钩子）：
- 用 generation 1 完成真实 SM2/ML-KEM 认证与 Finished（真实 TCP + `handshakeConn`）；
- 断言 `ch.PeerSnapshot()` 记录的是 gen 1 / D001-SIG-1；
- 管理端提交 N+1（真实 `ApplyRotation` 写盘）；
- 恢复交接：用**同一 Channel** 调 `admitSession` → **被拒**，错误明确指向 generation；
  注册表中**没有**该会话（`Count()==0`）→ 未登记成 N+1；
- **后续真正使用 N+1 的新握手成功**并被正常准入（设备重载新世代身份）✅
- **无轮换对照** `TestAdmissionSucceedsWithoutInterleaving`：正常握手 → 准入成功，
  登记的 key_id/generation 与认证快照一致 ✅（防止门变成一律拒绝）
- 无 `telemetry-accepted`/`control-applied`/ACK（明文未交付、业务未接受）✅
- 结构化记录：`ch=true/true, pt=false, biz=false, record_replay_accepted=null`（未观测即 null）

**关于 PreAdmitHook**：我上一轮加过 `Gateway.PreAdmitHook` 公开字段，审查指出这属于
"公开 Gateway 测试字段"，已**移除**。定义/调用位置/构建范围：字段声明于 `Gateway` 结构体、
仅在 `serve` 的握手成功后调用一次、只在 `_test.go` 赋值 —— 但确实会随生产二进制编译进去。
现改为拆分 `handshakeConn`/`admitSession`，测试直接驱动真实函数，**不再需要任何注入点**。

---

## B3：旧会话在锁外清理

**审查复现**：`Register` 先 `defer Unlock` 后 `defer cleanup`，LIFO 使 cleanup **在锁内**执行
（审查用 `mu.TryLock()` 在 cleanup 回调里返回 false 证明了这一点）。

**修复**：锁内只做 map 替换与计数（替换同身份不计槽、满容量对**新身份**仍拒绝），显式
`Unlock` 之后才 `cancel()` / `Channel.Close()`。注释如实写明旧实现为何在锁内。

**回归**：
- `TestReviewReplacementCleanupRunsOutsideRegistryLock`（审查模板，TryLock 正向证明）✅
- `TestReviewSlowCloseDoesNotBlockOtherIdentities`（**通道同步，无 sleep**）：cleanup 的
  `Close()` 用 `entered` 通道宣告"已进入 Close"，测试等到这一点后再注册 D002 ——
  D002 的 `Register` 必须在 teardown 仍阻塞时完成；teardown 结束后计数为 1、D001 已移除 ✅（连跑 3 次稳定）
- 满容量同身份替换、新身份满容量拒绝、旧协程不能删除新会话 ✅

> 修正说明：我最初版本的 slow-close 测试用计数断言有竞态（未确认清理已进入 Close 就断言），
> 已改为通道同步并连跑 3 次验证。

---

## B4：及时释放握手名额

**审查确认**：`Admit` 的 release 被 defer 到整个 `serve` 退出，等于把业务会话也算进并发握手数。

**修复**：release 移入 `handshakeConn`，在 `handshake.Server` 返回后**立即**调用——成功、失败、
取消三条路径都到达这一个释放点；不再在 Run 的 goroutine 上 defer。幂等由单一调用点保证，
未调大任何上限。

**确定性回归**（`interleave_test.go`）：
- `TestHandshakeSlotReleasedBeforeBusiness`：`MaxHandshakes=1, MaxSessions=4`，第一台
  **保持已建立业务连接**，第二台仍能完成握手 ✅
- `TestHandshakeSlotReleasedOnFailure`：半握手（连上不发数据）→ 先等它**确实占用名额**
  （in-flight==1），再等它释放（in-flight==0），随后真实设备能用该名额完成握手 ✅
- `TestStaleGenerationSessionIsNotRegistered` 内断言：握手结束、业务未开始时 in-flight==0 ✅
- 等待全部有上限（`waitFor` 预算 10–25s），使用事件/轮询上限，未加 sleep、未调大上限 ✅

---

## E1：结构化案例提取

**审查复现**：原始全量流 **13 条 M4CASE**，旧解析器只提取 **10 条** —— 三条 replay 在
`business_accepted` 与 `detail` 之间多了 `record_replay_accepted`，固定字段顺序的正则失配，
且**仍返回 PASS**。我复跑确认：修复前 10 条、修复后 13 条。

**修复**（`scripts/summarize-go-test-json.py`）：
- 删除固定顺序正则，改为 **marker + 顺序无关的 `key=value` 扫描**；未知新增字段**保留**不拒收；
- 必需字段缺失、空值、tri-state 非 `true/false/null` → 记入 `problems` → **证据不完整 / 非零退出**；
- 计数完全来自数据，**未硬编码 13**；`test` 来源随每条记录保存。

**独立自测** `scripts/m4case_extractor_selftest.py`（**24 项，与 17 项通用自测分开**）：
文档顺序 / 乱序 / 末尾未知字段 / **中间插字段（即本次事故形态）** / 三态三种取值 / Go 转义字符串 /
缺必需字段→非零 / 坏 tri-state→非零 / 空必需字段→非零 / 重复 case id 可从数据计数 / 空集合报 0 /
计数随数据（3 条→3）✅ 24/24
修复前行为验证：`results/raw/m4_2_r1_tests.jsonl` 在旧解析器下 10 条、新解析器 14 条（本轮多了一条
stale_generation 案例），两个数字都来自实际数据。

---

## E2：Fleet 验收

**审查复现**（`evidence/verifier_checks.json`）：删 operation_id、业务 SID 指向不存在的会话、
删/重复坏设备行 —— 原验收器全部误判 **exit 0**。

**修复**（`scripts/verify_fleet_events.py`），新增关联校验：
1. `telemetry-accepted`/`control-confirmed` 的 `(run_id, peer_id, session_id)` 必须命中
   一条**已建立会话**（`established`），否则拒绝；
2. 每条确认控制必须有 `operation_id`；每条遥测必须有 `sample_id`；若有 ack 事件，必须能按
   `(run_id, device_id, session_id, operation_id)` 对上一条控制；
3. 坏设备行**恰好 1 条**、`ok` 必须是布尔 **false**、必须有非空失败信息，且该设备不得出现在
   已认证 peer 集合中（缺/重/类型错/ok=true 全部拒绝）；
4. `controls_sent` 由 `control-sent` 事件计数，不再抄 confirmed 的数量。

**独立自测** `scripts/fleet_verifier_selftest.py`（**11 项**，非 17 项汇总器自测）：
合成内部一致的 3 设备流（2 好 + 1 坏）→ 未篡改必须 PASS；七种篡改（删 operation_id、
SID 指向未知会话、删坏设备行、重复坏设备行、ok=true、ok 非布尔、缺 sample_id）逐一必须 FAIL
且失败原因指向对应规则 ✅ 11/11。
**原始数据对照**：真实 20×3 证据用新验收器重跑 → `VERDICT: PASS — 60 unique session ids` ✅
（未把正常数据误伤）。

---

## 验证与证据

| 项 | 结果 |
|---|---|
| `go mod verify` | all modules verified |
| `go vet ./...` | exit 0 |
| `go build ./cmd/...` | exit 0 |
| **全量 `go test -count=1 -json ./...`** | **239 顶层 / 194 子测试，0 失败**，15 包全覆盖，真实退出码 0 |
| targeted（identity/app/handshake/secctl） | exit 0（便利回归，不支撑全项目数字） |
| Suite 1 E2E | PASS |
| Suite 2 E2E | PASS |
| 20×3 fleet | **PASS — 60 会话 / 60 唯一 SID / 180 遥测 / 180 控制确认** |
| 19 good + 1 bad | PASS — 19 台、57 会话，D010 失败 |
| 汇总器自测 | 17/17 |
| M4CASE 提取自测（E1） | 24/24 |
| Fleet 验收自测（E2） | 11/11 |
| race | **ENVIRONMENT_BLOCKED，未执行** |
| SM2 交换独立验证 | **不可得，pending** |

过程中发现并修复的两个自引入回归（如实记录，均有原始失败记录
`results/raw/m4_2_r1_e2e_suite1.txt` 的第一次 FAIL 运行）：
1. `serve` 拆分后 `handshake/established` 事件发了两次 → 严格验收器判"exactly one gateway handshake — found 2"；
2. 网关终态 `peer-closed` 被我误加 `Incomplete:true` → "no endpoint marked the session incomplete"。
两者均已修复并复跑通过；失败日志保留未删除。

---

## 声明

- 未开放 `cmd/bench`，未做 M5 采样；M5 指标未用未修复基线生成任何性能结果；
- 事件日志只含公开元数据；包内不含 `runtime/` 私钥、可执行文件、缓存、`.git`；
- 审查包的 `isolated_module`/测试替身**未复制**进项目；`regression/` 两个模板已在
  **本地真实密码依赖**下执行（其中一个夹具按审查允许做了接口适配，并正向证明交错发生）；
- 本轮修复不构成安全审计或合规认证，也不意味着不存在其他缺陷。

**完成 M4.2-R1，停止，不进入 M5。**
