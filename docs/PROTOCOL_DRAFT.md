> 本文保留原消息版本 1 草案与历史测量口径。当前 interactive-v1.3-sm2confirm1 的消息版本 2 必需标准 S1/S2；字段、流程与兼容性见 [本轮方案](SM2_CONFIRMATION_PLAN.md)。原记录层与派生规则保持，转录输入新增确认字段。

# 协议设计草案 v0.2 —— 已按实现冻结

**状态：本文件是本项目的设计起点，不是原题内容、现行标准或完整安全证明。**

> **v0.3 变更（M4.2 收口）**
> 本文件已按实现修订，并与 `docs/PROTOCOL_REVIEW_M2.md`、
> `docs/PROTOCOL_REVIEW_M3.md`、`docs/PROTOCOL_REVIEW_M4.md` 一致。
> 早期版本中“Suite 2 未实现 / 默认配置拒绝启动”的描述**已过时**——
> Suite 2 自 M3 起实现并开放，生命周期自 M4 起实现。
> 后者是**审查记录**，本文件是**设计说明**；若两者冲突，以审查记录为准。
>
> 主要修正：
> 1. 四消息改用 `H1..H4` 命名，避免与里程碑 M1/M2/M3/M4 重名。
> 2. 明确 **Hx 指实际线编码**（含外层长度前缀），发送端存所发、接收端存所收。
> 3. 明确 **本轮不使用** GB/T 32918.3 的显式确认字段 S1/S2；
>    只做“核心协商 + 协议级 Finished”，且该选择不可由网络开关跳过。
> 4. 明确 HKDF 必须**一次 Extract、多次 Expand**，不得使用组合函数重复 Extract。
> 5. 明确帧长预算换算与应用明文上限。
> 6. 明确未实现的套件必须**拒绝启动**，而不是降级。
>
> 下列流程借鉴认证握手、transcript 与 HKDF 的通用构造思路，
> 但不能因引用 TLS/HKDF 文档就宣称这个新组合已经得到安全证明。
> 首版限定一网关/多模拟设备、TCP、预登记身份、完整握手，不做 0-RTT/会话恢复。

---

## 1. 信任与套件

可信登记记录必须绑定 ID、角色、SM2 用户标识、签名公钥及 key_id、独立的静态协商公钥及 key_id、最低模式、吊销状态与修订号。
预登记公钥本身须由可信离线管理流程配置；**不能相信握手消息里自行携带的身份公钥**。
SM2 签名密钥、SM2 静态协商密钥分别生成；每轮更新 SM2 临时密钥和 ML-KEM 临时密钥。

- **Suite 1（传统）**：SM2 密钥协商 + 双向 SM2 签名认证 + HKDF-SM3 + SM4-GCM。
  **本轮实现。**
- **Suite 2（混合）**：相同框架，但建钥同时加入 ML-KEM-768 共享结果。
  **已在 M3 实现并开放**，并在 M4/M4.1/M4.2 中继续回归。

默认 `minimum_mode=hybrid`、`allow_legacy=false`。兼容旧设备需可信管理员明确许可，
不能由网络输入决定授权。

**未实现套件的处理（v0.2 修订，M4.2 更新）**：若本地声明的套件列表含有本构建无法
运行的套件，端点**拒绝启动**并说明原因。**不得**静默降级——
这一条仍然成立；当前默认配置声明的套件 1 与套件 2 **都已实现**，因此默认配置可以运行（实际运行 Suite 2）。
那既是对外宣告了不存在的能力，也是一次真实的降级。因此默认的
`configs/gateway.json` / `configs/device.json` 仍然明确拒绝运行；
显式允许传统的 demo 配置见 `configs/*.traditional.demo.json`。

当双方都支持 Suite 2 时选 Suite 2。即使允许传统模式，本轮被删改的混合能力声明仍应导致 transcript 验证失败。
最低模式和能力声明不是同一概念，不得只测本地策略就声称握手防降级已经完成。
（防降级的 wire 级测试已在 M3 完成，并在 M4/M4.1/M4.2 中继续回归；“未实现即拒绝”仍然成立。）

---

## 2. 编码与消息

wire 已实现 `uint32长度 || version || kind || payload`；最大 64KiB，配置可更低。
握手 payload 已实现于 `internal/handshake/codec.go`：固定字段顺序、固定整数端序、
每个变长字段显式长度；拒绝重复、越界、未知、缺失或额外字段（读到末尾必须恰好归零）。

身份限定为 1..64 字节 ASCII `[A-Za-z0-9_-]`；该字符集排除 `.`、`/`、`\` 与空白，
因此网络输入无法被拼进路径。公钥、签名、密文按选定组件严格检查编码和长度。

**安全字段不使用无序 map 的 JSON 字符串拼接。** 业务 payload 可在认证后解析 JSON。

`E(tag, fields...)` 是带唯一类型与长度编码的域分离编码，不是裸拼接。

**`H1...H4` 指双方实际发送/接收的完整合法线编码，包括外层长度/类型/版本。**
发送端把 `wire.Encode` 的返回值同时用于写出与转录；接收端把 `wire.ReadRaw`
返回的原始字节用于转录。规范编码满足 `encode(parse(x)) == x`。

对未签名 core 的编码独立定义，**不在包含自身签名的最终报文里再计算自身签名**。

---

## 3. 四消息流程（已冻结）

A=设备（SM2 initiator）；B=网关（SM2 responder）。

**H1 ClientHello，A→B**：
- version、role、A/B 身份与 key_id、有序支持套件列表；
- 32 字节随机数 Na、SM2 临时公钥 Ra；
- 混合套件时携带本次临时 ML-KEM-768 公钥 ekA；**套件 1 时必须为空**。

**ServerCore，B 准备**：
- 对应身份/密钥标识、服务器有序支持列表、选中套件、32 字节 Nb、SM2 临时公钥 Rb；
- 混合模式下针对 ekA 生成的封装密文 ct；**套件 1 时必须为空**。

**H2 ServerHello，B→A**：
- `ServerCore`；
- `SigB = SM2.Sign(skB_sign, uidB, E("edgehybrid/v1/server-auth", H1, ServerCore))`。

A 验证可信网关签名、期望身份、选中套件和字段；计算 SM2 共享结果并（混合时）解封装 ct。
B 使用双方静态/临时密钥计算相应 SM2 共享结果。

**H3 ClientAuth，A→B**：
- `ClientCore`：身份/key_id/角色与选定的套件；
- `SigA = SM2.Sign(skA_sign, uidA, E("edgehybrid/v1/client-auth", H1, H2, ClientCore))`；
- `FinishedA`：使用独立完成确认密钥计算，覆盖 H1、H2、ClientCore、SigA，但不含其自身值。

B 验证 A 的签名、身份、确认值及 FinishedA；失败就关闭，不进入应用状态。

**H4 ServerFinished，B→A**：
- `FinishedB`：使用另一把完成确认密钥，覆盖完整 H1、H2、H3，不含其自身值。

A 验证 FinishedB；B 成功发送 H4 之后、A 成功验证 H4 之后，各自才可进入已建立状态。
**B 的“发送成功”并不证明 A 已收到**；应用交付与业务确认另行处理。
全部完成后按最终 transcript 派生业务密钥、销毁临时材料。

### SM2 显式确认字段（v0.2 明确）

GB/T 32918.3 的可选显式确认值 **S1/S2 本轮不实现**。本项目只做
“核心协商 + 协议级 Finished”。

因此 `FinishedA/FinishedB` **不是**标准 S1/S2，不得这样称呼。
一致性局限：与同样实现标准 S1/S2 的第三方 SM2 实现无法在确认值上互操作，
能互操作的只有协商出的 `Zsm2` 本身。该选择**不可由网络开关跳过**。

---

## 4. 密钥日程（已冻结，无循环依赖）

H=SM3。HKDF 用 HMAC-SM3 实例化；参照 RFC 5869 的 extract/expand 结构。
所有 label 均固定为唯一协议域；派生长度纳入 info。

```text
Zsm2 = 完整 SM2 协商输出的 32 字节结果
Zpq  = ML-KEM-768 的 32 字节共享结果（仅混合模式）

IKM_T = E("edgehybrid/v1/ikm", u16(1), "sm2", Zsm2)
IKM_H = E("edgehybrid/v1/ikm", u16(2), "sm2", Zsm2, "mlkem768", Zpq)
salt  = H(E("edgehybrid/v1/salt", Na, Nb))
PRK   = HKDF-Extract-SM3(salt, 选定模式的 IKM)      <-- 只 Extract 一次
T2    = H(E("edgehybrid/v1/t2", H1, H2))

KfinA = Expand(PRK, E("edgehybrid/v1/finished-A", T2, u16(32)), 32)
KfinB = Expand(PRK, E("edgehybrid/v1/finished-B", T2, u16(32)), 32)
FA    = HMAC-SM3(KfinA,
        E("edgehybrid/v1/confirm-A", H(E("edgehybrid/v1/t3core", H1,H2,ClientCore,SigA))))
FB    = HMAC-SM3(KfinB,
        E("edgehybrid/v1/confirm-B", H(E("edgehybrid/v1/t3", H1,H2,H3))))

T4    = H(E("edgehybrid/v1/t4", H1,H2,H3,H4))
K_AtoB = Expand(PRK, E("edgehybrid/v1/app-AtoB", T4, u16(16)), 16)
K_BtoA = Expand(PRK, E("edgehybrid/v1/app-BtoA", T4, u16(16)), 16)
P_AtoB = Expand(PRK, E("edgehybrid/v1/iv-AtoB",  T4, u16(4)),   4)
P_BtoA = Expand(PRK, E("edgehybrid/v1/iv-BtoA",  T4, u16(4)),   4)
SID    = Expand(PRK, E("edgehybrid/v1/session-id",T4, u16(16)), 16)
```

### v0.2 必须写清的实现细节

`cryptosuite.HKDFSM3` 是**一次 Extract+Expand**的组合函数。本日程需要
**一次 Extract、多次 Expand**，因此生产代码只调用 `HKDFExtractSM3` 一次，
之后全部使用 `HKDFExpandSM3`。**把 PRK 再交给组合函数会悄悄重新 Extract**，
导致与对端派生出不同的密钥。

Kfin 依赖 H2 之前的上下文，业务密钥依赖最终 H4，避免 Finished 与自身密钥循环依赖。
身份和公钥标识必须确实包含在对应消息里，不能只在注释声称“绑定了”。

此日程是待分析组合：需要说明两种建钥、KDF、签名、随机性和独立性假设；
不直接宣称“任一组件安全即可抵抗所有攻击”。

---

## 5. 数据记录与换钥

固定头部 33 字节：version(1)、suite(2)、content_type(1)、direction(1)、SID(16)、epoch(4)、seq(8)。
完整头部作为 AEAD 的 AAD；密文在其后，使用 16 字节标签。
Nonce = 4 字节方向专用派生前缀 || 8 字节大端序号。上下行使用不同密钥。

TCP 首版采用严格递增序号；不实现滑动窗口、不允许乱序记录进入业务层。
任何接收状态只在 AEAD 认证成功后推进；握手、方向、会话不匹配就拒绝。
调用者串行化一次“取序号 + Seal + 写入”；部分发送失败、认证失败、错序均关闭会话。

**帧长预算（v0.2 新增）**：`max_frame_bytes` 指线帧 body 长度
（`version + kind + payload`），**不含**外层 4 字节长度前缀。保护记录在载荷中
占 `33 + 密文 + 16` 字节，故事件明文上限为

```text
plaintext ≤ max_frame_bytes − 2 − 33 − 16
```

默认 16384 时为 16333 字节。该换算只在 `record.PlaintextBudget` 一处实现，
避免两处口径漂移。

默认每方向最多 2^20 条记录，是本原型策略而非通用安全定理；
还应限制会话时长、总加密字节数并记录采用界限的理由。
计数或定时到限：停止发送、关闭旧连接、重新执行完整握手，生成新密钥、新临时 KEM 和新 SID。
旧 epoch/SID 的数据不进入新会话。**不得在旧密钥上把 seq 清零。**

业务控制使用 operation_id 和幂等操作；需要跨会话精确去重时单独实现持久化账本。
**会话内的幂等记忆不跨重启，因此不得称为 exactly-once。**

---

## 6. 必须检查的安全细节

同长度非法 ML-KEM 密文可能没有 error，而是隐式拒绝产生不同共享结果：
必须靠认证 transcript/Finished 拒绝，**不以解封装返回码代替认证**。
（注意：这与“公钥格式检查”是两件事，见 `docs/M1_ERRATA.md` E3。）

公钥校验、SM2 用户标识、签名方法对“消息/摘要”的语义、签名随机性都要核验所选库，
不要双重哈希或漏 ZA。使用真正的 SM2 协商 API，不把 TLS 中的 SM2 曲线 ECDHE 当成相同协议。

检查截断、重复消息、H3/H4 错序、版本回退、身份互换、签名密钥/协商密钥替换、等长 KEM 篡改。

拒绝服务通过帧限长、连接/握手并发上限、超时和可信身份查找缓解，**不宣称完全消除**。

长期密钥泄露和临时密钥泄露要分情形分析；**SM2 身份认证不是后量子签名认证**。

进入真实部署或声称安全达标前，应另行做协议与实现审查；集成测试不能替代安全证明。

---

## 7. 与 v0.1 的差异汇总

| 项 | v0.1 | v0.2（已冻结） |
|---|---|---|
| 消息命名 | M1..M4 | H1..H4 |
| Hx 定义 | 未明确是否含外层长度 | 明确为完整线编码，含外层长度前缀 |
| 未实现套件 | 未说明 | **拒绝启动**，不降级 |
| SM2 显式确认 | “如采用” | **本轮不采用**，且不可由网络开关跳过 |
| HKDF | 未区分组合/拆分 | **必须**一次 Extract、多次 Expand |
| 帧长预算 | 未给出换算 | 明确 `−2−33−16`，默认上限 16333 |
| GCM nonce 重用 | 隐含在记录层 | 明确：AEAD 不提供防护，由记录层结构化保证 |
| 业务幂等 | 提到 operation_id | 明确会话内幂等，**不声称跨重启 exactly-once** |
