# M5B 本轮校正契约（2026-09-30）

本轮仅修复 G1/G2/G3，原国密 H1–H4、KDF、认证、记录保护不变。旧审查包、旧报告和旧 smoke 保留为历史，不作为当前源码通过依据。正式 1000 次仍锁定。

## 资源

资源 experiment 测量一批真实握手（不把它称为业务 probe）。设备与网关是独立 Windows 子进程。CPU 是从进程启动到终态采样的累计值，不含采样后退出成本；设备另保存批量握手窗口的 before/after 原始计数与每次 API 错误。FILETIME 100 ns 到 ns 检查溢出；差值检查回退。真实零值保留。API 错误输出 null 和原因。峰值工作集、当前工作集、私有提交及 Go heap 分列。

网关以已经完成的会话数为正常退出条件；控制器等待真实 Wait。超时或取消后的强制清理单独记录 PID、退出码、forced_cleanup 和 Wait，不称优雅退出。正常流程中事件生产者结束后才 Sync/Close，失败传播到命令返回码。

## 吞吐与确认

每轮独立会话，默认 3×200。完整关联键是 run_id、认证设备身份、完整 SID（P 使用独立连接 ID）、sample_id、operation_id。逐条记录实际序列化遥测长度；网关 accepted 事件也记录长度。accepted、control-sent、control-confirmed 分别计数，不能互代。

`accept_confirmed_window_ns`：第一笔发送前到末次控制消息被 worker 接收，仅代表遥测接受被观察。

`ack_send_window_ns`：同一起点到末次本地 ACK 写出。

`end_to_end_confirmed_window_ns`：同一起点到 worker 观察到网关已经验证该 ACK 的精确 `control-confirmed` 事件。T/H/P 均通过本机公共事件文件观察，同一 worker 的单调时钟闭合；包含日志可见性及约 1 ms 轮询观察成本。它是本地软件仿真的测量旁路，不是新增网络协议确认，不支持推断跨机业务吞吐或纯算法速度。

每笔另外保存 observed/sent 布尔事实及相对纳秒数，时钟分辨率可能产生真实 0，不能用 0 反推“没发生”。若整个确认窗口未得到正值，速率输出 null 和原因。goodput 分子来自网关真实接受字节，TPS 来自本地观察到的真实远端确认完成数。确认事件存在但预算内未被本地观察时，保留远端确认事实，事务仍未在预算内完成。失败不缩小请求分母。

双向 wire 记录握手后 baseline 和数据结束累计计数，只输出其差；P 的 wire 是 TLS 连接上的网络字节，T/H 是国密记录网络字节，不包含 IP/TCP 头。关闭通知不进入数据计数。

## P 业务与边界

TLS 1.3、X25519MLKEM768、mTLS、SAN/ServerName 和预期设备 CN 均保留。每次吞吐会话记录双方实际 Version、CurveID、CipherSuite、DidResume、验证链与身份。P 不是国密 Suite 3。

遥测复用生产 `ValidateTelemetry`，device_id 绑定证书身份，检查温度范围。客户端调用真实 `HandleControl`；服务端检查 ACK 操作、applied、实际间隔。只有完整事务后的下一条消息边界 EOF 可正常结束；缺 ACK、半条 JSON、坏 JSON、超长消息、超时、取消均失败。拨号、握手、业务和确认共享预算，握手后继续执行 deadline/取消，所有服务端都 join。

## 完整验收与返回码

新契约使用 `m5b-resource/2`、`m5b-throughput/2`、`m5b-public-hybrid/2`，明确区分历史 `/1` 输出。公共 evidence 层拒绝所有层级重复 JSON 键、缺非可选字段、错类型、未知字段、尾随 JSON；长度有界。manifest 固定配置 SHA-256、源码 SHA-256、请求数、模式和 smoke。角色、PID、trial/round、关联键、原始计数、窗口、字节、速率、每项 summary 与真实退出码逐项重算。

生产命令在文件全部落盘后调用本类完整验收器；`validation.json` 保存 problems、evidence_status、run_status、producer_exit_code、acceptance_passed。0 为运行和证据通过；2 为配置/执行问题；3 为真实试验失败；4 为取消；5 为完整验收或持久化失败。完整真实失败可为 VALID，但不会 PASS。验证器拒绝时不能发布无问题的 validation；失败状态和命令码记录真实情况。

原握手 probe 回归也保留取消后的真实远端确认：仅当该失败行已完成并记录认证、控制应用和 ACK 写出阶段，且确认等待超时/取消，才允许同 run/身份/SID/operation 的唯一确认归属该行。客户端 success/probe_success/gateway_confirmed 仍为 false；错误身份、重复或未绑定确认仍拒绝，不能用迟到确认把失败改写为成功。

## 平台与复现

本轮 Windows 原生 PowerShell/Python，Go 1.27.1 windows/amd64；进程级 GOTOOLCHAIN=local 与既有 Go PATH。没有安装工具或修改全局环境。`scripts/windows_integration.py` 用新临时测试身份调用生产程序和原严格 Python 事件验证器，替代依赖 Linux 命令的 `.sh` 运行入口；不覆盖旧结果。

实际结果和状态见 `results/raw/m5b_corrected_20260930/`、`results/m5b_corrected_readiness.json` 和 `results/milestone_M5B_CORRECTED.md`。race 无新执行证据、SM2 协商独立对照 pending、Linux 资源 NOT_TESTED、P 同进程 CPU/RSS null+原因。原型没有安全审计、证明或合规认证。
