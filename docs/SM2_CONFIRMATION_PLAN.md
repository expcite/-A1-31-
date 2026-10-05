# 标准 SM2 S1/S2 增补与验证计划

用户本轮授权补齐标准确认；前端继续冻结。当前版本 interactive-v1.3-sm2confirm1，基线 440 项源清单 SHA-256：64d8ba340e5da098167ce0a02aad67219dbf986b4a68e4f4c36aa09e1d210c6e。基线源码只作比较，不覆盖工作区。

## 生产范围

复用 gmsm v0.44.1 的 SM2MQV、SM2SharedKey、SM2ZA 和 SM3。只组合标准确认输入：inner = SM3(xU‖ZA‖ZB‖xRA‖yRA‖xRB‖yRB)，S1 = SM3(02‖yU‖inner)，S2 = SM3(03‖yU‖inner)。所有坐标固定 32 字节；A 是发起方，B 是响应方；现有 32 字节核心 KDF 不变。不是自行实现底层曲线或散列。

网关 H2 带定长 S1，设备验签后校验 S1；设备 H3 带 S2，网关验签后校验 S2，再校验 FinishedA。原 FinishedB 继续必需。H2/H3 各增加 u32 长度与 32 字节确认，合计 72 字节。确认值受原签名与转录绑定；S1/S2 只确认 SM2 部分，项目 Finished 继续确认混合双秘密和全部转录。

握手体版本由 1 升为 2，旧版本明确拒绝，无回退。外层 wire.Version、SM4 记录格式、H1–H4 顺序、HKDF 算法/域分离规则、长期密钥来源、生命周期 CAS、会话计数器规则不改。转录输入变化会自然改变最终会话密钥与 SID。本地公开临时值必须等于私有句柄对应公钥，避免确认输入和 MQV 输入分离。升级需设备与网关同步；不自动恢复吊销身份或重置计数器。

## 冻结的验证边界

1. 保留修前缺失 S1/S2 的同例失败。适配器正反例检查方向、长度、身份、静态/临时材料、本地句柄绑定。
2. BC 独立标准确认：固定 sm2p256v1、256 位共享密钥。12 组：3 组公开 GMSM 输入、UTF-8/binary ID、反转角色、改变静态/临时材料、4 组内存随机输入。比较两端密钥、S1、S2；BC 发起方 calculateKeyWithConfirmation 实际接收并校验 Go S1；BC 响应方 API 返回预期 S2，桥用 BC 恒定时间工具实际验证 Go S2。错误位、长度、角色、身份分别拒绝。公开输入不是自行声称的国标附录 KAT，随机私钥/输出不记录。
3. 原 14 组核心独立对照及 OpenSSL 双向签名/HKDF 保留范围；OpenSSL 不当作 SM2MQV/S1/S2 第二实现。
4. 真实 loopback TCP T/H：正常保护记录 26.3/28.7、双向控制、新 SID；S1/S2 缺失/短/长、翻位未重签（签名拒绝）、翻位重签（标准确认拒绝）、旧会话确认重签、错角色、旧消息版本；原 FinishedA/B 拒绝对照。仅断言可观测端点，不把未知业务验收手填 false。
5. 全量 Go JSON、真实 Windows race、既有 Friend/Recheck、Python/Node 自测、构建/vet；有限 parser fuzz。测试与构建串行，原始命令/退出码/失败/跳过保留。缺少配置的独立工具不得计通过。
6. 新构建实际 1/20 设备安装运行、网关/设备生命周期、容量/API/导出/停止重启、实际 1 秒→5 秒节奏、新版有界稳定性（短测明确不是 1800 秒正式长测）。独立解压重新构建并执行标准确认/TCP/CLI 等必要回归，历史正式结果只验收已有 raw，不重新采样。

## 影响与不执行项

协议字节和密码工作量改变，旧性能不能归入新版本。此轮优先正确性和独立确认，不默认重跑正式 1000 次、1800 秒或重写旧 M5/M6。旧 raw、源清单、run_id、Core_Frozen ZIP 完全保留；本轮日志使用新目录与新源码哈希。内嵌性能图属于历史原测量版本。新版正式矩阵/正式长测未执行时明确 NOT_EXECUTED，不能由短 smoke 或旧记录替代。

不作形式化证明、标准协议/合规认证、全硬件或所有攻击的断言。Go Destroy/clear 不保证消除所有内存副本。SM2 经典身份认证的后量子边界不改变。前端、聊天/文件/云等功能不在本轮。

## 复现与来源

配置已存在的 Go、批准的隔离 Windows GCC、Java/javac、BC JAR 与 OpenSSL；仅子进程环境，不改系统 PATH。BC JAR SHA-256 固定 cefef94b35315f4b94c2653a99e38c94ca8b312a59dd61b0d5e956839fa858b9。JAR 是本机 JetBrains 发行物；不声称已核验 BC 官方发行签名。用户运行包不包含这些测试工具。

`go test -count=1 -json ./internal/cryptosuite -run TestSM2StandardConfirmationIndependentBouncyCastle`，`go test -count=1 -json ./internal/handshake -run TestSM2StandardConfirmationRealTCP`。全量以 `scripts/backend_regression.py` 执行；构建以 `scripts/backend_build.py`；安装使用 `scripts/interactive_installed_acceptance.py`。具体命令与现有环境精确路径在本轮 command.json，交付路径均使用包根相对路径。

参考：[gmsm v0.44.1 EC 实现](https://github.com/emmansun/gmsm/blob/v0.44.1/ecdh/sm2ec.go)、[BC 标准确认 API](https://downloads.bouncycastle.org/java/docs/bcprov-jdk15to18-javadoc/org/bouncycastle/crypto/agreement/SM2KeyExchange.html)、[BC 确认编码实现](https://github.com/bcgit/bc-java/blob/r1rv83/core/src/main/java/org/bouncycastle/crypto/agreement/SM2KeyExchange.java)。本机执行库版本/哈希按实际元数据为准，不以网页最新版代替。旧 GMSM 向量与本轮脚本的源文件哈希均随源清单保存。
