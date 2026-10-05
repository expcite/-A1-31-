# 本次运行结果报告（M1.1 + M2）

**运行时间**：2026-09-24
**项目**：A1-31 园区边缘设备国密与后量子混合安全通信（EdgeHybrid）
**运行环境**：Windows amd64 / `go1.27.1` / MinGW-W64 gcc 8.1.0 / OpenSSL 3.5.7

---

## 一、总体结论

| 项目 | 结论 |
|---|---|
| 代码能否编译通过 | ✅ 通过 |
| 全部测试能否通过 | ✅ **145 个顶层测试 + 166 个子测试，失败 0** |
| 真实 TCP 端到端能否跑通 | ✅ **通过**（`scripts/m2-e2e.sh` 退出码 0） |
| 攻击/篡改能否被拒绝 | ✅ **27 条负面用例全部在预期阶段被拒绝** |
| 传统国密安全链路 | ✅ **已完成** |
| 混合（后量子）链路 | ❌ **未实现，且被明确拒绝** |
| 竞态检测 `-race` | ⚠️ **环境阻塞，未执行** |

### 判定：**功能合格，完整验证未通过**

准确的状态名是 **`M2_FUNCTIONAL_VERIFICATION_PENDING`**。

**为什么不是"完整通过"**：`-race` 竞态检测在这台机器上**无法运行**（原因见第五节，
已用三行程序证明是环境问题而非代码问题），另外 SM2 密钥交换还缺独立实现对照。
按本轮验收规则，这两项缺口不能靠"功能都通过了"来抵消。

**能不能进入下一阶段（M3）**：可以。缺口阻止的是"完整通过"的结论，
不阻止继续开发。但**部署前或任何"完整通过"的声明前必须闭环**。

---

## 二、这次到底做成了什么

### 2.1 一句话

**一台设备与一台网关之间，已经能通过真实 TCP 跑起一条真正的国密安全链路**：
双向 SM2 身份认证、完整 SM2 密钥协商、HKDF-SM3 派生会话密钥、
真实 SM4-GCM 保护遥测与控制报文，并且所有篡改、替换、重放、乱序都被拒绝。

### 2.2 实测的端到端结果

```text
== 4. start the gateway
   gateway is listening on 127.0.0.1:14533
== 5. write the device configuration and run the device
== 6. wait for the gateway to finish
== 7. verify the observed events
   session id: 75053a2ca7cff6ba (agreed by both sides)
PASS: real TCP handshake, telemetry, control and acknowledgement all succeeded.
```

**关键点**：设备端和网关端**各自独立算出了同一个 `session_id`**。
这意味着它们从真实的 SM2 协商结果出发，经过同一套 HKDF-SM3 日程，
派生出了完全相同的会话密钥——如果任何一步不一致，这里就会是不同的值。

两端各自记录的事件（节选）：

```text
{"role":"device","stage":"handshake","outcome":"established","peer_id":"GW001","session_id":"75053a2ca7cff6ba","suite":1}
{"role":"gateway","stage":"handshake","outcome":"established","peer_id":"D001","session_id":"75053a2ca7cff6ba","suite":1}
{"role":"gateway","stage":"business","outcome":"telemetry-accepted","detail":"sample_id=1"}
{"role":"gateway","stage":"business","outcome":"control-confirmed","detail":"set-interval-D001-1"}
{"role":"device","stage":"business","outcome":"control-applied","detail":"set-interval-D001-1 duplicate=false interval_ms=5000"}
```

### 2.3 数字（全部由脚本从 `go test -json` 自动统计，不是手写）

```text
全项目：
  packages        : 14 total, 9 ok, 0 failed, 5 without tests
  top-level tests : pass=145 fail=0 skip=0
  subtests        : pass=166 fail=0 skip=0
  负面用例        : 27

M1.1 范围（密码组件）：
  packages        : 5 total, 5 ok, 0 failed
  top-level tests : pass=92 fail=0 skip=0
  subtests        : pass=86 fail=0 skip=0
```

### 2.4 三个"独立来源"的交叉核验（这是本轮最实的进步）

之前 M1 的证据只是"代码自己跟自己一致"。本轮补上了**外部对照**：

| 组件 | 对照来源 | 结果 |
|---|---|---|
| **SM4-GCM** | **RFC 8998 附录 A.1 官方向量** | 密文 + 标签**逐字节一致** |
| **HKDF-SM3** | **OpenSSL 3.5.7**（独立实现） | **8 组向量全部一致**，含 8160 字节最大长度 |
| **SM2 签名** | **OpenSSL 3.5.7** 双向互操作 | 他们签 → 我们验 ✅；我们签 → 他们报 `Signature Verified Successfully` ✅ |
| SM2 密钥交换 | — | ⚠️ **仍无独立对照**（OpenSSL 命令行不暴露 SM2MQV） |

> ⚠️ **注意一个容易吹过头的地方**：SM2 **签名**互操作成功，
> **不能**顺带说 SM2 **密钥交换**也独立验证了。两者是不同的计算路径。
> 密钥交换的证据等级仍然是 `partially_verified`。

---

## 三、关键代码

### 3.1 协议流程：为什么这样设计就不会有自引用

设备端（`internal/handshake/client.go`，节选）：

```go
// --- msg1: ClientHello ---
hello := ClientHelloBody{
    OfferedSuites:       r.u16,          // 有序能力列表，原样送出
    InitiatorID:         c.Local.ID,
    InitiatorSignKeyID:  c.Local.SignKeyID,
    InitiatorAgreeKeyID: c.Local.AgreeKeyID,
    ResponderID:         c.ExpectedPeerID,
    Nonce:               na,             // 32 字节，来自 crypto/rand
    EphemeralSM2:        eph.Public,     // 本次握手的临时公钥
    EphemeralKEM:        nil,            // 传统套件：必须为空
}
m1, err := fio.send(wire.ClientHello, hello.encode())   // 返回"实际写出的字节"

// --- msg2: 先验签，再算密钥 ---
coreBytes, sigB, err := splitServerHello(payload2)
core, err := parseServerCore(coreBytes)
...
if err := cryptosuite.NewSM2Identity().Verify(
    peer.Public.SignPublic, peer.Public.SM2UserID,
    serverAuthMessage(m1, coreBytes), sigB,     // 签名覆盖 H1 与 ServerCore，不含自身
); err != nil {
    return nil, fmt.Errorf("%w: gateway signature: %v", ErrAuthentication, err)
}
```

`fio.send` 返回的是**真正写到网线上的那串字节**，直接交给转录；
接收端用 `wire.ReadRaw` 拿回**真正读到的那串字节**。
这样"签的名"和"算的转录"一定是同一份数据，不存在"重新编码后不一致"的可能。

### 3.2 密钥日程：一次 Extract、多次 Expand

`internal/handshake/keyschedule.go`（节选）：

```go
salt  = H(E("edgehybrid/v1/salt", Na, Nb))          // 双方 nonce 都绑定
PRK   = HKDF-Extract-SM3(salt, IKM)                 // ← 只 Extract 一次
T2    = H(E("edgehybrid/v1/t2", H1, H2))
KfinA = Expand(PRK, E("...finished-A", T2, u16(32)), 32)
KfinB = Expand(PRK, E("...finished-B", T2, u16(32)), 32)
T4    = H(E("edgehybrid/v1/t4", H1, H2, H3, H4))    // ← 完整握手之后才有
K_AtoB= Expand(PRK, E("...app-AtoB",  T4, u16(16)), 16)
```

```go
// --- SM2 key exchange and key schedule ---
res, err := ag.Derive(cryptosuite.Initiator, localID, c.Local.AgreeKey, eph, peerID, core.EphemeralSM2)
kikm, err := ikm(core.SelectedSuite, res.SharedSecret, nil)
slt,  err := salt(na, core.Nonce)
prk := cryptosuite.HKDFExtractSM3(slt, kikm)   // 生产代码调用"拆分"版本
t2 := transcriptHashT2(m1, m2)
kfinA, kfinB, err := finishedKeys(prk, t2)
```

**这里有个坑**：库里还有一个组合函数 `HKDFSM3`（一次 Extract + Expand）。
如果每个标签都调它，就会**悄悄重复 Extract**，跟对端派生出不同的密钥。
所以生产代码只 Extract 一次，后面全部用 Expand。

`KfinA/KfinB` 只依赖到 `H2` 为止的转录，业务密钥依赖完整的 `T4` ——
这样 `Finished` 不会依赖它要认证的消息，业务密钥也不会在握手写完之前存在。

### 3.3 防止"悄悄降级"：未实现的套件直接拒绝启动

`internal/cryptosuite/interfaces.go`：

```go
var suiteReadiness = map[uint16]bool{
    SuiteTraditional: true,   // 适配器已实现且握手已集成
    SuiteHybrid:      false,  // ML-KEM 适配器有，但消费它的握手不存在
}

func RequireImplemented(suite uint16) error {
    if ok, known := suiteReadiness[suite]; known && ok {
        return nil
    }
    return ErrNotImplemented
}
```

这是**两个相反错误之间的正确位置**：

- 没有"永远保留总开关" —— 传统链路做完后，显式传统配置下服务真的能跑；
- 也没有"把总开关改成永远 nil" —— 它是**按套件**判断的，
  套件 2 和未知套件仍然被拒绝。

所以默认配置 `configs/gateway.json`（声明 hybrid）的行为是：

```text
$ go run ./cmd/gateway
refusing to serve: hybrid (suite 2) is not implemented in this build.
This build implements the traditional suite only.
```

**它拒绝运行，而不是悄悄退化成传统模式。** 这正是防降级该有的样子。

### 3.4 记录层：一把锁覆盖"取号 → 加密 → 写整帧"

`internal/handshake/channel.go`（节选）：

```go
c.sendMu.Lock()                 // ← 一把发送锁
defer c.sendMu.Unlock()
if err := c.conn.SetWriteDeadline(time.Now().Add(c.timeout)); err != nil { ... }

sealed, err := c.send.Seal(contentType, plaintext)   // ① 分配并消费序号 + 加密
if err != nil {
    _ = c.Close()               // 序号已消费，继续用会重用 nonce → 必须关会话
    return err
}
enc, err := wire.Encode(wire.Frame{Kind: wire.ProtectedRecord, Payload: sealed}, c.maxFrame)
if err := wire.WriteEncoded(c.conn, enc); err != nil {   // ② 写完整帧
    _ = c.Close()               // 部分写失败 → 流状态未知，只能关
    return fmt.Errorf("%w: record write failed: %v", ErrProtocol, err)
}
return nil
```

关键点：

- **序号不回退**。写失败就关会话，绝不用旧密钥重来一遍。
- 接收端只在认证成功后推进：

```go
f, _, err := wire.ReadRaw(c.conn, c.maxFrame)
if err != nil {
    _ = c.Close()
    if errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed) {
        return 0, nil, ErrPeerClosed     // 对端正常关闭 ≠ 协议违规
    }
    return 0, nil, fmt.Errorf("%w: record read failed: %v", ErrProtocol, err)
}
kind, pt, err := c.recv.Open(f.Payload)   // ← 只有这里会推进接收序号
```

### 3.5 篡改测试：为什么能说"不是解析失败挡住的"

中继（`internal/handshake/testutil_test.go`）只动外层帧的载荷，
重新计算长度前缀，所以对端**解析出来的是语法合法的消息**：

```go
func rewritePayload(raw, payload []byte) []byte {
    out := make([]byte, 6+len(payload))
    binary.BigEndian.PutUint32(out[:4], uint32(len(payload)+2))  // 修正长度
    out[4], out[5] = raw[4], raw[5]                              // 保留 version/kind
    copy(out[6:], payload)
    return out
}
```

于是可以精确断言**在哪一个阶段失败**：

```go
o.assertClientRejected(t, "signature_authentication", "tampered gateway signature (SigB)")
o.assertServerRejected(t, "finished_confirmation",    "tampered device Finished (FinishedA)")
o.assertClientRejected(t, "identity_or_key_binding",  "ServerHello with a substituted signing key identifier")
```

而且 `assertClientRejected` 里会先检查 **Channel 是否为 nil**，
所以 `channel_created=false` 是**断言结果**，不是人工填的。

### 3.6 最难的一个用例：离曲线点 + 有效签名

其它篡改都被"签名"挡住了，那就留下一个疑问：
**如果攻击者能对坏点给出有效签名，还有别的东西拦得住吗？**

`TestOffCurveEphemeralRejectedDespiteValidSignature` 让中继**拿着网关自己的私钥**
把改动后的 ServerCore 重新签名，使签名合法：

```go
c.EphemeralSM2[1] ^= 0xFF                      // 把点弄到曲线外
newCore := c.encode()
sig, err := cryptosuite.NewSM2Identity().Sign( // 用网关自己的密钥重新签！
    gw.SignKey, []byte(gw.UID), serverAuthMessage(hello, newCore))
return pass(rewritePayload(raw, append(newCore, sig...)))
```

结果：**仍被拒绝**，而且这次是**密码层**拒绝：

```text
actual_stage="message_sequence_or_encoding"
detail="SM2 key exchange: cryptosuite: invalid key material or handles"
```

这一条同时证明了分层是自洽的：编解码器**只做长度检查是对的**，
因为密码层确实会做曲线检查，而且这条路径确实会被走到。

### 3.7 SM4-GCM 官方向量测试（最关键的一段）

`internal/cryptosuite/sm4_gcm_rfc8998_test.go`：

```go
sealed := aead.Seal(nil, iv, plaintext, aad)          // 真实生产构造函数
gotCT  := sealed[:len(sealed)-SM4TagSize]
gotTag := sealed[len(sealed)-SM4TagSize:]
if !bytes.Equal(gotCT, wantCT) {                      // 逐字节比对密文
    t.Fatalf("ciphertext does not match RFC 8998 A.1\n got  %x\n want %x", gotCT, wantCT)
}
if !bytes.Equal(gotTag, wantTag) {                    // 逐字节比对标签
    t.Fatalf("tag does not match RFC 8998 A.1\n got  %x\n want %x", gotTag, wantTag)
}
```

**这条测试的价值**：它不是"加密再解密看是否相同"（那只证明自洽）。
它比的是**官方文档里的字节**。如果把 SM4 换成 AES、或者标签长度变了，
它会立刻失败。

### 3.8 OpenSSL 双向互操作（签名）

```go
// 出向：我们签，OpenSSL 验
out, err := exec.Command(ossl, "pkeyutl",
    "-verify", "-pubin", "-inkey", pubPath,
    "-rawin", "-digest", "sm3",
    "-pkeyopt", "distid:"+DefaultSM2UserID,
    "-in", msgPath, "-sigfile", sigPath,
).CombinedOutput()
// t.Logf("OpenSSL verdict: %s", ...)  →  Signature Verified Successfully
```

这覆盖了 ZA 计算（含 ENTLA、UID）、曲线参数、SM3 对 ZA‖M 的哈希、
以及 r‖s ↔ DER 互转。任何一环错，都会在这里失败。

---

## 四、这次修掉的 4 个真实缺陷

这 4 个都不是"设计取舍"，是**测试跑出来的真问题**：

| # | 问题 | 怎么发现的 | 修法 |
|---|---|---|---|
| 1 | `Client`/`Server` 在**错误路径上解引用 nil 连接 → panic** | 配置校验测试传了 nil 连接 | 关闭前判空 |
| 2 | `init-demo` 把**空身份静默默认**成 `GW001`/`D001` | 参数校验测试 | 改为显式拒绝；默认值只留在命令行层 |
| 3 | **对端正常关闭被误报成"违反消息序列"** | 第一次端到端运行的事件日志 | 新增 `ErrPeerClosed`，与 `ErrProtocol` 区分 |
| 4 | 记录层明文预算**写死 16384**，与帧长设置脱节 | 需要按 `max_frame_bytes` 统一口径 | 新增 `record.PlaintextBudget`，默认 **16333** |

第 3 个值得多说一句：首轮端到端跑完，网关把设备的正常断开
报成了 `peer violated the message sequence or encoding`。
安全上它"安全失败"没错，但把正常结束误报成攻击，是真实的**可观测性缺陷**。

---

## 五、`-race` 为什么没跑：这台的机器的环境问题

`go test -race` 在本机失败，退出码 `0xc0000139`。

**已经排除的可能**（都用实测排除）：

| 怀疑 | 实测 | 结论 |
|---|---|---|
| 项目代码有问题 | 用一个**只有三行的、和项目无关的**程序，**全新构建缓存** | **同样失败** → 不是代码问题 |
| 构建缓存被污染 | 同上，用的是全新 `GOCACHE` | **同样失败** → 不是缓存问题 |
| C 工具链不可用 | 普通 cgo（**不带** `-race`） | **正常输出 `cgo ok`** → 工具链没问题 |

**已证实的直接原因**：

```text
race 产物请求 __iob_func
        ↓
系统 msvcrt.dll 导出该符号：0 处
MinGW libmsvcrt.a 引用该符号：3 处
        ↓
STATUS_ENTRYPOINT_NOT_FOUND (0xc0000139)
```

**绕不过去**：`CGO_ENABLED=0` 被 Go 明确拒绝（`-race requires cgo`）；
本机唯一的 C 编译器是 MinGW-W64 gcc 8.1.0，没有 clang/MSVC；
WSL 被本机安全策略阻止；没有 Docker。

**我没有做的事**：没有改系统 DLL、没有修补 MinGW 归档、
没有关闭 race/cgo/模块校验、没有删除并发测试来换一个 PASS。

**闭环方式**：在干净的 Linux 上跑 `go test -race ./...`。
CI 草案已写好放在 `.github/workflows/race.yml`，但**没有上传、也没有运行**。

> 顺带纠正一件事：上一轮我把原因部分归为"构建缓存污染"。
> **那个说法是错的**，本轮已用全新缓存 + 三行程序证伪，
> 并在 `docs/M1_ERRATA.md` 里明确标注。保留这条更正记录，避免以后再被引用。

---

## 六、现在还没做的（必须知道）

| 没做 | 属于 |
|---|---|
| **混合套件**（ML-KEM 参与建钥）—— 本轮**完全没有后量子成分**参与会话 | M3 |
| wire 级**降级攻击**测试（改能力列表/选中套件再重编码） | M3 |
| 在线**吊销**传播、长期密钥**轮换**/退役、活动会话终止 | M4 |
| **20 台设备并发**服务（本轮只能一次服务一个会话） | M4 |
| 跨重启 exactly-once（当前只有会话内幂等） | M4 |
| 正式**性能基准**（时延/内存/CPU/1000 次握手成功率） | M5 |
| 与公开混合实现（如 Go TLS X25519MLKEM768）的对照 | M5 |
| **竞态检测实际执行** | 环境阻塞 |

**另外必须说清的三点**：

1. 本轮**没有**任何后量子成分参与会话，所以**不能说整个系统抗量子**。
   后量子适配器（ML-KEM-768）存在且有测试，但**没有握手消费它**。
2. 这是**自定义协议**，不是标准协议，**没有**形式化安全证明，**没有**经过安全审计。
3. Windows 上 `os.Chmod` 无法设置 ACL，所以 `init-demo` 会明确返回
   `os_permission_restriction_enforced: false` —— **不能**在 Windows 上声称密钥目录已被访问控制隔离。

---

## 七、你可以自己跑的命令

```bash
GO="C:/Users/20477/.workbuddy/binaries/go/go/bin/go.exe"
cd "C:/Users/20477/WorkBuddy/密码技术竞赛/edgehybrid-agent-starter"

# 1. 全部测试
"$GO" test ./... -count=1

# 2. 创建本地测试身份（写入 runtime/，已被 git 忽略）
"$GO" run ./cmd/secctl init-demo -dir runtime

# 3. 真实端到端（推荐先跑这个看效果）
GO="$GO" bash scripts/m2-e2e.sh

# 4. 手工跑一网关一设备
"$GO" run ./cmd/gateway -config configs/gateway.traditional.demo.json -max-sessions 1
"$GO" run ./cmd/device  -config configs/device.traditional.demo.json  -samples 3

# 5. 查看默认配置的行为（应当拒绝启动）
"$GO" run ./cmd/gateway

# 6. 状态与登记表
"$GO" run ./cmd/secctl status
"$GO" run ./cmd/secctl check-trust -trust runtime/trust.json
```

### 正常应该看到什么

- 第 3 步结尾：`PASS: real TCP handshake, telemetry, control and acknowledgement all succeeded.`，退出码 **0**
- 第 4 步：两端各出现一条 `"outcome":"established"`，且 **`session_id` 相同**
- 第 5 步：`refusing to serve: hybrid (suite 2) is not implemented in this build`，退出码 **2**
- 第 1 步：全是 `ok`，没有 `FAIL`

---

## 八、关键文件索引

| 想看什么 | 打开 |
|---|---|
| **本报告** | `results/RUN_REPORT_M1_1_M2.md` |
| 主里程碑报告 | `results/milestone_M2.md` |
| 机器可读验收等级 | `results/m2_readiness.json` |
| M1 勘误（含 race 归因纠正） | `docs/M1_ERRATA.md` |
| 协议冻结与审查 | `docs/PROTOCOL_REVIEW_M2.md` |
| 27 条负面用例原始记录 | `results/raw/m2_negative_cases.jsonl` |
| 端到端事件流 | `results/raw/m2_e2e_events.jsonl` |
| 测试原始 JSON 与统计 | `results/raw/m2_tests.jsonl`、`m2_summary.txt` |
| 全部命令与退出码 | `results/raw/m2_verification.txt`、`m1_1_verification.txt` |
| 测试状态映射表 | `docs/TEST_MATRIX.csv` |
| 上手说明 | `README.md` |

---

## 九、声明

- 本轮**没有**记录任何真实密钥，**没有**提交运行时产物或秘密。
- **没有**为了绕过依赖或环境错误关闭任何校验。
- **没有**伪造测试结果、向量或"官方标准一致性"结论。
- **没有**自动降级，**没有**用布尔量冒充完成，**没有**声称已通过安全审计。
- 报告中的每个数字都可由 `results/raw/` 下的原始文件重新推导。

**一句话总结**：传统国密链路是真的跑通了，攻击也是真的都被挡住了；
但竞态检测在这台机器上跑不了，所以这次的结论是"功能合格、完整验证待闭环"，
而不是"全部通过"。
