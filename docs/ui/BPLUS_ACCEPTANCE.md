# A01—A42 验收方法

逐项详细结果在交付包 `reports/ui-bplus/ACCEPTANCE.json` 和 `BPLUS_ACCEPTANCE_RESULTS.md`，每项包括 test_id、环境、步骤、预期、实际结果、证据路径、源码版本、run_id及实际状态 PASS/FAIL/BLOCKED/NOT_RUN。该文件只规定复现方法，不预写通过数量。

三类证据分开：Node隔离UI状态/API故障夹具不证明TCP或密码；真实浏览器从本轮嵌入EXE操作既有API，故障注入只作用于独立浏览器只读/响应通道；Windows独立解压运行由原验收脚本验证真实本机TCP/Channel与设备。

1. `node scripts/interactive_frontend_test.mjs` 保留既有计量转换、状态与分页测试。
2. `node scripts/logs_refresh_test.mjs --assets internal/console/assets` 保留F07原全部回归，不删除或放宽。
3. `node scripts/bplus_state_test.mjs` 检查只读超时、迟到/乱序、上下文、草稿/回执、写响应丢失、重复提交、引导引用、有限容量及文本隐私。
4. 原生浏览器测试默认工作台、同run双向操作、焦点/详情至少10轮询、受控路由、危险确认、引导无隐式POST、导出下载和四种视口/200%缩放。A10可在独立测试浏览器暂停state Fetch资源，5000ms内检查过期；恢复时保留离线/吊销的禁用条件。不得把故障注入截图当协议攻击成功。
5. `go test -count=1 -json ./...`、`go vet ./...`、构建；按配置执行原生Windows race及现有独立对照，保存真实JSON/stdout/stderr/退出码。缺依赖要记录阻塞，不能将SKIP改PASS。
6. `python scripts/backend_build.py --out <新二进制目录> --evidence <新日志目录>` 构建来源；`python scripts/interactive_installed_acceptance.py --exe <独立解压EXE> --devices 1或20 --out <新结果>` 检查既有完整API/真实Channel。脚本是审查工具，用户运行包不需要Python。

G3门槛在当前API能力下检验的是不误判因果；本次换钥因果缺口须保留，不写“四步全部完成”。G4导出回执只说明生成，实际下载时另比对字节/SHA；不能把摘要查看写成文件保存。

既有历史正式实验不重新采样。多尺寸截图须注明真实run和版本；隔离UI数据必须单独标注，缺真人试用或屏幕阅读器结果写NOT_RUN。最终交付索引、manifest与EXE来源从最终发布目录生成并独立解压核验。
