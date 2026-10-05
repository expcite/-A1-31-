# backend1 朋友复查定向修复记录

目标：按本次 FriendReview_Followup 修复 F01/F02/F03/F04/F05/F06/F08/F09，保持前端和旧 M5/M6/backend1 原始证据冻结；F07 等待前端授权。

版本 interactive-v1.2-backend2-reviewed；实际工作目录 C:\Users\20477\WorkBuddy\密码技术竞赛\edgehybrid-agent-starter，无 Git。基线源码 ZIP e213c49ca69cbe515cdf15ab92f8e9b15ecece4860516940465c52ba650cfe37 对应原367清单52de021cd4f322432448e6e65b45d2b28e56cc6b54f54bb2d8c7b62c965b0d3b。当前388清单21a723b1443f33e9e9fcbfa766060f0100df780b5a8c069f21cac172a8396d1b。所有8个EXE由当前源码新构建。

实际修改位置与差异见 deliverables/Backend_Reviewed_Fixed/SOURCE_CHANGES.json、ACTUAL_CHANGES.patch；没有删除既有源码。两个 CURRENT 文档原来存在但未纳入367清单，本版纳入清单并保存原字节，不能把“新增到清单”当作新造文件。生产修改集中于 app 状态机/网关本地所有者、wire/Channel 错误链、identity/management 事务、console Cookie、pbench 拒绝前提，密码算法/H1–H4/记录编码未重写。

## 实际测试与命令

所有命令/JSON/stdout/stderr/真实退出码/逐次源码清单在 results/raw/backend_friend_20261002；RAW_EVIDENCE_INDEX.json 提供索引。

|检查|状态|真实结果与边界|
|---|---|---|
|修前多实例/分帧、保护ACK、CLI本地吊销、轮换、P前提|FAIL|各同场景缺陷复现真实退出1，修前日志保留|
|开发中驱动编译/基线记录/普通全量迭代|FAIL|不算缺陷证明，修正驱动后重验；早期记录保留|
|go mod verify / vet / build ./...|PASS|全量真实退出0|
|go test -count=1 -json ./...|PASS|29包；425顶层PASS、477子测试PASS、0 FAIL；1可选长测SKIP|
|Windows go test -race -count=1 -json ./...|PASS|同样测试统计，真实Windows GCC15.2/MinGW13 UCRT隔离工具链；只覆盖实际执行路径|
|既有Python验收器/Node前端自测|PASS|各真实退出0；前端源不变|
|独立SM2核心交换与OpenSSL|PASS|BC第二实现14组核心交换，OpenSSL动态双向签名互操作；不是标准S1/S2或全面认证|
|独立cmd/gateway正反例|PASS|T/H各20设备：正常、只吊销D010、网关吊销/退役/当前旧键失效；持久拒绝与观察时刻记入真实事件|
|真实管理API/CLI轮换恢复/legacy|PASS|真实.tmp提交失败后同owner/Registry/process重试，CAS/赢家引用/不确定持久状态、首代文件保护与正常对照|
|双真实console同host多port认证|PASS|共享CookieJar；OTP/CSRF/Origin/过期/停止/再启动隔离；不是抵御同机恶意进程的强隔离|
|新版EXE 1设备与timer|PASS|26.3/28.7、暂停后主动控制、真实1秒→5秒；HTTP排队不等于ACK|
|新版EXE 20与D001/D010/D020失效|PASS|全20正对照；三种失效各其余19台被尝试并正常通信|
|新版EXE生命周期/API/导出/停止重启|PASS|1/20网关世代维护、旧SID不重放、吊销/退役持久、任务验收器与导出真实对应本次运行|
|新版短持续运行|PASS|20台120.047秒，资源序列与清理/重启；不声称1800秒长测|
|T/H/P小规模|PASS|各10次实际握手和业务，真实结果验收器退出0；P三项拒绝各精确原因，同素材健康基线先通过|
|旧16批正式结果只读验收|PASS|当前验收器核对旧run/源码/原始数据，没有采样|
|F07日志选项刷新|PENDING_FRONTEND_AUTHORIZATION|未改前端，不写已修|
|本版正式1000/1800、物理断网、真人试用、标准S1/S2与安全认证|NOT_EXECUTED|未产生相应新结果，不从旧日志扩写|
|最终独立解压验收|以外置回执为准|实际Windows包运行、源码重建/全量Go/旧16批只读核验，结果写ZIP旁verification.json及verification_logs|

风险与范围：异常ACK复现需要已认证端点发送真实保护消息；截断分类不等于绕过加密；跨进程管理仍要求单写者，CAS限定同Registry实例。旧P拒绝若缺健康前提不能证明目标机制；历史正常性能样本不修改。旧300秒测量脚本失败和随后完整1800秒记录保留原版本，修正脚本的复现入口在 README/REPRODUCE。

下一步：交付这批定向缺陷修复及两包独立回执后停止，等待用户决定前端方案；不新增功能或里程碑。
