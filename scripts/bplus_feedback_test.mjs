// Presentation-only DOM adapter; browser evidence is recorded separately.
import assert from 'node:assert/strict';
import {fieldFeedback} from '../internal/console/assets/components.js';
import {temperature,secondsToMillis} from '../internal/console/assets/usability.js';
class Element{constructor(){this.attrs={};this.events={};this.value='';this.focused=false}setAttribute(k,v){this.attrs[k]=v}removeAttribute(k){delete this.attrs[k]}addEventListener(k,v){this.events[k]=v}focus(){this.focused=true}}
globalThis.document={createElement:()=>new Element()};
const results=[];
function test(name,fn){try{fn();results.push({name,status:'PASS'})}catch(e){results.push({name,status:'FAIL',error:e.stack})}}
const input=new Element();input.id='temperature';const f=fieldFeedback(input);
test('error is associated with the field and starts hidden',()=>{assert.equal(input.attrs['aria-describedby'],'temperature-error');assert.equal(f.message.attrs.role,'alert');assert.equal(f.message.hidden,true)});
test('invalid input focuses the field and preserves original validation refusal',()=>{input.value='200';assert.throws(()=>f.validate(temperature),e=>e.fieldValidation===true);assert.equal(input.attrs['aria-invalid'],'true');assert.equal(input.focused,true);assert.equal(f.message.hidden,false);assert.ok(f.message.textContent.length)});
test('typing clears obsolete error without submitting',()=>{input.events.input();assert.equal(f.message.hidden,true);assert.equal(input.attrs['aria-invalid'],undefined)});
test('valid value retains the exact business conversion',()=>{input.value='28.7';assert.equal(f.validate(temperature),28.7);input.value='5';assert.equal(f.validate(secondsToMillis),5000);assert.equal(f.message.hidden,true)});
test('HTML-looking invalid input remains literal error text',()=>{input.value='<img>';assert.throws(()=>f.validate(()=>{throw Error(input.value)}));assert.equal(f.message.textContent,'<img>')});
console.log(JSON.stringify({scope:'isolated presentation validation; no Channel/TCP claim',results,passed:results.filter(r=>r.status==='PASS').length,failed:results.filter(r=>r.status==='FAIL').length},null,2));process.exitCode=results.some(r=>r.status==='FAIL')?1:0;
