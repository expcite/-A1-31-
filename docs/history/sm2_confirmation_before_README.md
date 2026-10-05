# EdgeHybrid 核心冻结验收输入版

版本 interactive-v1.2-backend2-status1-releasefix3-finalize2，继续真实releasefix3工作树，不用旧ZIP覆盖。当前密码协议、生命周期CAS、安全检查及前端全部保持基线；只完善执行工具、测试、输入文档和发布验收。文件名Core_Frozen不是通过证明，当前结果以外置且匹配哈希的验收回执为准。

[用户手册](docs/user/USER_MANUAL.md)、[API](docs/INTERACTIVE_API.md)、[构建和验收](docs/BACKEND_BUILD_AND_VERIFY.md)、[冻结与执行范围](docs/CORE_FREEZE.md)、[当前预登记](docs/CORE_FINAL_EXECUTION.md)、[原题要求](docs/REQUIREMENTS.md)。Windows软件双击启动1设备.cmd或启动20设备.cmd，使用启动窗口的一次口令授权；停止入口退出，关网页不等于停止。用户运行不依赖Go/Python/Node/Java/CDN。

T/H采用认证的SM2核心交换、SM3派生、SM4-GCM；H同时绑定真实ML-KEM-768。P为TLS1.3/X25519MLKEM768公开混合对照，整体差异不全归于ML-KEM。BC核心交换、OpenSSL签名/HKDF、项目Finished和标准SM2 S1/S2分别报告；现协议没有标准S1/S2接口，本轮不实现、不计通过。

手动温度、网关主动控制、实际timer、1/20设备、设备/网关生命周期、隔离任务、有界日志/导出保留。授权、CSRF、Host/Origin、防降级、Finished和抗重放不改。HTTP接受不等于通信确认；超时不能断言对端未执行；吊销身份不会自动恢复。

当前执行先普通回归、专项验证及工具自测，完成输入修改后一次冻结。正式16批固定分母、不补成功数；长测1800秒与race/构建/M6不重叠。报告写外部，不改冻结源码或旧raw。当前CURRENT_DELIVERY_INDEX.json生成于实际审查包根，列明当前与历史证据、文件hash和必要Windows包。独立解压使用scripts/core_release_verify.py；旧scripts/release_verify.py保留为历史32批/M6布局验收器，不能替代当前门槛。

内嵌历史性能图仍属于其原版本；新性能报告单独从本轮raw重算。0ns、计时局限、失败、null指标与原因保留。进程批次CPU/峰值不冒充单次指标，1000/1000不推成未来100%。M6保留原A断言与三个隔离变体，以当前冻结源码重新比较；漏检/不可达与执行错误分别保留，不预写B优于A。

原始历史milestone和先前README/说明已保留。项目许可证由权利人决定，本轮不擅自授予开源权；测试BC组件是指定hash的本机JetBrains发行物，不声称官方JAR签名已核验。
