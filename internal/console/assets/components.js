export function node(tag,text,cls){const n=document.createElement(tag);if(text!==undefined)n.textContent=String(text);if(cls)n.className=cls;return n}
export function card(title){const n=node('section',undefined,'card');n.append(node('h3',title));return n}
export function button(text,fn,cls){const b=node('button',text,cls);b.type='button';b.dataset.write='1';b.addEventListener('click',async()=>{if(b.disabled||b.dataset.busy==='1')return;b.disabled=true;b.dataset.busy='1';try{await fn()}catch(e){if(e.fieldValidation)return;document.dispatchEvent(new Event('action-error'));const msg=e.userMessage||e.message;const notice=document.querySelector('#notice');if(notice){delete notice.dataset.kind;notice.textContent=msg;}const out=b.closest('section')?.querySelector('[data-result]');if(out&&out.dataset.managed!=='1'){out.dataset.error='1';out.replaceChildren(node('p',msg,'bad'))}}finally{delete b.dataset.busy;b.disabled=b.guard?!!b.guard():b.dataset.write==='1'&&document.body.dataset.unavailable==='1';b.recheck?.();if(b.dataset.restoreFocus==='1'){delete b.dataset.restoreFocus;if(b.isConnected&&!b.disabled)b.focus();else document.querySelector('#title')?.focus()}}});return b}
export function table(head,rows){const wrap=node('div',undefined,'table-wrap'),t=node('table'),h=node('tr');head.forEach(s=>h.append(node('th',s)));t.append(h);for(const row of rows){const tr=node('tr');row.forEach(v=>{const td=node('td');td.append(v instanceof Node?v:node('span',v??'未知'));tr.append(td)});t.append(tr)}wrap.append(t);return wrap}
const SENSITIVE_KEY_RE=/(private|csrf|token|cookie|password|temperature_c|^code$|one_time_code|authorization_code|secret|seed|priv_key|symmetric_key|handshake_hash|mac_key|auth_key|bearer|vector|nonce)/i;
export function publicDetails(value){if(Array.isArray(value))return value.map(publicDetails);if(value&&typeof value==='object')return Object.fromEntries(Object.entries(value).filter(([k])=>!SENSITIVE_KEY_RE.test(k)).map(([k,v])=>[k,publicDetails(v)]));return value}
export function detail(value){const d=node('details');d.dataset.key=value?.operation_id||value?.job_id||value?.event_id||value?.id||'source';d.append(node('summary','查看关联详情'),node('pre',JSON.stringify(publicDetails(value),null,2)));return d}
export function field(label,type,value){const l=node('label',label),i=node('input');i.type=type;i.value=value;i.dataset.label=label;l.append(i);return [l,i]}
// Local validation is separate from the last real operation receipt.
export function fieldFeedback(input){const message=node('p',undefined,'field-error');message.id=input.id+'-error';message.hidden=true;message.setAttribute('role','alert');input.setAttribute('aria-describedby',message.id);const clear=()=>{message.hidden=true;message.textContent='';input.removeAttribute('aria-invalid')};input.addEventListener('input',clear);return {message,clear,validate(parse){clear();try{return parse(input.value)}catch(e){input.setAttribute('aria-invalid','true');message.textContent=e.userMessage||e.message;message.hidden=false;input.focus();e.fieldValidation=true;throw e}}}}

export const statusMap={
 online:{zh:'已认证在线',en:'online'},
 offline:{zh:'离线状态',en:'offline'},
 connecting:{zh:'正在安全连接',en:'connecting'},
 queued:{zh:'操作已排队',en:'queued'},
 running:{zh:'任务运行中',en:'running'},
 completed:{zh:'任务已完成',en:'completed'},
 writing:{zh:'开始加密写出',en:'writing'},
 written:{zh:'已写出·等待确认',en:'written'},
 confirmed:{zh:'双端已确认',en:'confirmed'},
 unconfirmed:{zh:'未确认·可能已执行',en:'unconfirmed'},
 late_confirmed:{zh:'迟到确认',en:'late_confirmed'},
 failed:{zh:'执行失败',en:'failed'},
 cancelled:{zh:'已取消',en:'cancelled'},
 authenticated:{zh:'安全握手已认证',en:'authenticated'},
 telemetry_accepted:{zh:'网关验收温度',en:'telemetry_accepted'},
 telemetry_written:{zh:'温度写出完成',en:'telemetry_written'},
 control_write_result:{zh:'控制写出结果',en:'control_write_result'},
 peer_processed:{zh:'设备已处理',en:'peer_processed'},
 local_sampling:{zh:'本地采样调整',en:'local_sampling'}
};
export const statuses=Object.fromEntries(Object.entries(statusMap).map(([k,v])=>[k,`${v.zh} (${v.en})`]));

export function badge(v){
 const item=statusMap[v];
 const tone=['confirmed','late_confirmed','online','authenticated'].includes(v)?'good':['failed','rejected'].includes(v)?'bad':'warning';
 const span=node('span',undefined,'badge badge-bilingual '+tone);
 if(item){
  span.append(node('span',item.zh,'badge-zh'),node('span',item.en,'badge-en'));
 }else{
  const str=String(v||'未观测');
  const m=str.match(/^(.*?)\s*\((.*?)\)$/);
  if(m){
   span.append(node('span',m[1],'badge-zh'),node('span',m[2],'badge-en'));
  }else{
   span.append(node('span',str,'badge-zh'));
  }
 }
 return span;
}

export function readButton(text,fn,cls='secondary'){const b=button(text,fn,cls);b.dataset.write='0';return b}
export function guardButton(b,guard){b.guard=guard;const reason=guard();b.disabled=b.dataset.busy==='1'||!!reason;b.title=reason||'';return b}
export function updateBadge(n,v){
 const b=badge(v);
 n.className=b.className;
 n.replaceChildren(...b.childNodes);
}
export function setText(n,text){if(n.textContent!==String(text))n.textContent=String(text)}
