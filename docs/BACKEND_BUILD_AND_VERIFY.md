# V12前端冻结后的后端构建与验证

继续当前工程，后端构建版本为`interactive-v1.2-backend1`。被冻结的页面仍显示原V12静态文字和历史图表；当前来源以`GET /api/v1/build`、`build_info.json`、源码清单为准。新的正式数据不写入历史图表。用户软件运行不需要Go、Python、Node、Java或外网CDN。

## 构建及后端接口

用现有Go构建，`python scripts/backend_build.py --out 新的二进制目录 --evidence 新的证据目录`。Python仅用于开发审查。程序入口仍为`EdgeHybridInteractive.exe -devices 1|20 -no-browser`，`-stop`停止本包实例。bench/pbench/gateway/device/fleet/secctl为现有独立CLI，`scenario.exe`仅运行隔离测试，不供生产端点引入。所有二进制由同一源码清单关联，console内置任务仍核对允许的子程序SHA-256。

授权/CSRF/Host/Origin与原API兼容，授权口令只在启动窗口显示，Cookie和CSRF只在内存中用于审查。温度与周期通过原真实Channel，HTTP接受仍只是排队；必须观察operation的真实状态与对端确认。刷新页面不重发请求。新增`state.diagnostics`只补充Go进程观测，不改变权限、操作状态或业务字段；Go heap不等于操作系统工作集。

## 回归与便携工具

`python scripts/backend_regression.py --out 新证据目录 --cc 隔离工具gcc.exe --node 现有node.exe`保存全量Go JSON、stderr、命令和真实退出码、全量Windows race、既有Python验收器与Node前端测试。源码清单从执行前后检查；失败仍保留，不改写成通过。第二实现SM2与OpenSSL测试工具的来源、精确版本、许可和指定变量见BACKEND_W1.md/DEPENDENCIES.md。BC/Java、便携WinLibs仅为测试依赖，不打入运行包。

原MinGW环境失败和新工具链路径探测记录都保留。用户授权的WinLibs为隔离便携目录，仅测试进程设置CC/PATH；英文目录别名只解决GCC中文路径问题，不替换系统编译器。race通过只表示执行覆盖到的路径未报告数据竞争，不是逻辑正确性或安全证明。

## 当前正式数据与持续负载

具体矩阵、分母、测量边界、0ns、失败处理、30分钟负载见BACKEND_FORMAL_EXPERIMENT_PLAN.md。`backend_formal_run.py`须使用已通过回归/各模式10次smoke的最终freeze.json；生产完整验收器核验后，`backend_formal_report.py`从raw重算。原M5/M6资料保持原hash，历史formal契约ID不代表旧采样。

`backend_live_acceptance.py --exe 当前EXE --out 新目录`默认1800秒，20台真实端点，有计划断连、网关维护、时间序列、停止与再次启动。该负载与benchmark分开执行，不能用其累计CPU代替单次握手指标。无新测量的指标保持null+原因。任务/日志/导出配额保持原限制，清理只针对本次进程。

## 环境与发布限制

Windows独立解压副本在中文空格路径、仅Windows系统目录PATH中验收；这证明运行不依赖开发工具PATH，不能替代物理断网实测。没有授权关闭网卡，物理断网保留NOT_EXECUTED。无新增代码签名、密码合规认证、跨机器独立复核、Linux资源实测或真人易用性结果。页面改版与新手体验优化等待用户下一轮方案。

最终状态、原题覆盖、当前与历史证据索引以发布包BACKEND_COMPLETION.md和BACKEND_READINESS.json为准。被核验ZIP回执放ZIP外，包内manifest不引用自身。
