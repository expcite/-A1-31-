"""Actual new Windows EXE gateway maintenance, generation evidence and restart.
Review tooling only; credentials in memory, never part of captured evidence.
"""
import argparse,datetime,hashlib,http.cookiejar,json,os,pathlib,queue,subprocess,threading,time,urllib.request,urllib.error
def stamp():return datetime.datetime.now(datetime.timezone.utc).isoformat()
def save(p,v):p.write_text(json.dumps(v,ensure_ascii=False,indent=2)+'\n',encoding='utf-8')
class Run:
 def __init__(self,exe,out,count):
  self.exe=exe;self.out=out;self.count=count;out.mkdir(parents=True,exist_ok=False);self.api=[];self.lines=[];self.csrf='';self.run=None;self.start=stamp()
  env=os.environ.copy();system_root=os.environ['SystemRoot'];env['PATH']=str(pathlib.Path(system_root)/'System32')+os.pathsep+system_root;self.command=[str(exe),'-devices',str(count),'-no-browser'];self.p=subprocess.Popen(self.command,cwd=exe.parent,env=env,stdout=subprocess.PIPE,stderr=subprocess.STDOUT,text=True,encoding='utf-8',errors='replace');q=queue.Queue()
  def read():
   for line in self.p.stdout:q.put(line);self.lines.append('[REDACTED LOCAL CODE]\n' if '口令（' in line else line)
  self.thread=threading.Thread(target=read,daemon=True);self.thread.start();code=None;self.url=None
  while not(self.url and self.run and code):
   line=q.get(timeout=20).strip()
   if line.startswith('PAGE_URL='):self.url=line.split('=',1)[1]
   if line.startswith('RUN_ID='):self.run=line.split('=',1)[1]
   if '口令（' in line:code=line.rsplit(': ',1)[1]
  self.client=urllib.request.build_opener(urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()));self.csrf=self.req('/login',{'code':code})['csrf_token'];code=None
  build=self.req('/build');info=json.loads((exe.parent/'build_info.json').read_text());assert build['source_manifest_sha256']==info['source_manifest_sha256'];assert hashlib.sha256(exe.read_bytes()).hexdigest()==info['binaries'][exe.name]['sha256']
 def req(self,path,body=None,expected=200):
  headers={'Origin':self.url,'Content-Type':'application/json'}
  if body is not None and self.csrf:headers['X-CSRF-Token']=self.csrf
  r=urllib.request.Request(self.url+'/api/v1'+path,headers=headers,data=json.dumps(body).encode() if body is not None else None)
  try:resp=self.client.open(r,timeout=12)
  except urllib.error.HTTPError as e:resp=e
  raw=resp.read()
  try:d=json.loads(raw)
  except ValueError:d=raw.decode('utf-8','replace')
  self.api.append({'at':stamp(),'path':path,'status':resp.code,'request':'[REDACTED]' if path=='/login' else body,'response':'[REDACTED]' if path in ['/login','/session'] else d});assert resp.code==expected,(path,resp.code,d);return d
 def wait(self,pred):
  end=time.monotonic()+15
  while time.monotonic()<end:
   s=self.req('/state')
   if pred(s):return s
   time.sleep(.1)
  raise AssertionError('state wait timeout')
 def close(self):
  if self.p.poll() is None:
   stop=subprocess.run([str(self.exe),'-stop'],cwd=self.exe.parent,capture_output=True,timeout=10);save(self.out/'stop.command.json',{'command':[str(self.exe),'-stop'],'exit_code':stop.returncode});assert stop.returncode==0
  self.p.wait(timeout=15);self.thread.join(timeout=2)
  save(self.out/'command.json',{'command':self.command,'started_utc':self.start,'finished_utc':stamp(),'run_id':self.run,'exit_code':self.p.returncode,'binary_sha256':hashlib.sha256(self.exe.read_bytes()).hexdigest(),'source_manifest_sha256':json.loads((self.exe.parent/'build_info.json').read_text())['source_manifest_sha256']});save(self.out/'api.json',self.api);(self.out/'startup_redacted.txt').write_text(''.join(self.lines),encoding='utf-8')
  for active in (pathlib.Path(os.environ['LOCALAPPDATA'])/'EdgeHybridDemo').glob('*/interactive/active.json'):
   if json.loads(active.read_text()).get('run_id')==self.run:
    log=active.parent/'logs'/(self.run+'.jsonl');(self.out/log.name).write_bytes(log.read_bytes())
  assert self.p.returncode==0
def main():
 ap=argparse.ArgumentParser();ap.add_argument('--exe',required=True);ap.add_argument('--out',required=True);ap.add_argument('--devices',type=int,choices=[1,20],required=True);a=ap.parse_args();exe=pathlib.Path(a.exe).resolve();out=pathlib.Path(a.out).resolve();out.mkdir(parents=True,exist_ok=False);r=None;checks={};ok=False
 try:
  r=Run(exe,out/'rotation',a.devices);r.req('/run/start',{});before=r.wait(lambda s:sum(d['status']=='online' and d['suite']==2 for d in s['devices'])==a.devices);save(r.out/'generation_before.json',before)
  for d in before['devices']:r.req('/devices/'+d['id']+'/sampling',{'auto':False,'temperature_c':25})
  for label,temp in [('263',26.3),('287',28.7)]:
   r.req('/devices/D001/telemetry',{'request_id':'old-manual-'+label,'temperature_c':temp},202);s=r.wait(lambda s:any(o['operation_id']=='old-manual-'+label and o['status']=='confirmed' for o in s['operations']));assert s['devices'][0]['last_received_temperature_c']==temp;save(r.out/('manual_'+label+'.json'),s)
  r.req('/identities/GW001/rotate',{'confirm':'rotate:GW001'},409)
  result=r.req('/identities/GW001/rotate',{'confirm':'rotate:GW001:ALL'});save(r.out/'maintenance_receipt.json',result);assert result['all_sessions_closed'] and result['admission_reopened'] and result['commit']['registry_committed'];closed=r.req('/state');assert all(d['status']=='offline' for d in closed['devices']);assert closed['gateway']['generation']==before['gateway']['generation']+1
  r.req('/run/start',{});after=r.wait(lambda s:sum(d['status']=='online' and d['suite']==2 for d in s['devices'])==a.devices);save(r.out/'generation_after.json',after)
  assert all(a1['sid']!=b['sid'] for a1,b in zip(after['devices'],before['devices']))
  for d in after['devices']:
   snapshots=[e['gateway_authenticated'] for e in after['events'] if e['device_id']==d['id'] and e['endpoint']=='device' and e['stage']=='authenticated' and e.get('gateway_authenticated')]
   assert any(g['KeyGeneration']==after['gateway']['generation'] and g['SignKeyID']==after['gateway']['sign_key_id'] for g in snapshots)
  accepted=after['devices'][0]['accepted_samples'];old=r.req('/devices/D001/telemetry',{'request_id':'old-manual-287','temperature_c':28.7},202);assert old['sid']==before['devices'][0]['sid'];assert r.req('/state')['devices'][0]['accepted_samples']==accepted
  for d in after['devices']:
   r.req('/devices/'+d['id']+'/telemetry',{'request_id':'new-temp-'+d['id'],'temperature_c':28.7},202);r.req('/gateway/commands',{'request_id':'new-period-'+d['id'],'device_id':d['id'],'interval_ms':5000},202)
  s=r.wait(lambda s:sum(o['status']=='confirmed' and o['operation_id'].startswith('new-') for o in s['operations'])==a.devices*2);save(r.out/'new_generation_communication.json',s);assert all(d['interval_ms']==5000 for d in s['devices']);checks['new_generation_real_channel_all_devices']=True;checks['old_idempotent_request_not_replayed']=True
  sel={'run_id':s['run_id'],'from_event_id':s['events'][0]['event_id'],'to_event_id':s['cursor'],'device_id':'D001','endpoint':'gateway','stage':'telemetry_accepted'};export=r.req('/exports',{'format':'json','selection':sel},201);doc=r.req(export['url'].removeprefix('/api/v1'));assert doc['selection']==sel;assert doc['event_count']==len(doc['events']);assert all(e['device_id']=='D001' and e['stage']=='telemetry_accepted' for e in doc['events']);save(r.out/'filtered_export.json',doc);checks['filtered_export_same_scope']=True
  revoked=r.req('/identities/GW001/revoke',{'confirm':'revoke:GW001:ALL'});assert revoked['all_sessions_closed'] and not revoked['admission_reopened'];r.req('/devices/D001/connect',{},409);save(r.out/'revoked.json',r.req('/state'));r.close();r=None
  r=Run(exe,out/'revoked_restart',a.devices);s=r.req('/state');assert s['gateway']['lifecycle']=='REVOKED';batch=r.req('/run/start',{});assert batch['attempted']==a.devices and batch['rejected']==a.devices;r.req('/identities/GW001/rotate',{'confirm':'rotate:GW001:ALL'},409);save(r.out/'restart_refusal.json',s);checks['revocation_persists_restart']=True
  retire=r.req('/identities/GW001/retire',{'confirm':'retire:GW001:ALL'});assert retire['commit']['lifecycle']=='RETIRED' and retire['commit']['old_files_removed'];r.req('/devices/D001/connect',{},409);save(r.out/'retired.json',r.req('/state'));r.close();r=None
  r=Run(exe,out/'retired_restart',a.devices);assert r.req('/state')['gateway']['lifecycle']=='RETIRED';r.req('/devices/D001/connect',{},409);r.req('/identities/GW001/cleanup',{'confirm':'cleanup:GW001:ALL'});r.req('/run/exit',{'confirm':'exit:'+r.run});r.p.wait(timeout=15);checks['retirement_cleanup_restart_and_exit']=True;ok=True
 finally:
  if r is not None:r.close()
  save(out/'acceptance.json',{'passed':ok,'devices':a.devices,'checks':checks,'source_manifest_sha256':json.loads((exe.parent/'build_info.json').read_text())['source_manifest_sha256'],'binary_sha256':hashlib.sha256(exe.read_bytes()).hexdigest(),'scope':'Actual Windows local Suite2 endpoints; no formal performance resampling.'})
 assert ok;print(json.dumps({'passed':True,'devices':a.devices,'checks':checks}))
if __name__=='__main__':main()
