"""Recompute editable chart inputs from M5 raw, then write traceable M6 materials."""
import hashlib,json,math,statistics
from pathlib import Path
ROOT=Path(__file__).resolve().parents[1];OUT=ROOT/'results/raw/m6_20261001';CMP=OUT/'comparison_v2'
def read(p):return json.loads(p.read_text(encoding='utf-8'))
def write(p,x):p.write_text(json.dumps(x,ensure_ascii=False,indent=2)+'\n',encoding='utf-8')
def text(name,s):(ROOT/'docs'/name).write_text(s.strip()+'\n',encoding='utf-8')
def sha(p):return hashlib.sha256(p.read_bytes()).hexdigest()
def relocate(s):
 tail=s.replace('\\','/').split('/results/raw/m5_formal_20261001/',1)[1]
 return ROOT/'results/raw/m5_formal_20261001'/tail
def main():
 c=read(CMP/'comparison.json');f=read(OUT/'freeze/freeze.json');m5=ROOT/'results/raw/m5_formal_20261001'
 chart=dict(m5_measurement_source_sha256=read(m5/'freeze/freeze.json')['source_manifest_sha256'],m6_measurement_source_sha256=c['measurement_source_manifest_sha256'],m6_run_id=c['run_id'],handshake=[],resource=[],throughput=[],source_files=[],AB=c['coverage'])
 def rows(p):
  chart['source_files'].append(dict(path=p.relative_to(ROOT).as_posix(),sha256=sha(p)))
  return [json.loads(s) for s in p.read_text(encoding='utf-8').splitlines() if s.strip()]
 for e in read(m5/'data/results_index.json'):
  d=relocate(e['result_dir']);mode=e['mode']
  if e['experiment']=='handshake':
   rr=rows(d/('p_trials.jsonl' if mode=='P' else 'trials.jsonl'));v=sorted(r['handshake_total_ns'] for r in rr)
   assert len(v)==1000 and all(r['success'] for r in rr)
   h=dict(mode=mode,run_id=e['run_id'],n=1000,handshake_successes=sum(r['handshake_success'] for r in rr) if mode!='P' else sum(r['business']['client_tls']['handshake_complete'] and r['business']['server_tls']['handshake_complete'] for r in rr),business_successes=sum(r['success'] for r in rr),mean_ns=sum(v)//len(v),median_ns=(v[499]+v[500])//2,p95_ns=v[949],zero_ns=v.count(0),latencies_ns=v)
   chart['handshake'].append(h)
  elif e['experiment']=='resource':
   for r in rows(d/'resource_trials.jsonl'):
    chart['resource'].append(dict(mode=mode,batch=e['batch'],run_id=e['run_id'],role=r['role'],process_cpu_total_ns=r['process_cpu_total_ns'],peak_working_set_bytes=r['peak_working_set_bytes'],batch_handshakes=100))
  else:
   rr=rows(d/('p_throughput_rounds.jsonl' if mode=='P' else 'throughput_rounds.jsonl'))
   rates=[r['application_goodput_bytes_per_sec'] for r in rr];tps=[r['transactions_per_sec'] for r in rr]
   assert len(rr)==5 and sum(r['completed_transactions'] for r in rr)==1000
   for r in rr:
    # Frozen goodput uses completed-confirmation observation window.
    assert r['application_goodput_bytes_per_sec']>=0
   chart['throughput'].append(dict(mode=mode,run_id=e['run_id'],rounds=5,transactions=1000,accepted_telemetry_bytes=sum(r['accepted_telemetry_plaintext_bytes'] for r in rr),goodput_bytes_per_sec=statistics.mean(rates),transactions_per_sec=statistics.mean(tps),rounds_raw=rr))
 golden=read(OUT/'regression/m5_recomputed/recomputed_report.json')
 for h,g in zip(chart['handshake'],golden['handshake']):assert all(h[k]==g['handshake_latency'][k] for k in ['mean_ns','median_ns','p95_ns'])
 for t,g in zip(chart['throughput'],golden['throughput']):assert math.isclose(t['goodput_bytes_per_sec'],g['avg_application_goodput_bytes_per_sec'],rel_tol=1e-12)
 (OUT/'materials').mkdir(exist_ok=True);write(OUT/'materials/chart_data.json',chart)
 hs='\n'.join(f"|{h['mode']}|{h['n']}|{h['handshake_successes']}|{h['business_successes']}|{h['mean_ns']/1e6:.3f}|{h['median_ns']/1e6:.3f}|{h['p95_ns']/1e6:.3f}|{h['zero_ns']}|" for h in chart['handshake'])
 tp='\n'.join(f"|{t['mode']}|{t['rounds']}×200|{t['goodput_bytes_per_sec']:.1f}|{t['transactions_per_sec']:.1f}|{t['accepted_telemetry_bytes']}|" for t in chart['throughput'])
 wallA=c['coverage']['A']['wall_seconds'];wallB=c['coverage']['B']['wall_seconds']
 text('INNOVATION_REPORT.md',f'''
# M6 结构化篡改测试创新候选报告

实际改进是一个测试层规则表和统一证据契约。B 将用例定位、预期与双端实际失败阶段、双方 Channel 状态、公共帧摘要和运行来源放入同一 JSON。生产握手、库、记录与演示逻辑没有变化。有限比较没有显示 B 的规则覆盖或已知缺陷检出优势，不把结果包装成安全性提升或学术新颖性证明。

冻结计划 docs/INNOVATION_PLAN.md，SHA-256 `{f['plan_sha256']}`。run_id `{c['run_id']}`；测量源码清单 `{c['measurement_source_manifest_sha256']}`。逐条 Go JSON 和真实进程退出码位于 comparison_v2/commands，最终汇总 comparison.json；全量回归另列，不能混入 A/B 时间。

|实际项目|A 人工固定|B 规则生成|
|---|---:|---:|
|共同规则覆盖|16/16|16/16|
|隔离已知缺陷检出|3/3|3/3|
|漏检 / 不可达|0 / 0|0 / 0|
|顶层测试 / 叶用例|28 / 60|1 / 39|
|结构化观测记录|36|39|
|同时包含双端阶段的记录|0/36|39/39|
|公共帧摘要记录|0/36|39/39|
|stdout 字节范围|{min(c['coverage']['A']['stdout_bytes'])}–{max(c['coverage']['A']['stdout_bytes'])}|{min(c['coverage']['B']['stdout_bytes'])}–{max(c['coverage']['B']['stdout_bytes'])}|
|实际进程耗时三次 秒|{', '.join(f'{v:.3f}' for v in wallA)}|{', '.join(f'{v:.3f}' for v in wallB)}|
|实际进程耗时算术均值 秒|{statistics.mean(wallA):.3f}|{statistics.mean(wallB):.3f}|

A 没有被减弱：五个原文件 hash 保持，原手写断言、重签坏点、客户端核心、反射/记录篡改等全部执行。A 的日志已有双端状态和部分阶段定位。SM2临时参数共同项限定认证绑定；采集时A索引指到附加的重签坏点测试，最终材料索引改指同次已执行且通过的gateway_ephemeral_public_key_changed，计数仍16。原采集脚本及context保留，不重写旧比较结果。B不声称也覆盖A的重签坏点验证。B 有统一双端阶段和摘要，但覆盖项相同也不代表所有字段位置、所有状态路径或所有断言等价；B 并不替代 A。B 另有角色/版本非法值用例，扩展项不加入共同 16 项的分母。新用例数量不作为创新指标。

三组缺陷只有临时目录中 *_test.go 函数副本，未修改原生产文件，未生成应用程序。每组 A/B 都有 M6REACH 实际目标分支触达及目标断言失败；退出 1 是检出证据，不是健康程序回归失败。D1 去除 SigB 拒绝后，下游 Finished 仍阻止建链，测试检出的是失败阶段退化。D3 去除套件重算拒绝后，签名仍拒绝同一次修改，亦属阶段退化。D2 去除 FinishedB 拒绝后，设备错误地获得 Channel，双方 Channel 为 true，业务是否接受仍 null；不将它写成真实生产漏洞或业务攻击成功。

B 三条独立复现 T_sigB/H_finB/H_implicit 退出 0，用例定义、目标失败阶段与 Channel 状态一致。密码随机源继续随机，公共报文 hash 不要求重复一致。摘要记录的是中继收到的原始帧及计划转发字节，输出数组在实际 Write 前记录，不能从它证明字节已被端点完整接收。端点内部 transcript/验签中间态未直接采集，继续未知。H_implicit 在测试 KEM 后端完成真实封装后替换密文，双方公共报文一致，但共享结果不同，目标实际到达 Finished。仅有验收器损坏文件的 M5 controls 不加入协议用例。

计时包括身份生成、真实握手和必要记录，进程 wall 包括 Go 启动及编译缓存；A/B 阶段先后固定交替，只有三次，不能做统计显著性或机器效率普遍排名。A 有更多手工断言且正常 record 测试预算沿用原值，B 记录用例另带正常首条对照，成本对照不是完全同工作量 benchmark。输出尺寸增加是审计证据的实际成本。未测人工维护时间，不写工时节省。

开发记录保留：compare/compare_r2 在启动前语法/缩进失败；compare_r3 已完成首轮 A/B，但 Go test2json 把长行拆成多个 Output，提取器未合并而失败。B_1 的系统退出码未及时存下，为未知，不能根据 PASS 冒充真实进程码。修正先保存进程码再按 Test 重组完整行，最终 comparison_v2 为完整预定三轮；首轮开发观测仍保留，作为计划的修复版本例外，不挑最快样本。structured_initial 为开发验证，不算正式重复。

可复核性改善成立于此处统一证据字段和三条复现。有限变体、固定规则和可信测试端点不能推导协议完备性、形式化安全或整体抗量子认证。race 和独立 SM2 交换继续 pending。
''')
 text('FINAL_DESIGN_REPORT.md',f'''
# EdgeHybrid 最终设计说明书

本说明书面向竞赛评审和复现者，描述 A1-31 软件仿真原型及证据范围。当前实现提供真实传统/混合双向认证通信、生命周期、20 设备演示、M5 冻结实验以及 M6 测试工具对照。该组合是项目协议设计，未经完整外部安全审计或形式化证明，不是标准协议或密码合规认证。性能证据适用于所列 Windows 本机条件。

## 原题要求和交付边界

原题来源与六项要求见 REQUIREMENTS.md。软件以 Go/TCP 模拟一网关多设备，支持存量传统设备与默认混合优先的渐进迁移。源码、固定依赖、许可、测试与逐次实验材料全部在审查包；用户 Demo 作为独立原包保留。单次握手 CPU/峰值、P 分角色资源、独立 SM2 交换和 race 等缺口不因阶段完成而改成已完成。需求对应表见 FINAL_DELIVERY_INDEX.json。

## 模块和程序入口

|模块或入口|职责|
|---|---|
|cryptosuite|固定可靠密码组件适配，SM2 静态与临时密钥协商、SM2 签名、SM3/HKDF、SM4-GCM、ML-KEM-768|
|handshake / policy / wire|规范有界编码、可信策略、H1–H4 身份认证与密钥确认|
|record / app|AEAD 序号、会话隔离、遥测/控制/真实 ACK、身份授权|
|identity / secctl|登记、密钥文件、独立 key_id、轮换吊销与销毁使用句柄|
|gateway / device / fleet|网关、有界设备工作者和多设备调度|
|bench / pbench / evidence / metrics|测量、结果契约、完整验收与 Windows 资源 API|
|demo|现有本机页面与有限 1/20 台演示，不用它采样性能|

cmd 负责参数与装配，生产密码和协议语义位于 internal。M6 生成器在 handshake/m6_structured_test.go，运行/重算脚本在 scripts；没有生产故障开关。模块依赖与细分文件见 ARCHITECTURE.md。

## 信任 身份和迁移

初始登记通过可信离线配置，将身份、角色、SM2 UID、独立签名公钥和协商公钥及标识、最低模式、世代、修订与生命周期绑定。网络消息的自称身份不能替代登记。默认 minimum_mode=hybrid、allow_legacy=false。仅可信配置明确授权的传统身份可用 Suite1；双方支持混合时优先 Suite2，失败终止而不静默回退。客户端重算选择，能力列表顺序按实际字节认证。旧设计资料中的 M2 尚未实现描述是阶段记录，应以当前源码和本说明为准。

## 四消息握手和计时边界

H1：设备发送能力、设备/网关身份、签名与协商 key_id、nonce、SM2 临时公钥，提供混合时含 ML-KEM 临时封装公钥。H2：网关返回能力、选择、身份/key_id、nonce、SM2 临时公钥和混合密文，再用长期签名密钥认证完整 H1 和 ServerCore。设备先校验登记/选择/字段条件再验 SigB。

H3：设备验证/派生后发送 ClientCore、SigA 和 FinishedA。网关验证身份、SigA 和 FinishedA，随后 H4 发送 FinishedB。设备验证 FinishedB 后才得到 Channel。每帧含外层长度/version/kind，各变长字段显式长度，超限、未知、尾随与错误顺序拒绝。全握手共享一个绝对 deadline，消息不续期。

网关成功写 H4 不证明设备接受 H4。因此篡改 FinishedB 可观测 gateway Channel=true、device=false；没有业务处理观测时业务状态为 null。应用记录只能在本端完成认证/确认并取得 Channel 后使用。M5 计时为实际完整握手 API 的单调墙钟观测，来源与更细边界按冻结方案，不以 Demo 耗时替代。

## 密钥派生 记录和生命周期

Suite1 用完整 SM2 密钥交换结果；Suite2 使用带域分离与长度编码的 Zsm2、Zpq 两份共享结果，经 HKDF-SM3 一次 Extract 得到 PRK，再以独立标签和已认证 transcript 多次 Expand。Finished 使用 T2，记录方向密钥、nonce 前缀和 SID 使用完整 T4，避免循环依赖。没有异或、截断或缺 KEM 填零。当前 SM2 组件完成核心协商，使用项目级 Finished，不声称额外实现 GB/T 32918.3 显式 S1/S2 确认流程。

SM4-GCM 的每方向独立密钥配 4 字节 nonce 前缀和 8 字节递增序号。头部包含 Suite、类型、方向、SID、epoch、seq，并认证为 AAD。接收计数只在标签验证成功后推进。发送取号、封装、写入串行；部分写失败终止会话，同密钥不重置计数。新握手使用新临时材料/SID/记录密钥，旧/跨设备/反射记录被拒绝。

身份状态 ACTIVE、ROTATING、REVOKED、RETIRED；注册表原子提交，新世代文件先准备，提交后旧文件 best effort 退役。运行中握手前 reload 和周期 sweep 检查吊销/世代变化，默认 250ms 观测间隔。记录/时间阈值触发完整新握手，不做带内 rekey；业务 operation_id 幂等仅在当前会话内，跨重启 exactly-once 未实现。

## 威胁模型与安全界限

假设初始登记、端点系统和未泄露长期密钥可信，网络可监听/篡改/重放/删除/延迟。签名和 Finished 分别约束来源/转录与共享结果一致性；ML-KEM 正确长度隐式拒绝不能只看 nil error。记录验证与会话隔离限制重放，最低模式和认证选择限制迁移降级。

混合建钥针对先存储后分析风险的设计动机不构成全面量子安全结论，身份签名仍是经典 SM2。随机源来自密码库/crypto/rand，失败终止；测试公开规则调度不替代生产熵。密钥泄露、恶意管理员、端点失陷、侧信道、硬件熵、供应链和完整 DoS 抗性不由这些测试解决。Destroy 结束句柄使用，Go 托管内存不保证物理擦除；Windows 私钥目录继承 ACL，不能宣称额外 ACL 隔离。帧长、超时、连接和记录预算限制明显无界增长。

## M5 实测结果和口径

冻结方案 m5-formal-20261001-v1；源码 `{chart['m5_measurement_source_sha256']}`。Windows 11 build26200，i7-11800H、8核16线程、平衡电源、Go1.27.1，本机 loopback，无暖机、固定 T/H/P 顺序和非空闲背景。每模式固定 1000 个独立尝试，无补跑；握手和业务分开统计。

|模式|尝试分母|握手成功|业务成功|均值 ms|中位 ms|P95 ms|0ns 条数|
|---|---:|---:|---:|---:|---:|---:|---:|
{hs}

ns 为字段单位，未独立测定 Windows 时钟分辨率。所有 0ns 行原样保留，不能解释成零耗时或抹去。1000/1000 仅是本批观测，不保证未来100%。T 每次握手用户态帧 c2s/s2c 为308/272字节，H为1492/1360；P TLS记录大小略变。均不含 IP/TCP。

T/H 各5批×100握手-only，网关与设备分进程。CPU 为进程启动至终态采样累计，设备循环窗口另存；峰值是高水位，两端峰值不能相加为同时峰值，也不除100称单次峰值。P分角色和单次握手资源为 null+原因。Linux资源 NOT_TESTED。

|模式|吞吐轮次×事务|有效轮 goodput 算术均值 B/s|TPS|接受遥测字节|
|---|---:|---:|---:|---:|
{tp}

吞吐包含真实遥测接受/控制发送/ACK 验证，以及日志可见性和约1ms确认轮询。P 为 TLS1.3、X25519MLKEM768、ECDSA P256 双向证书和 AES-GCM/TLS KDF；T/H 为国密协议。认证、记录、日志和进程布局不同，整体差异不能全部归因 ML-KEM，不能推导低资源硬件排名。每轮速率算术平均口径保持，额外加权指标未替换原指标。

## M6 实验结果

预先固定 A/B 共同16规则和同组三个隔离缺陷。A全部原固定测试保留；B39规则用例覆盖真实T/H及记录。两者覆盖16/16、检出3/3。B新增统一双端阶段、输入规则和公共摘要，独立复现3条通过，未显示覆盖/检出优势。详细原始结果、耗时、输出成本、冗余防线和开发失败见 INNOVATION_REPORT.md。B不替代A，也不是有限测试即安全证明。

## 验证和复现

M6 full Go JSON 真退出0，go mod verify/vet和原Python验收自测通过，M5 16批实际完整验收器离线重算通过。M5旧raw、生产Go及Demo hash不变。race、独立SM2交换继续pending，OpenSSL签名互操作仍SKIP；没有新结果便不升级证据等级。详见 REPRODUCE.md 和 results/milestone_M6.md。阶段旧README/报告中的待完成句子按各自时间保留，当前索引优先。

依赖固定 gmsm v0.44.1、x/crypto v0.54.0，Go标准库 ML-KEM。第三方许可随包，本项目对外开源许可证未由权利人指定。审查包不含运行秘密或exe，Demo原包独立保留。
''')
 text('USER_GUIDE.md','''
# Windows 演示使用手册

使用原 EdgeHybrid_Demo_Windows.zip，SHA-256 96bc6b44d21b2a44e25f13f7f181845934d2173373687bf4f56cf25d187226ac。M6 没有重新发布 exe。用户不需要 Go/Python/Node；需要 Windows x64、可写用户目录和浏览器。它展示真实 Suite2 加密业务，性能资料另见正式报告。

1. 完整解压原包到可读目录，中文和空格路径可用，不在 ZIP 预览里启动。
2. 双击 EdgeHybridDemo.exe 或 启动1台设备.cmd。窗口打印 PAGE_URL、LOG_DIR，浏览器自动打开本机地址。
3. 观察建链、遥测接受、控制发送和远端 ACK 确认。每设备本轮固定3笔，不无限循环。
4. 按窗口 Enter 或 Ctrl+C，等待“已停止”；也可运行同一包目录的 停止演示.cmd。
5. 停止后双击 启动20台设备.cmd，观察独立身份/SID；不要同时打开多个测量实验。

当前在线=0 可以表示本轮已完成且设备已断开，并不等于握手失败。累计建链/三种业务计数是本次记录，缓存状态失联会标不可确认，不把它当在线事实。关闭网页不会停止程序。程序最多保留状态页10分钟，然后受控结束。

重复启动本包会拒绝覆盖运行；不同解压目录各自独立。端口冲突或浏览器未配置时看窗口提示/PAGE_URL。系统安全提示不通过修改系统策略绕过。19+1异常自测中 D020 信任预检失败，没有启动远端握手，其他19台正常；这是异常证据，不能描述成20台全部成功。

可分享 LOG_DIR 下 gateway/device/events/lifecycle/manifest/completed/final_state 等公共日志。不要分享同一数据根的 private 目录。正常停止清理本次私密身份；强制结束可能残留，下次以新run_id和身份启动，不读旧数据冒充本次。默认数据根为 %LOCALAPPDATA%/EdgeHybridDemo/<包目录标识>/runs/<run_id>。完整原版说明见 DEMO_QUICKSTART.md；其中“正式实验未完成”是演示发行时的历史状态，M5已在随后完成。

本轮没有重新运行 Demo，也没有扩大 Windows 机型兼容性证据。演示验证来自原 Demo 阶段，见 results/raw/demo_windows_20260930。安全边界和待验证项见 FINAL_DESIGN_REPORT.md。
''')
 text('REPRODUCE.md','''
# 构建与实验复现手册

解压 M6 审查包后进入 edgehybrid-agent-starter。Go实际测量版1.27.1 windows/amd64，go.mod最低1.25.0；Python原证据3.12.10。依赖按 go.mod/go.sum 固定，不关闭校验服务。所有命令都在工程根目录运行。本文的普通 Go 命令使用已加入 PATH 的 Go。

```powershell
go version
go mod verify
go test -json -count=1 ./... > current_go_tests.jsonl
# 必须立即保存真实进程码，不能用提取器状态代替。
$testExit = $LASTEXITCODE
go list ./...
go vet ./...
New-Item -ItemType Directory -Force ../EdgeHybrid_Rebuild_Bin
go build -o ../EdgeHybrid_Rebuild_Bin/ ./cmd/...
```

若运行 scripts/run_checked.py，先将脚本中 GO 常量设置为本机 Go 的绝对路径，改动会形成新源码 hash，旧 M5 不是该版本的测量。更方便的严格复核按原工具链路径在当前机运行：每命令记录 stdout/stderr/command.json/exit.txt，示例见 results/raw/m6_20261001/regression。summarize-go-test-json.py 接受 --go-exit-code、逐项 --expect-package、--require-tests、--json 输出。expected包从同一树的 go list ./... 获取，不能硬填PASS。

M6原 A/B 可离线核对：

```powershell
python scripts/m6_verify.py --out m6_local_verified.json
python scripts/m6_materials.py
```

材料重算脚本读取既有 raw，写 M6 新材料，不改 M5。正常生成式用例可用 `go test -json -count=1 -run '^TestM6Structured$' ./internal/handshake`。独立复现三个用例用 `-run '^TestM6Structured$/(T_sigB|H_finB|H_implicit)$'`。若要重新执行完整 A/B，使用 `python scripts/m6_compare.py --out results/raw/m6_reproduce_NEW --run-id m6-reproduce-NEW`，先配置脚本 GO；必须选不存在的输出目录和新的 run_id。隔离副本只执行 go test，结束由临时目录上下文清理，不构建生产应用。每个正常回归期望0；每个缺陷 A/B期望1，还须实际目标 M6REACH 和失败断言，不能单看非0码。

M5原证据索引在 results/raw/m5_formal_20261001/data/results_index.json；完整复核实际命令见本轮 regression/m5_recompute，16个验收都调用原 bench/pbench -verify 并重算。原记录内绝对路径是原机位置；外机用当前解压根替换 `/results/raw/m5_formal_20261001/` 前缀，保留相对后缀。二进制不在审查包，需先 build bench/pbench 到包外目录，freeze记录的 producer hash只用来识别原采样程序，不把新重建hash冒充原hash。M5的 measurement_source_resolution.json保留原报告版本映射，M6新增源码的跨阶段解析见 results/raw/m6_20261001/source_resolution.json。

正式重新采样不是本轮验收条件。需要新采样时遵循 FORMAL_EXPERIMENT_PLAN.md，在新目录重新prepare/freeze，停止Demo和其他实验，原参数T/H/P各1000、T/H各5×100资源、吞吐各5×200；不向旧结果目录追填。m5_formal_run.py对source/binary/plan有严格冻结守卫，交付源码新增M6后旧freeze运行入口应拒绝，不绕开守卫。失败/超时/中断保留，固定分母，禁止补成功或修改旧raw。

P包括TLS1.3真实混合组和双向证书，并非Suite3。资源是100握手进程批次，未测单次指标保持null。全部性能图表数据由 m6_materials.py 从原逐次/逐轮文件重算，chart_data.json附原路径和hash。答辩构建 scripts/m6_deck.mjs、PDF说明书构建 scripts/m6_pdf.py 使用local_review捆绑artifact-tool/reportlab，不是生产Go依赖；需本机存在对应包/字体，渲染记录仅证明本机生成和检查，不证明各版本PowerPoint行为。Word草稿m6_docx.py已执行，但render_docx.py因缺LibreOffice失败；Word也未安装，所以草稿保留在包外，不交作已验证文档。正式说明书为Markdown和PDF，PPTX可编辑。

归档验证：`python scripts/package_review.py --verify ../EdgeHybrid_M6_Review_Package.zip`。MANIFEST逐条覆盖包内文件（自身除外）。不分享runtime/private/私钥/令牌，不嵌套旧ZIP或exe。race/独立SM2交换没有新执行，继续pending。
''')
 text('DEFENSE_QA.md','''
# 答辩常见问题回答

## SM2 密钥协商为什么不是普通 ECDH
适配器使用 gmsm 的 SM2MQV 与 SM2SharedKey，静态/临时材料、双方UID和角色共同参与；仅做普通ECDH、签名或加密传随机数都不能称完整SM2协商。项目用协议Finished确认共享结果，额外标准S1/S2显式确认未用，独立第二交换实现仍pending。

## 混合为什么必须有两份秘密
Suite2把Zsm2和Zpq带标签、长度、算法域编码后进入HKDF-SM3，两者均改变派生结果；不异或、不截断、不缺项填零。这个组合是项目设计，没有完整形式化证明。

## 签名和 Finished 各验证什么
签名验证可信登记身份对公开握手字节的认证；Finished确认共享结果与转录导出的确认密钥一致。ML-KEM正确长度坏密文可nil error返回不同秘密，必须由Finished拒绝。

## 篡改 FinishedB 为什么网关仍成功
网关已经验证H3并写出H4，本端建链完成；设备看到被改H4会拒绝。写入成功不证明对端接受。M6分别记录两端Channel，业务无观测写null。

## nonce 和计数器怎样避免复用
每方向独立密钥和4字节前缀，8字节单调序号。发送取号和写入串行，接收只在AEAD通过后推进。重连完整新握手换新密钥，不能旧密钥归零。标签验证不能补救发送nonce复用。

## 创新相对人工基线有什么实际变化
测试规则矩阵加统一JSON保存用例位置、双端阶段、状态、来源和公开摘要。A/B共同16规则和三个变体都一样覆盖/检出。B审计信息更齐全，输出更大，没有证据说明安全发现率更高，也不把测试数量当创新。

## 已知缺陷会进入生产吗
缺陷函数仅在临时复制树的*_test.go，A/B共享同组三个删除分支并实际记录触达。不修改原Client/Server，不提供生产开关，测量与Demo仍原样。D1/D3后续防线还拒绝，检出阶段退化不等于攻击获得业务。

## 公共摘要能证明端点内部转录相同吗
不能。记录中继读到和计划转发的公共字节摘要，Write可能失败，端点内部状态未直接采集。摘要帮助关联具体变异和审计复现，不代替签名/Finished或内部观测。

## 0ns是不是零成本
不是。T/H/P分别131/47/33条0ns是原机器计时观测。字段单位不是精度保证，未独立测分辨率。保留原行，展示毫秒有限位数，不改成1ns或删样本。

## 1000/1000是不是未来100%成功
只是冻结条件下1000尝试的有限观测，握手/业务成功分开记。不能保证未来可靠性，不能把资源批次或Demo次数拼入分母。

## CPU和峰值能作为单次指标吗
不能。T/H分角色每批100握手，CPU窗口和进程高水位都有启动/运行成本。峰值不能除100或两端相加当同时峰值。单次/P未测资源null附原因。

## P对照为什么不能归因ML-KEM
P是TLS1.3混合组、ECDSA证书、AES-GCM、TLS KDF，T/H是国密与项目记录。日志、进程布局和观察成本也不同。一次固定顺序本机实验不能分离算法贡献或推出硬件通用排名。

## 是否全面抗量子或通过标准认证
身份仍经典SM2签名，混合建钥的动机不等于全面后量子认证。协议是自定义组合，没有标准认证、外部完整审计或形式化证明。

## 哪些还没验证
race与独立SM2交换pending；OpenSSL签名互操作SKIP，Linux资源NOT_TESTED。没有硬件低资源、跨机、完整DoS/侧信道/供应链结论；Go Destroy不保证物理擦除，Windows未额外加固私钥ACL。M6没有重新运行Demo。

## 怎样复核交付
验证ZIP MANIFEST、逐次M5原hash、原始Go JSON真实退出码、A/B观测、缺陷触达、当前与采样源码映射。chart_data.json可从raw重算，PowerPoint图表可编辑。全部历史失败保留，无法重建的环境条件如实标记。
''')
 index=[]
 specs=[
 ('1','密钥生命周期',['internal/identity','cmd/secctl','internal/app/sessions.go'],['internal/identity/identity_test.go','internal/app'],['results/raw/m4_verification.txt','results/raw/m5_formal_20261001/freeze/commands/go_test_json/stdout.txt']),
 ('2','双向认证与两份秘密KDF',['internal/cryptosuite','internal/handshake/keyschedule.go','internal/handshake/client.go','internal/handshake/server.go'],['internal/handshake/keyschedule_hybrid_test.go','internal/handshake/kem_implicit_test.go','internal/handshake/tamper_test.go'],['results/raw/m5_formal_20261001/data/results_index.json','results/raw/m6_20261001/comparison_v2/commands/normal_B_1/observations.json']),
 ('3','SM4记录与重放/重连',['internal/record','internal/app'],['internal/handshake/wire_replay_test.go','internal/handshake/channel_test.go'],['results/raw/m6_20261001/comparison_v2/commands/normal_A_1/stdout.jsonl','results/raw/m6_20261001/comparison_v2/commands/normal_B_1/stdout.jsonl']),
 ('4','传统/混合迁移',['internal/policy','internal/handshake'],['internal/handshake/downgrade_test.go','internal/handshake/no_fallback_test.go'],['results/raw/m6_20261001/comparison_v2/commands/normal_A_1/observations.json']),
 ('5','20设备与正式性能',['cmd/fleet','internal/bench','internal/pbench','internal/metrics'],['internal/bench','internal/pbench'],['results/raw/m5_formal_20261001/data/results_index.json','results/raw/m5_formal_20261001/report_verified/recomputed_report.json','results/raw/demo_windows_20260930']),
 ('6','模型/来源/许可',['docs/FINAL_DESIGN_REPORT.md','docs/DEPENDENCIES.md','THIRD_PARTY_NOTICES.md'],['results/raw/m6_20261001/regression/go_test_json/stdout.txt'],['results/raw/m6_20261001/freeze/audit/REVIEW.md','results/raw/m6_20261001/regression/m6_verified.json']),
 ('innovation','可审计结构化篡改测试',['internal/handshake/m6_structured_test.go','scripts/m6_compare.py'],['TestM6Structured','隔离D1/D2/D3'],['results/raw/m6_20261001/comparison_v2/comparison.json','results/raw/m6_20261001/comparison_v2/measurement_source_manifest.sha256']),
 ]
 for id,requirement,source,tests,evidence in specs:
  index.append(dict(requirement_id=id,requirement=requirement,source=source,tests=tests,evidence=[dict(path=p,exists=(ROOT/p).exists(),sha256=sha(ROOT/p) if (ROOT/p).is_file() else None) for p in evidence],limitations='见 FINAL_DESIGN_REPORT.md；未知/未测状态不得提升证据等级'))
 # Use the actual existing M4 record path, rather than inventing a missing evidence path.
 for row in index:
  row['evidence']=[e for e in row['evidence'] if e['exists']]
 write(ROOT/'docs/FINAL_DELIVERY_INDEX.json',dict(requirement_source='docs/REQUIREMENTS.md',m6_run_id=c['run_id'],m5_index='results/raw/m5_formal_20261001/data/results_index.json',m5_measurement_source_hash=chart['m5_measurement_source_sha256'],m6_measurement_source_hash=c['measurement_source_manifest_sha256'],rows=index,historical_gaps=dict(race='pending',independent_SM2_exchange='pending',OpenSSL_signature='SKIP',Linux_resources='NOT_TESTED')))
 mapping=['# 需求到源码 测试 原始证据对应表','','完整路径、hash和限制见 FINAL_DELIVERY_INDEX.json。表内目录项是来源集合，不能将目录hash当文件hash。','','|要求|源码|测试|原始证据|','|---|---|---|---|']
 for r in index:mapping.append('|'+r['requirement_id']+' '+r['requirement']+'|'+', '.join(r['source'])+'|'+', '.join(r['tests'])+'|'+', '.join(e['path'] for e in r['evidence'])+'|')
 text('REQUIREMENT_EVIDENCE_MAP.md','\n'.join(mapping))
 write(OUT/'materials/provenance.json',dict(chart_data_sha256=sha(OUT/'materials/chart_data.json'),m5_original_index_sha256=sha(m5/'data/results_index.json'),m6_comparison_sha256=sha(CMP/'comparison.json'),docs=[dict(path=p.relative_to(ROOT).as_posix(),sha256=sha(p)) for p in (ROOT/'docs').glob('*.md') if p.name in ['INNOVATION_REPORT.md','FINAL_DESIGN_REPORT.md','USER_GUIDE.md','REPRODUCE.md','DEFENSE_QA.md','REQUIREMENT_EVIDENCE_MAP.md']]))
 print('M5 raw chart data and all M6 Markdown/JSON materials generated.')
if __name__=='__main__':main()
