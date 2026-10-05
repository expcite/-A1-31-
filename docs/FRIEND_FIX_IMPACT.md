# 朋友复查定向修复与补验范围

基线：interactive-v1.2-backend1；源码包 e213c49ca69cbe515cdf15ab92f8e9b15ecece4860516940465c52ba650cfe37，367 项源码清单 52de021cd4f322432448e6e65b45d2b28e56cc6b54f54bb2d8c7b62c965b0d3b。

当前工作目录为 C:\Users\20477\WorkBuddy\密码技术竞赛\edgehybrid-agent-starter，无 Git。修前源码逐项哈希、前端 22 项哈希、历史原始文件和旧包哈希保存在 results/raw/backend_friend_20261002/baseline.json；外置修前源码备份保留本地修改。

按 F04/F08（真实受保护 ACK/截断）、F01（独立 CLI 网关）、F02/F03（轮换事务与旧格式）、F05（P 拒绝前提）、F06（多实例授权）、F09（当前入口文档）顺序修复，先保存修前失败，再运行同例与正常对照。朋友附带两个原始回归测试；另一份 Friend_Review ZIP 为历史复核集合，未取得其余六项的新增完整工程测试，按明确场景补本机回归。

F04 改变有限设备业务异常终态；F08 改变截断错误分类，不改变记录加密。F01 影响 CLI 本地身份失效后的准入和资源释放。F02/F03 影响轮换提交失败恢复与旧格式。F05 影响安全拒绝结论的有效性，历史过期素材拒绝记录不能证明目标机制。F06 只改变后端 Cookie 命名。密码算法、H1–H4、KDF、记录格式和正常性能计时边界不变。

本轮补验真实 Channel/TCP、CLI/live 生命周期与异常交错、事务恢复、多实例 HTTP、T/H/P 小规模、短有界持续运行、全量 Go/Windows race、现有 BC SM2/OpenSSL 与 Python/Node 验收器。旧 1000 次正式性能与 1800 秒长测仍绑定 backend1；不用于宣称修后源码完成同等规模测量，不重采样、不改旧 raw。该影响范围不要求重采样正常性能；新版本没有正式性能数据。

前端全部冻结。F07 日志选项刷新为 PENDING_FRONTEND_AUTHORIZATION。任何尚未执行的检查均为 NOT_EXECUTED，环境缺失为 ENVIRONMENT_BLOCKED，真实失败为 FAIL，不预先宣称完成。
