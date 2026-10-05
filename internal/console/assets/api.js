import {READ_TIMEOUT_MS} from './store.js';
import {errorMessage} from './usability.js';
// Credentials stay in this closure; no URL, DOM, storage, public log or export.
export function createAPI(store,{fetcher=(...args)=>fetch(...args),notify=()=>{}}={}){
  let csrf='';const active=new Map();
  async function api(path,body,options={}){
    const write=body!==undefined,login=path==='/login',epoch=store.epoch;
    const target=options.context;
    if(write&&!login){
      if(!store.authValid())throw Error('本机授权不可用，请按原流程重新授权');
      if(!['/run/stop','/run/exit','/exports'].includes(path)&&!store.fresh())throw Error('当前快照已过期，未发送写请求');
      if(target&&!store.matches(target))throw Error('运行、设备或会话已改变，未重发旧动作');
      if(options.guard){const reason=options.guard();if(reason)throw Error(reason)}
    }
    const key=[target?.run||store.snapshot?.run_id||'',path,body?.device_id||''].join('|');
    if(write&&active.has(key))throw Error('同一目标动作正在提交，未重复发送');
    if(write&&active.size>=64)throw Error('前端提交队列已满，请等待现有请求结束');
    if(write)active.set(key,true);
    const controller=new AbortController();const timer=setTimeout(()=>controller.abort(),write?10000:READ_TIMEOUT_MS);
    try{
      const response=await fetcher('/api/v1'+path,{method:write?'POST':'GET',credentials:'same-origin',headers:write?{'Content-Type':'application/json','X-CSRF-Token':csrf}:{},body:write?JSON.stringify(body):undefined,signal:controller.signal});
      const text=await response.text();let result;
      try{result=JSON.parse(text)}catch{result={message:text.slice(0,300)}}
      if(!response.ok){const e=Error(result.message||'接口拒绝请求');e.code=result.code;e.reason=result.reason;e.http_status=response.status;e.userMessage=errorMessage(e);if(response.status===401&&!login){csrf='';store.revoke()}throw e}
      if(!login&&epoch!==store.epoch)throw Error('授权上下文已改变，未显示迟到响应');
      return result;
    }catch(e){
      if(!e.http_status){e.submissionUnknown=write;e.userMessage=write?'提交结果未知，可能已执行；未自动重发。'+(e.message||''):('只读请求超时或不可达：'+e.message)}
      if(write)notify(e.userMessage||e.message);throw e;
    }finally{clearTimeout(timer);if(write)active.delete(key)}
  }
  api.setSession=s=>{csrf=s.csrf_token||'';if(store.authValid()){store.expiry=Date.parse(s.expires_at);store.emit()}else store.authorize(s.expires_at)};
  api.clearSession=()=>{csrf='';store.revoke()};api.store=store;api.notify=notify;
  return api;
}
