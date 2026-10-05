# M5B G1/G2/G3 本轮校正报告（2026-09-30）

当前状态：源码、回归和原始证据一致，G1/G2/G3 本地关闭；同源码交付包独立解压核心及 CLI 复验已通过。最终 ZIP 自身的完整性和打包后复验见相邻 verification.json/verification_logs，避免 ZIP 自身哈希循环引用。

## 目标、来源与基线

唯一开发、构建、测试和打包源码目录：C:\Users\20477\WorkBuddy\密码技术竞赛\edgehybrid-agent-starter。不是 Git 仓库；没有初始化、提交或上传。原 AGENTS.md 保持原字节。用户授权来源为本轮粘贴请求，允许修复、回归及本地交付；不重新执行 M0/M1/M4 阶段工作，不进入 M6、不跑正式 1000 次。既有结果与失败运行保留。

修改前备份：相邻 EdgeHybrid_M5B_Baseline_20260930T163316/source_before.zip。基线清单 SHA-256：
3cc7ed0560c0a63276b77f127063828bb0b70b6b0489ef976cc58addd917bb4e
备份 ZIP SHA-256：
6a74f8723cd227047af3ec1f5a35c8d77b548cc7fbfa7ab302bff3e670d037a6

codex_handoff_reference/ 未提供；相邻 EdgeHybrid_M5B_Final_Audit 不存在，未读取。已读较早 EdgeHybrid_M5B_Audit，仅作为参考，结论来自当前源码和本轮日志。适用本地指令为根 AGENTS.md；未找到嵌套适用指令。核对 scripts/configs/docs/交接说明，没有发现另一源码工作目录被脚本或配置用作本轮开发入口；当前目录、实际工具路径及历史 Audit 名称保留，没有批量替换路径。

## 实际修复与函数

| 项目 | 修改前判断 | 本轮校正与验证 |
|---|---|---|
| G1 | 问题尚存；轮次、逐条字节、双向阶段分离已修 | RunDeviceResourceWorker 在工作结束采样进程累计 CPU，保存窗口原始快照/API 错误；metrics.SampleCPU/Delta、filetimeToNS 检查错误、回退和单位溢出，真实 0 保留。RunDeviceThroughputWorker、RunThroughputSmoke 分别统计 accepted/sent/confirmed，按 run/peer/SID/sample/operation 精确匹配。ConfirmationObserver 用 worker 单调时钟观察已验证 ACK 的公共事件闭合窗口。runMeasurementWorkers 正常会话上限退出并 Wait，异常有界清理记录 forced_cleanup、Wait 和退出码。 |
| G2 | 问题尚存；真实 TLS 配置及实际状态基础保留 | ValidateTelemetry 复用生产业务校验，P ServeOne/ServeConnection 绑定认证身份、检查温度范围及 ACK 操作/applied/间隔。readMessage 区分边界 EOF 与截断/坏 JSON/超长消息。measureRound/runPRounds 统一拨号、握手、业务、确认预算及取消关闭，等待服务端结果后发布。每会话记录双方实际 TLS 1.3/X25519MLKEM768/套件/身份/DidResume。完整循环移入 internal/pbench，cmd/pbench.run 只装配。 |
| G3 | 问题尚存；旧 nil 算术保护已修但需强化验证 | evidence.Decode/ReadLines 拒绝重复键、缺字段、错类型、未知字段与尾随 JSON；事件 64 KiB、批量行 2 MiB、文件 64 MiB 有界。ValidateResourceResults、ValidateThroughputResults/CheckThroughputRows、ValidatePResults 读取完整 manifest/raw/events/summary/status，重算计数、CPU/峰值、字节、窗口、速率及摘要。WriteBytes/Complete 传播 Write/Sync/Close/验收失败，实际 CLI 使用完整验收器；有效失败保持 FAIL/INCOMPLETE，不能改写 PASS。 |

回归还发现取消后的迟到确认会被旧 JudgeResults 判为孤立事件。修复 trial 的实际控制事实采样位置，仅允许同 run/身份/SID/operation 的唯一真实确认归属已完成 ACK 但等待失败的行，成功标志仍为 false。错操作、缺实际应用和重复确认的反例仍拒绝。

原 handshake/cryptosuite（含 KDF）/record/policy 文件保持原字节；没有更改 H1–H4、密码算法、认证、防降级或重放逻辑。完整改动文件及前后 SHA-256 见 M5B_CORRECTED_CHANGES.md、M5B_CORRECTED.diff 和 raw/m5b_corrected_20260930/source_changes.json。

## 实际执行与退出码

平台：Windows-11-10.0.26200-SP0，Windows 原生 PowerShell/Python。Go：C:\Users\20477\.workbuddy\binaries\go\go\bin\go.exe，go1.27.1 windows/amd64。Python：C:\Python312\python.exe，3.12.10。Bash 入口进入 WSL /usr/bin/bash 5.0.17，不用于本轮执行。仅当前测试子进程设置 GOTOOLCHAIN=local、既有 Go PATH、PYTHONUTF8/PYTHONIOENCODING；没有安装、升级、go env -w、全局 PATH 修改或工具链自动下载。隔离临时目录新建测试身份，退出后清理；未读取现有运行环境秘密。

冻结源码清单：raw/m5b_corrected_20260930/source_frozen_r4.sha256，共 204 文件。
清单 SHA-256：aa0ea21668d8f14902c80c39701bbfe8702918d02e3b78b5536dec58ca9518ca
每条命令 stdout/stderr/真实退出码/平台/完整参数保存于同 raw 目录各命令子目录的 stdout.txt、stderr.txt、exit.txt、command.json。

| 执行 | 原始日志目录 | 实际结果 |
|---|---|---|
| go mod verify | mod_verify_r4 | 退出 0 |
| go vet ./... | vet_r4 | 退出 0 |
| go build ./cmd/... | build_r4 | 退出 0 |
| go test -count=1 -json ./... | final_go_tests_r4 | 退出 0；21 包，16 有测试、5 无测试；336 顶层通过、1 跳过；268 子测试通过，0 失败 |
| 严格 JSON 测试汇总 | go_json_summary_r4 | 退出 0；读真实 exit.txt 和 go list 的完整预期包列表；37 negative、14 M4 旧回归案例来自原始 JSON |
| summarize_selftest / m4case_extractor_selftest / fleet_verifier_selftest / bench_result_selftest | python_r4_0…3 | 四项退出 0 |
| Suite1 / Suite2 真实 E2E | e2e_suite1_r4 / e2e_suite2_r4 | 退出 0；各 3 笔接受/控制/确认；正常停止并 Wait |
| 20 设备×3 会话 | fleet_20x3_r4 | 退出 0；60 独立 SID、180 笔确认 |
| 19 good + 1 bad | fleet_19plus1_r4 | 场景验收退出 0；合法 57 会话/171 笔确认；坏身份拒绝、fleet 子进程非零；网关正常预算结束并 Wait，未强杀 |
| M5B 全部新 smoke、实际 CLI 正/负对照 | smoke_r4 | 总入口退出 0；原始每条命令见 smoke_r4_data/commands 与 command_index.json |

## 新 smoke 原始结果

本轮 run_kind=smoke、formal_results_eligible=false，未运行正式 1000 次。全部原始目录由 smoke_r4_data/results_index.json 定位，manifest 指向同一冻结哈希。

| 试验 | 请求与成功 | 结果 |
|---|---|---|
| T 资源 | 10/10 握手，gateway/device 独立 PID | PASS；终态 CPU 31,250,000 / 15,625,000 ns；峰值工作集 11,386,880 / 10,465,280 bytes |
| H 资源 | 10/10 握手，gateway/device 独立 PID | PASS；终态 CPU 31,250,000 / 15,625,000 ns；峰值工作集 11,587,584 / 10,977,280 bytes |
| P 握手 | 10/10 | PASS；每次真实 TLS 1.3/X25519MLKEM768/mTLS，DidResume=false；另存业务确认事实 |
| T 吞吐 | 3×200=600 完成，0 失败 | PASS；accepted/sent/confirmed 各 600；接受明文字节 34,200；均值 97,634.12 B/s、1,712.88 TPS |
| H 吞吐 | 3×200=600 完成，0 失败 | PASS；accepted/sent/confirmed 各 600；接受明文字节 34,200；均值 89,186.70 B/s、1,564.68 TPS |
| P 吞吐 | 3×200=600 完成，0 失败 | PASS；accepted/sent/confirmed 各 600；接受明文字节 34,200；均值 107,787.92 B/s、1,891.02 TPS |
| P TLS 拒绝 | 错服务端身份、缺客户端证书、无共同指定组 | 3/3 按预期拒绝；真实双方错误与 worker_waited 记录于 p_rejections.jsonl |

资源 CPU 是进程累计值及握手批量窗口，不能称为单次纯算法 CPU；Windows 计时粒度下窗口可真实为 0。峰值为 Windows 进程峰值工作集。P CPU/RSS 同进程不能归属两个角色，保持 null+原因。T/H/P 确认窗口均包含本机事件可见性和轮询观察成本，较旧 Send 窗口的速率下降是边界变更；不能推断跨机器、线路或密码算法性能。不同试验/机器负载不能用该小样本排名。

## 成功对照、拒绝与真实失败

三类完整验收器经实际命令调用：资源、吞吐、P。6 个正常原始目录实际 CLI 验证退出 0，且生产 validation.json 的 problems=[]、VALID、acceptance_passed=true 一致。36 个独立公开证据变异副本实际 CLI 退出 5，包含缺 status/trials、空 trial、删全部确认、坏 JSON、重复 ID、错 run、伪造 summary、错误 TLS；未改动正常对照，没有以 validation.json 自证通过。每项命令及诊断保留在 smoke_r4_data/commands。

新增真实 P 服务端/CLI 测试覆盖正常业务、D001 冒称 D002、遥测越界、ACK 错操作/应用/间隔、缺最后 ACK、截断、坏 JSON、超长消息、取消、持久化失败。Go JSON 中 TestPServerRealBusinessBoundaries、TestPCLIActualHandshakeThroughputAndVerify、TestPThroughputRealServerAndPublicationFailure 及子测试记录真实 TCP/TLS 路径。TestM5BCorrectedActualWorkersAndCLI 使用生产子进程、200 笔正常对照、删确认拒绝、启动失败和取消；TestM5BCompleteFailureIsValidEvidence、TestR2LateConfirmationIsBoundButDoesNotGrantSuccess 区分 VALID 失败与 PASS；CPU API 错误/回退/零值/溢出和持久化失败也有确定性回归。

本轮失败全部保留：早期定向编译/边界失败；Python 初次 GBK/UTF-8 解码冲突（仅测试子进程 UTF-8 后通过）；final_go_tests_r2 的取消孤立确认（修复并确定性回归后通过）；go_json_summary 初次传错预期包 ./... 且未读真实退出码，验证器确实拒绝，不能作为成功证据；smoke_r3 在真实 3×200 下超出旧批量证据 64 KiB 上限（返回 5、未报告成功），独立提高批量行有界上限并补真实 200 笔回归后，smoke_r4 新目录完整通过。没有删除失败样本或拼接成功。

## 未执行项、风险与下一步

唯一跳过的顶层测试是 TestSM2SignatureFromThisAdapterVerifiesInOpenSSL，openssl 不在当前 PATH；这不等于独立 SM2 交换对照已完成。race 未获新执行证据、SM2 密钥交换独立对照仍 PENDING、Linux 资源 NOT_TESTED，P CPU/RSS 不扩展。原型没有安全审计、证明或合规认证；SM2 身份认证不是后量子安全。确认观察旁路仅限本机软件仿真。

本轮完成后停止；继续阶段、正式实验、安装或系统配置、远端上传均等待新的明确指示。独立解压副本只用于复验，开发仍以当前唯一源码目录为准。

## 交付包复验

打包与复验均使用当前源码产物，开发目录未切换。审查暂存 ZIP：相邻 EdgeHybrid_M5B_Corrected_Stage_20260930.zip，清单覆盖 2794 文件，SHA-256 9fe90942d27da985e2c696216cb1eb2599478c07020cdbe748bb621e1b67b45c。独立解压 EdgeHybrid_M5B_Corrected_Verification_20260930/edgehybrid-agent-starter 后：

- extracted_source_hash：204 文件源码哈希与冻结清单完全一致。
- extracted_core_tests：go test -count=1 -json 覆盖 evidence/metrics/bench/pbench/app/cmd/bench/cmd/pbench，实际退出 0；7 包、127 顶层和 88 子测试通过、0 失败/跳过。extracted_core_summary 严格读取真实退出码和完整预期包。
- extracted_build_validators：由解压源码重建 bench/pbench，退出 0。
- extract_verify_* / extract_reject_*：实际完整 CLI 在解压的原始数据上，6 个正常对照退出 0、36 个反例退出 5；索引 extracted_cli_index.json。
- source_post_package：主开发目录源码仍与冻结清单一致。

最终 ZIP 路径：C:\Users\20477\WorkBuddy\密码技术竞赛\EdgeHybrid_M5B_Corrected_Review_Package.zip。该 ZIP 加入上述复验日志和最终报告/readiness，源码哈希保持一致；有效 MANIFEST.sha256 覆盖包内全部文件。排除 runtime、私钥/会话秘密、缓存、可执行文件、备份与 .git。相邻 EdgeHybrid_M5B_Corrected_Review_Package.verification.json 和 .verification_logs 保存最终 ZIP 完整性以及封包后独立解压复验；ZIP 大小/自身 SHA-256 在相邻 .sha256 与最终回复中发布，不能嵌入自身以形成循环。

新手阅读顺序：CODEX_HANDOFF_M5B.md → docs/M5B_CORRECTED_CONTRACT.md → 本报告 → m5b_corrected_readiness.json。bench -verify -dir DIR -exit-code CODE 验收资源或吞吐，pbench -verify -dir DIR -exit-code CODE 验收 P；DIR 为 results_index.json 对应目录，CODE 是原生产命令的 exit.txt。退出 0 说明此次证据完整且运行成功；反例退出 5 是按预期拒绝。交付包不含二进制，复现时用已验证的 Go 从唯一源码目录构建，保持 GOTOOLCHAIN=local。


## Windows 演示轮次同步勘误

本轮 M5B Corrected 19+1 原始参数、summary 和 verify 的异常设备为 D020，不是 D010。此处明确同步编号；旧原始数据保持不变，没有重跑或拼接该批数据。
