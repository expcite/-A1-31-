import {node,readButton,publicDetails,setText,badge,updateBadge,button,guardButton} from './components.js';
import {operationMessage} from './usability.js';
// One persistent dialog; refresh replaces text, never the focused controls.
export function createDrawer(store,api){
  const dialog=node('dialog',undefined,'detail-drawer'),title=node('h3'),summary=node('p'),status=badge('unknown'),stages=node('div'),raw=node('pre'),close=readButton('关闭详情',()=>dialog.close());
  dialog.setAttribute('aria-label','关联证据详情');let reference=null,origin=null;const samplingSlot=node('div');samplingSlot.hidden=true;
  const cancel=button('取消尚未写出的队列项',async()=>{
    const o=current();if(!o||o.status!=='queued')throw Error('操作已不在可取消队列；已写出动作无法保证撤回');
    await api('/operations/'+o.operation_id+'/cancel',{}, {guard:()=>current()?.status==='queued'?'':'操作状态已经改变'});await api.refreshState();
  },'secondary');cancel.hidden=true;
  const heading=node('div',undefined,'drawer-heading');heading.append(title,close);const technical=node('details');technical.append(node('summary','技术详情与公开原始记录'),stages,cancel,raw);dialog.append(heading,status,summary,samplingSlot,technical);document.body.append(dialog);
  dialog.addEventListener('close',()=>{reference=null;if(origin?.isConnected)origin.focus()});
  function current(){
    const s=store.snapshot;if(!reference||reference.run!==s?.run_id)return null;
    return reference.kind==='run'?s:reference.kind==='operation'?s.operations.find(o=>o.operation_id===reference.id&&o.device_id===reference.device):['device','sampling'].includes(reference.kind)?s.devices.find(d=>d.id===reference.id):s.events.find(e=>e.event_id===reference.id);
  }
  function refresh(){
    if(!reference)return;if(!store.authValid()){dialog.close();raw.textContent='';return}
    const value=current();samplingSlot.hidden=reference.kind!=='sampling';title.textContent=(reference.kind==='operation'?'操作':['device','sampling'].includes(reference.kind)?'设备':'事件')+' · '+(reference.device||reference.id);
    if(!value){setText(summary,'关联记录已经淘汰或运行已改变，无法重新核验；不会复用其他记录。');raw.textContent='未观测';cancel.hidden=true;updateBadge(status,'unknown');return}
    setText(summary,reference.kind==='run'?'本次运行 '+value.run_id+' · 容量 '+JSON.stringify(value.capacity):reference.kind==='operation'?operationMessage(value):['device','sampling'].includes(reference.kind)?(store.fresh()?'当前设备观测':'状态过期／上次设备观测'):(value.stage+' · '+value.detail));
    updateBadge(status,store.fresh()?value.status||value.stage||'观测':value.status?value.status:'未知／上次观测');
    const rows=reference.kind==='operation'?[['排队',value.queued_at],['写出',value.written_at],['对端处理',value.peer_processed_at],['确认',value.confirmed_at]]:[['会话开始',value.session_started_at],['事件时间',value.at]];
    stages.replaceChildren(...rows.map(([label,time])=>node('p',label+'：'+(time?new Date(time).toLocaleString():'未观测'))),node('p','SM2 S1/S2 独立阶段：未观测。协议能力说明不等于本条记录的阶段证据。','muted'));
    setText(raw,JSON.stringify(publicDetails(value),null,2));cancel.hidden=reference.kind!=='operation'||value.status!=='queued';guardButton(cancel,()=>!store.fresh()?'当前状态过期':current()?.status==='queued'?'':'已写出或状态不可取消');
  }
  return {setSampling(panel){samplingSlot.replaceChildren(panel)},open(kind,id,device=''){const s=store.snapshot;if(!store.authValid()||!s)return;reference={kind,id,device,run:s.run_id};origin=document.activeElement;refresh();if(!dialog.open)dialog.showModal();close.focus()},refresh,clear(){if(dialog.open)dialog.close();raw.textContent='';stages.replaceChildren();samplingSlot.replaceChildren()},dialog};
}
