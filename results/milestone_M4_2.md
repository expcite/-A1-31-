# M4.2 里程碑报告：生命周期与并发边界收口

**项目**：A1-31 园区边缘设备国密与后量子混合安全通信软件设计实现
**本轮范围**：只做 M4.2 收口 —— 修 M4.1 审查确认的缺陷、补真实证据、同步文档
**本轮不做**：M5 benchmark、1000 次握手、公开混合方案对照、GUI/Web、数据库、0-RTT

**run_id**：`m4_2-20260927T040822Z`
**源码清单**：`results/raw/m4_2_source_manifest.sha256`（127 个文件，
清单自身 SHA-256 `0820393ecbc4855ba05dfd298aef38a3197f9589603c320a92318602c65dc23e`）
**核验机器**：Windows amd64，`go1.27.1`，MinGW-W64 gcc 8.1.0，OpenSSL 3.5.7

**验收等级**：`M4_2_HARDENING_VERIFIED_PENDING`

> **为什么不是"完整通过"**：本轮修复全部落地且回归通过；但 **`-race` 仍是 ENVIRONMENT_BLOCKED**，
> **SM2 密钥交换仍缺独立实现对照**。

---

## 1. legacy keystore fallback 怎么修

**确认是真实缺陷**：`FileStore.load` 里判断的是 `keyPaths()` 的错误（`err`），
而不是本次 `os.ReadFile` 的结果（`lastErr`）。由于 `err` 在循环里恒为 nil，
循环会在**第一次**迭代就 `break` 并把一个**根本没读到的路径**当成命中 ——
结果：**legacy `signing.key`/`agreement.key` 布局永远加载不到**，
M4.1 声称的"旧目录无需迁移"是假的。

修复（`internal/identity/keystore.go`）：

| 点 | 做法 |
|---|---|
| 成功判定 | `if lastErr == nil`（本次读取的结果） |
| 非 ENOENT | 直接返回错误，**不**静默回退到旧文件（权限/目录/损坏都算真错误） |
| 找不到 | 明确的 "no key file for identity/key" 错误，而不是让人困惑的解析错误 |

新增回归（`keystore_layout_test.go`）：
- `TestLegacyKeyLayoutStillLoads` —— 只有 legacy 文件、registry 仍带对应 key_id → 两把密钥都能加载；
- `TestVersionedLayoutTakesPrecedence` —— 两者都在时优先 versioned，旧文件不能遮蔽已轮换世代；
- `TestNonNotExistReadErrorIsNotSilentlyFallback` —— versioned 路径是目录时**必须**报错，不能悄悄用 legacy；
- `TestMissingKeyFileReportsNotFound`。

---

## 2. stale rotation 怎么阻止

M4.1 的 `ApplyRotation` 是"读出来 → 改 → 无条件 `Replace`"，**没有原子校验**。
于是两个从同一世代准备的轮换都会通过各自的检查，后到者覆盖先到者，
甚至可能把世代**倒退**到已退役的密钥材料上。

修复：**compare-and-replace**。新增 `Registry.CompareAndReplace(p, RotationPrecondition)`，
前置条件校验与写入在**同一个写锁临界区**内完成：

```
current generation       == FromGeneration
current sign_key_id      == RetiredSignKeyID
current agreement_key_id == RetiredAgreementKeyID
```

不满足 → 返回 `ErrStaleRotation` 且**一个字节都不写**。
`ApplyRotation` 先在锁外做一次快速检查（给出清晰错误），再交给 `CompareAndReplace` 做权威判定。
另外 `Rotation.Validate()` 收紧为 **`ToGeneration == FromGeneration + 1`**（严格 +1，不许跳号或回退）。

新增测试：`TestStaleRotationCannotRollbackGeneration`、
`TestStaleRotationCannotOverwriteNewerKeyIDs`（只有 key-id 检查能抓住的那种伪造）、
`TestConcurrentRotationsOnlyOneCommit`（8 个 worker 竞争同一世代 → **恰好 1 个提交**）。

**作用域声明（不夸大）**：
> **trusted admin control plane is single-writer across processes.**
> 上述保证只在**同一个 Registry 实例内**成立。跨进程（例如两个 `secctl` 同时改）
> **没有文件锁**，因此**不声称**跨进程并发更新安全。运维上必须保证单写者。
> 已写入 `docs/LIFECYCLE.md` 与 `IMPLEMENTATION_STATUS.json` 的 `single_writer_scope`。

---

## 3. Reload revision rollback 怎么阻止

`Reload` 是"先读文件并解析 → 之后才加锁 → 直接覆盖 `s.peers`/`s.revision`"。
中间若 `Replace` 提交，Reload 会把**更旧的视图**写回内存，静默撤销刚提交的吊销/轮换。

修复：采用**发布时比较**（option B）。加完锁后：

```go
if fs.revision < s.revision {
    return s.revision, nil   // 不采纳更旧的快照
}
```

之所以不选"把读文件也放进锁里"（option A）：那会让每次 reload 与每次提交串行，
拖慢握手路径；而陈旧快照本来就应该被丢弃，没必要为它加锁。

**确定性测试** `TestReloadCannotRollbackNewerReplace`：
1. 加载（rev R）→ 2. `Revoke` 提交（rev R+1）→ 3. **直接把旧的 rev R 文件写回磁盘**
   （精确复现"reload 读到了提交之前的内容"）→ 4. `Reload` 必须**不采纳**，
   `Revision()` 不下降，D001 仍保持吊销。
另有并发伴随测试 `TestRevisionIsMonotonicUnderConcurrentReloadAndReplace`。

**最终不变量：`Registry.Revision()` 单调不下降。**

---

## 4. full-capacity reconnect 怎么修

M4.1 的 `Register`：替换同设备时虽然不再拒绝，但**之后**仍执行
`len(sessions) >= MaxSessions`。后果是**满容量时替换会被错误拒绝**，
而 `defer` 又已经把旧 session 关掉了 —— map 里留下一个已关闭的会话。

修复（`internal/app/sessions.go`）：

| 规则 | 行为 |
|---|---|
| 替换同设备 | **不计入** session count，**不受** MaxSessions 限制（是 swap，不是 admission）；旧 session 在**锁外**关闭 |
| 新 device | 仍是 admission，满容量（或超单身份上限）**仍拒绝** |
| map 最终态 | 必须指向新 session（已有测试断言） |

新增：`TestReconnectCanReplaceAtSessionCapacity`（容量=1，同设备重连成功且 count 仍为 1、
map 指向新 session）、`TestNewIdentityCannotEnterAtSessionCapacity`（新 device 仍被拒且不影响既有会话）、
`TestSupersededSessionCannotRemoveReplacement`。

---

## 5. lifecycle sweep 如何只杀旧 session

原 `SweepRevoked`：读到 `st` → 检查 → 然后 `Terminate(id)`。
检查与终止之间，新握手可能已完成并取代了 `st`，此时 `Terminate(id)` 会**误杀新会话** ——
设备会因为一条关于"它已经换掉的会话"的决定而被踢下线。

修复：新增 `SessionRegistry.TerminateIfCurrent(deviceID, st *SessionState)`，
只有当 map 里当前仍是**它检查过的那个指针**时才移除并关闭。
`SweepRevoked` 的两条路径（lookup 失败、key-id 退役）都改为使用它。
被取代的旧 `serve` 协程的收尾也统一走 `UnregisterIfCurrent`。

新增：`TestLifecycleSweepCannotKillSupersedingSession`。

---

## 6. registry commit failure 是否真的注入并验证 —— **是**

M4.1 的 `TestRegistryFailureLeavesOldGenerationUsable` **从未真正调用会失败的
`ApplyRotation`**，所以它对"提交失败"什么都没证明。

本轮重写为**真实失败注入**：

1. 建立真实 `fileStore` → 写好 N+1 密钥文件；
2. **注入**：`Replace` 的持久化是"写 `<trust>.tmp` 再 rename"。
   把 `<trust>.tmp` 占用为一个**目录** → `os.WriteFile` 必然失败。
   （注意：把 trust 文件本身换成普通文件**无效** —— rename 会直接覆盖它，提交会"成功"，
   这一点已在注释里写明，避免后来者误用。）；
3. **真正调用 `ApplyRotation`** → 断言返回错误；
4. 断言：内存 registry 仍指向 N；**磁盘 registry 仍指向 N**；N 的两把私钥仍可加载；
   N+1 文件存在但**未被 registry 引用**（不在当前 key_id、也不在 retired 列表）。

已重新生成 `results/raw/m4_2_rotation_failure_cases.jsonl`（3 条）。

---

## 7. full go test 的实际统计

**实际运行 `go test -count=1 -json ./...`（全项目，非 targeted）**，
原始流保存于 `results/raw/m4_2_tests.jsonl`，真实退出码
`results/raw/m4_2_go_test_exit_code`，严格汇总器以**全部 15 个包**为 expected set：

```text
packages        : 15 total, 10 ok, 0 failed, 5 without tests
top-level tests : pass=222 fail=0 skip=0
subtests        : pass=190 fail=0 skip=0
negative cases  : 37
m4 cases        : 10
VERDICT: PASS (exit 0) — evidence complete, expected packages covered, go exit code 0
```

**222 / 190 由 JSON 自动统计，未预写。**
另单独保留 targeted 回归流（`m4_2_targeted_tests.jsonl`，identity/app/handshake/secctl：
138 顶层 / 114 子测试，0 失败）作为便利回归；
**但"全部测试通过"的数字由上面的全量流支撑，不用 targeted 流冒充。**

---

## 8. 20×3 和 19+1 结果

**20 devices × 3 Suite 2 sessions**（`m4_2_fleet_summary.json`，精确核算）：

```
result                       : PASS
expected_devices             : 20
devices_with_hybrid          : 20
hybrid_sessions_total        : 60
sessions_per_device_expected : 3
unique_sid_count             : 60
telemetry_expected/accepted  : 180 / 180
controls_confirmed           : 180
kem_used_count               : 20   |   selected_suite: 2
checks                       : 17 / 17 ok
```

**19 good + 1 bad**（独立运行）：`PASS — 19 devices, 57 sessions`，D010（不可信身份）失败。

Suite 1 / Suite 2 单设备 E2E 均回归 PASS。

---

## 9. wire replay 证据字段语义修正

此前把 `device_channel_created` / `gateway_channel_created` 写成了 `false` ——
但 **Session B 在注入之前已经完成握手**，两个 channel 是**真实存在的**，写 false 是**假陈述**。

修正为：

```
device_channel_created  = true    （B 的握手确实完成了）
gateway_channel_created = true
plaintext_delivered     = false   （重放的明文从未交付）
business_accepted       = false   （无任何业务副作用）
record_replay_accepted  = false   （新增字段：重放记录未被接受）
```

跨设备重放同样处理。**未观测的字段继续用 `null`，不猜 `false`** ——
轮换 failure 类用例从未建立 channel，因此其 channel/明文/业务字段一律为 `null`。

---

## 10. manifest / run_id / 文档

- **根目录旧 `MANIFEST.sha256`（Starter 骨架 47 文件，自检 25 项 FAILED）已移出**，
  改为 `docs/history/STARTER_MANIFEST.sha256` 并附
  `docs/history/STARTER_MANIFEST.README.md` 说明"仅供追溯、不可用于校验"。
  现在在项目根目录运行 `sha256sum -c MANIFEST.sha256` **不会**得到一堆 FAILED（该文件已不存在）。
  **每次运行自己的清单**位于 `results/raw/<run>_source_manifest.sha256`；
  **交付包的根目录 `MANIFEST.sha256` 是唯一且有效的交付清单**。
- **统一 M4.2 run_id**：`milestone_M4_2.md`、`m4_2_readiness.json`、
  `m4_2_verification.txt`、`m4_2_summary.*` 均引用 `m4_2-20260927T040822Z`。

### 文档清理（清掉的过时说法）

| 文件 | 清掉的内容 |
|---|---|
| `README.md` | "当前里程碑 M2" → **M4.2**；"密钥轮换/吊销/20设备并发 未实现（M4/M5）" → **已实现并测试**，仅性能基准保留未实现 |
| `IMPLEMENTATION_STATUS.json` | `milestone` → M4.2，`version` → `0.6.2-m4.2`，`readiness` → `M4_2_HARDENING_VERIFIED_PENDING`；新增 `single_writer_scope` |
| `docs/PROTOCOL_DRAFT.md` | "Suite 2 本轮未实现" → **M3 起已实现并开放**；"默认配置拒绝启动" → 该规则仍成立但**当前默认配置可运行（实跑 Suite 2）**；版本头改为 v0.3 |
| `docs/LIFECYCLE.md` | **"轮换一次只替换其中一类密钥"**（与实现矛盾）→ 改为"一次设备轮换**同时**产生新 signing 与新 agreement 密钥，两类始终相互独立"；新增 CAS 与单写者作用域章节 |
| `cmd/secctl status` | `milestone` → `M4.2` |

---

## 11. 本轮被测试抓出的缺陷

1. `load` 判断错变量 → legacy 布局**根本无法加载**（M4.1 的兼容声明是假的）；
2. `ApplyRotation` 无原子校验 → stale 轮换可覆盖/回滚世代；
3. `Reload` 可把旧快照写回 → 静默撤销刚提交的吊销/轮换；
4. `Register` 满容量拒绝替换 → 重连失败且 map 留下已关闭会话；
5. `SweepRevoked` 误杀取代它的新会话；
6. 轮换"提交失败"测试从未真正调用失败路径。

---

## 12. 仍然保留的两个历史 gap

- **`-race`：ENVIRONMENT_BLOCKED**，未执行。新增并发测试是**功能性**的，
  **不是** race detector 结果，不得如此汇报。
- **SM2 密钥交换独立实现对照**：没有第二实现，继续 pending。

不因此伪造 PASS。

---

## 13. 声明

- 未生成正式性能数据，未跑 1000 次握手，未做公开方案对照；
- `cmd/bench` 继续 fail-closed；
- 事件日志只含公开元数据，不含共享秘密、PRK、会话密钥、nonce 前缀或业务明文；
- 包内不含 `runtime/` 私钥、ML-KEM 私钥（从不落盘）、可执行文件、缓存、`.git`；
- **Suite 2 存在 ≠ 系统抗量子**（SM2 身份认证不是后量子认证）。

**完成 M4.2，停止，不进入 M5。**
