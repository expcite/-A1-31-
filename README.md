# EdgeHybrid 软件源码

当前公开运行版本为 `interactive-v1.6.3-ui24`，保持既有协议及业务机制，使用相同 Go 1.27.2 工具链重新构建，并对全部 8 个程序启用路径裁剪。源码中嵌入的历史数据仅对本机环境路径作文本脱敏，历史测量值不改写。

## 构建与测试

需要本机 Go 及模块缓存；依赖版本由 go.mod/go.sum 固定。推荐设置 GOTOOLCHAIN=local、GOPROXY=off，避免在复核时自动换工具链或下载模块。

```powershell
go test ./... -count=1
python -X utf8 -B scripts/backend_build.py --out <不存在的输出目录> --evidence <不存在的构建日志目录> --version interactive-v1.6.3-ui24
```

构建脚本从 PATH 查找 Go，或使用环境变量 GO 指定可执行文件；没有某个开发工具专有目录的依赖。其余历史采集脚本可能需要按各脚本参数指定本机路径，当前交付入口及版本以包根 README 和 runtime/build_info.json 为准。

原题、协议、威胁模型和接口说明在 docs/。密码原语、握手、记录和业务实现在 internal/，运行入口在 cmd/。原始测量位于包根 experiments/，不是本轮重建后重新采样。

开发提示词、阶段交接材料、旧编译产物和重复报告不作为当前源码交付。文件保留范围及公开文本身份见包根 verification/PUBLIC_COPY_MAP.json。

## 详情界面

右侧抽屉采用中文摘要、容量卡片和操作时间线；关联编号与公开原始记录按需展开。缺失值、合法零值和状态过期分别显示，刷新保留展开状态。布局沿用工作台的颜色、卡片和明暗主题。

可用 Node 运行 scripts/detail_presenter_test.mjs --assets internal/console/assets，检查缺失值、容量上限、零值、过期状态及操作进度的展示规则。

## GitHub 当前提交

当前默认分支同步竞赛提交v26，软件版本为interactive-v1.6.3-ui24。

- [最新作品设计报告](report/作品设计报告.pdf)
- [可编辑报告](report/作品设计报告_可编辑.html)
- [上传范围与验证说明](GITHUB_UPLOAD.md)
- [历史验收记录](verification/submission_v26/)

源码位于本仓库根目录。运行程序和完整实验原件随本地支撑材料ZIP交付；本仓库保留对应身份和验收摘要。Git文本文件统一LF换行，原构建源码清单仅标识本地交付原件，不能直接冒充本仓库的新构建记录。旧版本可在Git历史中查阅。
