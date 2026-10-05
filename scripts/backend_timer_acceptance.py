"""Actual current EXE manual values, idle control and 1s/5s timer observations."""
import argparse,pathlib,json,datetime,time
from interactive_gateway_acceptance import Run,save
def main():
 ap=argparse.ArgumentParser();ap.add_argument('--exe',required=True);ap.add_argument('--out',required=True);a=ap.parse_args();out=pathlib.Path(a.out).resolve();out.mkdir(parents=True,exist_ok=False);r=None;ok=False;checks={}
 try:
  r=Run(pathlib.Path(a.exe).resolve(),out/'run',1);r.req('/run/start',{});s=r.wait(lambda s:s['devices'][0]['status']=='online'and s['devices'][0]['suite']==2)
  r.req('/devices/D001/sampling',{'auto':False,'temperature_c':25})
  def control(id,ms):
   r.req('/gateway/commands',{'request_id':id,'device_id':'D001','interval_ms':ms},202)
   s=r.wait(lambda s:any(o['operation_id']==id and o['status']=='confirmed'and o['peer_processed_at']is not None and o['applied']is True for o in s['operations']));save(out/(id+'.json'),s);return s
  for label,temp in [('263',26.3),('287',28.7)]:
   id='backend-manual-'+label;r.req('/devices/D001/telemetry',{'request_id':id,'temperature_c':temp},202);s=r.wait(lambda s:any(o['operation_id']==id and o['status']=='confirmed'for o in s['operations']));assert s['devices'][0]['last_received_temperature_c']==temp;save(out/(id+'.json'),s)
  checks['manual_263_287_real_gateway']=True
  count=s['devices'][0]['accepted_samples'];control('backend-idle-five',5000);time.sleep(1.2);s=r.req('/state');assert s['devices'][0]['accepted_samples']==count;checks['idle_active_control_without_new_telemetry']=True
  control('backend-one',1000);r.req('/devices/D001/sampling',{'auto':True,'temperature_c':25});start=r.req('/state')['cursor']
  def events(s,cut):return[e for e in s['events']if e['event_id']>cut and e['stage']=='telemetry_accepted'and e['device_id']=='D001']
  s=r.wait(lambda s:len(events(s,start))>=4);one=events(s,start);save(out/'one_second.json',s)
  control('backend-five',5000);cut=r.req('/state')['cursor'];end=time.monotonic()+24
  while time.monotonic()<end:
   s=r.req('/state')
   if len(events(s,cut))>=4:break
   time.sleep(.25)
  five=events(s,cut);assert len(five)>=4;save(out/'five_second.json',s)
  observations=[]
  for period,records in [(1,one),(5,five)]:
   gaps=[(datetime.datetime.fromisoformat(b['at'].replace('Z','+00:00'))-datetime.datetime.fromisoformat(a['at'].replace('Z','+00:00'))).total_seconds()for a,b in zip(records,records[1:])]
   assert all(period*.75<=g<=period*1.25 for g in gaps),(period,gaps)
   observations.append({'expected_seconds':period,'records':records,'gateway_observed_gaps_seconds':gaps,'clock':'gateway public UTC event observation; not algorithm benchmark'})
  save(out/'timer_observations.json',observations);checks['actual_one_to_five_second_intervals']=True;r.req('/run/stop',{});ok=True
 finally:
  if r is not None:r.close()
  save(out/'acceptance.json',{'passed':ok,'checks':checks,'run_id':r.run if r else None,'source_manifest_sha256':json.loads((pathlib.Path(a.exe).parent/'build_info.json').read_text())['source_manifest_sha256']})
 assert ok;print(json.dumps({'passed':True,'run_id':r.run,'checks':checks}))
if __name__=='__main__':main()
