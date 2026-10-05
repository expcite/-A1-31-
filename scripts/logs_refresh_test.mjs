// Local DOM-adapter regression. Executes the actual logs.js/components.js/
// usability.js copies. Does NOT run a real browser, HTTP server or cryptography.
// Usage: node logs_refresh_test.mjs --assets PATH_TO_CONSOLE_ASSETS
import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
import os from 'node:os';
import path from 'node:path';
import {pathToFileURL} from 'node:url';

class Element {
  constructor(tag) { this.tagName=tag;this.children=[];this.parent=null;this.dataset={};this.listeners=new Map();this._text='';this._value=undefined;this.checked=false; }
  append(...items) { for(const v of items){const n=v instanceof Element?v:new Element('#text');if(n!==v)n.textContent=String(v);n.parent=this;this.children.push(n);} }
  replaceChildren(...items) { this.children=[];this._text='';this._value=undefined;this.append(...items); }
  get textContent(){return this._text+this.children.map(c=>c.textContent).join('');}
  set textContent(v){this._text=String(v);this.children=[];}
  get options(){return this.children.filter(c=>c.tagName==='option');}
  get value(){if(this.tagName==='select')return this._value??this.options[0]?.value??'';return this._value??this._text;}
  set value(v){this._value=String(v);}
  addEventListener(name,fn){this.listeners.set(name,fn);}
  closest(tag){for(let n=this;n;n=n.parent)if(n.tagName===tag)return n;return null;}
  querySelector(selector){return walk(this).find(n=>selector.startsWith('#')?n.id===selector.slice(1):selector==='[data-result]'?n.dataset.result!==undefined:n.tagName===selector)??null;}
}
function walk(root){return [root,...root.children.flatMap(walk)];}
const notice=new Element('div');notice.id='notice';
globalThis.Node=Element;
globalThis.document={createElement:tag=>new Element(tag),body:{dataset:{}},dispatchEvent(){},querySelector:s=>s==='#notice'?notice:null};
let clock=1_000_000;const oldNow=Date.now;Date.now=()=>clock;
const at='2026-10-02T00:00:00Z';
const ev=(id,stage,endpoint='device',device='D001')=>({event_id:id,stage,endpoint,device_id:device,at});
const initial=()=>({run_id:'run-A',cursor:1,events_omitted:0,devices:[{id:'D001'}],events:[ev(1,'initial')]});
const newer=()=>({run_id:'run-A',cursor:3,events_omitted:0,devices:[{id:'D001'},{id:'D002'}],events:[ev(1,'initial'),ev(2,'new_stage','management','D002'),ev(3,'new_stage','management','D002')]});
const arg=process.argv.indexOf('--assets');
if(arg<0||!process.argv[arg+1])throw Error('Required: --assets PATH');
const tmp=await fs.mkdtemp(path.join(os.tmpdir(),'edgehybrid-f07-'));
const cases=[];let failures=0;
try {
  await fs.writeFile(path.join(tmp,'package.json'),'{}');
  // Explicit .mjs avoids reliance on host package.json or Node defaults.
  for(const name of ['logs','components','usability']){
    let code=await fs.readFile(path.join(process.argv[arg+1],name+'.js'),'utf8');
    code=code.replaceAll("'./components.js'","'./components.mjs'").replaceAll("'./usability.js'","'./usability.mjs'");
    await fs.writeFile(path.join(tmp,name+'.mjs'),code,'utf8');
  }
  const {logs}=await import(pathToFileURL(path.join(tmp,'logs.mjs')));
  function setup(next=newer()){
    const calls=[];
    const view=logs(initial(),async(route,body)=>{calls.push({route,body});if(route==='/state'){if(next instanceof Error)throw next;return structuredClone(next);}return {url:'/exports/example',event_count:2,bytes:300,sha256:'0'.repeat(64)};});
    return {view,calls,selects:walk(view).filter(n=>n.tagName==='select')};
  }
  async function click(view,text){clock+=1000;const b=walk(view).find(n=>n.tagName==='button'&&n.textContent===text);assert.ok(b,'button exists: '+text);await b.listeners.get('click')();}
  function choose(s,v){assert.ok(s.options.some(o=>o.value===v),'option exists: '+v);s.value=v;s.onchange();}
  async function test(name,fn){try{await fn();cases.push({name,pass:true});console.log('PASS '+name);}catch(e){failures++;cases.push({name,pass:false,error:String(e)});console.log('FAIL '+name+' :: '+e.message);}}
  await test('normal initial filter works',async()=>{const {view,selects}=setup();choose(selects[2],'initial');assert.ok(view.textContent.includes('匹配 1条'));});
  await test('explicit refresh discovers endpoints and stages',async()=>{const {view,selects}=setup();await click(view,'更新记录范围');assert.ok(selects[1].options.some(o=>o.value==='management'));assert.ok(selects[2].options.some(o=>o.value==='new_stage'));});
  await test('new filter and export use same new scope',async()=>{const {view,selects,calls}=setup();await click(view,'更新记录范围');choose(selects[1],'management');choose(selects[2],'new_stage');assert.ok(view.textContent.includes('匹配 2条'));await click(view,'导出筛选记录 JSON');assert.deepEqual(calls.at(-1).body.selection,{run_id:'run-A',from_event_id:1,to_event_id:3,endpoint:'management',stage:'new_stage'});});
  await test('still-valid selection survives refresh',async()=>{const {view,selects}=setup();choose(selects[2],'initial');await click(view,'更新记录范围');assert.equal(selects[2].value,'initial');assert.ok(view.textContent.includes('匹配 1条'));});
  await test('vanished selection is explicit, not broadened',async()=>{const n=newer();n.events=n.events.slice(1);const {view,selects,calls}=setup(n);choose(selects[2],'initial');await click(view,'更新记录范围');assert.equal(selects[2].value,'initial');assert.ok(selects[2].options.find(o=>o.value==='initial').textContent.includes('当前范围无匹配'));assert.ok(view.textContent.includes('匹配 0条'));await click(view,'导出筛选记录 CSV');assert.equal(calls.at(-1).body.selection.stage,'initial');});
  await test('options rebuilt from unfiltered scope and deduplicated',async()=>{const {view,selects}=setup();choose(selects[0],'D001');await click(view,'更新记录范围');assert.equal(selects[1].options.filter(o=>o.value==='management').length,1);assert.equal(selects[2].options.filter(o=>o.value==='new_stage').length,1);assert.ok(selects[0].options.some(o=>o.value==='D002'));});
  await test('poll refresh does not change fixed range',async()=>{const {view,selects}=setup();view.refresh(newer());assert.ok(view.textContent.includes('事件 1～1'));assert.ok(!selects[2].options.some(o=>o.value==='new_stage'));});
  await test('API failure preserves old range and selections',async()=>{const {view,selects}=setup(Error('network failed'));choose(selects[2],'initial');await click(view,'更新记录范围');assert.ok(view.textContent.includes('事件 1～1'));assert.equal(selects[2].value,'initial');assert.equal(notice.textContent,'network failed');});
  await test('different run is not silently adopted',async()=>{const n=newer();n.run_id='run-B';const {view,calls}=setup(n);await click(view,'更新记录范围');assert.ok(view.textContent.includes('run run-A'));await click(view,'导出筛选记录 HTML');assert.equal(calls.at(-1).body.selection.run_id,'run-A');});
  await test('same-run empty snapshot renders empty without old facet claims',async()=>{const n={...initial(),events:[],cursor:0,devices:[]};const {view,selects}=setup(n);await click(view,'更新记录范围');assert.equal(selects[2].options.length,1);assert.ok(view.textContent.includes('匹配 0条'));});
  await test('option values remain text, not HTML markup',async()=>{const n=newer();const value='<img src=x onerror=alert(1)>';n.events.push(ev(4,value));n.cursor=4;const {view,selects}=setup(n);await click(view,'更新记录范围');const op=selects[2].options.find(o=>o.value===value);assert.ok(op);assert.equal(op.textContent,value);assert.equal(op.children.length,0);});
  await test('same API and run remount preserves snapshot filters and page',async()=>{
    const calls=[],api=async(route,body)=>{calls.push({route,body});return route==='/state'?newer():{url:'/exports/example',event_count:1,bytes:1,sha256:'0'.repeat(64)};};
    const many=initial();many.events=Array.from({length:45},(_,i)=>ev(i+1,'initial'));many.cursor=45;
    const first=logs(many,api),selects=walk(first).filter(n=>n.tagName==='select');choose(selects[2],'initial');await click(first,'下一页');
    const writes=walk(first).filter(n=>n.tagName==='button'&&n.dataset.write==='1');writes.forEach(b=>b.disabled=true);document.body.dataset.unavailable='1';logs(newer(),api);assert.ok(writes.every(b=>b.disabled));document.body.dataset.unavailable='0';writes[0].dataset.busy='1';
    const remounted=logs(newer(),api);assert.strictEqual(remounted,first);assert.ok(writes[0].disabled);assert.ok(writes.slice(1).every(b=>!b.disabled));delete writes[0].dataset.busy;logs(newer(),api);assert.ok(writes.every(b=>!b.disabled));assert.ok(remounted.textContent.includes('事件 1～45'));assert.ok(remounted.textContent.includes('第 2/3页'));assert.equal(selects[2].value,'initial');await click(remounted,'导出筛选记录 JSON');assert.equal(calls.at(-1).body.selection.to_event_id,45);assert.equal(calls.at(-1).body.selection.stage,'initial');
  });
  await test('new API or run never reuses previous view',async()=>{
    const api=async()=>({});const first=logs(initial(),api);choose(walk(first).filter(n=>n.tagName==='select')[2],'initial');const next={...newer(),run_id:'run-B'};const second=logs(next,api);assert.notStrictEqual(second,first);assert.ok(second.textContent.includes('run run-B'));assert.equal(walk(second).filter(n=>n.tagName==='select')[2].value,'');const third=logs(next,async()=>({}));assert.notStrictEqual(third,second);
  });
  console.log(JSON.stringify({scope:'actual module copies under a minimal DOM adapter; not a browser or network test',passed:cases.length-failures,failed:failures,cases},null,2));
} finally {Date.now=oldNow;await fs.rm(tmp,{recursive:true,force:true});}
process.exitCode=failures?1:0;
