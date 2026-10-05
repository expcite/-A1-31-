# 当前完成度：interactive-v1.2

工作目录：C:\Users\20477\WorkBuddy\密码技术竞赛\edgehybrid-agent-starter

339项源码清单：0febefe557f00534d93aa6b40b89f2a19fd5e010004069827ee4067aa4242fe8。源码、最终EXE、当前截图关联同一版本。全部证据根为results/raw/interactive_v12_20261002；只读历史M5/M6不用于证明新增功能。页面存在、按钮存在、API可调用、真实业务生效分别由源码/Go契约/EXE/浏览器双端证据支持。

|区项|入口|后端/前端源码（相对项目根）|真实Channel|测试与原始证据（上述根）|状态|限制|
|---|---|---|---|---|---|---|
|总览|高级→总览|handlers_live.go/state|读取真实Channel派生状态|shipping_installed_one/twenty；screenshots/history_separate|已实现且有证据|本机端点；非硬件|
|通信工作台|收发数据|handlers_live.go；workbench.js；live/session.go|温度/控制/ACK为真实Channel|browser_shipping_one/twenty；shipping_installed_*|已实现且有证据|ACK超时可能已执行；手动源与自动源独立|
|设备与会话|设备|devices.js；live/manager.go|每设备真实TCP/Channel|shipping_bad_D001/D010/D020；shipping_installed_twenty；shipping_go_events|已实现且有证据|20台为本机模拟；accepted不是authenticated|
|密钥与迁移|高级→密钥与迁移|keys.js；management/gateway.go；live/gateway_lifecycle.go|管理动作触发真实新握手/旧会话关闭|shipping_gateway_one/twenty；gateway_lifecycle_test.go|已实现且有证据|本机CAS；网关影响全部设备；不称跨机PKI或物理擦除|
|安全实验室|高级→安全实验室|jobs.js；console/jobs.go；jobs/|独立本机实例真实任务|shipping_installed_twenty；m6_*；all_go_shipping|已实现且有证据|有界8任务；scenario为隔离测试构建；不是生产跳过验证|
|性能与结果|高级→性能与来源|performance.js；handlers_history.go|历史查看不发Channel；有限新任务另有来源|history_reverify；shipping_installed_twenty/jobs_*|已实现且有证据|历史M5与新版有限smoke分开；新版未做正式1000次|
|日志与导出|记录|logs.js；handlers_export.go；export/|真实双端事件有run/ID/SID关联|screenshots/logs_*；filtered_export.json；shipping_gateway_*|已实现且有证据|最近2000内存；20MiB文件；筛选固定范围；非完整历史数据库|
|帮助与设置|帮助 / 高级入口|help.js；app.js；usability.js|只呈现同源状态，权限不变|frontend_shipping；screenshots/help_version/novice_*|已实现且有证据|未做真人试用；native缩放/DPI未验证|
|一键启动停止发布|启动1/20设备.cmd；停止交互平台.cmd；退出软件|cmd/console；console/handlers_live.go；scripts/interactive_build.py|停设备/连接/全软件分开|shipping_installed_one/twenty；browser_shipping_*/command.json|已实现且有证据|Windows amd64本机；其他平台未验证|

前端实际位于internal/console/assets，原生ES模块，无npm工程、lock或前端编译步骤。43项Node验证为纯模块自测，浏览器操作单独记录。原M5/M6冻结，仅重新核验16批已有结果及来源hash。race/独立SM2交换pending；OpenSSL签名SKIP；真人、跨机硬件、物理断网、WindowsDPI未执行。网关管理不能跨进程并发改trust；本机入口见ISSUE_CLOSURE.md。
