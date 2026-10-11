# GitHub 当前源码与交付物上传说明

本次上传是 EdgeHybrid 最新整合代码与交付物快照，包含 Final v16 文稿基线与已验证的 Evidence 测试增强升级。

- **软件版本**：`interactive-v1.6.2-e08fix1`
- **构建基线**：481 项生产源码清单精准匹配 `02863dd2efb03dd8885fb45133fdac44128eb462f581d03301417068f39b29ad`
- **增强测试树**：包含 5 个新增测试文件（10 组顶层 EVX 测试、137 个子测试），清单精准匹配 `fc771244200a641cd3554d9af5881f1cb2d207d8b7bddec39d062cd43d436cc0`
- **覆盖率指标**：
  - 六包联合语句覆盖率：`1549 / 1832`（**84.55%**）
  - `internal/evidence` 覆盖率：`457 / 471`（**97.03%**）
  - `internal/exports` 覆盖率：`85 / 88`（**96.59%**）
- **Race 状态**：`PASS`（10 组 EVX 竞态测试全部通过，0 数据竞争告警）
- **Fuzz 状态**：`PASS`（FuzzEVXEvidenceDecode 累计 617,175 次执行，0 崩溃）

### 包含内容
- 完整 `cmd/`、`internal/`（包含 5 个新增强测试用例与内嵌前端组件）；
- 核心配置 `configs/`、运维脚本 `scripts/` 与开发文档 `docs/`；
- 正式报告文档 `report/EdgeHybrid_Report_Final_v16.pdf` 与 HTML 版本；
- 验证回执 `verification/` 与整合凭证 `INTEGRATION_RECEIPT_EVIDENCE_UPGRADE.json`。

### 排除内容
根据安全与轻量化规则：
不上传 runtime 二进制可执行文件（8 个 EXE 随本地发布 ZIP 分发）、真实私钥、会话秘密、临时缓存及大体积历史 raw 数据。

本次提交使用 `[skip ci]`，未安排远端执行。
