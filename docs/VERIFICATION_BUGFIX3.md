# 第三轮独立复查与修复（releasefix3）

当前版本 `interactive-v1.2-backend2-status1-releasefix3`。基线是发布包
`EdgeHybrid_Verification_Bugfix_Windows.zip` / `EdgeHybrid_Verification_Bugfix_Review_Package.zip`
里的 `interactive-v1.2-backend2-status1-releasefix2`（源清单 SHA-256
`63fc9215f2ba15ae80a0651e250370eb5e6c668412c496c80a3703ee8bad6fa7`）。

本轮按《EdgeHybrid 完整功能验证与发布验收流程》执行可执行部分，独立复核了
releasefix2 声称已修好的六项缺陷，并修掉复核中新发现、可复现的缺陷。原发布包、
历史 `results/raw/**` 和旧 ZIP 全部保持字节不变；本轮工作只在独立副本进行。

## 复核结论：releasefix2 的六项修复成立

| 原缺陷 | 本轮独立复核 | 证据 |
|---|---|---|
| LIFE-01 退役未清理目标长期密钥文件 | 已修复。`retire` 后 `D001/` 下 `D001-SIG-1.key`、`D001-AGR-1.key` 均消失 | 真实 `secctl.exe` 运行 |
| LIFE-02 已吊销设备无法继续退役 | 已修复。REVOKED → RETIRED 退出码 0，重复退役仍为 0，`rotate` 对已退役设备仍拒绝 | 真实 `secctl.exe` 运行 |
| CLI-01 忽略未知 status 参数及尾随位置参数 | 部分修复（见缺陷 1） | 真实 `secctl.exe` 运行 |
| EXPORT-01 满 2000 事件快照立即过期 | 已修复。`TestVerificationFreshSnapshotImmediateExport` 在 1999/2000 事件下均通过，快照按 run+范围+2 分钟保留至多 16 份 | `internal/console` 测试 |
| D-BUILD-DEMO-PS51 PS5.1 解析失败 | 已修复。`scripts/build_demo.ps1` 带 UTF-8 BOM，内置 Windows PowerShell 5.1 解析无错误 | 本机 PS 5.1 解析器 |
| D-DEMO-HELP-EXIT 帮助被当启动失败 | 已修复。`demo -h` 退出码 0，不打印启动失败 | 真实 `demo.exe` 运行 |

F07（记录页固定范围刷新）修复同样成立：`internal/console/assets/logs.js` 的选择保持、
无匹配显式提示、失败不换范围三条路径在 `scripts/logs_refresh_test.mjs` 与
`scripts/interactive_frontend_test.mjs` 下全部通过。

## 本轮修复的缺陷

| 编号 | 级别 | 问题 | 最小修订 | 回归入口 |
|---|---|---|---|---|
| CLI-02 | P2 | `secctl` 只在 `status` / `check-config` / `retire` 上拒绝尾随位置参数；`check-trust`、`lifecycle`、`revoke`、`rotate`、`init-demo` 会静默忽略，其中 `revoke` 会照常执行吊销 | 新增 `rejectPositional`，八个子命令统一拒绝 | `cmd/secctl/verification_arguments_test.go` |
| CLI-03 | P2 | `secctl lifecycle` 对 `Lookup` 拒绝的条目只输出 `lookup_error`，丢掉 lifecycle / generation / 两类 key_id / retired_key_ids；运维无法区分 REVOKED 与 RETIRED，也无法确认退役清理 | 拒绝条目改由 `identity.Metadata` 取公开元数据，补 `lookup_error` 字段说明拒绝原因；准入仍走 `Lookup` | `cmd/secctl/verification_lifecycle_report_test.go` |
| BENCH-01 | P3 | `bench` 创建输出目录失败时一律报 “output directory (it must not already exist)”，父目录缺失或权限不足时误导排查方向 | 区分 `os.IsExist` 与其它错误，后者报告真实原因并说明 `-out` 是结果根目录 | `internal/bench` 测试 |
| IDENT-01 | P3 | `ApplyRotation` 的 CAS 前置条件用 `PreparedRevision != 0 \|\| PreparedLifecycle != ""` 判断后同时覆盖两个字段，只给 revision 的调用者会把 lifecycle 断言清空，与代码注释“无条件比较”不符 | 两个字段独立覆盖 | `internal/identity` 现有交错测试 |
| API-01 | P3 | 写接口接受非对象 JSON 体：`null` 让 Go 的类型化解码空转，`q.keys` 为空，`validateRequest` 通过，`POST /run/start` 用 `null` 返回 200。契约只允许 JSON 对象 `{}` | `decode` 要求首个非空白字节为 `{`，否则 400 | `internal/console` 表驱动用例新增 `null_body` / `scalar_body` / `empty_body` |
| JOB-01 | P3 | 任务状态用 `ctx.Err()` 推断 `cancelled`，而控制台自己的 8MiB 输出上限取消的是同一个 context，于是“输出超限被杀”被记成运营者取消；`receipt_status` 也在回执落盘前就置为 PASS | 取消原因改用 `cancelJob` 显式置位的 `cancelled` 标志；`terminalView` 统一算终态；回执先写盘、成功后再发布终态与 `receipt_status` | `internal/console` 新增 `TestVerificationJobStateDistinguishesOutputLimitFromOperatorCancel` |
| AUDIT-01 | P2 | 页面打开时前端每秒轮询 `/api/v1/state`，每次都在同一条有界事件环里写一条 `api_result` 审计。用真实 EXE 实测：60 秒轮询产生 60 条 `接口观测`、0 条协议事件。约 33 分钟就把 2000 条环写满，记录页的筛选、分页与默认导出范围读到的正是这条环；同时每秒消耗约 330 字节公共日志 | 成功的只读 GET 不再写审计事件；所有写操作与所有被拒绝的请求（401/403/404/429/5xx）仍照常记录 | `internal/console` 新增 `TestVerificationReadOnlyPollingDoesNotConsumeTheEventRing` |

| AUDIT-02 | P2 | 被拒绝请求的审计条目没有上限。每条审计都写进本次运行唯一的有界 20MiB 公共日志，写满即停止运行；授权调用受 256 条操作账本约束，而本机客户端只发未认证/错 Host 请求时只受 120 请求/秒限速约束，按每条约 330 字节计约 9 分钟即可写满 | 每次运行的前 512 条被拒记录逐条保留，随后写入一条 `audit_budget_exhausted` 通知，其后的拒绝不再逐条记录，因而无法再触发停机；授权调用的记录不受影响 | `internal/console` 新增 `TestVerificationRejectedRequestAuditIsBounded` |
| API-02 | P3 | 缺少必需字段的请求落到业务层，返回 409 `operation_rejected`，客户端无法区分“输入不合法”与“动作被拒绝”，与文档所述 400 不符 | `validateRequest` 增加逐路由必需字段表（presence 判定，`temperature_c: 0` 与 `auto: false` 等合法零值仍然通过），缺字段返回 400 | `internal/console` 表驱动用例新增 11 条缺字段 400 与 1 条零值 409 对照 |
| LIFE-03 | P3 | `ROTATING` 是公开声明的状态，但 `Lookup` 拒绝它，`revoke` 与 `retire` 也都不接受它：一旦出现，只能手改 `trust.json` | 拒绝条目的读取路径扩展为“ROTATING 可由 revoke/retire 结束”；REVOKED→RETIRED 与 RETIRED 重试保持原样，轮换与准入仍拒绝 | `internal/identity` 新增 `TestVerificationRotatingStateIsTerminable` |
| TRANS-01 | P3 | 连接名额耗尽时接受后立即关闭并继续循环：对端看到的是与“服务已下线”无法区分的裸断开，本机洪泛还会维持一个热 accept/close 空转 | 改为在 `Accept` 中先等待名额再接受连接（内核 backlog 承接多余连接），`Close` 通过 done 通道释放等待者 | `internal/console` 新增 `TestVerificationListenerHoldsExcessInsteadOfClosingIt` |
| FMT-01 | P3 | 源码并非全部符合 `gofmt`：本次实测有 13 个文件会被 `gofmt` 重写（对齐、每条语句一行、import 分组顺序、文档注释规范） | 对全部 Go 源码执行一次 `gofmt` 归一；`go fmt -n ./...` 现在无输出 | 全量 `go test ./...` 通过；改动性质逐文件核对 |

| TEST-01 | P3 | internal/cryptosuite/sm2_openssl_dynamic_test.go 只要 PATH 上找得到 openssl 就继续，于是 OpenSSL 1.1.1（Ubuntu 20.04 默认，不支持 genpkey -algorithm SM2）导致正向对照失败被记成产品 FAIL；同仓库的 sm2_openssl_test.go 早已把这种情况记成 NOT_EXECUTED | 正向对照前增加一次 SM2 能力探测，不支持则 SKIP 并写明 NOT_EXECUTED；显式设置 EDGEHYBRID_OPENSSL 时仍硬失败 | Linux/WSL 全量 race 复跑：该用例由 FAIL 变为 SKIP，全套 0 失败 |

| IDENT-02 | P3 | `secctl rotate` 输出的 `previous_generation` 与 `retired_key_ids` 取自命令自己事务前的一次读取；若注册表在两次读取之间变化，审计记录会写出**并未被退役**的 key_id（注册表本身仍正确） | `management.Result` 新增 `previous_generation` / `retired_key_ids`，由事务内部 CAS 保护的那次读取填充，CLI 改为打印它 | `cmd/secctl` 新增 `TestVerificationRotateReportsTheTransactionRetiredKeys` |

补充说明：
- FMT-01 是纯机械的格式化归一：受影响的 13 个文件只发生空白、语句折行、import 分组顺序与文档注释规范的变化，不含语义修改；全量测试通过。
- AUDIT-02 与 API-02 会改变对外可观测行为：前者在超过 512 条被拒记录后不再逐条留痕，后者把缺字段的 409 改为 400。两者都是对既有文档契约的落实，已在 docs/VERIFICATION_BUGFIX3.md 与发布说明中披露。


- `status` 的 REPEAT 语义未改；`retire` 的既有提示文本未改，历史 `results/raw/**`
  里的 stderr 字节仍可对照。
- IDENT-01 对现有调用者无行为变化（现有调用者都同时给出两个字段），它只是让
  前置条件真的按注释比较。
- 已复核但判定**不是**缺陷、因此未改动的项：遥测在会话结束时被置为 `unconfirmed`
  而非 `failed`（`writing`/`written` 分支优先，`failed` 只用于从未写出的 `queued`
  操作）；`Channel.Receive` 两段式空闲读各自 `armIO`，`ioGuard` 的代数在第二段重新
  认领，取消回调并未失效；`record` 层在写入结果未知前消耗序号是有意设计并在注释中
  声明；`state.go` 按序号取设备视图在 `Count ∈ {1,20}` 约束下不会空指针。
- 已复核、确认存在但本轮**有意不改**（改动属契约或设计决策，需要单独授权）：
  - `secctl lifecycle` 之外，`ROTATING` 状态没有生产写入路径，但 `Lookup` 与
    `revoke`/`retire` 都不接受它；一旦手工写入该状态只能改 `trust.json` 恢复。
  - 未认证请求也会写公共审计日志（AUDIT-01 只免掉了成功的只读轮询）：本机
    120 请求/秒仍可在约 9 分钟内耗尽 20MiB 上限并触发 `运行停止`。
  - 缺少必需字段时返回 409 业务拒绝而不是文档所述的 400 输入错误。
  - `transport.go` 的连接名额耗尽时接受后立即关闭并继续循环，而不是返回 503；
    20 台上限下正常使用碰不到 24 个并发握手，属本机洪泛场景。
  - 已发布源码中有 5 个 `internal/identity/*.go` 文件不符合 `gofmt` 对齐；本轮为
    保持改动可复核，未把格式化混进修复补丁。

## 本轮验证边界

已实际执行：`go mod verify`、`go vet ./...`、`go build ./cmd/...`、
`go test -count=1 ./...`（24 个有测试的包全部通过，896 个子测试通过，0 失败）、
`go test ./cmd/gateway ./cmd/secctl ./internal/app ./internal/identity
./internal/management ./internal/pbench ./internal/console ./internal/wire -run '^TestFriend'`、
`node scripts/interactive_frontend_test.mjs`、`node scripts/logs_refresh_test.mjs`、
真实 Windows EXE 单设备与 20 设备安装验收（含任务、导出、取消、停机）、真实
`secctl.exe` 生命周期与参数正反例、`source_manifest.py` 清单重算比对。

未执行或阻塞：`go test -race` 在本机缺失可用的 race 运行时依赖，测试二进制以
`0xc0000139` 退出，记为 BLOCKED，不声称 race 通过；标准 SM2 S1/S2 独立确认需要
BouncyCastle/OpenSSL，本机未安装，记为 BLOCKED；1800 秒长测、物理断网、真人新手
试用未执行。旧性能数据仍属于原测量版本，本轮不重新采样。
