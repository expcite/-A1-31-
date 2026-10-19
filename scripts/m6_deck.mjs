import fs from 'node:fs/promises';
import path from 'node:path';
import {pathToFileURL,fileURLToPath} from 'node:url';
const root=process.env.EDGEHYBRID_ROOT||path.resolve(path.dirname(fileURLToPath(import.meta.url)),'..');
const dep=process.env.EDGEHYBRID_NODE_MODULES||'C:/Users/[REDACTED_USER]/.cache/local_review-runtimes/local_review-primary-runtime/dependencies/node/node_modules';
const skill=process.env.EDGEHYBRID_PRESENTATIONS_SKILL||'C:/Users/[REDACTED_USER]/.local_review/plugins/cache/local_provider-primary-runtime/presentations/26.909.11814/skills/presentations';
const python=process.env.EDGEHYBRID_ARTIFACT_PYTHON||'C:/Users/[REDACTED_USER]/.cache/local_review-runtimes/local_review-primary-runtime/dependencies/python/python.exe';
process.env.RUNTIME_NODE_MODULES=dep;
const {Presentation,PresentationFile,FileBlob}=await import(pathToFileURL(path.join(dep,'@oai/artifact-tool/dist/artifact_tool.mjs')).href);
const {resolvePresentationFont,applyPresentationChartFont,finalizePresentation}=await import(pathToFileURL(path.join(skill,'container_tools/artifact_tool_utils.mjs')).href);
const family=resolvePresentationFont({fontFamily:'Microsoft YaHei'});
const raw=path.join(root,'results/raw/m6_20261001');
const build=path.join(raw,'deck_build_final');const output=path.join(root,'deliverables/M6');
await fs.mkdir(build,{recursive:true});await fs.mkdir(output,{recursive:true});
const data=JSON.parse(await fs.readFile(path.join(raw,'materials/chart_data.json'),'utf8'));
const c=JSON.parse(await fs.readFile(path.join(raw,'comparison_v2/comparison.json'),'utf8'));
const p=Presentation.create({slideSize:{width:1280,height:720}});
function txt(s,content,left,top,width,height,size=30,color='#142735',bold=false){
 const x=s.shapes.add({geometry:'textbox',position:{left,top,width,height},fill:'none',line:{fill:'none',width:0}});
 x.text=content;x.text.style={typeface:family,fontSize:size,color,bold,autoFit:'none'};return x;
}
function slide(title,source){const s=p.slides.add();s.background.fill='#FAFBFC';txt(s,title,65,40,1150,95,48,'#142735',true);txt(s,`EdgeHybrid  M6     ${p.slides.items.length}`,65,670,1140,25,17,'#52636E');s.speakerNotes.textFrame.setText(source);return s;}
function body(s,lines,top=174){lines.forEach((x,i)=>txt(s,x,76,top+i*90,1120,78,30));}
function table(s,values,top=180,height=360,width=1120,left=76){
 const tb=s.tables.add({rows:values.length,columns:values[0].length,left,top,width,height,values});
 for(let r=0;r<values.length;r++)for(let col=0;col<values[0].length;col++){
  const cell=tb.getCell(r,col);cell.fill=r===0?'#E5EDF2':'#FAFBFC';cell.text.style={typeface:family,fontSize:25,color:'#142735',bold:r===0};
 }return tb;
}
function chart(s,categories,series,unit,top=170,height=380){
 series=series.map(x=>({...x,values:x.values.map(v=>Number(v.toFixed(3)))}));
 s.speakerNotes.textFrame.setText(s.speakerNotes.textFrame.text+' 展示图表值按3位小数舍入以保持Excel可编辑数据兼容，未舍入原始数值及来源保留在chart_data.json。');
 const ch=s.charts.add('bar',{position:{left:76,top,width:1120,height},categories,series,barOptions:{direction:'column',grouping:'clustered'},hasLegend:series.length>1,legend:{position:'bottom',textStyle:{fontSize:24,typeface:family}},xAxis:{textStyle:{fontSize:22,typeface:family}},yAxis:{min:0,numberFormatCode:'0.00',textStyle:{fontSize:22,typeface:family}},dataLabels:{showValue:true,position:'outEnd',textStyle:{fontSize:23,typeface:family}},chartFill:'#FAFBFC',plotAreaFill:'#FAFBFC'});
 applyPresentationChartFont(ch,{fontFamily:family});txt(s,unit,76,135,1120,30,23,'#52636E');return ch;
}
const m5note=`数据直接从M5逐次/逐轮raw重算。计划m5-formal-20261001-v1，测量源码SHA-256 ${data.m5_measurement_source_sha256}。来源文件及SHA-256见results/raw/m6_20261001/materials/chart_data.json。run_id: ${[...data.handshake,...data.throughput].map(x=>x.run_id).join(', ')}。不推断普遍排名。`;
let s=slide('EdgeHybrid 安全通信原型','来源 docs/REQUIREMENTS.md、FINAL_DESIGN_REPORT.md。标题中的安全通信是原型目标，无标准认证或安全证明。');
txt(s,'国密与后量子混合建钥',76,235,1120,90,54,'#142735',true);
txt(s,'传统与混合迁移  ·  可审计篡改测试',76,365,1120,70,34);
txt(s,'M5 冻结实验与 M6 有限对照\n2026 年 10 月 1 日',76,490,1120,100,29,'#52636E');
s=slide('题目要求与完成范围','来源 docs/REQUIREMENTS.md、FINAL_DELIVERY_INDEX.json。单次/P资源等要求缺口如实保留。');
body(s,['真实 SM2 协商、SM3 派生、SM4-GCM 与 ML-KEM-768','一网关、20 模拟设备与可信配置控制的传统兼容','M5 固定分母实验，M6 测试工具与最终材料','未测单次资源、独立交换和 race 状态随报告保留']);
s=slide('模块和程序入口','源码 internal/cryptosuite、handshake、record、identity、app、bench、pbench、cmd。见docs/ARCHITECTURE.md。');
table(s,[['入口或模块','主要职责'],['gateway / device / fleet','真实通信与多设备调度'],['cryptosuite / handshake / policy','密码适配、认证、确认与可信协商'],['record / app / identity','序号、业务授权、登记与生命周期'],['bench / pbench / evidence','采集、完整验收和原始结果重算']],174,405);
s=slide('四消息认证与确认','源码 internal/handshake/client.go、server.go、keyschedule.go。H表示消息而非里程碑。端点内部状态以返回结果为观测。');
table(s,[['消息','公开字段及验证'],['H1  设备发起','能力、身份、key_id、nonce、临时参数'],['H2  网关回应','选择与参数，SigB 认证 H1 和核心'],['H3  设备认证','SigA 与 FinishedA，网关校验'],['H4  网关确认','FinishedB，设备校验后获得 Channel']],174,390);
txt(s,'H4 写出成功不能证明设备接受。篡改时两端状态可能不同。',76,586,1120,60,27);
s=slide('密钥与会话生命周期','源码 identity、app/sessions.go、rekey.go、handshake/keyschedule.go、record。SM2标准显式S1/S2未用，项目级Finished不可跳过。');
body(s,['长期签名、静态协商、临时会话材料分别管理','Suite2 两份共享结果带标签和长度进入 HKDF-SM3','每方向独立密钥，nonce 前缀和递增序号','轮换或重连执行新握手，旧密钥计数器不重置']);
s=slide('威胁模型与验证边界','来源 docs/FINAL_DESIGN_REPORT.md、THREAT_MODEL.md、DEFENSE_QA.md。');
body(s,['假设初始登记、端点系统和未泄露密钥可信','覆盖篡改、降级与真实记录重放的有限测试','身份仍为经典 SM2，协议组合没有完整安全证明','资源上限限制无界增长，未证明完整 DoS 或侧信道安全']);
s=slide('M5 完整握手观测',m5note);
chart(s,['T','H','P'],[{name:'均值',values:data.handshake.map(x=>x.mean_ns/1e6),fill:'#1B5675'},{name:'中位数',values:data.handshake.map(x=>x.median_ns/1e6),fill:'#719AB0'},{name:'P95',values:data.handshake.map(x=>x.p95_ns/1e6),fill:'#B5C6D0'}],'毫秒  每模式独立完整握手 1000 次');
txt(s,'握手与业务各 1000/1000，仅限本批。0ns：T 131、H 47、P 33。',76,569,1120,52,27);
txt(s,'保留零值。ns 字段单位不代表时钟精度，未独立测定分辨率。',76,621,1120,40,23,'#52636E');
s=slide('M5 分角色进程资源',m5note+'资源各模式5批，每批100次握手-only。图显示每角色五批峰值的算术均值，不是单次峰值，不是同时峰值；全部20条原值在chart_data.json。');
const groups=[['T','gateway'],['T','device'],['H','gateway'],['H','device']];
chart(s,groups.map(x=>x.join(' / ')),[{name:'各角色五批峰值均值',values:groups.map(([mode,role])=>{const rr=data.resource.filter(x=>x.mode===mode&&x.role===role);return rr.reduce((a,x)=>a+x.peak_working_set_bytes,0)/rr.length/1048576}),fill:'#1B5675'}],'MiB  五批独立进程高水位均值，每批 100 次握手');
txt(s,'CPU 为进程累计，峰值不能除以 100 或两端相加。',76,572,1120,50,27);
txt(s,'单次握手资源与 P 分角色资源保持 null，Linux 未测。',76,625,1120,40,23,'#52636E');
s=slide('M5 真实业务吞吐',m5note+'口径有效轮速率算术平均。含遥测接受、控制、ACK和确认观察成本，不是算法吞吐上限。');
chart(s,['T','H','P'],[{name:'goodput',values:data.throughput.map(x=>x.goodput_bytes_per_sec/1000),fill:'#1B5675'}],'千字节每秒  每模式 5 轮 × 200 事务，逐轮速率算术平均');
txt(s,'P 为 TLS1.3 混合组、ECDSA、AES-GCM。协议与进程布局有差异。',76,568,1120,60,27);
txt(s,'整体差异不能全部归因 ML-KEM，不能推出低资源硬件表现。',76,625,1120,40,23,'#52636E');
s=slide('M6 规则生成与固定人工基线',`冻结计划docs/INNOVATION_PLAN.md，run_id ${c.run_id}，测量源码 ${c.measurement_source_manifest_sha256}。原始数据 comparison_v2/commands。`);
table(s,[['实际对照','A 人工固定','B 规则生成'],['共同规则覆盖','16 / 16','16 / 16'],['已知隔离缺陷检出','3 / 3','3 / 3'],['公共帧摘要记录','0 / 36','39 / 39'],['双端阶段统一记录','0 / 36','39 / 39']],174,375);
txt(s,'改进在审计定位与复现，未显示覆盖或检出优势。B 保留 A。',76,579,1120,66,28);
s=slide('M6 实际成本与缺陷边界',`来源 ${c.run_id}/comparison_v2/comparison.json，三次交替正常运行。A60叶用例，B39，工作量不相同。D1/D3后续防线仍拒绝，D2错误给出Channel，业务未知。临时*_test.go变体。`);
chart(s,['第 1 次','第 2 次','第 3 次'],[{name:'A',values:c.coverage.A.wall_seconds,fill:'#1B5675'},{name:'B',values:c.coverage.B.wall_seconds,fill:'#719AB0'}],'秒  Go 启动至退出，包括编译缓存和身份生成',170,315);
txt(s,'三个隔离缺陷两侧均触达并检出，阶段退化另列。',76,510,1120,45,27);
txt(s,'B 日志约 112KB，A 约 92KB。未测人工工时。',76,566,1120,45,27);
txt(s,'样本仅三次且工作量不同，不能宣称显著速度或维护优势。',76,620,1120,42,23,'#52636E');
s=slide('交付材料与未验证项','来源 docs/REPRODUCE.md、USER_GUIDE.md、DEFENSE_QA.md、results/milestone_M6.md。PPT由本机工具生成并渲染，不宣称在PowerPoint桌面编辑/跨机验证。');
body(s,['完整源码、测试、执行脚本、A/B raw、M5 来源和 MANIFEST','设计说明书、使用与复现手册、需求证据表、答辩问答','原 Demo 与 M5 hash 保留，全量回归与离线重算完成','race、独立 SM2 交换 pending，OpenSSL SKIP，Linux 未测']);
const candidate=path.join(build,'candidate_final.pptx');await (await PresentationFile.exportPptx(p)).save(candidate);
const finalPath=path.join(output,'EdgeHybrid_Defense_v2.pptx');
await finalizePresentation({workspaceDir:root,candidatePath:candidate,finalPath,pythonExecutable:python,integrityValidatorPath:path.join(skill,'container_tools/inspect_presentation_package_integrity.py'),layoutValidatorPath:path.join(skill,'container_tools/inspect_presentation_layout_geometry.py'),layoutArgs:['--expected-slide-size-emu','12192000,6858000','--validate-bullet-geometry','--validate-heading-fit',...[3,4,10].flatMap(x=>['--require-native-table-slide',String(x)])],explicitTotalSlideCount:12,requiredNativeTableOwnerSlides:[3,4,10],requiredNativeChartOwnerSlides:[7,8,9,11],materializeLiteralChartWorkbooks:true,fontPolicy:{basis:'design',families:[family]},verifyArtifactToolImport:true,receiptPath:path.join(build,'validation.json')});
const rendered=await PresentationFile.importPptx(await FileBlob.load(finalPath));
for(let i=0;i<rendered.slides.items.length;i++){
 const preview=await rendered.export({slide:rendered.slides.items[i],format:'png',scale:1});await fs.writeFile(path.join(build,`slide-${i+1}.png`),new Uint8Array(await preview.arrayBuffer()));
}
console.log(`Generated and rendered ${rendered.slides.items.length} editable slides: ${finalPath}`);
