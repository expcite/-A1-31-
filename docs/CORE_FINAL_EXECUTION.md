# 当前固定执行预登记

采用用户v3代替v2，来源与87项工具fixture测试均与产品结果分开。完整参数见CORE_CAMPAIGN.json及BACKEND_FORMAL_EXPERIMENT_PLAN.md；严格bench/pbench格式契约仍引用FORMAL_EXPERIMENT_PLAN.md，格式ID不表示旧采样归属。

先完成普通/Windows race/独立SM2核心/OpenSSL/HKDF、Friend/Recheck、既有前端及工具回归，然后冻结version/campaign/source/计划/命令来源和二进制。正式矩阵固定16批，失败不删，producer 3为有失败的完整证据而非全部成功；任何前置/证据失败不继续PASS。长测从全20实际认证计1800秒，300/600/1200秒断开指定设备再完整新握手，900秒受控网关轮换，保存限龄换钥和维护窗口。先短烟测、正常清理与重启，正式矩阵/长测/构建/race/M6串行。

M6以历史发现启发的后续预登记：原A五文件全部测试；现B39规则用例；三种隔离客户端变体D1/D2/D3，同一A/B集合，顺序A/B、B/A、A/B各三轮；先正常对照，独立复现三个case。记录DETECTED/MISSED/UNREACHABLE及执行错误，不新增变体、不改生产Go或测试后门。不称多测试就是创新，不推导人工工时或通用速度优势。

所有执行输出在外部工作根。报告、verification_status、CURRENT_DELIVERY_INDEX与H_delivery另存；冻结后不改源码/计划。当前与历史来源均逐文件标明，最终删除raw/坏hash/缺Windows依赖必须被拒绝。

当前回归的Go包间调度固定为-p=1；包内20设备并发、校验断言与deadline不改。首次默认包间并行的CLI网关有25秒受控停止失败，保留原FAIL及未证实根因；包间串行专项正对照另存，不改写初次结果。仅cmd/secctl/status.go发布版本常量同步VERSION，不改status验证语义。


本轮实际发现：v3首个1800秒运行的最终停止窗口内有一条DialContext的operation was canceled，旧工具无条件计为异常；原FAIL及7158条匹配通信保持原样。v3b预登记只修工具分类：必须为device已登记身份、当前run、最终停止窗口内、明确取消原因，且实际干净退出及连接/会话/worker/队列归零可证；保留原dial_failed计数和event，SID未知仍null。普通拨号失败、窗口外取消、未证实清理均不能接受。新当前campaign独立完整执行，固定分母不合并、不补成功数；不改变协议、前端或生产业务。
