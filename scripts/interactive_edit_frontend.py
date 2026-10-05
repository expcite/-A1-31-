"""Small deterministic migration of the already implemented local workbench."""
from pathlib import Path
root=Path(__file__).resolve().parents[1]
p=root/'internal/console/assets/pages.js'
s=p.read_text(encoding='utf-8')
if 'requestId]=field' not in s:
 s=s.replace("const [tl,temp]=field", "const [rl,requestId]=field('幂等操作ID（留空自动）','text','');requestId.maxLength=128;const [tl,temp]=field")
s=s.replace("form.append(select,tl,button", "form.append(select,rl,tl,button")
s=s.replace('request_id:crypto.randomUUID()', 'request_id:requestId.value||crypto.randomUUID()')
s=s.replace("o.kind,badge(o.status),detail(o)", "o.kind,badge(o.status),(()=>{const box=node('div');box.append(detail(o));if(o.status==='queued')box.append(button('取消未写出 '+o.operation_id,()=>api('/operations/'+encodeURIComponent(o.operation_id)+'/cancel',{}),'secondary'));return box})()")
p.write_text(s,encoding='utf-8')
p=root/'internal/console/assets/jobs.js';s=p.read_text(encoding='utf-8').replace("badge(j.state)+''",'badge(j.state)');p.write_text(s,encoding='utf-8')
p=root/'internal/console/assets/app.js';s=p.read_text(encoding='utf-8').replace('[n.parentElement.id,n.cloneNode(true)]','[n.id||n.parentElement.id,n.cloneNode(true)]');p.write_text(s,encoding='utf-8')
