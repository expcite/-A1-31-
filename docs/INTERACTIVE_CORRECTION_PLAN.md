# interactive-v1.1-corrected 定向修订冻结方案

来源：本次 EdgeHybrid_Interactive_Audit/REVIEW.md、SOURCE_EVIDENCE.md、AGENT_NEXT.md、候选补丁、真实工程回归模板及evidence。候选通过非密码隔离回归，不等于完整工程验证；采用等价修复并补会话、本地请求和终态处理。未复制isolated测试替身，未修改密码组件或认证规则。

I01：本地请求绑定run/设备/SID；发送前独立检查操作run/设备/kind/SID/queued。终态不重新writing。actor结束关闭准入、排空有界本地队列、完成等待者并保留旧SID/失败状态。sampling携带调用context，超时且尚未处理的请求拒绝；若处理已开始，超时不能作为未执行证明。控制发送也检查当前session实例与操作范围，写入中会话断开按unconfirmed处理，失败/取消ACK不确认。

I02：StartAllDetailed逐设备尝试并返回20项准入结果与accepted/rejected计数；StartAll保留聚合error兼容CLI/测试。POST /api/v1/run/start返回200结构体status=accepted/partial/rejected，而非首错409；准入与真实握手在线状态分开，前端逐项显示。不能把accepted解释为握手成功。

真实回归：有界测试暂存队列与manager mutex构造确定顺序，实际TCP/真实Channel正常标记证明排空；覆盖旧SID queued/failed、当前终态、错run/设备/kind、换钥优先、断连重连、超时sampling、旧sampling/rekey；控制终态也不能重发。20端点全成功及D001/D010/D020吊销分别验证其余19完成Suite2真实握手和遥测/主动控制ACK。

完整回归：go test -count=1 -json ./...，既有Python 3套自测及Node前端；实际Windows新exe一设备26.3/28.7、暂停主动控制、1s/5s节奏、20设备、API授权防伪、导出、停止重启。新核心执行独立10分钟Hybrid持续回归，含静置主动控制和换钥；旧616秒仅为历史。不采样旧M5正式1000次。

网关生命周期需求缺口：management.Apply和secctl rotate只支持device，GUI无网关操作，未找到另外的受控网关轮换流程。最小后续方案：串行管理锁→暂停网关新准入→关闭/观测全部旧会话与未完成任务→按网关角色生成签名/协商新世代且验证文件→CAS提交→加载新配置→显式设备新握手→旧key拒绝→旧文件受控清理。写入/CAS失败保留原世代；提交后清理失败记录待清理；吊销停止业务且不自动恢复信任。应补真实TCP正常/失败/旧key拒绝/吊销/退役测试，不能删除角色判断实现。本轮仅核对与计划，不宣称此缺口已实现。

日志页最近80条，内存事件上限2000、公共日志20MiB、有界导出；没有完整筛选分页。体验待办保留。race与独立SM2交换pending、OpenSSL历史SKIP、物理断网/系统DPI/跨机硬件未验证。

构建来自当前工程，冻结清单在results/raw/interactive_corrected_20261001/freeze；新版exe哈希、run、测试、截图及手册按同清单关联。失败/中断不删除。旧版本原始记录和ZIP保留。
