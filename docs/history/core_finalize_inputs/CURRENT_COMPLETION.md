# 当前实现与本轮证据

版本interactive-v1.2-backend2-status1-releasefix2。本轮只关闭F07/D1/D2/D3/D4。logs.js固定范围选项与表格/分页/导出一致，保存有效选择、明确无匹配、失败不换范围；其他前端文件冻结。当前事实以本轮原始命令/浏览器记录和发布外置回执为准，不按文件名推定全部通过。

八个页面及后端真实通信、设备网关生命周期、任务/配额/授权/防伪、旧队列和批量修复沿用现有实现，只回归。完整历史证据从自包含审查包活动索引解析；旧索引单独归档，不借历史pending覆盖当前实现。

手册docs/user/USER_MANUAL.md。本轮results/raw/release_five_20261003；发布目录说明PACKAGE_INFO.md准确列source/edgehybrid-agent-starter层级。当前接口包头纠正实现说明，错误拒绝逻辑未改。

旧性能非本版新实测，不重新采样1000或1800。标准SM2 S1/S2独立确认未执行；M6 A/B覆盖和检出相同；未知和失败保留。新Windows EXE必须重建嵌入logs.js并与build_info/source清单一致。

本轮六项独立复查修复见docs/VERIFICATION_BUGFIX.md；本轮证据results/raw/verification_bugfix_20261003。发布文件为EdgeHybrid_Verification_Bugfix_Windows.zip和EdgeHybrid_Verification_Bugfix_Review_Package.zip；此前发布及五项关闭记录属于历史来源。当前完成状态以匹配新源码的原始命令与新外置回执为准。

## releasefix3 增量

版本interactive-v1.2-backend2-status1-releasefix3。基线是上面的releasefix2（源清单63fc9215…），复核其六项修复后新修十四项：secctl全部子命令拒绝尾随位置参数（CLI-02）、secctl lifecycle保留被拒绝条目的生命周期字段（CLI-03）、bench输出目录错误信息不再误报“目录已存在”（BENCH-01）、ApplyRotation前置条件按注释无条件比较lifecycle（IDENT-01）、写接口拒绝非对象JSON体如null（API-01）、任务输出超限不再记成运营者取消且回执先落盘（JOB-01）、页面轮询不再淹没事件环（AUDIT-01）、被拒请求审计加上限（AUDIT-02）、缺必需字段改回400（API-02）、ROTATING可被revoke/retire结束（LIFE-03）、连接名额耗尽不再空转（TRANS-01）、全量gofmt归一（FMT-01）。细节见docs/VERIFICATION_BUGFIX3.md。原发布包与历史results/raw字节不变；旧性能仍属原测量版本。
