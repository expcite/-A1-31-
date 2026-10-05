# M5B Windows Demo 阶段记录

目标：基于保留的M5B Corrected交付最小Windows一键演示；正式1000次/M6不在范围。
唯一源码目录：C:\Users\20477\WorkBuddy\密码技术竞赛\edgehybrid-agent-starter。

修改文件：新增cmd/demo、internal/demo（协调/事件/状态/HTTP/Windows平台/测试/嵌入assets）、scripts/build_demo.ps1、demo_windows_acceptance.py、demo_m5b_regression.py、package_demo.py、docs/DEMO_QUICKSTART.md、LICENSE.txt、THIRD_PARTY_NOTICES.md及第三方许可原文；更新README.md、PACKAGE_INFO.md、CODEX_HANDOFF_M5B.md、source_manifest.py；追加M5B_CORRECTED的D020勘误；新增DEMO_TEST_REPORT.md与本记录。旧Go实现/测试和依赖未改，详见final_preserved_source_check.json。

命令与退出码：results/raw/demo_windows_20260930/final_command_index.json；原始stdout/stderr、command.json、exit.txt随包。最终Go23包、343顶层/291子测试通过、1OpenSSL签名对照跳过、0失败；vet/modverify/allbuild通过。1/20/19+1真实Suite2分别3/60/57笔网关确认；原E2E逐正常设备40次通过。Suite1/Suite2原入口通过。M5B离线6正例退出0、36反例退出5。最终exe无开发工具PATH、中文空格独立解压、真正双击与自动浏览器、状态/异常/停止/重启/重复/端口及连接数测试通过。公开日志和真实截图见新raw目录。

失败测试：两次GUI开发验证编排失败（环境键大小写、日志目录重名），未冒称exe失败或成功；随后修正验证编排并通过，保留原始记录。异常D020退出3/拒绝反例退出5符合预期。最终全量Go、应用验收无失败。

仍未实现/未执行：正式1000次、M6、race、独立SM2交换对照、Linux资源、新Windows跨机覆盖；P CPU/RSS保持pending/null；Ctrl+C本轮未单独发信号实测；Windows ACL额外加固与发布签名未实现。本地原型/模拟业务，不是部署或合规证明。

风险：未外部安全审计；继承用户目录权限；异常强制退出可能保留独立private，不能分享；只分享logs。没有新增协议/密码/成功规则或性能排名。正常停止只清理本次资源。

下一阶段：最小演示完成后停止，等待用户授权；不自行扩展后台或进入M6。最终源码清单hash f79964fff00733e40c4b58b1eb1eb65b60928c09afb8736a81fe467bd3ce8db8；exe hash fa614b5daf1f6c2cab19c0a56c614e09356badf5d9020eb69e03588aaa52e699；两ZIP及包验证见DEMO_TEST_REPORT.md和相邻交付验证记录。
