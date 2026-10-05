# 修订版实现与真实验证报告

I01通过双重请求/操作归属校验及退出有界清理修正。运行、设备、类型、SID和queued状态都必须匹配；控制同样检验当前session实例。失败、取消、确认终态不复活；旧queued不自动重发，不修改原SID。写入中断按未确认处理而非断言未执行。I02逐台尝试，API/页面保留每目标准入结果、部分失败与真实在线状态。

版本interactive-v1.1-corrected；最终源码清单de66f76fa0b245aa2fd112023214fa913fb4509eab4b88f4d848c6b435136c57，328项。新EXE在build_final/build_info.json，构建/测试/截图来自当前树。源码基线和diff保存；旧M5/M6的2954项SHA-256未变，不采旧1000次。不初始化Git。

最终Go完整验收器PASS；378顶层PASS、0 FAIL、2 SKIP，真实Go退出0。新持续测试616.16秒含512记录新握手和静置16秒主动5000ms控制，退出0；与最终源码只差可见版本文字，live/密码/测试一致。三套既有Python17/45/23项、Node7断言通过。实际新EXE one/twenty/三种坏设备全通过，分别记录run_id、binary/source哈希、26.3/28.7手动验收、暂停控制、定向控制、启停重启、API、导出和实际隔离M6/T/H/P固定smoke。周期变化来自本轮实际时间事件和TestActualSamplingIntervals，非页面timer显示。

新版截图run=interactive-35553815c66755f5000475f3fd41dca1；20正常、真实手动控制确认、19部分成功、离线拒绝；截图来自本机Codex浏览器，没有冒称原生Edge操作或系统DPI。4页修订手册PDF/MD已生成并逐页目视检查。源与截图哈希在发布清单。

网关完整生命周期未完成：CLI已有创建及通用登记吊销/退役能力，缺网关轮换和旧会话处理真实闭环；保留角色保护与最小补齐计划。日志只有最近80条及有界导出，无完整筛选分页。race、独立SM2交换pending；OpenSSL历史SKIP；物理断网、系统DPI、跨机硬件未验证。历史M5计时0ns和批次资源口径不变；旧1000/1000不是未来100%，P差异不全部归因ML-KEM。所有失败、超时、取消与归档/工具错误记录保留。

完成当前固定范围后停止，无新平台功能。

修改文件：internal/live/{device,commands,dispatch,manager,state,request_scope,session_queue_test}.go；internal/console/{handlers_live,build,batch_test}.go及assets/{app,batch,index}；scripts/interactive_build、installed_acceptance、bad_device_acceptance、frontend_test；docs/API/OpenAPI/修订计划。下一阶段：本轮停止，缺口未自动推进。
