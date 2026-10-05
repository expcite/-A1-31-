# M5 正式实验工作记录

日期：2026-10-01（Asia/Shanghai）。结果：固定矩阵完成、原始证据 VALID；本轮到 M5 停止，不进入 M6。

## 目标与范围

读取相邻 EdgeHybrid_Demo_Audit 的三份文件，冻结正式方案、开放受控 formal 入口、补参数/结果/中断/写入失败测试，完整回归后实际执行 T/H/P 1000 次、T/H 分角色资源、T/H/P 真实业务吞吐。三份审计副本位于 raw/m5_formal_20261001/audit。
保留 Windows Demo ZIP 及原哈希，不扩展页面、不重做 M4/M5B、不上传远端。密码、握手、record、业务、identity、policy、demo 及 go.mod/go.sum 相对本轮起点逐字不变；测量与验收层、执行和报告脚本、文档有增量。修改前源码清单 f79964…ce8db8 对应已交付 Demo，不是 Git 提交。

## 修改文件

新增 docs/FORMAL_EXPERIMENT_PLAN.md；internal/evidence/formal.go、formal_test.go；internal/bench、internal/pbench、cmd/bench、cmd/pbench 的 formal_test.go；scripts/m5_formal_prepare.py、m5_formal_run.py、m5_formal_report.py、m5_formal_controls.py。
修改 cmd/bench/main.go、cmd/pbench/main.go；bench 的 types/run/stats/persist/schema/judge/m5b_common/resource/resource_validate/process/throughput；evidence 的 run/throughput/throughput_types；pbench 的 measurement/validate；README、PACKAGE_INFO、依赖与实验索引文档。旧取消测试改为首行落盘后取消，移除对固定 150ms 性能的假设。完整路径、前后哈希及差异见 raw/m5_formal_20261001/source_changes.json、source_changes.diff 和 postprocessing_report.diff。

formal manifest 使用独立 schema、固定方案 ID、恰好 1000/100/5×200、30s 和显式来源。复用 M5B 原始行 schema及采集/业务边界；旧 smoke 的默认范围和非正式资格保留。正式 T/H 配置哈希改用 SHA-256，旧 smoke FNV 配置指纹原样保留。

## 冻结与来源关联

正式方案 ID：m5-formal-20261001-v1，plan SHA-256：34f5e38248b9ee04bca1f1e4b39721a6d4827917ca642fb4b0cd2138a2bb1fdc。
**测量时冻结的 241 文件清单**：fad0abe7326003b0b1b1ce51f0699acdb3b7f5bcf6bbc9f636026ada2cf996fd。实际16批次均记录这个源码 hash，不能替换成后处理修正后的交付 hash。
bench 二进制：33de6c40fb0dc907b0d3b0e5d76a5b567d94e8024b8022a282b112df98d64adf。
pbench：1c9d904d8df9ab04756a824c1d138a167054a02fb5dbcafcf97febdc334360fa。
gateway：6c57a1914a9d2066cc60ac307d8c8a17b1c7f6f46f713511dcc568dd4c476fde。
完整工具链、构建命令/元数据、其他程序 hash 见 freeze/freeze.json；各批次的 config hash、二进制 hash、run_id 和实际命令见 data/results_index.json。

采样后离线报告脚本两次失败：第一次错读 total_goodput_bytes（真实字段为 total_accepted_telemetry_plaintext_bytes）；第二次 Python/Go 浮点汇总差一个 ULP，最大约 2.91e-11 B/s。修正仅涉及离线报告：正确字段和浮点显示重算容差 1e-12；生产 Go 验收器仍逐项精确重算，未改采集器、二进制、参数、原始记录或 summary，未重新采样。两次真实 exit 1、stderr、原冻结脚本和差异均保留。
最终交付源码清单为 11c49ff72ec758a5a2d6c2f1d2d46b0c88894ed00bbaa441785e5b1922b0227d，**与测量源码分开**。唯一字节差异是 scripts/m5_formal_report.py；measurement_source_resolution.json 将原清单这一条解析到 freeze/m5_formal_report_measured_version.py，241/241 校验通过，可完整还原测量时源码。其余源码仍符合冻结清单。复现新采样须先在新结果目录重新 prepare/freeze；旧冻结 run 脚本不会静默接受修正后交付 hash。

## 实际命令与退出码

全部 stdout/stderr/退出码由 run_checked 保存；下列是索引，不代替原始命令。

|命令/检查|退出码/结果|
|---|---|
|2026-09-30 基线 go test -count=1 -json ./...|0；暂停前完成，日志保留|
|开发 targeted_initial|1；旧 smoke fixture 的原因字符串匹配过严，以及旧取消测试时长假设，已修正并保留失败|
|targeted_r2、formal_final_targeted|各 0|
|prepare 中 go mod verify / go vet ./... / go build -o BIN ./cmd/...|各 0|
|prepare 中 go test -count=1 -json ./... / 严格日志重算|各 0；23 包，17 有测试通过、6 无测试；352 顶层 PASS、301 子测试 PASS、1 顶层 SKIP|
|Python summarizer / fleet / bench / M4CASE 自测|各 0；17、23、9、45 项通过|
|T/H E2E、20台、19+1 事件回归|各验收入口 0；异常场景的应用失败码如原日志保留，不算应用全成功|
|当前 M5B smoke 与历史离线验收|0；每类完整验收；历史 6 正向、36 拒绝对照通过|
|m5_formal_run.py 固定16批矩阵|0；16个 producer 均0，16个实际CLI完整验收均0；无重试|
|m5_formal_controls.py|0；8 正向、80 损坏/缺确认/错误组/来源/参数/重复记录等拒绝对照通过|
|m5_formal_report.py 首两次|各1，错误真实保留；并非数据失败|
|修正报告重算及16个生产CLI复验|0；report_verified 为最终成功运行；report/ 中文件为同字节阅读副本|

正式生产命令在落盘后调用完整验收器；validation.json 来自程序，不手填。fixture 和取消/写盘故障在测试中明确隔离，不能算正式观测样本。最终 package 校验记录由实际打包/独立解压检查生成。

## 正式观测

|模式|执行次数|握手成功|端到端业务成功|握手均值 ms|中位数 ms|P95 ms|
|---|---:|---:|---:|---:|---:|---:|
|T|1000|1000/1000|1000/1000|0.924315|1.000100|1.513100|
|H|1000|1000/1000|1000/1000|1.112905|1.004200|2.000500|
|P|1000|1000/1000|1000/1000|1.223204|1.007900|2.013700|

每次全新 TCP/临时材料/完整认证确认及业务探测；实际协商/身份/wire/失败字段保留。正式批次本轮未观察到失败、超时或中断，分母一直1000，无补跑。1000/1000只表示本批次，不能推出未来成功概率100%。握手与业务成功分别统计；不能用温身、20台演示或资源握手补分母。暖机统一0。

资源：T/H 各5批×100完整握手（handshake-only，无业务 probe），20个实际角色进程，各有 PID、原始 CPU 快照/终态 API/工作集/Go heap/Wait。全批通过。整批 CPU 和工作集峰值不均分、不叫单次握手指标、不将两角色峰值相加。

吞吐：T/H/P 各5轮×200，分别1000/1000完整真实事务。有效轮速率算术平均（现有 M5B 口径）：T 99612.1803 B/s、1742.8798 TPS；H 114327.8797 B/s、2000.8866 TPS；P 113004.0482 B/s、1976.4982 TPS。逐笔事务、网关接受字节、三个窗口、wire 与两端事实均在 raw 中，可重算。

## 环境、仍未实现项与风险

Windows 11 10.0.26200，Intel i7-11800H 2.30GHz，8核/16逻辑核，平衡电源；背景CPU负载并非空闲（首批快照32%），逐批完整进程/负载快照保留。采样前均无 Demo/其他 bench/gateway/device/fleet/go 测试进程；结束检查无实验残留。实际 Go1.27.1 windows/amd64，GOTOOLCHAIN=local，GOSUMDB=sum.golang.org。依赖未升级，实际 module graph 随包。
Windows 真零和量化时延保留；ns字段单位不代表真实ns分辨率。吞吐确认窗口包含日志可见性、约1ms轮询和业务策略。T/H/P认证/协议/记录加密与进程组织有差异，本机整体结果不证明ML-KEM单算法优劣、不预测低功耗硬件或跨机吞吐。
单次握手CPU/RSS、P分角色CPU/RSS、空进程基线、IP/TCP开销、网关单调接收窗口未支持：null+理由；Linux资源NOT_TESTED。race与独立SM2交换对照保持历史缺口；唯一SKIP仍为OpenSSL SM2签名验证，不冒称独立SM2交换通过。无安全审计、证明、合规认证。

## 交付与下一阶段

EdgeHybrid_M5_Formal_Review_Package.zip：完整源代码、测试、执行/验收/统计脚本、冻结方案、全部正式raw/资源/吞吐、事件、失败开发/后处理记录、来源关联、报告、PACKAGE_INFO和有效MANIFEST；不包含运行时秘密、缓存、exe或历史嵌套ZIP。历史日志均以各自阶段身份保留。
Demo ZIP 和.sha256原样保留。本轮正式实验完成后停止。M6创新验证、最终答辩材料留下一轮。

## 独立解压复核

Stage 包完整解压并校验全部清单条目；按 source_resolution 核对测量源码 241/241、交付源码 241/241。使用解压源码重新构建 bench/pbench 离线验收工具，16 个正式结果全部再验收退出 0；这两份二进制仅为复验，不冒充原生产二进制。包内运行时秘密目录、缓存、可执行文件、历史 ZIP 条目及私钥 PEM 扫描均为 0。实际18条构建/验收命令及退出码见 extracted_checks；package_verification.json 对应 Stage 包 hash 5d228d79b9c2344ec08f01bd2e70ba9b9efd164589743c385599a63fec7c75ca。最终 ZIP 的全部清单/hash复核在外置 verification.json 与 delivery verification_logs 中，避免把ZIP自身哈希循环嵌入包内。
