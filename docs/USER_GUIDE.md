# Windows 演示使用手册

使用原 EdgeHybrid_Demo_Windows.zip，SHA-256 96bc6b44d21b2a44e25f13f7f181845934d2173373687bf4f56cf25d187226ac。M6 没有重新发布 exe。用户不需要 Go/Python/Node；需要 Windows x64、可写用户目录和浏览器。它展示真实 Suite2 加密业务，性能资料另见正式报告。

1. 完整解压原包到可读目录，中文和空格路径可用，不在 ZIP 预览里启动。
2. 双击 EdgeHybridDemo.exe 或 启动1台设备.cmd。窗口打印 PAGE_URL、LOG_DIR，浏览器自动打开本机地址。
3. 观察建链、遥测接受、控制发送和远端 ACK 确认。每设备本轮固定3笔，不无限循环。
4. 按窗口 Enter 或 Ctrl+C，等待“已停止”；也可运行同一包目录的 停止演示.cmd。
5. 停止后双击 启动20台设备.cmd，观察独立身份/SID；不要同时打开多个测量实验。

当前在线=0 可以表示本轮已完成且设备已断开，并不等于握手失败。累计建链/三种业务计数是本次记录，缓存状态失联会标不可确认，不把它当在线事实。关闭网页不会停止程序。程序最多保留状态页10分钟，然后受控结束。

重复启动本包会拒绝覆盖运行；不同解压目录各自独立。端口冲突或浏览器未配置时看窗口提示/PAGE_URL。系统安全提示不通过修改系统策略绕过。19+1异常自测中 D020 信任预检失败，没有启动远端握手，其他19台正常；这是异常证据，不能描述成20台全部成功。

可分享 LOG_DIR 下 gateway/device/events/lifecycle/manifest/completed/final_state 等公共日志。不要分享同一数据根的 private 目录。正常停止清理本次私密身份；强制结束可能残留，下次以新run_id和身份启动，不读旧数据冒充本次。默认数据根为 %LOCALAPPDATA%/EdgeHybridDemo/<包目录标识>/runs/<run_id>。完整原版说明见 DEMO_QUICKSTART.md；其中“正式实验未完成”是演示发行时的历史状态，M5已在随后完成。

本轮没有重新运行 Demo，也没有扩大 Windows 机型兼容性证据。演示验证来自原 Demo 阶段，见 results/raw/demo_windows_20260930。安全边界和待验证项见 FINAL_DESIGN_REPORT.md。
