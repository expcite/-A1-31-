import assert from 'node:assert/strict';
import {pendingControls} from '../internal/console/assets/state.js';
import {batchMessage} from '../internal/console/assets/batch.js';
const operations=['queued','writing','written','unconfirmed','confirmed','late_confirmed','failed','cancelled'].map(status=>({kind:'control',status}));
assert.equal(pendingControls(operations),4);
assert.equal(pendingControls([{kind:'telemetry',status:'queued'}]),0);
assert.equal(pendingControls([{kind:'control',status:'failed'},{kind:'control',status:'cancelled'}]),0);
assert.equal(pendingControls([]),0);
assert.match(batchMessage({attempted:20,accepted:19,rejected:1,results:[{device_id:'D001',status:'rejected',error:'revoked'},{device_id:'D020',status:'accepted'}]}),/尝试 20，准入 19，拒绝 1/);
assert.match(batchMessage({attempted:20,accepted:19,rejected:1,results:[{device_id:'D001',status:'rejected',error:'revoked'}]}),/D001 rejected：revoked/);
assert.equal(batchMessage({}),null);
console.log(JSON.stringify({test:'pending control state and partial batch display',status:'PASS',assertions:7}));
const {secondsToMillis,operationMessage,errorMessage,filterEvents,pageEvents,temperature}=await import('../internal/console/assets/usability.js');
let checks=0;for(const [s,ms]of [['1',1000],['5',5000],['10',10000],['.1',null],['0.1',100],['60',60000],['1.001',1001],[' 5 ',5000]]){if(ms===null)assert.throws(()=>secondsToMillis(s));else assert.equal(secondsToMillis(s),ms);checks++;}
for(const s of ['','NaN','Infinity','1e3','1.0001','0.099','60.001','-1','a','5.']){assert.throws(()=>secondsToMillis(s));checks++;}
assert.match(operationMessage({kind:'control',status:'unconfirmed'}),/可能已执行/);checks++;
assert.match(operationMessage({kind:'telemetry',status:'confirmed',temperature_c:26.3}),/网关已收到 26.3℃/);checks++;
assert.match(operationMessage({kind:'control',status:'confirmed',interval_ms:5000}),/5秒.*验证回执/);checks++;
assert.match(errorMessage({code:'operation_limit'}),/换钥不会/);checks++;
assert.match(errorMessage({code:'queue_full'}),/等当前/);checks++;
const events=Array.from({length:43},(_,i)=>({event_id:i+1,device_id:i%2?'D001':'D002',stage:'confirmed',endpoint:'gateway'}));
assert.equal(filterEvents(events,{device_id:'D001'}).length,21);checks++;assert.equal(pageEvents(events,2).rows.length,20);checks++;assert.equal(pageEvents(events,9).page,3);checks++;assert.equal(pageEvents([],1).pages,1);checks++;
for(const value of [26.3,28.7,0,-50,150]){assert.equal(temperature(String(value)),value);checks++;}
for(const value of ['', 'NaN','151','-51']){assert.throws(()=>temperature(value));checks++;}
console.log(JSON.stringify({test:'novice presentation, exact units, truthful states and bounded pagination',status:'PASS',assertions:checks}));
