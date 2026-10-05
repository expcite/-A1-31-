# M2.1 里程碑报告：取消、超时、分帧与验收器修补

**项目**：A1-31 园区边缘设备国密与后量子混合安全通信软件设计实现
**本轮范围**：只做 M2.1 —— 修 M2 审查中确认的缺陷、补回归、补证据
**本轮明确不做**：M3 混合套件、网页、20 设备扩展、数据库、正式性能基准

**run_id**：`m21-20260924T120236Z`
**核验机器**：Windows amd64，`go1.27.1`，MinGW-W64 gcc 8.1.0，OpenSSL 3.5.7
**源码清单**：`results/raw/m2_1_source_manifest.sha256`（102 个文件，
清单自身 SHA-256 见 `results/raw/m2_1_source_manifest.json`）

**验收等级**：`M2_1_FUNCTIONAL_FIXES_VERIFIED_PENDING`（见 `results/m2_1_readiness.json`）

> **为什么不是"完整通过"**：M2.1 的修复全部落地且回归通过、传统端到端已被严格验收器
> 在**两端**验证；但 **race 检测器在本机无法运行**，
> 且 **SM2 密钥交换仍无独立实现对照**。
> **环境阻止 race 不等于项目不存在竞态** —— 这两件事不能互相替代。

---

## 一、审查结论的核对（先确认审查针对的就是当前源码）

审查包给出的 9 个文件 SHA-256 与本机**逐一完全一致**，
说明审查针对的就是当前源码，没有版本漂移，可以直接修补：

| 文件 | 一致 |
|---|---|
| `internal/handshake/channel.go` | ✅ |
| `internal/handshake/params.go` | ✅ |
| `internal/handshake/channel_test.go` | ✅ |
| `internal/handshake/testutil_test.go` | ✅ |
| `internal/wire/frame.go` | ✅ |
| `internal/app/service.go` | ✅ |
| `internal/app/service_test.go` | ✅ |
| `scripts/summarize-go-test-json.py` | ✅ |
| `scripts/m2-e2e.sh` | ✅ |

---

## 二、逐项修补

### R1 取消与整个握手超时（A）

**M2 的实际问题**（源码确认）：

- `Send`/`Receive` 只在**加锁前**检查一次 `ctx`，之后 `sync.Mutex.Lock()`
  不可中断；获取锁后也没有再检查。
- 实际读写用的是 `time.Now().Add(c.timeout)`，**没有**取调用方更早的 deadline，
  也没有在已经阻塞时监听取消。
- 握手 `frameIO` **每帧**重置 timeout，外层传入的 `WithTimeout` 管不住真实网络读写。

**M2.1 的做法**：

| 点 | 实现 |
|---|---|
| 等待序列化权限可取消 | 新增 `semaphore`（容量 1 的 channel）替代 `sync.Mutex`；`acquire(ctx)` 在 `ctx.Done()` 时返回 `ctx.Err()`，且**不占用槽位** |
| 排队者被取消后不得发送 | 取消发生在 acquire 阶段 → 直接返回，**没有** Seal、**没有** 写、**没有** 消费序号 |
| 已阻塞读写的取消 | `armIO` 注册 `context.AfterFunc`，命中时把该方向的 deadline 设为**当前时刻**，使阻塞中的 Read/Write 立即返回 |
| 相邻操作不被误伤 | 每个方向一个**代数计数器**；`stop()` 先自增代数再 `stopFn()`。因为 `AfterFunc` 的 stop **不等待已启动的回调**，只有代数仍匹配的回调才会动 deadline |
| 读写 deadline 互不干扰 | 分开使用 `SetReadDeadline` / `SetWriteDeadline`（不用 `SetDeadline`），发送的回调不会打断并发的接收 |
| 握手总预算 | `handshakeDeadline(ctx, budget)` 取**调用方 deadline 与本地预算中更早者**；`frameIO` 只算一次，`arm` 永远用同一个绝对时刻 |
| 无 deadline 的父 ctx | 仍必须给本地预算；`budget <= 0` 直接拒绝（`ErrConfig`） |
| 错误分类 | `operationError` 按优先级：ctx 结束 → `ctx.Err()`；deadline 已过 → 调用方的算 `context.DeadlineExceeded`，本地的算 `ErrTimeout`；其余**保持原错误**（对端关闭、截断、传输失败各自保留原因） |
| 序号不回退 | 部分写/取消/超时 → 关闭会话；`Send` 在会话关闭后返回 `ErrClosed`，绝不用旧密钥重建 Sender |
| 握手级回调不过期 | 握手**不**注册任何关闭连接的回调；每次 I/O 的 watcher 用 `defer stop()` 结束并失效，所以握手 ctx 到期**不会**误关已建立的业务通道 |
| 网关父 ctx 取消 | `Run` 的 `AfterFunc` 同时关 listener **和** 已接受的活动连接（`closeActive`），使正在进行的握手/业务有界退出 |

**新增测试**（`internal/handshake/cancel_test.go`，全部有确定性同步点）：

- 调用前已取消（保留原有）
- **等待发送槽时取消** —— 关键在于断言传输层**只看到 1 次 Write**
- **Write 已阻塞时取消**
- **Read 已阻塞时取消**
- 调用方 deadline 早于本地 timeout（150 ms 对 5 s）
- **整个 H1–H4 不可逐帧延长预算**（断言两次 `arm` 交给传输层的 deadline **完全相同**）
- 沉默对端下握手在预算内失败
- **取消握手子 ctx 不误杀已建立业务**；取消业务父 ctx 关会话
- 反复取消/Close 幂等；goroutine 不泄漏（有界轮询，不靠固定 sleep）

### R2 测试汇总器（C）

**M2 的实际问题**（审查已动态复现）：空、乱码、截断输入**都退出 0**。

**M2.1 的做法**：非空；严格 JSON（含行号）；字段类型校验；
生命周期闭合（`start`→终态、`run`→结果）；适用 Go 1.24+ 的
`build-output` / `build-fail` 事件；预期包集合由调用方给出（**不硬编码任何测试数量**）；
**关联真实 `go test` 退出码**（未提供就不能给 PASS）。

**退出码三分**（这是关键设计）：

| 退出码 | 含义 |
|---|---|
| 0 | 证据完整、包集合覆盖、go 退出码 0 → 通过 |
| 1 | 运行确实失败（有 `fail` 事件、构建失败、或 go 退出码非 0） |
| 2 | 证据不可用（空/乱码/截断/未闭合/缺包/未提供退出码） |

**判定优先级**：**失败优先于不完整**。一个明确的 `fail` 事件本身就是结论，
不因日志同时不完整而被降级成"无法判断"；只有**给 PASS** 才要求证据完整。

**自测**：`scripts/summarize_selftest.py`，**17 个用例全部通过**，含审查包提供的
`empty.jsonl` / `garbage.jsonl` / `truncated.jsonl` / `failure_control.jsonl`。

**真实退出码的取法**：`go test -json` 的输出重定向到文件（**不经管道**），
立即取 `$?` 存进 `results/raw/m2_1_go_test_exit_code`，再把这个值传给汇总器。
**不使用**汇总器或 `tee` 的退出码冒充 `go test` 的状态。

### R3 分帧 EOF（B）

**M2 的实际问题**：`00 00 00 02` 之后连接结束 → 第二次 `io.ReadFull` 返回普通 `io.EOF`，
被 `channel.Receive` 归类为 `ErrPeerClosed`，于是**死在中途的对端看起来像正常结束**。

**M2.1 的做法**：三种结束方式分开

| 情况 | 结果 |
|---|---|
| 一个字节都没读到 | `io.EOF`（帧间干净结束） |
| 长度前缀只读到一部分 | `io.ErrUnexpectedEOF`（帧已开始） |
| 前缀读完、正文缺失或不全 | **`wire.ErrTruncated`**（帧已开始） |

`ErrTruncated` **包装** `io.ErrUnexpectedEOF`，所以审查提供的回归测试
（期望 `io.ErrUnexpectedEOF`）与本项目的分类断言**同时成立**。
`channel.Receive` 把截断报成 `ErrProtocol` + `ErrTruncated`，**不再**报 `ErrPeerClosed`。

审查的 `review_truncation_test.go` 已原样放入 `internal/wire/`（只做了 gofmt 缩进规范化，
测试逻辑与用例一字未改）。它在 M2 上失败，现在通过。

**业务关闭状态**：`internal/app/service.go` 现在返回
`businessStats`（实测计数），终态事件据此填写：

- `peer-closed` 的 detail 只说"对端在帧间关闭"，**不再声称"设备已完成"**
- 控制已发未收 ACK → `pending_controls > 0` 且 `incomplete = true`
- `DeliveredPlaintext` 由实测计数推导，**不再无条件写 `true`**
- 设备只有"发满 N 条且 N 次控制都应用"才算 `completed`

### R4 端到端脚本与事件验收（D）

| 项 | M2 | M2.1 |
|---|---|---|
| 换行 | CRLF，Linux `bash -n` 退出 2 | **全部 LF**，`.gitattributes` 固定，CR 字节 0，`bash -n` 退出 0 |
| 验收规则 | 各写一套，用 grep/sed 计数 | **共用** `scripts/verify_e2e_events.py`（shell 与 PowerShell 都调用它） |
| 校验内容 | 单端 peer、事件条数 | 双方角色/身份、Suite 1、**完整 32 位十六进制 SID 且两端一致**、遥测-控制-ACK 的 `sample_id`/`operation_id` **一一对应**、无 `terminated`、无 `incomplete`、网关无未确认控制 |
| 结果保存 | 只有成功才建目录，可能残留旧 PASS | 每次 `results/raw/e2e/<run_id>/`，先写 `RUNNING`，结束写本次 `PASS`/`FAIL`/`INTERRUPTED`；失败也留日志；信号退出码 130/143，**不返回成功** |
| 非 JSON 行 | 会被忽略 | **直接失败并报行号** |

**一处真实缺口由新验收器抓到**：事件里**没有 `run_id`**。
已给 `gateway`/`device` 增加 `-run-id`，`Logger.SetRunID` 给每个事件打标。

### R5 端点观测与 SID 口径（E）

- **`session_id` 现在是完整 32 位十六进制**；短显示值另设
  `session_id_short`。协议 SID 仍是 `[16]byte`，**密钥日程与线上长度未改动**。
- `testutil_test.go` 的 `negCase`/`assertStage` 合并写法已删除，改为
  `negObservation`：**分端点**记录 `device_channel_created` 与
  `gateway_channel_created`，未观测的值写 **`null`**。
  篡改 FinishedB 时网关确实已建立会话，现在会被如实记录，不再被一个 `false` 掩盖。
- 新增 `Event.SampleID` / `Event.OperationID`，使验收**结构化**而不是解析自由文本。

### R6 文档收口（E）

| 文件 | 修正 |
|---|---|
| `docs/DEPENDENCIES.md` | `SM2MQV` 的接收者从错误的 `PublicKey` 改为 **`*PrivateKey`**（`ecdh.go:169`）；`SM2SharedKey` 的接收者是上一步返回的 **`*PublicKey`**（`ecdh.go:117`） |
| `internal/cryptosuite/README.md` | 删除"完整安全握手仍未实现"（M2 已实现）；门控说明改为按套件门控 |
| `README.md` | 新增 M2.1 修补表与**已知局限**（控制不驱动定时采样；同 ID 异参数回 `Duplicate` 的现行契约） |
| `docs/THREAT_MODEL.md` | 明确 **race 环境阻塞 ≠ 无竞态**；`Finished` 不证明密钥诚实生成/随机性；控制不驱动采样 |
| `VERSION` | 从过时的 `0.1.0-starter` 改为 `0.4.0-m2.1`，与 `IMPLEMENTATION_STATUS.json` 一致 |
| `IMPLEMENTATION_STATUS.json` | 改为 M2.1，分列 M2.1 修复项、保留项、验证缺口与已记录局限 |

**保留历史报告**：`results/milestone_M1.md`、`milestone_M1_1.md`、`milestone_M2.md`
原样保留并仍带勘误链接。项目根目录的 `MANIFEST.sha256` 是
**原始 Starter 骨架的 47 文件清单**，已在 `README.md` 与 `PACKAGE_INFO.md` 中注明
**不能**当当前源码清单使用。

---

## 三、执行的命令与真实退出码

完整记录见 `results/raw/m2_1_verification.txt` 与 `results/raw/m2_1_run_info.json`。

| # | 命令 | 退出码 | 结果 |
|---|---|---|---|
| 1 | `go version` | 0 | `go1.27.1 windows/amd64` |
| 2 | `go env …` | 0 | 见 run_info |
| 3 | `go mod verify` | 0 | `all modules verified` |
| 4 | `go vet ./...` | 0 | 无告警 |
| 5 | `go build ./cmd/...` | 0 | 构建通过 |
| 6 | `go test -count=1 -json ./...` | **0** | 原始流 `results/raw/m2_1_tests.jsonl`；退出码**先于任何管道**取出 |
| 7 | 严格汇总器（附 `--go-exit-code 0`、14 个预期包） | **0** | `VERDICT: PASS` |
| 8 | `scripts/summarize_selftest.py` | **0** | 17/17 用例符合要求 |
| 9 | `bash -n scripts/m2-e2e.sh` | **0** | CR 字节数 0（修复前退出 2） |
| 10 | `scripts/m2-e2e.sh`（真实 TCP 端到端） | **0** | 25 项严格检查全 `ok`，`VERDICT: PASS` |
| 11 | `go test -race ./...` | — | **未执行**，环境阻塞 |

### 测试数量（由 JSON 自动统计，非手写）

```text
packages        : 14 total, 9 ok, 0 failed, 5 without tests
top-level tests : pass=158 fail=0 skip=0
subtests        : pass=175 fail=0 skip=0
negative cases  : 27
VERDICT: PASS (exit 0) — evidence complete, expected packages covered, go exit code 0
```

（M2 时是 145/166；本轮新增取消与截断回归后为 158/175。
**汇总器不硬编码任何数量**，增删测试不会误判。）

### 端到端验收结果（节选）

```text
ok   gateway authenticated the device
ok   device authenticated the gateway
ok   negotiated suite is the traditional suite
ok   gateway reports a full 32-hex session id
ok   both endpoints agree on the session id
ok   telemetry sample ids are exactly 1..N on both sides
ok   gateway control operation ids match the expected set
ok   each sample maps to its own control on both sides
ok   the gateway has no unacknowledged control
VERDICT: PASS — session c546b115280e59e47a189c3387ce6fb4
```

---

## 四、本轮由测试发现并修掉的额外缺陷

除 R1–R6 之外，新加的测试与验收器还抓出这些问题：

| # | 问题 | 谁抓到的 |
|---|---|---|
| 1 | 事件里**没有 `run_id`**，日志无法归属到某一次运行 | 新的严格验收器直接判定 FAIL |
| 2 | `summarize-go-test-json.py` 被前一轮的脚本写入时带上了 **CRLF**（407 个 CR），`./script.py` 的 shebang 在 Linux 上会报 "bad interpreter" | 逐文件 CR 字节统计 |
| 3 | `m2-e2e.sh` 在打补丁时又被写回 **CRLF**（201 个 CR） | 同上（现已建立规范化流程） |
| 4 | 验收器把"有 `fail` 事件但缺 `start`"判成"证据不可用"（退出 2），而正确结论是"运行失败"（退出 1） | 审查包自带夹具 `failure_control.jsonl` |
| 5 | 测试传输桩 `gatedConn` 在设置了 deadline 时只等 deadline、**不理会 hold**，导致测试挂 5 秒 | 取消测试自身 |
| 6 | deadline 与 ctx 之间存在竞态：传输层可能先报超时而 `ctx.Err()` 尚未发布，同一条件会被分类成不同错误 | `TestCallerDeadlineShorterThanLocalTimeout` |

第 6 条值得单说：它说明**不能只靠 `ctx.Err()` 判断超时**。
`operationError` 现在显式检查调用方 deadline 是否已过，使分类与调度无关。

---

## 五、保留没有削弱的既有安全属性

- **真实 SM2 签名**与**完整 SM2 密钥协商**（`SM2MQV` + `SM2SharedKey`，不是普通 ECDH）
- 全部身份、key_id、角色、签名与 `Finished` 检查
- **真实 SM4-GCM**（不是 AES 替身）
- 序号**绝不回退**，绝不用旧密钥重建 Sender
- **Suite 2 仍未实现且继续被明确拒绝**；默认 `minimum_mode` 仍是 `hybrid`，
  默认配置**拒绝启动**
- `cmd/bench` 仍拒绝生成未实现的性能结果
- 事件日志不输出共享秘密、PRK、会话密钥、nonce 前缀、随机种子或业务明文

---

## 六、未执行 / 未闭环

| 项 | 状态 | 说明 |
|---|---|---|
| `go test -race -count=1 ./...` | **未执行** | 环境阻塞。本轮用**纯 Go**（无 cgo）三行程序 + 全新 `GOCACHE` 复现 `exit status 0xc0000139`，与项目情况一致。**未**修改系统 DLL、**未**关 cgo/模块校验、**未**删并发测试 |
| 带 `-race` 的 cgo 探针 | **未完成** | 在本沙箱时间预算内没编译完，**两种结果都不主张** |
| Linux / WSL / 容器的 race | **未执行** | `wsl.exe` 被本机安全策略阻止；无 Docker；**未授权**上传或跑远端 CI |
| SM2 密钥交换独立对照 | **未完成** | OpenSSL 命令行不暴露 SM2MQV |
| M3 / 网页 / 20 设备 / 性能基准 | **未开始** | 超出本轮范围 |

**环境阻止 race 不等于排除项目竞态。** 本轮为并发路径补了针对性测试
（取消、Close、Send/Receive 交错、部分写、goroutine 泄漏），
但这些**不能**替代竞态检测器。

---

## 七、声明

- 本轮**未**记录或提交任何真实密钥、随机种子或业务数据。
- **未**为了绕过错误而关闭任何校验（模块校验、cgo、race、并发测试均未关闭）。
- **未**伪造任何测试结果、事件或向量；所有数字可由 `results/raw/` 原始文件重算。
- **未**从 Starter 重建项目，**未**覆盖已有改动。
- 每个断言都尽量配对一条**能失败的**反向检查（例如"取消后传输层只看到 1 次 Write"）。

**本轮到此停止。** M2.1 的修复项与回归全部通过，传统端到端已被严格验收器在两端验证；
但 race 与 SM2 独立交换对照两个缺口仍在，因此**不声称完整验证通过**。
