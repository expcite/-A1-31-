# EdgeHybrid 简约交互平台

当前版本 interactive-v1.5-minimalui1，前端 MINIMAL-UI-v1.0。四主入口：工作台、设备、记录、实验；管理、帮助、当前运行/停止是辅助入口。保留已有授权、CSRF、Host/Origin、配额、会话隔离及真实 Channel 通信。

[快速入门与完整说明](docs/user/MINIMALUI_MANUAL.md) · [功能入口与修改范围](docs/ui/MINIMALUI_IMPLEMENTATION.md) · [API](docs/INTERACTIVE_API.md) · [原题要求](docs/REQUIREMENTS.md)。双击启动1设备.cmd或启动20设备.cmd，使用启动窗口的一次口令授权，再明确连接。用户运行无需 Go/Python/Node/Java/CDN；关闭网页不退出软件。

本轮前端布局与展示重组不改变密码协议。用户另行授权补齐G3换钥因果回执：明确操作ID绑定run、设备、旧SID和消费请求的工作者实际建立的新SID；真实双端握手准入完成与新会话业务验证分别核验。HTTP受理或任意SID变化不能完成G3。自动换钥、失败、旧会话请求、淘汰记录、未知提交仍诚实显示；不会自动重试。

当前构建、截图、测试及退出码以同版本build_info、源码清单和本轮回执为准。旧M5/M6/SM2/backend性能与长测保持旧字节和原来源，不代表新版正式测量；本轮不重新采样1000次或1800秒。M6历史改进是审计信息更完整，覆盖和检出相同。标准SM2独立验证按对应历史回执范围说明，不代表全面安全认证。
