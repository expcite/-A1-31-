# M1 里程碑报告：真实密码组件集成与验证

> **勘误：本报告的部分表述已被 `docs/M1_ERRATA.md` 收窄或修正。**
> 最重要的一条：第八节关于 `-race` 失败的“构建缓存污染”归因**已被证伪**，
> 请不要继续引用该段。另有多处证据等级需要收窄（Finished 的保证范围、
> ML-KEM 公钥格式检查与密文隐式拒绝的区分、SM4 三类证据的区分、
> GCM nonce 历史、依赖表接收者类型、测试数量改为自动统计）。
> 修订后的证据等级见 `results/milestone_M2.md` 第五节。

**项目**：A1-31 园区边缘设备国密与后量子混合安全通信软件设计实现
**本轮范围**：仅 M0 + M1（环境核验、脚手架复现、真实密码组件的集成与验证）
**明确不在本轮范围**：完整握手、网页、复杂会话恢复、最终性能报告

**核验时间**：2026-09-24
**核验机器**：Windows amd64，`go1.27.1 windows/amd64`
**完整原始日志**：`results/raw/m1_verification.txt`（含每条命令的真实退出码）；M0 基线见 `results/raw/m0_baseline.txt`

---

## 一、总体结论

M1 **组件级**目标已完成：SM2 身份签名、SM2 完整密钥交换、SM3/HMAC-SM3/HKDF-SM3、SM4-GCM、ML-KEM-768 五类适配器均已实现并通过测试（**47 个测试全部 PASS，0 FAIL**）。

**必须同时声明以下未完成事项，不得视为"整个系统可用"**：

- 完整认证握手**未实现**（M2/M3）。
- `RequireImplemented()` **仍返回错误**，gateway / device / bench **仍然 fail-closed**，本轮未翻转、未降级。
- 原型**未经过安全审计**。
- `go test -race` 在本机环境**失败**，属**未解决的环境限制**（详见第八节），**未通过关闭任何校验绕过**。
- SM2 已知答案向量目前只有**上游可追溯向量**，**尚无独立复现的 GB/T 32918 附录 A 官方向量**，该项**标注为待验证**，不声称"已按官方标准向量集验证"。

---

## 二、环境核验

| 项 | 实测值 | 命令 / 来源 |
|---|---|---|
| 操作系统 | Windows amd64 | — |
| Go 版本 | `go1.27.1 windows/amd64` | `go version` |
| 模块路径 | `example.com/edgehybrid` | `go.mod` |
| `go` 指令版本 | `>= 1.25.0` | `go.mod`（原为 1.23.0，因固定依赖需要上调） |
| 模块代理 | `https://goproxy.cn,direct` | `go env GOPROXY` |
| 校验数据库 | `sum.golang.org`（未关闭） | `go env GOSUMDB` |
| `GOFLAGS` | 空（未设置任何跳过校验的开关） | `go env GOFLAGS` |

> Go 1.27.1 满足 `crypto/mlkem` 所需的 Go 1.24+，也满足 gmsm v0.44.1 要求的 Go 1.25+。

---

## 三、修改 / 新增的文件清单（逐项）

### 新增：实现文件（`internal/cryptosuite/`）

| 文件 | 说明 |
|---|---|
| `suite.go` | 域分离与尺寸常量、错误值、`validUserID()` 长度校验（1..8191 字节） |
| `sm3_hkdf.go` | `NewSM3`/`SumSM3`/`HMACSM3`/`SM3KDF`/`HKDFSM3`（RFC 5869 结构）、`Encode` 长度前缀域分离编码、`ConstantTimeEqual` |
| `sm2_identity.go` | SM2 身份签名适配器；ASN.1 ↔ 定长 r‖s（64 字节）互转；固定 UserID |
| `sm2_agreement.go` | SM2 完整密钥交换适配器（`SM2MQV` + `SM2SharedKey`）；静态协商密钥句柄；严格公钥解析；临时点反射检测 |
| `sm4_gcm.go` | `NewSMPrimitives`、`NewSM4GCM`（16 B 密钥 / 12 B nonce / 16 B 标签） |
| `mlkem768.go` | ML-KEM-768 适配器（`crypto/mlkem`）；隐式拒绝语义显式记录 |

### 新增：测试文件（`internal/cryptosuite/`）

| 文件 | 覆盖内容 |
|---|---|
| `sm3_hkdf_test.go` | SM3 已知答案向量、流式一致性、HKDF RFC 5869 结构、域分离、编码无歧义、常量时间比较、输出长度上限、HMAC 密钥长度规则 |
| `sm2_identity_test.go` | 签名往返、错误身份/公钥/消息/签名、畸形输入、句柄销毁、签名随机性、密钥唯一性 |
| `sm2_agreement_test.go` | SM2MQV/SM2SharedKey 上游参考向量、角色互换改变密钥、两端一致、临时点反射拒绝、畸形对端公钥、空 UserID、非法角色、签名密钥不可当协商密钥、静态密钥导入往返、销毁 fail-closed |
| `sm4_gcm_test.go` | SM4 分组已知答案向量、密钥长度、往返与 AAD、篡改拒绝、错误密钥拒绝、nonce 重用可检测、边界长度、非 AES 确认 |
| `mlkem768_test.go` | FIPS 203 参数尺寸、往返、**等长非法密文隐式拒绝**、错误长度密文拒绝、畸形封装密钥、每次封装新鲜性、密钥对唯一性、销毁 fail-closed、错误句柄类型拒绝 |
| `hkdf_rfc5869_test.go` | 仅测试用 SHA-256 HKDF 结构交叉核验辅助 |
| `aes_guard_test.go`、`block_alias_test.go` | AES 替代物防护（防止误用 AES 代替 SM4） |

### 修改

| 文件 | 改动 |
|---|---|
| `go.mod` | `go 1.23.0` → `go 1.25.0`；新增 `require github.com/emmansun/gmsm v0.44.1`、`require golang.org/x/crypto v0.54.0 // indirect` |
| `go.sum` | 新增 gmsm / x/crypto / x/sys 的 `h1:` 与 `/go.mod h1:` 双哈希条目 |
| `docs/DEPENDENCIES.md` | 写入已固定版本、来源、许可证、API 核验、完整性校验、风险检查 |
| `docs/TEST_MATRIX.csv` | C01–C05 状态由 `planned` 更新为真实状态（C01 标注部分验证） |
| `internal/cryptosuite/README.md` | 更新为 M1 实际状态与关键约束 |

### 新增：结果文件

| 文件 | 说明 |
|---|---|
| `results/raw/m1_verification.txt` | 本轮全部命令与**真实退出码**的原始日志 |
| `results/milestone_M1.md` | 本文件 |

### 未改动（关键）

| 文件 | 状态 |
|---|---|
| `internal/cryptosuite/interfaces.go` | **未改动**；`RequireImplemented()` 仍返回 `ErrNotImplemented` |
| `cmd/gateway`、`cmd/device`、`cmd/bench` | **未改动**；仍 fail-closed |

---

## 四、执行的命令与真实退出码

来源：`results/raw/m1_verification.txt`

| # | 命令 | 退出码 | 结果 |
|---|---|---|---|
| 1 | `go version` | 0 | `go1.27.1 windows/amd64` |
| 2 | `go test ./...` | **0** | 全部包通过 |
| 3 | `go test ./internal/cryptosuite/ -count=1 -v` | **0** | 47 PASS / 0 FAIL |
| 4 | `go vet ./...` | **0** | 无告警 |
| 5 | `go build ./cmd/...` | **0** | 全部构建通过 |
| 6 | `go run ./cmd/lab -devices 20 -messages 3` | **0** | `delivered=60`，并打印明文告警 |
| 7 | `go run ./cmd/gateway` | **1**（内部 exit 2） | 拒绝运行：`secure cryptographic backend/handshake is not implemented; refusing operation` |
| 8 | `go run ./cmd/device` | **1**（内部 exit 2） | 同上，拒绝运行 |
| 9 | `go run ./cmd/bench` | **1**（内部 exit 2） | 拒绝运行：`secure benchmark NOT IMPLEMENTED` |
| 10 | `go test -race ./internal/cryptosuite/` | **1** | **环境失败** `exit status 0xc0000139`（见第八节） |

> 说明：第 7–9 项外层退出码 1 来自 `go run` 包装（子进程 exit 2 被 `go run` 转述为 `exit status 2` 并以 1 退出）。**协议层要求的行为是"拒绝运行"，已满足。**

---

## 五、测试通过情况

### 通过（47 / 47）

按组件分组：

- **ML-KEM-768（10）**：FIPS 参数尺寸、往返、等长非法密文隐式拒绝、错误长度拒绝、畸形封装密钥、封装新鲜性、密钥对唯一性、销毁 fail-closed、错误句柄类型拒绝、公钥一致性。
- **SM2 密钥交换（11）**：MQV 与 SharedKey 上游向量、角色互换改变密钥、两端一致、临时点反射拒绝、畸形对端公钥、空 UserID、非法角色、签名密钥不可作协商密钥、静态密钥导入往返、销毁 fail-closed。
- **SM2 身份签名（11）**：往返、错误身份、错误公钥、篡改消息、篡改签名、9 项畸形输入子用例、坏身份拒绝、句柄销毁、签名随机性、密钥唯一性。
- **SM3 / HMAC-SM3 / HKDF（8）**：SM3 已知答案向量、空输入与流式、HKDF RFC 5869 结构、域分离、编码无歧义、常量时间比较、输出长度上限、HMAC 密钥长度规则。
- **SM4-GCM（7）**：SM4 分组已知答案向量、密钥长度、往返与 AAD、篡改拒绝、错误密钥拒绝、nonce 重用可检测、边界长度、非 AES 确认。

### 失败

无。

### 未执行 / 待验证

| 项 | 原因 |
|---|---|
| `go test -race ./...` | 环境不支持（第八节） |
| ML-KEM 的 Finished 层验证 | 依赖 M2/M3 握手，本轮不在范围；相关测试已保留（`TestMLKEM768ImplicitRejectionSameLength` 明确注明"须由后续 Finished 检测"） |
| 端到端握手、降级、密钥生命周期、性能 | M2–M5，本轮明确不做 |

---

## 六、测试向量来源与诚信声明

按任务要求"不得编造官方测试向量"，逐项标注：

| 组件 | 向量来源 | 可信度声明 |
|---|---|---|
| SM3 | GB/T 32905 经典 `abc` 向量：`66c7f0f4...8f4ba8e0` | **标准已知答案**，公开且广泛复现 |
| SM4 分组 | GB/T 32907 标准向量：密钥/明文 `0123456789abcdeffedcba9876543210` → 密文 `681edf34d206965e86b3e94f536e4246` | **标准已知答案** |
| SM2 密钥交换 | `gmsm@v0.44.1/ecdh/ecdh_test.go` 中的上游参考向量（3 组） | **上游可追溯**，但**不是**独立取得的 GB/T 32918.3 附录 A 官方向量 → 该项在 `TEST_MATRIX.csv` 标为 `partially_verified_upstream_vector_pending_standard_vector` |
| HKDF-SM3 | 无独立官方向量；以 **RFC 5869 的 SHA-256 结构**做独立交叉核验（`hkdf_rfc5869_test.go`），确认 Extract/Expand 结构与 label/salt/ikm 域分离行为正确 | **结构性交叉核验**，非 SM3-HKDF 官方向量 → 若需标准一致性结论，**待补独立向量** |
| ML-KEM-768 | FIPS 203 Table 2 参数尺寸；ACVP 风格向量未引入 | **参数级核验**；标准库 `crypto/mlkem` 自身实现正确性由 Go 工具链保证 |

**关于 SM2 向量的明确说明**：本轮 SM2 向量证明的是"我们确实在跑 SM2MQV + SM2SharedKey（含 ZA/ZB 顺序），而不是普通 ECDH"——因为普通 ECDH 产生 32 字节 x 坐标，无法复现 65 字节点编码。但**尚无独立实现的交叉核验**，因此不声称"已按官方标准向量集验证标准一致性"。

---

## 七、本轮的实测行为发现（供后续里程碑使用）

### 发现 1：`crypto/mlkem` 接受全零封装密钥

`mlkem.NewEncapsulationKey768()` 对**全零的 1184 字节**输入**不报错**。原因：FIPS 203 的 `ek` 是字节编码的多项式向量，没有"合法点"校验，全零编码在结构上可正常解码。

**影响**：这是一个格式合法但密码学退化的公钥。Hybrid 握手**不得**把"Encapsulate 成功"当作"对端公钥可靠"的证据，必须依赖认证转录（Finished/确认值）检测该类异常。

**证据**：`internal/cryptosuite/mlkem768_test.go::TestMLKEM768MalformedEncapKeyRejected`，运行日志打印
`CONFIRMED: crypto/mlkem accepts an all-zero 1184-byte encapsulation key from FIPS 203 encoding`。

### 发现 2：ML-KEM 等长非法密文不返回 error

FIPS 203 隐式拒绝：等长但非法的密文返回**另一个共享秘密**且 `err == nil`。已按此语义固定测试断言（断言"值不同"而非"报错"），并保留给后续 Finished 层验证。

**证据**：`TestMLKEM768ImplicitRejectionSameLength`（5 种篡改方式全部验证）。

---

## 八、未解决的问题：`go test -race` 环境失败

**状态：未解决，如实记录，保持安全失败状态。**

**现象**：
```
$ go test -race ./internal/cryptosuite/
exit status 0xc0000139
FAIL	example.com/edgehybrid/internal/cryptosuite	0.877s
```

**已定位的根因**：
MinGW 运行时库 `libmsvcrt.a` 从 `msvcrt.dll` 导入 `__iob_func` 符号，而现代 Windows（Windows 10 及以上）的 `msvcrt.dll` 已不再导出该符号，导致 `-race` 所需的 C 运行时链接失败。

**已尝试的修复**：
用 `dlltool` 重建去除 `__iob_func` 的导入库（`/tmp/racetest/fix/libmsvcrt_fixed.a`），但因 `-o` 输出路径冲突污染了 Go 构建缓存（`C:/Users/20477/AppData/Local/go-build/...`），反复尝试强制使用修正库仍命中缓存中的旧归档。`go clean -cache` 与删除 `GOCACHE` 目录均未能清除该污染产物。

**影响范围**：仅限于 `-race` 竞态检测器。**普通测试、`go vet`、`go build` 全部正常**。

**处理决定**：按任务硬约束"遇到组件验证问题要给出证据与可复现途径，并保持安全失败状态"，**未**通过关闭任何校验或跳过测试来掩盖该问题。M4 的 `N03 并发竞态检查` 项因此在本机环境暂不可执行，需在 CI（Linux amd64）或修复 MinGW 环境后补做。

**复现命令**：`go test -race ./internal/cryptosuite/`

---

## 九、未完成 / 继承到后续里程碑

| 项 | 里程碑 | 说明 |
|---|---|---|
| C01 独立实现交叉核验 / GB/T 32918.3 官方向量 | M1 补 | 当前仅有上游向量，标注 `partially_verified` |
| HKDF-SM3 独立已知答案向量 | M1 补 | 当前仅有 RFC 5869 结构交叉核验 |
| 真实双向认证握手 | M2 | H01–H03 |
| 混合握手、降级防护、Finished 检测 KEM 隐式拒绝 | M3 | H04–H08 |
| 密钥生命周期、并发与竞态检查 | M4 | K01–K05、N01–N03 |
| 传统/混合性能对照 | M5 | B01–B03 |
| `-race` 环境修复或在 CI 补做 | M4 | 见第八节 |

---

## 十、声明

- 本轮**未**记录任何真实密钥，**未**提交运行时产物或秘密。
- **未**为了绕过依赖错误而关闭任何校验（模块校验、`GOFLAGS`、`GOSUMDB` 均未改动）。
- **未**伪造任何测试结果或"官方测试向量"。
- **未**自动降级，**未**翻转任何 readiness 布尔值来宣称完成。
- **未**声称原型已通过安全审计。
- `RequireImplemented()` 仍返回错误；gateway / device / bench 仍 fail-closed。
