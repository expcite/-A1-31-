# 摩托罗拉 X30 / Android 14 手机接入验证

目标：Windows 电脑开移动热点，Android 手机作为独立终端，通过现有 Suite 2（SM2 + ML-KEM-768、HKDF-SM3、SM4-GCM）上报电池温度，接收并执行上报间隔控制，再返回安全 ACK。手机可在控制生效后调用一次震动提示；API 返回不等于对马达动作的独立观测。

当前已确认：用户手机 uname -m 为 aarch64，Termux 电池 API 实际返回 temperature=33.0。电脑热点已观测到 192.168.137.1。尚待完成：Android 程序实际启动、跨热点握手、真实上报、远程控制和震动实测。

## 启动顺序

1. 电脑开启移动热点，手机连接该热点。
2. 打开生成目录中的“01_启动手机验证.cmd”。浏览器只在电脑的 127.0.0.1 提供管理页面；手机连接的是 192.168.137.1:9443 加密业务端口。
3. 把启动窗口的一次口令填入电脑网页，选择 D001。无需点击“连接当前设备”，等待手机主动握手。
4. 第一次使用时，右键“02_允许手机连接.cmd”，以管理员身份运行。脚本只允许本验证程序的热点地址、TCP 9443 和本地子网入站。
5. 使用 USB 文件传输，把 EdgeHybrid_Phone_v27.zip 放到手机 Download 文件夹。本包包含本次演示设备的独立私钥，保留在电脑和手机，不上传 GitHub 或作为公开竞赛附件。

手机 Termux 依次执行：

```bash
termux-setup-storage
pkg install unzip
mkdir -p ~/edgehybrid-phone27
unzip ~/storage/downloads/EdgeHybrid_Phone_v27.zip -d ~/edgehybrid-phone27
cd ~/edgehybrid-phone27
bash run-phone.sh
```

存储授权时允许 Termux 访问文件。程序在 Termux 自有目录运行，不直接在 Download 或共享存储运行。run-phone.sh 会设置该演示目录和私钥的访问权限。已有同名安装目录时，先核对是否为同一份配对包，不覆盖其他配对身份。

若电脑热点地址不是 192.168.137.1，电脑启动脚本可用参数 -HotspotIP 指定实际地址；手机执行 `bash run-phone.sh 实际地址:9443`。不要把业务地址改成手机自己的 IP。

## 验收一条真实链路

1. 手机出现 authenticated，电脑 D001 显示在线且套件为混合安全通道。
2. 电脑显示手机的电池温度及最近接收时间；这是电池温度，不是环境温度。采集失败不会生成零值或模拟温度。断连或长时间无新数据时保留并标记上次观测。
3. 电脑“设置间隔”申请 5 秒，观察手机 interval_applied_ack_written 与同一操作 ID 的电脑确认回执。手机支持 1～60 秒。
4. 手机按新间隔继续采集。调用电池 API 的耗时与系统调度会叠加到报告间隔中，不把该界面值当成严格实时周期。
5. 若震动 API 成功返回，可现场观察手机是否震动。回执只确认上报间隔生效；震动调用单独记录。

默认一次运行10分钟，手机 Ctrl+C可结束，重新启动会进行完整新握手，不自动重发旧控制。当前手机版不通过本机设备队列执行手动模拟温度、暂停模拟采样或网页换钥；需要新会话时重启手机程序。原正式实验与原报告保持原批次，本次软件联调不冒充手机实测或新的性能样本。

## 构建

沿用已验证的 Go 1.27.2 与 go.mod/go.sum 固定依赖，执行：

```powershell
python -X utf8 -B scripts/build_phone_demo.py --out <不存在的输出目录>
```

构建两个独立程序：Windows/amd64 的 EdgeHybridPhoneGateway.exe 与 Android/arm64 的 edgehybrid-phone，均关闭 cgo。手机安装 Termux、同来源的 Termux:API 应用及 termux-api 命令行包；该机的命令行包已观测为0.60.0。构建成功仅证明目标文件可生成，实机验收另外记录。
