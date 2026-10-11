# 五项修订范围与复现

基线interactive-v1.2-backend2-status1，398项源码清单d4622b841e570e511f06c61f40174111544c5e3b73e2aa08a87734b4ce50abcc；基线ZIP7faafa17f432dd3ab6a194bccf7921f299c2add475e6adb879c04d66e7ea5a13。本轮继承工作区，不从旧ZIP拼最新源码。

F07使用朋友候选等价完整补丁，只改logs.js功能逻辑与新增测试。手工应用时首次漏接结尾上下文导致语法错误，原失败保存after/F07；补齐完整hunk后同例记录after/F07_complete_hunk。不删除该开发期失败。布局/CSS/nav/其他页面未修改。

D1活动索引从最终发布staging的文件生成exists/hash，旧索引原字节另存；补齐M5/backend1的32批原raw、采样源清单和M6完整源解析上下文。脚本release_verify.py严格JSON/路径/ID/hash/目录成员/来源关联检查，真实重建bench/pbench完整验收器并离线重算。m6_verify.py只调整离线工具不再读取历史作者绝对Demo路径，Demo原哈希仍保存，其未嵌套的旧ZIP不影响AB证据重算，不宣称本次Demo复验。

D2手册转为源码内真实路径，并保留原V12字节。D3说明全部明确审查包根和source/edgehybrid-agent-starter层级。D4只改interfaces.go包头注释，ErrNotImplemented、unknown-suite拒绝及所有算法逻辑不变。secctl默认版本和F07能力说明同步；status仍不检测安全，语义测试仍校验验证边界，原Recheck测试不删改。

## 实际复现入口

node scripts/logs_refresh_test.mjs --assets internal/console/assets；node scripts/interactive_frontend_test.mjs；python scripts/status_review_regression.py --out 新目录 --node Node路径。原生WindowsCheckKit UTF8自测/quick保留原工具、原命令和真实退出码，工具测试不是协议攻击。

python scripts/release_verify.py --root 审查包根 --out 新目录 --go Go路径：Go依赖需可用，结果验收不联网采样。缺依赖阻止通过，旧绝对result_dir只作为原记录，派生portable索引明确映射。

本轮不修改密码协议、后端业务或资源路径；不默认重新正式1000次、1800秒或全量race。普通Go回归中外部对照是否执行读取实际JSON；历史全量race为原source21a723记录。旧性能保留0ns/失败/批次CPU峰值口径，P整体差异非ML-KEM单一效应。S1/S2独立确认仍未执行，M6没有覆盖率或检出率提升。

## 交付交错补验

候选9518814的独立解压构建发现：新归档的修前Go片段被go ./...当作包编译。仅归档文件名加.txt，原字节不变；失败保留，不修改旧冻结原始资料。F07补同一API/run页面恢复重建的有界单视图复用，保持失败前快照/页码/选择；不同API或run不复用。只改logs.js与测试，app.js及布局/导航不变。新增修前反例和修后同例，并重新冻结、构建、回归和真实浏览器验证。早期候选包被撤回保存于开发工作目录，不作为最终交付，也不嵌套到最终包。
