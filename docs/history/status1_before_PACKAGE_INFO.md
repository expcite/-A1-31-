# EdgeHybrid M6 审查包说明

本包为当前完整源码、测试、脚本与公开证据。M6到此停止。生产Go、旧M5数据和原Windows用户Demo未改；本包不含Demo可执行文件，也没有重新发布用户包。

首读 docs/FINAL_DESIGN_REPORT.md、docs/INNOVATION_REPORT.md、docs/USER_GUIDE.md、docs/REPRODUCE.md、docs/DEFENSE_QA.md。需求对应表 docs/FINAL_DELIVERY_INDEX.json 与 REQUIREMENT_EVIDENCE_MAP.md。正式设计说明书为 deliverables/M6/EdgeHybrid_Final_Design.pdf，答辩为 EdgeHybrid_Defense_v2.pptx（12页、可编辑图表/表格）。Word生成但渲染失败，草稿保留包外，不当作已验证材料交付。

M6冻结计划 docs/INNOVATION_PLAN.md；raw results/raw/m6_20261001。comparison_v2是完整三轮对照、三个隔离缺陷A/B和三条复现，保留实际命令/Go JSON/退出码。A/B共同覆盖16/16、缺陷检出3/3，未显示覆盖或检出优势。B改善统一诊断与公共证据，日志更大，未测人工效率。compare_v1及开发提取失败、构建和渲染失败仍保存，不能宣称所有历史命令全部通过。

M5来源在 results/raw/m5_formal_20261001/data/results_index.json 与 report_verified。原采样source hash fad0abe7326003b0b1b1ce51f0699acdb3b7f5bcf6bbc9f636026ada2cf996fd，原交付11c49ff72ec758a5a2d6c2f1d2d46b0c88894ed00bbaa441785e5b1922b0227d，均不替换为M6版本。source_resolution.json为M5采样/交付与M6测量每条源码提供实际hash验证归档。最终当前源码清单另见M6 delivery_source_manifest.sha256和delivery_source_info.json。

regression存全量Go JSON/真实退出码、严格汇总、go mod verify/vet、四组原Python自测、新证据拒绝自测，以及16批M5实际完整验收重算。本轮未重采1000次、未新跑Demo。M5原文件hash逐一未变，Demo zip原hash 96bc6b44d21b2a44e25f13f7f181845934d2173373687bf4f56cf25d187226ac。

0ns观测保留；1000/1000是有限观测。CPU/峰值为100握手进程批次，缺测null+原因。P与T/H的整体差异不全归因ML-KEM。race、独立SM2交换pending，OpenSSL签名SKIP，Linux资源NOT_TESTED。没有独立密码安全证明、标准合规认证或新增跨机/低资源硬件验证。

包内MANIFEST.sha256覆盖自身外全部文件。实际最终ZIP校验、解压重建与16批复验结果在包外 EdgeHybrid_M6_Review_Package.verification.json 和同名 verification_logs；它们标明是重建验收程序，不是原采样二进制。包内已有原采样/完整回归和重算验证结果。不含运行秘密、缓存、exe/dll或历史嵌套ZIP。

依赖及许可见 go.mod/go.sum、docs/DEPENDENCIES.md、THIRD_PARTY_NOTICES.md。自定义协议与本项目未获整体安全认证；项目对外开源许可未由权利人指定。
