# M5B 里程碑报告：CPU/峰值内存、真实业务吞吐、公开混合对照

**run_id**：m5b-20260930T032912Z（父：m5a_r2_final-20260930T004433Z）
**基线**：EdgeHybrid_M5A_R2_Final_Review_Package（含审查 CLI 补丁，已合入并回归）
**源码清单**：156 文件，SHA-256 `2a1848b04d74e2367c6d58dc80f90f4018729f55a3173c42f8fc48ed6f4e8492`
**验收等级**：`M5B_SMOKE_TOOLS_AND_SMALL_SCALE_VERIFIED_PENDING`

## 0. CLI 证据持久化补丁（已合入）
审查确认最终包 cmd/bench/main.go 丢失 validation.json 写入检查（含 ":/n" 笔误）。
已按 cli_validation_persistence.patch 合入三文件：finishValidation 统一 Write/Sync/Close，
写失败返回 5。5 个新回归（正常保存/保留失败码/无效证据/目标为目录/父目录缺失）全部通过，
原 4 个 CLI 回归保持通过。isolated_cli_validation 未复制进生产工程。

## 1. CPU 与峰值内存（Windows 实测）
- 设备与网关为**两个独立进程**：网关 = cmd/gateway 子进程（-metrics 自报，优雅退出后读取）；
  设备 = 本 bench 二进制的 resource-worker 子进程（自报 + 握手窗口两次 GetProcessTimes 快照差值）。
- 指标分名：process_cpu_user/kernel/total_ns（**process_lifetime** 口径，方法字段写明）；
  window_cpu_user/kernel_ns（**window_delta** 口径，明确含 GC 与其他线程，不是纯密码 CPU）；
  peak_working_set_bytes（高水位）/ working_set_bytes（采样时点）/ private_usage_bytes；
  go_heap_alloc/sys（**不同指标**，单独命名）。
- FILETIME 100ns → ns 换算精确，分辨率说明随行输出；真实零保留，API 失败返回错误由调用方
  写 null+原因；不均分同进程资源；无 wall-clock 冒充 CPU；空进程基线为 future work（未混入减法）。
- 实测（T/H 各 10 次握手资源 smoke，PASS）：gw_peak_WS ≈14.5–15.3 MB、dev_peak_WS ≈19.1–19.6 MB；
  每角色记录 PID、方法、分辨率说明与平台说明。

## 2. 真实业务吞吐（T/H 各 3 轮 × 200 笔，全过）
- 复用 Telemetry→Control→ACK 与共享 HandleControl；每轮独立会话；sample_id 会话内唯一。
- goodput 分子 = **网关 telemetry-accepted 事件数 × 每笔明文字节**（非发送尝试量）；
  分母 = 设备端单调时钟窗口（首笔发出前 → 末笔 ACK 发出，含确认观察成本，已披露）。
- 网关接收窗口：null + 原因（跨进程 UTC 相减被契约禁止，未实现同进程单调窗口）。
- 实测（各 600 笔全完成，0 失败）：T avg ≈ 213–234 KB/s（≈3.7–4.1k tps）；
  H avg ≈ 216–233 KB/s（≈3.8–4.1k tps）。线路密文字节单独记录，不与 goodput 混算。

## 3. 公开混合 P 对照（10/10 握手 + 3×200 吞吐 + 3 项拒绝）
- TLS 1.3 only；双方仅 X25519MLKEM768；双向证书认证（ClientAuth=RequireAndVerifyClientCert，
  CN 绑定 D001/gateway.edgehybrid.test）；票据禁用，实测 DidResume=false；
  实读 ConnectionState：Version=TLS1.3，CipherSuite=TLS_AES_128_GCM_SHA256，CurveID=X25519MLKEM768。
- 禁用 InsecureSkipVerify/KeyLogWriter/秘密导出；独立 connection_id（p-<run>-<n>），不冒充 SID。
- 拒绝测试 3/3：错误 ServerName、缺客户端证书（写读探测观察 alert）、组不匹配，全部按预期拒绝。
- P 吞吐 3×200 全完成：avg ≈ 691 KB/s（≈12.1k tps）。P 的 CPU/RSS：null+原因（服务端与客户端
  同进程，无法归属；未做子进程拆分）。
- 协议差异清单见 docs/PUBLIC_HYBRID_COMPARISON.md；不把整体差值归因单一算法。

## 4. 回归与结果契约
- 全量 Go：**308 顶层 / 218 子测试 / 0 失败**（20 包），真实退出码 0；
- 四套 Python 自测 17/45/23/9；Suite1/2 E2E PASS；20×3（60/60/180/180）PASS；19+1 PASS；
- 资源/吞吐/P 结果各有独立 schema + 验收器（ValidateResourceResults/ValidateThroughputResults/
  ValidatePResults），summary 全部从 raw 重算并核对；run_id/trial_id/源码清单哈希/配置哈希关联；
- 全部样本 run_kind=smoke、formal_results_eligible=false；失败保留不补跑；
- BENCHMARK_SPEC/PLAN 的旧口径文字修正并入本轮文档（合计与单次分列；CPU 终态累计 ≠ 握手增量）。

## 未执行 / pending
- `-race` ENVIRONMENT_BLOCKED（未执行）；SM2 交换独立对照不可得；
- Linux 资源口径 NOT_TESTED；空进程基线、P 分进程资源归属：未实现，已如实标注；
- 正式 1000 次实验未运行，保持锁定。

**完成 M5B 工具与小规模验证，停止；不执行正式 1000 次，不进入 M6。**

## 5. 模块化拆分（追加要求，已完成）
bench.go（1688 行）→ types.go / run.go / trial.go / events.go / stats.go / persist.go /
judge.go（+既有 schema.go）；m5b.go（1101 行）→ m5b_common.go / m5b_resource.go /
m5b_throughput.go；app/service.go（1083 行）→ events.go / gateway.go / gateway_business.go /
device.go（sessions/rekey/wiring/messages 未改动）。全部为完整声明移动，公开 API 与行为不变；
JudgeResults 未复制第二套规则（schema.go 表 + judge.go 单一流程）。M5B 新增功能分别位于
m5b_*.go 与 internal/pbench、internal/metrics，未再堆入大文件。逐批 gofmt + 包测试通过。
完整映射见 docs/ARCHITECTURE.md。

**拆分后回归（本机 Go 1.27.1）**：全量 Go **308 顶层 / 218 子测试 / 0 失败**（真实退出码 0）；
T/H 握手 smoke 10/10 均 PASS 且 -verify 通过；T 资源 smoke（roles=2）PASS；
H 吞吐 smoke 3×200=600 全完成（tps≈5135）；Suite1/2 E2E PASS；20×3 与 19+1 PASS；
自测 17/45/23/9 保持。源码清单更新为 171 文件
（`506abe671b26580f0e0fce26346a0dc2e32ec58d3b01824e7a09c12598992218`）。

## 6. G1/G2/G3 关闭记录（M5B-Audit 修复，2026-09-30）

### G1 测量阶段与计数
- 吞吐分桶偏移：采纳 candidate/round_mapping_minimal.patch（round = (n-1)/perRound - 1），
  附带的 2 项 review_m5b_round_mapping_test 回归通过；
- 逐笔累计：accepted bytes 逐消息累加（无 57/34200 硬编码；实测 600 笔 57B 负载=34200B）；
- 双向 wire：握手完成后立即取 C2S/S2C 基线，数据段为纯差值（修复 S2C 从未赋值 0 的缺陷）；
- 窗口改名：accept_confirmed_window_ns（末笔控制收到=远端接受被本地观测）与
  ack_send_window_ns（末笔 ACK 写出）分离，不再把本地 ACK 写出冒充 confirmed；
- 资源 CPU：设备 process_cpu_* 在试验循环结束后重新读取（真正的 process_lifetime 终态），
  before/after 快照含 API 错误记录；峰值/工作集/私有提交/heap 分名；
- 资源与吞吐使用各自独立 run/type 目录（throughput 不再先跑 resource 留旧 PASS）；
- 控制器核对网关确认数与设备完成数一致，不一致写 problems 并返回 5。

### G2 P 对照业务与确认
- ServeConnection/ServeOne 绑定 payload device_id 至认证 CN；
- HandleControl 承担范围检查与幂等（ACK 由真实处理结果生成）；
- ServeConnection 仅把干净 EOF 视为正常结束，其他解码错误原样返回；
- 服务端连接加 60s/5min deadline，防止死客户端长期占用；
- P 吞吐以服务端 ServeConnection 循环 + 客户端轮次完成数、网关确认数为证据
  （实测 3×200=600 完成、avg≈548–715KB/s、tps≈9.6–13k）；
- cmd/pbench 的吞吐循环已移入 internal/pbench，入口只做装配与退出码。

### G3 严格证据纪律
- ValidateResourceResults 修 nil panic（total=0/user=nil 不再崩溃），CPU total=user+kernel 校验，
  缺失计数器无错误字段必须拒绝；
- 三个验收器接受正对照、拒绝空 trials/坏 CPU 算术/重复 round/伪造 summary（回归：
  m5b_validate_test.go 6 项全过，畸形数据零 panic）；
- RunThroughputSmoke 所有 Write/Sync/Close/子进程 Wait 错误传播；网关先 graceful 退出再读事件；
- 吞吐 summary 从 raw rounds + 网关事件重算（含 gateway_confirmed 与设备 completed 一致性核对）。

### 本轮实测（关联冻结源码清单 173 文件，哈希见 m5bf_source_manifest）
- T resource 10 trials roles=2 PASS；H resource 10 trials roles=2 PASS
- T throughput 3×200=600 完成 0 失败 goodput≈513KB/s tps≈8994
- H throughput 3×200=600 完成 0 失败 goodput≈554KB/s tps≈9723
- P handshake 10/10 PASS（TLS1.3/X25519MLKEM768/DidResume=false）
- P throughput 3×200=600 完成 0 失败 avg≈691KB/s tps≈12132
- P rejections 3/3 按预期拒绝
- 全量 Go：310 顶层 / 218 子测试 / 0 失败（真实退出码 0）
- 旧带错误计数的吞吐/资源目录（20260930T033*、0337*、0338*）保留原样未改写，
  已知计数错误由本轮修正的代码与验收器区分。


## 2026-09-30 当前源码校正记录（保留上述历史）

上述历史 G1/G2/G3 关闭记录和计数不代表当前源码已验证。用户本轮授权的校正以 milestone_M5B_CORRECTED.md、m5b_corrected_readiness.json 和 raw/m5b_corrected_20260930 为准。已完成新源码检查、Go/Python 回归、T/H/P smoke 和完整 CLI 反例；ZIP 独立解压复验状态见新报告。不覆盖既有失败/旧结果，不执行正式 1000 次，不进入 M6。
