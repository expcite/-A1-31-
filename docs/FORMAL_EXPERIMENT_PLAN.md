# M5 正式实验冻结方案

冻结日期：2026-10-01（Asia/Shanghai）。方案 ID：`m5-formal-20261001-v1`。
依据：本次 EdgeHybrid_Demo_Audit 的 REVIEW、AGENT_RUN_FORMAL_EXPERIMENTS、SOURCE_EVIDENCE，以及现有 M5B_CORRECTED_CONTRACT。后者的测量边界继续适用；历史文件中的 smoke-only 状态只描述当时阶段。

## 固定矩阵与顺序

|顺序|类型|模式|请求量|单次/单轮预算|
|---|---|---|---|---|
|1–3|顺序完整握手及业务探测|T、H、P|每模式一个批次，恰好 1000 次独立尝试|30 秒，拨号至远端确认共享|
|4–13|分角色资源|T 的 5 批，再 H 的 5 批|每批 100 次完整握手|30 秒/握手|
|14–16|业务事务吞吐|T、H、P|各 5 轮，每轮 200 笔；每轮新完整会话|30 秒/轮，拨号至最后确认共享|

暖机统一为 **0**；无自动重试，无恢复，无 0-RTT。串行执行所有批次，启动前检查本机 Demo、bench、pbench、gateway、device、fleet 及其他 Go 测试是否仍运行；有冲突就停在采样前，保留原因。采样期间不启动演示或其他实验。每次新 TCP、新临时材料；长期身份/证书初始化、构建和依赖验证在握手窗口外。
顺序握手的总体保护预算 45 分钟；外层脚本 50 分钟，超时保留运行并清理其进程树。资源控制器沿用 `(会话数+1)*单次预算+10秒` 网关寿命及有界 Wait/异常清理。吞吐沿用同一轮预算。

## 环境与冻结产物

使用当前开发机 Windows 原生、amd64、loopback TCP，同一源码树及 Go 工具链。正式源码在完整回归后冻结；源码清单 SHA-256、精确 Go/依赖、OS/CPU/核数/GOMAXPROCS、电源模式、背景进程/负载、日志策略、二进制 SHA-256/Go 构建元数据另存 `results/raw/m5_formal_20261001/freeze/`。源码清单不是 Git 提交（本目录无 Git）。运行脚本在每个批次前核对源码及二进制，记录 run_id、配置哈希、源码哈希和二进制哈希关联。
保留 Demo ZIP SHA-256 `96bc6b44d21b2a44e25f13f7f181845934d2173373687bf4f56cf25d187226ac`、源码清单 `f79964fff00733e40c4b58b1eb1eb65b60928c09afb8736a81fe467bd3ce8db8`、Demo exe `fa614b5daf1f6c2cab19c0a56c614e09356badf5d9020eb69e03588aaa52e699`。它们仅标识已交付演示版，不填入新实验来源。

## 计时、字节和资源边界

T/H 的 handshake_total_ns：调用 handshake.Client 之前（临时 SM2、H 的 KEM 生成之前）至客户端校验 H4/Finished 返回。TCP 另计；probe 和远端确认等待另计。P 从 TLS HandshakeContext 前至返回，临时 X25519/ML-KEM 在其内部。两端实际 TLS Version、CurveID、CipherSuite、DidResume、证书链/身份逐连接核验。
wire 是客户端真实 Conn Read/Write n 的阶段差，含用户态 framing/TLS，排除 IP/TCP；双方方向分列，只计一次。握手与业务分开。

**资源采用已实现的 M5B handshake-only 批次**，不另加入 probe；这是冻结时选定的口径，与顺序成功率的握手+业务尝试分开。每批使用新的 gateway/device 独立进程。CPU 是进程启动至终态采样的 user+kernel 累计；设备另存整个 100 次循环的 before/after 快照及差值，含同期 GC、其他线程与日志。峰值工作集为进程启动至 API 采样的高水位，不含采样后的退出。绝不除以 100 称单次 CPU/峰值，不将双方峰值相加称同时峰值。Go heap、当前工作集、私有提交分列。空进程基线未实现，null+原因；不做自动扣除。

吞吐分子：网关真实接受的序列化遥测明文字节；TPS 分子：worker 已观察到的远端 ACK 验证完成数。分母：worker 第一笔发送前至其观察到最后完成的 control-confirmed 事件，单进程单调时钟闭合，包含公共日志及约 1 ms 轮询。accept_confirmed、ack_send、end_to_end_confirmed 三窗口分别保存。每轮独立会话，真实 ValidateTelemetry/HandleControl/ACK 检查保留；无人工更改业务限速。goodput 是本机软件业务吞吐，不是算法极限/跨机吞吐/线速。

## 分母、统计与失败

握手成功率 = 真实完整握手成功数/1000；端到端业务成功率 = 真实 probe 与网关确认闭环成功数/1000。完整 1000 行含失败仍可 VALID，但 FAIL，不能称全部成功。全部成功仅为本批次布尔事实，不估计未来概率。
中断保留连续已执行前缀；计划分母仍 1000，分别列实际执行、失败、未启动，INCOMPLETE。资源分母每批 100，吞吐每轮计划 200；中断不缩小请求量。未执行不是观测到的认证失败。不得拼批次、挑选成功或补齐成功数。
均值/中位数/P95 的样本集为 handshake_success；业务失败不删除已成功握手。中位数偶数取中间均值，P95 为 nearest-rank `ceil(.95*n)-1`；整数 ns 均值使用整数除法，展示 ms 不改变原始值。吞吐主汇总沿用现有各有效轮速率算术平均，另列逐轮值和原始窗口/分子；失败/无正窗口速率 null 与原因。失败阶段分布从所有原始失败行重算。

## 结果契约与退出码

formal 使用独立 manifest schema、固定方案 ID、source SHA-256、恰好匹配的请求量和 30 秒预算；沿用已验证 M5B 资源/事务原始行 schema，不改变采集定义。旧 smoke schema、请求范围和 formal_results_eligible=false 保持。旧数据不得用改标记转正。
生产命令调用对应完整验收器，离线实际 CLI 再核验，报告从原始数据重算。0 PASS；2 配置/执行；3 真实试验失败；4 受控中断；5 证据验收/持久化失败。VALID 与 PASS 独立；写入/Sync/Close/Wait 失败保留实际错误和退出码，不手写成功 validation。不可捕获的终止由脚本记录 ABORTED/INCOMPLETE，残缺证据可 INVALID；绝不改写为成功。
结果目录唯一、拒绝覆盖。私钥/证书/共享秘密仅在独立临时运行目录，正常退出清理；不进结果、CSV、日志、ZIP。全体命令 stdout/stderr、退出码、失败批次均保存。

## 比较限制与未支持指标

T：Suite1 完整 SM2 认证/协商、HKDF-SM3、SM4-GCM。H：Suite2 加真实 ML-KEM-768。P：Go TLS1.3 X25519MLKEM768、ECDSA P-256 mTLS、TLS KDF 与 AES-GCM；P 非国密 Suite3。P 的握手/吞吐两端同进程，T/H 顺序握手也同进程，T/H 资源/吞吐两端分进程；日志和协议结构不同，整体差异不能全部归因于 ML-KEM，也不能称等价后量子认证。
单次握手 CPU/RSS、P 分角色 CPU/RSS、IP/TCP 开销、网关单调接收窗口、空进程资源基线未支持：null 与原因。Windows API 真零保留，ns 单位不等于计数器分辨率。Linux 资源 NOT_TESTED；无新 race、独立 SM2 交换/安全审计/合规证明证据，保留历史缺口。

完成 M5 交付后停止；M6 创新与答辩材料留下一轮。
