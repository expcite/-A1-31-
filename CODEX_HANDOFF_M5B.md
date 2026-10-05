# M5B 当前工作区交接（2026-09-30）

- 唯一开发、构建、测试和打包源码目录：`C:\Users\20477\WorkBuddy\密码技术竞赛\edgehybrid-agent-starter`。
- 当前阶段：M5B。用户已授权本轮 G1/G2/G3 源码修复、回归、smoke、文档及本地交付；不执行正式 1000 次，不进入 M6。
- 当前不是 Git 仓库；不初始化、提交或上传。保留 AGENTS.md 和全部现有修改。
- 修改前备份：相邻 `EdgeHybrid_M5B_Baseline_20260930T163316/source_before.zip`，176 文件。基线清单 SHA-256：`3cc7ed0560c0a63276b77f127063828bb0b70b6b0489ef976cc58addd917bb4e`。备份排除 runtime、秘密、缓存、可执行文件。
- Go：`C:\Users\20477\.workbuddy\binaries\go\go\bin\go.exe`，`go1.27.1 windows/amd64`。
- Python：`C:\Python312\python.exe`，3.12.10。当前进程可设置 GOTOOLCHAIN=local 及 Go PATH；不修改系统配置。
- Bash 启动入口进入 WSL `/usr/bin/bash` 5.0.17，不是 Git Bash。本轮优先 Windows PowerShell/Python，必要时适配测试入口，不改变密码协议。
- `codex_handoff_reference/` 未提供。已读相邻 `EdgeHybrid_M5B_Audit` 的 README、REVIEW、AGENT_FINISH_M5B、M5B_ACCEPTANCE 和历史源码摘录，仅作为参考。相邻 `EdgeHybrid_M5B_Final_Audit` 不存在，未读取；不搜索 Temp。

## 当前核对状态

G1/G2/G3：已按当前源码实施校正，定向真实回归通过；最终关闭状态仅取自本轮 results/m5b_corrected_readiness.json 与原始日志。历史报告的“已关闭”不能作为当前源码已验证的依据。新契约见 docs/M5B_CORRECTED_CONTRACT.md。

G1：终态 CPU、窗口原始快照/API 错误、真实 accepted/sent/confirmed 关联、远端 ACK 确认窗口、受控停止及 Wait。保留正确轮次、逐条字节、双向阶段字节和独立目录。

G2：真实 TLS 1.3/X25519MLKEM768/mTLS、认证身份绑定、业务范围及 ACK 语义、EOF 边界、统一预算/取消、每会话协商事实，入口保持薄层。

G3：严格 JSON/schema/唯一 ID/关联/重算、完整失败证据与 PASS 区分、实际 CLI 调用完整验收器和可靠 validation.json 持久化。

## 必须完成的回归与交付

定向回归后执行 go mod verify、go vet、go build ./cmd/...、go test -count=1 -json ./...，保存 stdout/stderr/实际退出码及冻结源码哈希。执行原 Python 自测、T/H E2E、20×3、19+1、T/H 资源各 10、P 握手 10、T/H/P 各 3×200 与实际三类验收器；新增缺确认/失败/取消/写盘故障回归。均为 smoke、formal_results_eligible=false，失败原样保留。

交付 `EdgeHybrid_M5B_Corrected_Review_Package.zip`：源码/测试/脚本/文档/原始日志/本文件/改动清单/报告/readiness/PACKAGE_INFO/MANIFEST。排除 runtime 秘密、缓存、可执行文件、备份与 .git。独立解压副本仅用于复验，不作为开发目录。

历史缺口：race 未获新执行证据；SM2 密钥交换独立对照 pending；Linux 资源 NOT_TESTED；P CPU/RSS 可 null+原因。不以旧日志或环境可启动冒充新验证。

本轮最终核对结果、命令/退出码、打包信息及解压复验索引见 results/milestone_M5B_CORRECTED.md。如报告未生成或其门槛未通过，不能仅凭本交接文件宣布完成。源码冻结清单保存在本轮 raw 目录，最终 ZIP 清单另覆盖交付全部文件。

## Windows 最小演示交付（用户本轮明确调整范围）

已读相邻 EdgeHybrid_M5B_Corrected_Next/REVIEW.md 与 AGENT_BUILD_WINDOWS_DEMO.md。当前源码与 Corrected 204 文件清单一致，未由旧 ZIP 覆盖；本轮新增 cmd/demo、internal/demo 及嵌入页面、Windows 构建/验收/双包脚本，保留 G1/G2/G3 与测量器。

用户优先要求 Windows 一键演示，明确允许本机只读界面；默认 Suite2，固定 1/20 台，复用 app.Device/Gateway、identity 和真实 handshake，共用原业务处理。exe 默认 1 台；20 台使用固定启动入口。页面不读历史 results、不控制启动/停止、不开放文件或密钥接口。单实例/停止使用 Windows 本包范围内的命名对象；当前工作目录不决定路径。新测试身份独立于工程 runtime，正常停止只清理本次私钥目录。

演示以 DEMO_TEST_REPORT.md、results/milestone_M5B_DEMO.md、results/raw/demo_windows_20260930/ 的实际证据为准；代码存在不等于已验收。用户包需要 exe，审查包排除 exe 和全部运行时秘密；不嵌套历史 ZIP。19+1 原批异常设备为 D020，文档已补勘误，不修改旧数据。本轮仍不运行正式 1000 次、不进入 M6，原 race/独立 SM2 交换等缺口保持 pending。完成最小交付后停止。
