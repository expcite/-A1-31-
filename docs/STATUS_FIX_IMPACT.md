# F09 状态一致性修订影响与补验计划

版本 interactive-v1.2-backend2-status1；实际根 C:/Users/20477/WorkBuddy/密码技术竞赛/edgehybrid-agent-starter；继续 source21a723…、388项backend2工作树。不用旧ZIP覆盖，无Git，不初始化Git。

仅修改secctl status输出和维护文档（含cryptosuite/suite.go过期包头注释，package之后内容逐字节保持基线）；status拆到cmd/secctl/status.go，原CLI其他命令及业务状态机保持原实现。能力按真实suite gate和已实现入口描述，不把implemented标为PASS。正式构建为secctl注入当前版本/源清单，未注入时源hash为null；来源为构建者提供元数据，status不执行安全检测、不复核回执。验证部分引用已核对的source21a723…历史平台、run和路径，不能当新构建全项通过。

原TestRecheckSecctlStatusDoesNotClaimMissingImplementedBackend继续在新CheckKit overlay逐字保留，由quick在独立副本执行。直接复制同名文件/函数进工程会冲突，项目新增独立命名的真实可执行CLI回归，检查能力、版本、来源、范围和无副作用。修前真实FAIL及新语义测试失败保留；修后同例与正常正对照另存。

补验：目标CLI、go mod verify/vet/build、全量Go JSON及实际退出码/完整验收器、既有Python和Node测试；新版CheckKit在原生中文Windows -X utf8=0及-X utf8分别35自测，然后冻结源码执行quick。工具无改动，不修改全局编码，不吞解码异常；保留全部原字节和来源。真实构建8个EXE，更新build_info，独立解压检查清单、CLI状态、源码重建与运行启停。发布包内必须为新secctl.exe。

此前六项后端缺陷和截断、app/wire/Channel、CLI网关生命周期、identity/management、pbench、console/live实现不重写；通过源逐文件diff及既有回归确认。前端22文件开始/结束hash保持不变；F07日志筛选选项刷新 PENDING_FRONTEND_AUTHORIZATION，本轮未修。

不默认重跑race专项、正式1000或1800秒。既有 Windows全量race、SM2核心14组第二实现、OpenSSL记录保持原source/run；常规Go内实际包含对照时按当前JSON单列，不能冒充标准S1/S2确认或全面安全认证。M5/M6和原300秒失败/1800秒完整记录不改。旧答辩/M6文档pending是历史范围，当前索引明确其版本，不重写冻结资料。

本轮结果目录 results/raw/backend_status_20261002，发布整理 deliverables/Backend_Status_Corrected，外部独立工具/发布目录 ../EdgeHybrid_F09_Status_Work。失败、超时、中断及NOT_EXECUTED均保留。完成此范围后停止，等待用户前端授权。
