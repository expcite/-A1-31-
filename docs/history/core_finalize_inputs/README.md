# EdgeHybrid 五项发布修订版

版本 interactive-v1.2-backend2-status1-releasefix2。继续当前工作区及已有修改。本轮修复独立复查六项问题：退役密钥清理、吊销后退役、CLI参数拒绝、满日志快照导出、PowerShell5.1 Demo构建和Demo帮助退出码。没有新增功能或里程碑；前端全部冻结，密码协议未修改。

T/H使用双向认证、SM2完整核心密钥交换、SM3/HKDF-SM3、SM4-GCM；H绑定真实ML-KEM-768两份共享结果。P为TLS1.3/X25519MLKEM768公开对照。权限、CSRF、Host/Origin、Finished、防降级、抗重放、配额与会话隔离保持原实现。

交互版八个页面、真实手动温度、主动控制、实际timer、1/20设备、设备和网关生命周期、隔离任务、有界日志和导出均保留。仅logs.js必要筛选逻辑解除冻结，布局、样式、导航和其他页面不变。更新记录范围同时更新未筛选快照的选项/表格/分页/导出；有效选择保留，消失选择明确0匹配，不静默放宽。轮询不打乱固定分页。

当前详细使用入口：[用户手册](docs/user/USER_MANUAL.md)。原V12手册按原字节另存；补充说明优先。后端说明：[API](docs/INTERACTIVE_API.md)、[构建和验收](docs/BACKEND_BUILD_AND_VERIFY.md)。当前发布核对：[本轮影响与复现](docs/RELEASE_FIVE_FIXES.md)、[需求证据映射](docs/REQUIREMENT_EVIDENCE_MAP.md)。

## Windows运行

解压Windows包，双击启动1设备.cmd或启动20设备.cmd，在页面输入启动窗口的一次口令。停止交互平台.cmd结束软件；关闭网页不停止进程。HTTP接受不等于通信确认，ACK超时不证明设备未执行。网关维护影响全部相关设备，已吊销身份不自动恢复。运行不依赖Go/Python/Node/Java/外网CDN。

## 复查

源码项目根执行go mod verify、go vet ./...、go test -count=1 -json ./...；Python/Node自测及CheckKit记录以实际退出码为准。构建python scripts/backend_build.py --out 新目录 --evidence 新目录，EXE/build_info/源码清单必须一致。secctl status只描述能力和元数据，不执行验证。

审查包根RELEASE_INDEX.json和本项目docs/FINAL_DELIVERY_INDEX.json内容一致，路径根均为“审查包根”。其树清单覆盖必需M5、M6、backend1原始下层文件及采样源清单；历史作者绝对路径仅保留为原始元数据，不作为复查入口。完整复查执行python source/edgehybrid-agent-starter/scripts/release_verify.py --root 审查包根 --out 新输出目录 --go Go路径，实际重建结果验收器，对32批旧结果和M6进行只读离线验收。

旧M5/M6/backend1原run与源hash不变；旧300秒失败与后1800秒通过都保留。长测复现用scripts/backend_live_acceptance.py修订脚本（connect HTTP202）；不重写旧冻结脚本。当前仅有限界面与交付修订，不重新采样正式1000次或1800秒，也不将旧性能标为当前版本。保留0ns、计时限制、批次CPU/峰值口径及失败分母；P整体差异不能全归于ML-KEM。

历史Windows全量race、14组BC独立SM2核心对照和OpenSSL互操作已执行，按原source21a723/backend_friend_20261002解释；当前新回归以本轮记录为准，不冒充全面安全认证。标准SM2 S1/S2独立确认未执行。M6两组共同规则覆盖和缺陷检出相同，改善可审计信息完整度。

历史README/索引原字节在docs/history；历史阶段的pending不表示当前实现状态。

本轮六项独立复查修复见docs/VERIFICATION_BUGFIX.md；本轮证据results/raw/verification_bugfix_20261003。发布文件为EdgeHybrid_Verification_Bugfix_Windows.zip和EdgeHybrid_Verification_Bugfix_Review_Package.zip；此前发布及五项关闭记录属于历史来源。当前完成状态以匹配新源码的原始命令与新外置回执为准。
