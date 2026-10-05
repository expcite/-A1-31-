import {node,card,button,field,fieldFeedback,badge,readButton,guardButton,setText,updateBadge} from './components.js';
import {secondsToMillis,operationMessage,temperature} from './usability.js';
import {confirmAction} from './confirm.js';
import {guidePanel} from './guide.js';
export function communication(initial,api){
 const store=api.store,all=node('div'),top=node('section',undefined,'connection-strip');
 const label=node('label','当前设备'),select=node('select');select.id='workbench-device';label.append(select);
 const connection=badge('unknown'),mode=node('span'),gateway=node('span'),latest=node('p',undefined,'muted'),info=node('details'),tech=node('p',undefined,'break-id');
 info.append(node('summary','连接与授权技术详情'),tech);
 const connect=button('连接当前设备',async()=>{const id=store.selected,c=store.context(id);await api('/devices/'+id+'/connect',{}, {context:c,guard:()=>store.canWrite(id)});api.notify('连接调度已受理，请等待实际认证在线。');await api.refreshState()});
 const details=readButton('设备详情 →',()=>api.drawer.open('device',store.selected),'link-button');mode.className='badge mode-badge';gateway.className='strip-meta';latest.hidden=true;info.hidden=true;
 top.append(label,node('span',undefined,'strip-divider'),connection,mode,gateway,node('span',undefined,'fill'),connect,details,latest,info);all.append(top);
 const layout=node('div',undefined,'workbench-layout'),main=node('div'),taskPanel=node('section',undefined,'task-panel'),grid=node('div',undefined,'task-grid'),tabs=node('div',undefined,'action-tabs');grid.id='bidirectional';const cards={},inputs={},panels={};let activeAction='telemetry';
 const tabButtons={};
 function selectAction(kind){activeAction=kind;for(const k of ['telemetry','control']){panels[k].hidden=k!==kind;tabButtons[k].classList.toggle('selected',k===kind);tabButtons[k].setAttribute('aria-selected',String(k===kind));tabButtons[k].tabIndex=k===kind?0:-1}}
 tabs.setAttribute('role','tablist');tabs.setAttribute('aria-label','通信任务');
 for(const [kind,label] of [['telemetry','发送温度'],['control','设置间隔']]){const b=readButton(label,()=>selectAction(kind));b.id='tab-'+kind;b.dataset.actionTab=kind;b.setAttribute('role','tab');b.setAttribute('aria-controls','panel-'+kind);b.addEventListener('keydown',e=>{if(!['ArrowLeft','ArrowRight','Home','End'].includes(e.key))return;e.preventDefault();const next=e.key==='Home'?'telemetry':e.key==='End'?'control':kind==='telemetry'?'control':'telemetry';selectAction(next);tabButtons[next].focus()});tabs.append(b);tabButtons[kind]=b}taskPanel.append(tabs,grid);main.append(taskPanel);layout.append(main);
 for(const kind of ['telemetry','control']){
  const c=card(kind==='telemetry'?'设备 → 网关 · 发送温度':'网关 → 设备 · 设置上报间隔');c.dataset.action=kind;c.id='panel-'+kind;c.setAttribute('role','tabpanel');c.setAttribute('aria-labelledby','tab-'+kind);
  const target=node('p',undefined,'target-label'),[l,input]=field(kind==='telemetry'?'温度':'自定义秒数',kind==='telemetry'?'number':'text',kind==='telemetry'?'26.3':'5');
  input.id=kind==='telemetry'?'manual-temperature':'interval-seconds';input.autocomplete='off';input.maxLength=24;
  if(kind==='telemetry'){input.step='0.1';input.min='-50';input.max='150'}else input.inputMode='decimal';
  const validation=fieldFeedback(input),fieldWrap=node('div',undefined,'field-wrap');fieldWrap.append(input,node('span',kind==='telemetry'?'℃':'秒'));l.append(fieldWrap,validation.message);
  const submitted=node('p',undefined,'submitted'),out=node('div',undefined,'operation-result'),msg=node('p'),status=badge('unknown'),when=node('p',undefined,'muted'),opid=node('p',undefined,'break-id muted');
  const receiptTop=node('div',undefined,'receipt-top');receiptTop.append(node('span','本次操作回执','eyebrow'),status);out.dataset.result=kind;out.dataset.managed='1';out.setAttribute('aria-live','polite');out.append(receiptTop,msg,submitted,when,opid);opid.hidden=true;
  const evidence=readButton('查看详情与关联记录 →',()=>{const r=store.receipt(store.selected,kind);if(r?.operation_id)api.drawer.open('operation',r.operation_id,store.selected)},'link-button');evidence.hidden=true;out.append(evidence);
  const send=button(kind==='telemetry'?'发送温度':'设置上报间隔',async()=>{
   const id=store.selected,context=store.context(id),guideToken=api.guide.token();
   const value=validation.validate(kind==='telemetry'?temperature:secondsToMillis),requestID=crypto.randomUUID();
   const receipt={context,submitted:kind==='telemetry'?{temperature_c:value}:{interval_ms:value},request_id:requestID,local_status:'submitting'};store.setReceipt(id,kind,receipt);
   try{
    const body=kind==='telemetry'?{request_id:requestID,temperature_c:value}:{request_id:requestID,device_id:id,interval_ms:value};
    const o=await api(kind==='telemetry'?'/devices/'+id+'/telemetry':'/gateway/commands',body,{context,guard:()=>store.canWrite(id,{online:true})});
    if(o.run_id!==context.run||o.device_id!==id||!o.operation_id)throw Error('返回操作归属无法核验，未标为成功');
    receipt.operation_id=o.operation_id;receipt.result=o;receipt.local_status='received';api.guide.track(kind,o,guideToken);
    if(kind==='telemetry'&&api.guide.current?.rekey?.accepted&&o.sid!==api.guide.current.rekey.old_sid)api.guide.track('verify',o,guideToken);
   }catch(e){receipt.local_status=e.submissionUnknown?'unknown':'rejected';receipt.error=e.userMessage||e.message;throw e}
   finally{if(store.authValid()&&store.snapshot?.run_id===context.run&&store.epoch===context.epoch)store.setReceipt(id,kind,receipt)}
  });
  input.addEventListener('input',()=>store.setDraft(store.selected,kind,input.value));
  input.addEventListener('keydown',e=>{if(e.key==='Enter'){e.preventDefault();if(!send.disabled)send.click()}});
  const submitRow=node('div',undefined,'submit-row');submitRow.append(send,node('small',kind==='telemetry'?'通过设备安全通道发送':'由网关主动下发，等待实际设备确认'));
  c.append(target,l,node('p',kind==='telemetry'?'手动输入不会改变自动采样使用的模拟温度。':'0.1～60秒；严格换算为整数毫秒。申请值与当前生效值分别记录。','field-hint'),submitRow,out);
  let presets;
  if(kind==='control'){const presetLabel=node('label','申请上报间隔');presets=node('select');presets.id='interval-preset';presets.className='control-select';for(const n of [1,5,10,'自定义']){const o=node('option',n==='自定义'?'自定义间隔…':'每 '+n+' 秒上报一次');o.value=String(n);presets.append(o)}presets.value='5';l.hidden=true;presets.onchange=()=>{l.hidden=presets.value!=='自定义';if(presets.value!=='自定义'){input.value=presets.value;store.setDraft(store.selected,kind,input.value);validation.clear()}else input.focus()};presetLabel.append(presets);c.insertBefore(presetLabel,l)}
  cards[kind]={target,submitted,msg,status,when,opid,evidence,send,out,validation,presets,custom:l};inputs[kind]=input;panels[kind]=c;grid.append(c);
 }
 selectAction(activeAction);
 const sampling=card('自动采样与会话'),auto=node('p'),[sl,source]=field('自动采样源 ℃（本机模拟传感器）','number','25');source.id='auto-source';source.step='.1';source.addEventListener('input',()=>store.setDraft(store.selected,'sampling',source.value));
 const samplingButtons=[];
 for(const [autoValue,text] of [[false,'暂停采样'],[true,'开启采样 / 更新源']]){
  const b=button(text,async()=>{const id=store.selected,context=store.context(id);await api('/devices/'+id+'/sampling',{auto:autoValue,temperature_c:temperature(source.value)},{context,guard:()=>store.canWrite(id,{online:true})});api.notify('设备本地采样事务已有结果；这不是网关远端指令ACK。');await api.refreshState()},'secondary');samplingButtons.push(b);
 }
 const rekey=button('完整会话换钥',async()=>{
  const id=store.selected,context=store.context(id),token=api.guide.token();if(api.drawer.dialog.open)api.drawer.dialog.close();const yes=await confirmAction(id,'完整会话换钥','rekey:'+id);if(!yes)return;
  try{const request_id=crypto.randomUUID(),receipt=await api('/devices/'+id+'/rekey',{request_id}, {context,guard:()=>store.canWrite(id,{online:true})});if(receipt.rekey_id!==request_id||receipt.run_id!==context.run||receipt.device_id!==id||receipt.old_sid!==context.sid)throw Error('换钥回执归属无法核验，未标为完成');api.guide.rekey('accepted',context,token,receipt);api.notify('换钥已排队；将核验本次操作关联的双端新握手，再手动发送验证消息。')}catch(e){if(e.submissionUnknown)api.guide.rekey('unknown',context,token);throw e}
 },'secondary');rekey.id='workbench-rekey';samplingButtons.push(rekey);
 const disconnect=button('断开当前设备',async()=>{const id=store.selected,context=store.context(id);if(api.drawer.dialog.open)api.drawer.dialog.close();const yes=await confirmAction(id,'断开设备','disconnect:'+id);if(yes)await api('/devices/'+id+'/disconnect',{}, {context,guard:()=>store.canWrite(id)});await api.refreshState()},'secondary');samplingButtons.push(disconnect);
 sampling.append(auto,sl,...samplingButtons,node('p','暂停保持连接；断开结束连接；退出软件结束全部工作者。会话换钥是完整新握手，区别于长期身份轮换。','muted'));
 const eventCard=card('最近活动'),events=node('div',undefined,'recent-events'),eventEmpty=node('p','该设备尚无可关联的公开事件。','muted'),more=readButton('查看完整记录 →',()=>{location.hash='#logs'},'link-button');eventCard.append(events,eventEmpty,more);
 const contextCard=card('当前设备'),contextSummary=node('div',undefined,'context-metrics'),samplingOpen=readButton('采样与会话设置 →',()=>api.drawer.open('sampling',store.selected),'link-button'),contextValues={};
 for(const [key,text]of [['interval','上报间隔'],['auto','自动采样'],['source','自动采样源'],['received','最近网关接收']]){const pair=node('div',undefined,'metric-pair'),value=node('strong');pair.append(node('span',text),value);contextSummary.append(pair);contextValues[key]=value}
 const currentDevice=node('span',undefined,'badge'),contextHead=node('div',undefined,'context-heading');contextHead.append(contextCard.firstChild,currentDevice);contextCard.append(contextHead,contextSummary,samplingOpen,node('p','本机采样源与网关控制是不同操作。','context-note'));const contextColumn=node('aside',undefined,'context-column');contextColumn.setAttribute('aria-label','当前设备上下文');contextColumn.append(contextCard,eventCard);layout.append(contextColumn);
 api.drawer.setSampling(sampling);
 const guide=guidePanel(store,api,api.guide,i=>{if(i===1)selectAction('telemetry');if(i===2){api.drawer.open('sampling',store.selected);rekey.focus();return}const target=i===0?select:i===1?inputs.telemetry:guide;target.focus?.()});const guideNote=node('div',undefined,'guide-note'),guideOpen=readButton('打开指引 →',()=>{api.guide.enabled=!api.guide.enabled;if(api.guide.enabled)api.guide.select();api.guide.persist();store.emit()},'link-button');guideNote.append(node('span','第一次使用？按步骤了解双向通信。'),guideOpen);main.append(guideNote,guide);all.append(layout);
 let optionsKey='',lastDevice='',lastRun='',eventsKey='';
 select.onchange=()=>{store.selected=select.value;try{sessionStorage.setItem('selected_device',store.selected)}catch{};store.emit()};
 function refresh(){
  const s=store.snapshot;if(!s)return;const ids=s.devices.map(d=>d.id),key=JSON.stringify(ids);
  if(key!==optionsKey){optionsKey=key;select.replaceChildren(node('option','请选择设备'));select.options[0].value='';for(const id of ids){const o=node('option',id);o.value=id;select.append(o)}}select.value=store.selected;
  const id=store.selected,d=store.device();if(lastDevice!==id||lastRun!==s.run_id){for(const kind of ['telemetry','control']){inputs[kind].value=store.draft(id,kind,kind==='telemetry'?'26.3':'5');cards[kind].validation.clear();if(kind==='control'){const common=['1','5','10'].includes(inputs[kind].value);cards[kind].presets.value=common?inputs[kind].value:'自定义';cards[kind].custom.hidden=common}}source.value=store.draft(id,'sampling','25');lastDevice=id;lastRun=s.run_id;eventsKey=''}
  updateBadge(connection,store.fresh()?d?.status:'未知／上次观测');setText(mode,d?.sid?(d.suite===2?'混合安全通道':'可信策略允许的传统通道'):'尚无安全会话');setText(gateway,'网关 '+(s.gateway?.id||'未观测')+' · '+(s.gateway?.status||'未观测'));
  setText(latest,'设备当前间隔 '+(d?.interval_ms!=null?d.interval_ms/1000+'秒':'未观测')+' · 网关最近接收 '+(d?.last_received_temperature_c!=null?d.last_received_temperature_c+'℃':'未观测'));
  setText(tech,'run '+s.run_id+' · SID '+(d?.sid||'未观测')+' · 授权剩余 '+Math.max(0,Math.ceil((store.expiry-store.wall())/60000))+'分钟');
  setText(auto,'自动采样 '+(d?.auto?'开启':'暂停／未观测')+' · 自动源 '+(d?.temperature_c??'未观测')+'℃');
  setText(currentDevice,id||'未选择');setText(contextValues.interval,d?.interval_ms!=null?d.interval_ms/1000+' 秒':'未观测');setText(contextValues.auto,d?.auto===true?'进行中':d?.auto===false?'已暂停':'未观测');setText(contextValues.source,d?.temperature_c!=null?d.temperature_c+' ℃':'未观测');setText(contextValues.received,d?.last_received_temperature_c!=null?d.last_received_temperature_c+' ℃':'未观测');contextColumn.dataset.stale=store.fresh()?'0':'1';connect.hidden=['online','connecting'].includes(d?.status);samplingOpen.disabled=!id||!store.authValid();
  for(const kind of ['telemetry','control']){
   const v=cards[kind],r=store.receipt(id,kind),o=r?.operation_id?store.operation(r.operation_id):null,evicted=r?.operation_id&&!o;
   delete v.out.dataset.error;setText(v.target,(kind==='telemetry'?(id||'未选择')+' → '+(s.gateway?.id||'网关未观测'):(s.gateway?.id||'网关未观测')+' → '+(id||'未选择'))+' · '+(kind==='telemetry'?'设备安全上报':'网关主动控制'));setText(v.submitted,r?'本次已提交：'+(kind==='telemetry'?r.submitted.temperature_c+'℃':r.submitted.interval_ms/1000+'秒'):'申请值、已提交值和实际接收值分别展示。');
   setText(v.msg,o?operationMessage(o):evicted?'本次操作ID尚未出现在当前快照，结果未知；等待新快照或查看历史回执，不重新发送':r?.local_status==='unknown'?'提交结果未知，可能已执行；未自动重发；尚无operation_id可关联查询':r?.error||(r?'正在提交本机请求，尚无通信确认':'尚未发送'));
   v.status.hidden=false;updateBadge(v.status,o?.status||(r?.local_status==='unknown'?'提交结果未知':r?.local_status==='rejected'?'failed':r?'等待关联观测':'尚未提交'));setText(v.when,'确认时间：'+(o?.confirmed_at?new Date(o.confirmed_at).toLocaleTimeString():'未观测'));setText(v.opid,'操作ID：'+(r?.operation_id||'未观测'));v.evidence.hidden=!r?.operation_id;guardButton(v.send,()=>store.selected?store.canWrite(store.selected,{online:true}):'请选择设备');
  }
  guardButton(connect,()=>store.canWrite(store.selected)||(!store.selected?'请选择设备':['online','connecting'].includes(store.device()?.status)?'已有连接／连接中':''));
  for(const b of samplingButtons)guardButton(b,()=>store.selected?store.canWrite(store.selected,{online:b!==disconnect}):'请选择设备');
  const recent=s.events.filter(e=>e.device_id===id).slice(-3),ek=recent.map(e=>e.event_id).join('|');eventEmpty.hidden=recent.length>0;
  // Preserve event controls too; keyed rows are retained as the rolling window changes.
  for(const child of [...events.children])if(!recent.some(e=>String(e.event_id)===child.dataset.event)){if(child.contains(document.activeElement))more.focus();child.remove()}
  if(ek!==eventsKey){eventsKey=ek;const eventLabels={authenticated:'安全握手已认证',telemetry_accepted:'网关验收温度',confirmed:'收到确认',queued:'操作已排队',local_sampling:'设备本地采样调整',peer_processed:'设备已处理',telemetry_written:'温度写出结果',control_write_result:'控制写出结果',offline:'连接已结束',unconfirmed:'操作尚未确认'};for(const e of recent){if([...events.children].some(n=>n.dataset.event===String(e.event_id)))continue;const p=node('div',undefined,'event-row');p.dataset.event=String(e.event_id);p.append(node('span',new Date(e.at).toLocaleTimeString()),node('span',eventLabels[e.stage]||e.stage),readButton('事件详情 '+e.event_id,()=>api.drawer.open('event',e.event_id,id)));events.prepend(p)}}
  guide.refresh();setText(guideOpen,api.guide.enabled?'收起指引':'打开指引 →');
 }
 all.refresh=refresh;refresh();return all;
}
