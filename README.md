# EdgeHybrid 软件源码

本轮交付版本为 `interactive-v1.8.0-phone28`，默认保留纯电脑演示，另提供可选 Android 实机验证终端；沿用 Go 1.27.2 和固定模块依赖。源码中嵌入的历史数据仅对本机环境路径作文本脱敏，历史测量值不改写。

## 构建与测试

需要本机 Go 及模块缓存；依赖版本由 go.mod/go.sum 固定。推荐设置 GOTOOLCHAIN=local、GOPROXY=off，避免在复核时自动换工具链或下载模块。

```powershell
go test ./... -count=1
python -X utf8 -B scripts/backend_build.py --out <不存在的输出目录> --evidence <不存在的构建日志目录> --version interactive-v1.8.0-phone28
```

构建脚本从 PATH 查找 Go，或使用环境变量 GO 指定可执行文件；没有某个开发工具专有目录的依赖。其余历史采集脚本可能需要按各脚本参数指定本机路径，当前交付入口及版本以包根 README 和 runtime/build_info.json 为准。

原题、协议、威胁模型和接口说明在 docs/。密码原语、握手、记录和业务实现在 internal/，运行入口在 cmd/。原始测量位于包根 experiments/，不是本轮重建后重新采样。

开发提示词、阶段交接材料、旧编译产物和重复报告不作为当前源码交付。文件保留范围及公开文本身份见包根 verification/PUBLIC_COPY_MAP.json。

## 详情界面

右侧抽屉采用中文摘要、容量卡片和操作时间线；关联编号与公开原始记录按需展开。缺失值、合法零值和状态过期分别显示，刷新保留展开状态。布局沿用工作台的颜色、卡片和明暗主题。

可用 Node 运行 scripts/detail_presenter_test.mjs --assets internal/console/assets，检查缺失值、容量上限、零值、过期状态及操作进度的展示规则。

## GitHub 当前提交

历史正式性能材料对应v26及interactive-v1.6.3-ui24。本轮手机功能、构建和独立部署验收分别记录，不把旧性能值改标为新程序实测。

- [最新作品设计报告](report/作品设计报告.pdf)
- [可编辑报告](report/作品设计报告_可编辑.html)
- [上传范围与验证说明](GITHUB_UPLOAD.md)
- [历史验收记录](verification/submission_v26/)

源码位于本仓库根目录。运行程序和完整实验原件随本地支撑材料ZIP交付；本仓库保留对应身份和验收摘要。Git文本文件统一LF换行，原构建源码清单仅标识本地交付原件，不能直接冒充本仓库的新构建记录。旧版本可在Git历史中查阅。

## 可选手机实机入口

[手机端部署与验收](docs/PHONE_DEMO.md)说明依赖安装、生成独立配对、首次完整部署和录像步骤。构建完整公开软件包使用`scripts/build_phone_release.py`；公开包没有预置私钥，使用者首次部署时生成。独立目录验收已通过，演示视频与四幅报告实机图已补齐，见[视频说明](demo/README.md)和[补充核对记录](verification/phone_v28/media_completion/)。

## 代码公开与演示材料

源码已公开至[GitHub仓库](https://github.com/expcite/-A1-31-)。保持现有[授权声明](LICENSE.txt)，未新增开源许可证。

- [手机实机演示视频](demo/IMG_1445.MP4)：约51.72秒，约第48秒可见手电筒亮起后熄灭。公开副本去除拍摄元数据。
- [视频说明与公开副本哈希](demo/README.md)。
- [四幅报告实机图及原始核验记录](verification/phone_v28/media_completion/)。
- 当前报告54页，补充温度上报、控制确认、Android日志与手电筒亮灯实况及仓库入口。

本次仅补充文档与媒体，通信实现、已验收二进制和历史测量值不变。交付包source/与源码ZIP保留构建输入快照；最新使用说明见本仓库及交付包根README。旧验收文件保留其当时状态，当前视频状态见MEDIA_COMPLETION.json。

2026年10月20日报告修订：围绕研究问题、设计实现、实验结果和应用方向组织正文，突出混合建钥、业务确认与实机验证成果。实验表数值、参考文献、运行程序及演示视频保持对应版本。

2026年10月20日交付对齐：报告54页，90处目录页码已核对；四幅实机图完整保留，分页与交付文件统一。
