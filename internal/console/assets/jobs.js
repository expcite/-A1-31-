import {node,card,button,readButton,setText,guardButton,publicDetails} from './components.js';
import {confirmAction} from './confirm.js';
export function experimentBlockReason(store){if(!store.fresh())return '状态过期或未授权';const active=store.snapshot.devices.filter(d=>['online','connecting'].includes(d.status));if(active.length)return '仍有 '+active.length+' 台在线／连接中设备，请明确断开';if(!store.jobsKnown||store.now()-store.lastJobsSuccess>=3500)return '当前任务状态未完成读取';if(store.jobs.some(j=>['queued','running'].includes(j.state)))return '有互斥实验任务运行中';if(store.jobs.length>=8)return '本运行8个任务额度已满，请保留证据后新运行';return ''}
export function jobResultMessage(j){return '执行状态 '+j.state+' · 退出码 '+(j.exit_code??'未观测')+' · 任务回执 '+(j.receipt_status||'未核对')+(j.error?' · '+j.error:'')}

export function parseJobHighlights(j){
 const text=j.output_excerpt||'';if(!text)return null;
 const receipt=j.receipt_status==='PASS'?'已持久化 (PASS，仅任务回执)':j.receipt_status==='FAIL'?'失败 (FAIL)':'未核对';
 if(j.kind.startsWith('bench')){
  const median=text.match(/median=(\d+)ns/),p95=text.match(/p95=(\d+)ns/),wire=text.match(/wire bytes per trial C->S=(\d+) S->C=(\d+)/);
  const successes=text.match(/successes=(\d+)/),requested=text.match(/requested=(\d+)/);
  if(!median&&!wire&&!successes)return null;
  const time=m=>m?m[1]+' ns ('+(Number(m[1])/1000000).toFixed(2)+' ms)':'未观测';
  return {title:(j.kind==='bench_H'?'H混合':'T传统')+' · 本次有限smoke输出摘要',metrics:[
   {label:'握手时延 (中位/P95)',value:time(median)+' / '+time(p95)},
   {label:'输出中的单次报文量',value:wire?'C->S: '+wire[1]+'B, S->C: '+wire[2]+'B':'未观测'},
   {label:'握手成功数 / 请求分母',value:successes?successes[1]+' / '+(requested?requested[1]:'分母未观测'):'未观测'},
   {label:'任务回执',value:receipt},
   {label:'范围',value:'部分原始输出摘要；真实退出和CLI验收输出另列，非正式性能结论'}]};
 }
 if(j.kind==='m6_rules')return {title:'本次M6固定规则任务',metrics:[
  {label:'执行状态',value:j.state||'未观测'},
  {label:'进程退出码',value:j.exit_code==null?'未观测':String(j.exit_code)},
  {label:'任务回执',value:receipt},
  {label:'逐例规则结论',value:'查看原始逐例输出；不据退出码补写全部通过或固定断言数量'}]};
 return null;
}

export function benchmarkComparisonCard(){
 const c=card('模式对比与攻防验证基线（传统国密 vs 后量子混合）');
 const tableWrap=node('div',undefined,'table-wrap');
 const table=node('table'),thead=node('thead'),headTr=node('tr'),tbody=node('tbody');
 for(const h of ['评估维度','传统国密模式 (T)','后量子混合模式 (H)','验证范围与限制'])headTr.append(node('th',h));
 thead.append(headTr);
 const rowsData=[
  ['密码套件组成','SM2密钥交换 + SM3 + SM4-GCM','SM2密钥交换 + ML-KEM-768 + SM3 + SM4-GCM','实现能力说明，不是合规认证；SM2身份认证不是后量子身份认证'],
  ['单次握手报文','查看记录→历史测量的原始数据','查看记录→历史测量的原始数据','需绑定测量版本、平台、run和分母；本卡不提供实测数值'],
  ['握手耗时','历史结果属于原测量版本','当前任务属于有限smoke','保留0ns和失败；不将整体差异全部归因于ML-KEM'],
  ['防降级','可信配置明确允许的传统身份','混合失败不自动回退','具体正反例与拒绝阶段见原始规则记录，不承诺阻断所有攻击'],
  ['完整会话换钥','完整新握手和新密钥','按本次rekey_id关联双端新SID','不重置同key计数器；不保证无丢失或无中断，业务验证另行确认']
 ];
 for(const row of rowsData){
  const tr=node('tr');
  row.forEach((col,idx)=>{
   const td=node('td');
   if(idx===0)td.innerHTML='<strong>'+col+'</strong>';
   else td.textContent=col;
   tr.append(td);
  });
  tbody.append(tr);
 }
 table.append(thead,tbody);
 tableWrap.append(table);
 c.append(tableWrap,node('p','上表为能力与证据边界说明，不是性能测量结果。下方固定任务的执行状态、退出码和持久回执分别展示；旧M5/M6保持原来源。','muted'));
 return c;
}

export function lab(initial,api){
 const store=api.store,all=node('div'),c=card('独立本机实验 · 固定任务'),pre=node('p',undefined,'warning'),label=node('label','实验种类'),select=node('select'),result=node('div');label.append(select);result.dataset.result='job';
 for(const[kind,text]of [['m6_rules','M6结构化规则（正常生产实例）'],['bench_T','T传统 · 3次有限smoke'],['bench_H','H混合 · 3次有限smoke'],['pbench','P公开混合 · 3次有限smoke']]){const o=node('option',text);o.value=kind;select.append(o)}
 function blocked(){return experimentBlockReason(store)}
 const start=button('开始实验',async()=>{
  await api.refreshState();const jobs=await api('/jobs');store.jobs=jobs;store.jobsKnown=true;store.lastJobsSuccess=store.now();const reason=blocked();if(reason)throw Error(reason);
  const r=await api('/jobs',{kind:select.value},{guard:blocked});store.jobs=[...store.jobs.filter(j=>j.job_id!==r.job_id),r].slice(-8);result.replaceChildren(node('p','任务已受理 '+r.job_id+'；尚无规则通过结论。'));store.emit();
 });
 const disconnect=button('确认断开全部live设备',async()=>{const run=store.snapshot?.run_id,yes=await confirmAction('全部live设备','断开全部设备','stop:'+run);if(!yes)return;if(run!==store.snapshot?.run_id)throw Error('运行已经改变');await api('/run/stop',{});await api.refreshState();api.notify('已重新读取断开状态；需要再次明确点击开始实验，不自动运行。')},'secondary');
 const setup=node('details');setup.append(node('summary','新建实验'),label,disconnect,start);c.append(node('p','实验与live互斥。固定任务不替代正式性能测量。','muted'),pre,setup,result);
 const history=card('历史 M6 结论 · 非本次任务结果');history.append(node('p','历史A/B共同规则均16/16，已知隔离缺陷均3/3；B提供更完整的统一双端审计字段，不是覆盖率、检出率或人工工时提高。'),node('p','当前任务结果需看下方真实退出码、验收器回执与原始输出。','muted'));
 const matrix=benchmarkComparisonCard();
 const jobs=jobTable(api);all.append(matrix,c,jobs,history);all.refresh=()=>{setText(pre,blocked()||'前置读取已满足，可明确启动独立实验');guardButton(start,blocked);guardButton(disconnect,()=>store.authValid()?'':'本机授权不可用');jobs.refresh()};all.refresh();return all;
}

export function jobTable(api){
 const store=api.store,c=card('本次任务与真实退出结果'),empty=node('p'),rows=node('div'),records=new Map();c.append(empty,rows);
 c.refresh=()=>{setText(empty,!store.jobsKnown?'读取任务中／任务状态未知':!store.jobs.length?'本次没有任务；历史测量不会作为本次结果。':'任务退出与规则通过分别显示。');for(const j of store.jobs){
  let r=records.get(j.job_id);if(!r){const section=node('section',undefined,'job-row'),title=node('p'),status=node('p'),gridBox=node('div'),details=node('details'),pre=node('pre');details.append(node('summary','任务来源与部分原始输出'),pre);const cancel=button('取消任务 '+j.kind,async()=>{const current=store.jobs.find(x=>x.job_id===j.job_id);if(!current||!['queued','running'].includes(current.state))throw Error('该任务已经结束');await api('/jobs/'+j.job_id+'/cancel',{});api.notify('已提交任务取消，真实退出码与部分输出保留。')},'secondary');section.append(title,status,gridBox,cancel,details);r={section,title,status,gridBox,pre,cancel};records.set(j.job_id,r);rows.append(section)}
  setText(r.title,j.kind+' · '+j.run_id+' · '+j.job_id);setText(r.status,jobResultMessage(j));setText(r.pre,JSON.stringify(publicDetails(j),null,2));r.cancel.hidden=!['queued','running'].includes(j.state);guardButton(r.cancel,()=>store.fresh()?'':'当前状态过期');
  const hl=parseJobHighlights(j);
  if(hl){
   r.gridBox.className='details-grid';r.gridBox.style.margin='10px 0';
   r.gridBox.replaceChildren();
   for(const m of hl.metrics){
    const box=node('div');box.append(node('span',m.label),node('strong',m.value));r.gridBox.append(box);
   }
  }else{r.gridBox.replaceChildren()}
 }};
 c.refresh();return c;
}
