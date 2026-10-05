# EdgeHybrid 标准 SM2 确认增补版

当前版本 interactive-v1.3-sm2confirm1，在已有工程上补标准 SM2 S1/S2，不重建项目。前端源文件冻结，界面完善留待用户下一轮方案。

[用户手册](docs/user/USER_MANUAL.md)、[本轮范围与复现](docs/SM2_CONFIRMATION_PLAN.md)、[API](docs/INTERACTIVE_API.md)、[原题要求](docs/REQUIREMENTS.md)。双击启动1设备.cmd或启动20设备.cmd，用启动窗口的一次口令授权；停止入口结束进程，关闭网页不等于停止。用户运行不依赖 Go/Python/Node/Java/CDN。

T/H 均强制标准 S1/S2：网关 H2 签名覆盖 S1；设备验证后 H3 签名覆盖 S2；网关验证 S2 后仍需验证 Finished。原签名、防降级、混合双秘密派生、SM4-GCM、抗重放和生命周期 CAS 保持。握手消息版本 2 拒绝版本 1；外层记录格式保持版本 1。

本轮独立对照与 TCP 回归单独记录，不把旧 14 组核心对照或 OpenSSL 签名互操作冒称为标准确认测试。status 不执行安全检测。实际平台、源码、EXE、run 和退出码见 DELIVERY_INDEX.json、reports/ 与包外 verification.json。

旧 M5/M6、Core_Frozen 的 1000 次正式批次和 1800 秒结果保持原始字节，属于原源码版本；本版不借用旧数据宣称相同性能或长测通过。新增确认使 H2/H3 各增加 36 字节，需后续单独测量才可给新性能数字。M6 的既有结论仍是审计信息更完整，不是覆盖率或检出率提高。

项目组合没有形式化证明或合规认证；SM2 身份认证不是后量子认证。BC 为已有本机 JetBrains 分发测试库，独立结果范围限于固定曲线、输入及确认编码，不称全面安全认证。历史失败、跳过和未执行原样保留。
