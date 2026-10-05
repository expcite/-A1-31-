# F09 定向问题关闭表

当前 interactive-v1.2-backend2-status1；源码398项，SHA-256 d4622b841e570e511f06c61f40174111544c5e3b73e2aa08a87734b4ce50abcc。实际工作区 C:\Users\20477\WorkBuddy\密码技术竞赛\edgehybrid-agent-starter；发布目录 C:\Users\20477\WorkBuddy\密码技术竞赛\EdgeHybrid_F09_Status_Work\Release。无Git，未初始化Git，没有旧ZIP覆盖。只关闭本轮F09；F07和下列未执行范围继续明确保留。

|项|状态|实际修改/范围|原始证据（相对results/raw/backend_status_20261002）|
|---|---|---|---|
|F09 CLI状态|CLOSED_WITH_EVIDENCE|旧M4.2/benchmark未实现/本机race阻塞/无SM2独立对照/GUI未实现断言删除；新增真实能力、版本、平台、构建source及历史证据范围；不是status自测安全|before/status_output_correct_path；before/unchanged_checkkit_status退出1；after/final_release_status退出0；release_regression/commands/status_target与go_test|
|F09维护说明|CLOSED_WITH_EVIDENCE|密码README、维护态边界、三行过时密码包头注释及当前README/交接/版本修订；原README字节归档，密码包package之后全同|ACTUAL_CHANGES.patch、SOURCE_CHANGES.json、CORE_PRESERVATION.json；audit_release|
|原朋友status回归|PASS|原TestRecheckSecctlStatusDoesNotClaimMissingImplementedBackend原样保留在工具overlay，真实构建CLI执行；产品新增独立命名真实语义测试，未靠删字段通过|checkkit_quick_release/check-20261002T161712Z-454a3b29/logs/ordinary_01；新增cmd/secctl/status_cli_test.go|
|UTF8工具Windows35自测|PASS|原生中文Windows11、Python3.12.10，实际cp936/gbk默认和-X utf8分别35/35，exit0；未改全局编码，未ignore/replace吞日志|checkkit_selftests/report.json、两模式原始stderr/stdout/退出码/环境探针|
|新CheckKit quick|PASS_WITHIN_EXECUTED_SCOPE|10包28顶层+161子项PASS，无FAIL/SKIP/缺必测；实际工具/Go/driver exit0，原工程前后hash相同|checkkit_quick_release/check-20261002T161712Z-454a3b29；checkkit_quick_release_driver|
|常规全量/构建|PASS（已执行范围）|29包、427顶层PASS/477子项PASS、0FAIL、1可选长测SKIP；mod verify/vet/build、4个Python自测和43个Node断言exit0；8个EXE真构建|release_regression、release_build、final_release_build_driver；BUILD_INFO.json|
|F07日志选项刷新|PENDING_FRONTEND_AUTHORIZATION|仍未修，明确为已知筛选功能缺陷；日志基本有界分页/导出保留|FRONTEND_FREEZE_RECEIPT.json：22项开始/结束一致|
|此前六项后端及截断|仅回归、不重写|ACK、错误链、CLI网关失效、轮换/legacy、P目标拒绝前提、多实例认证原实现保留；旧包头仅注释修订|225个旧Go文件逐字不变；真实CLI/TCP原回归在本轮full/quick，SOURCE_CHANGES/CORE_PRESERVATION|

status只报告实现能力和构建者提供元数据；未注入source时null，current_build为NOT_CHECKED_BY_STATUS、executed_by_status为false，不读取或核验历史回执。历史source21a723…的Windows race、SM2核心14组BC第二实现、OpenSSL动态双向SM2签名/HKDF已实际执行；不是当前status新检测，不是标准S1/S2确认或全面安全认证。SM4-GCM另有RFC8998向量/组件验证，不能写成OpenSSL SM4互操作。

本轮普通Go包含外部组件对照时按当前JSON记录；本轮未另跑race专项、正式1000或1800秒，不把旧性能改绑本版。原300秒失败、1800秒完整记录及M5/M6冻结材料继续保留。首次测量驱动相对路径错误没有启动产品，NOT_EXECUTED；初始只读审视因冻结8eb与已收窄4819不匹配退出1，记录保留；两次中间源码完整通过记录保留但不冒充最终d462。版本相同但源hash不同，必须按源hash区分。

Windows/审查ZIP独立解压复验的实际回执放ZIP外，结论须读其status和zip/source绑定；打包CRC/hash通过不等于运行验收。未执行的新race/长测/新截图/真人试用/标准S1/S2/全面认证不写成PASS。完成本轮后停止，前端等待用户方案。
