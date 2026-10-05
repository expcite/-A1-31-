"""New campaign, existing strict formal engine; immutable old M5/M6 data."""
import argparse,datetime,hashlib,json,os,pathlib,subprocess,sys,tempfile
import m5_formal_run as shared
R=pathlib.Path(__file__).resolve().parents[1]
def sha(p):return hashlib.sha256(p.read_bytes()).hexdigest()
def write(p,v):p.parent.mkdir(parents=True,exist_ok=True);p.write_bytes((json.dumps(v,ensure_ascii=False,indent=2)+'\n').encode())
def main():
 ap=argparse.ArgumentParser();ap.add_argument('--freeze',type=pathlib.Path,required=True);ap.add_argument('--out',type=pathlib.Path,required=True);a=ap.parse_args();f=json.loads(a.freeze.read_text('utf-8'));assert f['status']=='FROZEN'and f['regression_passed'];out=a.out.resolve();out.mkdir(parents=True,exist_ok=False)
 parent='backend-formal-'+datetime.datetime.now(datetime.timezone.utc).strftime('%Y%m%dT%H%M%S.%fZ');matrix=[('handshake',m,1)for m in ['T','H','P']]+[('resource',m,b)for m in['T','H']for b in range(1,6)]+[('throughput',m,1)for m in['T','H','P']];rows=[];bins={n:pathlib.Path(v['path'])for n,v in f['binaries'].items()}
 def frozen():
  assert shared.current_source(R)==f['source_manifest_sha256'],'source changed since final regression'
  assert sha(R/'docs/BACKEND_FORMAL_EXPERIMENT_PLAN.md')==f['campaign_plan_sha256']
  assert sha(R/'docs/FORMAL_EXPERIMENT_PLAN.md')==f['plan_sha256']
  for n,p in bins.items():assert sha(p)==f['binaries'][n]['sha256'],n
  assert sha(pathlib.Path(f.get('demo_zip_path', R.parent/'EdgeHybrid_Demo_Windows.zip')))==f['demo_zip_sha256']
 def run(n,cmd,t):
  subprocess.run([sys.executable,str(R/'scripts/run_checked.py'),'--log-dir',str(out/'commands'),'--name',n,'--timeout',str(t),'--',*map(str,cmd)],cwd=R,check=False)
  return json.loads((out/'commands'/n/'command.json').read_text('utf-8'))
 try:
  frozen();write(out/'matrix.json',{'parent_run_id':parent,'campaign_plan_id':f['campaign_plan_id'],'formal_contract_id':f['plan_id'],'source_manifest_sha256':f['source_manifest_sha256'],'matrix':matrix,'warmup':0,'retries':0})
  with tempfile.TemporaryDirectory(prefix='edgehybrid-backend-p-private-')as private:
   for kind,mode,batch in matrix:
    frozen();name=f'{kind}_{mode}_{batch}';env=shared.environment();extra=[p for p in env['background']if p['Name']in['EdgeHybridInteractive','console','live.test','cryptosuite.test','handshake.test']];env['interactive_test_conflicts']=extra;env['captured_at_utc']=datetime.datetime.now(datetime.timezone.utc).isoformat();write(out/(name+'_environment.json'),env)
    if env['conflicts']or extra:raise RuntimeError('Active app/experiment/test conflicts, sampling not started')
    rid=parent+'-'+name;dest=out/name;common=['-experiment',kind,'-run-kind','formal','-timeout','30s','-run-id',rid,'-parent-run-id',parent,'-source-manifest-sha256',f['source_manifest_sha256'],'-out',dest];binary=bins['pbench'if mode=='P'else'bench'];cmd=[binary,*common]
    if mode=='P':cmd+=['-certs-dir',pathlib.Path(private)/'certs']
    else:
     dest.mkdir();cmd+=['-mode','traditional'if mode=='T'else'hybrid']
     if kind!='handshake':cmd+=['-gateway-bin',bins['gateway']]
    cmd+=['-trials','1000'if kind=='handshake'else'100']if kind!='throughput'else['-rounds','5','-per-round','200']
    print('START '+name+'; fixed attempts, no retries',flush=True);meta=run(name,cmd,3000);dirs=[dest]if mode=='P'else list(dest.iterdir());actual=dirs[0]if len(dirs)==1 and dirs[0].is_dir()else None
    row={'name':name,'mode':mode,'experiment':kind,'batch':batch,'run_id':rid,'parent_run_id':parent,'campaign_plan_id':f['campaign_plan_id'],'result_dir':str(actual)if actual else None,'source_manifest_sha256':f['source_manifest_sha256'],'binary_sha256':sha(binary),'gateway_binary_sha256':sha(bins['gateway'])if mode!='P'and kind!='handshake'else None,'producer_command':meta};rows.append(row);write(out/'results_index.json',rows)
    if actual:
     row['verification_command']=run('verify_'+name,[binary,'-verify','-dir',actual,'-exit-code',str(meta['exit_code'])],120);m=json.loads((actual/'manifest.json').read_text('utf-8'));row['config_hash']=m.get('config_sha256',m.get('config_hash'));assert m['run_id']==rid and m['source_manifest_sha256']==f['source_manifest_sha256']
    write(out/'results_index.json',rows);print('END '+name+' producer='+str(meta['exit_code'])+' validator='+str(row.get('verification_command',{}).get('exit_code')),flush=True)
    if meta['timeout_cleanup']or meta['exit_code']not in[0,3,4]or not actual or row['verification_command']['exit_code']!=0:raise RuntimeError('Execution/evidence error retained; remaining matrix not started, no retry')
    if meta['exit_code']==4:raise RuntimeError('Interrupted batch retained, remaining not started')
  frozen();cleanup=shared.environment();write(out/'cleanup_environment.json',cleanup)
  if cleanup['conflicts']:raise RuntimeError('Remaining experiment process after matrix')
  write(out/'execution_status.json',{'status':'COMPLETED'if all(x['producer_command']['exit_code']==0 for x in rows)else'COMPLETED_WITH_FAILURES','runs':len(rows),'planned_runs':len(matrix),'retries':0,'parent_run_id':parent,'campaign_plan_id':f['campaign_plan_id'],'private_P_directory_removed':True});return 0
 except BaseException as e:
  write(out/'execution_status.json',{'status':'INCOMPLETE','reason':str(e),'runs':len(rows),'planned_runs':len(matrix),'retries':0,'parent_run_id':parent});print(str(e),file=sys.stderr);return 1
if __name__=='__main__':sys.exit(main())
