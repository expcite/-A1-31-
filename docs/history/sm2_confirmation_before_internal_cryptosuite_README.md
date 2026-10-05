# 密码适配层：当前能力与验证来源

本文维护当前代码能力。M1/M2 时的未实现状态和环境失败属于历史记录，不能覆盖当前实现。
修订前原文逐字节保存在 `docs/history/cryptosuite_README_backend2_original.md`；
`docs/M1_ERRATA.md`、历史 milestone 和原始日志继续保留，不追填新结论。

## 当前实现

Suite 1（传统）和 Suite 2（混合）均有真实组件和完整 H1–H4 握手。
传统建钥调用 GMSM 的 SM2MQV / SM2SharedKey，混合建钥再加入 Go `crypto/mlkem`
的 ML-KEM-768 共享结果，两份结果都参与经认证上下文绑定的 HKDF-SM3。
应用记录使用 SM4-GCM。`RequireImplemented(suite uint16)` 对套件 1、2 返回 nil，
对未知套件返回错误；这只是组件门控，不能当作安全证明或测试结果。

默认 hybrid 配置现在可使用 Suite 2；失败必须终止，禁止静默回退。
传统模式仍只服务可信配置明确授权的身份。`gateway`、`device`、`bench` 使用这些真实组件，
`bench` 已有受控 smoke/formal 路径；`pbench` 是另行标明口径的公开混合对照。
交互式控制台复用真实 Channel。状态命令只报告能力与已记录的验证来源，执行它不会运行密码检测。

|文件|作用|
|---|---|
|`interfaces.go`|组件接口、套件编号、按套件的真实实现门控|
|`suite.go`|域分离与尺寸常量、错误值、身份字节限制|
|`sm2_identity.go`|SM2 身份签名；ASN.1 与定长 r‖s 转换；明确的 UserID|
|`sm2_agreement.go`|SM2 核心密钥交换；静态协商与签名密钥分离；独立临时材料|
|`sm3_hkdf.go`|SM3、HMAC-SM3、HKDF-SM3、域分离编码与恒定时间比较|
|`sm4_gcm.go`|SM4-GCM：16 字节密钥、12 字节 nonce、16 字节标签|
|`mlkem768.go`|真实 ML-KEM-768 适配器，Go 标准库 `crypto/mlkem`|
|`validate.go`、`scalar.go`|已有公开输入与私钥标量的边界校验|

本层的生产算法来自固定的可靠组件，未自写或替换底层 SM2/SM3/SM4/ML-KEM。
组件测试、协议测试、真实 TCP 测试分别说明其范围，不能互相冒充。

## 已取得的验证记录

下表引用已经存在的历史冻结记录，源码版本为 `interactive-v1.2-backend2-reviewed`，
源清单 SHA-256 为 `21a723b1443f33e9e9fcbfa766060f0100df780b5a8c069f21cac172a8396d1b`。
平台为 Windows 11 `10.0.26200`、windows/amd64、Go 1.27.1，运行来源为
`backend_friend_20261002`。本轮不单独重跑这些专项或重新采样性能；常规 Go 中实际执行的组件对照以本轮 JSON 为准；
新构建的常规回归和 CheckKit 结果以本轮独立原始目录与发布回执为准。

|验证项|已核对的历史结果|范围与原始出处|
|---|---|---|
|SM2 核心交换第二实现|14 组 PASS；实际命令退出 0|`TestSM2IndependentBouncyCastle` 比较 Go 两端与 BC 两端的 32 字节核心结果；`results/raw/backend_friend_20261002/W1/independent_cases.json`、`W1/crypto_security/command.json`、`W1/crypto_security/stdout.txt`|
|OpenSSL 互操作|两方向正常签名/验签和各自错误身份、消息、签名拒绝通过；实际命令退出 0|`TestSM2DynamicOpenSSLMatrix`；OpenSSL 3.5.7；同一 `W1/crypto_security/stdout.txt` 中有实际命令和拒绝退出码。不是 SM2MQV 的 OpenSSL 互操作|
|Windows 全量 race|实际 `go test -race -count=1 -json ./...` 退出 0；验收 PASS|`results/raw/backend_friend_20261002/final_regression/commands/go_race/{command.json,stdout.txt,stderr.txt,exit.txt}` 与 `go_race_summary.json`；29 包，425 顶层 PASS、477 子项 PASS、1 可选长测 SKIP|
|已有组件与协议安全回归|165 顶层 PASS、195 子项 PASS、0 SKIP；实际退出 0|`W1/crypto_security/verified.json`；仅 cryptosuite/handshake/record/wire 四包范围|
|HKDF-SM3、SM4-GCM 向量与早期签名对照|既有公开材料和测试保留|`hkdf_openssl_test.go`、`sm4_gcm_rfc8998_test.go`、`sm2_openssl_test.go` 及各自材料来源；历史阶段结论依原记录|

这些结果具有指定代码、工具和用例范围，不能写成全面安全认证或“所有攻击均可抵御”。
全量 race 通过也不证明所有并发交错均无竞争，未执行的平台和用例仍应明确标记。
旧 MinGW 导致的启动失败继续保存在历史证据中；后续使用隔离便携工具链取得的 Windows
结果不能改写成旧工具链当时通过。

独立对照调用的是现成 PyCharm 分发内 BC 1.80 测试库，不是生产依赖，也不是经核验的
BC 官方原始发行 JAR。实际来源、JAR/类哈希和版本见 `W1/independent_tool.json`，
复现方式见 `docs/BACKEND_W1.md`、`scripts/backend_w1.py`。测试素材和桥仅位于测试路径；
共享结果、随机私钥与随机种子未保存。没有配置相应工具时，测试明确跳过或报配置失败，
不得把这种结果当通过。

## 不可改变的安全约束

- SM2 建钥调用 SM2MQV 与 SM2SharedKey，包含静态与临时材料、双方 UID 和角色顺序；不能以普通 ECDH、仅签名或加密传随机数代替。
- 签名密钥与静态协商密钥由信任登记与各自 key_id 分离。临时握手材料来自密码学安全随机源，失败即终止。
- 混合模式必须同时依赖 SM2 与 ML-KEM 的共享结果；能力、模式、角色、身份、公钥标识和临时材料均绑定认证转录及派生。
- ML-KEM 等长非法密文会隐式拒绝并产生另一结果，`err == nil` 不代表认证成功；仍需验证签名和双向 Finished。
- 认证和确认完成前不交付应用明文；记录层验证标签后才推进接收序号，发送序号、加密与网络写入保持串行顺序。
- 换钥执行完整新握手，不在旧 key 上重置计数器；旧会话记录不得进入新会话。
- AES/SHA-256 或 mock 替身只能用于明确隔离的测试，不能进入生产密码路径。替代物防护测试继续保留。

## 已知边界

适配器只返回 32 字节核心交换结果，没有执行 GB/T 32918.3 标准 S1/S2 显式确认。
项目 H1–H4 中的 Finished 是本项目的转录绑定确认，不能称为标准 S1/S2 互操作。
14 组 BC 对照验证有限输入的核心结果，不证明协议组合的安全性；ML-KEM 第二实现的
完整向量对照没有对应新证据，不能借用 SM2 或 OpenSSL 结果补齐。

ML-KEM 等长输入的成功解析不证明公钥诚实生成或随机性质量；对端跨会话复用临时材料
也不能由 Finished 自动排除。历史材料中关于全零封装公钥与隐式拒绝的观察按各自测试
和当时实现范围保留，不扩写为对全部恶意公钥的防护。

SM2 身份认证仍是经典认证，不能把混合会话建钥写成整个系统全面后量子安全。
没有形式化证明、完整 DoS/侧信道/供应链审计，也没有对所有硬件和操作系统的验证。
Go 托管内存中的 `Destroy` 只尽力结束使用，不保证清除库内部、栈、GC、交换区和转储副本。
Windows `os.Chmod` 不能代替私钥目录 ACL 加固。

依赖与许可见 `docs/DEPENDENCIES.md`，公开来源见 `docs/SOURCES.md`。
本项目协议草案不是标准协议、密码合规认证或已经证明的安全方案。
