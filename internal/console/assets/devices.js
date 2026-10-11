import {node,card,button,readButton,guardButton,badge,updateBadge,setText} from './components.js';
import {batchSummary} from './batch.js';
import {inspectBatchOutcome,waitForBatchOutcome} from './batch-observer.js';
import {confirmAction} from './confirm.js';
import {secondsToMillis,temperature} from './usability.js';

export function devices(initial,api){
 const store=api.store,c=card('设备池与真实连接'),filterLabel=node('label','搜索设备'),filter=node('input'),count=node('p'),result=node('div');c.classList.add('device-page');filterLabel.append(filter);filter.placeholder='设备编号或连接状态';result.dataset.result='batch';
 const selected=new Set();let activeBatch=false;let batchRecords=[];

 const connect=button('连接本次全部设备',async()=>{const r=await api('/run/start',{}, {guard:()=>store.canWrite()});const details=node('details');details.append(node('summary','查看逐设备结果（失败优先）'));for(const item of [...r.results||[]].sort((a,b)=>(a.status==='accepted')-(b.status==='accepted')))details.append(node('p',item.device_id+' · '+item.status+(item.error?' · '+item.error:'')));result.replaceChildren(node('p',batchSummary(r)),details);await api.refreshState()});
 const stop=button('断开本次全部设备',async()=>{const run=store.snapshot.run_id,yes=await confirmAction('全部 '+store.snapshot.devices.length+' 台设备','断开全部设备','stop:'+run);if(yes){if(run!==store.snapshot?.run_id)throw Error('运行已改变，未发送旧批次动作');await api('/run/stop',{});result.replaceChildren(node('p','本机断开请求已返回；实际连接状态继续读取。'));await api.refreshState()}},'secondary');
 const wrap=node('div',undefined,'table-wrap'),table=node('table'),thead=node('thead'),head=node('tr'),tbody=node('tbody');

 const selectAllTh=node('th',undefined,'device-select-cell'),selectAll=node('input');
 selectAll.type='checkbox';selectAll.title='全选 / 取消全选';
 selectAllTh.append(selectAll);
 head.append(selectAllTh);
 for(const h of ['设备','连接','模式','采样 / 间隔','最近接收','操作'])head.append(node('th',h));
 thead.append(head);table.append(thead,tbody);wrap.append(table);const rows=new Map();

 const addInput=node('input'),addBtn=button('+ 添加设备',async()=>{
  const target=addInput.value.trim().toUpperCase();
  const body=target?{device_id:target}:{};
  const res=await api('/devices',body,{guard:()=>store.canWrite()});
  api.notify('设备 '+(res.id||target||'新设备')+' 已加入设备池');
  addInput.value='';
  await api.refreshState();
 },'secondary');
 addInput.placeholder='编号(如D002,留空自选)';addInput.style.maxWidth='180px';
 addInput.onkeydown=e=>{if(e.key==='Enter'){e.preventDefault();addBtn.click()}};

 const batchMenu=node('details',undefined,'overflow'),menu=node('div',undefined,'menu'),toolbar=node('div',undefined,'toolbar'),summaryRow=node('div',undefined,'summary-row');
 menu.append(connect,stop);batchMenu.append(node('summary','批量操作 ⌄'),menu);
 toolbar.append(filterLabel,node('span',undefined,'fill'),addInput,addBtn,batchMenu);
 summaryRow.append(count);

 // 底部浮动批量操作面板
  // 顶部浮动批量操作中枢
  const batchBar=node('div',undefined,'batch-top-hub');
  const batchCount=node('span','已选 0 台设备','batch-count-badge');

  // 温度输入控制组
  const tempGroup=node('div',undefined,'batch-control-group');
  const tempInput=node('input');tempInput.type='number';tempInput.step='0.1';tempInput.min='-50';tempInput.max='150';tempInput.value='26.5';
  tempGroup.append(node('label','目标温度:'),tempInput,node('span','℃','muted'));

  // 间隔选择控制组
  const intervalGroup=node('div',undefined,'batch-control-group');
  const intervalSelect=node('select');
  for(const sec of [1,5,10,'自定义']){
    const opt=node('option',sec==='自定义'?'自定义…':sec+' 秒');
    opt.value=String(sec);
    if(sec===5)opt.selected=true;
    intervalSelect.append(opt);
  }
  const customIntervalInput=node('input');customIntervalInput.type='number';customIntervalInput.step='0.1';customIntervalInput.min='0.1';customIntervalInput.max='60';customIntervalInput.value='5';customIntervalInput.style.width='54px';customIntervalInput.hidden=true;
  intervalSelect.onchange=()=>{customIntervalInput.hidden=intervalSelect.value!=='自定义'};
  intervalGroup.append(node('label','上报间隔:'),intervalSelect,customIntervalInput);

  function getBatchIntervalMs(){
    const val=intervalSelect.value==='自定义'?customIntervalInput.value:intervalSelect.value;
    return secondsToMillis(val);
  }

  function getBatchTempC(){
    return temperature(tempInput.value);
  }

  function triggerCascade(targets){
    targets.forEach((id, idx)=>{
      setTimeout(()=>{
        const r=rows.get(id);
        if(r?.tr){
          r.tr.classList.remove('cascade-active');
          void r.tr.offsetWidth;
          r.tr.classList.add('cascade-active');
        }
      }, idx * 35);
    });
  }

  function triggerGreenRipple(id){
    const r=rows.get(id);
    if(r?.tr){
      r.tr.classList.remove('ripple-green');
      void r.tr.offsetWidth;
      r.tr.classList.add('ripple-green');
    }
  }

  function syncBatchBar(){
   const n=selected.size;
   batchCount.textContent=`已选 ${n} 台设备`;
   batchBar.classList.toggle('visible',n>0);
   const total=store.snapshot?.devices.length||0;
   selectAll.checked=n>0&&n===total;
  }

  selectAll.onchange=()=>{
   const s=store.snapshot;if(!s)return;
   if(selectAll.checked){
    for(const d of s.devices){
     const r=rows.get(d.id);
     if(r&&!r.tr.hidden){selected.add(d.id);r.checkbox.checked=true;r.tr.classList.add('selected-row')}
    }
   }else{
    selected.clear();
    for(const[,r]of rows){r.checkbox.checked=false;r.tr.classList.remove('selected-row')}
   }
   syncBatchBar();
  };

  async function runBatchTasks(title,targets,kind,taskFn){
   if(activeBatch)throw Error('已有批量任务，请等待或中止剩余执行');
   const contexts=new Map(targets.map(id=>[id,store.context(id)]));
   activeBatch=true;batchRecords=[];
   try{
   triggerCascade(targets);
   const dlg=node('dialog',undefined,'batch-modal');
   dlg.setAttribute('aria-label',title);
   const h=node('h3',`${title} · 共 ${targets.length} 台设备`);
   const pbar=node('div',undefined,'batch-progress-bar'),pfill=node('div',undefined,'batch-progress-fill');
   pfill.style.width='0%';
   pbar.append(pfill);
   const list=node('div',undefined,'batch-device-list');
   const items=new Map();
   for(const id of targets){
    const row=node('div',undefined,'batch-device-item');
    const name=node('strong',id);
    const st=node('span','等待执行…','muted');
    row.append(name,st);list.append(row);items.set(id,{row,st});
   }
   let aborted=false;
   const abortBtn=button('中止剩余执行',()=>{aborted=true;abortBtn.disabled=true;abortBtn.textContent='已请求中止…';},'secondary');
   abortBtn.dataset.write='0';
   const closeBtn=button('完成关闭',()=>{dlg.close();dlg.remove()},'primary');
   closeBtn.dataset.write='0';closeBtn.disabled=true;
   const actionsRow=node('div');actionsRow.style.display='flex';actionsRow.style.justifyContent='flex-end';actionsRow.style.gap='10px';actionsRow.style.marginTop='14px';
   actionsRow.append(abortBtn,closeBtn);
   dlg.append(h,pbar,list,actionsRow);
   document.body.append(dlg);
   dlg.addEventListener('cancel',e=>{e.preventDefault();aborted=true;abortBtn.disabled=true;abortBtn.textContent='已请求中止剩余；已提交项继续核验';});
   dlg.showModal();

   let doneCount=0,succCount=0,failCount=0,unknownCount=0,observedCount=0,skippedCount=0;
   for(let i=0; i<targets.length; i++){
    const id=targets[i];
    if(aborted){
     const it=items.get(id);it.row.className='batch-device-item muted';it.st.textContent='未发送：已中止剩余';skippedCount++;continue;
    }
    const it=items.get(id);
    it.row.className='batch-device-item active';it.st.textContent='下发中…';
    try{
     const context=contexts.get(id),receipt=await taskFn(id,context);
     const record={context,kind,receipt,row:it.row,st:it.st};batchRecords.push(record);
     const outcome=await waitForBatchOutcome(store,context,kind,receipt,{onUpdate:o=>{it.st.textContent=o.text;}});
     it.st.textContent=outcome.text;
     if(outcome.status==='confirmed'){succCount++;it.row.className='batch-device-item done';triggerGreenRipple(id)}
     else if(outcome.status==='observed'){observedCount++;it.row.className='batch-device-item muted'}
     else if(['failed','cancelled'].includes(outcome.status)){failCount++;it.row.className='batch-device-item failed'}
     else{unknownCount++;it.row.className='batch-device-item muted'};
    }catch(e){
     if(e.submissionUnknown){unknownCount++;it.row.className='batch-device-item muted';it.st.textContent='提交结果未知，可能已执行；未自动重发'}else{failCount++;it.row.className='batch-device-item failed';it.st.textContent='请求被拒或失败 ✗ ('+(e.userMessage||e.message||'错误')+')'};
    }finally{
     doneCount++;
     pfill.style.width=Math.round((doneCount/targets.length)*100)+'%';
    }
    if(i < targets.length - 1){
     await new Promise(r => setTimeout(r, 35));
    }
   }
   abortBtn.hidden=true;closeBtn.disabled=false;
   api.notify(`${title}观察结束：确认 ${succCount} 台，本机状态观测 ${observedCount} 台，拒绝/失败 ${failCount} 台，未知/未确认 ${unknownCount} 台，未发送 ${skippedCount} 台`);
   await api.refreshState();
   }finally{activeBatch=false}
  }

  const batchTele=button('批量发温度',async()=>{
   if(!selected.size)return;
   const targets=[...selected];
   let temp;try{temp=getBatchTempC()}catch(e){api.notify(e.userMessage||e.message);return}
   await runBatchTasks(`批量发送温度 ${temp}℃`,targets,'telemetry',async (id,context)=>{
    const reqId=crypto.randomUUID();
    return await api('/devices/'+id+'/telemetry',{request_id:reqId,temperature_c:temp},{context,guard:()=>store.canWrite(id,{online:true})});
   });
  },'primary');

  const batchInterval=button('批量设间隔',async()=>{
   if(!selected.size)return;
   const targets=[...selected];
   let ms;try{ms=getBatchIntervalMs()}catch(e){api.notify(e.userMessage||e.message);return}
   await runBatchTasks(`批量设置间隔 ${ms/1000}s`,targets,'control',async (id,context)=>{
    const reqId=crypto.randomUUID();
    return await api('/gateway/commands',{request_id:reqId,device_id:id,interval_ms:ms},{context,guard:()=>store.canWrite(id,{online:true})});
   });
  },'secondary');

  const batchRekey=button('批量换钥',async()=>{
   if(!selected.size)return;
   const targets=[...selected];
   const yes=await confirmAction(`${targets.length} 台设备`,'批量会话换钥',`rekey:${targets.length}`);
   if(!yes)return;
   await runBatchTasks('批量会话换钥',targets,'rekey',async (id,context)=>{
    const reqId=crypto.randomUUID();
    return await api('/devices/'+id+'/rekey',{request_id:reqId},{context,guard:()=>store.canWrite(id,{online:true})});
   });
  },'secondary');

  const batchConn=button('批量连接',async()=>{
   if(!selected.size)return;
   const targets=[...selected];
   await runBatchTasks('批量发起安全连接',targets,'connect',async (id,context)=>{
    return await api('/devices/'+id+'/connect',{},{context,guard:()=>store.canWrite(id)});
   });
  },'secondary');

  const batchDisc=button('批量断开',async()=>{
   if(!selected.size)return;
   const targets=[...selected];
   const yes=await confirmAction(`${targets.length}台设备`,'批量断开连接',`disconnect:${targets.length}`);
   if(!yes)return;
   await runBatchTasks('批量断开安全通道',targets,'disconnect',async (id,context)=>{
    return await api('/devices/'+id+'/disconnect',{},{context,guard:()=>store.canWrite(id)});
   });
  },'secondary');

  const selectAllBtn=button('全选全部设备',()=>{
   const s=store.snapshot;if(!s)return;
   for(const d of s.devices){
    const r=rows.get(d.id);
    if(r&&!r.tr.hidden){selected.add(d.id);r.checkbox.checked=true;r.tr.classList.add('selected-row')}
   }
   syncBatchBar();
  },'secondary');
  selectAllBtn.dataset.write='0';

  const batchClear=button('取消选择',()=>{
   selected.clear();selectAll.checked=false;
   for(const[,r]of rows){r.checkbox.checked=false;r.tr.classList.remove('selected-row')}
   syncBatchBar();
  },'clear-btn');
  batchClear.dataset.write='0';

  batchBar.append(batchCount,selectAllBtn,tempGroup,batchTele,intervalGroup,batchInterval,batchRekey,batchConn,batchDisc,batchClear);

 c.append(toolbar,summaryRow,result,wrap,batchBar,node('p','准入受理不代表认证在线。详情在侧栏打开；维护操作按设备展开。','table-foot'));

 function make(id){
  const tr=node('tr');tr.dataset.device=id;
  const checkTd=node('td',undefined,'device-select-cell'),checkbox=node('input');
  checkbox.type='checkbox';checkbox.title='选择 '+id;
  checkTd.append(checkbox);
  checkbox.onchange=()=>{
   if(checkbox.checked){selected.add(id);tr.classList.add('selected-row')}
   else{selected.delete(id);tr.classList.remove('selected-row')}
   syncBatchBar();
  };

  const name=node('td'),nameButton=readButton(id,()=>api.drawer.open('device',id),'link-button'),status=badge('unknown'),mode=node('td',undefined,'muted'),statusCell=node('td'),sampling=node('td'),received=node('td'),actions=node('td');name.append(nameButton);statusCell.append(status);
  const conn=button('连接 '+id,async()=>{await api('/devices/'+id+'/connect',{}, {context:store.context(id),guard:()=>store.canWrite(id)});api.notify(id+' 已受理连接调度，等待认证在线。')},'secondary');
  const view=readButton('查看 '+id,()=>api.drawer.open('device',id),'link-button'),more=node('details'),summary=node('summary','更多操作 '+id);more.append(summary);
  const disc=button('断开 '+id,async()=>{const context=store.context(id),yes=await confirmAction(id,'断开设备','disconnect:'+id);if(yes)await api('/devices/'+id+'/disconnect',{}, {context,guard:()=>store.canWrite(id)})},'secondary');
  const rekey=button('完整换钥 '+id,async()=>{const context=store.context(id),yes=await confirmAction(id,'完整会话换钥','rekey:'+id);if(yes)await api('/devices/'+id+'/rekey',{}, {context,guard:()=>store.canWrite(id,{online:true})})},'secondary');
  more.append(conn,disc,rekey);actions.append(view,more);
  tr.append(checkTd,name,statusCell,mode,sampling,received,actions);
  return {tr,status,mode,sampling,received,conn,disc,rekey,checkbox};
 }

 function refresh(){
  const s=store.snapshot;if(!s)return;const ids=new Set(s.devices.map(d=>d.id));
  for(const[id,r]of rows)if(!ids.has(id)){selected.delete(id);if(r.tr.contains(document.activeElement))filter.focus();r.tr.remove();rows.delete(id)}
  for(const d of s.devices){
   let r=rows.get(d.id);if(!r){r=make(d.id);rows.set(d.id,r);tbody.append(r.tr)}
   r.tr.hidden=!(d.id+' '+d.status+' '+(d.error||'')).toLowerCase().includes(filter.value.toLowerCase());
   updateBadge(r.status,store.fresh()?d.status:'未知／上次观测');
   setText(r.mode,d.sid?(d.suite===2?'国密混合通道 (hybrid)':'传统通道 (classic)'):(d.error||'尚无安全会话'));
   const intervalText=(d.status==='online'||(d.interval_ms&&d.interval_ms>0))?(d.interval_ms/1000)+'秒':'未观测';
   setText(r.sampling,intervalText+' / '+(d.auto?'采样中':'已暂停'));
   setText(r.received,d.last_received_temperature_c!=null?d.last_received_temperature_c+'℃':'未观测');
   guardButton(r.conn,()=>store.canWrite(d.id)||(['online','connecting'].includes(store.device(d.id)?.status)?'已有连接／连接中':''));
   guardButton(r.disc,()=>store.canWrite(d.id));
   guardButton(r.rekey,()=>store.canWrite(d.id,{online:true}));
  }
  setText(count,'当前设备池 '+s.devices.length+' 台 · 在线 '+s.devices.filter(d=>d.status==='online').length+' 台');
  guardButton(connect,()=>store.canWrite());
  guardButton(stop,()=>store.authValid()?'':'本机授权不可用');
  syncBatchBar();
  for(const r of batchRecords){const o=inspectBatchOutcome(store,r.context,r.kind,r.receipt);if(o.status!=='pending'){r.st.textContent=o.text;r.row.className='batch-device-item '+(o.status==='confirmed'?'done':['failed','cancelled'].includes(o.status)?'failed':'muted')}}
 }
 filter.oninput=refresh;c.refresh=refresh;refresh();return c;
}
