# 当前交接

当前程序发布版本为 `interactive-v1.6.2-e08fix1`，前端为 `MINIMAL-UI-v1.0`；提交为 Final_v13（说明同步 1）。既定软件原型与验证材料已完成，当前无必须继续开发或补测的交接项，范围以 `../ACCEPTANCE_STATUS.json` 为准。

运行入口为 `../runtime/启动交互平台.cmd`，使用输出的 PAGE_URL 和本次一次口令；停止入口为 `../runtime/停止交互平台.cmd`。E08 修复、新工具链构建、定向回归和发布业务短流程已归档在 `../02_evidence/`；G01—G07 结果见 `../experiments/focused_retest_v11/` 及其独立复核。

本次仅修正文档版本标记，未改协议、CAS、安全参数、Go/前端逻辑、程序或原始结果。原程序绑定的构建源码清单为 `02863dd2efb03dd8885fb45133fdac44128eb462f581d03301417068f39b29ad`；更新后的说明树身份单列在 `../verification/document_sync_v13_1/`。以后重新构建应使用实际当前树的新清单，不得将现有 EXE 重标为新清单构建。

旧阶段交接、失败和中间记录保留原字节与原版本来源，不视为当前待执行指令。原文快照见 `../history/v13_before_document_sync/source/`。不要将旧性能或未执行/未实现工况写成当前新测 PASS。
