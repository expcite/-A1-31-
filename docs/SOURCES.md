# 要求与技术参考来源

核对日期：2026-09-24。版本化内容请在本地实现时重新核对并锁定依赖。

## 要求来源

用户提供的《（A1-31）园区边缘设备国密与后量子混合安全通信软件设计实现(2).docx》，第1页场景、定位与要求1-6，第2页要求6续及创新要求。
本工程没有修改原题要求，也没有把建议的语言和协议草案说成原题指定实现。

## 外部技术参考（不是对本项目的安全背书）

- Go crypto/mlkem 官方API、Encapsulate返回顺序、等长非法密文的隐式拒绝：
  `https://pkg.go.dev/crypto/mlkem`
- Go 1.24发行说明，标准库ML-KEM引入：
  `https://go.dev/doc/go1.24`
- Go官方发行下载与工具链版本核验：
  `https://go.dev/dl/`
- GMSM官方仓库：
  `https://github.com/emmansun/gmsm`
- GMSM ecdh官方包文档，SM2MQV与SM2SharedKey：
  `https://pkg.go.dev/github.com/emmansun/gmsm/ecdh`
- GMSM SM4官方包文档，SM4结合GCM接口示例：
  `https://pkg.go.dev/github.com/emmansun/gmsm/sm4`
- Go AEAD接口，包括nonce唯一要求：
  `https://pkg.go.dev/crypto/cipher`
- RFC5869，HKDF结构和上下文绑定：
  `https://datatracker.ietf.org/doc/html/rfc5869`
- RFC8446，认证握手、transcript与Finished的参考：
  `https://datatracker.ietf.org/doc/html/rfc8446`
- RFC8998，SM4认证加密及SM2曲线上ECDHE与SM2密钥交换的区别：
  `https://datatracker.ietf.org/doc/html/rfc8998`
- Go TLS官方接口，公开混合参考组：
  `https://pkg.go.dev/crypto/tls`

本骨架没有下载并运行这些密码库的官方测试向量；这属于M1要交付的证据。不要将参考链接当作已通过验证。
