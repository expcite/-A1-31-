# M4.1 里程碑报告：并发安全、真实重放证据与轮换 failure-safe

**项目**：A1-31 园区边缘设备国密与后量子混合安全通信软件设计实现
**本轮范围**：只做 M4.1 收口 —— 修 M4 审查确认的缺陷、补真实证据、同步文档
**本轮不做**：M5 benchmark、1000 次握手、公开混合方案对照、GUI/Web、数据库、0-RTT

**run_id**：`m4_1-20260927T023335Z`
**源码清单**：`results/raw/m4_1_source_manifest.sha256`（123 个文件）
**核验机器**：Windows amd64，`go1.27.1`，MinGW-W64 gcc 8.1.0，OpenSSL 3.5.7

**验收等级**：`M4_1_HARDENING_VERIFIED_PENDING`

> **为什么不是"完整通过"**：本轮修复全部落地且回归通过；但 **`-race` 仍是 ENVIRONMENT_BLOCKED**，
> **SM2 密钥交换仍缺独立实现对照**。

---

## 一、Lookup 并发问题怎么修

**确认是真实缺陷**：`fileStore.Lookup` 直接读 `s.peers`，而 `Replace`/`Reload`
在写锁下整体替换该 map —— 这是数据竞争。

修复（`internal/identity/identity.go`）：

| 点 | 做法 |
|---|---|
| 加锁 | `Lookup` 改用 `mu.RLock/RUnlock`（读路径共享锁，写路径独占锁不变） |
| **防御性拷贝** | 返回前 `p.clone()`，深拷贝 `SignPublic` / `AgreementPublic` / `SM2UserID` / `RetiredKeyIDs`。此前调用方可通过这些 slice 直接改写注册表内部状态 |
| 锁纪律审查 | `IDs`/`Revision` 用 RLock+defer ✓；`Reload` Lock+defer ✓；`Replace` 从手写 Unlock 改为 **defer**（两处 return，手写解锁极易在后续编辑中被破坏）；`writeLocked` 文档明确"调用方持锁" |
| `Replace` 也存副本 | 存入 `p.clone()`，与 Lookup 对称 |

**新增并发功能测试**（`internal/identity/concurrency_test.go`）：
Lookup+Reload、Lookup+Replace、Lookup+吊销、Lookup+轮换，以及
`TestLookupReturnsADefensiveCopy` / `TestPeerCloneIsolation`。

**诚实声明**：这些是**功能性并发测试**。本机 `-race` 无法运行
（`ENVIRONMENT_BLOCKED`），**不得**把它们当成 race PASS。

---

## 二、真正的旧密文 wire 级重放如何测试

此前 `TestOldSessionRecordsCannotBeReplayed` 只在关闭旧通道后调 `chA.Send` ——
**没有把 Session A 的真实密文送给 Session B**，证明力不足。

新测试 `TestWireReplayOfOldSessionRecordsIsRejected`（`internal/handshake/wire_replay_test.go`）：

1. 建立 Session A，经 relay 捕获其**真实 `wire.ProtectedRecord` 完整原始帧**
   （4 字节长度前缀 + version + kind + SM4-GCM 密文与 tag）—— 共 3 条：首/中/末；
2. 正常关闭 A；
3. 完整新握手建立 Session B（新 SID，实测与 A 不同）；
4. 在 B 建立后，把 A 的 3 条原始帧**逐字节原样**注入 B 的连接；
5. B **拒绝并关闭**，网关侧报 `invalid record`；
6. **无任何业务副作用**（无 telemetry-accepted / control-applied / ACK）。

**用真实密文，不自构造"类似旧记录"的假 payload** —— 结构化记录见
`results/raw/m4_1_wire_replay_cases.jsonl`。

---

## 三、真正的跨设备密文 replay 如何测试

原测试只证明**业务身份绑定**（payload 写 `device_id=D002`）。新增
`TestWireCrossDeviceCiphertextIsRejected`，**两个方向都测**：

| 方向 | 注入 | 结果 |
|---|---|---|
| D001 → D002 | D001 的一条真实密文帧注入 D002 会话 | 记录层拒绝 |
| D002 → D001 | 对称 | 记录层拒绝 |

D002（及 D001）**不接受任何来自对方会话的业务数据**。
结构化记录：`cross_device_ciphertext_replay = rejected`（每个方向一条）。
原业务绑定测试**保留**。

---

## 四、rekey 是否只在 Due 时触发 —— **是（已修）**

**M4 的真实缺陷**：`Device.Run` 只要 `MaxSessions > 1` 就无条件跑满，
即使 `Rekey.Due()` 为 false 也关闭会话重新握手 —— 设备被无理由断开。

**修正**：
- `MaxSessions` 只是**连续会话数的上限**，**绝不是**再发起一次握手的原因；
- 只有 `Due()` 为 true（在事务边界判定）才进入下一次完整握手；
- 未达阈值 → 本会话服务完流量后**正常结束**，不产生 rekey-required 事件。

**计数口径（选定并三处一致）**：
> **每次成功写出或读入 1 条 ProtectedRecord 计 1。**
> 一个完整 `遥测→控制→ACK` 事务在设备侧 = **3 条**（1 遥测发出 + 1 控制收到 + 1 ACK 发出）。
> 判定**只在事务边界**进行：达到阈值后允许完成当前 control/ACK，
> 在**下一条遥测之前**关闭并换钥 —— 因此**不会产生 pending control**。

实现、`docs/LIFECYCLE.md`、测试三处一致。事件新增 `SentAcks` 字段，
使**可观测计数与规则一致**（否则事件里只有 2 条/事务）。

新增测试：
- `TestNoRekeyBeforeThreshold`：`MaxSessions=3, MaxRecords=1000, 3 samples` →
  **恰好 1 个会话、0 次 rekey 事件、9 条记录**（3 事务 × 3）✅
- `TestRekeyStillFiresWhenDue`：同一设备、阈值可达 → **3 个会话、3 个不同 SID** ✅

---

## 五、rotation failure 如何保证旧 generation 仍可用

**M4 的真实缺陷**：先删旧密钥文件、再写新文件。若中途失败，
设备已无可用私钥而注册表仍指向旧世代。

**修正（采用推荐的 key-id/versioned 布局）**：

```
keys/<device>/<sign-key-id>.key
keys/<device>/<agreement-key-id>.key
```

流程改为：

1. 写入并验证**两把新密钥文件**（按 key_id 命名，**不动旧文件**）；
2. `ApplyRotation` **原子**提交注册表到新世代；
3. **之后** best-effort 退役旧文件。

由此：新文件写失败 → 旧世代完整可用；注册表提交失败 → 旧世代完整可用（新文件只是闲置）；
注册表提交成功但旧文件未删 → 旧 key_id 已被注册表拒绝，残留文件只是待清理。
加载时优先读 key_id 命名文件，回退到旧命名 —— **旧目录无需迁移**。

**"原子"的准确范围**（文档已同步收窄）：注册表提交是原子的；
**"私钥文件 + 注册表"的整个轮换不是 crash-atomic**，它是 **failure-safe**。

新增测试（`internal/identity/rotation_failure_test.go`）：
- `TestRotationFailureLeavesOldGenerationUsable`（第二把密钥写失败）✅
- `TestRegistryFailureLeavesOldGenerationUsable`（注册表提交失败）✅
- `TestSuccessfulRotationMakesOldGenerationUnusable`（成功后旧 id 被拒、旧文件退役后不可恢复）✅

---

## 六、secctl 新增了哪些命令

| 命令 | 作用 |
|---|---|
| `secctl status` | 重写：`milestone=M4`、`suite1=implemented`、`suite2=implemented`、`lifecycle=implemented`、`fleet=implemented/tested`、`benchmark=not_implemented_M5`、`race=environment_blocked` |
| `secctl lifecycle -trust PATH` | 列出每个身份及其生命周期状态（不含任何密钥材料） |
| `secctl revoke -trust PATH -id D005` | 吊销（持久化，网关 reload 后生效；reconnect 无法恢复） |
| `secctl retire -trust PATH -id D005` | 退役（与 REVOKED 刻意区分） |
| `secctl rotate -trust PATH -keys DIR -id D007` | 完整轮换：生成新签名+协商密钥、世代 +1、新 key_id、原子提交注册表、按 failure-safe 顺序退役旧文件 |

`cmd/secctl/main.go` 里"M4 tasks not implemented"的旧注释已删除。
**所有命令不打印私钥或共享秘密** —— `TestCLINoPrivateMaterialInLifecycleOutput`
会把每条命令的输出与磁盘上的每个私钥标量逐字节比对。

CLI 端到端联调（临时 runtime）：`init-demo → status → lifecycle → rotate D002 →
revoke D003 → lifecycle`，记录在 `results/raw/m4_1_cli_lifecycle.jsonl`；
另有 3 个 Go 测试直接驱动同一代码路径。

---

## 七、20×3 的 verifier 最终数字

`scripts/verify_fleet_events.py` 新增 `--expect-sessions`，全部**精确**（不再只是下限）：

```
result                       : PASS
expected_devices             : 20
devices_with_hybrid          : 20
hybrid_sessions_total        : 60
sessions_per_device_expected : 3
unique_sid_count             : 60
telemetry_expected           : 180
telemetry_accepted           : 180
controls_confirmed           : 180
kem_used_count               : 20
selected_suite               : 2
checks                       : 17 / 17 ok
```

每台设备**恰好** 3 个 established session、9 条遥测、9 条确认控制。
`PACKAGE_INFO` 中 13/13 与 12/12 的不一致也已修正（本包不再引用旧数字）。

**19 good + 1 bad**（独立运行）：19 台、57 会话、171 遥测/控制，D010 失败 ✅

---

## 八、测试数量与失败数

```text
packages        : 15 total, 10 ok, 0 failed, 5 without tests
top-level tests : pass=209 fail=0 skip=0     （全量 ./...）
subtests        : pass=190 fail=0 skip=0
negative cases  : 37
m4 cases        : 10
M4.1 targeted   : 125 top-level / 114 subtests, 0 fail（handshake+app+identity+secctl）
go test exit    : 0
VERDICT: PASS
```

**失败数：0。**

**本轮被测试抓出的真实缺陷（均已修）**：
1. `Lookup` 无锁读共享 map（数据竞争）；
2. `Unregister` 无条件删除 → 被取代会话的清理可能**删掉新会话**（导致 20×3 偶发失败）；
   改为 `UnregisterIfCurrent`（只有持有者可移除自己）；
3. `Register` 拒绝重连 → 改为**新会话取代旧会话**（每设备仍至多一个活跃会话）；
4. pre-M4 设备 `KeyGeneration=0`，首次轮换派生出已占用的 `-SIG-1` id → `InitDemo` 登记为世代 1；
5. 换钥提前停止时 `err==nil` 落入 default 分支 → 空指针 panic → 新增 `rekey-due` 终态；
6. fleet 阈值与样本数关系未文档化（阈值 4 会在 3 样本前截断会话）。

---

## 九、race 状态

**`ENVIRONMENT_BLOCKED` —— 未执行。**
本轮新增的并发测试（registry 并发、吊销/轮换并发）是**功能性**测试，
**不是** race detector 结果，不得如此汇报。SM2 独立对照仍缺。

---

## 十、其余

- Suite 1 / Suite 2 单设备 E2E 均回归 PASS；
- `cmd/bench` 继续 fail-closed；
- 未生成正式性能数据，未做 1000 次握手；
- 文档同步：README / IMPLEMENTATION_STATUS / LIFECYCLE / secctl status /
  VERSION(`0.6.1-m4.1`) / TEST_MATRIX —— 已清除"Suite 2 未实现""hybrid 拒绝启动"
  等过时表述，并修正 LIFECYCLE 中"轮换只替换一类密钥"与实现（同时替换两类）的矛盾。

**完成 M4.1，停止，不进入 M5。**
