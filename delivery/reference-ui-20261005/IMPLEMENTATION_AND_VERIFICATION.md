# 前端参考复刻交付说明

工作目录：`C:/Users/20477/Documents/deepseek-harness/default-workspace/edgehybrid_work/Work/edgehybrid-agent-starter`。本轮以当前工作区为准，保留既有修改，没有使用旧 ZIP 覆盖源码。

软件版本保留 interactive-v1.5-minimalui1；外观修订 reference-ui-20261005。源码清单 475 项 SHA-256：`5330ac977d60422492215d3b5fed6d83162283b067ef33741d88a2edb68fde28`。只修改 FRONTEND_ONLY_RECEIPT.json 所列 9 个前端文件；所有非前端文件以及 API 客户端、ConsoleStore、guide、confirm、usability 与历史数据文件字节不变。

## 实际完成

参考提供的 HTML 实现浅灰画布、白色卡片、蓝色主操作、左侧四主入口、辅助管理/帮助、顶栏当前运行、双标签工作台、右侧设备状态/最近活动与统一详情侧栏。保留真实接口、危险确认、状态语义、迟到响应防护和 F07 范围冻结逻辑。参考 HTML 的示例数据和模拟成功逻辑未接入生产。

| 功能 | 新入口 |
| --- | --- |
| 发送温度、设置周期 | 工作台 / 发送温度、设置间隔 |
| 暂停采样、自动源、断开、换钥 | 当前设备 / 采样与会话设置，设备 / 更多操作 |
| 批量连接、停止设备 | 设备 / 批量操作 |
| 日志筛选、分页、JSON/CSV/HTML 导出 | 记录 / 筛选条件、更新范围、导出 |
| 历史 M5 结果 | 记录 / 历史结果，保留原测量版本来源 |
| bench/pbench/M6、安全任务 | 实验 / 原有受控任务 |
| 设备及网关密钥管理 | 管理 / 身份与密钥，按对象展开 |
| 本机授权、停止软件 | 当前运行 |
| 用户说明与版本 | 帮助与版本 |
| 真实分步操作演示 | 工作台 / 打开指引；与普通工作台共享接口与状态 |

## 实际验证

最终源码全量 Go JSON、结果验收器和 vet 均退出 0：452 个顶层测试 PASS、555 个子测试 PASS；1 个显式长期 opt-in 测试 SKIP，5 个入口包没有测试，详见 GO_SUMMARY_DELIVERY.json。前端 BPlus 30、反馈 5、G3 19、Minimal 5、F07 13 个用例均通过；旧前端 2 组共 43 个断言通过，断言不算作 43 个测试。

最终 EXE 的真实浏览器验证包括手动 26.3/28.7、暂停后主动下发 5 秒并确认、1 秒到 5 秒真实接收节奏、20 台独立 SID、定向 D020 控制不改变 D001 等其他设备、非法输入、键盘/移动导航、详情、管理、帮助、筛选导出及页面切换/真实文档刷新无业务 POST。TIMER_OBSERVATIONS.json 从当前公开事件原样重算接收间隔。截图不是设计图；SCREENSHOT_INDEX.json 明确当前、中途构建及仅参考图的来源。

Windows 包独立解压，1 台通过原 CMD 入口启动，20 台通过新 EXE 启动；子进程 PATH 仅保留 Windows 系统路径。两次 installed 验收均通过：真实 Hybrid、主动控制/隔离、全新会话、幂等冲突、容量拒绝、真实三格式导出、离线不显示确认、资源停止，以及退出/重启与隔离测试任务。20 台还完成 T/H/P 小规模 smoke 及真实结果验收。这不是新的正式性能矩阵，未重跑 1000 次或 1800 秒。

## 边界和失败

保留初次 F07 适配失败、过早读取连接/确认状态、错误选择器、移动 aria 与键盘焦点问题以及后续同例修复。中途旧绑定/哈希导航观察错误单列，不能把这些原始 FAIL 改成 PASS。当前截图使用源码 5330ac977d60422492215d3b5fed6d83162283b067ef33741d88a2edb68fde28 与 EXE 17b8ed69334f6d840a3847bf31d8c3bbeb56d1856886ebfd5a19c93e8a804c18；旧截图有独立来源标识。

1440、1024、768、375 宽度通过无横向溢出检查；真实 200% 缩放、真人试用、连续录屏和物理断网未执行。此次未重新执行 race，沿用历史来源的适用范围；不声称当前 UI 测试是全面安全认证。旧 M5/M6/后端历史文件 27034 项只读比对未变化，HISTORY_PRESERVATION.json 记录来源和哈希。本审查包交付本轮源码与证据，不声称重新归档或验收全部旧正式实验原始资料。

## 使用与构建

用户下载 Windows 包解压，运行“启动1设备.cmd”或“启动20设备.cmd”，按终端一次口令授权，在工作台执行真实操作；退出软件使用当前运行或“停止交互平台.cmd”。用户运行不需要 Go/Python/Node/外网 CDN。不要同时开启不同入口重复启动。

完整源码在 source/edgehybrid-agent-starter；原用户手册在其 docs/user/MINIMALUI_MANUAL.md，API 说明在 docs/INTERACTIVE_API.md。本文件给出本轮入口变化，原文件保留原字节。构建命令及实际工具版本见 evidence/commands/delivery_build 与 build/build_info.json。前端补丁由真实基线生成，隔离 git apply --check、实际应用后 475 项逐字节一致，详见 PATCH_VERIFICATION.json。没有 Git 仓库，未为打包初始化 Git。

所有打包文件列于相对路径 DELIVERY_INDEX.json 与 MANIFEST.sha256；清单自自身发布目录生成。针对 ZIP 的最终校验回执在 ZIP 外，避免自哈希循环。独立复核脚本在 tools/verify_archive.py；运行依赖 Python 仅属于评审工具，不属于用户软件。
