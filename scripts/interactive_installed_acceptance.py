"""Independent extracted Windows acceptance. Python is a REVIEW tool only.
The launched app's PATH contains only Windows system directories. Credentials
remain in memory and are redacted from all saved request/startup evidence.
No performance formal batch is sampled and no private directory is copied.
"""
import argparse,datetime,hashlib,http.cookiejar,json,os,pathlib,queue,socket,subprocess,sys,threading,time,urllib.request,urllib.error
def stamp():return datetime.datetime.now(datetime.timezone.utc).isoformat()
def save(p,v):p.write_text(json.dumps(v,ensure_ascii=False,indent=2)+'\n',encoding='utf-8')
def sha(p):return hashlib.sha256(p.read_bytes()).hexdigest()
def main():
 ap=argparse.ArgumentParser();ap.add_argument('--exe',required=True);ap.add_argument('--out',required=True);ap.add_argument('--devices',type=int,choices=[1,20],required=True);ap.add_argument('--process-checks',action='store_true');ap.add_argument('--jobs',action='store_true');ap.add_argument('--launcher',action='store_true');a=ap.parse_args()
 exe=pathlib.Path(a.exe).resolve();out=pathlib.Path(a.out).resolve();out.mkdir(parents=True,exist_ok=False)
 env=os.environ.copy();env['PATH']=str(pathlib.Path(os.environ['SystemRoot'])/'System32')+os.pathsep+os.environ['SystemRoot']
 cmd=[str(exe),'-devices',str(a.devices),'-no-browser']
 if a.launcher:cmd=[os.environ['ComSpec'],'/d','/c',str(exe.parent/('启动'+str(a.devices)+'设备.cmd'))]
 started=stamp();p=subprocess.Popen(cmd,cwd=exe.parent,env=env,stdout=subprocess.PIPE,stderr=subprocess.STDOUT,text=True,encoding='utf-8',errors='replace')
 lines=[];q=queue.Queue();url=None;code=None;run=None;stop_result=None;ok=False;checks={};api=[]
 def reader():
  for line in p.stdout:
   q.put(line);lines.append('本机一次口令: [REDACTED]\n' if '口令（' in line else line)
 thread=threading.Thread(target=reader,daemon=True);thread.start()
 try:
  deadline=time.monotonic()+20
  while not (url and code and run):
   if time.monotonic()>deadline:raise RuntimeError('startup timeout')
   line=q.get(timeout=10).strip()
   if line.startswith('PAGE_URL='):url=line.split('=',1)[1]
   if line.startswith('RUN_ID='):run=line.split('=',1)[1]
   if '口令（' in line:code=line.rsplit(': ',1)[1]
  jar=http.cookiejar.CookieJar();client=urllib.request.build_opener(urllib.request.HTTPCookieProcessor(jar));csrf=''
  def request(method,path,body=None,expected=200,authenticated=True):
   headers={'Origin':url,'Content-Type':'application/json'}
   if method=='POST' and csrf:headers['X-CSRF-Token']=csrf
   req=urllib.request.Request(url+'/api/v1'+path,data=json.dumps(body).encode() if body is not None else None,headers=headers,method=method)
   begun=stamp()
   try:r=(client if authenticated else urllib.request.build_opener()).open(req,timeout=10)
   except urllib.error.HTTPError as e:r=e
   status=r.code;raw=r.read()
   try:data=json.loads(raw)
   except (ValueError,UnicodeError):data=raw.decode('utf-8','replace')
   api.append({'at':begun,'method':method,'path':path,'request':{'redacted':True} if path=='/login' else body,'status':status,'response':{'redacted_credentials':True} if path in ['/login','/session'] else data})
   if status!=expected:raise AssertionError((method,path,status,data))
   return data,raw,r.headers
  request('GET','/state',expected=401,authenticated=False)
  login,_,_=request('POST','/login',{'code':code});csrf=login['csrf_token'];code=None
  build,_,_=request('GET','/build');info=json.loads((exe.parent/'build_info.json').read_text())
  assert build['source_manifest_sha256']==info['source_manifest_sha256'];assert sha(exe)==info['binaries'][exe.name]['sha256'];checks['source_binary_link']=True
  if a.process_checks:
   duplicate=subprocess.run(cmd,cwd=exe.parent,env=env,capture_output=True,timeout=10)
   (out/'duplicate.stdout.txt').write_bytes(duplicate.stdout);(out/'duplicate.stderr.txt').write_bytes(duplicate.stderr);save(out/'duplicate.command.json',{'command':cmd,'exit_code':duplicate.returncode});assert duplicate.returncode==2;checks['duplicate_instance_rejected']=True
  def state():return request('GET','/state')[0]
  def wait(predicate,seconds=8):
   end=time.monotonic()+seconds
   while time.monotonic()<end:
    s=state()
    if predicate(s):return s
    time.sleep(.1)
   raise AssertionError('observable state timeout')
  request('POST','/run/start',{})
  s=wait(lambda s:len([d for d in s['devices'] if d['status']=='online' and d['suite']==2])==a.devices)
  save(out/'authenticated.json',s);checks['all_real_hybrid']=True
  s=wait(lambda s:all(d['accepted_samples']>=3 for d in s['devices']),seconds=8);save(out/'three_samples_each.json',s)
  for d in s['devices']:request('POST','/gateway/commands',{'request_id':'extract-control-'+d['id'],'device_id':d['id'],'interval_ms':1000},expected=202)
  s=wait(lambda s:len([o for o in s['operations'] if o['kind']=='control' and o['status']=='confirmed'])==a.devices)
  save(out/'controls_each.json',s);checks['each_three_samples_and_control']=True
  request('POST','/devices/D001/sampling',{'auto':False,'temperature_c':25})
  for suffix,temp in [('263',26.3),('287',28.7)]:
   id='extract-manual-'+suffix;body={'request_id':id,'temperature_c':temp};request('POST','/devices/D001/telemetry',body,expected=202)
   s=wait(lambda s:any(o['operation_id']==id and o['status']=='confirmed' for o in s['operations']));assert s['devices'][0]['last_received_temperature_c']==temp
   before=s['devices'][0]['accepted_samples'];request('POST','/devices/D001/telemetry',body,expected=202);time.sleep(.05);assert state()['devices'][0]['accepted_samples']==before
   request('POST','/devices/D001/telemetry',dict(body,temperature_c=temp+1),expected=409)
   save(out/('manual_'+suffix+'.json'),s)
  checks['manual_and_idempotency']=True
  before=state()['devices'][0]['accepted_samples'];time.sleep(.35)
  request('POST','/gateway/commands',{'request_id':'extract-idle','device_id':'D001','interval_ms':5000},expected=202)
  s=wait(lambda s:any(o['operation_id']=='extract-idle' and o['status']=='confirmed' and o['applied'] is True and o['peer_processed_at'] is not None for o in s['operations']))
  assert s['devices'][0]['accepted_samples']==before;assert s['devices'][0]['interval_ms']==5000
  if a.devices==20:assert s['devices'][1]['interval_ms']==1000
  save(out/'idle_control.json',s);checks['active_idle_control_and_isolation']=True
  old=s['devices'][0]['sid'];request('POST','/devices/D001/rekey',{},expected=202);s=wait(lambda s:s['devices'][0]['status']=='online' and s['devices'][0]['sid']!=old);save(out/'rekey.json',s);checks['full_new_session']=True
  request('POST','/jobs',{'kind':'m6_rules'},expected=409);checks['live_experiment_exclusion']=True
  # Exercise the actual run ledger limit without injecting a fake Channel.
  existing=len(state()['operations'])
  for i in range(256-existing):
   time.sleep(.02) # Stay below documented 120 requests/s; first overrate run is retained.
   request('POST','/gateway/commands',{'request_id':'bounded-'+str(i),'device_id':'D001','interval_ms':1000},expected=202)
  request('POST','/gateway/commands',{'request_id':'bounded-overflow','device_id':'D001','interval_ms':1000},expected=409)
  s=state();assert len(s['operations'])==256 and len(s['events'])<=2000
  save(out/'bounded_state.json',s);checks['operation_limit_rejected_and_event_ring_bounded']=True
  for fmt in ['json','csv','html']:
   result,_,_=request('POST','/exports',{'format':fmt},expected=201)
   content,raw,headers=request('GET',result['url'].removeprefix('/api/v1'))
   assert hashlib.sha256(raw).hexdigest()==result['sha256'];assert len(raw)==result['bytes'];(out/('export.'+fmt)).write_bytes(raw);save(out/('export_'+fmt+'_receipt.json'),result)
   if fmt=='json':assert all('temperature_c' not in e and e['detail']==e['stage'] for e in content['events'])
  checks['exports_hashed_and_default_metadata']=True
  request('POST','/devices/D001/disconnect',{});request('POST','/gateway/commands',{'device_id':'D001','request_id':'offline-control','interval_ms':1000},expected=409);checks['offline_no_fake_ack']=True
  request('POST','/run/stop',{});s=wait(lambda s:not any(d['status']=='online' for d in s['devices']));save(out/'stopped_devices.json',s);checks['workers_stopped']=True
  # M6 rules through actual fixed installed test binary; not formal measurement.
  canceled,_,_=request('POST','/jobs',{'kind':'m6_rules'},expected=202)
  request('POST','/jobs/'+canceled['job_id']+'/cancel',{})
  end=time.monotonic()+10
  while time.monotonic()<end:
   canceled=request('GET','/jobs/'+canceled['job_id'])[0]
   if canceled['exit_code'] is not None:break
   time.sleep(.05)
  save(out/'canceled_job.json',canceled);assert canceled['state']=='cancelled' and canceled['exit_code'] is not None;checks['real_child_process_cancel_and_exit']=True
  job,_,_=request('POST','/jobs',{'kind':'m6_rules'},expected=202)
  end=time.monotonic()+40
  while time.monotonic()<end:
   job=request('GET','/jobs/'+job['job_id'])[0]
   if job['exit_code'] is not None:break
   time.sleep(.15)
  save(out/'m6_job.json',job);assert job['state']=='completed' and job['exit_code']==0;checks['installed_isolated_m6']=True
  if a.jobs:
   for kind in ['bench_T','bench_H','pbench']:
    job,_,_=request('POST','/jobs',{'kind':kind},expected=202)
    end=time.monotonic()+40
    while time.monotonic()<end:
     job=request('GET','/jobs/'+job['job_id'])[0]
     if job['exit_code'] is not None:break
     time.sleep(.1)
    save(out/(kind+'_job.json'),job);assert job['state']=='completed' and job['exit_code']==0
   checks['installed_T_H_P_smoke_and_real_validators']=True
  save(out/'last_state.json',state());ok=True
 finally:
  if p.poll() is None:
   stop_result=subprocess.run([str(exe),'-stop'],cwd=exe.parent,env=env,capture_output=True,timeout=10)
   p.wait(timeout=15)
  thread.join(timeout=2)
  (out/'startup_redacted.txt').write_text(''.join(lines),encoding='utf-8')
  save(out/'api.json',api);save(out/'command.json',{'command':cmd,'cwd':str(exe.parent),'started_utc':started,'finished_utc':stamp(),'exit_code':p.returncode,'app_PATH':env['PATH'],'run_id':run,'binary_sha256':sha(exe),'source_manifest_sha256':json.loads((exe.parent/'build_info.json').read_text())['source_manifest_sha256'],'credentials_saved':False})
  if stop_result is not None:save(out/'stop.command.json',{'command':[str(exe),'-stop'],'exit_code':stop_result.returncode,'stdout':stop_result.stdout.decode('utf-8','replace'),'stderr':stop_result.stderr.decode('utf-8','replace')})
  if run:
   for marker in (pathlib.Path(os.environ['LOCALAPPDATA'])/'EdgeHybridDemo').glob('*/interactive/active.json'):
    if json.loads(marker.read_text())['run_id']==run:
     source=marker.parent/'logs'/(run+'.jsonl')
     if source.exists():(out/(run+'.jsonl')).write_bytes(source.read_bytes())
  save(out/'acceptance.json',{'passed':ok and p.returncode==0,'checks':checks,'run_id':run,'app_exit_code':p.returncode,'platform':sys.platform,'network_adapter_disconnected':None,'network_reason':'Application PATH stripped; only loopback requests. No system network adapter was changed. Actual physical disconnection remains NOT_EXECUTED.'})
 assert ok and p.returncode==0
 if a.process_checks:
  with socket.socket() as busy:
   busy.bind(('127.0.0.1',0));busy.listen(1);port=busy.getsockname()[1]
   args=[str(exe),'-no-browser','-devices','1','-http-port',str(port)];occupied=subprocess.run(args,cwd=exe.parent,env=env,capture_output=True,timeout=15)
   save(out/'occupied.command.json',{'command':args,'exit_code':occupied.returncode});(out/'occupied.stdout.txt').write_bytes(occupied.stdout);(out/'occupied.stderr.txt').write_bytes(occupied.stderr);assert occupied.returncode==2
  restart=subprocess.Popen([str(exe),'-devices','1','-no-browser'],cwd=exe.parent,env=env,stdout=subprocess.PIPE,stderr=subprocess.STDOUT)
  line=restart.stdout.readline();assert line.startswith(b'PAGE_URL=');stop=subprocess.run([str(exe),'-stop'],cwd=exe.parent,env=env,capture_output=True,timeout=10);restart.wait(timeout=15);assert stop.returncode==0 and restart.returncode==0
  remaining=restart.stdout.read().decode('utf-8','replace');remaining='\n'.join('[REDACTED]' if '口令（' in x else x for x in remaining.splitlines())
  save(out/'restart.command.json',{'command':[str(exe),'-devices','1','-no-browser'],'exit_code':restart.returncode,'stop_exit_code':stop.returncode,'stdout':line.decode()+remaining})
 print(json.dumps({'out':str(out),'passed':True,'devices':a.devices,'run_id':run},ensure_ascii=True))
if __name__=='__main__':main()
