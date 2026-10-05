# 结果目录

## 阶段报告

| 文件 | 内容 |
|---|---|
| `milestone_M1.md` | M1：真实密码组件集成与验证（**部分表述已被勘误**） |
| `milestone_M2.md` | **M2：传统国密一网关一设备真实安全链路**（含 M1.1 修补） |
| `m2_readiness.json` | 机器可读的验收等级与判据 |

`docs/M1_ERRATA.md` 修正 M1 报告中的过宽表述与一处**被证伪的故障归因**。
引用 M1 报告前请先读勘误。

## 原始证据（`raw/`）

| 文件 | 内容 |
|---|---|
| `m0_baseline.txt` | M0 骨架基线 |
| `m1_verification.txt` | M1 命令与退出码 |
| `m1_1_verification.txt` | **M1.1**：组件交叉核验 + **race 环境证据**（含 `__iob_func` 分析） |
| `m1_1_tests.jsonl` / `m1_1_summary.txt` | M1.1 范围 `go test -json` 与其自动统计 |
| `m2_verification.txt` | M2 命令与 `go mod verify` |
| `m2_tests.jsonl` / `m2_summary.txt` | 全项目 `go test -json` 与其自动统计 |
| `m2_negative_cases.jsonl` | **27 条结构化负面用例**（场景/期望阶段/实际阶段/是否建通道/是否交付明文） |
| `m2_e2e_events.jsonl` | 真实 TCP 端到端脚本产生的事件流 |
| `m2_e2e_summary.json` | 该次运行的结果摘要 |

统计与提取都由 `scripts/summarize-go-test-json.py` 完成，
**报告中引用的测试数字来自这些文件，不是手写的**。

## 旧骨架记录

`starter_verification.txt` / `starter_tests.jsonl` / `starter_verification.json`
是生成这个骨架时实际运行的检查记录。它们**只验证骨架**，
不是密码正确性报告，也不是完整握手或性能结果。

## 纪律

- **不要**用零值、随机数字或明文 lab 结果填充安全性能表。
- 结果文件**不得**包含真实秘密：不写共享秘密、PRK、会话密钥、nonce 前缀、随机种子。
- 未测量的字段留空并写明原因，**不要**用 0 冒充已测量。
- 本目录中的运行产物受 `.gitignore` 保护（`raw/*` 默认被忽略，仅保留 `.gitkeep`）。
  需要交付证据时请显式打包**脱敏后**的报告，不要把 `runtime/`、密钥文件或整个构建缓存打进去。
