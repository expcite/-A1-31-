# Android 实机验证终端

本终端用于验证独立物理设备通过局域网接入 EdgeHybrid。已验证设备为摩托罗拉 X30、Android 14、aarch64，Termux 电池 API 返回真实电池温度。设备使用 Suite 2 的混合握手和安全记录通道，上报温度并接收上报间隔控制。该结果不代表其他机型已经适配，也不替代既有性能试验。

2026年10月19日的已有实机验收通过：手机通过电脑热点连接，温度持续上报，10秒间隔控制收到安全ACK；用户在1.7.2更新后确认每次设置均能亮灯。震动仅观察到API返回，未观察到物理动作，最终演示改用手电筒。1.8.0完整包已在独立手机目录通过新配对连接、温度上报和控制确认，用户确认重复设置均亮灯；演示视频已补齐，公开副本去除拍摄元数据，见[演示视频](../demo/README.md)；约第48秒可见一次亮灯并熄灭，重复设置均亮灯另有独立验收观察。初选9444端口受Windows保留范围阻止，现已改用经实际绑定检查的10443，并增加启动前检查。新的发布重建身份与实测身份分别记录，不将已有验收改标为另一个构建实测。

## 首次部署

电脑运行“00_启动电脑演示.cmd”即可使用完整本机模拟演示。手机验证为可选入口，不要求评委持有手机才能运行核心流程。

手机依赖 Termux、同一安装来源的 Termux:API 安卓应用，以及命令行 termux-api 和 unzip。首次安装允许联网；安装后演示通过电脑移动热点运行，不调用外网服务。官方项目入口：https://github.com/termux/termux-app 和 https://github.com/termux/termux-api 。手机命令行API包已观测为0.60.0。

1. 电脑开启移动热点，手机连接热点。默认地址192.168.137.1，新的验证入口使用TCP 10443，避免与既有9443演示冲突。
2. 电脑运行“01_生成本机手机配对包.cmd”。它创建全新的本机身份和可首次安装的手机ZIP，不依赖旧手机目录。生成的local-phone目录含本机部署私钥，不放入公开交付或GitHub。
3. 右键“02_允许手机连接.cmd”以管理员身份运行，只添加本程序、热点地址和TCP 10443的本地子网入站规则；不关闭防火墙。
4. 运行“03_启动手机验证.cmd”。电脑网页仍只监听127.0.0.1，用新启动窗口的一次口令登录，不访问旧网页。
5. 将local-phone/EdgeHybrid_Phone_Fresh_v28.zip复制到手机Download。

先在手机旧程序中按CTRL+C退出；保留旧目录不变。Termux执行：

```bash
termux-setup-storage
pkg install termux-api unzip
mkdir ~/edgehybrid-phone28-clean && unzip ~/storage/downloads/EdgeHybrid_Phone_Fresh_v28.zip -d ~/edgehybrid-phone28-clean && cd ~/edgehybrid-phone28-clean && bash run-phone.sh 192.168.137.1:10443
```

mkdir若提示目录已存在，停止操作并核对已有内容，不覆盖旧部署。程序必须在Termux自有目录运行。配置和配对由电脑生成，不能把一套手机私钥接到另一套网关身份。

若热点地址不同，在电脑PowerShell依次运行 `powershell -NoProfile -ExecutionPolicy Bypass -File .\prepare_phone.ps1 -HotspotIP 实际IP`、对应的allow_phone.ps1和start_phone.ps1并传相同参数；已生成配对后修改地址时应另行生成独立目录，不覆盖既有配对。手机可用 `bash run-phone.sh 实际IP:10443` 覆盖连接地址。

## 验收和视频

手机build_identity应显示interactive-v1.8.0-phone28；随后authenticated，电脑D001在线。温度来自电池传感器，不是环境温度。连续看到至少3笔新温度记录后，依次设置10秒、5秒、10秒，两次操作至少间隔6秒。每次点击“设置上报间隔”后检查新操作的安全ACK，再观察手机亮灯约1秒并熄灭。不要把选中下拉框当成已提交。

闪灯只由新的有效间隔控制触发，不是每次温度上报都会闪。程序串行执行off、等1秒、on、等1秒、off、等2秒。最多8个待处理提示；队列已满会单独记录提示失败。间隔ACK确认设置生效，torch_api_returned只表示调用结束，物理亮灯由现场观察确认。

借另一部手机录制约一分钟，使电脑和被测手机背面同时入镜。展示新版身份、在线状态、持续温度、三次控制的确认与对应灯光。拍摄时只保留作品界面和被测设备；遵循匿名评审模板，不出现单位、教师或团队成员姓名。视频是否上传按赛事平台的实际要求处理。

手机默认每次运行10分钟，CTRL+C退出；重新启动进行新握手。原始JSON日志保存在Termux目录phone-events-*.jsonl，建议和电脑日志一起导出用于对应SID和操作ID。两端时钟不同，不将时间戳直接相减作为网络时延。

## 构建

Go 1.27.2，Windows/amd64网关与Android/arm64设备，CGO关闭，依赖由go.mod/go.sum固定。先安装所需模块缓存，再运行：

```powershell
python -X utf8 -B scripts/build_phone_release.py --out <不存在的公开软件目录> --evidence <不存在的构建日志目录>
```

公开包不含预置私钥，使用时自行生成配对。既有原实验值、计时边界和批次继续按原始记录解释；本次现场功能验证没有产生跨主机性能统计、更多机型或长时间稳定性结论。

## 日常启动与已补充材料

已经配对好的部署无需重新运行01或解压手机ZIP。电脑开热点、手机连接热点后，运行原部署目录的03_启动手机验证.cmd，并使用本次网页和口令。手机执行一整行：

```bash
cd ~/edgehybrid-phone28-clean && bash run-phone.sh 192.168.137.1:10443
```

手机默认10分钟后退出，需要时重跑同一命令。新设置触发约1秒闪灯，温度上报不触发。先在手机CTRL+C，再在电脑网关CTRL+C结束；关闭网页不会停止网关。重复启动前先检查原网关窗口，避免占用10443。

三张原始网页截图、报告裁剪图和视频核对记录见verification/phone_v28/media_completion/。源码已公开至https://github.com/expcite/-A1-31-，仍按仓库现有授权声明提供。
