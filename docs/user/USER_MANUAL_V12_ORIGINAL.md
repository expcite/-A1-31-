# EdgeHybrid 交互版完整手册
interactive-v1.2 · 2026-10-02 · Windows amd64
源码清单 SHA-256：0febefe557f00534d93aa6b40b89f2a19fd5e010004069827ee4067aa4242fe8
本机模拟端点，通过现有真实SM2/ML-KEM建钥与SM4-GCM Channel通信。不是密码合规认证或安全证明。当前版本与旧只读/自动有限Demo分开；旧M5/M6测量版本保持冻结。
## 新手操作
完整解压，启动1设备.cmd；输入启动窗口当次一次口令授权。开始使用连接D001，实际在线后进入收发数据。手动输入26.3℃或28.7℃并发送，查看按钮旁“网关已收到”。间隔输入秒，1/5/10秒预设或0.1～60秒自定义，最多三位小数；后端只接受100～60000整数毫秒，界面不截断/舍入。
“当前生效”来自设备真实处理，申请值要等ACK。暂停采样保持连接，不需要新遥测就能设周期。开启采样后真实timer按新间隔运行。手动温度与自动模拟源各自输入；最近接收值可能被自动源更新，独立手动回执及时间保留。
![截图](results/raw/interactive_v12_20261002/screenshots/workbench_confirmed.jpg)
截图为实际运行页面；run与版本关联见UI_EVIDENCE.json。操作结果紧邻按钮，草稿/焦点不被轮询重建；刷新、切页只读取同一运行，不自动提交或重试。
---
## 设备、模式与网关维护
简洁模式：开始使用 / 收发数据 / 设备 / 记录 / 帮助。高级功能展开总览、密钥与迁移、安全实验室、性能与来源；数据和权限完全同源。默认1台；启动20设备.cmd明确选择20台本机模拟端点，非20台硬件。
设备页逐设备连接、断开，高级详情可看身份/SID/世代/套件和完整换钥。批量20项全部尝试，partial不是全失败，accepted不是握手成功。每台独立TCP和Channel。换钥执行新H1～H4，旧请求保留旧SID及状态，不重发到新会话。
高级“密钥与迁移”：设备动作仅目标设备；网关独立维护影响全部设备，须owner授权和输入action:GW001:ALL。网关轮换先准备分离签名/协商钥，关闭准入、关闭全部旧连接及排队请求、CAS提交、加载校验新世代，成功后才开放明确重连。信任来自受控本机注册表，不接受网关自报未知新公钥。
准备/写入/提交失败：旧可信世代保持；已断会话需明确重新连接。排空超时：未提交、准入保持关闭。提交后加载失败：准入关闭报错，不回滚旧钥；修复后受控维护再验证。清理失败：pending，不回滚提交，可明确重试清理。吊销保留材料供调查并拒绝所有相关会话；退役删除目标密钥文件，重启仍拒绝。文件删除不是物理擦除，不能自动恢复吊销身份。
![截图](results/raw/interactive_v12_20261002/screenshots/gateway_rotated.jpg)
不得与secctl/其他进程同时修改本实例trust。底层CAS只承诺同Registry实例单写者，不宣称跨进程文件锁；本机模拟不等于远程PKI分发。
---
## 记录、实验与历史结果
记录页：最近2000事件的有界内存，公共文件最多20MiB。按设备、观察端、阶段筛选，每页20条。固定cursor范围不随轮询跳页，点更新记录范围再纳入新事件；显示事件编号范围和已省略数量。导出匹配当前固定范围的全部筛选事件，JSON/HTML包含selection，CSV的selection_json列及创建回执说明范围。不是无限历史数据库。
默认导出删除温度与详细载荷，明确勾选才包含模拟温度。下载受同一owner授权，摘要显示实际SHA-256。过滤范围已移出内存会拒绝并提示更新；旧公共文件保留。导出16份/4MiB上限，禁止任意路径读取。
安全实验室仅现有allowlist本机隔离任务，需先断开在线设备；M6规则来自隔离测试构建，bench/pbench有限批次有真实退出码及完整验收器，取消也保留非零记录。生产程序没有跳过认证开关。最多8任务，不把排队或子进程启动当完成。
性能与来源优先展示原M5冻结结果、平台、run、源码和测量口径。当前交互版没有新正式1000次性能数据；保留0ns观测及计时局限、失败分母、握手/业务成功率区别。1000/1000不能推成未来100%；批次CPU/峰值内存不是单次握手；P与T/H整体差异不能全部归因ML-KEM。
帮助显示实际版本、源码hash、运行、容量及安全边界。所有页面的数据来自现有后端；高级只改变呈现，不改变授权。
---
## 后端CLI与API
用户版无Go/Python/Node依赖，无CDN。命令行等价入口：EdgeHybridInteractive.exe -devices 1；-devices 20；-no-browser可只打印本机页面地址；-stop只作用于本包控制事件。重复同包实例拒绝，停止再启动产生新run与会话，不重写已有长期身份。
程序只监听127.0.0.1；端口以启动窗口PAGE_URL为准。本机授权POST /api/v1/login，JSON code为启动窗口当次口令；消费一次码后owner Cookie有效1小时，HttpOnly/SameSiteStrict；GET session给CSRF/到期时间。写入须精确Origin/Host、Cookie和X-CSRF-Token；不把这些值放URL、公开日志或命令历史。不自动续期。
GET state /devices /events /build /operations/<id>：只读当前运行。POST devices/D001/connect、disconnect、rekey；telemetry={request_id,temperature_c}；sampling={auto,temperature_c}；gateway/commands={device_id,request_id,interval_ms}。采样开关是本机设备actor动作；温度、控制和ACK是真实Channel。HTTP202只排队。
POST identities/<id>/rotate、revoke、retire、policy：设备confirm=action:id，policy再带mode=traditional或hybrid；网关rotate/revoke/retire/cleanup须confirm=action:GW001:ALL。网关结果分别给commit、会话关闭、加载/采用、准入开放与cleanup_pending。
POST exports={format,include_payload,selection?}；selection={run_id,from_event_id,to_event_id,device_id?,endpoint?,stage?}。包含载荷须confirm=include_simulated_payload；GET exports/<id>下载。POST run/start返回逐设备准入；run/stop断开；run/exit须confirm=exit:<run_id>，返回stopping后真实退出。POST jobs与cancel使用固定kind；不存在任意Shell入口。
严格JSON拒绝重复/未知字段及超过8KiB请求；401授权/过期/CSRF，403来源，409离线/冲突/容量/CAS，429速率。机器契约docs/INTERACTIVE_OPENAPI.json，本次本地引用均已核验。
---
## 构建、测试与复现
完整源码审查包Source保留cmd/internal、全部测试、公开testdata、嵌入assets页面/组件/样式/API、go.mod/go.sum、脚本、配置模板和交接。前端是原生ES模块，没有npm依赖清单或lock，未制造空目录。cmd只作入口。
开发者在Source根执行：python scripts/interactive_build.py --out <新输出目录> --evidence <新构建记录目录>；Go1.27.1本轮实际版本。脚本写339项源码清单、四个新exe摘要、ldflags版本/来源，scenario.exe为隔离测试构建仅实验任务使用。用户不得将测试exe替代正常通信程序。
全量：go test -count=1 -json ./...；用scripts/run_checked.py保存stdout/stderr/command.json/exit.txt，并调用summarize-go-test-json.py带真实Go退出码及完整expected-package集合。Node scripts/interactive_frontend_test.mjs；Python汇总、M4CASE、fleet、bench和M6验收器自测。历史M6自测输出须--out新路径，禁止覆盖冻结raw。
实际EXE复现：interactive_installed_acceptance.py --exe <本次exe> --out <新目录> --devices 1/20；interactive_bad_device_acceptance.py分别--bad D001/D010/D020；interactive_gateway_acceptance.py --devices 1/20，检查新网关双端实际世代、轮换、旧会话、吊销/退役重启。每套在独立解压目录/身份池运行，不吊销用户正在使用的身份。
持续测试显式EDGEHYBRID_INTERACTIVE_LONG=1，go test -count=1 -json -timeout=12m ./internal/live -run ^TestContinuousTenMinutes$。本轮独立执行约10分钟并静置后主动控制，不是正式性能测量。旧M5/M6只按原hash和来源复算，不重采1000次，不回填新代码结果。
历史验收器自测的完整复现另需来源索引指向的原M5/M6证据；当前源码包不嵌套历史ZIP，也不复制无关旧日志。当前Go/前端和正常隔离规则不依赖这些旧结果。
本轮原始记录根results/raw/interactive_v12_20261002；所有实际命令和退出码逐目录保留，失败/候选也保留。构建、发布源码、最终EXE和截图关联发布清单；首次持续命令因Go默认10分钟超时而失败，记录保留；最终持续命令显式12分钟预算，来自本发布源码。候选与发布差异见release_association/shipping_association，失败不改写为成功。
---
## 故障处理与真实边界
等待发送 → 正在写出 → 已发出等待确认 → 已确认，是不同事实。控制收到验证ACK才称执行确认；未确认/ACK超时/写入失败可能已执行，不自动重发；迟到确认显示实际迟到。失败或取消终态不会因重连进入writing。
队列32项满：等待当前任务完成。累计256操作满：先导出，停止并重启新run；换钥不会清空。授权1小时过期：停止重启取得新码，不删除身份/密钥。不使用浏览器清Cookie来重置已消费口令。实验8/导出16达到上限先保存，再停止重启。日志20MiB满按既定策略停止并保留文件。
失去管理服务：当前状态未知、禁写；检查启动窗口与端口，等待停止完成再启动。暂停采样保持会话；断开结束设备；退出关闭软件，关闭浏览器不退出。吊销/退役不可通过重启恢复；新环境初始化只在trust缺失时执行，不覆盖既有身份。
生产保护保持：完整SM2交换、两份共享结果混合KDF、认证与Finished、SM4-GCM、严格发送顺序/接收验证、抗重放、防静默降级，一个连接唯一Receive分发者，有界队列与日志。算法组件未重写。新API不拼Shell，不绕CAS，不预置私钥或访问令牌。
没有真人参与本轮试用，满意度/上手时间未执行；自动化浏览器不能代替真人。race与独立SM2交换仍pending；OpenSSL历史签名测试SKIP；Windows系统DPI/跨机硬件/物理断网未验证，浏览器尺寸/缩放以UI_EVIDENCE实际记录为准。文件权限Windows ACL历史限制保留，不称私钥物理安全擦除。
本轮交付后停止；不新增聊天、文件、云账号或LLM。
