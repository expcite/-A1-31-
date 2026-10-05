// Pure presentation rules, without protocol state or transport simulation.
export function secondsToMillis(text){const s=String(text).trim();if(!/^\d+(?:\.\d{1,3})?$/.test(s))throw Error('请输入0.1～60秒，最多三位小数；必须能精确换算为整数毫秒。');const [a,b='']=s.split('.'),ms=Number(a)*1000+Number(b.padEnd(3,'0'));if(!Number.isSafeInteger(ms)||ms<100||ms>60000)throw Error('上报间隔允许0.1～60秒。');return ms;}
export const operationNames={queued:'等待发送',writing:'正在写出',written:'已发出，等待确认',unconfirmed:'结果未确认（设备可能已执行）',failed:'失败',cancelled:'已取消',late_confirmed:'稍后收到确认',confirmed:'已确认'};
export function operationMessage(o){if(!o)return '尚未提交操作。';if(['confirmed','late_confirmed'].includes(o.status))return (o.status==='late_confirmed'?'稍后收到确认：':'')+(o.kind==='telemetry'?`网关已收到 ${o.temperature_c}℃`:`设备已设置 ${o.interval_ms/1000}秒上报，网关已验证回执`);return (operationNames[o.status]||'状态未知')+(o.error?'；'+o.error:'');}
export function errorMessage(e){if(e.code==='authorization_expired')return '本机授权失效或防伪校验未通过。停止软件后重新启动，使用启动窗口的新口令；保留现有身份。';if(e.code==='queue_full')return '待发队列已满（32项）。等当前发送任务完成后再操作，勿连续重试。';if(e.code==='operation_limit')return '本次累计操作已达256项。先导出记录，再停止并重新启动新运行；换钥不会清空操作。';return e.message||String(e);}
export function filterEvents(events,q){return events.filter(e=>(!q.device_id||e.device_id===q.device_id)&&(!q.endpoint||e.endpoint===q.endpoint)&&(!q.stage||e.stage===q.stage));}
export function pageEvents(events,page,size=20){const pages=Math.max(1,Math.ceil(events.length/size));page=Math.max(1,Math.min(page,pages));return {page,pages,rows:[...events].reverse().slice((page-1)*size,page*size)};}

export function temperature(text){const s=String(text).trim(),v=Number(s);if(s===''||!Number.isFinite(v)||v< -50||v>150)throw Error('温度须为-50～150℃的有限数值，不能留空。');return v;}
