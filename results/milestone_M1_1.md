# M1.1 里程碑报告：证据与表述修补

**本轮范围**：只修补 M1 的**证据完整性与表述范围**，以及补上最有价值的组件交叉核验。
**本轮没有**因为措辞问题重写密码层；`internal/cryptosuite` 的生产代码只增加了
存储与校验所需的最小接口（`Scalar` / `Kind` / `Public` / 校验函数）。

**详细勘误条目见 `docs/M1_ERRATA.md`**（本文件是阶段性小结与证据索引）。

---

## 一、目标与做法

M1 交付了五个可用的密码适配器，但它的**证据等级表述过宽**，且有一处
**故障归因是错的**。M1.1 的目标是让报告的每一句话都能被证据支撑。

做法：

1. 用**全新 `GOCACHE`** 重新采集证据，避免历史污染；
2. 把测试数量改为**从 `go test -json` 自动统计**，不再手写；
3. 补上三个**独立来源**的交叉核验（OpenSSL 与 RFC 8998）；
4. 把 race 失败重新定位到**可复现的最小复现**，只保留已证实部分；
5. 修正一个**声称了 GCM 不具备能力**的测试名称。

---

## 二、执行的命令与真实退出码

来源：`results/raw/m1_1_verification.txt`

| # | 命令 | 退出码 | 结果 |
|---|---|---|---|
| 1 | `go version` | 0 | `go1.27.1 windows/amd64` |
| 2 | `go mod verify` | 0 | `all modules verified` |
| 3 | `go vet ./...` | 0 | 无告警 |
| 4 | `go build ./cmd/...` | 0 | 构建通过 |
| 5 | `go test ./internal/cryptosuite/ -count=1 -v` | 0 | 见下 |
| 6 | `go test ./internal/record/ ./internal/identity/ -count=1 -v` | 0 | 见下 |
| 7 | `go test -count=1 -json <M1.1 范围>` | 0 | `results/raw/m1_1_tests.jsonl` |
| 8 | race 最小复现（三行程序 + 全新缓存） | 1 | `exit status 0xc0000139`（**环境**） |
| 9 | 普通 cgo（不带 `-race`）对照 | **0** | `cgo ok` |
| 10 | `CGO_ENABLED=0 go run -race` | 2 | `-race requires cgo` |

M1.1 范围统计（`results/raw/m1_1_summary.txt`，由 JSON 生成）：

```text
packages        : 5 total, 5 ok, 0 failed, 0 without tests
top-level tests : pass=92 fail=0 skip=0
subtests        : pass=86 fail=0 skip=0
```

---

## 三、补上的最有价值组件测试

### 3.1 SM4-GCM：RFC 8998 A.1 官方向量

`internal/cryptosuite/sm4_gcm_rfc8998_test.go`

用**真实生产构造函数**（`sm4.NewCipher` + `cipher.NewGCM`）对官方向量做
Seal/Open，**密文与标签逐字节比对**，另加 8 项篡改（密文首/尾、标签首/尾、
截断标签、换 nonce、改 AAD、空 AAD）全部拒绝。

向量来源：RFC 8998 Appendix A.1，从 `rfc-editor.org/rfc/rfc8998.txt` 回查原文，
来源与生成方式写进测试常量与注释。**没有**把本实现的输出当作期望值。

### 3.2 HKDF-SM3：OpenSSL 独立实现对照

`internal/cryptosuite/hkdf_openssl_test.go` + `testdata/hkdf_sm3_openssl.json`

先确认本机 `openssl version` 与 `openssl list -kdf-algorithms` 确实支持
`HKDF` 与 `SM3`，再用

```text
openssl kdf -keylen N -kdfopt digest:SM3 -kdfopt hexkey:K \
            [-kdfopt hexsalt:S] [-kdfopt hexinfo:I] HKDF
```

生成 **8 组**仅含公开固定输入的向量，覆盖：RFC 5869 用例 1 结构（42）、
空 salt（42）、空 info（32）、块边界（32）、跨块（64）、跨块（96）、
空 salt+空 info（48）、**最大长度 8160**。

测试同时走**拆分路径**（`HKDFExtractSM3` + `HKDFExpandSM3`，握手实际用法）
与组合路径，并断言二者一致。**8/8 逐字节一致。**

明确区分了 Extract / Expand / info / salt / IKM 的编码；没有拿 SHA-256 输出
当作 SM3 的期望值。

### 3.3 SM2：独立实现双向互操作

`internal/cryptosuite/sm2_openssl_test.go` + `testdata/sm2_openssl_interop.json`

| 方向 | 结果 |
|---|---|
| OpenSSL 签发 → 本实现验签 | **通过** |
| 本实现签发 → OpenSSL 验签 | **`Signature Verified Successfully`** |

夹具只含**公钥与签名**，**不含私钥**；出向测试在临时目录生成一次性密钥，
测试结束即消失。负向控制：换 UID、改消息、改签名都必须失败。

**边界**：这只提升 **SM2 签名**的证据等级。**SM2 密钥交换**（SM2MQV）
仍然只有上游自有向量——OpenSSL 命令行不暴露 SM2MQV。
**不得**因为签名互操作成功就顺带声称协商也已独立验证。

### 3.4 ML-KEM：保留既有测试

`internal/cryptosuite/mlkem768_test.go` 未改语义，保留：
错误长度拒绝、**等长非法密文隐式拒绝**（断言共享结果不同，而非断言报错）、
全零封装密钥被接受的实测记录。**没有**修改生产随机数，**没有**导入 Go internal 包。

### 3.5 本阶段没有自写底层原语

新增的只有测试、夹具与证据生成脚本。夹具只进入 `testdata/`，
**不进入 `gateway` / `device` 的依赖**。

---

## 四、测试语义修正

| 项 | 处置 |
|---|---|
| `TestSM4GCMNonceReuseIsDetectable` | **改名**为 `TestSM4GCMDistinctNoncesProduceDistinctCiphertexts`，注释写明原名称错误（GCM **不**检测 nonce 重用）。**未删除**。 |
| 新增 `TestSM4GCMNonceReuseIsNotPreventedByTheAEAD` | 用 `C1⊕C2 == P1⊕P2` 正面证明 AEAD 不提供重用防护 |
| 依赖表接收者类型 | 按 `gmsm@v0.44.1` 源码校正：`SM2MQV` 接收者 `*PrivateKey`；`SM2SharedKey` 接收者 `*PublicKey` |
| 测试数量 | 改为从 `go test -json` 自动统计 |
| 库正确性 / SM2 编码形状 / 官方向量等声明 | 全部收窄到证据支持的程度（见勘误 E8） |

---

## 五、race：从“猜测”到“已证实”

M1 报告把失败部分归因于**构建缓存污染**。M1.1 用最小复现**证伪**了该归因：

```text
# 三行程序，无项目代码，全新 GOCACHE
$ GOCACHE=<fresh> go run -race ./cmd
exit status 0xc0000139
```

已证实的因果链：

```text
race 产物请求 __iob_func  →  系统 msvcrt.dll 不导出该符号  →  STATUS_ENTRYPOINT_NOT_FOUND
（MinGW libmsvcrt.a 仍引用 __iob_func：3 处；msvcrt.dll 导出：0 处）
对照：普通 cgo 正常输出 "cgo ok"；CGO_ENABLED=0 + -race 被 Go 拒绝。
```

**状态：环境阻塞，未执行。** 不是通过，也没有通过关闭任何检查来绕过。

---

## 六、仍未完成

| 项 | 状态 |
|---|---|
| Windows `-race` | 环境阻塞（MinGW 8.1.0 与系统 msvcrt 不兼容） |
| Linux `-race` | 未执行（WSL 被本机安全策略阻止；无 Docker；未授权远端 CI） |
| SM2MQV 独立实现对照 | OpenSSL 命令行不可得 |
| HKDF-SM3 官方标准向量 | 无（RFC 5869 只定义结构，SM3 实例化无官方附录向量）→ 用独立实现对照代替 |
| GB/T 32918.3 附录 A 官方向量 | 未取得；曲线不同者不得硬套 |

---

## 七、结论

M1.1 **没有**改变密码层的实现，但改变了**关于它的说法**：

- 一处错误归因被证伪并纠正；
- 三个组件补上了独立来源的交叉核验（HKDF-SM3 与 SM4-GCM 达到“独立/官方”等级，
  SM2 签名达到“双向互操作”，SM2 密钥交换仍是“上游向量”）；
- 测试数量不再手写；
- race 的表述从“疑似缓存污染”变成“已证实的环境不兼容”。

修订后的完整证据等级表见 `results/milestone_M2.md` 第四节与第五节。
