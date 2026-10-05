> **2026-09-30 本轮实施口径：** 以 M5B_CORRECTED_CONTRACT.md 为准。以下为早期设计建议，特别是“退出 CPU”和确认窗口名称不能直接当成当前已实现口径。

# M5B 测量契约（建议实施方案，尚未实现）

本文件依据仓库docs/REQUIREMENTS.md、BENCHMARK_SPEC.md、BENCHMARK_PLAN.md安排下一步；以下实现细节是审查方建议，不是题目额外硬要求。范围仅为测量工具与小规模试验。

## 1. 三条通道、三类实验分开

- T：现有Suite1，SM2双向认证/协商、HKDF-SM3、SM4-GCM。
- H：现有Suite2，加入真实ML-KEM-768，沿用现有握手与业务处理。
- P：独立benchmark适配的Go TLS1.3 + X25519MLKEM768，双向证书认证。它是公开混合对照，不是国密Suite3，也不能替代T/H。

分别建立 `handshake_smoke`、`resource_smoke`、`transaction_throughput_smoke` 结果类型。一次资源实验的CPU、握手时延、业务探测耗时不得混成一个数字。实验编号使用P，不给record.Meta强加第三种套件。

## 2. CPU（先支持实际Windows开发机）

首版可采用一个控制进程加设备/网关两个独立工作进程。每个资源试验使用新工作进程，长期密钥/证书事先配置；私钥仅在本地私有临时目录。控制进程和工作进程只传递公开事件、地址、run_id和结果。

可以实现两种明确口径，不能混名：

A. `process_cpu_user_ns` / `process_cpu_kernel_ns` / `process_cpu_total_ns`：工作进程从启动到退出的CPU累计值；包括运行时、身份载入、握手、probe、日志与退出。可通过ProcessState.UserTime/SystemTime获取。**不是 handshake_cpu_ns，也不是仅一次密码运算的CPU。** 子进程不再生成未计入的孙进程。

B. 可选 `handshake_cpu_user_ns` / `handshake_cpu_kernel_ns`：进程内部在明确定义的握手前后分别调用OS进程计数器再做差。这个窗口仍含该进程同期其他线程、GC等工作，不能叫纯算法CPU。只有真的实现两次快照才能填写。Windows GetProcessTimes FILETIME需正确从100ns单位转纳秒；数值单位不等于实际测量分辨率。

双方必须分别记录PID/角色/测量起止阶段/方法/单位/错误。不能把同进程网关与设备混合CPU均分。空工作进程基线单独报告，不用它作自动减法。零值可能来自分辨率，保留并解释；不支持时null+原因。外层wall-clock不可填入CPU字段。

## 3. 峰值内存

Windows以PROCESS_MEMORY_COUNTERS.PeakWorkingSetSize为工作集峰值（bytes），与WorkingSetSize当前值、PagefileUsage/PrivateUsage、Go HeapAlloc分别命名。

若工作进程自行在退出前采集，明确是“进程启动至该次采样时的峰值”，不要声称覆盖后续整个退出过程。若控制进程采集，保证句柄有效且在目标平台上实测；不要假设Wait之后仍能取到信息。采集失败保留API错误，不能填0。

Linux仅在实际可用环境实现并运行时声明支持。周期采样只能称sampled peak，记录周期；未运行的平台标NOT_TESTED。

不要用“CPU握手窗口起止内存峰值之差”充当握手独占峰值。峰值是高水位，不能这样相减。

## 4. 首版吞吐：真实业务事务吞吐

先复用现有Telemetry→Control→ACK业务，而非新增无关大包协议。建议每模式每轮固定200笔，3轮，明细全部保留。其定位是本业务负载的应用goodput/TPS，不是SM4极限或网络线速。

- 每笔都有独立sample_id，operation_id在会话内唯一，仍调用真实HandleControl和网关确认。
- 记录实际接收并接受的序列化遥测明文字节；控制、ACK字节另列。不得用发送尝试字节代替已接受字节。
- 客户端事务耗时在同一进程单调时钟上测量，从第一笔发出前到最后一笔远端确认已被本地观测。该口径包含观察确认的IPC/事件成本，必须披露。
- 可增加网关同一时钟上的接收窗口，起点在等待本轮第一笔前，终点为最后一笔通过业务检查后；分母和统计范围必须明确，不能混合两台机器绝对时钟。
- 输出completed_transactions、accepted_telemetry_plaintext_bytes、data_window_ns、end_to_end_confirmed_window_ns、application_goodput_bytes_per_sec、transactions_per_sec；线路密文字节和控制流量分列。
- 不人为篡改生产采样策略。若应用限速影响结果，报告为含该业务策略的吞吐，不当作密码算法上限。
- 失败与中断不能伪装成完成200笔；按实际有效计数报告，不替换失败运行。
- 网络有限超时、上限和原有nonce/记录数限制保留。超过安全记录数则新完整握手并披露，不重置旧密钥序号。

若以后需要固定256/4096字节的流式负载，作为另一个独立profile设计；本轮不把它作为完成M5B的强制范围。

## 5. P公开混合对照

独立包/入口，可复用控制处理和结果基础设施；不得改T/H密码适配器，AES/SHA等标准TLS依赖仅在P路径出现。

- TLS版本限定1.3。
- 双方CurvePreferences仅允许X25519MLKEM768；Handshake完成后读取实际ConnectionState，而不是依据配置填结果。
- 核验实际Version、CurveID、CipherSuite、HandshakeComplete、DidResume，检查双方证书验证链与预期设备/网关身份。
- ClientAuth=RequireAndVerifyClientCert；配置明确ClientCAs/RootCAs、SAN/ServerName、有效期与用途。受信CA签发不等于就是预期身份，客户端证书仍绑定测试设备ID。
- 禁用票据/会话恢复，ClientSessionCache保持nil，DidResume=true视作不符合完整握手样本。无0-RTT。
- InsecureSkipVerify不得开启；禁止KeyLogWriter、秘密导出、unsafe读取未导出字段。
- 当前官方API已有ConnectionState.CurveID，先核验实际本机Go版本支持再调用；接口不可用时如实停在未验证，不从配置推断或偷偷换组。
- P连接用独立公开connection_id关联；不要把它冒充国密会话密钥派生的SID。
- P对照公开采用X25519+ML-KEM，其证书、签名、KDF、记录加密与T/H存在差异。只比较实现整体，不能从整体差值证明单一算法优劣，不能声称等价后量子认证。
- 私有测试CA/证书生成在握手计时外；各实现握手临时材料生成在计时内。既有TCP与在线握手耗时分列。

## 6. 结果契约与可复现性

对新资源/吞吐/P结果使用显式schema版本/类型，复用严格JSON、唯一ID、缺字段处理、状态与真实退出码规则。不要让现有T/H验收器无条件接受任意mode/suite。

唯一run_id、parent_run_id、trial_id；记录源码manifest、配置SHA-256、精确工具链/依赖、OS/CPU/逻辑核/GOMAXPROCS、电源/后台负载与方法说明。配置哈希不包含秘密。

结果字段增量时同步生产验收器和自测，不能新增字段就静默漏案例。失败证据可VALID但run为FAIL；完整运行有部分失败不应被删除，统计分母不缩小。

summary从raw重算，CPU/RSS各自单位和口径不可省略。资源值不从旧M5A HeapAlloc填充。当前所有样本run_kind=smoke，formal_results_eligible=false。

## 7. 本轮退出标准

T/H原功能回归；P实际协商并有错误身份拒绝对照；T/H/P小规模新握手/资源结果；每模式3轮真实事务吞吐；新增资源/结果单元测试、损坏证据与失败路径对照。正式1000次不启动。

没有测到某个项目必须标明；Windows实际资源指标未取得时不能宣称M5B资源部分全部完成。race/独立SM2交换验证另列，不伪造解决，也不把它们无限扩大成本轮新功能。
