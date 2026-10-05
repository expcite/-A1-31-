# W1：本轮密码组件核验

本文件描述测试方法与实际首轮结果；最终冻结版本的结果以本轮 `results/raw/backend_20261002/w1/`、全量 Go JSON 和构建来源回执为准。历史 M5/M6 数据没有修改。仅新增或调整测试与开发脚本，生产密码、H1–H4 与记录层没有因 W1 重写，前端源文件没有修改。

## 实际工具与来源

已安装工具只以进程级路径调用，没有安装、下载、修改系统 PATH 或远端执行。

|工具|实际版本/位置|作用|
|---|---|---|
|Go|1.27.1 windows/amd64，WorkBuddy 已有工具链|当前生产 GMSM 适配器、握手/记录层回归|
|GMSM|`github.com/emmansun/gmsm v0.44.1`，既有 go.mod/go.sum|生产 SM2/SM3/SM4 实现|
|JBR/java/javac|21.0.8，PyCharm 2025.2.3 已有 `jbr/bin`|隔离独立实现桥，仅开发测试|
|Bouncy Castle|Provider info 为 **v1.80**，`getVersionStr()` 显示 `1.8`；后者是版本数字格式，不能据此写成旧 1.8 版本|独立 `SM2KeyExchange`，仅测试|
|OpenSSL|3.5.7 9 Jun 2026，PortableGit 已有 `usr/bin/openssl.exe`|当前动态 SM2 双向签名互操作与负例|

本地 BC 文件是 `C:/Program Files/JetBrains/PyCharm 2025.2.3/lib/bouncy-castle.jar`；SHA-256 为 `cefef94b35315f4b94c2653a99e38c94ca8b312a59dd61b0d5e956839fa858b9`。PyCharm `license/third-party-libraries.json` 同时声明 bouncy-castle-provider 1.80、MIT，原始文件哈希及摘录保存在 `independent_tool.json`。Java metadata 的许可证文字、实际 Provider 信息和完整曲线参数由执行日志保存。

这是已安装 JetBrains **重打包**文件：没有根 `META-INF/MANIFEST.MF`，不能把本地整体哈希叫作 BC 官方发行 JAR 哈希或声称已验证官方发行签名。交付只包含桥源码、许可证说明、文件与相关类哈希，运行包不引入 JBR 或 BC。这不是升级/推荐旧 BC 作为生产依赖。

方法与 API 核对使用官方资料：[BC r1rv80 `SM2KeyExchange`](https://github.com/bcgit/bc-java/blob/r1rv80/core/src/main/java/org/bouncycastle/crypto/agreement/SM2KeyExchange.java)、[私有参数](https://github.com/bcgit/bc-java/blob/r1rv80/core/src/main/java/org/bouncycastle/crypto/params/SM2KeyExchangePrivateParameters.java)、[公开参数](https://github.com/bcgit/bc-java/blob/r1rv80/core/src/main/java/org/bouncycastle/crypto/params/SM2KeyExchangePublicParameters.java)、[BC 许可证](https://www.bouncycastle.org/about/license/)。OpenSSL 使用显式 `distid`，依据 [OpenSSL pkeyutl 文档](https://docs.openssl.org/3.5/man1/openssl-pkeyutl/)核验签名和验签参数。

## 独立 SM2 核心结果对照

入口为 `TestSM2IndependentBouncyCastle` 和 `scripts/sm2_independent/SM2ExchangeOracle.java`。Java 只调用已有 BC 库，不实现 SM2 算法，不进入 gateway/device/console。

Go 两端都调用生产 `NewSM2Agreement().Derive`，BC 两端分别调用 `SM2KeyExchange.init` / `calculateKey`。同一用例严格对齐：sm2p256v1 的 p/a/b/G/n/h；双方身份的**原始字节**；发起/响应角色；各自独立静态与临时标量以及对端 65 字节未压缩公钥；Go 输出 **32 字节**、BC 输入长度 **256 位**。曲线参数由 metadata 逐项验证，不只相信曲线名称。

逐字节断言 `Go-device == BC-device`、`Go-gateway == BC-gateway`。正常一致输入还断言四端一致；错误输入按事先定义的结果变化/端点不一致断言，不能要求所有身份/角色变化都由底层库报错。

|用例集合|预定义期待|首轮观测|
|---|---|---|
|3 组公开 GMSM 上游测试材料|四端核心结果一致|PASS|
|UTF-8 身份、含零字节身份|四端一致，且不同于同标量 Alice/Bob 基线|PASS|
|双方一致反转角色|四端一致，且不同于原基线|PASS|
|双方都声称 Initiator|两个端点结果不一致，但每个 Go 端与对应 BC 端一致|PASS|
|单侧视图的对端 UID 改错|两个端点结果不一致，且受影响端改变；各自跨实现一致|PASS|
|静态材料改变、临时材料改变，各自正确公开给对端|四端一致，且不同于原基线|PASS|
|4 组 crypto/rand 临时材料|四端一致；不保存随机种子或私钥|PASS|

共 14 组对照。公开固定标量沿用 `internal/cryptosuite/sm2_agreement_test.go` 中已注明来源的 `gmsm@v0.44.1/ecdh/ecdh_test.go` 材料，不能叫作独立官方国标向量。动态标量和共享结果仅存在于测试内存与匿名进程管道，未写文件或命令行；JSON 仅写 case_id、来源、曲线、长度、布尔比较、实际期待与状态。单独比较输出长度不够，测试实际使用 `bytes.Equal` 比较每一端结果。

未运行标准 SM2 S1/S2 确认；项目 Finished 是其自身经转录绑定的确认路径。本次核心结果一致不能写成标准确认互操作、标准认证或协议安全证明。ML-KEM 第二实现的全向量对照仍未执行；保持其历史边界。

## OpenSSL 与原有安全回归

`TestSM2DynamicOpenSSLMatrix` 当场生成一次性测试 SM2 私钥，两方向都执行当前签名与验签。每方向保留正常对照，以及错误身份、消息、签名三类拒绝：Go 实际返回验证错误，OpenSSL 实际退出 **1** 且输出签名验证失败，不能把命令未启动或参数错误当负例通过。私钥只在 `testing.TempDir`，退出时清理，私钥文本仅供内存导入，不进日志。

旧公开入站签名夹具、旧出站互操作继续执行，没有以新测试代替或删除。新增 `EDGEHYBRID_OPENSSL` 测试定位变量，显式路径与独立测试环境只影响子进程；未配置/工具不可用时准确标记跳过或配置失败。

本轮执行 `cryptosuite/handshake/record/wire` 全部既有测试，涵盖真实 Suite1/Suite2、双秘密依赖、防降级、KEM 隐式拒绝后 Finished、转录/身份绑定、SM4 向量、方向与会话隔离、重放、并发 Send 和取消。首轮完整验收器观测 **164 顶层 PASS、188 子项 PASS、0 SKIP、退出 0**。这是一组组件回归，不替代全量工程或 race。

新增 `FuzzSM2PublicInput` 只触及已有公开点验证器；与既有 `FuzzFrame` 各以 20 秒、2 工作者有界执行。输入没有私钥或会话秘密，失败最小反例由 Go 自身保存；未发现反例不能写成安全证明。缓存不交付。

## 复现与原始结果

在项目根执行：

```powershell
C:\Python312\python.exe scripts/backend_w1.py --out results/raw/backend_20261002/w1/new_run --fuzz-seconds 20
```

该脚本默认只定位当前已安装工具；别的电脑需明确传入 `--go`、`--java`、`--javac`、`--bc-jar`、`--bc-sha256`、`--openssl`，不下载或安装。输出目录必须不存在，失败不被覆盖；每个命令保存 stdout、stderr、真实退出码、平台、起止时间与源码清单。每个 Go JSON 均**实际调用**完整结果验收器，核对预期包集合和真实退出码。

完整工作区在并行编辑时，起止全源码 hash 可能不同；回执明确保存两者，不能把开发期间组件结果贴成最终冻结源码结果。最终冻结后以新目录执行并与本轮发布 source/build/run 关联。没有经过本轮工具实测的外部平台保持 NOT_EXECUTED。
