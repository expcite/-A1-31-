# M5A-R2 Final 重验报告（审查补丁采纳后）

**run_id**：`m5a_r2_final-20260930T004433Z`
**基线**：`EdgeHybrid_M5A_R2_Fixed_Package`（审查方在提交的
`EdgeHybrid_M5A_R2_Review_Package.zip` 上应用的 review patch，见
`results/M5A_R2_ASSISTANT_PATCH_NOTES.md` 与 `results/M5A_R2_REVIEW_PATCH.diff`）。
补丁文件已全部采纳到当前工程，**未回退任何修复**。

**本环境**：Go 1.27.1（满足工程 >=1.25 要求），Windows/amd64 —— 补丁作者声明其审查环境
Go 1.23.2 无法编译，因此以下全部结果均为本机真实执行，非审查环境复述。

**源码清单**：146 文件，哈希 `73080319c1a1319f260633f42fa14c1589304b0248a6b424bda41b0075b0bdf2`

## 补丁内容采纳核对

补丁修复的 8 类缺陷（生产者关闭顺序、持久化错误传播、重复键/尾随垃圾、mode↔suite 绑定、
SID 十六进制语法、孤儿确认、契约完整性、状态文档更新）全部体现在当前树中；
对应的 6 个新回归 `assistant_closure_test.go` 全部通过（见下）。

**一处包内不一致的处置（如实记录）**：补丁自带的
`TestVerifyResultsRejectsUnbackedSuccess` 断言问题消息包含 `"claims success"`，
而补丁的 bench.go 实际输出为 `success=... but phase facts recompute to ...` ——
缺陷本身被检出（两条问题），仅措辞不一致。处置：把验证器消息改为
`claims success but the phase facts recompute to success=...`（补齐语义而非削弱断言），
测试随即通过。

## 重验结果（全部本机真实执行）

| 项 | 结果 |
|---|---|
| `go version` | go1.27.1 windows/amd64 |
| `go mod verify` / `go vet ./...` / `go build ./cmd/...` | 全部 exit 0 |
| **全量 `go test -count=1 -json ./...`** | **301 顶层 / 218 子测试 / 0 失败**，17 包（12 有测试），真实退出码 0 |
| `assistant_closure_test.go` | **6/6 通过**（持久化错误记录、重复 summary 键、事件尾随垃圾、mode→suite 绑定、精确十六进制 SID、未匹配确认） |
| 四套 Python 自测 | 17 / 45 / 23 / 9 全过（bench 自测驱动生产 Go 验证器，夹具已按收紧后的契约更新） |
| Suite 1 / Suite 2 E2E | PASS / PASS |
| 20×3 fleet | PASS — 60 会话 / 60 唯一 SID / 180 遥测 / 180 控制 |
| 19 good + 1 bad | PASS — 57 会话，D010 失败 |
| **新 T smoke（补丁后源码）** | 10/10，PASS，median 1.02 ms，单次 308/272 B，网关确认 10/10 |
| **新 H smoke（补丁后源码）** | 10/10，PASS，median 1.02 ms，单次 1492/1360 B，网关确认 10/10 |
| 离线 `cmd/bench -verify` | 两组新 smoke `problems=[]`（exit 0） |

smoke 目录（**旧数字未复用**，均为补丁后源码新生成）：
`results/bench/m5a-traditional-20260930T005355.637923800Z`、
`results/bench/m5a-hybrid-20260930T005408.730663400Z`，均带
`run_kind=smoke`、`formal_results_eligible=false`、父 run_id 与源码清单哈希。

## 用户指定 6 项缺陷扫描（两组新 smoke，独立脚本 + 严格 object_pairs_hook）

| 检查项 | T | H |
|---|---|---|
| duplicate JSON key | 0 | 0 |
| trailing garbage | 0 | 0 |
| traditional/Suite2 或 hybrid/Suite1 | 无 | 无 |
| 非 32 位十六进制 SID | 0 | 0 |
| orphan control-confirmed | 0 | 0 |
| evidence write 错误被吞 | 无（`EventSink.Err()` + finalize 路径；assistant 测试覆盖） | 无 |

## 未执行 / 保持 pending

- `-race`：**ENVIRONMENT_BLOCKED，未执行（pending）**；
- SM2 密钥交换独立对照：**不可得（pending）**；
- 正式 1000 次实验：**未运行，保持锁定**（本轮不进入 M5B）。

## 声明

smoke 数字为单机单进程小规模观测，不构成正式性能数据；本轮为补丁采纳后的
重验与收口，不构成安全审计或合规认证。

**M5A-R2 Final 重验完成，停止；正式 1000 次保持锁定。**
