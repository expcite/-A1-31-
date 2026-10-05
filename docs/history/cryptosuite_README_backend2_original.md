# 密码适配层（M1 实现 + M1.1 勘误）

> **重要：先读 `docs/M1_ERRATA.md`。**
> 本文档中对证据等级的表述以勘误为准。要点：
> SM2 **密钥交换**目前只有上游自有向量（**未**提升为独立交叉核验）；
> SM2 **签名**已有与 OpenSSL 的双向互操作；
> HKDF-SM3 已有 OpenSSL 独立对照；SM4-GCM 已有 RFC 8998 A.1 官方向量。
> `go test -race` 是**环境阻塞**（见勘误 E1），不是通过。

## 当前状态

**已完成组件级实现与验证；传统套件（Suite 1）的完整安全握手已在 M2 实现。**
混合套件（Suite 2）仍未实现并被明确拒绝；
`bench` 仍拒绝生成未实现的性能结果。
`RequireImplemented()` 仍返回 `ErrNotImplemented`，`cmd/gateway`、`cmd/device`、`cmd/bench` 仍然 fail-closed（实测 exit 2）。
本层通过组件测试 **不等于** 完整协议可用，也 **不等于** 系统经过安全审计。

## 文件

| 文件 | 内容 |
|---|---|
| `interfaces.go` | 组件边界（未改动）。`RequireImplemented()` 保持返回错误 |
| `suite.go` | 域分离常量、尺寸常量、错误值、`validUserID` |
| `sm2_identity.go` | SM2 身份签名（GB/T 32918.2）。ASN.1 签名与定长 r‖s 互转；固定 UserID |
| `sm2_agreement.go` | SM2 完整密钥交换（GB/T 32918.3）：`SM2MQV` + `SM2SharedKey`；静态协商密钥与签名密钥分离 |
| `sm3_hkdf.go` | SM3、HMAC-SM3、HKDF-SM3（RFC 5869 结构）、`Encode` 域分离编码、`ConstantTimeEqual` |
| `sm4_gcm.go` | SM4-GCM（16 B 密钥 / 12 B nonce / 16 B 标签） |
| `mlkem768.go` | ML-KEM-768（FIPS 203，Go 标准库 `crypto/mlkem`） |

测试文件与之同名（`*_test.go`），另加 `hkdf_rfc5869_test.go`（SHA-256 HKDF 结构交叉核验辅助）、`aes_guard_test.go`、`block_alias_test.go`（替代物防护）。

## 关键设计约束（不要改动）

- **SM2 协商不是普通 ECDH。** 只调用 `SM2MQV` + `SM2SharedKey`，因此包含隐式签名、ZA/ZB 顺序与双方身份绑定。若换成普通 ECDH，会得到"像共享秘密"但丢失全部绑定的值。
- **`isResponder` 必须匹配对端角色。** 角色互换会改变 ZA/ZB 顺序，从而改变派生密钥——这是角色错配可被检测的原因。
- **签名密钥与协商密钥分开管理。** 二者字节编码形态相同（都是未压缩 SM2 点），因此必须靠信任注册表而非编码来区分角色。
- **ML-KEM 等长非法密文不返回 error。** FIPS 203 隐式拒绝会返回另一个共享秘密。**不得**把 `err == nil` 当作认证成功；必须由握手 Finished/确认值在后续阶段检测。见 `mlkem768_test.go` 中的 `TestMLKEM768ImplicitRejectionSameLength`。
- **`RequireImplemented(suite uint16)` 是按套件门控，不得改成无条件 nil。**
  套件 1（传统）为 true，套件 2（混合）与未知套件为 false。
  这不是一个可以翻转的全局就绪布尔量：默认 hybrid 配置**拒绝启动，绝不降级**。
- **不可用 AES/SHA-256 替代 SM4/SM3。** `aes_guard_test.go` 与 `block_alias_test.go` 存在的目的就是让这类替换在测试中立刻可见。

## 已实测的行为发现

- `crypto/mlkem` 会**接受**全零的 1184 字节封装密钥：FIPS 203 的 `ek` 只是字节编码的多项式向量，没有"合法点"校验。这是格式合法但密码学退化的公钥，库不报错，**必须由握手的认证转录检测**。
- SM3/SM4 已知答案向量、SM2 上游参考向量均已通过（向量来源与局限见对应测试文件顶部注释与 `results/milestone_M1.md`）。

## 环境限制（如实记录）

`go test -race` 在本机 Windows 环境失败（`exit status 0xc0000139`），原因是 MinGW 的 `libmsvcrt.a` 从 `msvcrt.dll` 导入 `__iob_func`，而现代 Windows 不再导出该符号。**普通测试不受影响**，未通过关闭任何校验来绕过。详见 `results/milestone_M1.md`。

## 许可与来源

依赖固定版本、许可证与 API 核验见 `docs/DEPENDENCIES.md`。
SM2 身份签名不是后量子身份认证；不要宣称整个系统达到无条件"全面抗量子"。
私钥句柄 `Destroy` 在 Go 托管内存中只能尽力清理，不保证清除库内部副本、栈副本、GC 副本、交换区或系统转储。

标准/官方接口链接见 `docs/SOURCES.md`；完整协议待办见 `docs/PROTOCOL_DRAFT.md`。
