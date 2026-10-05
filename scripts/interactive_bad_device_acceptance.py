"""Actual installed local API/TCP partial fleet; no keys or login data saved."""
import argparse,datetime,hashlib,http.cookiejar,json,os,pathlib,queue,subprocess,threading,time,urllib.request,urllib.error
def save(p,v):p.write_text(json.dumps(v,ensure_ascii=False,indent=2)+'\n',encoding='utf-8')
def stamp():return datetime.datetime.now(datetime.timezone.utc).isoformat()
def main():
 ap=argparse.ArgumentParser();ap.add_argument('--exe',required=True);ap.add_argument('--out',required=True);ap.add_argument('--bad',choices=['D001','D010','D020'],required=True);a=ap.parse_args()
 exe=pathlib.Path(a.exe).resolve();out=pathlib.Path(a.out).resolve();out.mkdir(parents=True,exist_ok=False)
 p=subprocess.Popen([str(exe),'-devices','20','-no-browser'],cwd=exe.parent,stdout=subprocess.PIPE,stderr=subprocess.STDOUT,text=True,encoding='utf-8',errors='replace');q=queue.Queue();lines=[];started=stamp();url=code=run=None;api=[];ok=False
 def reader():
  for line in p.stdout:q.put(line);lines.append('[REDACTED LOCAL CODE]\n' if '口令（' in line else line)
 th=threading.Thread(target=reader,daemon=True);th.start()
 try:
  while not (url and code and run):
   line=q.get(timeout=20).strip()
   if line.startswith('PAGE_URL='):url=line.split('=',1)[1]
   if line.startswith('RUN_ID='):run=line.split('=',1)[1]
   if '口令（' in line:code=line.rsplit(': ',1)[1]
  client=urllib.request.build_opener(urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()));csrf=''
  def req(path,body=None,expected=200):
   headers={'Origin':url,'Content-Type':'application/json'}
   if body is not None and csrf:headers['X-CSRF-Token']=csrf
   r=urllib.request.Request(url+'/api/v1'+path,headers=headers,data=json.dumps(body).encode() if body is not None else None)
   try:resp=client.open(r,timeout=10)
   except urllib.error.HTTPError as e:resp=e
   b=resp.read();d=json.loads(b);api.append({'at':stamp(),'path':path,'status':resp.code,'body':'[REDACTED]' if path=='/login' else body,'response':'[REDACTED]' if path=='/login' else d});assert resp.code==expected,(path,resp.code,d);return d
  csrf=req('/login',{'code':code})['csrf_token'];code=None
  req('/identities/'+a.bad+'/revoke',{'confirm':'revoke:'+a.bad})
  batch=req('/run/start',{});assert batch['attempted']==20 and batch['accepted']==19 and batch['rejected']==1 and batch['status']=='partial'
  assert [r['device_id'] for r in batch['results'] if r['status']=='rejected']==[a.bad];save(out/'batch.json',batch)
  def wait(pred):
   end=time.monotonic()+12
   while time.monotonic()<end:
    s=req('/state')
    if pred(s):return s
    time.sleep(.1)
   raise AssertionError('real state wait timeout')
  s=wait(lambda s:sum(d['status']=='online' and d['suite']==2 for d in s['devices'])==19);save(out/'online19.json',s)
  healthy=[d for d in s['devices'] if d['id']!=a.bad]
  for d in healthy:
   req('/devices/'+d['id']+'/telemetry',{'request_id':'manual-'+d['id'],'temperature_c':28.7},202)
   req('/gateway/commands',{'request_id':'control-'+d['id'],'device_id':d['id'],'interval_ms':1000},202)
  s=wait(lambda s:len([o for o in s['operations'] if o['status']=='confirmed'])==38)
  assert len({d['sid'] for d in s['devices'] if d['id']!=a.bad})==19;save(out/'communication19.json',s)
  req('/run/stop',{});save(out/'stopped.json',req('/state'));ok=True
 finally:
  stop=subprocess.run([str(exe),'-stop'],cwd=exe.parent,capture_output=True,timeout=10);p.wait(timeout=15);th.join(timeout=2)
  save(out/'command.json',{'command':[str(exe),'-devices','20','-no-browser'],'started_utc':started,'finished_utc':stamp(),'run_id':run,'exit_code':p.returncode,'stop_exit_code':stop.returncode,'binary_sha256':hashlib.sha256(exe.read_bytes()).hexdigest(),'source_manifest_sha256':json.loads((exe.parent/'build_info.json').read_text())['source_manifest_sha256']})
  save(out/'api.json',api);(out/'startup_redacted.txt').write_text(''.join(lines),encoding='utf-8');save(out/'acceptance.json',{'passed':ok and p.returncode==0,'bad':a.bad,'run_id':run,'healthy_peers':19 if ok else None})
  if run:
   for active in (pathlib.Path(os.environ['LOCALAPPDATA'])/'EdgeHybridDemo').glob('*/interactive/active.json'):
    if json.loads(active.read_text())['run_id']==run:
     log=active.parent/'logs'/(run+'.jsonl')
     if log.is_file():(out/log.name).write_bytes(log.read_bytes())
 assert ok and p.returncode==0
 print(json.dumps({'passed':True,'bad':a.bad,'run_id':run}))
if __name__=='__main__':main()
