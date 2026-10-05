# M4 里程碑报告：生命周期、20 台真实混合设备与并发健壮性

**项目**：A1-31 园区边缘设备国密与后量子混合安全通信软件设计实现
**本轮范围**：M3.1 清理 + M4（生命周期 / 换钥 / 重连 / K 台设备 / 并发）
**本轮不做**：M5 benchmark、1000 次握手性能实验、与 Go TLS 混合方案对比、GUI/Web、数据库、0-RTT、会话恢复

**run_id**：`m4-20260925T090942Z-final`
**源码清单**：`results/raw/m4_source_manifest.sha256`（118 个文件，
清单自身 SHA-256 `75e992c07e3dfa0d2c4b45cf7b77332d006569fd0717e9c87906b1aa43d8c36d`）
**核验机器**：Windows amd64，`go1.27.1`，MinGW-W64 gcc 8.1.0，OpenSSL 3.5.7

**验收等级**：`M4_LIFECYCLE_VERIFIED_PENDING`

> **为什么不是"完整通过"**：20 台真实 Suite 2 联调、吊销、轮换、换钥、重放拒绝、
> 跨设备隔离、异常设备不拖垮其他设备，以及 Suite 1/Suite2/防降级/隐式拒绝全部回归通过。
> 但 **`-race` 仍未执行**（环境阻塞），**SM2 密钥交换仍缺独立对照**。

---

## 一、M3.1 清理（先完成）

### 1. 报告不再手写 session_id

`results/milestone_M3.md` 与 `results/m3_readiness.json` 原先直接复制了随机 SID，
与原始 summary 已经发生漂移。**SID 每次运行都是新值，手工复制必然过期。**

改为：报告一律引用 `results/raw/*_summary.json` 的 `session_id` 字段。
已用脚本校验两个文件中**不再存在任何 32 位十六进制字面量**。

### 2. 生产代码不再有测试钩子

| 原钩子 | 处置 |
|---|---|
| `kemCiphertextOverride`（server.go 包级变量） | **删除**。改为**可注入的 KEM 后端**：`Config.KEM`，默认标准库适配器 |
| `suiteImplemented`（params.go 包级变量） | **删除**。Suite 2 已开放，该覆盖已无必要，直接调用真实门控 |

注入点只写在 `_test.go`：测试通过 `Config.KEM` 传入一个包装器（`kem_inject_test.go`），
该包装器**先真实 Encapsulate**，再替换返回的密文。因此：

- 真实 Encapsulate / Decapsulate / 签名 / Finished **全部保留**；
- 隐式拒绝测试仍落在 **`finished_confirmation`**（实测确认）；
- 生产二进制中**不存在任何可被重新赋值的包级变量**。

---

## 二、20 台是否全部完成真实 Suite 2 —— **是**

`scripts/m4-fleet-e2e.sh` + `scripts/verify_fleet_events.py` 的规范结果
（`results/raw/m4_20device_summary.json`）：

```
result                : PASS
expected_devices      : 20
devices_authenticated : 20
hybrid_sessions       : 20
kem_used_count        : 20
unique_sid_count      : 60        ← 20 台 × 3 次会话（换钥开启）
telemetry_accepted    : 180       ← 20 × 3 样本 × 3 会话
controls_confirmed    : 180
selected_suite        : 2
checks                : 13 / 13 ok
```

每台设备都是真实：SM2 双向认证 + 完整 SM2 密钥协商 + 真实 ML-KEM-768 + HKDF-SM3 + SM4-GCM。
**没有明文模拟。**

验收断言包括：每设备唯一 id、唯一 32 位十六进制 SID、`suite == 2`、`kem_used == true`、
SID 无重复、`operation_id` 归属正确、遥测与控制计数正确。

---

## 三、一般 K 个设备（不写死 20）

**代码中不存在任何逐设备分支**——没有 `D001`、`D010` 之类的字面量开关。

- 网关维护 `map[device_id]*SessionState`（线程安全，`internal/app/sessions.go`）；
- 每台设备独立拥有：可信身份、当前密钥世代、当前会话 SID、收发记录状态、吊销状态、生命周期状态；
- **不共享**会话计数器、AEAD 状态、记录密钥、nonce 前缀、序列号。

`secctl init-demo -devices N -id-pattern 'D%03d'` 与 `cmd/fleet -devices N` 都是参数化的：
同一套代码服务 1 台或 20 台或 K 台。20 只是传入的验收规模值。

**隔离硬保证**：`Register` 拒绝同一 device_id 的第二个活跃会话，
因此两个连接不可能都认为它们拥有同一台设备的状态。

---

## 四、吊销怎么实现

状态：`REVOKED`（与 `RETIRED` 分开，见 `docs/LIFECYCLE.md`）。

**机制不是"下次启动时读一次 trust.json"**，而是两处保证：

1. **握手前 reload**：`serve` 每次握手前调用 `Registry.Reload()` →
   吊销/轮换在**下一次连接尝试**时即生效；
2. **周期 sweep**：`watchLifecycle` 每 250ms reload 并 `SweepRevoked` →
   处理**变更发生前就已建立**的会话。

reload 失败保留旧视图（半写的注册表不能一次性踢掉所有设备）。
**吊销状态持久化**（原子写文件，写入前用同一解析器自检），
因此 **reconnect 无法恢复**。

### 实测（K01，3 条结构化用例）

| 场景 | 结果 |
|---|---|
| 吊销后新连接 | `refused` |
| 吊销后重连 | `refused` |
| 吊销前已建立的会话 | 被终止（`session_terminated`） |
| 其他设备 | 不受影响 |

---

## 五、轮换怎么实现

**策略：原子替换**（不做双密钥过渡窗口）。

```
generation N → 生成+持久化(N+1) → ApplyRotation：装新 key_id 与新公钥，立刻退役 N 的 key_id
```

选择原子替换的理由：为一个身份同时保留两个可用世代会把可攻击面翻倍，
并让事后"这条消息用哪把密钥签的"含糊。更简单、更可审计。

**轮换必须同时携带公钥**：只装 key_id 而留下旧公钥，
会让注册表声称一把密钥文件里并不存在的密钥（这是本轮实测踩到的 bug，已修）。

**退役按 key_id 判定**：`Peer.UsableForNewSessions(signKeyID, agreementKeyID)` 是唯一判定点，
注册表查找与握手绑定检查都用它，不可能给出不同答案。

**不变式持续成立**：签名密钥与协商密钥分离、轮换不得合并两类、私钥不入日志、
key_id 参与绑定、旧 key_id 退役后不能建新会话。

### 实测（K02，2 条结构化用例 + 2 条 identity 层用例）

| 场景 | 结果 |
|---|---|
| 退役 key_id 提议新会话 | `refused` |
| 新世代建立会话 | `established` |
| 世代递增 / 持久化后重新加载 | 通过（`identity_test.go`） |
| `Retire` 只删指定 key_id，不误删新密钥 | 通过 |

---

## 六、换钥怎么实现

**首版不做 in-band rekey。换钥 = 一次完整新握手：**

```
达到阈值 → REKEY_REQUIRED，停止发送业务记录 → 关闭旧会话
        → 完整 H1..H4 → 新 SID / 新记录密钥 / 新 nonce 前缀 / seq 从 0 → 继续业务
```

触发条件：`RekeyPolicy.MaxRecords`（消息数）与 `RekeyPolicy.MaxAge`（时间），至少配一个。
测试用很小阈值（4 条记录 / 1ms）以便自动触发。
计数器只属于**当前会话**。

### SID / key / nonce 新鲜性证据

| 证据 | 来源 |
|---|---|
| **old SID ≠ new SID，且均为完整 32 hex** | K03/K04：3 次连续会话得到 **3 个互不相同**的全长 SID |
| **新记录密钥 ≠ 旧记录密钥** | 整个密钥日程用**新转录**重跑（转录绑定测试 + 密钥日程测试） |
| **新 nonce 前缀 ≠ 旧** | 同一 PRK 按 T4 派生，T4 变了 |
| **即使 seq 回到 0 也不存在 `(key, nonce)` 重用** | 密钥与前缀**同时**换新 |
| **旧会话记录在新会话下失败** | K05 |

Channel 接口**刻意不暴露**记录密钥与 nonce 前缀（那会削弱封装），因此新鲜性用上述**可观测后果**证明，而不是打印密钥。

### 20 台规模下同样成立

`DEVICES=20 SESSIONS=3 REKEY_RECORDS=4` → **60 个唯一 SID**（20 台 × 3 会话）。

---

## 七、旧记录跨会话重放 / 跨设备记录

| 场景 | 结果 |
|---|---|
| Session A 的记录重放到 Session B | **全部拒绝**（A 的通道已关闭，B 的密钥与前缀都是新的） |
| 重放首条 / 中间 / 最后一条 | 均拒绝 |
| **D001 的合法记录送给 D002 的会话** | **拒绝**并终止会话（网关把载荷 `device_id` 与**已认证** peer 绑定） |

用例记录在 `results/raw/m4_isolation_cases.jsonl` 与 `K05` 测试中。

---

## 八、安全重连

- 正常断线 → 完整重新握手 → 新会话；
- 断线时有未确认控制 → 记为**未完成**，业务侧不声称已确认；
- 旧连接残留记录到达 → 拒绝；
- seq 从 0，但 SID / 密钥 / nonce 前缀**全部新鲜**；
- **混合失败不自动回退传统**（M3 的 no-fallback 测试继续保留并通过）。

---

## 九、19 good + 1 bad 场景结果

`DEVICES=20 BAD_DEVICE=D010`：

```
VERDICT: PASS — 19 devices, suite 2, all with the KEM, 19 unique session ids
  the deliberately bad device D010 failed        ✅
```

D010 使用不可信身份（注册表里没有）→ **失败**；
其余 **19 台全部成功**。证明一台异常设备不能拖垮其他设备。

---

## 十、测试数量与失败数量

由 `go test -json` 自动统计（`scripts/summarize-go-test-json.py`），非手写：

```text
packages              : 15 total, 9 ok, 0 failed, 6 without tests
top-level tests       : pass=193 fail=0 skip=0
subtests              : pass=188 fail=0 skip=0
negative cases        : 37
m4 cases              : 8   (revocation 3, rotation 2, rekey 2, isolation 1)
wire-level attacks    : 10  (9 downgrade + 1 implicit rejection)
go test exit code     : 0
VERDICT: PASS (exit 0) — evidence complete, expected packages covered, go exit code 0
```

**失败数量：0**（当前源码上没有任何失败测试）。
`results/raw/e2e/` 中确实保留了历史的一次 **FAIL** 目录，那是**验收器正确工作的证据**，不是当前缺陷。

37 条负面用例的阶段分布（证明不是只剩长度校验）：
`message_sequence_or_encoding` 16、`signature_authentication` 10、
`identity_or_key_binding` 7、`finished_confirmation` 3、`suite_policy` 1。

### 本轮被测试抓出的真实缺陷（均已修）

1. **轮换只装 key_id 不装公钥** → 注册表与密钥文件绑定失败；
2. **吊销不被运行中的网关观测**（两个独立 registry 实例）→ 改为握手前 reload + 持久化；
3. **`Retire` 当时未实现**（返回 "M4 task"）→ 已实现，且带 keyID 防误删校验；
4. **并发化后 `MaxSessions` 停止条件丢失** → 单设备 E2E 会挂到超时；
5. 汇总器 `m4cases` 未绑定到 `main()`（自测抓到）；
6. 测试发送非 JSON 遥测被网关正确拒绝（测试自身问题）。

---

## 十一、race 状态

**`ENVIRONMENT_BLOCKED` —— 未执行。**

纯 Go（无 cgo）三行程序 + 全新 `GOCACHE` 复现 `exit status 0xc0000139`
（MinGW 导入库请求 `msvcrt.dll` 未导出的 `__iob_func`）。
本轮新增的并发测试是**功能性**的，**不得**当作 race detector PASS。

另：SM2 密钥交换仍缺独立实现对照（OpenSSL 命令行不暴露 SM2MQV）。

---

## 十二、readiness

**`M4_LIFECYCLE_VERIFIED_PENDING`**

| # | 完成条件 | 状态 |
|---|---|---|
| 1 | 20 台真实 Suite 2 联调成功 | ✅ |
| 2 | 每设备独立 SID / state | ✅（60 唯一 SID） |
| 3 | 吊销生效 | ✅ |
| 4 | 密钥轮换/退役生效 | ✅ |
| 5 | rekey 产生新 SID/key/prefix | ✅ |
| 6 | 旧记录跨 session 被拒绝 | ✅ |
| 7 | cross-device 记录被拒绝 | ✅ |
| 8 | 异常设备不拖垮其他设备 | ✅（19 + 1） |
| 9 | Suite 1/Suite 2/M3 防降级全部回归 | ✅ |
| 10 | 所有结果有原始机器可读证据 | ✅ |
| — | race 通过 | ❌ 环境阻塞 → 等级带 `PENDING` |

---

## 十三、声明

- 本轮**未**记录或提交任何 SM2 / ML-KEM 私钥、会话密钥、随机种子或真实业务数据。
- **未**关闭任何校验（模块校验、cgo、race、并发测试均未关闭）。
- **未**因为 M4 完成就把 race 或 SM2 独立对照标成通过。
- 所有数字可由 `results/raw/` 下的原始文件重算。
- **Suite 2 存在 ≠ 系统抗量子**：SM2 身份认证不是后量子认证。
- **20 台联调的耗时是联调耗时，不是性能数据。**
- 资源边界（并发握手/会话上限等）**不是抗 DoS 论证**。
- 密钥销毁是 **best effort**，不是安全擦除。
- **跨进程重启 exactly-once 未实现**（本轮刻意不引入数据库）。

**完成 M4，停止，不进入 M5。**
