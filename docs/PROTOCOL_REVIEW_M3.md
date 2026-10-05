# M3 协议冻结：Suite 2（SM2 + ML-KEM-768 混合）

**状态：这是 M2/M2.1 之上的增量，不是推倒重建。**

本文档是 M3 的**审查记录与冻结说明**；`docs/PROTOCOL_REVIEW_M2.md` 与
`docs/PROTOCOL_DRAFT.md` 仍然有效，本文只描述**新增部分**。

---

## 0. 明确保留的内容（未改动）

| 保留项 | 说明 |
|---|---|
| H1–H4 四消息结构 | 设备=initiator，网关=responder，消息顺序与 `wire.Kind` 不变 |
| 双向 SM2 身份签名（SigA / SigB） | 含 UID 与 ZA/ZB 绑定 |
| 完整 SM2 密钥协商 | `SM2MQV` + `SM2SharedKey`，不是普通 ECDH |
| HKDF-SM3 日程 | 一次 Extract、多次 Expand；T2 用于 Finished 密钥，T4 用于业务密钥 |
| FinishedA / FinishedB | 方向独立，协议级确认值（**不是**标准 S1/S2） |
| SM4-GCM 记录层 | 33B 头作为 AAD，方向专用 nonce 前缀，序号只在认证成功后推进 |
| 身份注册表与 key_id 绑定 | 含签名/协商密钥分离与逐字节公钥比对 |
| M2.1 修补 | 取消贯穿、握手单一总预算、截断分类、严格验收器、三态观测 |
| 能力列表顺序作为认证数据 | 双方**都不得**排序后再参与转录 |

**Suite 1 的线格式字节布局未变**（`EphemeralKEM` 与 `KEMCiphertext` 字段在 M2
就已预留，Suite 1 下为空；M3 只是开始使用它们）。

---

## 1. Suite 2 的实际流程

设备是 initiator。

### H1 ClientHello

设备为**本次握手**生成一个**新的** ML-KEM-768 密钥对：

```text
ekA, dkA = MLKEM768.Generate()      // 每次握手一个新的，绝不复用
```

载荷在原有字段之外增加：

```text
vec  ekA      1184 字节的 ML-KEM-768 封装公钥
```

**Suite 2 下 `ekA` 的规则**：

- 必须是本次握手新生成的 ML-KEM-768 公钥；
- **不得**固定、**不得**缓存跨会话复用、**不得**填零。

响应端可检测的只有**退化情形**（例如全零）；**跨会话复用在响应端不可检测**，
这一点如实记录为局限，不谎称已防御。

### H2 ServerHello

网关先完成**已有的**身份/套件/策略检查，然后：

若选择 Suite 2：

```text
Zpq, kem_ct = MLKEM768.Encapsulate(ekA)      // 真实封装，不是模拟
```

- `kem_ct` 放入 `ServerCore.KEMCiphertext`；
- **SigB 必须认证 H1 与包含 `kem_ct` 的 ServerCore**；
- `kem_ct` 因此是签名覆盖的认证数据。

### 设备收到 H2 后

1. 先执行**已有的**身份 / key_id / 签名检查；
2. 用 `dkA` **真实** Decapsulate `kem_ct`，得到本地 `Zpq`；
3. 双方继续执行**真实** SM2 密钥交换得到 `Zsm2`。

### Suite 2 的 IKM（关键禁令）

```text
IKM_H = E("edgehybrid/v1/ikm", u16(2), "sm2", Zsm2, "mlkem768", Zpq)
```

**必须真实依赖 `Zsm2` 与 `Zpq` 两份秘密。** 明确禁止：

- 只用 `Zsm2`；
- 只用 `Zpq`；
- 把两份秘密 **XOR**；
- 简单拼接后直接截断；
- PQ 结果缺失时**填全零**；
- 复用上次握手的 `Zpq`。

继续沿用现有基于 SM3 的 HKDF Extract/Expand 日程（不改 KDF）。

---

## 2. 协商一致规则：`支持` 与 `选中` 必须分开

这是本轮最容易搞错的地方。

| 情况 | 约束 |
|---|---|
| H1 `offered_suites = [1]` | `ekA` **必须为空** |
| H1 `offered_suites` 含 2（如 `[2,1]`） | `ekA` **必须存在且尺寸为 1184** |
| 最终 `selected_suite = 2` | H2 `kem_ct` **必须存在且尺寸为 1088**；IKM 使用 `Zsm2 + Zpq` |
| 最终 `selected_suite = 1` | H2 `kem_ct` **必须为空**；IKM **只用** `Zsm2` |

### 即使最终选了 Suite 1，H1 里已经发出去的东西也不能删

> 即使最终选择 Suite 1，H1 中已经发送的 `offered_suites` 与 `ekA`
> **仍然属于认证转录的一部分**，**不得**从转录中删除或规范化。

能力列表的顺序**继续**作为认证数据的一部分，**禁止**双方排序后再参与转录。

### Suite 1 下 `Zpq` 的处理

`ikm(SuiteTraditional, Zsm2, Zpq)` 现在**显式拒绝**非空的 `Zpq`（fail closed），
而不是忽略它。理由：忽略一个"本不该存在"的 PQ 输入，会让"这个会话是否用了
后量子"这件事变得依赖于调用方是否忘记传参；显式拒绝把错误变成可检测的。

---

## 3. 防降级（不是只靠 `minimum_mode`）

**`minimum_mode = hybrid` 只是本地策略，不能证明防降级。** 本轮的防降级来自
**三个独立机制叠加**：

| 机制 | 作用 |
|---|---|
| **客户端重算选择** | 客户端用**自己的**配置套件 + 服务端通告列表 + **可信**最低模式，重算应得套件，并与 `selected_suite` 比对。不一致即终止 |
| **能力列表进入转录** | 删除/重排/改写 H1 的能力列表都会改变 H1 字节，从而让 **SigB 验证失败** |
| **`kem_ct` 进入转录** | 改写/删除 `kem_ct` 同理，破坏 SigB 或（重签时）破坏 Finished |

### 要求的攻击场景与预期失败阶段

| # | 攻击 | 预期失败阶段 |
|---|---|---|
| 1 | 删除 H1 中的 Suite 2 能力 | `message_sequence_or_encoding`（客户端重算得 2，服务端给 1） |
| 2 | `[2,1]` 改为 `[1]` | 同上 |
| 3 | `[2,1]` 改成 `[1,2]` | **`signature_authentication`**（结构一致，但 H1 字节变了 → SigB 失败） |
| 4 | H2 `selected_suite` 由 2 改 1 | `message_sequence_or_encoding` |
| 5 | 删除 `ekA`（offered 仍含 2） | `message_sequence_or_encoding` |
| 6 | 删除 `kem_ct`（selected=2） | `message_sequence_or_encoding` |
| 7 | 修改 `ekA` | **`signature_authentication`** |
| 8 | 修改 `kem_ct` | `signature_authentication`；**重签时 → `finished_confirmation`** |
| 9 | 改 suite/version/能力顺序并重编码为合法长度 | `signature_authentication` 或 `finished_confirmation` |

**刻意保证**：并非所有用例都因为"长度错误"或"无法解析"而失败。
场景 3、7、9 都是**合法编码、正确长度**，真正进入签名验证阶段；
场景 8 的重签版本进入 **Finished** 阶段。

---

## 4. ML-KEM 隐式拒绝必须由 Finished 捕获

FIPS 203 的隐式拒绝意味着：**等长非法密文不会返回 error，而是返回另一个
伪随机共享秘密**。因此"解封装没报错"**不是**认证成功的证据。

本轮的证据链：

```text
篡改 kem_ct（等长）
  → 设备真实 Decapsulate 得到 Zpq_device ≠ Zpq_gateway
  → PRK 不同 → Finished 密钥不同
  → FinishedA 不匹配 → 网关在 finished_confirmation 阶段拒绝
```

**测试约束**：

- 用 **test-only 故障注入**（只出现在 `*_test.go` / test fixture）；
- 网关**真实** Encapsulate，注入发生在**签名与转录形成之前**；
- 用**真实网关签名**重签修改后的 ServerCore（所以不会被 SigB 拦住）；
- **不得**使用 mock KEM、手工指定共享秘密、绕过签名、或 production CLI 调试开关。
- 双方转录必须相同（这正是隐式拒绝危险的地方：转录一致，秘密却不同）。

---

## 5. 兼容与降级策略

必须同时支持：

- **A. 新设备**：能力 `[2,1]`，默认优先 Suite 2；
- **B. 明确登记的 legacy 设备**：能力 `[1]`，可继续使用 Suite 1。

### 禁止自动回退

```text
禁止：
  try hybrid
  失败
  自动 reconnect traditional
```

一旦开始混合握手，**任何**失败（验证失败、KEM 失败、Finished 失败、转录不一致）
都让**这次连接直接失败**。

传统连接必须**从一开始**就是由**本地可信迁移策略明确允许**的 Suite 1 连接
（即 `minimum_mode=traditional` 且 `allow_legacy=true` 的可信配置，或能力列表
本身只有 `[1]`）。

本轮以测试固定：失败的混合握手**只发出一次 H1**，不存在第二次尝试。

---

## 6. 开放 Suite 2 的条件（过程门，不是代码门）

`suiteReadiness[SuiteHybrid]` **保持 false**，直到以下**全部**满足：

1. 真实 ML-KEM-768 已进入握手；
2. Suite 2 E2E 成功；
3. `Zsm2` 与 `Zpq` 双依赖测试成功（测试 A / B）；
4. KEM 隐式拒绝 → Finished 测试成功；
5. wire 级防降级测试成功；
6. no automatic fallback 测试成功；
7. Suite 1 回归成功。

满足后开放，使**默认** `configs/gateway.json` / `device.json`
（`minimum_mode=hybrid`）**真正运行 Suite 2**。

**不得**通过把默认配置改成 traditional 来制造 PASS。

---

## 7. 本轮不做

M4 密钥生命周期、20 台安全设备、长期密钥轮换、在线吊销传播、
M5 benchmark、1000 次握手性能实验、GUI/Web、数据库、会话恢复、0-RTT。
`cmd/bench` 继续 fail-closed。

---

## 8. 历史验证缺口继续保留

1. **Windows 本机 `-race` 环境阻塞**；
2. **SM2 密钥交换缺独立实现对照**。

若有合法可用环境可并行补测，但**不得**修改系统 DLL、关闭校验、
擅自上传源码到远端 CI，也**不得**因为 M3 完成就把这两项标成通过。

---

## 9. 不声称的事项

- 这不是标准协议，没有形式化安全证明，没有经过外部审计。
- **本轮只是把 SM2 与 ML-KEM-768 两份秘密都纳入建钥**；
  这**不等于**"整个系统已抗量子"——**SM2 身份认证不是后量子身份认证**。
- Signed/Finished 通过的语义不变：**只证明双方对同一转录一致**，
  不证明对端公钥诚实生成、也不证明其随机性质量。
- 隐式拒绝测试证明的是"篡改的密文会被 Finished 捕获"，
  **不是**"ML-KEM 实现已被独立验证"。
