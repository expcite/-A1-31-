# EdgeHybrid 交互通信平台

当前程序发布版本 `interactive-v1.6.2-e08fix1`，提交 Final_v13（说明同步 1），前端 `MINIMAL-UI-v1.0`。既定软件原型与补测范围已完成；当前状态见 `CURRENT_COMPLETION.md`、`CURRENT_HANDOFF.md` 与 `IMPLEMENTATION_STATUS.json`。本次仅同步说明，不更换协议或增加功能。

8 个现有程序由 Go 1.27.2 构建，原构建源码清单为 `02863dd2efb03dd8885fb45133fdac44128eb462f581d03301417068f39b29ad`。当前说明更新后的清单另列在 `../verification/document_sync_v13_1/`；原构建清单、EXE 和原始实验不改写为新构建或重测。

[快速入门与完整手册](docs/user/MINIMALUI_MANUAL.md) · [API](docs/INTERACTIVE_API.md) · [原题要求](docs/REQUIREMENTS.md) · [本轮修复边界](docs/REFERENCEUI_REVIEW_FIXES.md)。

Windows运行包：双击`启动1台设备.cmd`或`启动20台设备.cmd`；授权后明确连接，输入26.3/28.7发送并核对真实回执。停止用`停止交互平台.cmd`。关闭网页不退出软件，刷新/切页不重发。运行不依赖Go/Python/Node/Java/CDN。

批量：HTTP接受只代表受理，只有本次run/身份/SID/ID关联的真实操作确认才计为确认。连接/断开的本机状态观测、未知、未确认、拒绝分别列出；超时可能已执行，不自动重发。换钥双端新握手与之后的业务验证分开。

构建统一使用`scripts/interactive_build.py`（委托`scripts/backend_build.py`），从同一源码清单构建全部8个程序及内嵌页面，新输出目录不覆盖已有包。`build_info.json`、`CURRENT_SOURCE_MANIFEST.sha256`和实际命令绑定版本；软件GET /api/v1/build可核对源码哈希。开发默认值不冒称发布版。

旧M5/M6/SM2/OpenSSL/race记录保持原字节与原版本来源，不能作为本次全面安全认证或当前实时性能。历史阶段的正式1000次/1800秒结果保留原来源；后续已列明的补测独立记录，本次说明同步没有新增采样。M6历史A/B覆盖和检出相同，改进是审计信息更完整；当前任务原始输出和进程退出、回执持久化分别记录。
