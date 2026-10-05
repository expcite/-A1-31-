# 当前维护源及发布路径

版本 interactive-v1.2-backend2-status1-releasefix2；工作区沿用原工程，不重建、不覆盖既有修改。只修本次五项问题，其他前端资源和协议/业务/安全路径冻结。

以下路径均以审查ZIP解压后的包根为准：完整当前源码source/edgehybrid-agent-starter；本轮原始记录source/edgehybrid-agent-starter/results/raw/release_five_20261003；此前F09记录source/edgehybrid-agent-starter/results/raw/backend_status_20261002；六项后端回归source/edgehybrid-agent-starter/results/raw/backend_friend_20261002；正式原始记录source/edgehybrid-agent-starter/results/raw/m5_formal_20261001及source/edgehybrid-agent-starter/results/raw/backend_20261002；M6重算使用historical_sources/M6_resolved，不把旧源当成当前实现。

活动RELEASE_INDEX.json的路径根为审查包根，目录附逐文件hash。源码内docs/FINAL_DELIVERY_INDEX.json是同一索引的副本。旧索引原字节在source/edgehybrid-agent-starter/docs/history/FINAL_DELIVERY_INDEX_m6_original.json。没有开发机C盘依赖。

当前手册source/edgehybrid-agent-starter/docs/user/USER_MANUAL.md；构建来源build_info.json；本轮问题关闭表documents/ISSUE_CLOSURE.md；针对ZIP自身的最终哈希与独立解压回执置于ZIP外。根MANIFEST.sha256覆盖自身之外的实际文件。旧嵌套ZIP、EXE、运行时秘密和缓存不进入源码审查包。Windows包含本次重建8个EXE但无预置私钥和令牌。

本轮六项独立复查修复见docs/VERIFICATION_BUGFIX.md；本轮证据results/raw/verification_bugfix_20261003。发布文件为EdgeHybrid_Verification_Bugfix_Windows.zip和EdgeHybrid_Verification_Bugfix_Review_Package.zip；此前发布及五项关闭记录属于历史来源。当前完成状态以匹配新源码的原始命令与新外置回执为准。

## releasefix3 增量

版本 interactive-v1.2-backend2-status1-releasefix3，基线为上面的releasefix2（源清单63fc9215…，源文件417个）。本轮复核六项修复成立后另修十四项：CLI-02、CLI-03、AUDIT-01、AUDIT-02、BENCH-01、IDENT-01、API-01、API-02、JOB-01、LIFE-03、TRANS-01、FMT-01，见docs/VERIFICATION_BUGFIX3.md。八个EXE必须用scripts/backend_build.py重建；releasefix2的EXE与releasefix3源码不再对应。旧发布包、历史results/raw与旧ZIP保持原字节，不作为releasefix3的证据。
