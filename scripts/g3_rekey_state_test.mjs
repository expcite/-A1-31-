// UI-only fixture tests. Real Channel/TCP tests live in rekey_receipt_test.go.
import assert from 'node:assert/strict';
import path from 'node:path';
import {pathToFileURL} from 'node:url';
const at=process.argv.indexOf('--assets');
const assets=at<0?path.resolve('internal/console/assets'):path.resolve(process.argv[at+1]);
const {guideResults,GuideController}=await import(pathToFileURL(path.join(assets,'guide.js')));
const {ConsoleStore}=await import(pathToFileURL(path.join(assets,'store.js')));
const rows=[];
function test(name,fn){try{fn();rows.push({name,status:'PASS'})}catch(e){rows.push({name,status:'FAIL',error:e.stack})}}
function fixture(){
 const store=new ConsoleStore({now:()=>0,wall:()=>0});store.authorize(new Date(3600000).toISOString());
 const snapshot={run_id:'run',cursor:10,events:[],devices:[{id:'D001',status:'online',sid:'old'}],operations:[],rekeys:[]};
 store.accept(snapshot,store.ticket());const guide=new GuideController(store);guide.enabled=true;const g=guide.select();
 const receipt={run_id:'run',device_id:'D001',rekey_id:'r1',old_sid:'old',new_sid:'new',status:'confirmed',queued_at:'2026-10-05T01:00:00Z',started_at:'2026-10-05T01:00:01Z',client_authenticated_at:'2026-10-05T01:00:02Z',gateway_authenticated_at:'2026-10-05T01:00:02Z',completed_at:'2026-10-05T01:00:03Z'};
 guide.rekey('accepted',store.context('D001'),g.guide_session_id,receipt);snapshot.devices[0].sid='new';snapshot.rekeys=[receipt];
 const verify={run_id:'run',device_id:'D001',operation_id:'v1',kind:'telemetry',sid:'new',status:'confirmed',queued_at:'2026-10-05T01:00:04Z',confirmed_at:'2026-10-05T01:00:05Z'};
 snapshot.operations=[verify];g.refs.verify='v1';return {g,snapshot,receipt,verify,guide,store,result(){return guideResults(g,snapshot,true)[2]}};
}
test('exact causal receipt and later protected message complete G3',()=>assert.equal(fixture().result(),'本次完整换钥双端准入已关联，新SID验证消息已确认'));
for(const [field,value] of [['rekey_id','other'],['run_id','other'],['device_id','D002'],['old_sid','other'],['new_sid','other'],['status','failed'],['client_authenticated_at',null],['gateway_authenticated_at',null],['completed_at',null]])test('reject receipt '+field,()=>{const f=fixture();f.receipt[field]=value;assert.doesNotMatch(f.result(),/^本次完整换钥双端准入已关联，新SID验证消息已确认$/)});
for(const [field,value]of [['sid','old'],['status','unconfirmed'],['device_id','D002'],['queued_at','2026-10-05T01:00:00Z'],['confirmed_at',null]])test('reject verification '+field,()=>{const f=fixture();f.verify[field]=value;assert.doesNotMatch(f.result(),/^本次完整换钥双端准入已关联，新SID验证消息已确认$/)});
test('evicted receipt keeps evidence insufficient',()=>{const f=fixture();f.snapshot.rekeys=[];assert.match(f.result(),/关联证据不足/)});
test('stale snapshot cannot complete G3',()=>{const f=fixture();assert.match(guideResults(f.g,f.snapshot,false)[2],/过期.*未知/)});
test('foreign rekey response never stored as this guide receipt',()=>{const f=fixture();f.guide.rekey('accepted',{run:'run',device:'D001',sid:'old'},f.g.guide_session_id,{...f.receipt,device_id:'D002'});assert.equal(f.g.rekey.rekey_id,undefined);assert.match(f.result(),/关联证据不足/)});
test('legacy accepted plus changed SID remains insufficient',()=>{const f=fixture();f.guide.rekey('accepted',{run:'run',device:'D001',sid:'old'},f.g.guide_session_id);assert.match(f.result(),/关联证据不足/)});
console.log(JSON.stringify({scope:'isolated UI_ONLY fixtures; no TCP or browser claim',passed:rows.filter(r=>r.status==='PASS').length,failed:rows.filter(r=>r.status==='FAIL').length,cases:rows},null,2));process.exitCode=rows.some(r=>r.status==='FAIL')?1:0;
