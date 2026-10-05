# M3 里程碑报告：Suite 2（SM2 + ML-KEM-768）混合套件

**项目**：A1-31 园区边缘设备国密与后量子混合安全通信软件设计实现
**本轮范围**：只做 M3 —— 完整实现 Suite 2、防降级与对应负面测试
**本轮明确不做**：M4 密钥生命周期、20 台安全设备、长期密钥轮换、在线吊销传播、
M5 benchmark、1000 次握手性能实验、GUI/Web、数据库、会话恢复、0-RTT

**run_id**：`m3-20260925T014447Z-final`
**源码清单**：`results/raw/m3_source_manifest.sha256`（110 个文件，
清单自身 SHA-256 `f32a55e49dc90cf922080b0eba0614f69a0a0bdcecb1e6efc8e4733c7bccf854`）
**核验机器**：Windows amd64，`go1.27.1`，MinGW-W64 gcc 8.1.0，OpenSSL 3.5.7

**验收等级**：`M3_HYBRID_VERIFIED_PENDING`（见 `results/m3_readiness.json`）

> **会话标识纪律**：SID 由双方独立派生、每次运行都是新值。本报告**不**复制具体 SID，
> 一律引用 `results/raw/*_summary.json` 的 `session_id` 字段，避免报告与原始证据漂移。

> **一句话**：Suite 2 真实跑通了 —— 真实 ML-KEM-768 进入握手、真实 TCP 端到端通过、
> 结果真实依赖两份秘密、隐式拒绝被 Finished 捕获、9 个降级场景全部被拒、无自动回退。
> 但 **race 仍未执行**、**SM2 密钥交换仍缺独立对照**，
> 且**这仍不等于"系统已抗量子"**（SM2 身份认证不是后量子认证）。

---

## 一、Suite 2 是否真实跑通

**是。** 三条独立证据：

1. **真实 ML-KEM-768 进入握手**（不是模拟、不是填零、不是复用）
   - `TestHybridKEMFieldSizes`：H1 `ekA` = **1184** 字节，H2 `kem_ct` = **1088** 字节（FIPS 203 的 ML-KEM-768 尺寸）
   - `TestHybridEphemeralKEMIsFreshPerHandshake`：三次握手的 `ekA` 字节**互不相同且非全零**
2. **Suite 2 端到端通过**（真实 TCP，两端严格验收）
   - `scripts/m2-e2e.sh MODE=hybrid` → **`VERDICT: PASS`，26/26 项检查 ok**
   - 双方协商 **suite 2**、双方 `kem_used=true`、完整 32 位十六进制 SID 一致
   - 本次会话标识见 `results/raw/m3_hybrid_e2e_summary.json` 的 `session_id`；
     **SID 是每次运行随机的，不得手工复制进报告** —— 报告只引用原始 summary
3. **双方独立派生同一个会话标识**
   - `TestHybridHandshakeEstablishesMatchingSessions`：设备与网关各自算出**相同的 SID**，
     且记录通道双向可用。若任何一份秘密不一致，SID 就会不同。

### IKM 真实依赖两份秘密（禁令逐条对应）

```text
IKM_H = E("edgehybrid/v1/ikm", u16(2), "sm2", Zsm2, "mlkem768", Zpq)
```

| 禁止项 | 如何防止 |
|---|---|
| 只用 `Zsm2` | `ikm(SuiteHybrid, ...)` 要求 `len(Zpq) == 32`，否则 fail closed |
| 只用 `Zpq` | 同上：也要求 `len(Zsm2) == 32` |
| XOR 两份秘密 | `TestHybridIsNotAnXOROfTheHalves`：IKM 长度严格大于两份秘密之和，排除了折叠/截断 |
| 拼接后截断 | 同一测试 + `TestHybridIKMCarriesBothHalves`：两个带标签的原始秘密都**完整出现在** IKM 中 |
| PQ 缺失时填零 | `TestHybridZpqMissingFailsClosed`：`nil`、空、**全零** 三种情况全部拒绝 |
| 复用上次 `Zpq` | `TestHybridSessionsAreIndependent`：两次独立混合握手 SID 不同；`ekA` 每次新生成 |

---

## 二、开放 Suite 2 的条件：逐条核对

条件满足前 `suiteReadiness[SuiteHybrid]` **一直是 false**，
**没有**为了测试通过而提前翻牌。

| # | 条件 | 状态 | 证据 |
|---|---|---|---|
| 1 | 真实 ML-KEM-768 已进入握手 | ✅ | `internal/handshake/hybrid_test.go` |
| 2 | Suite 2 E2E 成功 | ✅ | `MODE=hybrid` → PASS，26/26 |
| 3 | Zsm2 与 Zpq 双依赖测试成功 | ✅ | `keyschedule_hybrid_test.go` 测试 A/B（+C/D/E） |
| 4 | KEM 隐式拒绝 → Finished 测试成功 | ✅ | `kem_implicit_test.go`，实测 `finished_confirmation` |
| 5 | wire 级防降级测试成功 | ✅ | `downgrade_test.go`，9/9 场景 |
| 6 | no automatic fallback 测试成功 | ✅ | `no_fallback_test.go`，3/3 |
| 7 | Suite 1 回归成功 | ✅ | 全量测试 + `MODE=traditional` → PASS，26/26 |

**开放后**：默认 `configs/gateway.json` / `device.json`（`minimum_mode=hybrid`）
**真正运行 Suite 2** —— 实测网关不再拒绝启动，而是发出 `listen/ready`。

**默认配置没有被改成 traditional 来制造 PASS**，这一点由
`internal/app/service_test.go::TestShippedDefaultConfigurationsSelectSuiteTwo` 固定：
它直接读取仓库里那两个文件，断言 `minimum_mode` 仍是 `hybrid` 且能力列表包含套件 2。

---

## 三、防降级：哪些测试通过，在哪个阶段被拒

**`minimum_mode = hybrid` 只是本地策略，不能证明防降级。** 本轮的防降级来自三个机制叠加：

| 机制 | 作用 |
|---|---|
| 客户端重算选择 | 客户端用自己的配置 + 服务端通告 + **可信**最低模式，重算应得套件并比对 |
| 能力列表进入转录 | 改写 H1 的能力列表会改变 H1 字节 → **SigB 失败** |
| `kem_ct` 进入转录 | 改写/删除 `kem_ct` → SigB 失败；重签时 → **Finished 失败** |

### 9 个降级场景的实测结果（`results/raw/m3_downgrade_cases.jsonl`）

| # | 场景 | 失败阶段 |
|---|---|---|
| 1 | 删除 H1 中 Suite 2 能力（`ekA` 留下，不一致） | `message_sequence_or_encoding` |
| 2 | `[2,1]` 改为 `[1]`（并删 `ekA`，一致降级） | `message_sequence_or_encoding` |
| 3 | `[2,1]` 改成 `[1,2]`（同集合、不同顺序） | **`signature_authentication`** |
| 4 | H2 `selected_suite` 由 2 改 1 | `message_sequence_or_encoding` |
| 5 | 删除 `ekA`（仍声明含 2） | `message_sequence_or_encoding` |
| 6 | 删除 `kem_ct`（仍选中 2） | `message_sequence_or_encoding` |
| 7 | 修改 `ekA`（等长） | **`signature_authentication`** |
| 8 | 修改 `kem_ct`（等长） | **`signature_authentication`** |
| 9 | 改 H2 `supported_suites` 顺序并重编码 | **`signature_authentication`** |

**刻意保证**：不是所有用例都因"长度错误"或"无法解析"失败。
场景 3、7、9 都是**合法编码、正确长度**，真正进入**签名/转录绑定**阶段。

场景 1 与 2 是一对有意义的对照：
- 场景 1 是**不一致**的降级声明（删了能力却留下 `ekA`）→ 响应端用套件条件字段规则拦住；
- 场景 2 是**一致**的降级（能力和密钥都改成套件 1）→ 服务端合法地选了 1，
  由**客户端重算**发现"我本该得到 2"，这才是防降级的核心机制。

---

## 四、隐式拒绝是否最终由 Finished 捕获

**是，实测失败阶段就是 `finished_confirmation`。**

证据链（`results/raw/m3_downgrade_cases.jsonl` 第 10 条）：

```text
scenario      : ML-KEM implicit rejection is caught by Finished
actual_stage  : finished_confirmation
detail        : handshake: Finished confirmation mismatch: device Finished did not match
mutation      : kem_ct 在真实 Encapsulate 之后、ServerCore/签名/转录形成之前
                被替换为等长非法密文
```

三层证据：

1. **单元级**（`TestMLKEMImplicitRejectionChangesSharedSecret`）：
   等长篡改的密文用**真实** Decapsulate → **没有 error**，但得到的共享秘密
   **与封装端不同**（两者都是 32 字节）。日志确认："implicit rejection confirmed"。
2. **握手级**（`TestImplicitRejectionIsCaughtByFinished`）：
   网关保留真实 `Zpq`，设备拿到隐式拒绝的 `Zpq` → 网关在 `finished_confirmation` 拒绝。
3. **转录一致**（`TestImplicitRejectionKeepsTranscriptsIdentical`）：
   失败类型是 **Finished 不匹配**，而**不是**签名或转录错误 ——
   这证明两边看到的字节**完全相同**，只有派生秘密不同。这正是隐式拒绝危险的地方。

### 关于注入点的一个必须说明的设计决定

故障注入点在 **server.go 里、真实 Encapsulate 之后、ServerCore/签名/转录形成之前**
（这正是任务书要求的时机），而**不是**在 wire 上用中继替换并重签。

原因：**一端认证它"发出去"的字节，对端认证它"收到"的字节。**
如果只在中继上改密文再重签，这两个字节串就不一样了，握手会在**下一个签名**处失败，
根本走不到 Finished。要复现真实攻击（两边对每个字节都真的一致），
注入必须发生在转录形成之前。这个推理我一开始做错了，是被测试抓出来的。

测试专用性：注入点 `kemCiphertextOverride` 与套件门 `suiteImplemented` 的覆盖
**都只写在 `_test.go` 文件里**，生产二进制中不存在，也没有任何开关、环境变量、
配置项或 CLI 参数能在生产里打开它们。

---

## 五、Suite 1 是否回归通过

**是。**

| 回归项 | 结果 |
|---|---|
| Suite 1 正常握手 | ✅ `TestHandshakeEstablishesMatchingSessions` |
| Suite 1 负面测试 | ✅ 全部保留并通过 |
| Suite 1 SM4-GCM 记录层 | ✅ `record_sm4_test.go` + `channel_test.go` |
| Suite 1 真实 TCP E2E | ✅ `MODE=traditional` → PASS，26/26（会话标识见 `results/raw/m2_e2e_summary.json` 的 `session_id`） |
| M2.1 取消 / 超时 / 截断 / 事件验收 / 三态观测 | ✅ 全部保留并通过 |

M3 只**新增** Suite 2 所需的数据与逻辑：
`EphemeralKEM`、`KEMCiphertext` 字段在 M2 就已预留（Suite 1 下为空），
H1–H4 结构与所有既有检查**未改动**。

### 一处契约变更（有意，且更强）

`ikm(SuiteTraditional, Zsm2, Zpq)` 过去会**忽略**传入的 PQ 秘密，现在**显式拒绝**。
理由（对应任务书"测试 E"）：忽略一个"本不该存在"的参数，会让
"这个会话是否依赖 KEM"取决于调用方是否忘记传 nil；拒绝把它变成可检测的失败。
旧测试 `TestIKMBindsTheSuite` 已同步更新，并新增
`TestTraditionalRejectsAPQSecret` 专门固定它。

---

## 六、测试实际数量

由 `go test -json` 自动统计（`scripts/summarize-go-test-json.py`），非手写：

```text
packages        : 14 total, 9 ok, 0 failed, 5 without tests
top-level tests : pass=180 fail=0 skip=0
subtests        : pass=186 fail=0 skip=0
negative cases  : 37
wire-level attack cases: 10
VERDICT: PASS (exit 0) — evidence complete, expected packages covered, go test exit code 0
```

37 条负面用例的**失败阶段分布**（这正是"不是只剩长度校验"的证据）：

| 阶段 | 条数 |
|---|---|
| `message_sequence_or_encoding` | 16 |
| `signature_authentication` | 10 |
| `identity_or_key_binding` | 7 |
| `finished_confirmation` | 3 |
| `suite_policy` | 1 |

其中 10 条带了 `target` / `mutation` 字段（9 个降级场景 + 1 个隐式拒绝），
单独落在 `results/raw/m3_downgrade_cases.jsonl`。

**汇总器不硬编码任何数量**（M2.1 是 158/175，M3 是 180/186），判据不受影响。

---

## 七、本轮修改的源码

| 文件 | 改动 |
|---|---|
| `internal/cryptosuite/interfaces.go` | `suiteReadiness[SuiteHybrid]` 由 false 翻为 **true**（仅在七项条件全部满足后）；`ImplementedSuites()` 改为 `[2,1]` |
| `internal/handshake/codec.go` | 新增 `offersHybrid` / `checkClientKEMField` / `checkServerKEMField` / `allZero`（套件条件字段规则） |
| `internal/handshake/keyschedule.go` | `ikm`：Suite 1 显式拒绝 PQ 秘密；Suite 2 拒绝缺失/长度错/全零的 `Zpq` |
| `internal/handshake/client.go` | 若声明含套件 2 则生成**每次握手新的** ML-KEM 密钥并放入 `ekA`；按选中套件真实 Decapsulate；改为用 `checkServerKEMField`；新增"选中 2 但未声明过 2"的拒绝 |
| `internal/handshake/server.go` | 收到 H1 时按**声明**校验 `ekA`；选中 2 时真实 Encapsulate 并把 `kem_ct` 放进 ServerCore（进入 SigB 与转录）；发送前按**选中**校验；IKM 传入 `Zpq`；新增 test-only 注入点与 `requireSuite` |
| `internal/handshake/params.go` | `suiteImplemented` / `requireSuite` 门控改造（覆盖只来自 `_test.go`） |
| `internal/app/service.go` | 事件新增 `KEMUsed`（公开元数据，非密钥材料） |
| 测试 | `hybrid_test.go`、`keyschedule_hybrid_test.go`、`downgrade_test.go`、`kem_implicit_test.go`、`no_fallback_test.go`、`suite_gate_test.go`、`kem_implicit_override_test.go` |
| 脚本 | `verify_e2e_events.py` 增加 `--expect-suite` / `--expect-kem`；`m2-e2e.sh` 增加 `MODE=traditional\|hybrid`；`summarize-go-test-json.py` 增加 `--downgrades` 并修正 M2NEG 字段映射 |
| 测试辅助 | `testutil_test.go`：`testStoreMin`、`hybrid*Config`、`hybridHandshakePair`、`runHybridHandshakeAs`、`negObservation` 增加 `business_accepted/target/mutation` |
| 文档 | 新建 `docs/PROTOCOL_REVIEW_M3.md`；更新 README / IMPLEMENTATION_STATUS / TEST_MATRIX / THREAT_MODEL |

---

## 八、本轮被测试抓出的额外缺陷

| # | 问题 | 谁抓到的 |
|---|---|---|
| 1 | M2NEG 发射端增加 `business_accepted`/`target`/`mutation` 后，**汇总器正则没同步** → 负面用例提取**静默变成 0 条**，而运行仍报 PASS | 空证据文件 + 新加的自测守卫（已把该回归固定为一条用例） |
| 2 | 隐式拒绝测试最初在**中继**上改密文并重签，结果在**签名**处失败，走不到 Finished | 测试自身（详见第四节） |
| 3 | `hybridHandshakePairOverRelay` 错误地断言"两边必须有一个失败"，导致三个**观察型**用例误报 | 全量运行时暴露 |
| 4 | 降级场景 2 的语义含糊（服务端拒了却断言客户端） | 复查用例的 `detail` 时发现 |
| 5 | 套件门控测试（4 个）编码了"套件 2 未实现"，开放后前提失效 | 开放后的全量运行 |

第 1 条尤其值得记：它正是"验收器把缺失证据当成功"的同类问题——
**证据为空时不能静默通过**。现在自测里有一条专门的守卫。

---

## 九、未执行 / 未闭环

| 项 | 状态 |
|---|---|
| `go test -race -count=1 ./...` | **未执行**。环境阻塞：纯 Go（无 cgo）三行程序 + 全新 `GOCACHE` 复现 `exit status 0xc0000139` |
| SM2 密钥交换独立实现对照 | **未获得**。OpenSSL 命令行不暴露 SM2MQV |
| `ekA` 跨会话复用检测 | **响应端不可检测**（只能拦全零这类退化情形）。已如实记为局限，不谎称已防御 |
| M3 之外的功能 | 未开始（见报告头） |

**环境阻止 race ≠ 项目不存在竞态。**
本轮为并发路径保留了 M2.1 的针对性测试（取消、Close、Send/Receive 交错、部分写、goroutine 泄漏），
但这些**不能**替代竞态检测器。

`cmd/bench` 继续 fail-closed。

---

## 十、声明

- 本轮**未**记录或提交任何私钥（SM2 / ML-KEM）、会话密钥、随机种子、访问令牌或业务数据。
- **未**关闭任何校验（模块校验、cgo、race、并发测试均未关闭）。
- **未**因为 M3 完成就把 race 或 SM2 独立对照标成通过。
- 所有数字可由 `results/raw/` 下的原始文件重算。
- **Suite 2 真实存在 ≠ 系统已抗量子**：SM2 身份认证不是后量子身份认证；
  本轮只是让会话密钥同时依赖 SM2 与 ML-KEM-768 两份秘密。
- 隐式拒绝测试证明的是"篡改的密文会被 Finished 捕获"，
  **不是**"ML-KEM 实现已被独立验证"。

**完成 M3 后停止，不进入 M4。**
