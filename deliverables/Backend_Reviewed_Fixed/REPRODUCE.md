# 本轮复现与历史边界

实际工作目录 C:\Users\20477\WorkBuddy\密码技术竞赛\edgehybrid-agent-starter；发布目录 C:\Users\20477\WorkBuddy\密码技术竞赛\EdgeHybrid_FriendFix_Work\Release；版本 interactive-v1.2-backend2-reviewed；Go go version go1.27.1 windows/amd64；无 Git，当前源码 388 文件 SHA-256 21a723b1443f33e9e9fcbfa766060f0100df780b5a8c069f21cac172a8396d1b。构建/测试/EXE 均取当前工作树，无旧 ZIP 源码覆盖。

源码目录执行 go mod verify、go vet ./...、go build ./...、go test -count=1 -json ./...。定向测试为 TestFriend*；全量 Windows race 使用 scripts/backend_regression.py 和既有隔离 WinLibs CC/PATH，实际命令/退出码在 raw/final_regression。Python/Node 是复查工具，不是用户运行依赖。独立 SM2/OpenSSL 用已有 Java/BC/OpenSSL，脚本 scripts/backend_w1.py；14 组核心交换，不含标准 S1/S2 的独立认证。

scripts/backend_build.py 生成全 8 EXE，build_info.json 给出源码清单/工具/各 EXE hash。源码审查包无 exe/私钥/JAR/DLL；Windows 包无预置私钥。前端22文件零变化，静态页面文字可能仍为 V12，当前后端版本在 build API 和 build_info.json。

本轮 EXE 验收脚本：interactive_installed_acceptance.py（1/20、API、防伪、任务、导出、停止/再启动）、backend_timer_acceptance.py（26.3/28.7、空闲控制与真实1秒/5秒）、interactive_bad_device_acceptance.py（D001/D010/D020 坏设备各其余19尝试通信）、interactive_gateway_acceptance.py（1/20 网关维护/轮换/吊销/退役/持久重启），backend_live_acceptance.py --duration 120（仅本版短有界稳定性，未声称1800秒）。每次新目录，不覆盖旧证据。

长测已修正的测量脚本为 scripts/backend_live_acceptance.py，connect 预期 HTTP 202。backend1 原 300 秒失败与完整1800秒见历史来源索引和 EdgeHybrid_Backend_Final_Receipts.zip；原冻结脚本/实验 hash 不变。本版没有1800秒或正式1000次新证据。

旧 M5/M6 与 backend1 16批保留原源码/run；本版验收器只重新验证已有 raw，绝非重新采样。旧缺健康基线的 P 安全拒绝结论须按 F05 限定，正常性能成功样本本身不因此改写。0ns 保留；CPU/峰值按原批次口径，P/T/H 整体差异不能全归ML-KEM。未来成功率不能从1000/1000推成100%。

未执行：F07 前端修复、独立标准 S1/S2、物理断网、第三方安全认证、真人易用性试用、本版正式1000/1800。这些不是历史 race/独立核心交换/OpenSSL 的 pending。最终 ZIP 解压验证回执在 ZIP 外，避免自身 hash 循环。
