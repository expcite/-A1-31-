# 独立复查六项修复

当前版本 interactive-v1.2-backend2-status1-releasefix2。来源是用户提供的 Verification_Result_Package/DEFECTS.md；不把其中测试结果当成本轮通过。旧版本、旧ZIP和旧原始结果保持字节不变。

|问题|最小修订|验证入口|
|---|---|---|
|LIFE-01|设备退役CAS提交后清理目标各世代文件；清理结果和可信提交分别返回；失败可重试|management/verification_retire_test.go、真实API/CLI|
|LIFE-02|仅退役管理允许读取拒绝条目，支持REVOKED→RETIRED；Lookup和轮换仍拒绝|identity生命周期原回归、API退役/重新准入拒绝|
|CLI-01|status拒绝任何附加参数；check-config拒绝尾随位置参数|secctl真实二进制正反例|
|EXPORT-01|授权state/events/devices返回的原事件范围保留至多16份、每份2分钟；三种导出读取同一快照；审计仍记录|朋友原full-ring反例原样保留，三格式/过期/配额/真实页面|
|D-BUILD-DEMO-PS51|中文PowerShell脚本加UTF-8 BOM，兼容内置PS5.1；构建流程不换工具|原生Windows PowerShell构建|
|D-DEMO-HELP-EXIT|flag.ErrHelp退出0，不获取运行目录、不创建实例|demo真实-h/--help及非法参数对照|

文件删除不声称物理安全擦除；清理失败不得恢复可信身份。CLI退役-keys默认采用trust文件旁的keys目录，非标准目录须显式指定。网关继续走已有维护窗口流程，不能通过设备管理绕过。

快照只含当前运行公共事件；在16个不同快照或2分钟保留期限之外，旧范围可被明确拒绝，需手动更新范围。它不提供全历史查询，也不扩大原16个导出文件、每文件4MiB限额。

本轮原始命令、退出码、失败和源码关联在results/raw/verification_bugfix_20261003。最终结论以发布包的问题关闭表与包外匹配验收回执为准。前端所有资源冻结；未改算法、H1–H4、记录层和授权/防伪校验。无正式1000次或1800秒重新采样；旧性能属于原测量版本；标准SM2 S1/S2独立确认仍未执行；M6覆盖与检出相同。
