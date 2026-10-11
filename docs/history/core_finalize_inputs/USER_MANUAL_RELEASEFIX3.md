# 当前使用手册入口

版本 interactive-v1.2-backend2-status1-releasefix。完整原 V12 操作说明见 [原手册](USER_MANUAL_V12_ORIGINAL.md)；原文件原字节保留，下述补充用于当前版本。

启动1设备.cmd / 启动20设备.cmd 显示本机一次口令，输入网页授权表单后连接设备。手动温度和主动采样周期使用真实 TCP 安全 Channel；HTTP 接受不代表对端已确认。暂停采样、断开设备与退出软件含义不同。

## 记录与导出

点击“记录”进入固定快照。轮询不改变当前事件范围或页码；点击“更新记录范围”才采用新完整快照。设备、观察端和阶段选项与表格、分页、导出使用同一快照。仍有效的选择保留；已离开内存窗口的选择保留并显示“当前范围无匹配”，匹配0条，不会静默导出全部。获取失败或不同run不替换当前范围。

JSON/CSV/HTML导出当前筛选的整个固定范围，不限于可见20行，默认去除温度载荷。显式勾选才包含模拟温度。内存最多2000事件、公共文件最多20MiB；有界日志不是无限历史检索。

## 版本和验证范围

八个页面、权限、CSRF、Host/Origin、协议与后端业务保持原实现。仅 logs.js 的选项刷新逻辑获授权改变，布局/样式/导航冻结。
secctl status 只描述能力和构建元数据，不执行安全检测。历史 Windows race、14组SM2核心独立对照及OpenSSL对照保留原来源；标准 SM2 S1/S2 独立确认未执行。旧性能属于旧测量版本，0ns与计时限制保留，批次CPU/峰值不是单次指标，P整体差异不全部归因ML-KEM。
M6两组覆盖率及已知缺陷检出相同，改善的是可审计信息完整度。

## 构建与独立复查

源码项目根执行 go mod verify、go vet ./...、go test -count=1 -json ./...。python scripts/backend_build.py --out 新目录 --evidence 新日志目录 会关联源码清单和8个EXE。
审查包根的 RELEASE_INDEX.json 指向完整历史证据；python source/edgehybrid-agent-starter/scripts/release_verify.py --root 包根 --out 新输出目录 --go go 执行清单、构建、32批旧结果验收及M6离线重算，不重新采样。
后端API见 [接口说明](../INTERACTIVE_API.md)。用户运行无需Go/Python/Node/外网CDN，复核开发测试需要明确列出的工具。
