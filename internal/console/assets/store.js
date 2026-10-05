// Presentation state only. Authenticated API snapshots are the sole business source.
export const FRONTEND_VERSION = 'MINIMAL-UI-v1.0';
export const READ_TIMEOUT_MS = 3000, STALE_MS = 3500, POLL_MS = 1000;
export class ConsoleStore {
  constructor({now=()=>performance.now(), wall=()=>Date.now()}={}) {
    this.now=now; this.wall=wall; this.snapshot=null; this.lastSuccess=-Infinity;
    this.expiry=null; this.authorized=false; this.epoch=0; this.sequence=0;
    this.accepted=0; this.retiredRuns=new Set(); this.listeners=new Set();
    this.drafts=new Map(); this.receipts=new Map(); this.selected='';
    this.build=null; this.jobs=[]; this.jobsKnown=false;
  }
  subscribe(fn){this.listeners.add(fn);return()=>this.listeners.delete(fn)}
  emit(){for(const fn of this.listeners)fn(this)}
  authorize(expires){this.expiry=Date.parse(expires);this.authorized=Number.isFinite(this.expiry)&&this.expiry>this.wall();this.epoch++;this.emit()}
  revoke(){this.authorized=false;this.expiry=null;this.epoch++;this.snapshot=null;this.lastSuccess=-Infinity;this.drafts.clear();this.receipts.clear();this.jobs=[];this.jobsKnown=false;this.build=null;this.emit()}
  authValid(){return this.authorized&&this.expiry>this.wall()}
  fresh(){return this.authValid()&&!!this.snapshot&&!this.snapshot.error&&this.now()-this.lastSuccess<STALE_MS}
  ticket(){return {sequence:++this.sequence,epoch:this.epoch}}
  accept(s,t){
    if(!this.authValid()||t.epoch!==this.epoch||t.sequence!==this.sequence||t.sequence<=this.accepted)return false;
    if(!s||typeof s.run_id!=='string'||!Array.isArray(s.devices)||!Array.isArray(s.operations)||!Array.isArray(s.events)||this.retiredRuns.has(s.run_id))return false;
    const previous=this.snapshot;
    if(previous?.run_id===s.run_id&&typeof previous.cursor==='number'&&typeof s.cursor==='number'&&s.cursor<previous.cursor)return false;
    if(previous&&previous.run_id!==s.run_id){this.retiredRuns.add(previous.run_id);if(this.retiredRuns.size>16)this.retiredRuns.delete(this.retiredRuns.values().next().value);this.drafts.clear();this.receipts.clear();this.selected='';this.jobs=[];this.jobsKnown=false}
    this.snapshot=s;this.accepted=t.sequence;this.lastSuccess=this.now();
    if(!s.devices.some(d=>d.id===this.selected))this.selected=this.selected?'':s.devices[0]?.id||'';
    this.emit();return true;
  }
  device(id=this.selected){return this.snapshot?.devices.find(d=>d.id===id)}
  key(device,action){return [this.snapshot?.run_id||'',device,action].join('|')}
  draft(device,action,fallback=''){return this.drafts.get(this.key(device,action))??fallback}
  setDraft(device,action,value){this.drafts.set(this.key(device,action),String(value))}
  receipt(device,action){return this.receipts.get(this.key(device,action))}
  setReceipt(device,action,value){if(this.receipts.size>=256&&!this.receipts.has(this.key(device,action)))this.receipts.delete(this.receipts.keys().next().value);this.receipts.set(this.key(device,action),value);this.emit()}
  context(id){const d=this.device(id);return {epoch:this.epoch,run:this.snapshot?.run_id,device:id,sid:d?.sid||''}}
  matches(c){return c.epoch===this.epoch&&c.run===this.snapshot?.run_id&&!!this.device(c.device)&&c.sid===(this.device(c.device)?.sid||'')}
  canWrite(id,{online=false,maintenance=false}={}){
    if(!this.fresh())return '当前状态已过期或授权不可用，请刷新／重新授权';
    if(!maintenance&&this.snapshot.maintenance?.admission_blocked)return '网关维护或准入关闭；不会自动恢复设备';
    if(this.jobs.some(j=>['queued','running'].includes(j.state)))return '隔离实验正在运行，通信操作暂不可用';
    if(id){const d=this.device(id);if(!d)return '目标设备已移除';if(d.lifecycle&&d.lifecycle!=='ACTIVE')return '身份已吊销或退役，不能恢复通信';if(online&&d.status!=='online')return '设备尚未认证在线，请先连接'}
    return '';
  }
  operation(ref){return this.snapshot?.operations.find(o=>o.operation_id===ref&&o.run_id===this.snapshot.run_id)}
}
