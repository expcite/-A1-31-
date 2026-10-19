# 当前用途归类

生产组件：Go标准库（包含ML-KEM/TLS组件）和固定gmsm，依赖事实须对照go list -m all、go list -deps及实际EXE元数据；go.sum出现条目不证明生产路径使用。现有依赖版本与校验服务不改。

构建/测试：Go、Python、Node、选定CC、Java/BC/OpenSSL仅用于构建和回归；用户软件不依赖这些开发工具。BC JAR来源为已安装JetBrains PyCharm组件，sha cefef94b35315f4b94c2653a99e38c94ca8b312a59dd61b0d5e956839fa858b9，不声称官方独立发行签名验证。其组件许可及Go/gmsm许可原件见docs/licenses和THIRD_PARTY_NOTICES.md。Windows系统API是运行平台，未改DLL或全局PATH。项目权利声明见LICENSE.txt，不能替作者擅自指定开源许可证。

以下为历史固定依赖及已知范围，新的实际工具版本/命令记录在外置构建及回归证据中。

# 第三方组件与版本状态

> **勘误：见 `docs/M1_ERRATA.md`。** 其中 E6 按 `gmsm@v0.44.1` 的实际源码校正了
> `SM2MQV` / `SM2SharedKey` 的接收者类型；E9 记录了本轮新增的独立交叉核验来源
> （OpenSSL 3.5.7）。该勘误不改变本文件中的版本与许可证结论。

## 当前骨架（M1/M1.1/M2 之后）

实际验证工具链、命令和返回码见 `results/raw/m1_1_verification.txt`、
`results/raw/m2_verification.txt`、`results/milestone_M1.md` 与 `results/milestone_M2.md`。

## 已固定的依赖（M1）

本机核验环境：

| 项 | 值 |
|---|---|
| 操作系统 | Windows 10/11 amd64 |
| Go 工具链 | `go1.27.1 windows/amd64`（`go version` 实测输出） |
| 模块路径 | `example.com/edgehybrid` |
| go 指令版本 | `go 1.25.0` |

### 1. `github.com/emmansun/gmsm`

| 字段 | 值 |
|---|---|
| 固定版本 | `v0.44.1` |
| 来源 | `https://github.com/emmansun/gmsm`（模块代理：`https://goproxy.cn`） |
| 发布页 | `https://pkg.go.dev/github.com/emmansun/gmsm@v0.44.1` |
| 发布时间 | 2026-07-28（pkg.go.dev 显示） |
| 许可证 | **MIT**（pkg.go.dev 标注 + 模块内 `LICENSE` 实测首行 `MIT License` / `Copyright (c) 2020 Sun Yimin`） |
| 要求 Go 版本 | **Go 1.25+**（README 实测，"要求 Go 版本：Go 1.25+"） |
| 引入目的 | 提供 GB/T 32918 / GB/T 32905 / GB/T 32907 的 SM2 签名与密钥交换、SM3、SM4 |
| 完整性校验 | `go.sum` 已记录 `h1:` 与 `/go.mod h1:` 双哈希；未关闭模块校验 |
| 已核验风险 | 为同名项目中的官方上游仓库（`emmansun`），非未知 fork；MIT 为宽松许可，需保留版权与许可声明（"被许可人义务"） |

实际使用的子包与接口（**逐一在模块缓存中核对过签名，非凭猜测**）：

| 子包 | 实际调用的接口 | 来源文件 |
|---|---|---|
| `github.com/emmansun/gmsm/sm2` | `GenerateKey`、`PrivateKey.Sign(..., NewSM2SignerOption(bool, []byte))`、`VerifyASN1WithSM2`、`NewSM2SignerOption` | `sm2/sm2.go` |
| `github.com/emmansun/gmsm/ecdh` | `P256()`、**`(*PrivateKey).SM2MQV(eLocal *PrivateKey, sRemote, eRemote *PublicKey) (*PublicKey, error)`**、**`(*PublicKey).SM2SharedKey(isResponder bool, kenLen int, sPub, sRemote *PublicKey, uid, remoteUID []byte) ([]byte, error)`**、`GenerateKey`、`NewPrivateKey`、`NewPublicKey` | `ecdh/ecdh.go:169`（SM2MQV）、`ecdh/ecdh.go:117`（SM2SharedKey） |
| `github.com/emmansun/gmsm/sm3` | `Sum`、`New` | `sm3/sm3.go` |
| `github.com/emmansun/gmsm/sm4` | `NewCipher` | `sm4/sm4.go` |

> **接收者类型已按 `gmsm@v0.44.1` 的实际源码校正（M2.1）。**
> 早期版本把两者都写成 `PublicKey` 的接收者，那是错的：
> `SM2MQV` 的接收者是**本方的静态私钥** `*PrivateKey`，返回一个 MQV 点 `*PublicKey`；
> `SM2SharedKey` 的接收者才是**上一步返回的那个 `*PublicKey` 点**。
>
> 注：`SM2SharedKey` 的 `isResponder` 形参决定 ZA||ZB 还是 ZB||ZA 进入 KDF；
> 本适配器由协议角色映射该布尔值，见 `internal/cryptosuite/sm2_agreement.go`。

### 2. `golang.org/x/crypto`（间接依赖）

| 字段 | 值 |
|---|---|
| 固定版本 | `v0.44.1` 引入的 `v0.54.0`（`go mod tidy` 自动选定，标记为 `// indirect`） |
| 许可证 | BSD-3-Clause（Go 项目标准许可） |
| 引入目的 | `gmsm/sm2` 的传递依赖；本项目**不直接**调用它 |
| 完整性校验 | `go.sum` 已记录双哈希 |

同时在 `go.sum` 中出现但**未被本模块导入**的传递条目：`golang.org/x/sys v0.47.0`。

## 未引入的依赖

| 组件 | 状态 | 说明 |
|---|---|---|
| OS 资源统计实现 | 未引入 | M5 依目标平台确定（当前 `bench` 拒绝运行） |
| `crypto/mlkem` | **标准库**，非第三方依赖 | 由 Go 1.24+ 标准库提供，见下 |

## 后量子组件来源

**ML-KEM-768 使用 Go 标准库 `crypto/mlkem`（FIPS 203），未引入任何第三方 PQC 库。**
使用标准库的原因是：后量子这一半依赖 Go 工具链自身的代码，避免新增供应链面。
对应参数集尺寸（FIPS 203 Table 2）：封装密钥 1184 B、密文 1088 B、共享秘密 32 B，已在 `internal/cryptosuite/mlkem768_test.go` 中固定。

**M3 的状态变化**：`crypto/mlkem` 从"组件级适配"变成**真正参与建钥**——
Suite 2 的握手里，网关真实 `Encapsulate`、设备真实 `Decapsulate`，
得到的 `Zpq` 与 SM2 的 `Zsm2` 一起进入 HKDF-SM3 的 Extract 输入。
线格式尺寸也已在握手层固定（`internal/handshake/hybrid_test.go`）。

这**不**表示后量子部分已被独立验证：本轮只验证了"尺寸正确、每次新生成、结果真实依赖两份秘密、篡改会被 Finished 捕获"，
**没有**把本实现的 ML-KEM 与任何第二个实现做过逐字节对照，也**没有**跑 ACVP/KAT 之外的官方向量集。

## 遵守的约束

- 未为了安装方便关闭 TLS 证书校验、模块校验或改用未知镜像。
- 未采用未知的同名 fork；版本号与 API 均来自上游仓库/官方文档核验，未凭猜测写入。
- 未提交真实密钥、运行时产物或秘密。

## M5 待核验项

| 组件 | 用途 | 状态 |
|---|---|---|
| Go `crypto/tls` | 公开混合对照 | M5 核验实际协商组、认证配置、版本 |
| 实际 tag/commit 的 LICENSE 复核 | 最终交付清单 | 交付前对当时实际版本再核一次 |

## 测试用外部工具（不进入任何服务的运行时依赖）

这些工具**只在测试与证据生成时使用**，不会成为 `gateway` / `device` 的依赖，
也不会被打包进运行时产物。

| 工具 | 版本 | 用途 | 是否运行时依赖 |
|---|---|---|---|
| OpenSSL | `OpenSSL 3.5.7 9 Jun 2026` | **独立实现对照基准**：HKDF-SM3 逐字节比对（8 组向量）、SM2 签名双向互操作 | **否** |
| MinGW-W64 gcc | 8.1.0 | `-race` 所需的 C 链接器。**本机不兼容**，见 `docs/M1_ERRATA.md` E1 | 否（仅 `-race` 需要） |

### OpenSSL 作为对照基准的边界

- OpenSSL **是**一个与本项目依赖完全独立的实现，因此它与本项目输出一致，
  是关于**构造正确性**的真实证据（而不是自洽性证据）。
- 但 OpenSSL **不是**标准本身。与它一致**不**等于“已按标准认证”。
- OpenSSL **不通过命令行暴露 SM2MQV**；截至原 M3，这条 SM2 **密钥交换**路径
  没有独立实现对照。2026-10-02 后端 W1 新增本机 BC 1.80 核心结果逐字节对照，
  当前结论与来源边界见本文件下节及 `docs/BACKEND_W1.md`，不追填历史结果。
- **签名互操作成功不得顺带提升密钥交换的证据等级**：两者是不同的计算路径。
- 向量夹具存放在 `internal/cryptosuite/testdata/`，只包含**公开固定输入**
  和一个**仅用于互操作测试的公钥与签名**，不含任何私钥。
# M5 正式实验补充（2026-10-01）

本轮未增加或升级依赖，go.mod/go.sum 保持原样。正式实验使用既有 Go 1.27.1 windows/amd64、GMSM v0.44.1、x/crypto v0.54.0；实际 `go version`、`go list -m all`、`go mod verify` 与 GOSUMDB 见正式冻结日志。不关闭 Go 校验服务。新增 formal 控制、统计与执行脚本使用 Python 标准库，未引入第三方采集器。

## 后端 W1 测试工具核验（2026-10-02）

以下全部是已安装工具的**隔离测试依赖**，没有安装、下载或改变系统环境，
没有加入 Go 模块或 Windows 服务的运行时依赖。`go.mod/go.sum` 未因 W1 改动。

|工具|实测版本|本机来源|用途|
|---|---|---|---|
|JBR/java/javac|OpenJDK `21.0.8`；JBR `21.0.8+1-1038.72-jcef`，build `21.0.8+1-b1038.72`；javac `21.0.8`|`C:/Program Files/JetBrains/PyCharm 2025.2.3/jbr/bin`|编译并运行测试隔离的 BC SM2 交换桥|
|Bouncy Castle Provider|Provider info `BouncyCastle Security Provider v1.80`；Java `getVersionStr()` 返回 `1.8`，与构造版本数字的表示有关；准确组件版本为 **1.80**|现成 PyCharm `lib/bouncy-castle.jar`；随 PyCharm 的 `license/third-party-libraries.json` 也记录 provider/PGP 为 1.80、MIT|SM2 核心共享结果第二实现，**不**执行标准 S1/S2 确认|
|OpenSSL|`OpenSSL 3.5.7 9 Jun 2026`；Msys-x86_64，built `Wed Jun 10 11:53:26 2026 UTC`|现成 `C:/Users/[REDACTED_USER]/.local_workspace/binaries/PortableGit/versions/1.2.0/usr/bin/openssl.exe`|当前动态 SM2 双向签名/验签，以及身份/消息/签名负例|

本地 BC 重打包 JAR 的 SHA-256 为
`cefef94b35315f4b94c2653a99e38c94ca8b312a59dd61b0d5e956839fa858b9`。
该文件没有根 `META-INF/MANIFEST.MF`，不是 BC 官方原始发行 JAR；本机整体哈希
与测试用类哈希用于锁定实际输入，**没有宣称**与官方发行签名/整包哈希一致。
版本来自实测 Provider 信息和已安装分发许可清单，方法核对使用官方
[r1rv80 `SM2KeyExchange` 源码](https://github.com/bcgit/bc-java/blob/r1rv80/core/src/main/java/org/bouncycastle/crypto/agreement/SM2KeyExchange.java)
和 [BC 许可](https://www.bouncycastle.org/about/license/)。许可文本为 MIT 风格，
版权与免责声明保存在 `docs/licenses/bouncycastle-test-LICENSE.txt`。
Review 提供 Java 桥源码、路径/校验定位参数与公开材料来源，**不复制整个 JAR/JBR**。

调用的 BC API 是 `SM2KeyExchange.init(ParametersWithID)`、
`calculateKey(256, ParametersWithID)`，曲线为 `GMNamedCurves.getByName("sm2p256v1")`。
256 的单位是位，与 Go 生产适配器固定 32 字节对齐。当前测试逐字节比较 Go
两端与 BC 两端，并覆盖身份字节、角色、静态与临时材料变化。与独立实现一致
不能写成国密认证、标准 S1/S2 互操作或协议证明；签名和密钥交换结论分别报告。

测试入口 `scripts/backend_w1.py` 只使用指定的现成路径。普通 Go 测试可通过
`EDGEHYBRID_BC_JAR` / `EDGEHYBRID_BC_SHA256` / `EDGEHYBRID_JAVA` /
`EDGEHYBRID_JAVAC` / `EDGEHYBRID_OPENSSL` 传入同一进程级环境。
没有工具或没有配置时，独立测试保持明确 NOT_EXECUTED/配置失败；
不以缺工具、命令未启动或跳过当作通过，不自动安装。
实际命令、stdout/stderr、退出码、源码清单、版本与包/类哈希在
`results/raw/backend_20261002/w1/`。
