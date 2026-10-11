# 当前完成度与证据范围

当前提交：Final_v13（说明同步 1）；程序发布版本：`interactive-v1.6.2-e08fix1`；前端：`MINIMAL-UI-v1.0`。8 个随包程序实际使用 Go 1.27.2 windows/amd64 构建。既定软件原型、补测 G01—G07、报告与封包范围已完成，具体边界见 `../ACCEPTANCE_STATUS.json`。

四主入口、真实 API、认证 Channel 与 G3 因果回执沿用已验证实现。E08 已合入 fleet 负值拒绝并完成 Go 1.27.2 重建；所选 25 项定向回归、7 项 CLI 边界与发布程序业务短流程均有原始证据。新 govulncheck 的 4 个模块级 finding、0 符号调用路径按适用性裁定，不能推导全仓零漏洞。证据见 `../02_evidence/`、`../verification/focused_v11_review/`。

本次仅同步版本和状态文档；生产 Go/前端代码、8 个 EXE、PDF/HTML、配置及实验记录均保持原字节，没有重新执行测试或性能采样。原构建源码清单 `02863dd2efb03dd8885fb45133fdac44128eb462f581d03301417068f39b29ad` 与程序身份保持不变；文档更新后的源码清单及原构建快照映射见 `../verification/document_sync_v13_1/`，两个身份不得混用。

旧 M5/M6、SM2、Windows race、T/H/P 及 Go 1.27.1 测量保留原来源与执行状态。旧 UI 阶段说明保存在 `../history/v13_before_document_sync/source/`。可选工况的范围排除不等于 PASS；实体工业环境、ARM64/MCU、物理侧信道、完整形式化证明及主办方验收不属于已完成声明。
