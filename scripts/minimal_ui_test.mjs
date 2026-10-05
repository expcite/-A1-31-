import assert from 'node:assert/strict';
import {batchSummary} from '../internal/console/assets/batch.js';
const rows=[];
function test(name,f){try{f();rows.push({name,status:'PASS'})}catch(e){rows.push({name,status:'FAIL',error:e.stack})}}
const results=Array.from({length:20},(_,i)=>({device_id:'D'+String(i+1).padStart(3,'0'),status:'accepted'}));
test('20 successful admissions remain compact and distinguish online',()=>{const text=batchSummary({attempted:20,accepted:20,rejected:0,results});assert.match(text,/尝试 20.*准入 20.*拒绝 0.*不代表握手完成/);assert.ok(text.length<100);assert.doesNotMatch(text,/D001|D020/)});
for(const bad of [0,9,19])test('first/middle/last partial failure counts retained '+bad,()=>{const r=results.map((x,i)=>({...x,status:i===bad?'rejected':'accepted'}));assert.match(batchSummary({attempted:20,accepted:19,rejected:1,results:r}),/准入 19.*拒绝 1/)});
test('unknown or inconsistent batch cannot be displayed as success',()=>{for(const x of [null,{}, {attempted:20,accepted:20,rejected:0,results:[]},{attempted:20,accepted:19,rejected:0,results}])assert.match(batchSummary(x),/不能核验/)});
console.log(JSON.stringify({scope:'UI_ONLY presentation tests; not TCP admission evidence',passed:rows.filter(r=>r.status==='PASS').length,failed:rows.filter(r=>r.status==='FAIL').length,cases:rows},null,2));process.exitCode=rows.some(r=>r.status==='FAIL')?1:0;
