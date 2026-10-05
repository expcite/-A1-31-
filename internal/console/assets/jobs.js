import {node,card,button,readButton,setText,guardButton,publicDetails} from './components.js';
import {confirmAction} from './confirm.js';
export function experimentBlockReason(store){if(!store.fresh())return '状态过期或未授权';const active=store.snapshot.devices.filter(d=>['online','connecting'].includes(d.status));if(active.length)return '仍有 '+active.length+' 台在线／连接中设备，请明确断开';if(!store.jobsKnown||store.now()-store.lastJobsSuccess>=3500)return '当前任务状态未完成读取';if(store.jobs.some(j=>['queued','running'].includes(j.state)))return '有互斥实验任务运行中';if(store.jobs.length>=8)return '本运行8个任务额度已满，请保留证据后新运行';return ''}
export function jobResultMessage(j){return '执行状态 '+j.state+' · 退出码 '+(j.exit_code??'未观测')+' · 验收器结果 '+(j.receipt_status||'未核对')+(j.error?' · '+j.error:'')}
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
 const jobs=jobTable(api);all.append(c,jobs,history);all.refresh=()=>{setText(pre,blocked()||'前置读取已满足，可明确启动独立实验');guardButton(start,blocked);guardButton(disconnect,()=>store.authValid()?'':'本机授权不可用');jobs.refresh()};all.refresh();return all;
}
export function jobTable(api){
 const store=api.store,c=card('本次任务与真实退出结果'),empty=node('p'),rows=node('div'),records=new Map();c.append(empty,rows);
 c.refresh=()=>{setText(empty,!store.jobsKnown?'读取任务中／任务状态未知':!store.jobs.length?'本次没有任务；历史测量不会作为本次结果。':'任务退出与规则通过分别显示。');for(const j of store.jobs){
  let r=records.get(j.job_id);if(!r){const section=node('section',undefined,'job-row'),title=node('p'),status=node('p'),details=node('details'),pre=node('pre');details.append(node('summary','任务来源与部分原始输出'),pre);const cancel=button('取消任务 '+j.kind,async()=>{const current=store.jobs.find(x=>x.job_id===j.job_id);if(!current||!['queued','running'].includes(current.state))throw Error('该任务已经结束');await api('/jobs/'+j.job_id+'/cancel',{});api.notify('已提交任务取消，真实退出码与部分输出保留。')},'secondary');section.append(title,status,cancel,details);r={section,title,status,pre,cancel};records.set(j.job_id,r);rows.append(section)}
  setText(r.title,j.kind+' · '+j.run_id+' · '+j.job_id);setText(r.status,jobResultMessage(j));setText(r.pre,JSON.stringify(publicDetails(j),null,2));r.cancel.hidden=!['queued','running'].includes(j.state);guardButton(r.cancel,()=>store.fresh()?'':'当前状态过期')
 }};
 c.refresh();return c;
}
