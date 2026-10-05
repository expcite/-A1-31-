# 当前实现与验证范围

版本 interactive-v1.3-sm2confirm1。标准 S1/S2 已接入真实 T/H 握手，消息版本 2 必需；原签名与 Finished 均保留。新版独立对照、TCP 正反例、常规回归/race 与实际运行结论以审查包 reports/ 和 evidence/ 为准。

八个页面、真实手动遥测/主动控制/timer、1/20设备、网关/设备生命周期、分页筛选日志与导出、任务隔离和本机授权继续保留；本轮未改前端。页面存在不等于新证据全部通过。旧版本完成表的原文见 docs/history/sm2_confirmation_before_CURRENT_COMPLETION.md。

已知限制：新旧握手不兼容；新版正式性能/1800秒未自动重测；历史图表按原测量版本；标准确认有限独立对照不等于标准协议或认证。跨重启 exactly-once、形式化证明、完整侧信道/DoS审计及全平台验证没有补齐证据。
