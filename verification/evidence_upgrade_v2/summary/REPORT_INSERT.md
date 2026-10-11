# v16 报告定点更新材料

本文件由实际测试回执生成；不是主报告。保留用户 v16 的现有排版和其他成绩。

任务状态：COMPLETE_FOR_TEST_ONLY_SCOPE。

## 新一轮同环境覆盖率（六包联合插桩、同一测试来源范围）

| 包 | 新基线覆盖/总数 | 补测后覆盖/总数 | 补测后语句覆盖率 |
|---|---:|---:|---:|
| `example.com/edgehybrid/internal/cryptosuite` | 262/389 | 262/389 | 67.35% |
| `example.com/edgehybrid/internal/evidence` | 168/471 | 457/471 | 97.03% |
| `example.com/edgehybrid/internal/handshake` | 705/831 | 705/831 | 84.84% |
| `example.com/edgehybrid/internal/policy` | 21/30 | 21/30 | 70.00% |
| `example.com/edgehybrid/internal/record` | 60/63 | 60/63 | 95.24% |
| `example.com/edgehybrid/internal/wire` | 44/48 | 44/48 | 91.67% |

六包合计：1260/1832（68.78%）→ 1549/1832（84.55%）。

历史 168/471、1307/1879 属于原批次，仍保存在历史说明和原始记录中。新前后对照来自本轮独立基线；不得将两个时期的百分比做算术平均。

链根与公开导出测试属于 internal/exports，单独列结果，不添加进上述六包统计。Parse 为结构检查，根复算与可信文件摘要比较是另行执行的步骤。

## 执行状态

| 阶段 | 状态 |
|---|---|
| preflight | PASS |
| baseline | PASS |
| smoke | PASS |
| measure | PASS |
| regression | PASS_WITH_SKIPS |
| race | PASS |
| fuzz | PASS |

生产实现默认不变，仅新增测试；发布 EXE 沿用原构建身份。增强测试树另有清单，不能把其哈希写进未重建 EXE 的 build_info。
新测试通过只支持对应输入与断言，不支持“证据不可伪造”“文件与摘要同时被改也可检测”“全仓零漏洞”等结论。
