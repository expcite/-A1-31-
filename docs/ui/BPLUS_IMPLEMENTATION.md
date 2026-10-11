# B+ 实施与接口审计

本轮来源为用户提供的 BPLUS-UI-SPEC-v1.0，按 P0—P5 实施。交接包 SHA-256 为 b17c009c4bb931649f3ea7b5c7619d2c0c4752cfa95487cc3110cd82d8648ad9。包内既有评测来自 Linux UI 模拟，不视为本轮 Windows 或密码验证。

真实工程为 `C:/Users/20477/Documents/deepseek-harness/default-workspace/edgehybrid_work/Work/edgehybrid-agent-starter`。无 Git，未初始化。起始完整源码452项，版本 interactive-v1.3-sm2confirm1，清单哈希 2c6cc0531b1c6da32b6099d6b3e5cc116252f2f91153d5fa2c7144974f1a3cd5。既有修改以这棵源码为基线，不从 ZIP 覆盖。修前源码副本和清单保存在独立工作目录，生成差异时使用这个基线。

## 分阶段实现

|阶段|工作与可复核产物|
|---|---|
|P0|核对完整源码、go:embed assets、原构建脚本、原测试和实际 API；原 EXE 在独立身份目录运行。原设备详情经过1.6秒轮询从打开变关闭，焦点从 SUMMARY 变 BODY；保留修前记录和截图。E3原代码无读超时，E8原在线状态下实验按钮仍可点击。其余E4—E9按来源和实际代码区分，不冒称全部已有真实复现。|
|P1|store/api/router 单一状态来源；3000ms只读超时，3500ms快照过期，1000ms独立检测；不积压state请求，独立读取build/session/jobs。设备表按ID更新，表单草稿仅在内存且按run/设备/动作隔离。取消中文按钮文案全局锁。|
|P2|五项导航及帮助、退出；双向提交快照和回执独立；持久详情抽屉；密钥维护菜单、网关影响提示、实验前置条件、固定日志和历史来源分区。用户系统字体、原生ES模块，无npm/CDN依赖。|
|P3|同一工作台上G1—G4；只保存公开引导引用和阅读位置。步骤切换不POST，跳过/未确认/证据不足不标完成；G3明确保守降级。|
|P4|Node隔离状态/故障测试、原F07/前端自测、真实浏览器/API/导出和Go回归；实际结果、首轮失败、未执行项在reports/ui-bplus记录。|
|P5|原backend_build.py重建8个EXE，生成来源清单与build_info；独立解压1/20台Windows运行验收。正式产物及最终哈希在包外索引，不在文档预写完成或PASS。|

## 模块与功能保留映射

|原功能|B+入口|事实来源|
|---|---|---|
|总览/通信|通信工作台：当前目标、模式、双向操作、最近事件|GET state；真实Channel由既有live处理|
|设备池/连接/断开/会话换钥|设备与连接；工作台次级设置|devices/{id}/connect、disconnect、rekey；run/start、stop|
|手动温度/自动采样|工作台左卡/自动采样区|telemetry(request_id,temperature_c)；sampling(auto,temperature_c)|
|主动周期控制|工作台右卡|gateway/commands(request_id,device_id,interval_ms)，0.1—60秒精确转换|
|操作取消/关联详情|操作详情抽屉，仅queued可尝试取消|operations/{id}/cancel；GET state的同run操作|
|长期身份/迁移策略|密钥与策略，设备维护菜单|identities/{id}/rotate、revoke、retire、policy，原confirm=action:id|
|网关维护|密钥与策略的独立网关区|原confirm=action:GW001:ALL；commit、session、admission、load、cleanup分别显示|
|固定实验及取消|安全实验室|jobs仅原m6_rules、bench_T、bench_H、pbench；退出码与receipt_status分开|
|日志/导出|记录与证据→当前运行|有界2000事件、固定selection、原三格式/载荷确认/同源下载|
|历史性能及来源|记录与证据→历史测量|history原报告及资产；原字节、0ns、失败、原run与采样源不变|
|帮助/首次说明/退出|帮助与版本；侧栏安全停止及退出|session/build独立读取；run/exit原confirm=exit:run|

## 观测缺口与安全边界

rekey返回调度受理，没有本次调用的operation_id；rekey事件无法区分手动、其他标签页和阈值触发。故G3的新SID与验证消息可以真实显示，但不能证明本次手动换钥因果。显示“新会话已观测，但本次换钥关联证据不足”，不通过时间等待补成功，不新增后端接口。

未提供逐条S1/S2验证阶段、单次处理时长和跨端统一时钟；详情显示未观测，不凭完整握手在线状态补阶段通过。队列、操作、任务、导出额度保持原后端限制；失败不清账本、不自动重试、不自动恢复吊销、不自动降级。

本轮不修改任何Go业务/密码源码、H1—H4、KDF、记录编码、生命周期CAS、go.mod/go.sum及配置模板。构建脚本仅更新前端来源描述，仍用原构建命令与版本/源码链接标志。最终核心字节比较见CORE_PRESERVATION.json。

所有密码回归范围沿用真实结果；历史成功不是本版的新性能测量。不会把本轮UI验证、3次smoke或原180秒短测扩写成1000次正式实验、1800秒长测或全面安全认证。
# 本次 UI/UX 复核补充

使用用户指定的 ui-ux-pro-max，针对原生 ES 模块界面查询字段错误反馈与触摸目标建议。保留 B+ 深蓝/青色主题、五项主导航和共享真实状态。温度/间隔错误显示在对应字段旁，关联 aria-describedby，并保留上次真实通信回执；输入修正清除旧字段错误。日志范围更新及文件生成各有准确提示，文件生成不宣称已经下载。小屏/粗指针按钮点击区域至少44 CSS像素；此项是易用性目标，不宣称全面无障碍认证。无密码协议或后端业务改动。
