# 标准 SM2 确认增补验收记录

版本 interactive-v1.3-sm2confirm1；452项源码清单 2c6cc0531b1c6da32b6099d6b3e5cc116252f2f91153d5fa2c7144974f1a3cd5；交互EXE b69f5961f20413133416258899ef34a84d3fceffe49352910899e0966341fd0d。

本轮实际补齐的是生产标准 S1/S2，而非把原项目 Finished 改名。底层 MQV/ZA/KDF/SM3 仍用固定可靠组件，签名与原双向 Finished 继续验证。H2/H3 各增加36字节，消息体版本2；旧版本拒绝，无回退。前端全部22项内嵌资源哈希保持，未进行界面重构。

|项目|实际结果|证据与范围|
|---|---|---|
|修前同例|FAIL，真实退出1|evidence/evidence/before_confirmation；原适配器缺少S1/S2；未捕获完整修前测试增量源清单，不把它冒称最终冻结源码运行|
|独立标准确认|PASS，12组|evidence/crypto/standard_confirmation_cases.json；密钥/S1/S2逐字节，BC实际校验S1及显式恒定时间S2；错误位/角色/长度/身份拒绝|
|14组核心与OpenSSL|PASS|evidence/crypto/crypto_security；范围分别为核心交换、动态双向签名/HKDF，不能冒称OpenSSL SM2MQV互操作|
|真实TCP标准确认|PASS|evidence/regression/commands/go_test；T/H共42个叶场景，包含正常保护记录及特定拒绝；业务处理未观察的字段保持null|
|全量Go及Windows race|PASS，均退出0|各448顶层/555子项PASS；1项可选10分钟测试SKIP；29包，其中5包无测试，不称全部路径证明|
|构建/vet、Python/Node自测|PASS|evidence/regression、evidence/build；原前端43断言，未改前端|
|实际Windows运行|PASS|evidence/actual：1/20设备、26.3/28.7、空闲控制、1→5秒、生命周期、容量/API/导出、首中末失效19设备、启动停止重启；本机模拟端点|
|新版稳定性短测|PASS|观察180.047秒，20设备；资源为进程批次观察，不是单次握手；1800秒状态NOT_EXECUTED|
|当前M6对照复查|PASS|A/B共同16/16规则、同3/3隔离缺陷；B审计字段更完整，没有检出率提高或工时节省结论；隔离变体退出1为预期检出原始记录|
|历史正式数据|原字节已打包；最终复验见包外回执|history/core_finalize2包含原16批及历史32批、M6、来源源码/清单和失败长测；原采样来源不改|
|新版正式1000次/1800秒|NOT_EXECUTED|不默认重测，不以旧性能和旧1800秒冒充新版|
|前端新方案/真人试用/物理断网|NOT_EXECUTED|等待用户下一轮界面方案；当前截图仅历史版本，不冒充新版实机截图|

保留的本轮失败：初次全量回归旧离曲线输入被更早拒绝，原签名阶段断言失败；改用有效替换点保留该签名断言，另补离曲线TCP精确拒绝；后续冻结源码全量普通/race同例通过。额外m6_tools_selftest调用遗漏必需--out，真实退出2保留；通过先正常基线再5份坏证据的规范入口已重新执行并退出0。早期编码/编辑脚本错误属于准备阶段，不计为协议通过。

S1/S2增补的影响：旧文档/版本逐字节保存在docs/history；原M5/M6和Core_Frozen包冻结。转录新字段会自然改变会话密钥/SID，记录编码、密钥来源、CAS和配额规则未取消。当前小规模T/H/P仅3次smoke，不能用于正式性能结论。

测试JAR是固定hash的本机JetBrains发行物，不声称官方原包签名核验。有限独立对照不是标准协议认证、形式化证明或侧信道/DoS/供应链审计。SM2身份认证仍为经典认证；库及Go内存清除限制保留。程序status不执行安全检测，历史14组核心记录保留其当时S1/S2未执行范围，当前确认另列新证据。

最终ZIP哈希回执在包外；若独立复验未通过，不能仅凭文件名或上表先行声明最终发布通过。

基线缺失确认的独立重现：evidence/evidence/baseline_confirmation_reproduction，使用完整440项旧基线加当前同名回归测试，在独立目录实际退出1，完整增量源清单及不变回执保留；它是明确标记的反例重现，不是旧实验采样。
