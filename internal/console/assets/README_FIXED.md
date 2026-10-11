# EdgeHybrid ReferenceUI 修复与改进版本说明

本目录收录了针对 `EdgeHybrid_ReferenceUI_Windows`（v1.5-minimalui1 / reference-ui-20261005）依照《EdgeHybrid 完整功能验证与发布验收流程》（PDF 39页）审计后修复的完整前端资产。

## 1. 修复文件与位置
本目录所有文件位于：
`C:\Users\20477\Downloads\fixed_reference_ui\`

包含全部完整运行所需的前端 ESM 资源：
- `index.html`：主结构入口，语义化与 a11y 骨架
- `style.css`：原生 CSS Design Tokens 与响应式样式表
- `app.js`：应用顶层控制器、路由挂载、轮询刷新与全局运行菜单
- `workbench.js`：通信工作台（双向收发、预设间隔语义明确化、操作回执）
- `logs.js`：记录与筛选（修复空事件 409 导出 Bug、默认展开 F07 阶段筛选器、清理空字符串参数）
- `devices.js`：20台设备连接、搜索、失败优先批量回执折叠
- `keys.js`：设备与网关身份管理、迁移策略、二次确认
- `jobs.js`：安全实验（M6与基准任务互斥前置保护、任务取消）
- `help.js`：帮助与版本动态读取
- `drawer.js`：持久化详情抽屉（轮询不关闭）
- `guide.js`：四步真实运行演示引导控制器
- `batch.js`：批量结果摘要格式化器
- `router.js`：Hash 路由映射
- `store.js`：ConsoleStore 状态机与 3.5s 新鲜度守卫
- `api.js`：API 统一门面、CSRF/Origin 守卫与同目标提交防重
- `confirm.js`：原生 `<dialog>` 精确文本回填确认
- `usability.js`：边界校验器（0.1~60s 整数毫秒转换、-50~150℃ 数值限制）
- `components.js`：通用组件与脱敏过滤器（`publicDetails`）
- `performance.js`：历史 M5/M6 测量结果与证据声明

---

## 2. 关键修复细节与对应的 PDF 验收阶段

### ① 阶段 13-05 / 13-07：修复空事件范围导出触发后端 409 Conflict 错误
- **缺陷现象**：在极端边界情况（如刚启动尚未产生任何事件或历史事件被滚动清空时），原代码中 `from_event_id: snapshot.events[0]?.event_id ?? 0` 会传入 `0`。而后端严格校验 `from_event_id >= min_event_id`，导致后端返回 `409: 所选范围事件已从内存移除，请刷新范围`。
- **修复方案**：`logs.js` 中使用 `(snapshot.events && snapshot.events.length > 0 && snapshot.events[0].event_id > 0) ? snapshot.events[0].event_id : (snapshot.cursor || 1)`，确保起始范围合法。
- **附加清理**：过滤掉值为 `""`（空字符串）的筛选器键，保证切回“全部”与初次进入未选择时的请求 payload 严格一致。

### ② 阶段 13-02 / 04-06：外显 F07 动态刷新选项（提升评测直观度）
- **优化点**：原代码将“观察端”、“阶段/状态”全部收纳在闭合的 `<details>` 内部，导致评测人员一眼看过去以为没有实现阶段筛选。
- **修复方案**：在 `logs.js` 中将筛选折叠栏默认展开（`advancedFilters.open = true`），并注明“筛选选项（F07动态更新）”，便于直接观察和操作。

### ③ 阶段 05-02 / 05-04：明确“申请待下发值”与“当前生效值”的视觉区分
- **优化点**：原代码在控制卡片下拉框中默认写死为“每 5 秒上报一次”，容易让用户误以为设备当前生效的值就是 5 秒。
- **修复方案**：在 `workbench.js` 中明确标签为“申请下发上报间隔（待下发）”，选项注明“申请每 N 秒上报一次”，杜绝认知混淆。

---

## 3. 如何同步到源码并重新构建（若需要发布新 EXE）

若需要将这些修好的文件编译进新的 Windows 原生可执行文件（`EdgeHybridInteractive.exe`）：
1. 将本目录（`C:\Users\20477\Downloads\fixed_reference_ui\`）中的 `.js`, `.html`, `.css` 复制覆盖到源码目录：
   `source/edgehybrid-agent-starter/internal/console/assets/`
2. 运行构建脚本：
   ```powershell
   python scripts/backend_build.py --out <输出目录> --version interactive-v1.5-minimalui1-fixed
   ```
   Go 的 `embed.FS` 会自动将最新的资产重新打包入二进制 EXE 中。
