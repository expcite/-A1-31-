# W2 并发、会话与生命周期核对

本文件记录 W2 专项开发与回归，依据当前 V12 工作区继续修订。原 M5/M6 原始数据和前端没有修改。最终统一冻结后的全量 Go、全量 race、20 台约 1800 秒负载与发布回执，以本轮 `BACKEND_READINESS.json` 的实际文件为准；本文件的专项记录不冒充这些后续运行。

## 实际缺陷及最小修复

1. `Manager.Close` 原来每个调用者都执行事件文件 `Sync/Close`。四个并发 Close 和再次 Close 在正常真实 TCP 会话结束后产生 `file already closed`，把正常停止变成失败。`shutdown_before` 保存了退出 1 的反例。现用 `sync.Once` 只执行一次停止流程并保留同一回执：取消运行、等待已接纳的管理事务、关闭已接受 TCP（包括尚未发送 H1 的连接）及 Channel、取消工作者、等待生产者结束，最后 `Sync/Close`。真实文件失败仍保留，并不改为成功。`shutdown_after` 同例退出 0。
2. 设备管理缺少网关管理已有的运行停止/实验占用准入检查。已停止 Manager 仍可轮换 D001 并修改持久世代，之后向关闭的事件文件写入。`stopped_management_before` 保存真实退出 1 及世代变化；同一管理入口添加停止/实验占用拒绝，`stopped_management_after` 退出 0。网关生命周期实现、CAS 和准入规则未重写。
3. `BeginExperiment` 原来允许已停止实例接纳新的实验。`stopped_experiment_before` 保存退出 1；现拒绝停止实例，并用既有管理锁使实验准入与维护事务串行。`stopped_experiment_after` 保存退出 0。

修改文件为 `internal/live/state.go`、`lifecycle.go`、`isolation.go`，及两份新的 `_test.go`。32 项队列、256 项累计操作、2000 条内存事件、20 MiB 持久事件上限不变。旧 SID 队列与批量首错不停的原修复仅回归。

## 真实 Channel/TCP 交错

新增隔离测试 `TestBackendEncryptedOldACKCannotCrossReconnectedTCP` 用本机 TCP relay 截留真实加密 ACK，不解密、不构造协议成功，也不添加生产开关：

- 首个控制实际处理并把周期改到 5000 ms，ACK 被截留后为 `unconfirmed`，`applied`、确认时间仍为 null。
- 断开后完整新握手产生不同 SID；在第二个控制的真实 ACK 被截留时，把旧会话原密文写入新 TCP 连接。
- 新 Channel 实际拒绝旧记录，两项待确认操作均不能假确认。
- 第三次完整新握手的 26.3、28.7 温度与 5000 ms 控制经真实 Channel 正常确认。

首次测试 relay 未传播 EOF，虽然网关已拒绝记录，测试端仍等待设备离线而超时。`late_ack_real_tcp` 的退出 1 保留；修正 `_test.go` relay 双端关闭后，`late_ack_real_tcp_after_fixture_fix` 退出 0。该失败是隔离测试夹具问题，不写成生产程序缺陷。真实事件记录在各运行的 `events/` 中；记录关联实际 run、身份、SID 和 operation_id。

既有真实 TCP 回归覆盖 20 台正常、D001/D010/D020 吊销后其余 19 台仍被尝试并通信；断开、换钥、采样等待超时的旧队列清理；网关新旧世代、吊销/退役跨重启及准备/提交/加载/清理失败安全。

## Windows race 工具与实际结果

按 [Go 官方 race 文档](https://go.dev/doc/articles/race_detector) 核对 Windows/amd64、CGO、C 编译器及 `--print-file-name libsynchronization.a`，随后实际运行极小 probe。旧 GCC 8.1.0 能找到同步库且编译成功，但运行子程序退出 `0xc0000139`。本轮 PE 导入/系统导出核对发现旧导入库要求 `kernel32.dll` 中不存在的 `WaitOnAddress`、`WakeByAddressSingle`、`WakeByAddressAll`。这不是直接沿用历史 M1 的 `__iob_func` 原因。

用户本轮明确批准隔离便携工具链。采用 [WinLibs 固定发布](https://github.com/brechtsanders/winlibs_mingw/releases/tag/15.2.0posix-13.0.0-ucrt-r5) 的 GCC 15.2.0、MinGW-w64 13.0.0 UCRT、x86_64 POSIX SEH release 5。265160987 字节 ZIP 的 SHA-256 为 `7c3fd442be612e6ad5f86c91cf33f336748500bc761e3112bfdba29434baa5f6`，与官方 `.sha256` 和发布资产 digest 一致。安全解压到独立 Work/Tools，工具不进入运行包，未替换旧 CC，未修改系统或全局 PATH。

两次新的路径条件 probe 因 GCC 把含中文的安装路径规范化后无法找到实际存在的 CRT 文件而失败，原始记录保留。仅创建本任务 ASCII junction，并在测试进程设置 `GCC_EXEC_PREFIX`、`COMPILER_PATH`、`LIBRARY_PATH` 后，实际极小 probe 运行 PASS/退出 0。可用环境逐项保存在 `w2/environment/portable_environment.json`，准确的命令和退出码在 `race_probe_portable_ascii_prefix/command.json`。

实际专项 race：

|运行|范围|结果|
|---|---|---|
|`scoped_race_initial`|live、management、identity 的现有及新增真实路径|68 个顶层 PASS、1 个显式持续测试 SKIP，退出 0；未报告数据竞争|
|`backend_new_cases_race_final`|最后修订后的 6 个新增后端专项|6 个 PASS，退出 0；未报告数据竞争|

专项源码清单和运行前后差异都保留。协作期间其他模块变化与本专项模块变化分别记录，不能据专项结果宣称整个最后源码均已通过。全量 `go test -race -count=1 -json ./...` 由总任务在统一冻结后执行。race 仅说明实际执行覆盖的路径未报告数据竞争，不证明所有并发逻辑正确，也不作为性能基准。

## 只读资源观测与 API 兼容

保留所有旧 Snapshot 字段，新增独立 `live_diagnostics`，避免覆盖 console 的既有进程级 `diagnostics`：

- `connections_active`：本 Manager 接受的 TCP，含进行中的握手。
- `sessions_active`：当前网关会话映射大小。
- `device_workers_active`：持有取消句柄的已准入设备工作者。
- `local_queue_items`、`control_queue_items`：现有有界 channel 的瞬时长度和。
- `operation_states`：真实现有操作按 status 分组，包含历史终态，不推断为 pending。
- `process_goroutines`：整个进程的 Go goroutine 数，不是本 Manager 独占数量。
- `pending_ack_count`：null；`pending_ack_reason` 明确说明没有独立可枚举的 pending ACK 映射。

上述 Manager 计数在既有锁内观测；读取后队列即可变化，不等同于写出或确认。真实停止测试验证连接、会话、设备工作者和队列结束。日志 Write、Sync、Close 的错误保留；短写即使未附带错误也按 `io.ErrShortWrite` 处理。未做物理磁盘/断电或文件系统故障注入，文件错误测试采用实际关闭句柄。

## 原始记录与未执行项

原始 stdout JSON、stderr、实际命令、平台、进程环境、真实退出码、源码清单与公共事件均在 `results/raw/backend_20261002/w2/`。`W2_READINESS.json` 从这些日志重新统计。失败、环境阻塞和通过分别记录，退出 1 的 probe/反例/夹具失败未删除。

未在本专项执行 Linux race（已有 WSL/gcc，缺 Linux Go）；不作 Linux 通过结论。30 分钟 20 设备、进程 CPU/内存/句柄时序及发布停止重启由总任务独立运行，不用上述数秒测试替代。前端、DPI、真人体验、物理网络中断及新业务功能均未进行。
