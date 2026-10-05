# W3 后端可靠性记录

本轮从当前 V12 工作区继续，冻结页面资源，不改密码握手、记录层及已修复的旧会话/批量连接实现。这里是 W3 开发阶段记录；最终全部工程源码、正式测量、race、Windows 包与解压结果由本轮总验收关联。W3 文件清单仅覆盖本模块，不冒充最终完整源码清单。

## 实际核对及最小修复

|项目|修前证据|本轮行为|同例与正常对照|
|---|---|---|---|
|停止后接纳新任务|`w3/before_counterexamples`：关闭 HTTP 后仍接受固定 bench 任务，真实 exit 1|任务准入与 `wg.Add` 使用同一锁；关闭时封闭准入，取消已拥有任务，排空 HTTP 和工作者；`Close` 幂等；不再产生停止后的 HTTP 审计事件|`TestBackendClosedServerRejectsJobAdmission`、`TestBackendServerCloseIsIdempotentAndStopsAuditProducers` PASS|
|来源哈希只检查长度|同目录：64 个 `g` 被导出契约接纳，真实 exit 1|使用完整 SHA-256 hex 解析，仍允许明确的开发零来源，不将其写成发布来源|`TestBackendExportSourceMustBeSHA256` PASS；正常 hex 对照 PASS|
|通信证据缺关联字段|`w3/before_correlation_counterexample`：无 SID/operation 的 confirmed 记录被接受，真实 exit 1|完整 16 字节 SID 校验；通信观察必须有设备/SID；遥测要求 sample，控制/确认要求 operation。筛选导出仍只验证已导出的范围，不推断未导出的对端事件|`TestBackendCommunicationEventRequiresActualCorrelation` 及实际 API 导出对照 PASS|
|子程序输出及退出|原实现 `cmd.Run` 未限定退出后继承管道的等待|固定白名单保持；增加 2 秒 `WaitDelay`；真退出码与输出不完整错误分别保留。不会因进程 exit 0 把 `ErrWaitDelay` 当成功|实际测试子进程 exit 7、取消、持有继承管道；正确保留退出码和失败 PASS|
|任务回执持久化|原回执使用 `os.WriteFile`|共享 `evidence.WriteJSON` 传递 Write/Sync/Close；回执写失败保留实际进程退出码，任务显示 failed；新增 `receipt_status` 仅指回执持久化，不等于实验成功|真实目录写入失败与正常失败任务回执 PASS|
|启动/停止公开 marker|原入口忽略 Mkdir/Marshal 和部分 Close 错误，直接覆盖 active.json|入口保持薄；模块化同目录临时文件、Write/Sync/Close、Rename 发布；启动失败传播清理错误，结束写 STOPPED/FAILED。未宣称掉电后的目录持久性|正常替换、坏编码不覆盖旧文件、目录目标失败且清临时文件 PASS|
|严格输入共用|现有 `evidence.Decode` 已拒绝重复键、尾随、缺字段和未知字段|新增 `exports.Parse` 共用该解析器；支持 time.Time 的标准类型解码，拒绝非法 UTF-8 和非法目标参数；确认和分母由事件重算|成组来源/ID/run/范围/字段/重复键/尾随变异均拒绝；正常导出 PASS|

失败记录没有删除。`after_counterexamples_and_contracts` 的 exit 1 是测试错误地假设 Windows 被取消进程退出码必为 Unix 的 -1。实测 Windows 返回 1；断言改为真实非零退出并要求 DeadlineExceeded，生产程序没有改写退出码。`after_counterexamples_and_contracts_corrected` 与后续正常回归均为 exit 0。

## 保留的边界与 API 兼容

固定任务白名单、8 个累计任务、实验/活跃会话隔离、40 秒任务 deadline、8 MiB 输出、16 个导出、4 MiB 单个导出、2000 个内存事件及 20 MiB 公共日志上限仍有效。没有用增大配额或删证据换取通过。

旧 API 字段、HTTP 状态及前端处理代码保持兼容。401 的 `code=authorization_expired` 不变，增加 `reason=missing_session/unknown_session/expired/csrf_mismatch`；既有 409 容量返回仍是原 code，增加 `reason=job_limit/export_limit`。已有 queue_full/operation_limit、CSRF、Host/Origin 和一次授权回归保留。关闭中的新请求是 503/server_stopping，不能误称已经处理。

GET `/api/v1/state` 保留原 Snapshot 全部字段，增加 `diagnostics`：观测时间、进程 goroutine、Go 堆分配/使用/保留、Go runtime sys、GC 数、累计/活跃任务、累计导出。`live_diagnostics` 保留 live 所拥有的连接、会话、工作者、队列及操作状态观察；`pending_ack_count=null` 并保留未独立观察原因。两者同时存在的 JSON 契约测试通过。Go 堆不是 OS working set、单次握手资源或无泄漏证明。

导出从一次 Snapshot 固定选择 run、事件 ID 区间及过滤项。2000 事件截断后，过期范围拒绝；在导出后新增事件不会进入旧导出。`earlier_events_omitted` 是该运行整体已移出的事件数量，不是能猜出的筛选命中数。默认载荷脱敏，CSV 公式和 HTML 注入仍被处理。公开原始日志与有界内存窗口分开，不把此导出称为全部历史。

## 命令与真实结果

完整命令、stdout/stderr、时间与退出码在 `results/raw/backend_20261002/w3/<名称>/command.json`、`stdout.txt`、`stderr.txt`、`exit.txt`。32 个顶层 PASS 是三个 W3 包本轮完整普通 Go 检查，非全工程总数；子测试与各轮重叠不累加成攻击次数。

|记录名称|实际执行|状态|
|---|---|---|
|before_counterexamples|两个修前反例|FAIL / exit 1，保留|
|before_correlation_counterexample|关联缺字段修前反例|FAIL / exit 1，保留|
|after_counterexamples_and_contracts|首轮取消测试|FAIL / exit 1，Windows 退出码断言问题，保留|
|final_w3_tests_with_bounds|`go test -count=1 -json ./internal/console ./internal/exports ./internal/evidence`|PASS / exit 0，32 顶层 PASS|
|integrated_state_diagnostics|真实授权 GET state，旧字段与 process/live 诊断共存、未知 pending 为 null|PASS / exit 0|
|fuzz_management_5s|5 秒、2 worker、8 KiB 输入限制|PASS / exit 0，46,119 次输入|
|fuzz_export_final_5s|5 秒、2 worker、64 KiB 输入限制|PASS / exit 0，87,474 次输入|
|micro_export_final_1s_3|2000 公开测试事件；两个微基准各 1 秒 × 3|PASS / exit 0|

两组 fuzz 是解析器输入，不是协议攻击；预算结束没有发现最小失败反例。已有 seed 和新增覆盖留在 Go 测试；编译缓存不交付。本轮正常 Go 检查不包含 race 结论。Windows race 的环境状态由 W2 单独报告。

## 优化依据及限制

最终 2000 事件 JSON 构建三次约 3.95/3.99/5.04 ms/op；完整严格 JSON 解析约 33.90/35.30/35.89 ms/op，原始分配结果也保留。它们只测公开合成事件序列化/解析，不能替代握手、资源、吞吐或 GUI 稳定性实验。没有完整同条件生产热点剖析或可比优化前后样本，故本轮没有宣称提升百分比，也没有重写扫描/锁/缓存来追数字。早期微基准使用补关联字段前的测试记录，单独保留，不作前后可比结论。

关闭路径有界等待超时会返回错误，并明确未确认工作者退出；不会以返回 HTTP 成功冒充通信或进程已经结束。任务白名单当前后端不产生任意外部命令；本轮继承管道测试只在 `_test.go` 启动并清理属于测试的子进程，不新增生产测试后门。

本模块的新源码和测试清单、记录再计数与限制在 `w3/W3_RECOMPUTED.json`；可执行 `python scripts/backend_w3_report.py` 重算。原 M5/M6 原始文件没有由本模块写入，不重跑旧 1000 次。W4 当前版本正式实验和 W5 Windows EXE/解压尚须总验收结果，不能从这里的 PASS 推断已经完成。
