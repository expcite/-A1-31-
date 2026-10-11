// Read-only observation of the existing authenticated snapshot; never retries a POST.
export function inspectBatchOutcome(store,context,kind,receipt){
 const pending=(text='已受理，等待真实观测')=>({status:'pending',text});
 const unknown=text=>({status:'unknown',text});
 if(!store.authValid()||store.epoch!==context.epoch||store.snapshot?.run_id!==context.run)return unknown('运行或授权已改变，结果未知；未自动重发');
 if(!store.fresh())return unknown('快照过期，结果未知；未自动重发');
 const d=store.device(context.device);if(!d)return unknown('目标已移除，结果未知');
 if(kind==='connect')return d.status==='online'&&d.sid?{status:'observed',text:'已观测认证在线；此接口无本次连接因果回执'}:d.error?{status:'failed',text:'连接观测失败：'+d.error}:pending('连接已受理，等待认证在线');
 if(kind==='disconnect')return d.status==='offline'?{status:'observed',text:'已观测本机断开；不是远端ACK'}:pending('本机断开已返回，等待离线观测');
 const rekey=kind==='rekey',field=rekey?'rekey_id':'operation_id';
 if(!receipt||receipt.run_id!==context.run||receipt.device_id!==context.device||(rekey?receipt.old_sid:receipt.sid)!==context.sid||!receipt[field])return unknown('返回回执归属无法核验，未标为完成');
 const list=rekey?store.snapshot.rekeys:store.snapshot.operations;
 const r=(list||[]).find(x=>x[field]===receipt[field]&&x.run_id===context.run&&x.device_id===context.device&&(rekey?x.old_sid:x.sid)===context.sid&&(rekey||x.kind===kind));
 if(!r){if(d.sid!==context.sid)return unknown('会话已改变且原请求缺少终态证据；未重发');return pending('已受理，等待本次ID出现在真实快照');}
 if(['failed','cancelled'].includes(r.status))return {status:r.status,text:(r.status==='failed'?'失败：':'已取消：')+(r.error||'见本次回执')};
 if(rekey){
  if(r.status==='confirmed'&&r.new_sid&&r.new_sid!==r.old_sid&&r.new_sid===d.sid&&r.started_at&&r.client_authenticated_at&&r.gateway_authenticated_at&&r.completed_at)return {status:'confirmed',text:'本次换钥双端新握手已确认；业务验证另行执行'};
  return pending('换钥已受理，等待本次双端新握手证据');
 }
 if(['confirmed','late_confirmed'].includes(r.status)){
  if(!r.written_at||!r.confirmed_at)return unknown('确认字段不足，未补写成功');
  if(kind==='control'){
   if(!r.peer_processed_at||r.applied!==true)return unknown('控制处理/ACK字段不足，未补写成功');
  }else if(kind==='telemetry'){
   // Telemetry has gateway acceptance, not a device control ACK. Do not invent
   // peer_processed_at or applied. Require the actual protected receive event.
   const accepted=(store.snapshot.events||[]).some(e=>e.run_id===context.run&&e.device_id===context.device&&e.sid===context.sid&&e.operation_id===r.operation_id&&e.endpoint==='gateway'&&e.stage==='telemetry_accepted'&&e.sample_id===r.sample_id&&Number.isSafeInteger(r.sample_id)&&r.sample_id>0&&Number.isFinite(r.temperature_c)&&e.temperature_c===r.temperature_c&&e.at);
   if(!accepted)return unknown('缺少本次网关验收事件，未补写成功');
   return {status:'confirmed',text:'本次温度已由网关安全验收；不是设备控制ACK'};
  }else return unknown('未知业务类型，未补写成功');
  return {status:'confirmed',text:r.status==='late_confirmed'?'本次操作迟到确认':'本次操作已确认'};
 }
 if(r.status==='unconfirmed')return unknown('未确认：设备可能已执行；未自动重发');
 return pending({queued:'已排队，等待写出',writing:'正在写出，尚未确认',written:'已写出，等待对端确认'}[r.status]||'等待本次处理证据');
}
export function waitForBatchOutcome(store,context,kind,receipt,{timeoutMs=12000,onUpdate=()=>{}}={}){
 return new Promise(resolve=>{
  let done=false,timer,unsubscribe=()=>{};
  const finish=r=>{if(done)return;done=true;clearTimeout(timer);unsubscribe();resolve(r)};
  const check=()=>{if(done)return;const r=inspectBatchOutcome(store,context,kind,receipt);onUpdate(r);if(r.status!=='pending')finish(r)};
  unsubscribe=store.subscribe?.(check)||(()=>{});
  timer=setTimeout(()=>finish({status:'unknown',text:'等待超时，未确认；设备可能已执行，未自动重发'}),timeoutMs);
  check();
 });
}
