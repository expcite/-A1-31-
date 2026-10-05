import {node,card,button,readButton,guardButton,badge,updateBadge,setText} from './components.js';
import {batchSummary} from './batch.js';
import {confirmAction} from './confirm.js';
export function devices(initial,api){
 const store=api.store,c=card('设备池与真实连接'),filterLabel=node('label','搜索设备'),filter=node('input'),count=node('p'),result=node('div');c.classList.add('device-page');filterLabel.append(filter);filter.placeholder='设备编号或连接状态';result.dataset.result='batch';
 const connect=button('连接本次全部设备',async()=>{const r=await api('/run/start',{}, {guard:()=>store.canWrite()});const details=node('details');details.append(node('summary','查看逐设备结果（失败优先）'));for(const item of [...r.results||[]].sort((a,b)=>(a.status==='accepted')-(b.status==='accepted')))details.append(node('p',item.device_id+' · '+item.status+(item.error?' · '+item.error:'')));result.replaceChildren(node('p',batchSummary(r)),details);await api.refreshState()});
 const stop=button('断开本次全部设备',async()=>{const run=store.snapshot.run_id,yes=await confirmAction('全部 '+store.snapshot.devices.length+' 台设备','断开全部设备','stop:'+run);if(yes){if(run!==store.snapshot?.run_id)throw Error('运行已改变，未发送旧批次动作');await api('/run/stop',{});result.replaceChildren(node('p','本机断开请求已返回；实际连接状态继续读取。'));await api.refreshState()}},'secondary');
 const wrap=node('div',undefined,'table-wrap'),table=node('table'),thead=node('thead'),head=node('tr'),tbody=node('tbody');for(const h of ['设备','连接','模式','采样 / 间隔','最近接收','操作'])head.append(node('th',h));thead.append(head);table.append(thead,tbody);wrap.append(table);const rows=new Map();
 const batchMenu=node('details',undefined,'overflow'),menu=node('div',undefined,'menu'),toolbar=node('div',undefined,'toolbar'),summaryRow=node('div',undefined,'summary-row');menu.append(connect,stop);batchMenu.append(node('summary','批量操作 ⌄'),menu);toolbar.append(filterLabel,node('span',undefined,'fill'),batchMenu);summaryRow.append(count);c.append(toolbar,summaryRow,result,wrap,node('p','准入受理不代表认证在线。详情在侧栏打开；维护操作按设备展开。','table-foot'));
 function make(id){
  const tr=node('tr');tr.dataset.device=id;const name=node('td'),nameButton=readButton(id,()=>api.drawer.open('device',id),'link-button'),status=badge('unknown'),mode=node('td',undefined,'muted'),statusCell=node('td'),sampling=node('td'),received=node('td'),actions=node('td');name.append(nameButton);statusCell.append(status);
  const conn=button('连接 '+id,async()=>{await api('/devices/'+id+'/connect',{}, {context:store.context(id),guard:()=>store.canWrite(id)});api.notify(id+' 已受理连接调度，等待认证在线。')},'secondary');
  const view=readButton('查看 '+id,()=>api.drawer.open('device',id),'link-button'),more=node('details'),summary=node('summary','更多操作 '+id);more.append(summary);
  const disc=button('断开 '+id,async()=>{const context=store.context(id),yes=await confirmAction(id,'断开设备','disconnect:'+id);if(yes)await api('/devices/'+id+'/disconnect',{}, {context,guard:()=>store.canWrite(id)})},'secondary');
  const rekey=button('完整换钥 '+id,async()=>{const context=store.context(id),yes=await confirmAction(id,'完整会话换钥','rekey:'+id);if(yes)await api('/devices/'+id+'/rekey',{}, {context,guard:()=>store.canWrite(id,{online:true})})},'secondary');
  more.append(conn,disc,rekey);actions.append(view,more);tr.append(name,statusCell,mode,sampling,received,actions);return {tr,status,mode,sampling,received,conn,disc,rekey};
 }
 function refresh(){
  const s=store.snapshot;if(!s)return;const ids=new Set(s.devices.map(d=>d.id));
  for(const[id,r]of rows)if(!ids.has(id)){if(r.tr.contains(document.activeElement))filter.focus();r.tr.remove();rows.delete(id)}
  for(const d of s.devices){
   let r=rows.get(d.id);if(!r){r=make(d.id);rows.set(d.id,r);tbody.append(r.tr)}
   r.tr.hidden=!(d.id+' '+d.status).toLowerCase().includes(filter.value.toLowerCase());updateBadge(r.status,store.fresh()?d.status:'未知／上次观测');setText(r.mode,d.sid?(d.suite===2?'混合':'传统 · 可信策略'):(d.error||'尚无安全会话'));setText(r.sampling,(d.interval_ms/1000)+'秒 / '+(d.auto?'采样中':'已暂停'));setText(r.received,d.last_received_temperature_c!=null?d.last_received_temperature_c+'℃':'未观测');
   guardButton(r.conn,()=>store.canWrite(d.id)||(['online','connecting'].includes(store.device(d.id)?.status)?'已有连接／连接中':''));guardButton(r.disc,()=>store.canWrite(d.id));guardButton(r.rekey,()=>store.canWrite(d.id,{online:true}));
  }
  setText(count,'当前设备池 '+s.devices.length+' 台 · 在线 '+s.devices.filter(d=>d.status==='online').length+' 台');guardButton(connect,()=>store.canWrite());guardButton(stop,()=>store.authValid()?'':'本机授权不可用');
 }
 filter.oninput=refresh;c.refresh=refresh;refresh();return c;
}
