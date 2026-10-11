# 当前使用手册入口

版本 interactive-v1.2-backend2-status1-releasefix3-finalize2。完整原 V12 操作说明见 [原手册](USER_MANUAL_V12_ORIGINAL.md)；原文件原字节保留，下述补充用于当前版本。本文为冻结前输入说明；当前执行结果以审查包根 CURRENT_DELIVERY_INDEX.json、reports/ 和匹配包外回执为准，不预先宣称通过。

启动1设备.cmd / 启动20设备.cmd 显示本机一次口令，输入网页授权表单后连接设备。手动温度和主动采样周期使用真实 TCP 安全 Channel；HTTP 接受不代表对端已确认。暂停采样、断开设备与退出软件含义不同。

## 记录与导出

点击“记录”进入固定快照。轮询不改变当前事件范围或页码；点击“更新记录范围”才采用新完整快照。设备、观察端和阶段选项与表格、分页、导出使用同一快照。仍有效的选择保留；已离开内存窗口的选择保留并显示“当前范围无匹配”，匹配0条，不会静默导出全部。获取失败或不同run不替换当前范围。

JSON/CSV/HTML导出当前筛选的整个固定范围，不限于可见20行，默认去除温度载荷。显式勾选才包含模拟温度。内存最多2000事件、公共文件最多20MiB；有界日志不是无限历史检索。

## 版本和验证范围

八个页面、权限、CSRF、Host/Origin、协议与后端业务保持原实现。logs.js 的选项刷新修复属于已提供基线；本轮所有前端源文件均冻结，不修改布局/样式/导航。
secctl status 只描述能力和构建元数据，不执行安全检测。历史 Windows race、14组SM2核心独立对照及OpenSSL对照保留原来源；当前验证与性能实验分别列平台、实际测试源码及证据范围。标准 SM2 S1/S2 独立确认未执行，BC核心交换不替代它。旧性能属于旧测量版本，0ns与计时限制保留，批次CPU/峰值不是单次指标，P整体差异不全部归因ML-KEM。
历史M6两组覆盖率及已知缺陷检出相同，改善的是可审计信息完整度；当前M6按新冻结基线实际执行后单独报告，不预写优势。

## 构建与独立复查

源码项目根执行 go mod verify、go vet ./...、go test -count=1 -json ./...。python scripts/backend_build.py --out 新目录 --evidence 新日志目录 会关联源码清单和8个EXE。
当前审查包根 CURRENT_DELIVERY_INDEX.json 指向当前16批、历史32批、当前及历史M6、原始源码清单和同时交付的Windows ZIP。执行 python source/edgehybrid-agent-starter/scripts/core_release_verify.py --root 审查包根 --out 新外置输出目录 --go 明确Go路径 --software-archive 同时交付的WindowsZIP，进行独立构建、必要回归和既有结果验收/重算，不重新采样。旧 RELEASE_INDEX.json 与 release_verify.py 仅用于随包历史子树，不替代当前发布门槛。缺文件、错hash、缺Windows ZIP会阻止当前完整验收。
后端API见 [接口说明](../INTERACTIVE_API.md)。用户运行无需Go/Python/Node/外网CDN，复核开发测试需要明确列出的工具。
