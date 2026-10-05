# M5A 测量计划（正式 1000 次采样设计；本轮不执行）

M5A 交付的是**测量器**与**smoke 验证**。本文件把正式 1000 次实验的设计冻结下来，
使 M5B 可以直接执行而无需重新定义口径。M5A 的 smoke 结果带
`run_kind=smoke`、`formal_results_eligible=false`，**不得**当作正式数据。

## 1. 实验矩阵

| 编号 | 内容 | 状态 |
|---|---|---|
| T | 本工程 Suite 1（传统国密） | M5A smoke 已验证测量器 |
| H | 同一工程结构的 Suite 2（SM + ML-KEM-768） | M5A smoke 已验证测量器 |
| P | 公开混合对照：Go TLS 1.3 + X25519MLKEM768 | 设计/探针阶段，未实现 |

T/H/P 必须使用**同一源码版本、同一认证与业务强度、可比日志条件与运行环境**。
长期身份生成、编译、依赖下载、`init-demo` 初始化**不混入**在线握手时间；
每次握手的 SM2 临时材料与 KEM 密钥生成/封装/解封装**不得**移出计时窗口。

## 2. 正式 1000 次成功率实验

1. 每模式固定 **1000 次独立尝试**：分母恒为 1000。
2. 每次尝试 = 全新 TCP 连接 + 全新临时材料 + 完整握手 + 业务探测闭环。
3. **不重试补齐**：失败、超时、取消的记录原样保留；若重试，必须是新的 `attempt_id`，
   且失败行不删除（报表同时给出"1000 次中成功比例"与"全部 1000 次都成功的比例"）。
4. warm-up（如前 20 次）写入单独文件，不计入 1000 次分母。
5. 输出：成功样本的 median / P95 / mean + 全体失败率 + 按阶段失败分布
   （`tcp_connect` / `handshake` / `probe_send` / `probe_receive` / `probe_ack`）。
6. 中断：运行被中断标记 `INCOMPLETE`，summary 必须写实际完成数，
   **不得**把分母重算为已完成数。
7. 与 20 设备并发实验**分开**：1000 次串行成功率不与并发负载混在一个统计里。

## 3. 资源测量的正式方案（M5A 为 null 的指标）

M5A smoke 中 CPU 与峰值 RSS 为 `null`（原因见 BENCHMARK_SPEC.md）。正式采样：

- **每次试验一个隔离子进程**：子进程只做一次握手 + 探测后退出；
  父进程用 `os.Process.Wait()` 得到的 `ProcessState` 取 `UserTime()` / `SystemTime()`
  作为该次试验的进程级 CPU（user+system），并在结果中标注口径为
  `process_cpu_delta_subprocess`。
- 子进程的**启动开销单独测量并披露**：空子进程（不做握手）的 CPU 分布作为基线另列，
  不从握手 CPU 中扣除，也不混入。
- **峰值 RSS / 工作集**：子进程结束后经 OS 接口读取峰值工作集
  （Windows: `GetProcessMemoryInfo.PeakWorkingSetSize`；Linux: `ru_maxrss`），
  标注 `memory_measurement_method=process_peak_workingset`。
  M5A 未实现的原因是该路径需要平台特定代码，Go 标准库不提供。
- `go_heap_alloc_bytes` 继续作为**不同指标**并列报告，不与 RSS 混用。

## 4. P 对照的边界

- 使用 Go 官方 `crypto/tls` 的 `X25519MLKEM768`；以**实际协商的 NamedGroup/CipherSuite**
  为准记录，不以 `CurvePreferences` 配置推断。
- 双向证书认证；禁用会话恢复与 0-RTT；`DidResume()==true` 的连接**不算**完整握手样本。
- 不设置 `InsecureSkipVerify` / `KeyLogWriter` 来换取可运行性。
- P 与国密主协议的认证载体、算法、报文结构差异**单列**；
  不得把协议整体差异归因于某一个算法，也不得把 AES/SHA-256 路径引入 T/H 生产套件。
- 本轮 P 未实现则保持 `NOT_IMPLEMENTED`，不得用普通 X25519 或占位结果顶替。

## 5. 结果文件

```
results/bench/<run_id>/
  manifest.json   # 版本、口径、支持/不支持指标及原因
  trials.jsonl    # 每次尝试一行；失败行原样保留
  summary.json    # 从 trials.jsonl 重算；不得手填
  status.json     # RUNNING -> PASS / FAIL / INCOMPLETE
```

汇总必须可从 `trials.jsonl` 重算；任何汇总字段与原始行不一致都是证据缺陷。
`cmd/bench` 只开放 `smoke`；`formal` 请求显式拒绝（退出码 2）。
# 当前方案索引（2026-10-01）

正式实验采用 [FORMAL_EXPERIMENT_PLAN.md](FORMAL_EXPERIMENT_PLAN.md)。以下保留 M5A 时的设计和未实现状态；逐次子进程 CPU 建议没有作为当前测量口径实施。现有 M5B 资源是每角色独立进程的批量握手，不能读成单次握手指标。
