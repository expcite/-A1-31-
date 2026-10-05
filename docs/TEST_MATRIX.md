# 测试清单说明

按阶段保存的可编辑清单在 `TEST_MATRIX.csv`；其中 `status_at_delivery` 与 `milestone`
表示对应阶段交付时的状态，不是当前构建检测结果，不覆盖后续原始证据。

## 当前验证来源（F09 修订）

当前源码已有 Suite1/Suite2、真实 benchmark 和交互通信。`secctl status` 报告能力与出处，
自身不执行测试或安全检测。`interactive-v1.2-backend2-reviewed` 的历史源清单
`21a723b1443f33e9e9fcbfa766060f0100df780b5a8c069f21cac172a8396d1b`
已在 Windows 11 / amd64、Go 1.27.1 执行全量 race、14 组独立 SM2 核心交换对照和
OpenSSL 双向签名互操作。来源为 `results/raw/backend_friend_20261002/final_regression/`
与 `W1/`。因此历史 `BLOCKED_environment_race_detector`、SM2 `agreement_pending`
不能继续写成当前状态。本轮状态/文档修订的新构建回归另行保存来源；没有对应新记录的
专项不得直接升级为新构建 PASS，旧失败和阶段清单不改写。

独立核心交换不包含标准 S1/S2 确认，签名互操作不能证明交换互操作；任何有限测试通过
都不是全面安全认证。当前能力与具体边界见 `internal/cryptosuite/README.md`。

## 状态词的含义

| 状态 | 含义 |
|---|---|
| `implemented_starter` | 骨架已有对应测试 |
| `implemented_test_aead_only` | 只用测试用 AES 替身检验记录状态机（不是生产密码） |
| `implemented_plaintext_only` | 明文实验，**不是**安全联调 |
| `implemented_component_level` | 组件级实现并测试；**不等于**协议级完成 |
| `implemented_tested` | 有真实实现，且有对应测试通过 |
| `implemented_tested_tcp` | 真实 TCP 端到端通过 |
| `implemented_tested_real_sm4` | 使用真实 SM4-GCM（非测试替身）通过 |
| `independently_cross_verified_openssl` | 与**独立实现**（OpenSSL）逐字节一致 |
| `official_rfc8998_vector_verified` | 与**官方标准向量**（RFC 8998 A.1）逐字节一致 |
| `partially_verified_*` | 部分验证；后缀写明**还缺什么** |
| `documented_hazard_test` | 该测试固定的是一个**已确认的风险/边界**，而非已修复的缺陷 |
| `replaced_by_per_suite_gate` | 旧的粗粒度占位检查已被**按套件**的门控取代 |
| `BLOCKED_environment_race_detector` | 对应历史命令的**环境阻塞**，不是通过；原失败见 `docs/M1_ERRATA.md` E1。后续 Windows race 结果另有来源，不能用这个阶段标签断言当前未执行 |
| `planned` | 未实现/未验证，**不得**在验收报告中打勾 |

## 记录要求

每一条后续用例应有：输入或攻击变更、预期失败阶段、实际失败阶段、进程退出码、
程序/配置版本、对应日志。对篡改测试要同时覆盖“字段仍然语法合法”的攻击，
不能只用无效长度包证明密码认证生效。

## 测试数量从 JSON 生成，不手填

`scripts/summarize-go-test-json.py` 从 `go test -json` 输出统计
package / top-level / subtest 的 pass、fail、skip。
原始 JSON 在 `results/raw/m2_tests.jsonl`，统计结果在 `results/raw/m2_summary.txt`。

## 负面用例的机器可读记录

篡改测试把每个用例的结构化观测写进测试输出（前缀 `M2NEG`），再由同一个脚本
提取为 `results/raw/m2_negative_cases.jsonl`：

```text
test, name, scenario, expected_stage, actual_stage,
channel_created, plaintext_delivered, detail
```

`channel_created=false` 与 `plaintext_delivered=false` 是**断言结果**而非人工填写：
它们只有在 Channel 确实为 nil 时才能通过。

**不能记录真实共享秘密**；相等性检查留在测试内部，不打印秘密或可恢复秘密的中间值。
事件日志只输出公开元数据（角色、阶段、结果、身份、SID 前缀、套件号）。
