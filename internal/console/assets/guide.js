import {node,card,readButton,button,setText} from './components.js';
import {filterEvents,operationNames} from './usability.js';
export const guideSteps=['设备接入','双向通信','完整会话换钥与恢复','证据总结'];
const confirmed=o=>o&&['confirmed','late_confirmed'].includes(o.status);
const selectionFields=['run_id','from_event_id','to_event_id','device_id','endpoint','stage'];
export function sameSelection(a,b){return !!a&&!!b&&selectionFields.every(k=>(a[k]??'')===(b[k]??''))}
export function guideResults(g,s,fresh){
  if(!s||g.run_id!==s.run_id)return ['历史上下文，未重新核验','未重新核验','未重新核验','未重新核验'];
  const d=s.devices.find(d=>d.id===g.device_id);
  const op=(id,kind)=>s.operations.find(o=>o.operation_id===id&&o.kind===kind&&o.run_id===g.run_id&&o.device_id===g.device_id);
  const left=op(g.refs.telemetry,'telemetry'),right=op(g.refs.control,'control'),verify=op(g.refs.verify,'telemetry');
  const g1=!fresh?'状态过期／未知':d?.status==='online'?(g.online_at_entry?'已有连接，当前条件已满足':'当前已认证在线；本次连接创建因果未独立观测'):'尚未认证在线';
  const describe=(o,id)=>!id?'尚未执行':!o?'记录淘汰，无法重新核验':operationNames[o.status]||'未知状态 '+o.status;
  const g2=confirmed(left)&&confirmed(right)?'双向操作已确认（仅本次关联手动操作）':'温度：'+describe(left,g.refs.telemetry)+'；间隔：'+describe(right,g.refs.control);
  // A changed SID alone remains insufficient. Only the retained receipt for
  // this request and a subsequently confirmed new-SID message complete G3.
  const rekey=(s.rekeys||[]).find(r=>r.rekey_id===g.rekey?.rekey_id&&r.run_id===g.run_id&&r.device_id===g.device_id&&r.old_sid===g.rekey.old_sid);
  const linked=rekey?.status==='confirmed'&&rekey.new_sid&&rekey.new_sid!==rekey.old_sid&&rekey.new_sid===d?.sid&&rekey.started_at&&rekey.client_authenticated_at&&rekey.gateway_authenticated_at&&rekey.completed_at;
  const verified=linked&&confirmed(verify)&&verify.sid===rekey.new_sid&&Date.parse(verify.queued_at)>=Date.parse(rekey.completed_at)&&Date.parse(verify.confirmed_at)>=Date.parse(verify.queued_at);
  const g3=!fresh?'状态过期／未知；本次换钥尚未重新核验':verified?'本次完整换钥双端准入已关联，新SID验证消息已确认':linked?'本次完整换钥双端准入已关联；等待手动发送新SID验证消息':rekey?.status==='failed'?'本次换钥失败：'+rekey.error:g.rekey?.accepted&&d?.sid&&d.sid!==g.rekey.old_sid?
    '新会话已观测，但本次换钥关联证据不足'+(confirmed(verify)&&verify.sid===d.sid?'；新SID验证消息已确认':'；新SID验证消息尚未确认'):
    g.rekey?.unknown?'换钥提交结果未知；不会自动重发':g.rekey?'本次请求已受理，等待新会话观测':'尚未执行；需要明确换钥与新会话验证消息';
  const g4=g.export?.verified?'导出已生成；不推断文件已保存到磁盘':g.export_ref?'已有导出公开引用，刷新后未重新核验生成回执':g.fixed_selection?'范围已锁定，等待明确导出':g.summary_viewed?'摘要已查看，尚未导出':'尚未查看摘要／导出';
  return [g1,g2,g3,g4].map((v,i)=>g.skipped.includes(i)?'已跳过／未执行 · '+v:v);
}
export class GuideController{
  constructor(store,storage){this.store=store;this.storage=storage;this.contexts=new Map();this.enabled=false;this.current=null;this.restorePending=false;
    try{const saved=JSON.parse(storage?.getItem('edgehybrid_bplus_guide')||'null');if(saved?.schema==='bplus-guide-v1'&&Array.isArray(saved.contexts)){for(const g of saved.contexts.slice(-20))if(typeof g.run_id==='string'&&typeof g.device_id==='string'&&g.refs&&Array.isArray(g.skipped))this.contexts.set(g.run_id+'|'+g.device_id,g);this.enabled=saved.enabled===true;this.restorePending=true}}catch{}
  }
  select(){const s=this.store.snapshot;if(!s)return null;const key=s.run_id+'|'+this.store.selected;let g=this.contexts.get(key);if(!g){g={guide_session_id:crypto.randomUUID(),run_id:s.run_id,device_id:this.store.selected,entry_sid:this.store.device()?.sid||'',online_at_entry:this.store.device()?.status==='online',step_index:0,refs:{},skipped:[]};this.contexts.set(key,g);if(this.contexts.size>20)this.contexts.delete(this.contexts.keys().next().value)}this.current=g;this.restorePending=false;this.persist();return g}
  persist(){
    // Only public references and reading position. Drafts, payloads and auth never persist.
    const contexts=[...this.contexts.values()].map(g=>({schema:'bplus-guide-context-v1',guide_session_id:g.guide_session_id,run_id:g.run_id,device_id:g.device_id,entry_sid:g.entry_sid,online_at_entry:g.online_at_entry,step_index:g.step_index,refs:{...g.refs},skipped:[...g.skipped],rekey:g.rekey?{old_sid:g.rekey.old_sid,rekey_id:g.rekey.rekey_id,accepted:g.rekey.accepted===true,unknown:g.rekey.unknown===true}:undefined,fixed_selection:g.fixed_selection,summary_viewed:g.summary_viewed===true,export_ref:g.export?.export_id||g.export_ref}));
    try{this.storage?.setItem('edgehybrid_bplus_guide',JSON.stringify({schema:'bplus-guide-v1',enabled:this.enabled,contexts}))}catch{}
  }
  token(){return this.enabled?this.select()?.guide_session_id:null}
  track(kind,o,token){const g=[...this.contexts.values()].find(g=>g.guide_session_id===token);if(!g||!['telemetry','control','verify'].includes(kind)||o.kind!==(kind==='verify'?'telemetry':kind)||o.run_id!==g.run_id||o.device_id!==g.device_id||!o.operation_id)return;g.refs[kind]=o.operation_id;this.persist()}
  rekey(result,context,token,receipt){const g=this.current;if(!this.enabled||!g||g.guide_session_id!==token||g.run_id!==context.run||g.device_id!==context.device)return;const valid=receipt?.run_id===context.run&&receipt.device_id===context.device&&receipt.old_sid===context.sid&&typeof receipt.rekey_id==='string';g.rekey={old_sid:context.sid,rekey_id:valid?receipt.rekey_id:undefined,accepted:result==='accepted',unknown:result==='unknown'};g.refs.verify=undefined;this.persist()}
  clear(){this.contexts.clear();this.current=null;this.enabled=false;this.restorePending=false;try{this.storage?.removeItem('edgehybrid_bplus_guide')}catch{}}
}
export function guidePanel(store,api,controller,focus){
  const c=card('四步演示引导 · 当前真实运行'),steps=node('div',undefined,'guide-steps'),explanation=node('p'),result=node('p',undefined,'guide-result'),actions=node('div',undefined,'toolbar'),summary=node('div'),exportResult=node('div');c.dataset.testid='guide';
  const explanationText=['选定目标设备，必要时明确点击连接。已在线只表示当前条件满足。','使用同一工作台手动发温度，再设置间隔；两条回执分别核验。','在采样与会话设置中明确换钥，再发送新SID验证消息。仅匹配本次换钥回执及双端真实准入才完成；缺失时保留证据不足。','查看本次逐步结果，锁定当前设备事件范围，再明确点击导出。'];
  const stepButtons=guideSteps.map((name,i)=>{const b=readButton('G'+(i+1)+' '+name,()=>{controller.select().step_index=i;refresh();focus(i)});steps.append(b);return b});
  actions.append(readButton('上一步',()=>{const g=controller.select();g.step_index=Math.max(0,g.step_index-1);refresh();focus(g.step_index)}),readButton('下一步',()=>{const g=controller.select();g.step_index=Math.min(3,g.step_index+1);refresh();focus(g.step_index)}),readButton('跳过本步',()=>{const g=controller.select();if(!g.skipped.includes(g.step_index))g.skipped.push(g.step_index);refresh()}));
  const lock=readButton('锁定当前设备证据范围',()=>{if(!store.fresh())throw Error('当前状态过期，请刷新后重新锁定范围');const g=controller.select(),s=store.snapshot;g.summary_viewed=true;g.fixed_selection={run_id:s.run_id,from_event_id:s.events[0]?.event_id??0,to_event_id:s.cursor,device_id:g.device_id};g.fixed_count=filterEvents(s.events,{device_id:g.device_id}).length;g.export=undefined;exportResult.replaceChildren();refresh()});
  const exp=button('导出本次引导证据 JSON',async()=>{const g=controller.select(),selection=g.fixed_selection;if(!selection)throw Error('请先锁定证据范围');exportResult.replaceChildren();const r=await api('/exports',{format:'json',include_payload:false,selection});if(!sameSelection(r.selection,selection)||!Number.isSafeInteger(r.event_count)||!/^\/api\/v1\/exports\/[a-zA-Z0-9_-]+$/.test(r.url)||!Number.isSafeInteger(r.bytes)||!/^[a-f0-9]{64}$/.test(r.sha256))throw Error('导出回执与请求范围或文件元信息不一致，未标为完成');g.export={verified:true,export_id:r.export_id};const a=node('a','下载已生成的证据 · '+r.event_count+'条 · '+r.bytes+'字节');a.href=r.url;a.download='';exportResult.append(a,node('p','SHA-256 '+r.sha256,'muted'));refresh()});
  const extras=readButton('附加：隔离安全实验',()=>{location.hash='#lab'});
  c.append(node('p','普通操作与引导共用真实接口；下一步、跳过及关闭引导不执行网络写操作。','muted'),steps,explanation,result,actions,summary,lock,exp,exportResult,extras);
  function refresh(){c.hidden=!controller.enabled;if(!controller.enabled)return;const g=controller.select();if(!g)return;const results=guideResults(g,store.snapshot,store.fresh());stepButtons.forEach((b,i)=>{b.classList.toggle('selected',i===g.step_index);b.setAttribute('aria-current',i===g.step_index?'step':'false')});setText(explanation,'本步操作：'+explanationText[g.step_index]);setText(result,'实际结果：'+results[g.step_index]);summary.replaceChildren(...results.map((r,i)=>node('p','G'+(i+1)+' · '+r)),node('p','这说明什么：只记录已有观察；HTTP受理、SID变化和浏览器时间都不单独证明远端执行。','muted'));lock.hidden=exp.hidden=exportResult.hidden=extras.hidden=g.step_index!==3;exp.disabled=!!exp.dataset.busy||!store.authValid()||!g.fixed_selection;lock.disabled=!store.fresh();controller.persist()}
  c.refresh=refresh;refresh();return c;
}
