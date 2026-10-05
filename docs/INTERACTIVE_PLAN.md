# 交互版本实施冻结计划 v1

工作区为原 edgehybrid-agent-starter；非 Git 工程。公开起点备份和逐文件哈希在包外 EdgeHybrid_Interactive_Work。旧 Demo、M5/M6 raw 保持冻结。新增 run 使用 interactive 前缀。

A：真实 D001 长连接、设备 actor 调度、唯一 Receive 分发、网关主动控制、受权本机 API。C01–C05 和最小 API 授权通过后才扩展整合页面。

B：真实 20 设备；八区导航；同一 CAS 身份服务；隔离固定实验；有界任务与导出。历史 M5 只读呈现，保留 0ns、分母、失败和计时局限。无新性能等价声明。

C：全量 Go 原始 JSON/退出码，浏览器操作截图，独立解压验收，三个用户指定产物与有效清单。race/独立 SM2 交换无新证据时仍 pending。

边界：套件和 H1–H4/KDF 编码不变。新本地 interactive-v1 profile：首字节空闲预算 30 分钟，部分帧 15 秒；会话 15 分钟或 512 业务发送后完整换钥。设备同会话控制去重上限 256；达到阈值关闭/完整握手，不清空旧 key 去重历史。每设备队列 32，当前操作最多 256，事件内存 2000、公共日志最多 20MiB，HTTP body 8KiB。日志写入失败使运行进入错误并停止工作者。

ACK 超时 3 秒：写出后未收到匹配 ACK 标 unconfirmed（可能已执行），迟到正确 ACK 标 late_confirmed；旧 SID、不匹配 operation/周期/Applied 的 ACK 不确认。不同载荷复用 request_id 拒绝。没有跨重启 exactly-once 承诺。

真实采样验证：1000ms 连续至少 3 个间隔，5000ms 至少 3 个间隔；允许 Windows 调度误差 ±25% 或 250ms 取较大者，并保留实际时间戳。可控时钟单元测试精确验证 reset/pause。持续验收 10 分钟，不靠有限 Demo 反复重启；暂停后仍可下发。

未知对端状态为 null。HTTP202只表示排队。本机 HTTP 并非 HTTPS；owner 会话由本地一次口令建立，Cookie HttpOnly/SameSite=Strict、防伪令牌、Host/Origin 精确匹配。GET 不改变状态。破坏性管理动作需确认身份和动作，生产无认证绕过。

计划 37 项，逐项按真实证据填状态，不以计划数代表通过。参考作品原 PDF 未找到时不声称读取，使用任务包限定的图形流程与报告结构。
