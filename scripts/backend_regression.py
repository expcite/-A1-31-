"""Final serial regression; real exits, immutable logs, no benchmark sampling."""
import argparse,json,os,pathlib,subprocess,sys,hashlib,datetime
import m5_formal_run
R=pathlib.Path(__file__).resolve().parents[1]
GO=pathlib.Path(os.environ.get('GO',r'C:\Users\20477\.workbuddy\binaries\go\go\bin\go.exe'))
def main():
 ap=argparse.ArgumentParser();ap.add_argument('--out',required=True,type=pathlib.Path);ap.add_argument('--cc',required=True,type=pathlib.Path);ap.add_argument('--node',required=True,type=pathlib.Path);ap.add_argument('--history-root',type=pathlib.Path);ap.add_argument('--package-parallelism',type=int,default=1);a=ap.parse_args()
 if not 1<=a.package_parallelism<=16:ap.error('package-parallelism must be 1..16')
 out=a.out.resolve();out.mkdir(parents=True,exist_ok=False);rows=[];source=m5_formal_run.current_source(R)
 env=os.environ.copy()
 cc=a.cc.absolute() # Preserve the approved ASCII junction; resolve() follows it into a Chinese path.
 env['CGO_ENABLED']='1';env['CC']=str(cc);env['GOTOOLCHAIN']='local';env['PATH']=str(GO.parent)+os.pathsep+str(cc.parent)+os.pathsep+env.get('PATH','')
 # Tool choices must be supplied by the checked process environment.
 # Never replace an explicitly configured BC/Java/OpenSSL with another copy.
 # No hard-coded GCC_EXEC_PREFIX/COMPILER_PATH/LIBRARY_PATH overrides.
 for required in ['EDGEHYBRID_BC_JAR','EDGEHYBRID_BC_SHA256','EDGEHYBRID_JAVA','EDGEHYBRID_JAVAC','EDGEHYBRID_OPENSSL']:
  if not env.get(required):
   raise RuntimeError('configure independent test prerequisite before regression: '+required)
 def write(p,v):p.write_text(json.dumps(v,ensure_ascii=False,indent=2)+'\n',encoding='utf-8')
 def run(name,cmd,timeout=600):
  print('START '+name,flush=True);subprocess.run([sys.executable,str(R/'scripts/run_checked.py'),'--log-dir',str(out/'commands'),'--name',name,'--timeout',str(timeout),'--',*map(str,cmd)],cwd=R,env=env,check=False)
  p=out/'commands'/name;d=json.loads((p/'command.json').read_text('utf-8'));rows.append({'name':name,'status':'PASS'if d['exit_code']==0 and not d['timeout_cleanup'] else'FAIL','source_manifest_sha256':source,**d});write(out/'command_index.json',rows);return p,d
 run('mod_verify',[GO,'mod','verify']);run('vet',[GO,'vet','./...']);run('build_all',[GO,'build','./...']);pkg,_=run('packages',[GO,'list','./...']);packages=(pkg/'stdout.txt').read_text('utf-8').splitlines()
 for name,flags in [('go_test',[]),('go_race',['-race'])]:
  log,d=run(name,[GO,'test',*flags,'-p='+str(a.package_parallelism),'-count=1','-json','./...'],1800)
  args=[sys.executable,R/'scripts/summarize-go-test-json.py',log/'stdout.txt','--go-exit-code',str(d['exit_code']),'--require-tests','--out',out/(name+'_summary.txt'),'--json',out/(name+'_summary.json')]
  for p in packages:args+=['--expect-package',p]
  run(name+'_log_acceptance',args)
 for name in ['summarize_selftest','fleet_verifier_selftest','bench_result_selftest','m4case_extractor_selftest']:
  run(name,[sys.executable,R/('scripts/'+name+'.py')])
 run('m6_historical_controls',[sys.executable,R/'scripts/backend_m6_history_controls.py','--root',(a.history_root.resolve() if a.history_root else R.parent/'EdgeHybrid_Interactive_V12_Work/Historical_M6_Resolved'),'--out',out/'m6_historical_controls'])
 run('frontend_existing_tests',[a.node.resolve(),R/'scripts/interactive_frontend_test.mjs'])
 for name,args in [('go_version',['version']),('go_env',['env','GOOS','GOARCH','GOVERSION','CGO_ENABLED','CC','GOTOOLCHAIN','GOSUMDB']),('modules',['list','-m','all'])]:run(name,[GO,*args])
 run('source_manifest',[sys.executable,R/'scripts/source_manifest.py','--out',out/'source_manifest.sha256','--json',out/'source_info.json'])
 stable=hashlib.sha256((out/'source_manifest.sha256').read_bytes()).hexdigest()==source
 result={'status':'PASS'if stable and all(r['status']=='PASS'for r in rows)else'FAIL','source_manifest_sha256':source,'source_unchanged':stable,'commands':len(rows),'cc':str(cc),'test_process_environment':{k:env.get(k)for k in ['CC','CGO_ENABLED','GOTOOLCHAIN','GCC_EXEC_PREFIX','COMPILER_PATH','LIBRARY_PATH']},'independent_tools':'existing local BC/Java/OpenSSL; test processes only','race_scope':'Windows native; instrumentation reports only paths actually exercised','finished_utc':datetime.datetime.now(datetime.timezone.utc).isoformat()};write(out/'REGRESSION_STATUS.json',result);return 0 if result['status']=='PASS'else 1
if __name__=='__main__':sys.exit(main())
