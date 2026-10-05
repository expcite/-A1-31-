# 当前版本使用与复查补充

版本 interactive-v1.2-backend2-status1；冻结源码398项，清单SHA-256 d4622b841e570e511f06c61f40174111544c5e3b73e2aa08a87734b4ce50abcc。此修订仅更新后端状态及说明，八个页面与布局/样式/导航均保持V12原样；原完整V12手册继续适用，本补充更正其中过期验证状态。F07日志选项刷新是已知功能问题，PENDING_FRONTEND_AUTHORIZATION，尚未修。

解压Windows包，双击“启动1设备.cmd”或“启动20设备.cmd”。在本机窗口取得一次口令，授权后连接、手动发送温度、设置周期并查看真实确认。刷新不重发操作。停止软件使用“停止交互平台.cmd”；暂停采样仅停止自动上报，断开设备结束该连接，退出软件停止全部工作者。运行依赖Windows，不需要Go/Python/Node/Java或外网CDN；首次本地生成测试身份，发布包不预置私钥或访问令牌。scenario.exe为已隔离测试入口。

在软件目录执行 `secctl.exe status`：JSON schema为edgehybrid-status/2，version为本版，build列实际Go/平台和注入的源码清单。suites/benchmark/components只描述代码能力；没有注入source时为null。verification单列历史源码21a723…、backend_friend_20261002及Windows11/amd64来源和范围；current_build为NOT_CHECKED_BY_STATUS，executed_by_status为false。命令本身不执行握手、race或攻击检测，也不读取回执来确认当前版本安全。

Windows全量race、SM2核心交换BC第二实现14组、OpenSSL动态SM2签名和HKDF已在历史source21a723…执行。SM4-GCM是独立RFC8998向量/组件验证，不称OpenSSL互操作；标准S1/S2独立确认没有执行。这些有限验证不是全面安全认证。新版本常规Go中实际执行的组件对照另看本轮JSON。

后端CLI/API不变：设备轮换由secctl或受控管理API；网关生命周期使用高级管理的维护窗口并影响全部相关设备，不通过去掉角色判断绕过维护。签名、Finished、防降级、抗重放、配额、会话归属和持久证据保留。HTTP接受/排队成功不是远端处理成功，ACK超时不证明设备没有执行。

复查命令：`go mod verify`、`go vet ./...`、`go test -count=1 -json ./...`；本轮脚本 `scripts/status_review_regression.py --out <新目录> --node <现有Node路径>` 保存stderr和真实退出码；构建 `python scripts/backend_build.py --out <新目录> --evidence <新日志目录> --version interactive-v1.2-backend2-status1`。新版secctl会注入本源hash，build_info逐EXE列hash。Go1.27.1与既有工具是开发/复查依赖，不是运行包依赖。

CheckKit `checkkit-utf8-v1.1` 在新的独立工具目录使用：`python -X utf8 -m unittest discover -s tests -v`，然后 `python -X utf8 tools/run_checks.py --project <源根> --profile quick --go <现有Go> --out <工程外新目录>`。不修改系统编码、不吞Unicode异常，stdout/stderr保留原字节。工具原TestRecheck状态断言逐字保留。源码包含完整工具来源与复现命令，不能把提供方Linux旧结果冒充本机运行。

历史M5/M6性能和300秒失败/1800秒完整长测保持原source/run/raw，未重新采样；不把0ns删掉、批次CPU/峰值改成单次指标或把P与T/H整体差异全归ML-KEM。本轮当前结果与未执行项看RESULTS.json、ISSUE_CLOSURE.md；ZIP最终解压验证回执在ZIP外，避免自身hash循环。原手册与M6答辩材料中的早期pending只属于其历史版，不能覆盖本轮实际记录。
