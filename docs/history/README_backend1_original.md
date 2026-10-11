<!-- M6 current index -->
# 当前交付状态 M6

M5正式实验与M6测试改进/最终材料已完成。最新事实以 [PACKAGE_INFO.md](PACKAGE_INFO.md)、[最终设计说明](docs/FINAL_DESIGN_REPORT.md)、[M6报告](docs/INNOVATION_REPORT.md) 和 results/m6_readiness.json 为准。下文保留原阶段说明，含历史未完成状态，不能据此覆盖最新证据。Demo与M5原始文件未修改，race与独立SM2交换仍pending。

---

> **当前阶段：M5 正式实验。** 当前规则见 [FORMAL_EXPERIMENT_PLAN](docs/FORMAL_EXPERIMENT_PLAN.md)，入口和复现命令见本文件末尾“M5 正式实验入口”。完成状态仅取自 results/milestone_M5.md 和 results/m5_formal_readiness.json。以下 M5A/M5B 的锁定状态及旧数值属于历史记录；Windows Demo ZIP 保持原样，不进入 M6。

/# EdgeHybrid Agent Starter

M5B state: T/H (SM2 \u00b1 ML-KEM-768) measurement harness with resource/throughput/P comparison smokes; formal 1000-trial sampling locked.
也不是完整的安全通信系统。**

当前里程碑：**M5A-R2（测量器/证据收口）**。Suite 1 / Suite 2、生命周期与 20 设备联调已经实现；`cmd/bench` 已实现**小规模 smoke 测量**，但正式 1000 次性能实验仍锁定。

本次审查补丁修复了结果验收与证据收尾中的确定问题：严格 JSON 对象解析、模式与套件绑定、完整 SID 语法、事件持久化错误传播，以及“先停止事件生产者再关闭事件文件”的顺序。由于当前审查环境只有 Go 1.23.2，而项目要求 Go >=1.25，**补丁后的完整工程仍需在原开发机重新执行 go test/vet/build、Suite 1/2 E2E 与 smoke 验证后，才可进入正式 M5 采样。**

`-race` 仍是环境阻塞；SM2 密钥交换的独立第二实现对照仍待补。这些验证缺口不能被写成 PASS。

---

## 一分钟了解现在能跑什么

| 能力 | 状态 |
|---|---|
| SM2 签名 / SM2 完整密钥协商 / SM3 / HMAC-SM3 / HKDF-SM3 / SM4-GCM | **已实现并测试** |
| ML-KEM-768 适配（Go 标准库） | **已实现并测试**（组件级） |
| 传统套件（Suite 1）双向认证握手 | **已实现并测试** |
| 真实 SM4-GCM 记录层、串行发送、反重放、会话隔离 | **已实现并测试** |
| 本地可信身份初始化（`secctl init-demo`） | **已实现** |
| 一网关一设备的真实 TCP 联调脚本 | **已实现并通过** |
| **混合套件（Suite 2 / ML-KEM 参与建钥）** | **已实现并开放**（M3 起默认配置即可运行） |
| 密钥轮换 / 吊销 / 20 设备并发 | **已实现并测试**（M4，M4.1/M4.2 收口） |
| 性能测量（M5） | **smoke 已实现**（传统/混合小规模验证）；正式 1000 次、CPU/RSS、吞吐与公开混合对照仍未完成 |
| `-race` 竞态检测 | **环境阻塞，未执行**（见下文） |

**默认配置继续保持 `minimum_mode=hybrid`，且 Suite 2 已实现。** 混合握手失败时不得自动回退到传统模式；显式传统配置仍用于存量设备兼容。当前尚未完成的是 M5 的正式性能采样与对照，而不是混合协议本身。

---

## M2.1 修补了什么（在 M2 之上）

| 修补项 | M2 的问题 | M2.1 的做法 |
|---|---|---|
| 取消 | `Send`/`Receive` 只在加锁前检查一次 ctx；等待锁、阻塞读写都无法被取消 | 用**可取消的信号量**替代 `sync.Mutex`；读写通过过期 deadline 被立即打断；被取消的排队者**不会**继续发送 |
| 握手总超时 | `frameIO` 每帧重置 `time.Now().Add(timeout)`，外层 deadline 管不住实际网络读写 | 整个 H1–H4 使用**一个绝对截止时间**（取调用方 deadline 与本地预算中更早者），后续消息**不可续期** |
| 分帧 EOF | 长度前缀读完但正文全缺时返回普通 `io.EOF`，被当成"对端正常关闭" | 已开始的帧不完整一律报 `ErrTruncated`（并满足 `errors.Is(err, io.ErrUnexpectedEOF)`）；只有"下一帧一字节未收"才是干净关闭 |
| 测试汇总器 | 空、乱码、截断输入都退出 0，把不可用证据当成成功 | 非空、严格 JSON、字段类型、生命周期闭合、预期包集合、**真实 go 退出码**全部检查；不可用证据退出 2，失败运行退出 1 |
| 端到端验收 | 只用 grep/sed 数事件、只看单端身份 | `scripts/verify_e2e_events.py` 为 shell 与 PowerShell **共用**的严格验收器；校验双方角色/身份/Suite 1/**完整 32 位十六进制 SID**/遥测-控制-ACK 一一对应 |
| 结果保存 | 失败时可能不建输出目录或残留旧 PASS | 每次运行独立目录 `results/raw/e2e/<run_id>/`，先写 `RUNNING`，结束写本次 `PASS`/`FAIL`/`INTERRUPTED` |
| 观测口径 | 用**一个** `false` 概括两个端点；终态无条件写 `DeliveredPlaintext: true` | 分端点记录 `device_channel_created` / `gateway_channel_created`；计数按实测；未知值写 `null` |
| 换行符 | `m2-e2e.sh` 是 CRLF，Linux 上 `bash -n` 退出 2 | 全部文本文件规范化为 **LF**，并加入 `.gitattributes` |
| run_id | 事件没有可归属的运行标识 | `gateway`/`device` 支持 `-run-id`，每个事件都带该标识 |

## M4 做了什么（在 M3 之上）

协议与密钥日程**未改动**。新增的是生命周期、换钥与多设备能力。

| 项 | 做法 |
|---|---|
| **动态设备集** | 网关按 `map[device_id]*SessionState` 维护；`cmd/fleet -devices N` 与 `secctl init-demo -devices N` 都是参数化的。**代码中没有任何逐设备分支**，20 只是传入的验收规模 |
| **生命周期状态** | `ACTIVE` / `ROTATING` / `REVOKED` / `RETIRED`（见 `docs/LIFECYCLE.md`）。空值按 `ACTIVE`，M4 前的注册表可直接加载 |
| **吊销** | 握手前 reload + 周期 sweep（250ms）。**不是**"下次启动读一次"。状态持久化，reconnect 无法恢复 |
| **轮换** | 注册表**原子提交**；密钥文件按 **failure-safe** 顺序写入新世代（按 key_id 命名，不先删旧文件），提交后 best-effort 退役旧文件。必须同时提供新 key_id **与新公钥** |
| **换钥** | 完整新握手，不做 in-band rekey。触发：消息数阈值、时间阈值。新 SID + 新记录密钥 + 新 nonce 前缀 + seq 从 0 |
| **重放** | 旧会话记录在新会话下全部拒绝；跨设备记录（载荷声称他人身份）拒绝并终止会话 |
| **资源边界** | 并发握手/会话上限、单身份上限、超时、帧长、每会话 deadline。是**避免无界增长**，不是抗 DoS |

### 常用命令

```bash
# 20 台真实 Suite 2 设备联调（含换钥）
MODE=hybrid bash scripts/m2-e2e.sh           # 单设备
DEVICES=20 SESSIONS=3 REKEY_RECORDS=4 bash scripts/m4-fleet-e2e.sh
DEVICES=20 BAD_DEVICE=D010 bash scripts/m4-fleet-e2e.sh   # 19 good + 1 bad

# 批量登记设备
go run ./cmd/secctl init-demo -dir runtime -devices 20 -id-pattern 'D%03d'
```

---

## M3 做了什么（在 M2.1 之上）

**Suite 2（SM2 + ML-KEM-768 混合）已完整实现并开放。**

保留未动：H1–H4 四消息结构、双向 SM2 签名、完整 SM2 密钥协商、HKDF-SM3、
FinishedA/B、SM4-GCM 记录层、身份注册表与 key_id 绑定，
以及 M2.1 的取消/超时/截断/事件验收/三态观测。

只增加 Suite 2 所需的数据与逻辑：

| 项 | 做法 |
|---|---|
| H1 | 设备为**本次握手**生成新的 ML-KEM-768 密钥对，`ekA` 随 Hello 发出（每次不同、非全零） |
| H2 | 网关**真实 Encapsulate** 得到 `Zpq` 与 `kem_ct`；`kem_ct` 进入 ServerCore，因此被 SigB 认证 |
| 设备收到 H2 | 按选中套件**真实 Decapsulate** 得到本地 `Zpq` |
| IKM | 真实依赖 `Zsm2` **和** `Zpq`；禁止只用其一、XOR、截断、填零、复用 |

协商必须区分**"声明支持套件 2"** 与 **"最终选中套件 2"**：
声明含 2 → `ekA` 必须存在且尺寸正确；选中 2 → `kem_ct` 必须存在且尺寸正确；
选中 1 → `kem_ct` 必须为空，且 `ikm` **显式拒绝**任何 PQ 秘密。
即使最终走套件 1，H1 已发出的能力列表与 `ekA` **仍然是认证转录的一部分**，
不得删除或规范化；双方**都不得**对能力列表排序后再参与转录。

### 防降级不是只靠 `minimum_mode`

三个机制叠加：客户端**重算**应得套件并比对、能力列表进入转录、`kem_ct` 进入转录。
九个 wire 级场景全部被拒（见 `results/raw/m3_downgrade_cases.jsonl`），
其中三个是**合法编码、正确长度**，真正失败在**签名/转录绑定**阶段。

### ML-KEM 隐式拒绝由 Finished 捕获

等长非法密文**不会**报错，只会给出另一个共享秘密。实测：单元级证明两边 `Zpq` 不同且无 error；
握手级在**转录完全相同**的前提下，最终在 **`finished_confirmation`** 阶段被拒。

### 禁止自动回退

混合握手一旦失败就**直接失败**，不允许 `try hybrid → 失败 → 自动重连传统`。
失败时只发出**一次** H1（测试按帧计数固定）。传统连接必须从一开始就是
由本地可信迁移策略明确允许的套件 1 连接。

### Suite 2 的开放条件

`suiteReadiness[SuiteHybrid]` 在七项条件全部满足**之前**一直是 `false`。
满足后开放，默认 `configs/gateway.json` / `device.json`（`minimum_mode=hybrid`）
**真正运行 Suite 2**；默认配置**没有**被改成 traditional 来制造 PASS
（由 `TestShippedDefaultConfigurationsSelectSuiteTwo` 固定）。

---

## 已知局限（不要当成已实现的功能）

- **控制只更新参数状态，没有按周期驱动采样。** 设备收到"设置采样周期"后
  更新一个变量并回 ACK，**没有**任何定时器按该周期去采样。
  当前演示证明的是**参数状态更新**，不是真实定时采样。
- **同一 `operation_id` 配不同参数会回 `Duplicate=true` 且不改变已生效值。**
  这是 `internal/app/service_test.go` 里**明确固定**的现行契约，
  记录为局限，而不是被悄悄重写的业务语义。跨重启 exactly-once 仍未实现。
- `docs/` 与 `results/` 中的 **M1 / M1.1 / M2 报告是历史材料**，
  引用前请先看 `docs/M1_ERRATA.md` 与 `results/milestone_M2_1.md`。
- 项目根目录的 `MANIFEST.sha256` 是**原始 Starter 骨架的 47 文件清单**，
  不是当前源码清单，**不能**用它校验收到的审查包。
- **`ekA` 跨会话复用在响应端不可检测**，只能拦住全零这类退化情形。已如实记录，不谎称已防御。
- **Suite 2 真实存在 ≠ 系统已抗量子**：SM2 身份认证不是后量子身份认证。
  Suite 2 只是让会话密钥同时依赖 SM2 与 ML-KEM-768 两份秘密。
- **`go test -race` 仍未在本机执行**（环境阻塞），这**不等于**项目不存在数据竞争。
- **SM2 密钥交换仍缺独立实现对照**（OpenSSL 命令行不暴露 SM2MQV）。

## 快速开始

前置：Go 1.25 或更新的受维护版本（本项目用 `go1.27.1` 验证）。
`go` 需要在 `PATH` 上（脚本可用 `GO=/path/to/go` 覆盖）。

### 1. 创建本地测试身份

```bash
go run ./cmd/secctl init-demo -dir runtime
```

这会在 `runtime/`（已被 `.gitignore` 忽略）下生成：
一个网关和一台设备各自的**独立签名密钥**与**独立静态协商密钥**，
以及只含公钥的 `runtime/trust.json`。

**这不是在线注册**，不信任握手报文自带公钥。私钥只留在本地密钥目录，
从不打印、不写日志、不进 Git。

### 2. 跑一次真实的端到端联调

```bash
# Linux / macOS / Git Bash
bash scripts/m2-e2e.sh

# Windows PowerShell
powershell -ExecutionPolicy Bypass -File scripts\m2-e2e.ps1
```

脚本会：隔离构建 → 生成全新测试身份 → 用**操作系统选定的端口**启动真实网关 →
**等待网关自己的“已监听”事件**（而不是 sleep）→ 运行真实设备完成
握手 → 遥测 → 控制 → 确认 → 校验事件 → 清理进程。**任何失败都会非零退出。**

### 3. 手工运行

```bash
# 终端 A：网关（用一个显式允许传统模式的配置）
go run ./cmd/gateway -config configs/gateway.traditional.demo.json -max-sessions 1

# 终端 B：设备
go run ./cmd/device -config configs/device.traditional.demo.json -samples 3
```

看到成对出现的 `"stage":"handshake","outcome":"established"` 且两端
`session_id` 相同，就说明真实握手成功了。

### 4. 常用检查

```bash
go test ./... -count=1          # 全部测试
go vet ./...                    # 静态检查
go build ./cmd/...              # 构建
go run ./cmd/secctl status      # 分组件 / 分模式状态
go run ./cmd/secctl check-trust -trust runtime/trust.json
go run ./cmd/lab -devices 20 -messages 3   # 明文实验（不是安全联调）
```

---

## 目录

```text
cmd/
  gateway/     安全网关：并发服务 K 台设备；hybrid 为默认模式
  device/      安全设备：同上
  secctl/      status / check-config / check-trust / init-demo
  bench/       安全基准入口；仍拒绝生成虚假结果（M5）
  lab/         明文回环实验（有醒目标记，不是安全通道）
internal/
  cryptosuite/ SM2/SM3/SM4/ML-KEM 适配层（不含自写底层算法）
  handshake/   编解码、转录、密钥日程、客户端/服务端状态机、安全通道
  identity/    文件信任库、私钥存储、本地初始化
  record/      记录头、AAD、方向、会话、序号、真实 SM4-GCM
  wire/        有界分帧
  config/      严格配置解析
  policy/      模式与套件策略
  app/         业务数据结构与服务循环
configs/       不含私钥的示例配置（含 traditional demo）
scripts/       端到端脚本与测试结果汇总器
docs/          需求、协议草案、协议审查、勘误、测试矩阵、依赖、威胁模型、来源
results/       真实运行记录与阶段报告（milestone_M1.md / milestone_M2.md）
```

---

## 安全设计的硬约束（不要绕过）

- **不自写底层密码算法。** 只做接口适配、KDF 编码与协议状态逻辑。
- **SM2 协商不是普通 ECDH。** 必须调用完整协商，含 ZA/ZB 顺序与身份绑定。
- **签名密钥与协商密钥分开。** 二者字节编码形状相同，靠信任注册表与带类型的句柄区分。
- **`Finished` 只证明双方对同一转录达成一致**，不证明对方公钥诚实生成、也不证明随机性质量。
- **ML-KEM 等长非法密文不返回 error**（FIPS 203 隐式拒绝），必须由 Finished 拒绝。
  这与“公钥格式检查”是两件事。
- **GCM 不提供 nonce 重用防护**，由会话层结构化保证：方向专用前缀 + 严格递增序号 + 上下行独立密钥。
- **认证和密钥确认完成前，不向业务交付明文、不发送应用记录。**
- **接收序号只在 AEAD 验证成功后推进**；失败关闭会话，序号不回退。
- **默认 hybrid 配置不得降级。** 当前默认配置即运行 Suite 2；不实现的能力就是拒绝，而不是悄悄降级。

---

## 如何判断工作能不能接受

必须同时给出：修改文件、实际执行命令、返回码、测试结果、未完成项和风险。
不能只看“编译成功”“连接成功”。

- `docs/TEST_MATRIX.csv`：题目要求 → 用例映射与**真实状态**
- `docs/M1_ERRATA.md`：M1 阶段的表述勘误（含一处被证伪的故障归因）
- `docs/PROTOCOL_REVIEW_M2.md`：协议冻结与审查
- `results/milestone_M2.md`：本阶段报告
- `results/m2_readiness.json`：机器可读的验收等级

---

## 已知限制（必须如实转述）

1. **`-race` 未执行。** 三个独立证据表明这是**环境问题**而非代码问题：
   三行无关程序在全新构建缓存下同样失败；race 产物请求 `msvcrt.dll` 并未导出的
   `__iob_func`；普通 cgo 正常。详见 `docs/M1_ERRATA.md` E1。
   因此状态是 `M2_FUNCTIONAL_VERIFICATION_PENDING`，**不是** `M2_LEGACY_VERIFIED`。
2. **SM2 密钥交换没有独立实现对照。** 签名已与 OpenSSL 双向互操作，
   但 OpenSSL 命令行不暴露 SM2MQV。该组件的证据等级仍为 `partially_verified`。
3. **混合模式完全没有实现。** 没有任何后量子成分参与会话，因此
   **不能**声称整个系统抗量子。
4. **未做**：0-RTT、会话恢复、跨重启 exactly-once、在线吊销传播、长期密钥轮换、
   20 设备并发服务、正式性能基准。
5. **Windows 上的密钥文件权限不由本工具强制。** `os.Chmod` 无法设置 ACL，
   `init-demo` 会返回 `os_permission_restriction_enforced: false`。
6. **原型未经安全审计，没有形式化安全证明。** 集成测试不能替代安全证明。

**项目声明：自定义握手仍需独立的协议审查与实现审查；本工程没有给出整个系统的形式化安全证明。**
# Windows 一键演示

本轮用户优先交付最小演示。运行包 EdgeHybrid_Demo_Windows.zip 中双击 EdgeHybridDemo.exe（默认 1 台）或固定 20 台启动入口，自动打开本机只读状态页；不需要安装开发工具。使用、停止和日志位置见 docs/DEMO_QUICKSTART.md。实际验收与未完成项见 DEMO_TEST_REPORT.md。真实 Suite2、模拟业务，不展示正式性能成绩。
# M5 正式实验入口（2026-10-01）

本轮保持已交付 Windows Demo ZIP 及哈希，结束界面开发。正式测量按 [冻结实验方案](docs/FORMAL_EXPERIMENT_PLAN.md) 执行：T/H/P 各 1000 次独立尝试；T/H 各 5×100 分角色资源；T/H/P 各 5×200 真实业务吞吐。失败不替换，中断不改分母。下文 M5A/M5B 的“formal 锁定”等文字保留为历史阶段记录，以本节和新方案为当前入口说明。

`cmd/bench` 测量 T/H；`cmd/pbench` 测量公开 TLS 混合对照 P；两者的 `-run-kind formal` 只接受冻结参数、显式 run_id 和源码 SHA-256。旧 smoke 继续标为非正式。`scripts/m5_formal_prepare.py` 完整回归并冻结源码/二进制，`m5_formal_run.py` 顺序执行固定矩阵，`m5_formal_report.py` 调用生产验收器并从 raw 重算报告，`m5_formal_controls.py` 在副本上检查篡改拒绝。实际成功、失败、未执行情况以 `results/milestone_M5.md`、`results/m5_formal_readiness.json` 和原始命令为准；代码入口存在不等于实验已完成。

复现实例（在解压源码目录执行，使用本机 Go/Python 路径；脚本中的 Go 路径可按实际工具链调整，这将形成新的源码版本）：

```powershell
python scripts/m5_formal_prepare.py --out results/raw/m5_formal_20261001/freeze --bin-dir ../EdgeHybrid_M5_Formal_Binaries_20261001
python scripts/m5_formal_run.py --freeze results/raw/m5_formal_20261001/freeze/freeze.json --out results/raw/m5_formal_20261001/data
python scripts/m5_formal_report.py --freeze results/raw/m5_formal_20261001/freeze/freeze.json --data results/raw/m5_formal_20261001/data --out results/raw/m5_formal_20261001/report
```

这不是密码合规认证或安全证明。单次握手 CPU/RSS 与 P 分角色资源未支持时输出 null+原因，整批资源不得冒称单次指标。Linux/race/独立 SM2 交换验证历史缺口保留。完成 M5 后停止，不进入 M6。
