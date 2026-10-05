# M2 里程碑报告：传统国密一网关一设备真实安全链路

**项目**：A1-31 园区边缘设备国密与后量子混合安全通信软件设计实现
**本轮范围**：M1.1（证据与表述修补）+ M2（传统套件完整链路）
**本轮明确不做**：混合套件（M3）、密钥生命周期（M4）、性能基准（M5）、网页/数据库

**核验时间**：2026-09-24
**核验机器**：Windows amd64，`go1.27.1 windows/amd64`，MinGW-W64 gcc 8.1.0，OpenSSL 3.5.7

**验收等级**：`M2_FUNCTIONAL_VERIFICATION_PENDING`（见 `results/m2_readiness.json`）

> **为什么不是 `M2_LEGACY_VERIFIED`**：功能、负面测试、真实 SM4 记录层都通过了，
> 但 **race 检查在本机无法执行**（环境阻塞，见第八节），且 **SM2 密钥交换仍缺独立实现对照**。
> 按本轮验收规则，这两项缺口阻止“完整通过”的结论。

**原始证据**（全部为本轮真实输出，退出码见各文件）

| 文件 | 内容 |
|---|---|
| `results/raw/m1_1_verification.txt` | M1.1 命令、退出码、组件交叉核验、race 环境证据 |
| `results/raw/m1_1_tests.jsonl` | M1.1 范围 `go test -json` 原始输出 |
| `results/raw/m1_1_summary.txt` | 上述 JSON 的自动统计 |
| `results/raw/m2_verification.txt` | M2 命令与 `go mod verify` |
| `results/raw/m2_tests.jsonl` | 全项目 `go test -json` 原始输出 |
| `results/raw/m2_summary.txt` | 上述 JSON 的自动统计 |
| `results/raw/m2_negative_cases.jsonl` | 27 条结构化负面用例观测 |
| `results/raw/m2_e2e_events.jsonl` | 真实 TCP 端到端脚本产生的事件流 |
| `results/raw/m2_e2e_summary.json` | 该次运行的结果摘要 |

---

## 一、功能实现状态

### 1.1 已实现（真实密码，非替身）

| 能力 | 实现位置 | 说明 |
|---|---|---|
| SM2 身份签名（GB/T 32918.2） | `internal/cryptosuite/sm2_identity.go` | 定长 r‖s（64 字节），非 ASN.1；强制 UID |
| **SM2 完整密钥协商**（GB/T 32918.3） | `internal/cryptosuite/sm2_agreement.go` | `SM2MQV` + `SM2SharedKey`，**不是**普通 ECDH；含 ZA/ZB 顺序 |
| SM3 / HMAC-SM3 / HKDF-SM3 | `internal/cryptosuite/sm3_hkdf.go` | HKDF 按 RFC 5869 结构 |
| SM4-GCM | `internal/cryptosuite/sm4_gcm.go` | 16 B 密钥 / 12 B nonce / 16 B 标签 |
| ML-KEM-768 适配 | `internal/cryptosuite/mlkem768.go` | Go 标准库 `crypto/mlkem`（FIPS 203） |
| 握手编解码 | `internal/handshake/codec.go` | 规范编码，拒绝未知/重复/尾随 |
| 转录与密钥日程 | `internal/handshake/keyschedule.go` | 一次 Extract、多次 Expand |
| 客户端/服务端状态机 | `internal/handshake/client.go`、`server.go` | 四消息 H1–H4 |
| 安全通道 | `internal/handshake/channel.go` | 真实 SM4-GCM，串行发送，认证后推进 |
| 记录层 | `internal/record/record.go` | 33 B 头 + AAD + 方向 + SID + epoch + seq |
| 信任注册表 | `internal/identity/identity.go` | 严格 JSON，拒绝重复键；吊销 fail-closed |
| 私钥存储 | `internal/identity/keystore.go` | 带身份/key_id/公钥交叉校验的 typed handle |
| 本地可信初始化 | `internal/identity/init.go` + `cmd/secctl` | `init-demo`，不覆盖已有材料 |
| 服务与业务闭环 | `internal/app/service.go` | 遥测 → 控制 → 确认；方向授权；幂等 |
| 命令行 | `cmd/gateway`、`cmd/device` | 按套件门控；hybrid 拒绝启动 |
| 端到端脚本 | `scripts/m2-e2e.sh`、`.ps1` | 系统选端口；等事件而非 sleep |

### 1.2 分模式行为（这是本轮最容易搞错的地方）

| 配置 | `minimum_mode` | 声明套件 | 行为 |
|---|---|---|---|
| `configs/gateway.json` / `device.json`（默认） | `hybrid` | 推导为 `[2]` | **拒绝启动**：`refusing to serve: hybrid (suite 2) is not implemented in this build` |
| `configs/*.traditional.demo.json` | `traditional` | 显式 `[1]` | **正常运行** |
| 声明套件含 2 的任何配置 | 任意 | 含 2 | **拒绝启动**（`ErrUnsupported`），**不降级** |

这是**两个相反错误之间的正确位置**：

- 错误一（永远保留总开关）：本轮**没有**犯——传统链路真实完成后，
  显式传统配置下 `gateway`/`device` 确实能跑。
- 错误二（把 `RequireImplemented()` 改成无条件 nil）：本轮**没有**犯——
  旧的粗粒度占位被替换为**按套件**的门控
  （`cryptosuite.RequireImplemented(suite uint16)` 查 `suiteReadiness` 表），
  套件 1 为 true、套件 2 与未知套件为 false。

### 1.3 本轮修改/新增的文件

**新增实现**：`internal/handshake/{codec,keyschedule,params,channel,client,server}.go`、
`internal/identity/{identity,strictjson,keystore,init}.go`、
`internal/app/{service,wiring}.go`、`internal/cryptosuite/{scalar,validate}.go`

**修改**：`internal/cryptosuite/interfaces.go`（套件门控）、
`internal/wire/frame.go`（`ReadRaw`/`WriteEncoded`）、
`internal/record/record.go`（明文预算）、
`internal/config/config.go`（`supported_suites`、`expected_peer_id`、统一身份校验）、
`internal/app/messages.go`（`Ack` 增加 `Duplicate`/`SamplingIntervalMS`）、
`cmd/{gateway,device,secctl}/main.go`、`go.mod`（`go 1.25.0`）

**新增测试**：`internal/cryptosuite/{hkdf_openssl,sm2_openssl,sm4_gcm_rfc8998}_test.go`、
`internal/handshake/{testutil,codec,keyschedule,tamper,channel}_test.go`、
`internal/identity/identity_test.go`、`internal/record/record_sm4_test.go`、
`internal/app/service_test.go`
（重写：`internal/handshake/handshake_test.go`、`internal/config/config_test.go`）

**新增脚本**：`scripts/m2-e2e.sh`、`scripts/m2-e2e.ps1`、`scripts/summarize-go-test-json.py`
**新增数据**：`internal/cryptosuite/testdata/{hkdf_sm3_openssl,sm2_openssl_interop}.json`
**新增文档**：`docs/M1_ERRATA.md`、`docs/PROTOCOL_REVIEW_M2.md`、
修订 `docs/PROTOCOL_DRAFT.md`、`docs/TEST_MATRIX.csv`、`docs/TEST_MATRIX.md`、
`docs/DEPENDENCIES.md`、`README.md`、`IMPLEMENTATION_STATUS.json`

---

## 二、已执行的测试（真实退出码）

全部命令在项目根目录执行；Go 以绝对路径调用（本机未加入 PATH）。

| # | 命令 | 退出码 | 结果 |
|---|---|---|---|
| 1 | `go version` | 0 | `go1.27.1 windows/amd64` |
| 2 | `go mod verify` | 0 | `all modules verified` |
| 3 | `go vet ./...` | 0 | 无告警 |
| 4 | `go build ./cmd/...` | 0 | 全部构建通过 |
| 5 | `go test ./... -count=1` | 0 | 全部包通过 |
| 6 | `go test -count=1 -json ./internal/cryptosuite/ ./internal/identity/ ./internal/record/ ./internal/wire/ ./internal/config/` | 0 | M1.1 范围；原始 JSON 已存 |
| 7 | `go test -count=1 -json ./...` | 0 | 全项目；原始 JSON 已存 |
| 8 | `bash scripts/m2-e2e.sh results/raw` | **0** | 真实 TCP 端到端 **PASS** |
| 9 | `powershell -File scripts/m2-e2e.ps1` | **0** | PS 版本通过（见第九节说明） |
| 10 | `go test -race ./...` | — | **未执行**，环境阻塞（第八节） |

### 2.1 测试数量（由 JSON 自动统计，非手写）

M1.1 范围（`results/raw/m1_1_summary.txt`）：

```text
packages        : 5 total, 5 ok, 0 failed, 0 without tests
top-level tests : pass=92 fail=0 skip=0
subtests        : pass=86 fail=0 skip=0
```

全项目（`results/raw/m2_summary.txt`）：

```text
packages        : 14 total, 9 ok, 0 failed, 5 without tests
top-level tests : pass=145 fail=0 skip=0
subtests        : pass=166 fail=0 skip=0
negative cases  : 27
```

（`without tests` 是 `cmd/*` 五个只有 `main` 的包。）

### 2.2 真实端到端的一次实测

```text
   gateway is listening on 127.0.0.1:14533
   session id: 75053a2ca7cff6ba (agreed by both sides)
PASS: real TCP handshake, telemetry, control and acknowledgement all succeeded
```

两端各自独立计算出**相同的** `session_id`，这说明它们从真实的 SM2 协商出发，
经同一 HKDF-SM3 日程派生出了相同的会话材料。

---

## 三、未执行 / 跳过的测试

| 项 | 原因 | 状态 |
|---|---|---|
| `go test -race ./...` | 本机 race 运行时环境不兼容（第八节） | **环境阻塞，未执行** |
| WSL / Linux 上的 `-race` | 本机安全策略阻止 `wsl.exe`；无 Docker | **未执行** |
| 混合套件、降级攻击（A11、H04–H08） | 属 M3 | 未执行 |
| 密钥生命周期（K01–K04） | 属 M4 | 未执行 |
| 20 设备并发（N01） | 属 M4；本轮显式限制为单会话服务 | 未执行 |
| 性能基准（B01–B03） | 属 M5 | 未执行 |
| SM2MQV 独立实现对照 | OpenSSL 命令行不暴露 SM2MQV | **不可得** |

### 3.1 失败与被替换的测试

| 测试 | 处置 | 原因 |
|---|---|---|
| 旧 `TestUnimplementedHandshakeFailsClosed`（G01） | **替换**为 `TestUnusablePeerNeverYieldsAChannel` + `TestDefaultHybridModeIsRefusedNotDowngraded` + `TestUnknownAndMalformedSuiteListsRejected` | 函数签名已按显式依赖注入改变。**回归覆盖没有减少**：默认 hybrid 拒绝、未知套件拒绝、传统未认证不返回 Channel 全部保留并加强 |
| `TestSM4GCMNonceReuseIsDetectable` | **改名**为 `TestSM4GCMDistinctNoncesProduceDistinctCiphertexts` | 原名称断言了 GCM 不具备的能力。**未删除**，并新增 `TestSM4GCMNonceReuseIsNotPreventedByTheAEAD` 正面证明该风险 |
| `internal/config/config_test.go` 的 `good` 夹具 | **更新** | 新增必填字段 `expected_peer_id`；同时新增 8 条规则用例 |

**本轮没有删除任何有效测试。**

### 3.2 本轮由测试发现并修复的真实缺陷

| # | 缺陷 | 发现方式 | 修复 |
|---|---|---|---|
| 1 | `Client`/`Server` 在错误路径上解引用 `nil` 连接 → **panic** | `TestConfigurationValidation/nil_connection` | 关闭前判空（`client.go`、`server.go`） |
| 2 | `InitDemo` 把空身份**静默默认**为 `GW001`/`D001` | `TestInitDemoRejectsBadIdentities` | 改为显式拒绝空值与空目录；默认值只留在 CLI 层 |
| 3 | 干净的 peer close 被误报为“违反消息序列” | 首次端到端运行的事件日志 | 新增 `ErrPeerClosed`，与 `ErrProtocol` 区分 |
| 4 | `internal/record` 的明文字节预算与帧长无关联（写死 16384） | 需要按 `max_frame_bytes` 统一口径 | 新增 `record.PlaintextBudget`，单点换算，默认 16333 |

> 缺陷 3 值得单独说明：首轮端到端运行时，网关在设备正常断开后把
> `EOF` 报成了 `peer violated the message sequence or encoding`。
> 这在安全上是“安全失败”，但会把正常结束误报成攻击，属于真实的**可观测性缺陷**。

---

## 四、SM2 向量证据等级

**结论：签名升到“独立实现双向互操作”；密钥交换仍是“上游自有向量”。**

### 4.1 SM2 签名 —— 已与独立实现互操作

证据文件：`internal/cryptosuite/sm2_openssl_test.go` + `testdata/sm2_openssl_interop.json`
对照实现：**OpenSSL 3.5.7**（与本项目依赖无共享代码）

| 方向 | 做法 | 结果 |
|---|---|---|
| 入向 | OpenSSL 生成 SM2 密钥并签发，本实现验签 | **通过** |
| 出向 | 本实现签发（用 OpenSSL 生成的临时密钥导入标量），OpenSSL 验签 | **`Signature Verified Successfully`** |

含义：这覆盖了 **ZA 计算（含 ENTLA、UID）、SM2 曲线参数、SM3 对 ZA‖M 的哈希、
签名的 r‖s 与 DER 互转**。曲线常数错、ZA 错、漏 ENTLA、双重哈希都会让该测试失败。

反向控制也测了：换 UID、改消息、改签名全部必须失败。

### 4.2 SM2 密钥交换 —— 仍为上游向量（未提升）

| 项 | 内容 |
|---|---|
| 现有向量 | `github.com/emmansun/gmsm@v0.44.1/ecdh/ecdh_test.go` 的 3 组 |
| 能证明什么 | 适配器调用的**函数、参数顺序、角色、ZA/ZB 顺序**正确 |
| **不能**证明什么 | 与**独立实现**的一致；与 GB/T 32918.3 附录 A 官方向量的逐字节一致 |
| 为什么没提升 | OpenSSL 命令行不暴露 SM2MQV；本机没有第二个 SM2 密钥交换实现 |
| 状态 | `partially_verified_upstream_vector_pending_standard_vector` |

**不得**因为签名互操作成功就顺带声称密钥交换也已独立验证——两者是不同计算路径。

### 4.3 曲线参数核查

`ecdh.P256()` 返回的是 SM2 推荐曲线群。该结论来自上游实现与其自测，而非本轮新增证明。
**本项目未独立复算曲线参数**，只通过向量与互操作间接覆盖。

---

## 五、HKDF / SM4-GCM 的独立验证

### 5.1 HKDF-SM3 —— 与 OpenSSL 逐字节一致

证据：`internal/cryptosuite/hkdf_openssl_test.go` + `testdata/hkdf_sm3_openssl.json`
基准：`openssl kdf -keylen N -kdfopt digest:SM3 -kdfopt hexkey:K [-kdfopt hexsalt:S] [-kdfopt hexinfo:I] HKDF`

| 用例 | 长度 | 覆盖点 |
|---|---|---|
| RFC 5869 用例 1 结构 | 42 | 基本路径 |
| 空 salt | 42 | RFC “缺省 salt = hashlen 个 0” 规则 |
| 空 info | 32 | 可选 info |
| 块边界 32 | 32 | 恰一个 SM3 块 |
| 跨块 64 | 64 | 两个块，验证计数器拼接 |
| 跨块 96 | 96 | 三个块 |
| 空 salt + 空 info | 48 | 组合边界 |
| **最大长度 8160** | 8160 | 255×32，RFC 5869 的上界 |

**8/8 全部一致**（含 8160 字节）。测试同时覆盖**拆分路径**
（`HKDFExtractSM3` + `HKDFExpandSM3`，握手实际使用的调用方式）与组合路径
（`HKDFSM3`），并断言二者一致。

此外 `TestHKDFSM3OutputLengthBounds` 固定上界：255×32 **接受**，
0 / −1 / 255×32+1 **拒绝**。

### 5.2 SM4-GCM —— 与官方 RFC 8998 A.1 向量逐字节一致

证据：`internal/cryptosuite/sm4_gcm_rfc8998_test.go`
向量来源：RFC 8998 Appendix A.1，2026-09-24 从 `rfc-editor.org/rfc/rfc8998.txt` 回查原文

| 项 | 值 |
|---|---|
| Key | `0123456789abcdeffedcba9876543210` |
| IV（12 字节） | `00001234567800000000abcd` |
| Plaintext | 64 字节（AA/BB/CC/DD/EE/FF/EE/AA 各 16） |
| AAD | `feedfacedeadbeeffeedfacedeadbeefabaddad2` |
| CipherText | `17f399f0…2691233b8d`（64 字节） |
| Tag | `83de3541e4c2b58177e065a9bf7b62ec` |

断言：**密文与标签都逐字节相等**，且官方密文本实现能正确解开。
另含 8 项篡改（密文首/尾字节、标签首/尾字节、截断标签、换 nonce、改 AAD、空 AAD），全部拒绝。

**注意这个向量的意义**：它固定的是**真实生产构造函数**
（`sm4.NewCipher` + `cipher.NewGCM`），不是自洽往返。
若把 SM4 换成 AES，或标签长度变了，该测试立刻失败。

### 5.3 SM4 的三类证据必须分开

| 类别 | 证据 | 强度 |
|---|---|---|
| SM4 分组 | GB/T 32907 标准向量 `681edf34d206965e86b3e94f536e4246` | 标准已知答案 |
| SM4-GCM AEAD | RFC 8998 A.1（上文） | **官方标准向量** |
| 往返/状态机 | 各类 RoundTrip / 篡改 / 边界 | 仅自洽 |

M1 报告把三者混述，已在 `docs/M1_ERRATA.md` E4 修正。

---

## 六、负面测试：27 条结构化用例

全部机器可读记录在 `results/raw/m2_negative_cases.jsonl`，字段含
**被测场景、期望失败阶段、实际失败阶段、是否创建 Channel、是否向业务交付数据**。

| 攻击/异常 | 期望阶段 | 实际阶段 | Channel | 明文交付 |
|---|---|---|---|---|
| 篡改网关签名 SigB | 签名认证 | 签名认证 | 否 | 否 |
| 篡改设备签名 SigA | 签名认证 | 签名认证 | 否 | 否 |
| 篡改 FinishedA | 确认值 | 确认值 | 否 | 否 |
| 篡改 FinishedB | 确认值 | 确认值 | 否 | 否 |
| 替换签名 key_id | 身份/密钥绑定 | 身份/密钥绑定 | 否 | 否 |
| 替换网关身份 | 身份/密钥绑定 | 身份/密钥绑定 | 否 | 否 |
| 把选中套件改成 2 | 消息/编码 | 消息/编码 | 否 | 否 |
| 篡改网关 nonce | 签名认证 | 签名认证 | 否 | 否 |
| 篡改临时公钥（X/Y） | 签名认证 | 签名认证 | 否 | 否 |
| **离曲线点 + 有效签名** | 消息/编码 | **消息/编码** | 否 | 否 |
| 篡改 ClientHello nonce | 签名认证 | 签名认证 | 否 | 否 |
| 传统 hello 夹带 ML-KEM 公钥 | 消息/编码 | 消息/编码 | 否 | 否 |
| 传统 ServerHello 夹带 KEM 密文 | 消息/编码 | 消息/编码 | 否 | 否 |
| ClientAuth 确认了别的套件 | 消息/编码 | 消息/编码 | 否 | 否 |
| ClientAuth 换了设备身份 | 身份/密钥绑定 | 身份/密钥绑定 | 否 | 否 |
| 换协商 key_id | 身份/密钥绑定 | 身份/密钥绑定 | 否 | 否 |
| 重复 ClientHello | 消息/编码 | 消息/编码 | 否 | 否 |
| 丢弃 ServerHello | 消息/编码 | 消息/编码 | 否 | 否 |
| 提前 ProtectedRecord | 消息/编码 | 消息/编码 | 否 | 否 |
| 注入 lab 消息 | 消息/编码 | 消息/编码 | 否 | 否 |
| 尾随数据 | 消息/编码 | 消息/编码 | 否 | 否 |
| 错序消息 | 消息/编码 | 消息/编码 | 否 | 否 |
| 默认 hybrid 配置 | 套件策略 | 套件策略 | 否 | 否 |
| 未登记网关 | 身份/密钥绑定 | 身份/密钥绑定 | 否 | 否 |
| 已吊销网关 | 身份/密钥绑定 | 身份/密钥绑定 | 否 | 否 |
| 角色登记错误 | 身份/密钥绑定 | 身份/密钥绑定 | 否 | 否 |
| 摇测身份不符 / 温度越界 / 间隔越界 | 业务规则 | 业务规则 | 否 | 否 |

### 6.1 两个值得单独说明的用例

**(a) 离曲线点 + 有效签名**（`TestOffCurveEphemeralRejectedDespiteValidSignature`）

其余篡改都被**签名**拦住，这留下一个疑问：如果攻击者**能**对坏点给出有效签名，
还有别的东西拦得住吗？该测试让中继**拿着网关自己的私钥**重新签名一个被改到
曲线外的临时点，使签名有效。结果：**仍被拒绝**，而且是密码层拒绝
（`SM2 key exchange: invalid key material or handles`）。

这同时证明了本项目的分层是自洽的：**编解码器只做长度检查是对的**，
因为密码层确实会做曲线检查，而这条路径确实会被走到。

**(b) 篡改 FinishedB 后的不对称性**（`TestTamperedFinishedBRejected`）

篡改 H4 后，**设备拒绝，而网关已经持有会话** —— 因为网关写完 H4 就无法知道设备是否接受。
这个不对称是四消息流程的真实性质，测试显式记录它，代码中也没有任何
“双方已收到”的结论。

### 6.2 真实 SM4-GCM 记录层的负面覆盖

在 `internal/record/record_sm4_test.go` 与 `internal/handshake/channel_test.go` 中，
用**真实 SM4-GCM**（非 AES 替身）覆盖：

- 记录头 9 个字段逐个篡改 → 拒绝且**不消耗接收状态**
- 密文首/中/尾字节、标签首/尾字节篡改 → 认证失败
- 截断、超长、nil 输入 → 结构性拒绝
- 同会话重放、错序、高序号伪造（0/1/3/4/2^40）→ 拒绝且状态不被污染
- 跨会话重放、方向反射、epoch/suite 不匹配 → 拒绝
- 会话隔离：两次独立握手 SID 不同，且各自可用
- 记录数到限 → `ErrLimit` 并关闭会话，**序号不回退**
- 超预算明文（16333 接受 / 16334 拒绝）
- 并发 24 个 Send → 接收端恰好收到 24 条不重复、顺序正确
- 部分写失败 → 关闭会话，之后不可再 Send
- 上下文取消 → 返回 `context.Canceled`

---

## 七、竞态检测：Windows 与 Linux 各自的状态

| 平台 | 状态 | 说明 |
|---|---|---|
| **Windows amd64（本机）** | **环境阻塞，未执行** | MinGW 导入库与系统 `msvcrt.dll` 不兼容 |
| **Linux（WSL / Docker / 远端 CI）** | **未执行** | `wsl.exe` 被本机安全策略阻止；无 Docker；**未经授权不上传、不运行远端 CI** |

### 7.1 Windows 侧已证实的根因（只写已证实部分）

| 证据 | 结果 |
|---|---|
| 三行无关程序 + **全新 `GOCACHE`** + `-race` | **同样失败** `exit status 0xc0000139` |
| race 产物中的符号 | 同时含 `__iob_func` 与 `msvcrt.dll` |
| `msvcrt.dll` 是否导出 `__iob_func` | **0**（不导出） |
| MinGW `libmsvcrt.a` 是否引用 `__iob_func` | **3**（引用） |
| 直接运行 race 产物 | 退出码 127，**无任何输出** |
| 普通 cgo（**不带** `-race`） | **正常输出 `cgo ok`** |
| `CGO_ENABLED=0` + `-race` | `go: -race requires cgo` |

**结论**：这是 `__iob_func` 缺失导致的 `STATUS_ENTRYPOINT_NOT_FOUND`，
**不是**项目代码问题，**不是**构建缓存污染问题。

> **勘误**：M1 报告第八节曾把原因之一归为“构建缓存污染”。**该归因已被证伪**，
> 见 `docs/M1_ERRATA.md` E1。

### 7.2 未做的事（明确声明）

- **没有**修改或替换系统 DLL；
- **没有**修补 MinGW 归档；
- **没有**关闭 race、关闭 cgo、关闭模块校验或删除并发测试来换取 PASS；
- **没有**把任何平台的 race 结果改写成另一个平台的。

### 7.3 后续闭环方式

1. 在干净 Linux（CI 或独立机器）上执行 `go test -race -count=1 ./...`；
2. 或在 Windows 上换用与 Go 官方 race 兼容的 C 工具链（更新版 MinGW-w64 或 MSVC）；
3. 参见本轮提交的 CI 工作流草案 `.github/workflows/race.yml`（**未上传、未运行**）。

---

## 八、默认模式行为

| 命令 | 结果 |
|---|---|
| `go run ./cmd/gateway`（默认 `configs/gateway.json`） | 退出码 2，`refusing to serve: hybrid (suite 2) is not implemented in this build` |
| `go run ./cmd/device`（默认 `configs/device.json`） | 退出码 2，同上 |
| `go run ./cmd/bench` | 退出码 2，明确拒绝生成虚假结果 |
| `go run ./cmd/lab -devices 20 -messages 3` | 退出码 0，输出 `PLAINTEXT_LOOPBACK_LAB_ONLY_NOT_A_SECURE_CHANNEL` 与 `delivered: 60` |
| `go run ./cmd/gateway -config configs/gateway.traditional.demo.json …` | **正常运行**（真实安全链路） |

**默认配置没有降级。** 明文 lab 仍然带醒目标记，且**不能**被安全入口调用
（它的消息类型在握手中会被 `ErrProtocol` 拒绝，见 A05）。

---

## 九、当前未实现的功能（完整清单）

| 项 | 里程碑 |
|---|---|
| 混合套件（ML-KEM-768 参与建钥）、降级攻击防护、wire 级协商篡改 | M3 |
| 在线吊销传播、长期密钥轮换/退役、活动会话终止、定时/计数换钥 | M4 |
| 一网关 20 设备并发服务、按身份限流、总并发握手上限 | M4 |
| 持久化 operation_id 去重（跨重启 exactly-once） | M4 |
| `cmd/bench` 正式基准（时延/字节/CPU/RSS/吞吐/1000 次握手成功率） | M5 |
| 与公开混合实现（如 Go TLS X25519MLKEM768）的真实对照 | M5 |
| 自动生成篡改用例与可审计证据 | M6 |
| Web 前端、数据库后台、证书颁发服务、0-RTT、会话恢复 | 不在计划内 |
| **race 检查的实际执行** | 环境阻塞，待闭环 |

---

## 十、声明

- 本轮**未**记录任何真实密钥，**未**提交运行时产物或秘密；
  `runtime/` 与 `results/raw/*` 均在 `.gitignore` 中，密钥文件为运行材料而非报告产物。
- 测试夹具只含**公开固定输入**与**仅用于互操作的公钥/签名**，**不含私钥**。
- **未**为了绕过依赖或环境错误而关闭任何校验（模块校验、`GOFLAGS`、`GOSUMDB` 均未改动）。
- **未**伪造任何测试结果、向量或标准一致性结论。
- **未**自动降级，**未**用布尔量冒充完成，**未**声称原型已通过安全审计。
- 事件日志只输出公开元数据；不打印共享秘密、PRK、密钥、随机种子或业务明文。
- 本报告中的每个数字都可由 `results/raw/` 下的原始文件重新推导。

**本原型是自定义协议的工程实现，未经形式化证明，未经安全审计，
不构成合规认证，且本轮不含任何后量子成分参与会话。**
