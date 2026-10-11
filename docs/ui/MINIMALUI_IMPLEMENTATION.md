# 功能入口映射与实施边界

基线 interactive-v1.4-bplus1：468项，a2d3d114ccb8e571642fcdddd94e76634face19ef9dc48996055045752173574。保留原AGENTS、本地修改和旧证据。实际工程见本轮PACKAGE_INFO的来源字段，评审使用包内相对路径。

|新位置|保留功能|真实原接口|对应源码|
|---|---|---|---|
|工作台→发送温度|手动值、提交快照、独立回执|devices/{id}/telemetry|workbench.js、Store、createAPI|
|工作台→设置间隔|秒严格转毫秒、主动控制|gateway/commands|workbench.js、usability.js|
|工作台→采样与会话设置|暂停/开启、自动源、断开、完整换钥|devices/{id}/{sampling,disconnect,rekey}|workbench.js、drawer.js|
|设备|设备池、连接、定向维护、批量摘要、失败优先|run/start、run/stop、devices/{id}/connect|devices.js|
|管理→设备身份与迁移|轮换、吊销、退役、最低模式|identities/{id}/{rotate,revoke,retire,policy}|keys.js、原CAS服务|
|管理→网关维护|轮换、吊销、退役、清理、影响全部提示|identities/GW001/*|keys.js、原维护窗口|
|记录→当前运行|有界日志、固定筛选分页、JSON/CSV/HTML、载荷危险确认|state、exports|logs.js、原F07逻辑|
|记录→历史|旧M5/M6来源和结果|原history|performance.js（历史数据字节保持）|
|实验|4种固定任务、互斥、取消、真实退出码|jobs、jobs/{id}/cancel|jobs.js|
|帮助|快速开始、术语、限制、版本、启用引导|读取build/session；引导与工作台共用写接口|help.js、guide.js|
|当前运行/停止|授权、容量、停止、退出|run/stop、run/exit|app.js、共享详情面板|
|操作详情|分阶段时间、公开原记录、仅queued取消|operations/{id}/cancel|drawer.js|

修改计划：应用壳/导航app.js、index.html；可读token样式style.css；工作台双标签/设备批量/管理选对象/记录折叠/实验分层；共享drawer与引导。原api.js不变，store.js仅前端版本元数据。CLI版本、导航能力和构建frontend元数据单列。

新增授权例外：G3最小本地观测回执，涉及live/{rekey_receipt,device,state,dispatch,request_scope}.go及console/{handlers_live,schema}.go；只关联已有真实完整握手，不改H1–H4、KDF、记录编码、密钥来源、生命周期CAS或安全校验。rekeys账本独立上限256，不清空旧同key计数、不增加并发Receive。run/device/oldSID/请求状态检查仍执行。完整握手双端准入不是新业务消息；G3还需要之后的新SID受保护遥测确认。

先保留源码基线，再执行产品入口规划、UI_ONLY预览、原生模块实现、原测试与新增回归、真实Windows/API/TCP/浏览器、独立解压及版本清单。预览仅设计参考，不入正式软件、不作为通信成功。实际结果在本轮报告，未执行不预填PASS。

新增G3回归先在浏览器及真实HTTP失败，原因是旧严格路由不接受request_id；只为devices/{id}/rekey增加这个可选字段，未知字段/CSRF/Origin仍拒绝。兼容原{}调用，由服务端生成公开随机ID；API响应为排队回执，后续state.rekeys为有界实际完成观察。此契约补齐为用户单独授权例外。
