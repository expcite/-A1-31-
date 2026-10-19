// Read-only presentation. Missing observations stay unknown.
import {operationMessage} from './usability.js';
export const observedNumber=v=>typeof v==='number'&&Number.isFinite(v)&&v>=0;
export function bytes(v){if(!observedNumber(v))return '未观测';if(v<1024)return v+' B';return v<1048576?(v/1024).toFixed(1)+' KiB':(v/1048576).toFixed(1)+' MiB';}
export function timeLabel(v){if(!v||String(v).startsWith('0001-'))return '未观测';const d=new Date(v);return Number.isNaN(d.getTime())?'未观测':d.toLocaleString();}
const num=(v,suffix='')=>observedNumber(v)?v+suffix:'未观测';
const stages={queued:'已受理',written:'已写出',peer_processed:'对端已处理',confirmed:'双端已确认',late_confirmed:'延迟确认',unconfirmed:'尚未确认',failed:'失败',cancelled:'已取消',authenticated:'身份认证完成',telemetry_accepted:'温度已接收',online:'在线',offline:'离线',connecting:'连接中',rekey_started:'开始换钥',rekey_confirmed:'换钥已确认'};
export const stageLabel=v=>stages[v]||'事件记录';
function metric(label,used,limit,format=String,note=''){return {label,value:observedNumber(used)?format(used):'未观测',limit:observedNumber(limit)?format(limit):'未观测',percent:observedNumber(used)&&observedNumber(limit)&&limit>0?Math.min(100,100*used/limit):null,note};}
export function presentDetail(kind,v,snapshot,fresh){
 const m={title:'事件详情',summary:'',status:fresh?'当前观测':'上次观测 · 状态已过期',fields:[],metrics:[],limits:[],timeline:[],identifiers:[]};
 const id=(label,x)=>{if(x!==undefined&&x!==null&&x!=='')m.identifiers.push([label,String(x)]);};id('运行编号',snapshot.run_id);
 if(kind==='run'){
  m.title='本次运行概览';m.summary='查看操作、换钥与日志的使用情况，以及本次运行的容量限制。';const c=v.capacity||{};
  m.metrics=[metric('操作记录',c.operations_used,c.operations_limit,String,'本次运行已使用 / 容量上限'),metric('换钥记录',c.rekeys_used,c.rekeys_limit,String,'本次运行已使用 / 容量上限'),metric('日志空间',c.log_bytes,c.log_bytes_limit,bytes,'当前日志大小 / 空间上限'),metric('最近事件',Array.isArray(v.events)?v.events.length:undefined,c.events_limit,String,'当前保留 / 滚动窗口上限')];
  m.limits=[['每个队列',num(c.queue_limit,' 项上限')],['导出文件',num(c.exports_limit,' 份上限')],['后台任务',num(c.jobs_limit,' 项上限')]];
  m.fields=[['设备',Array.isArray(v.devices)?v.devices.filter(d=>d.status==='online').length+' / '+v.devices.length+' 台在线':'未观测'],['记录范围',observedNumber(v.events_omitted)&&v.events_omitted>0?'已滚动淘汰 '+v.events_omitted+' 条事件':'最近保留的事件窗口']];
 }else if(kind==='device'||kind==='sampling'){
  m.title='设备 '+v.id;m.summary=(fresh?'设备当前状态：':'设备上次状态：')+(stages[v.status]||'未观测')+'。';
  m.fields=[['连接状态',stages[v.status]||'未观测'],['最近会话模式',v.suite===2?'国密与抗量子混合':v.suite===1?'传统国密':'未观测'],['上报间隔',observedNumber(v.interval_ms)?v.interval_ms/1000+' 秒':'未观测'],['自动上报',v.auto===true?'已开启':v.auto===false?'已关闭':'未观测'],[v.transport==='remote'?'电池温度（网关接收）':'温度观测',v.transport==='remote'?(typeof v.last_received_temperature_c==='number'&&Number.isFinite(v.last_received_temperature_c)?v.last_received_temperature_c+' ℃':'未观测'):typeof v.temperature_c==='number'&&Number.isFinite(v.temperature_c)?v.temperature_c+' ℃':'未观测'],['接收样本',num(v.accepted_samples,' 条')],['会话建立',timeLabel(v.session_started_at)]];id('设备编号',v.id);id('会话编号',v.sid);
 }else if(kind==='operation'){
  m.title=v.kind==='telemetry'?'温度发送详情':'设备控制详情';m.summary=operationMessage(v);m.fields=[['目标设备',v.device_id||'未观测'],['操作状态',stages[v.status]||v.status||'未观测']];
  if(observedNumber(v.interval_ms)&&v.interval_ms>0)m.fields.push(['申请上报间隔',v.interval_ms/1000+' 秒']);
  const steps=v.transport==='remote'?[['已受理',v.queued_at],['网关已下发',v.written_at],['手机执行回执已验证',v.confirmed_at]]:v.kind==='telemetry'?[['已受理',v.queued_at],['已写出',v.written_at],['网关已接收',v.confirmed_at]]:[['已受理',v.queued_at],['网关已下发',v.written_at],['设备已处理',v.peer_processed_at],['双端已确认',v.confirmed_at]];
  m.timeline=steps.map(([label,at])=>({label,time:timeLabel(at),done:timeLabel(at)!=='未观测'}));id('操作编号',v.operation_id);id('会话编号',v.sid);
 }else{
  m.title=stageLabel(v.stage);m.summary='该事件的阶段、时间及关联对象如下。';m.fields=[['事件阶段',stages[v.stage]||v.stage||'未观测'],['发生时间',timeLabel(v.at)],['所属端点',({gateway:'网关',device:'设备',management:'管理端'})[v.endpoint]||v.endpoint||'未观测']];if(v.device_id)m.fields.push(['关联设备',v.device_id]);
  if(v.detail){const text=String(v.detail).trim();let structured=false;try{const x=JSON.parse(text);structured=x!==null&&typeof x==='object';}catch{}m.summary=structured||/[\{\[]\s*"[\w_]+"\s*:/.test(text)?'结构化事件数据已收录在下方技术记录中，可展开核对。':text;}
  id('事件编号',v.event_id);id('操作编号',v.operation_id);id('换钥编号',v.rekey_id);id('会话编号',v.sid);
 }
 if(v.error)m.fields.push(['异常说明',String(v.error)]);return m;
}
