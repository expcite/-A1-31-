# M2 协议冻结与审查

**状态：这是本项目的自定义协议设计，已按实现冻结。它不是标准协议，没有形式化安全证明，也没有经过外部审计。**

本文档审查并冻结 `docs/PROTOCOL_DRAFT.md` 的四消息流程，使文档与代码一致。
凡代码与草案不一致之处，以本文档为准；草案已同步修订。

---

## 0. 命名（避免与里程碑混淆）

草案用 `M1..M4` 表示四条消息，与里程碑 `M1/M2/M3/M4` 重名，极易误读。现已冻结：

| 本命名 | 草案旧名 | 含义 |
|---|---|---|
| `H1` | M1 | ClientHello，设备 → 网关 |
| `H2` | M2 | ServerHello + 网关签名，网关 → 设备 |
| `H3` | M3 | ClientAuth + 设备签名 + FinishedA，设备 → 网关 |
| `H4` | M4 | ServerFinished，网关 → 设备 |

代码中的 `wire.Kind` 取值不变（`ClientHello=1`、`ServerHello=2`、`ClientAuth=3`、
`ServerFinished=4`、`ProtectedRecord=16`）。

**每条消息的“线编码”定义**：`Hx` 指该消息在网络上**实际的完整字节**，
即 `uint32 大端总长 || version(1) || kind(1) || payload`（含 4 字节外层长度前缀）。
发送端把**自己实际写出的字节**交给转录，接收端把**自己实际读到的字节**交给转录
（`wire.Encode` 的返回值与 `wire.ReadRaw` 的第二个返回值），
而不是对解析后的结构重新编码。规范编码满足 `encode(parse(x)) == x`，
并由 `TestCanonicalCodecRoundTripsExactly` 与
`TestOffCurveEphemeralRejectedDespiteValidSignature` 中的断言固定。

---

## 1. 信任与套件

- 可信登记条目绑定：`id`、`role`、`sm2_user_id`、签名公钥及其 `key_id`、
  **独立的**静态协商公钥及其 `key_id`、最低模式、吊销状态、修订号。
- **握手消息里携带的公钥永远不会被采纳。** 对端公钥必须与登记表中该身份的
  记录**逐字节一致**，否则握手终止（`ErrPeerIdentity`）。
- 签名密钥与协商密钥必须是**不同的密钥**。两者都是 65 字节未压缩点，
  **编码形状相同**，因此登记表在加载时显式比较二者并拒绝复用。
- 套件：
  - **套件 1（传统）**：SM2 完整密钥协商 + 双向 SM2 签名 + HKDF-SM3 + SM4-GCM。**本轮已实现。**
  - **套件 2（混合）**：同一框架，建钥再加入 ML-KEM-768 共享结果。**本轮未实现，且被明确拒绝。**
- **本轮不宣告混合能力。** 若配置声明的套件列表里含有套件 2，
  端点**拒绝启动**（`ErrUnsupported`），而不是悄悄降级到套件 1。
  默认的 `configs/gateway.json`、`configs/device.json` 正是这种情况，
  因此它们仍然明确拒绝运行。

---

## 2. 编码（冻结）

载荷编码器为 `internal/handshake/codec.go`。规则：

- 所有变长字段前有 **4 字节大端长度**；定长字段（nonce、点）也带长度前缀，
  但解析时**必须等于规定长度**。
- 套件列表：`u16 个数` + 每个 `u16`。个数 1..2，不允许 0、不允许重复、
  不允许未知套件号。
- 身份与 key_id：1..64 字节 `[A-Za-z0-9_-]`（`identity.ValidName`）。
  该字符集**排除** `.`、`/`、`\` 与空白，因此来自网络的标识符
  不可能被拼进文件路径而逃出密钥目录。
- **拒绝**：未知字段、重复字段、缺失字段、多余尾随字节、超出范围的取值。
  实现方式是“读到末尾必须恰好归零”（`reader.done()`）。
- 公钥字段**只做长度/编码层面的边界检查**；是否在曲线上由密码层判断。
  这个分层是有意的（编解码器不知道最终选中哪个套件），
  由 `TestCodecBoundsButDoesNotJudgeTheKEMField` 与
  `TestOffCurveEphemeralRejectedDespiteValidSignature` 两侧共同固定。

### 有序能力列表不得被“规范化”

发送端按**本地配置的声明顺序**写出能力列表，接收端按**收到的字节**处理。
任何一方都**不得**对列表排序后再参与转录，因为那会消除被认证的差异。

---

## 3. 四消息流程（冻结）

```text
H1 设备 → 网关：ClientHello
H2 网关 → 设备：ServerCore || SigB
H3 设备 → 网关：ClientCore || SigA || FinishedA
H4 网关 → 设备：FinishedB
```

### H1 ClientHello 载荷

```text
u8   version = 1
u8   role = 1 (initiator)
u16  套件个数 + u16...        有序支持列表
str  initiator_id
str  initiator_sign_key_id
str  initiator_agreement_key_id
str  responder_id             设备期望的网关身份
vec  Na                       32 字节
vec  Ra                       65 字节（SM2 临时公钥，未压缩）
vec  ekA                      本轮必须为空；套件 1 不得携带 KEM 公钥
```

### H2 ServerCore（`SigB` 签名覆盖它，但不含签名自身）

```text
u8   version = 1
u8   role = 2 (responder)
u16  selected_suite
u16  套件个数 + u16...        网关的有序支持列表
str  responder_id
str  responder_sign_key_id
str  responder_agreement_key_id
str  initiator_id
vec  Nb                       32 字节
vec  Rb                       65 字节
vec  kem_ct                   本轮必须为空
```

```text
SigB = SM2.Sign(skB_sign, uidB, E("edgehybrid/v1/server-auth", H1, ServerCore))
```

**无循环依赖**：`SigB` 覆盖 `ServerCore` 的**编码字节**，签名本身不在其中。

### H3 ClientCore / SigA / FinishedA

```text
u8   version = 1
u8   role = 1
u16  selected_suite
str  initiator_id
str  initiator_sign_key_id
str  initiator_agreement_key_id
```

```text
SigA = SM2.Sign(skA_sign, uidA, E("edgehybrid/v1/client-auth", H1, H2, ClientCore))
```

```text
FinishedA = HMAC-SM3(KfinA,
              E("edgehybrid/v1/confirm-A",
                H(E("edgehybrid/v1/t3core", H1, H2, ClientCore, SigA))))
```

`ClientCore` 与 `SigA` 都参与，但 `FinishedA` **不含自身**。

### H4 ServerFinished

```text
载荷 = FinishedB（32 字节）
```

```text
FinishedB = HMAC-SM3(KfinB,
              E("edgehybrid/v1/confirm-B",
                H(E("edgehybrid/v1/t3", H1, H2, H3))))
```

`H3` 是**完整线编码**，因此 `FinishedB` 也隐含覆盖了 `FinishedA`。

---

## 4. 密钥日程（冻结，无循环依赖）

`H` 为 SM3。HKDF 按 RFC 5869 的 extract/expand 结构实例化（HMAC-SM3）。

```text
Zsm2 = 完整 SM2 协商输出的 32 字节结果
Zpq  = ML-KEM-768 的 32 字节共享结果（仅混合模式；本轮不可达）

IKM_T = E("edgehybrid/v1/ikm", u16(1), "sm2", Zsm2)
IKM_H = E("edgehybrid/v1/ikm", u16(2), "sm2", Zsm2, "mlkem768", Zpq)
salt  = H(E("edgehybrid/v1/salt", Na, Nb))
PRK   = HKDF-Extract-SM3(salt, 选定模式的 IKM)      <-- 只 Extract 一次

T2    = H(E("edgehybrid/v1/t2", H1, H2))

KfinA = HKDF-Expand-SM3(PRK, E("edgehybrid/v1/finished-A", T2, u16(32)), 32)
KfinB = HKDF-Expand-SM3(PRK, E("edgehybrid/v1/finished-B", T2, u16(32)), 32)

T4    = H(E("edgehybrid/v1/t4", H1, H2, H3, H4))

K_AtoB = Expand(PRK, E("edgehybrid/v1/app-AtoB", T4, u16(16)), 16)
K_BtoA = Expand(PRK, E("edgehybrid/v1/app-BtoA", T4, u16(16)), 16)
P_AtoB = Expand(PRK, E("edgehybrid/v1/iv-AtoB",   T4, u16(4)),   4)
P_BtoA = Expand(PRK, E("edgehybrid/v1/iv-BtoA",   T4, u16(4)),   4)
SID    = Expand(PRK, E("edgehybrid/v1/session-id",T4, u16(16)), 16)
```

### 关于 HKDF 的一处必须写清的细节

`cryptosuite.HKDFSM3` 是**一次 Extract+Expand** 的组合函数。
本日程需要**一次 Extract、多次 Expand**，因此生产代码只调用
`HKDFExtractSM3` 一次得到 PRK，之后全部使用 `HKDFExpandSM3`。
把 PRK 再交给组合函数会**悄悄重新 Extract 一次**，从而与对端派生出不同的密钥。
详见 `internal/handshake/keyschedule.go` 顶部注释。

### 为什么 T2 与 T4 分开

`KfinA/KfinB` 只依赖 `T2`（到 `H2` 为止），业务密钥依赖 `T4`（到 `H4` 为止）。
这样 `Finished` 不会依赖它自己要认证的消息，业务密钥也不会在握手写完之前存在。

### 派生长度进入 info

`Expand` 把请求长度（`u16`）编码进 `info`，因此同一标签下 16 字节与 32 字节
的扩展是**两个不同的值**，而不是截断关系
（`TestExpandEncodesTheRequestedLength`）。

---

## 5. 角色、身份与 SM2 确认配置

- **设备 = SM2 initiator；网关 = SM2 responder。**
- 角色映射按**本地库源码**解释（见 `docs/M1_ERRATA.md` 第 E6 节），
  不照搬有歧义的注释：
  - `(*PrivateKey).SM2MQV(...)` 的接收者是**本方静态私钥**；
  - `(*PublicKey).SM2SharedKey(isResponder, ...)` 的接收者是**上一步的 MQV 点**；
  - 设备传 `isResponder=false`，网关传 `isResponder=true`。
- 角色错配会改变 ZA/ZB 顺序，从而改变派生密钥
  （`TestSM2RoleSwapChangesDerivedKey`）。

### 标准 SM2 显式确认字段：**本轮未使用**

GB/T 32918.3 定义了可选的显式确认值 S1/S2。**本轮不实现 S1/S2**，
只实现“核心协商 + 协议级 Finished”。

因此：

- **不得**把本项目的 `FinishedA/FinishedB` 称为“标准 S1/S2”。
- 它们是在 SM2 协商结果之上的**本项目设计**的确认值，
  由 HKDF-SM3 派生的独立完成密钥计算。
- 一致性局限：本实现与**同样实现了标准 S1/S2 的第三方 SM2 实现**
  无法直接在确认值上互操作；能互操作的只是**协商出的 Zsm2 本身**。
- 该选择**不可由网络开关跳过**：`FinishedA/FinishedB` 是四消息流程的组成部分，
  缺失或错误一律终止握手。

---

## 6. 记录层（冻结）

```text
头部 33 字节：version(1) || suite(2) || content_type(1) || direction(1)
              || SID(16) || epoch(4) || seq(8)
完整头部作为 AEAD 的 AAD；密文随后；末尾 16 字节标签。
Nonce = 方向专用 4 字节前缀 || 8 字节大端序号。
```

- 使用**真实 SM4-GCM**（`cryptosuite.NewSM4GCM`），不使用测试替身。
- 上下行使用**独立密钥**与**独立 nonce 前缀**。
- **一把发送锁**覆盖“分配并消费序号 → Seal → 整帧 Write”。
- 接收状态**只在 AEAD 验证成功后推进**；失败不向业务泄露任何明文。
- 认证失败、错序、超限、部分写失败、超时：一律**关闭会话**。
  序号**不回退**，绝不用旧密钥重建 Sender。
- 帧长预算：`max_frame_bytes` 指线帧的 body 长度（`version+kind+payload`），
  **不含**外层 4 字节长度前缀。因此应用明文上限为

  ```text
  plaintext ≤ max_frame_bytes - 2 - 33 - 16
  ```

  对默认的 16384：`16384 - 2 - 33 - 16 = 16333` 字节。
  该换算在 `record.PlaintextBudget` **一处**实现，由
  `TestPlaintextBudgetArithmetic` 与 `TestChannelOversizedPlaintextRejected` 固定。

---

## 7. 建立语义（冻结）

| 事件 | 含义 |
|---|---|
| 网关验证 `SigA` 与 `FinishedA` 并**完整写出 H4** | 网关可本地建立会话 |
| 设备验证 `FinishedB` | 设备可本地建立会话 |
| 网关写出 H4 成功 | **不**表示设备已收到、更不表示设备已接受 |

这一点由 `TestTamperedFinishedBRejected` 正面固定：篡改 `FinishedB` 后，
设备拒绝，而**网关已经持有会话**。因此：

- 不新增任何“双方已收到”的结论；
- 不把网关的成功当作端到端成功；
- 业务执行结果由设备在认证记录上**显式确认**（`Ack`）后才算数。

---

## 8. 本轮的负面规则（冻结）

在套件 1 下，以下情况一律拒绝：

| 情况 | 结果 |
|---|---|
| 配置声明的套件未实现（如套件 2） | 端点**拒绝启动**，不降级 |
| ClientHello 携带非空 `ekA` | 拒绝（`ErrProtocol`） |
| ServerCore 携带非空 `kem_ct` | 拒绝（`ErrProtocol`） |
| 未知/重复/空的套件列表 | 拒绝 |
| 身份、key_id、公钥与登记表不一致 | 拒绝（`ErrPeerIdentity`） |
| 登记表未登记或已吊销 | 查询阶段即失败（fail closed） |
| 角色与登记表不符 | 拒绝 |
| 签名、Finished 不匹配 | 拒绝（分别上报 `ErrAuthentication` / `ErrTranscript`） |
| 消息类型错序、重复、缺失 | 拒绝（`ErrProtocol`） |
| 握手期间出现 `ProtectedRecord` | 拒绝（`ErrProtocol`） |
| 握手期间出现 lab 消息（kind 240/241） | 拒绝（`ErrProtocol`） |
| 载荷有多余尾随字节 | 拒绝（`ErrCodec`） |
| 等长非法 ML-KEM 密文 | 套件 1 下不可达；套件 2 时须由 `Finished` 拒绝 |
| 认证失败/错序/超限/部分写/超时 | 关闭会话，序号不回退 |

---

## 9. 逐条对照 M2 协议审查检查清单

| 检查项 | 状态 | 依据 |
|---|---|---|
| 固定字段、字节序、长度边界 | 冻结 | 第 2、3 节；`codec.go` |
| 身份规则统一 | 冻结 | `identity.ValidName` 同时用于配置、登记表、编解码 |
| 有序能力列表 | 冻结 | 第 2 节末，任何一方都不得排序 |
| key_id、版本、选定套件 | 冻结 | 第 3 节；客户端**重算**选择并比对 |
| 实际线编码作为转录输入 | 冻结 | 第 0 节；`send` 返回编码、`recv` 返回原始字节 |
| 发送端保存所发、接收端保存所收 | 冻结 | 同上 |
| 不通过排序“规范化”能力列表 | 冻结 | 第 2 节末 |
| 各签名/Finished/T2/T4/PRK/业务密钥的准确输入 | 冻结 | 第 3、4 节 |
| 无签名包含自身、无密钥依赖自身 Finished | 冻结 | 第 3 节末、第 4 节末 |
| SM2 UID、身份、静态公钥与角色顺序 | 冻结 | 第 5 节 |
| 角色映射按库源码解释 | 冻结 | 第 5 节，`docs/M1_ERRATA.md` E6 |
| 标准显式确认字段是否使用 | **已固定为“不使用”**，且不可由网络开关跳过 | 第 5 节 |
| 不把 Finished 称为标准 S1/S2 | 冻结 | 第 5 节 |
| 沿用草案 HKDF 日程 | 冻结（并澄清必须拆分 Extract） | 第 4 节 |
| T2 用于方向独立的 Finished 密钥 | 冻结 | 第 4 节 |
| T4 在完整 H4 编码后形成，用于业务密钥/nonce 前缀/SID | 冻结 | 第 4 节 |
| 端点确认完成前不返回 Channel、不发记录、不交业务 | 冻结 | 第 7 节 |
| 服务端验证签名与 FinishedA 并写出 H4 后可本地建立 | 冻结 | 第 7 节 |
| 客户端验证 FinishedB 后才能本地建立 | 冻结 | 第 7 节 |
| 本轮仅传统套件，不填零 PQ 共享秘密 | 冻结 | 第 1、4、8 节 |
| 不宣告尚不存在的混合能力 | 冻结 | 第 1 节 |

---

## 10. 明确不声称的事项

- 这不是标准协议，**没有**形式化安全证明，**没有**经过外部协议审查或实现审查。
- 组合构造（SM2 + HKDF-SM3 + SM4-GCM + 自定义 Finished）的安全性
  **未被证明**。本项目只主张：各组件按上游实现工作，绑定关系在测试中被固定，
  声明的负面行为被测试覆盖。
- **SM2 身份认证不是后量子身份认证。** 本轮完全没有后量子成分参与会话。
- 未做：0-RTT、会话恢复、跨重启 exactly-once、在线吊销传播、长期密钥轮换、
  20 设备并发服务、正式性能基准。
- 拒绝服务只能缓解，不能消除。
