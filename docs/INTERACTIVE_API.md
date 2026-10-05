# 本机控制 API v1

绑定127.0.0.1随机端口；Host精确匹配；POST的Origin须精确为本运行URL。Content-Type为application/json，body最大8KiB，重复键/未知字段/尾随内容拒绝。速率120请求/秒，登录每分钟最多5次。单机owner，无多用户RBAC。

首次POST /api/v1/login携带启动窗口一次code，获得HttpOnly、SameSite=Strict Cookie及独立csrf_token。code只在启动窗口显示，不在URL、公用日志或导出。会话1小时过期后重新启动建立新本机会话；HTTP没有声称HTTPS。后续POST须X-CSRF-Token。GET /api/v1/session供同一会话刷新取得防伪令牌。状态API也需owner授权。

|方法与路径|用途|完成含义|
|---|---|---|
|GET /api/v1/state、devices、events|当前run快照|只读|
|POST /api/v1/run/start、stop|连接/断开固定可信设备池|本机调度/停止|
|POST /api/v1/devices/D001/connect、disconnect、rekey|设备连接/完整换钥|202仅接收调度|
|POST /api/v1/devices/D001/telemetry|request_id,temperature_c|202队列；网关验收另查操作|
|POST /api/v1/devices/D001/sampling|auto,temperature_c|设备actor本地开关完成|
|POST /api/v1/gateway/commands|request_id,device_id,interval_ms|202排队；真实Ack才确认|
|GET /api/v1/operations/{id}|状态、SID、真实时间戳|null代表无观测|
|POST /api/v1/operations/{id}/cancel|只取消未写出队列|写出后不能保证取消|

温度沿用原app.ValidateTelemetry范围；周期100..60000ms。request_id最长128字符，当前run同ID同参数幂等，不同参数409冲突。有效HTTP请求不是安全通信成功率的分母。

queued → writing → written → confirmed；peer_processed_at单独来自设备实际timer事务；ACK超时unconfirmed（可能执行），迟到正确ACK为late_confirmed。写入失败可能部分写出，保留unconfirmed；排队未写出会话结束为failed。旧SID或错误ACK记录ack_rejected，不完成其他操作。

## 管理、实验与导出

GET /api/v1/build 返回版本及生产者源码hash；history返回冻结M5来源；devices/{id}/sessions仅为当前run的有界会话观察。GET devices/events均返回当前快照对象中的对应集合，不是假历史分页。

POST identities/{id}/rotate、revoke、retire 的body为confirm=动作:id。policy另含mode=traditional或hybrid；仅支持本机设备管理，网关管理明确拒绝。提交返回注册表revision、网关实际采用revision和会话关闭观测，不能把保存等同全部生效。

POST jobs 的kind只能是m6_rules、bench_T、bench_H、pbench；最多8个，单任务40秒，live互斥。GET jobs/{job_id}取得真实退出码；POST jobs/{job_id}/cancel body为{}，保留取消与已写输出。

POST exports body含format=json/csv/html、include_payload（默认false）；true时必须confirm=include_simulated_payload。返回内部下载url、实际bytes及sha256；GET exports/{export_id}也要求owner。最多16文件、每份4MiB，仅内存导出，不接受文件路径。

完整实际路由、字段及状态码见INTERACTIVE_OPENAPI.json。所有空body写操作仍须JSON对象{}。JSON字段禁止跨路由混用。

## interactive-v1.1-corrected 连接批次

POST /api/v1/run/start 返回HTTP200以及run_id、status(accepted/partial/rejected)、attempted、accepted、rejected、results[{device_id,status,error}]。逐台准入，首/中/末失败均继续；accepted仅表示Connect已接受，不是握手确认。授权/输入错误仍保留401/403/400。请继续读取state判断真实在线。

本地sampling/rekey绑定run/身份/SID，过期或旧会话请求不在新会话执行；旧操作不改写SID或重发。日志页最近80条，没有完整检索分页。


## interactive-v1.2 增量契约（历史章节为v1/v1.1来源）

POST identities/GW001/{rotate,revoke,retire,cleanup}：独立网关维护事务，confirm必须action:GW001:ALL；会停止准入并关闭全部设备。commit.registry_committed、all_sessions_closed、admission_reopened、load_error、cleanup_pending分别观测；不以HTTP200表示已经重连或远端确认。设备原confirm格式保持。

GET state：maintenance.admission_blocked/phase/error与capacity增加；gateway使用公开Metadata展示吊销/退役，握手仍用拒绝吊销的Lookup。事件gateway_authenticated是握手时快照。

GET session增加expires_at，不续期；未授权/过期/CSRF拒绝返回401，code=authorization_expired。POST run/exit仅owner+CSRF，confirm=exit:<当前run>；返回stopping，随后真实关闭工作者。run/stop只断开设备。

POST exports可选selection={run_id,from_event_id,to_event_id,device_id?,endpoint?,stage?}；基于固定事件范围和精确过滤，响应event_count及selection；JSON/HTML记录selection，CSV列selection_json逐行记录。无selection保持整份有界快照。默认温度载荷删除；include_payload=true必须单独confirm=include_simulated_payload。过期范围或错误run拒绝，不偷偷改分母。空CSV无数据行，范围仍在创建回执中。

409 code=queue_full：待发32队列，等待现有发送完成。409 code=operation_limit：每run累计256操作，导出后停止重启；换钥不清账本。实验8/导出16/事件2000/公共日志20MiB边界保持。所有修改动作仍精确Host/Origin、8KiB严格JSON、未知/重复字段拒绝。HTTP接受只表示本地排队。


## interactive-v1.5-minimalui1 G3因果回执增补

POST /api/v1/devices/{id}/rekey 可选 request_id（1..128字符），其余未知字段仍拒绝。旧{}调用由服务端生成公开ID。202只表示本次排队，返回run_id/device_id/rekey_id/old_sid/status及queued_at；new_sid初始为空，completed_at初始null。GET state.rekeys包含独立上限256的当前run回执；消费本次旧SID请求的实际工作者才关联下次完整握手，双端真实准入后confirmed。自动换钥无此ID，不借用其他换钥完成记录。同设备同ID不重排队，不同设备同ID拒绝；故障/断开/旧队列不重发。客户端依然不自动重试未知提交。G3完成还要求之后同new_sid的手动遥测确认。新数据不是旧版本测量结果。
