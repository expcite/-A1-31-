# EdgeHybrid 当前后端复查修复版

版本 interactive-v1.2-backend2-reviewed，延续原 backend1 工作区；前端 V12 继续冻结。当前复查范围和补验理由见 [影响分析](docs/FRIEND_FIX_IMPACT.md)，完成状态只依据 [问题关闭表](deliverables/Backend_Reviewed_Fixed/ISSUE_CLOSURE.md) 和当前原始记录 results/raw/backend_friend_20261002。文件名不是全部通过或安全认证。

已实现 T/H 双向认证真实 SM2 完整密钥交换、SM3/HKDF-SM3、SM4-GCM；H 额外绑定真实 ML-KEM-768。混合失败不降级。P 是独立 TLS 1.3/X25519MLKEM768 公开对照，不是国密套件。签名、Finished、抗重放、序号写入顺序、限额和会话隔离保留。

交互版有八个页面、真实手动温度上报、网关主动控制、真实采样 timer、1/20 设备、受控设备/网关生命周期、有界筛选分页日志与导出、授权/CSRF/Host/Origin 校验。有限 gateway/device Demo、bench/pbench 与持续 live 的运行模式分别保留。HTTP 排队成功不等于远端确认，超时不等于对端未执行。网关维护影响全部相关设备，不会自动重连或恢复被吊销身份。

本轮修复保护消息 ACK 状态机和截断错误链、独立 CLI 网关本地身份失效、轮换事务与 legacy 0 世代兼容、P 拒绝健康前提以及同 host 多实例 Cookie 覆盖。F07 日志选择项刷新尚未修复，状态 PENDING_FRONTEND_AUTHORIZATION。此项不能视为完整体验验收完成。

backend1 的 Windows 全量 race、BC 第二实现 SM2 核心交换 14 组、OpenSSL 双向签名互操作已经实际执行。它们覆盖已执行路径，不是全面安全认证；本版须读本轮日志，不能把旧记录绑定到新源码。独立 SM2 对照范围不含对标准 S1/S2 的独立认证。

旧 M5/M6、backend1 的 16 批正式实验以及原 300 秒失败/随后完整 1800 秒长测保持原 run 和源码哈希。本版仅做小规模 T/H/P、短持续运行及缺陷回归，没有新 1000 次正式性能或 1800 秒结论。计时保留 0ns 及限制，批次 CPU/峰值内存不是单次指标，P 与 T/H 的整体差异不能全部归于 ML-KEM。历史 P 拒绝记录若缺同素材健康基线，不能证明目标机制。

## 运行与复查

Windows 包解压后使用启动1设备.cmd / 启动20设备.cmd，启动窗口的一次口令用于本机授权。暂停采样、断开设备、退出软件含义不同。运行不依赖 Go/Python/Node/Java/CDN；scenario.exe 仅隔离测试。不要同时运行 Demo/live 与测量程序。

开发与复查使用 Go 1.27.1；执行 go mod verify、go vet ./...、go test -count=1 -json ./...，Windows race 使用经授权的隔离便携 GCC（仅测试进程 CC/PATH）。scripts/backend_regression.py 保存真实 JSON、stderr、退出码；scripts/backend_w1.py 使用本机已有 Java/BC/OpenSSL，不安装依赖；scripts/friend_review_run.py 保存每次源清单和命令。

构建：python scripts/backend_build.py --out <新二进制目录> --evidence <新日志目录>。不覆盖现有发布包。各 EXE、源码哈希、版本通过 build_info.json / API build / 回执关联。

长测复现使用已修正 scripts/backend_live_acceptance.py：设备 connect 预期 HTTP 202。原 backend1 修正版外置测量脚本与原失败/完整通过记录由 EdgeHybrid_Backend_Final_Receipts.zip 保留；原冻结脚本和原实验哈希不改写。

现有详细手册见 deliverables/Interactive_V12/EdgeHybrid_Interactive_V12_Manual.md，后端接口见 docs/INTERACTIVE_API.md 和 docs/BACKEND_BUILD_AND_VERIFY.md。原 README 字节在 [历史 README](docs/history/README_backend1_original.md) 原样保留；历史未实现/未验证描述不表示当前功能状态。
