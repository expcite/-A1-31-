# 性能与成功率实验规范（待实现）

题目要求传统/混合两模式分别统计握手时延、报文长度、峰值内存、CPU时间、吞吐和连续1000次握手成功率，并与公开混合方案或同类实现比较。
本文件给出实现建议；骨架的明文lab不属于这些结果。

## 三类实现

T：本项目传统国密套件；H：同一程序结构的国密+ML-KEM混合套件。
P：真实公开混合实现，候选 Go TLS X25519MLKEM768；它不是符合题目国密主协议的替代品。
P必须启用可比的双向身份验证，记录实际协商组；没有可比条件时明确差异。
首版均不启用会话恢复、0-RTT，不以关闭检查换取速度。

## 测量口径

- 握手时延：客户端开始准备首轮密码材料到验证ServerFinished完成；另外拆分准备、网络握手和TCP建连。
  不把KEM生成放计时外却将另一方案生成计时内。明确设备端与网关端边界。
- 报文长度：记录双方应用层握手wire bytes与总和；需要IP/TCP层开销则另列，不能混用。
- CPU时间：OS进程user+system时间或可核查的等价机制，不是墙钟时延；采样profile不是精确进程CPU时间替代。
- 峰值内存：单独记录OS峰值RSS/工作集；周期采样须列采样周期，标为sampled peak。Go HeapAlloc是不同指标。
  同一进程20个goroutine无法直接当作20个独立设备RSS；资源实验优先每设备独立子进程。
- 吞吐：已认证接收并交付的有效明文字节/业务传输时间，另列线路吞吐；固定消息大小、时长和并发数。
- 成功率：每模式连续1000次独立握手，分母固定1000；保存失败与超时，不暗中补跑替换失败。
  温身样本另存，失败重试是新样本；区分“1000次中成功比例”和“全部1000次都成功”。

同时保留一网关/20设备的并发场景，不把1000次串行实验和并发负载混成一个统计。
统计时给出成功样本的中位数/P95/均值及失败率，避免删去失败造成性能偏差。
同机结果只能说明该软件实验环境，不能直接推断真实低功耗硬件指标。

## 原始CSV字段建议

```text
run_id,scenario,mode,implementation,trial_id,device_id,
started_at_utc,success,error_stage,error_code,
key_prepare_ns,tcp_connect_ns,handshake_wire_ns,handshake_total_ns,
client_wire_bytes,server_wire_bytes,
client_cpu_user_ns,client_cpu_system_ns,server_cpu_user_ns,server_cpu_system_ns,
client_peak_rss_bytes,server_peak_rss_bytes,memory_measurement_method,memory_sample_interval_ms,
app_plaintext_bytes,app_wire_bytes,app_duration_ns,
go_version,dependency_versions,os,arch,cpu_model,commit_id,config_hash
```

不能测量的字段使用空值/null与明确原因，不能用0假装已测。
环境元数据保存JSON；逐次握手保存CSV/JSONL；汇总必须可从原始数据重算。
哈希只用于配置/程序/结果来源核验，绝不把真实密钥放入结果文件。

## 创新候选的额外实验

比较“预先固定的人工用例集合”与“自动生成+可审计证据”的测试方法，而不是比较“安全检查打开/关闭”的传输速度。
固定注入缺陷集合和训练/评估划分，记录发现的已知缺陷、漏检、执行成本、重复结果一致性。
对未覆盖威胁明确声明，不把通过全部有限测试解释为不存在其他漏洞。

---

## M5A 增补（2026-09-28）：测量口径冻结与 smoke

M5A 实现了小规模 smoke 测量器（`internal/bench` + `cmd/bench`），并把以下口径冻结为
后续正式采样的唯一口径。与本文件前文冲突之处以本节为准。

### 实验编号
- **T**：本工程 Suite 1（传统国密）。**H**：同一工程结构的 Suite 2（SM + ML-KEM-768）。
- **P**：公开混合对照（候选 Go TLS 1.3 + X25519MLKEM768）。M5A 只做兼容性设计与探针设计，
  未实现正式 P 采样；P 不算国密主协议的替代品，认证载体与报文差异单列，不归因于单一算法。

### 握手时延（设备侧，单进程内时钟，不做跨进程绝对时钟相减）
| 字段 | 起点 | 终点 |
|---|---|---|
| `tcp_connect_ns` | `net.Dial` 前 | 连接建立 |
| `handshake_total_ns` | **临时密码材料生成之前**（`handshake.Client` 调用前一刻） | 客户端验证完 H4/FinishedB（`Client` 返回） |
| `probe_ns` | 遥测发出 | ACK 发出（完整 telemetry→control→ACK 往返） |

握手成功与业务准入成功是**两个观测**，分别输出；端到端 success 必须包含业务探测闭环。
全局 deadline 与单次超时均有界；失败保留所处阶段与耗时，**不计为 0 时延**。

### wire bytes
- 用真实 `net.Conn` 计数包装器记录 Read/Write 实际 `n`，处理部分写。
- 只在**客户端**连接上计数并拆分 C→S / S→C，保证每条线上字节**只计一次**
  （不把发送端 Write 与接收端 Read 相加双计）。网关侧计数另存用于交叉核对，不入总和。
- **含** 4 字节应用层帧前缀；**不含** IP/TCP 内核头（另一层级，用户态不可测，报告为 null）。
- Suite 2 的 KEM 公开数据带来的字节增长可直接从实测观察到（T ≈3.1/2.7 KB，H ≈14.9/13.6 KB），
  不以手写固定总数替代采集。

### CPU 与内存（本机支持情况如实声明）
| 指标 | M5A 状态 | 原因 |
|---|---|---|
| 单次握手 CPU（user+system） | **null** | 需要逐次隔离子进程；Go 标准库不提供按 goroutine 的 CPU 计数，进程级累计会把无关 goroutine 归到一次握手 |
| 单次峰值 RSS / 工作集 | **null** | Windows 上 Go 标准库不暴露；进程级峰值由网关与所有试验共享，不得均分 |
| `go_heap_alloc_bytes` / `go_sys_bytes` | **实测** | `runtime.ReadMemStats`；**是与 RSS 不同的指标**，字段名已区分 |
| 正式采样 CPU/RSS 方案 | 已设计 | 每次试验独立子进程 + `ProcessState` 增量窗口；见 BENCHMARK_PLAN.md |

### 吞吐
有效明文吞吐 = 接收端通过认证并被业务接收的字节 / 对应业务时间；发送尝试量与线路密文字节另列。
本轮 smoke 未做吞吐采样（消息大小/时长/并发尚未固定为正式矩阵）。

### 成功率与正式 1000 次（本轮只设计，不执行）
- 正式每模式为**固定 1000 次独立尝试**，分母固定 1000，不是"重试到有 1000 个成功"。
- 重试（若存在）是新 `attempt_id`；失败、超时、取消记录全部保留。
- warm-up 另存；成功样本分位数与全体失败率**分别**列出。
- 中断运行标记 `INCOMPLETE`，不得按已完成行数重算分母伪装成 1000 次完成。
- 20 设备并发场景与 1000 次串行成功率是两个实验，不混为一个统计。

### smoke 与正式结果的关系
smoke 运行带 `run_kind=smoke` 且 `formal_results_eligible=false`，**任何情况下不得**作为正式结论
或竞赛性能数据引用；正式 1000 次采样在 M5B 之前保持锁定。
# 当前正式测量索引（2026-10-01）

本轮固定参数、失败/中断、计时和分母以 [FORMAL_EXPERIMENT_PLAN.md](FORMAL_EXPERIMENT_PLAN.md) 为准，测量边界沿用 M5B_CORRECTED_CONTRACT。下文包含历史 M5A 状态和早期建议，不代表当前 P 未实现或 formal 仍锁定。
