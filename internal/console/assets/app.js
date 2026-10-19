import {ConsoleStore,FRONTEND_VERSION,POLL_MS} from './store.js';
import {createAPI} from './api.js';
import {routes,routeFrom} from './router.js';
import {createDrawer} from './drawer.js';
import {GuideController} from './guide.js';
import {communication} from './workbench.js';
import {devices} from './devices.js';
import {keys} from './keys.js';
import {lab} from './jobs.js';
import {logs} from './logs.js';
import {performance as historicalPage} from './performance.js';
import {help} from './help.js';
import {node,readButton,button,setText} from './components.js';
import {confirmAction} from './confirm.js';
const store=new ConsoleStore(),content=document.querySelector('#content'),nav=document.querySelector('#nav'),notice=document.querySelector('#notice'),login=document.querySelector('#login');
const api=createAPI(store,{notify:message=>{delete notice.dataset.kind;notice.textContent=message}});
document.querySelector('#notice-dismiss').onclick=()=>{notice.textContent=''};
store.selected=sessionStorage.getItem('selected_device')||'';
const pages=new Map();let page=routeFrom(location.hash),mounted=null,run='',mountTicket=0,readPromise=null,metaBusy=false,jobsBusy=false,lastWall=null,authChecked=false;
api.guide=new GuideController(store,sessionStorage);api.drawer=createDrawer(store,api);
for(const key of ['manual_temperature','interval_seconds','auto_source','advanced_mode'])sessionStorage.removeItem(key);
document.querySelector('#frontend-version').textContent=FRONTEND_VERSION;
const footer=document.querySelector('#footer-version');footer.textContent='产品构建待核对 · '+FRONTEND_VERSION;
api.confirmPayload=()=>confirmAction('模拟温度载荷','包含模拟载荷','include_simulated_payload');
api.validateExport=(r,selection)=>{
 if(!r?.selection||['run_id','from_event_id','to_event_id','device_id','endpoint','stage'].some(k=>(r.selection[k]??'')!==(selection[k]??''))||!/^\/api\/v1\/exports\/[a-zA-Z0-9_-]+$/.test(r.url)||!/^[a-f0-9]{64}$/.test(r.sha256)||!Number.isSafeInteger(r.bytes)||!Number.isSafeInteger(r.event_count))throw Error('导出回执范围或文件元信息不一致，未采用结果');
};
function records(initial){
 const root=node('div',undefined,'records-page'),tabs=node('div',undefined,'action-tabs'),current=logs(initial,api),history=node('div'),loadInfo=node('p','尚未读取历史数据。','muted');history.hidden=true;history.append(loadInfo);let loading=false,loaded=false;
 const selectRecord=selected=>{for(const [b,key]of [[currentButton,'current'],[historyButton,'history']]){b.setAttribute('aria-selected',String(key===selected));b.tabIndex=key===selected?0:-1}};
 const currentButton=readButton('当前运行',()=>{current.hidden=false;history.hidden=true;selectRecord('current')}),historyButton=readButton('历史测量与来源',async()=>{current.hidden=true;history.hidden=false;selectRecord('history');if(loaded||loading)return;loading=true;try{const h=await historicalPage(store.snapshot,api);history.replaceChildren(h);loaded=true}catch(e){loadInfo.textContent='历史数据读取失败：'+e.message}finally{loading=false}});
 tabs.setAttribute('role','tablist');tabs.setAttribute('aria-label','记录类别');for(const b of [currentButton,historyButton]){b.setAttribute('role','tab');b.addEventListener('keydown',e=>{if(['ArrowLeft','ArrowRight','Home','End'].includes(e.key)){e.preventDefault();const next=e.key==='Home'?currentButton:e.key==='End'?historyButton:b===currentButton?historyButton:currentButton;next.click();next.focus()}})}selectRecord('current');
 tabs.append(currentButton,historyButton);root.append(tabs,current,history);root.refresh=()=>current.refresh?.(store.snapshot);return root;
}
const factories={communication,devices,keys,lab,logs:records,help};
function safeReadButton(label,fn){const b=readButton(label,fn);b.dataset.write='0';return b}
function navigation(){
 const paths={communication:['M3 9h18M9 9v11','M5 4h14a2 2 0 0 1 2 2v12a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V6a2 2 0 0 1 2-2Z'],devices:['M3 3h7v7H3zM14 3h7v7h-7zM3 14h7v7H3zM14 14h7v7h-7z'],logs:['M6 3h9l4 4v14H6zM14 3v5h5M9 12h7M9 16h7'],lab:['M9 3h6M10 3v6L4 19q-1 2 2 2h12q3 0 2-2L14 9V3M8 14h8'],management:['M12 3v3M12 18v3M3 12h3M18 12h3M5.6 5.6l2 2M16.4 16.4l2 2M5.6 18.4l2-2M16.4 7.6l2-2','M16 12a4 4 0 1 1-8 0 4 4 0 0 1 8 0Z'],help:['M9 9a3 3 0 1 1 5 2c-2 1-2 2-2 3M12 17h.01','M21 12a9 9 0 1 1-18 0 9 9 0 0 1 18 0Z']};
 function icon(key){const svg=document.createElementNS('http://www.w3.org/2000/svg','svg');svg.setAttribute('viewBox','0 0 24 24');svg.setAttribute('aria-hidden','true');for(const d of paths[key]){const p=document.createElementNS(svg.namespaceURI,'path');p.setAttribute('d',d);svg.append(p)}return svg}
 for(const [id,label]of [['communication','工作台'],['devices','设备'],['logs','记录'],['lab','实验']]){
  const b=safeReadButton(label,()=>{location.hash='#'+id;closeSidebar()});
  b.dataset.page=id;b.setAttribute('aria-label',label);const text=node('span',label,'nav-label');b.replaceChildren(icon(id),text);nav.append(b);
 }
 const management=node('details',undefined,'management-nav'),summary=node('summary');summary.append(icon('management'),node('span','管理','nav-label'),node('span','⌄','chevron'));management.append(summary);
 for(const [target,label]of [['device','设备身份与迁移'],['gateway','网关维护']])management.append(safeReadButton(label,()=>{api.managementTarget=target;location.hash='#keys';pages.get(store.snapshot?.run_id+'|keys')?.choose(target);closeSidebar()}));
 const helpButton=safeReadButton('帮助与版本',()=>{location.hash='#help';closeSidebar()});helpButton.dataset.page='help';helpButton.replaceChildren(icon('help'),node('span','帮助与版本','nav-label'));document.querySelector('#aux-nav').append(management,helpButton);
 const runMenu=node('details',undefined,'overflow');runMenu.id='run-menu';runMenu.append(node('summary','当前运行 ⌄'));const runItems=node('div',undefined,'menu');
 const stop=button('安全停止设备',async()=>{const current=store.snapshot?.run_id,yes=await confirmAction('本次全部设备','停止设备','stop:'+current);if(!yes)return;if(current&&current!==store.snapshot?.run_id)throw Error('运行已经改变');await api('/run/stop',{});api.notify('停止请求已有本机返回；请查看连接状态。无法读取时用原停止脚本。');await readState()},'secondary');stop.id='safe-stop';
 const exit=button('退出软件',async()=>{const current=store.snapshot?.run_id;if(!current)throw Error('缺少当前run，请用原停止脚本／启动窗口退出');const yes=await confirmAction('本次全部设备','退出软件','exit:'+current);if(yes){if(current!==store.snapshot?.run_id)throw Error('运行已经改变');await api('/run/exit',{confirm:yes});api.notify('软件开始受控退出，等待启动窗口结束；尚未冒称已关闭。')}},'danger');exit.id='exit-software';stop.guard=exit.guard=()=>store.authValid()?'':'本机授权不可用';runItems.append(safeReadButton('运行与容量详情',()=>{if(store.snapshot)api.drawer.open('run',store.snapshot.run_id)}),document.querySelector('#refresh-state'),stop,exit);runMenu.append(runItems);document.querySelector('#run-actions').append(runMenu);
}
navigation();
// Default theme: light unconditionally unless explicitly saved
let currentTheme='light';
try{
 const storedTheme=localStorage.getItem('edgehybrid_theme');
 if(storedTheme==='dark'||storedTheme==='light'){
  currentTheme=storedTheme;
 }else{
  currentTheme='light';
  try{localStorage.setItem('edgehybrid_theme','light')}catch{}
 }
}catch{}
document.documentElement.dataset.theme=currentTheme;
const themeToggle=safeReadButton(currentTheme==='dark'?'☀️ 浅色':'🌙 深色',()=>{
 const next=document.documentElement.dataset.theme==='dark'?'light':'dark';
 document.documentElement.dataset.theme=next;try{localStorage.setItem('edgehybrid_theme',next)}catch{}
 themeToggle.textContent=next==='dark'?'☀️ 浅色':'🌙 深色';
 themeToggle.setAttribute('aria-label',next==='dark'?'切换浅色模式':'切换深色模式');
});
themeToggle.id='theme-toggle';themeToggle.className='secondary theme-toggle';themeToggle.setAttribute('aria-label',currentTheme==='dark'?'切换浅色模式':'切换深色模式');

// Motion Toggle (default ON: 60fps micro-animations, with localStorage memory)
let motionEnabled=true;
try{
 const storedMotion=localStorage.getItem('edgehybrid_motion');
 if(storedMotion!==null){motionEnabled=storedMotion==='1'}
}catch{}
document.body.classList.toggle('motion-disabled',!motionEnabled);
const motionToggle=safeReadButton(motionEnabled?'⚡ 动效: 开':'💤 动效: 关',()=>{
 motionEnabled=!motionEnabled;
 document.body.classList.toggle('motion-disabled',!motionEnabled);
 try{localStorage.setItem('edgehybrid_motion',motionEnabled?'1':'0')}catch{}
 motionToggle.textContent=motionEnabled?'⚡ 动效: 开':'💤 动效: 关';
 motionToggle.setAttribute('aria-label',motionEnabled?'关闭动效':'开启动效');
});
motionToggle.id='motion-toggle';motionToggle.className='secondary motion-toggle';motionToggle.setAttribute('aria-label',motionEnabled?'关闭动效':'开启动效');
const headerActions=document.querySelector('.header-actions');
if(headerActions){
 headerActions.prepend(themeToggle,motionToggle);
}
// Menus are presentation only; dismissing them never submits an action.
document.addEventListener('click',e=>{for(const menu of document.querySelectorAll('.overflow[open]'))if(!menu.contains(e.target)||e.target.closest('button'))menu.open=false});
document.addEventListener('keydown',e=>{if(e.key==='Escape')for(const menu of document.querySelectorAll('.overflow[open]'))menu.open=false});
const sidebar=document.querySelector('#sidebar-toggle'),sidebarMedia=matchMedia('(max-width:767px)');
function syncSidebar(){sidebar.setAttribute('aria-expanded',String(sidebarMedia.matches?document.body.classList.contains('sidebar-open'):!document.body.classList.contains('sidebar-collapsed')))}
function closeSidebar(){document.body.classList.remove('sidebar-open');syncSidebar()}
sidebar.onclick=()=>{
 if(sidebarMedia.matches)document.body.classList.toggle('sidebar-open');
 else{document.body.classList.toggle('sidebar-collapsed');sessionStorage.setItem('sidebar_collapsed',document.body.classList.contains('sidebar-collapsed')?'1':'0')}
 syncSidebar();
};
if(sessionStorage.getItem('sidebar_collapsed')==='1')document.body.classList.add('sidebar-collapsed');
syncSidebar();sidebarMedia.addEventListener('change',syncSidebar);
document.querySelector('#nav-backdrop').onclick=closeSidebar;
document.addEventListener('keydown',e=>{if(e.key==='Escape')closeSidebar()});
const welcome=document.querySelector('#welcome');api.showWelcome=()=>{welcome.hidden=true};
document.querySelector('#dismiss-welcome').onclick=()=>{sessionStorage.setItem('bplus_welcome_dismissed','1');welcome.hidden=true};
document.querySelector('#guide-toggle').onclick=()=>{api.guide.enabled=!api.guide.enabled;if(api.guide.enabled)api.guide.select();api.guide.persist();update()};
document.querySelector('#refresh-state').onclick=()=>readState();
function mount(){
 if(!store.snapshot||!store.authValid())return;
 const key=store.snapshot.run_id+'|'+page;if(mounted===key)return;const ticket=++mountTicket;let view=pages.get(key);
 try{if(!view){view=factories[page](store.snapshot,api,store.build,true);pages.set(key,view)}if(ticket!==mountTicket||!store.authValid())return;mounted=key;content.replaceChildren(view);view.refresh?.(store.snapshot)}
 catch(e){if(ticket===mountTicket)content.replaceChildren(node('p','页面读取失败：'+(e.userMessage||e.message),'bad'))}
}
function update(){
 const valid=store.authValid(),fresh=store.fresh();document.body.dataset.unavailable=fresh?'0':'1';
 login.hidden=valid;content.hidden=!valid;document.querySelector('#refresh-state').disabled=!valid;
 const banner=document.querySelector('#connection');setText(banner,!valid?'请先完成本机授权':fresh?'本机已授权 · 状态新鲜':'本机已授权 · 状态过期／未知');banner.className='badge '+(valid&&fresh?'good':'warning');
 setText(document.querySelector('#expiry'),valid?'授权剩余 '+Math.max(0,Math.ceil((store.expiry-store.wall())/60000))+'分钟 · 最近有效状态 '+(lastWall?new Date(lastWall).toLocaleTimeString():'未观测'):'口令和防伪令牌不会写入URL、存储或导出。');
 setText(document.querySelector('#stale-note'),valid&&!fresh?'状态过期：当前设备只能视为上次观测。依赖状态的写操作已禁用；安全停止／退出仍需有效授权，无法确认时使用原停止脚本。':'');
 document.querySelector('#stale-note').hidden=!valid||fresh;
 const guideToggle=document.querySelector('#guide-toggle');guideToggle.hidden=page!=='communication';guideToggle.disabled=!valid;guideToggle.textContent=api.guide.enabled?'关闭演示引导':'开启演示引导';
 const pageInfo={communication:['工作台','选择一个设备，完成发送、控制和确认。'],devices:['设备','先选择对象，再查看会话或执行维护。'],logs:['记录','固定范围查询，查看事件与对应的操作来源。'],lab:['实验','固定任务、独立实例；实验前置和真实结果分别显示。'],keys:['身份与迁移','长期身份管理与会话换钥分开，维护前明确确认。'],help:['帮助与版本','按实际任务学习，技术细节按需查看。']}[page];
 setText(document.querySelector('#title'),pageInfo[0]);setText(document.querySelector('#breadcrumb'),pageInfo[0]);setText(document.querySelector('#page-description'),pageInfo[1]);
 document.querySelectorAll('#nav [data-page], #aux-nav [data-page]').forEach(b=>{b.classList.toggle('selected',b.dataset.page===page);b.setAttribute('aria-current',b.dataset.page===page?'page':'false')});
 for(const b of document.querySelectorAll('#nav button[data-write="1"], #run-menu button[data-write="1"]'))b.disabled=!!b.dataset.busy||!valid;
 if(!valid){mounted=null;pages.clear();content.replaceChildren();api.drawer.clear();document.querySelectorAll('dialog:not(.detail-drawer)').forEach(d=>d.close());if(authChecked)api.guide.clear();welcome.hidden=true;return}
 if(run!==store.snapshot?.run_id){run=store.snapshot?.run_id||'';pages.clear();mounted=null;mountTicket++;api.drawer.clear()}
 const phoneMode=store.snapshot?.mode==='phone-bridge-v27';
 if(phoneMode){document.title='EdgeHybrid · 手机实机验证';setText(document.querySelector('.workspace-name>div'),'手机接入验证');setText(document.querySelector('.workspace-crumb'),'电脑热点');setText(document.querySelector('.scene-marker'),'手机安全通信');setText(document.querySelector('.sidebar-footer>small'),'电池温度 · 实机结果另行验收');document.querySelector('#nav [data-page=lab]').hidden=true;document.querySelector('#guide-toggle').hidden=true;if(page==='communication')setText(document.querySelector('#page-description'),'等待手机D001连接，查看真实电池温度并验证上报间隔控制。')}
 if(store.snapshot){mount();const view=pages.get(store.snapshot.run_id+'|'+page);view?.refresh?.(store.snapshot);api.drawer.refresh()}
 footer.textContent=(store.build?.version||'产品构建未知')+' · '+FRONTEND_VERSION;api.showWelcome();if(phoneMode)welcome.hidden=true;
}
store.subscribe(update);
async function readState(){
 if(!store.authValid())return;if(readPromise)return readPromise;
 const ticket=store.ticket();
 readPromise=(async()=>{try{const fresh=await api('/state');if(store.accept(fresh,ticket)){lastWall=Date.now();if(notice.dataset.kind==='read-error'){delete notice.dataset.kind;notice.textContent='读取已恢复，当前状态按新的真实快照重新核对。'}update()}}catch(e){notice.dataset.kind='read-error';notice.textContent=e.userMessage||e.message}finally{readPromise=null;update()}})();
 return readPromise;
}
api.refreshState=readState;
async function metadata(){
 if(!store.authValid()||metaBusy)return;metaBusy=true;
 const epoch=store.epoch;await Promise.allSettled([
  api('/build').then(b=>{if(epoch===store.epoch)store.build=b}).catch(()=>{if(epoch===store.epoch)store.build=null}),
  api('/session').then(s=>{if(epoch===store.epoch&&store.authValid())api.setSession(s)}).catch(e=>{if(e.http_status!==401&&store.authValid())notice.textContent='授权元信息读取失败；有效期保守沿用此前已核对的到期时间。'})
 ]);metaBusy=false;update();
}
async function readJobs(){
 if(!store.authValid()||jobsBusy)return;jobsBusy=true;const epoch=store.epoch;
 try{const jobs=await api('/jobs');if(epoch===store.epoch&&Array.isArray(jobs)){store.jobs=jobs;store.jobsKnown=true;store.lastJobsSuccess=store.now();update()}}catch{store.jobsKnown=false;update()}finally{jobsBusy=false}
}
document.querySelector('#login-form').onsubmit=async e=>{
 e.preventDefault();const input=document.querySelector('#code'),submit=document.querySelector('#login-submit');submit.disabled=true;
 try{await api('/login',{code:input.value});input.value='';const s=await api('/session');api.setSession(s);await readState();metadata();readJobs();api.notify('本机授权已完成；请选择设备并明确连接。')}
 catch(e){notice.textContent=e.userMessage||e.message}finally{input.value='';submit.disabled=false}
};
function route(){const next=routeFrom(location.hash);if(next!==page){page=next;mounted=null;mountTicket++}if(location.hash!=='#'+next)history.replaceState(null,'','#'+next);update()}
window.addEventListener('hashchange',route);document.addEventListener('visibilitychange',()=>{update();if(!document.hidden){readState();readJobs()}});
setInterval(()=>{if(store.authorized&&!store.authValid()){api.clearSession();notice.textContent='授权已到期；没有续期或重放动作。按原启动流程重新授权。'}update()},1000);
setInterval(readState,POLL_MS);setInterval(readJobs,POLL_MS);setInterval(metadata,10000);
route();
try{const s=await api('/session');authChecked=true;api.setSession(s);await readState();metadata();readJobs()}catch{authChecked=true;update()}
