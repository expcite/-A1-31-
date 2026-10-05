# M1 报告勘误

**本文档修正 `results/milestone_M1.md` 中过宽的表述、错误的归因和手写的测试数量。**
它不推翻 M1 的实现结论：五个密码适配器仍然存在并通过测试。
被修正的是**证据等级的说法**，以及一处**错误的故障归因**。

原始 M1 报告保留原样，以保留历史。维护中的文档已在
`internal/cryptosuite/README.md` 与 `docs/DEPENDENCIES.md` 中加上本文链接。

修正后的证据等级见 `results/milestone_M2.md` 的第五节。

---

## E1（重要）：race 失败的根因归因错误

**M1 报告写的**：`go test -race` 失败源于 MinGW 的 `libmsvcrt.a` 导入 `__iob_func`，
并且**“`-o` 输出冲突污染了 Go 构建缓存”**，导致修正后的库无法生效。

**实际证实的**：**缓存污染的说法是错的。** 本轮用一个**完全全新的 `GOCACHE`** 和
一个**只有三行的、与项目无关的 Go 程序**复现了同样的失败：

```
$ GOCACHE=<fresh dir> go run -race ./cmd
exit status 0xc0000139
exit=1
```

因此：

| 曾被怀疑的原因 | 实测结论 |
|---|---|
| 项目代码有竞态或链接问题 | **否**。三行程序同样失败。 |
| Go 构建缓存被污染 | **否**。全新缓存的干净目录同样失败。 |
| MinGW 导入库与系统 `msvcrt.dll` 不兼容 | **是**。见下。 |

已证实的直接原因（每条都有对应命令输出，见 `results/raw/m1_1_verification.txt`）：

1. `-race` 产物中同时出现符号 `__iob_func` 与 `msvcrt.dll`。
2. `msvcrt.dll` 导出 `__iob_func` 的次数为 **0**。
3. MinGW 的 `libmsvcrt.a` 引用 `__iob_func` 的次数为 **3**。
4. 直接运行该产物：退出码 127，**无任何输出**（进程未能启动）。
5. `CGO_ENABLED=0` 不可作为绕过：Go 明确报 `-race requires cgo`。
6. **对照实验**：普通 cgo（不含 `-race`）正常输出 `cgo ok`，
   说明 C 工具链本身可用，**只有 race 运行时这一条路径**失败。

本机唯一的 C 编译器是 MinGW-W64 gcc 8.1.0；没有 clang、没有 MSVC；
WSL 被本机安全策略阻止；没有 Docker。因此**本轮仍未取得 race 结果**。

**结论**：`results/milestone_M1.md` 第八节中“构建缓存污染”一段应视为
**已被证伪的猜测**，不得继续引用。race 检查是**环境阻塞**，不是通过。

---

## E2：`Finished` 不保证公钥诚实生成，也不保证随机性质量

**M1 报告容易导致的误读**：既然 `Finished`/确认值能发现等长非法 ML-KEM 密文，
那么“握手通过”就说明对方的公钥是好的。

**修正**：`Finished` 只证明**双方对同一段转录达成了共识**。它**不能**证明：

- 对端公钥是**诚实生成**的（可能是一个攻击者特意挑选的、他掌握私钥的弱公钥）；
- 对端随机数**质量合格**（可能是低熵、可预测的）；
- 对端实现没有**保留**临时私钥或共享秘密。

本轮实测到的一个具体例子：`crypto/mlkem` **接受**全零的 1184 字节封装密钥
（FIPS 203 的 `ek` 只是字节编码的多项式向量，没有“合法点”校验）。
见 `internal/cryptosuite/mlkem768_test.go::TestMLKEM768MalformedEncapKeyRejected`。
这类“格式合法但密码学退化”的公钥，只能靠握手之后的认证转录发现，
而且即使发现，也只是“双方一致”，不是“对方的密钥可信”。

**不得**把“`Finished` 校验通过”写成“对端公钥可信”或“随机性合格”。

---

## E3：ML-KEM 的“公钥格式检查”与“密文隐式拒绝”是两件不同的事

M1 报告把两者放在同一节里叙述，容易被读成同一机制。它们是分开的：

| | 公钥格式检查 | 密文隐式拒绝 |
|---|---|---|
| 对象 | 对端送来的**封装密钥** `ek` | 对端送来的**密文** `ct` |
| 行为 | 长度/编码不合规 → **报错** | 等长但非法 → **不报错**，返回**另一个**共享秘密 |
| 谁负责 | 编解码与适配层 | 只能由握手的 Finished/确认值负责 |
| 依据 | `NewEncapsulationKey768` | FIPS 203 隐式拒绝语义 |
| 测试 | `TestMLKEM768MalformedEncapKeyRejected` | `TestMLKEM768ImplicitRejectionSameLength` |

并且要注意 E2：**公钥格式检查通过 ≠ 公钥可信**。全零 `ek` 能通过格式检查。

---

## E4：SM4 的证据必须分三类列出

M1 报告把它们混在一起讲。三类证据的强度完全不同：

| 类别 | 具体证据 | 强度 |
|---|---|---|
| SM4 **分组**密码 | GB/T 32907 经典已知答案向量：密钥/明文 `0123456789abcdeffedcba9876543210` → 密文 `681edf34d206965e86b3e94f536e4246` | 标准已知答案，公开可复现 |
| SM4-**GCM** AEAD | **RFC 8998 附录 A.1 官方向量**，密文与标签逐字节比对（含 8 项篡改用例） | **官方标准向量** |
| 往返与状态机 | 各自 RoundTrip / 篡改 / 边界测试 | 只证明自洽，**不是**标准一致性证据 |

M1 报告说“SM4 分组已知答案向量”时没有区分这三类，容易让人以为
GCM 构造也已被官方向量验证。本轮补上了 RFC 8998 A.1 向量（见
`internal/cryptosuite/sm4_gcm_rfc8998_test.go`），现在这句话是有依据的。

---

## E5：GCM 不保存任何全局 nonce 历史

**M1 报告中的一个测试名 `TestSM4GCMNonceReuseIsDetectable` 是错的。**
GCM **不会**检测 nonce 重用：它没有 nonce 历史，无法区分新 nonce 和重复 nonce。
重用同一 (key, nonce) 对会**静默地**破坏机密性与真实性。

处理方式（未删除任何有效测试）：

- 重命名为 `TestSM4GCMDistinctNoncesProduceDistinctCiphertexts`，
  并在注释中写明原名称错误、以及它实际断言的是什么。
- 新增 `TestSM4GCMNonceReuseIsNotPreventedByTheAEAD`，用 `C1⊕C2 == P1⊕P2`
  这一可验证的事实**正面证明** AEAD 不提供重用防护。
- 明确写下：重用防护是**会话/记录层**的责任，靠
  `nonce = 方向专用 4 字节前缀 || 严格递增 8 字节序号`、上下行独立密钥、
  以及“换钥必换新会话、绝不在旧密钥上重置序号”来结构化保证。

---

## E6：依赖表中的接收者类型已按源码校正

M1 报告里对 `SM2MQV` / `SM2SharedKey` 的接收者的描述不够精确。以
`github.com/emmansun/gmsm@v0.44.1` 的实际源码为准：

| 接口 | 接收者 | 定义位置 |
|---|---|---|
| `(*PrivateKey).SM2MQV(eLocal *PrivateKey, sRemote, eRemote *PublicKey) (*PublicKey, error)` | **`*PrivateKey`** | `ecdh/ecdh.go:169` |
| `(*PublicKey).SM2SharedKey(isResponder bool, kenLen int, sPub, sRemote *PublicKey, uid, remoteUID []byte) ([]byte, error)` | **`*PublicKey`** | `ecdh/ecdh.go:117` |

也就是说：`SM2MQV` 的接收者是**本方的静态私钥**（`*PrivateKey`），
返回 MQV 点（`*PublicKey`）；`SM2SharedKey` 的接收者是**上一步返回的那个
`*PublicKey` 点**，不是私钥。本项目适配器的角色映射因此是：

- 设备为 initiator → `isResponder = false`
- 网关为 responder → `isResponder = true`

并在 responder 侧传 `sPub = 本方静态公钥`、`sRemote = 对端静态公钥`、
`uid = 本方 UID`、`remoteUID = 对端 UID`。该映射由
`internal/cryptosuite/sm2_agreement_test.go::TestSM2SharedKeyKnownAnswerVector`
与 `TestSM2RoleSwapChangesDerivedKey` 固定。

---

## E7：测试数量不再手写

M1 报告写“47 个测试全部 PASS”。**手写的数量会随代码漂移**，因此从现在起：

- 数量由 `scripts/summarize-go-test-json.py` 从 `go test -json` 输出**自动统计**；
- 原始 JSON 保存在 `results/raw/m2_tests.jsonl`；
- 统计结果保存在 `results/raw/m2_summary.txt`。

本轮的实测数字（从 JSON 生成，非手写）：

```
packages        : 14 total, 9 ok, 0 failed, 5 without tests
top-level tests : pass=145 fail=0 skip=0
subtests        : pass=166 fail=0 skip=0
negative cases  : 27
```

（`without tests` 是 `cmd/*` 五个只有 `main` 的包。）

---

## E8：需要收窄的公开声明

以下说法在原 M1 报告中出现过或被暗示，现按证据收窄：

| 原说法 | 收窄后 |
|---|---|
| “SM2 已知答案向量通过” | 通过的是**上游 gmsm 自己的** `ecdh_test.go` 向量。它固定了“调用顺序正确”，但**不是**独立实现交叉核验。 |
| “HKDF-SM3 已按 RFC 5869 结构核验” | 结构核验**不等于**标准一致性。本轮补上了 **OpenSSL 3.5.7 独立实现**的 8 组对照（含 8160 字节最大长度、空 salt、空 info），见 E9。 |
| “SM4-GCM 正确” | 现在有 **RFC 8998 A.1 官方标准向量**支撑（E4）。 |
| “库正确性由上游保证” | 上游库是**依赖**，不是本项目的证明。本项目的证据只覆盖**适配器调用方式**与**协议构造**。 |
| “SM2 编码形状相同” | 签名公钥与协商公钥**都是未压缩 SM2 点，字节长度相同**，因此**编码无法区分二者**。角色区分必须靠信任注册表与带类型的句柄（`Kind()`），不能靠编码。 |
| “原型已实现安全通信” | 原型**未经过安全审计**，**没有**形式化安全证明，**不是**合规认证。 |

---

## E9：本轮新增的独立交叉核验（修正证据等级）

| 组件 | 原证据等级 | 现证据等级 | 依据 |
|---|---|---|---|
| SM3 | 标准已知答案向量 | 标准已知答案向量（不变） | GB/T 32905 `abc` 向量 |
| HMAC-SM3 / HKDF-SM3 | 仅 RFC 5869 结构性交叉核验 | **独立实现交叉核验** | OpenSSL 3.5.7 `kdf HKDF digest:SM3`，8 组向量逐字节一致（`internal/cryptosuite/testdata/hkdf_sm3_openssl.json`） |
| SM4 分组 | 标准已知答案向量 | 标准已知答案向量（不变） | GB/T 32907 |
| SM4-GCM | 往返与自洽测试 | **官方标准向量** | RFC 8998 A.1，密文与标签逐字节一致 |
| SM2 **签名** | 上游自有向量 | **独立实现双向互操作** | OpenSSL 3.5.7 签发 → 本实现验签通过；本实现签发 → OpenSSL 报 `Signature Verified Successfully`（`internal/cryptosuite/sm2_openssl_test.go`） |
| SM2 **密钥交换** | 上游自有向量 | **仍为上游自有向量**（未提升） | OpenSSL 命令行不暴露 SM2MQV，本机无第二个实现可比。状态仍为 `partially_verified`。 |
| ML-KEM-768 | FIPS 203 参数尺寸 + 行为测试 | 不变 | Go 标准库实现；未引入 ACVP 向量 |

**重要**：SM2 **签名**的互操作成功**不能**顺带提升 SM2 **密钥交换**的证据等级。
两者用的是不同的计算路径，签名互操作只覆盖了 ZA 计算与 SM2 曲线参数。
所以 `docs/TEST_MATRIX.csv` 中 C01 的状态仍然是
`partially_verified_upstream_vector_pending_standard_vector`。

---

## E10：本轮修订的仍然不能说的话

即使有了 E9 的补充，以下仍然**不成立**：

- 整个系统达到“全面抗量子”。SM2 身份认证不是后量子身份认证；
  本轮**没有**实现任何混合握手（套件 2 被明确拒绝）。
- 项目的自定义握手是“标准协议”。它是**本项目设计**，
  见 `docs/PROTOCOL_REVIEW_M2.md` 的“不声称事项”。
- 通过功能测试等于通过安全审计或形式化证明。
- race 检查已通过（见 E1）。
