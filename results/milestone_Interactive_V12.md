# interactive-v1.2 最终验证报告
本轮完成A网关长期密钥生命周期与B新手主流程，在固定范围停止。当前源码直接来自工作区C:\Users\20477\WorkBuddy\密码技术竞赛\edgehybrid-agent-starter，未以旧ZIP组装；无Git，339项源码清单SHA-256 0febefe557f00534d93aa6b40b89f2a19fd5e010004069827ee4067aa4242fe8，本次EXE SHA-256 ce3cc09583d7cf5c16ed52973f4df8b0070453033fd293cec9fd1fa86dcc8779。源码、最终Go、7组最终EXE、浏览器截图、手册来源一致。

网关受控维护：分离长期签名/协商钥准备与校验、单写者、停止准入、全部旧连接与队列归属清理、CAS提交、校验加载、明确新世代重连、尽力清理。真实新世代单台/20台成功，旧钥端点真实TCP拒绝；吊销/退役重启持续拒绝。失败安全包括真实文件失败、CAS冲突、提交后加载失败关闭准入、清理失败pending。入口为高级GUI/owner HTTP与独立management服务，设备secctl保持原范围。仅本机可信更新，非远程PKI/跨进程锁，删除不是物理擦除。

新手默认简洁导航与五步向导、分开的温度/间隔任务卡、自动ID、严格秒→毫秒、技术折叠、真实排队→确认通知、未知禁写、独立手动回执/自动源/最近接收值。日志对现有2000事件固定范围筛选分页，每页20条，导出同范围；不建设无限历史库。高级功能与安全权限保留。

|验收|实际结果|原始记录根下的路径|
|---|---|---|
|最终全量Go及完整JSON验收器|386顶层PASS，371子测试PASS，2顶层SKIP，0失败；真实退出0|all_go_shipping，complete_go_shipping_verifier，go_shipping_recount.json|
|Node前端|43断言PASS，退出0|frontend_shipping|
|Python既有验收器|汇总17/M4CASE45/fleet23/bench9/M6拒绝5；退出0|*_selftest；historical_m6_resolved，historical_m6_recomputed.json|
|实际最终EXE单台/20台|手动26.3/28.7、空闲主动控制、真实节奏、断开/换钥、幂等、授权API/导出、容量、停止重启|shipping_installed_one/twenty；命令真实退出0|
|首/中/末坏设备|D001/D010/D020各失效，其余19均被尝试并真实收发，全20正对照保留|shipping_bad_*及20对照|
|最终EXE网关生命周期|单台/20台新世代双端真实认证、旧请求不跨会话、吊销/退役跨重启拒绝、清理与退出|shipping_gateway_one/twenty；退出0|
|新持续运行|616.12秒PASS，完整新SID握手后保持在线，暂停16秒后5000ms指令ACK确认|continuous_shipping_resume/events/summary；完整验收器退出0|
|实际浏览器|两组独立run、手动/主动确认、1秒→5秒实际接收间隔、20独立在线、D010定向、键盘D009、离线/退出禁写、刷新不重发、筛选分页下载|browser_shipping_one/twenty，screenshots；UI_EVIDENCE.json|
|文档|一页快速入门、六页完整PDF与MD，全部页渲染目视检查|DOCUMENT_VERIFICATION.json|
|历史冻结|3981项hash未变；16批M5只验收重算，不重采|historical_preservation_delivery.json；history_reverify|

原始记录根：results/raw/interactive_v12_20261002。完整命令与真实退出码另见COMMAND_INDEX.json。历史16批复验使用57f6源码的Release_Binaries，最终0febe仅调整验收器环境键处理，与测量/验收Go源逐一相同；旧执行回执没有改写为最终二进制。历史M6阳性校验按原归档来源解析，再运行5个坏结果拒绝；不把坏结果文件当协议攻击。

保留失败：gateway_first真实清理问题已修，原始失败不删；continuous_final默认10分钟Go超时，约600秒退出1，不能算完整通过；continuous_shipping工具中断无完整回执；最终另跑正确12分钟预算成功。启动CMD退出码问题、SystemRoot大小写、CP936后处理与delivery_reports的大小写路径错误均保留原记录及修复后的成功。截图助手的run标注错误保留原记录，以保存DOM和启动回执生成校正索引。没有“全部历史命令都成功”的结论。

root VERSION、IMPLEMENTATION_STATUS.json、PACKAGE_INFO.md和旧M5/M6材料是保留的历史CLI/交付元数据，不代表新交互应用版本；当前以本报告、CURRENT_COMPLETION.md、build_info.json及运行help/build为准。不改变旧采样来源映射，不把历史M5当新版本性能。0ns、失败分母、批次CPU/峰值口径及P/T/H归因限制保留。

race、独立SM2交换仍pending；OpenSSL签名SKIP；真人试用/满意度/上手时间未执行；跨机硬件、物理网卡断开和Windows系统DPI未验证。实际浏览器1366×768和1094×614 CSS视口主要结果可见且无横向溢出；Ctrl+缩放没有确认变化，不能称系统125%实测。没有整体密码认证/安全证明。清理遵守目标key-id，有运行身份不自动恢复。最终包外解压回执另附，不循环引用ZIP自身。

修改文件列表见deliverables/Interactive_V12/SOURCE_CHANGES.json。风险与未实现项如上；下一阶段：无，按用户要求停止。
