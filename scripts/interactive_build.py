"""Build the interactive software from a captured, public source manifest."""
import argparse,datetime,hashlib,json,os,pathlib,subprocess,sys
import source_manifest
root=pathlib.Path(__file__).resolve().parents[1]
GO=pathlib.Path(os.environ.get('GO',r'C:\Users\20477\.workbuddy\binaries\go\go\bin\go.exe'))
def run(cmd,name,logs,env):
 d=logs/name;d.mkdir(parents=True,exist_ok=False)
 start=datetime.datetime.now(datetime.timezone.utc).isoformat()
 with (d/'stdout.txt').open('wb') as out,(d/'stderr.txt').open('wb') as err:
  p=subprocess.run(cmd,cwd=root,env=env,stdout=out,stderr=err,timeout=120)
 info={'command':cmd,'cwd':str(root),'started_utc':start,'finished_utc':datetime.datetime.now(datetime.timezone.utc).isoformat(),'exit_code':p.returncode}
 (d/'command.json').write_text(json.dumps(info,indent=2),encoding='utf-8');(d/'exit.txt').write_text(str(p.returncode),encoding='ascii')
 if p.returncode:raise RuntimeError(f'Build failed: {name} exit={p.returncode}')
def main():
 ap=argparse.ArgumentParser();ap.add_argument('--out',required=True);ap.add_argument('--evidence',required=True);a=ap.parse_args();out=pathlib.Path(a.out).resolve();out.mkdir(parents=True,exist_ok=True);logs=pathlib.Path(a.evidence).resolve();logs.mkdir(parents=True,exist_ok=True)
 files=source_manifest.collect(str(root));manifest=''.join(f'{source_manifest.sha256_file(p)}  {pathlib.Path(p).relative_to(root).as_posix()}\n' for p in files).encode('utf-8');sha=hashlib.sha256(manifest).hexdigest();(logs/'measurement_source_manifest.sha256').write_bytes(manifest)
 env=os.environ.copy();env['GOTOOLCHAIN']='local';env['PATH']=str(GO.parent)+os.pathsep+env.get('PATH','');flags=f'-X example.com/edgehybrid/internal/console.SourceHash={sha} -X example.com/edgehybrid/internal/console.Version=interactive-v1.2'
 binaries={}
 for name,package in [('bench','./cmd/bench'),('pbench','./cmd/pbench'),('scenario','./internal/handshake'),('EdgeHybridInteractive','./cmd/console')]:
  if name=='EdgeHybridInteractive':
   for binary_name,field in [('bench.exe','BenchSHA'),('pbench.exe','PBenchSHA'),('scenario.exe','ScenarioSHA')]:flags+=f' -X example.com/edgehybrid/internal/console.{field}={binaries[binary_name]["sha256"]}'
  target=out/(name+'.exe');cmd=[str(GO),'test','-c','-o',str(target),package] if name=='scenario' else [str(GO),'build','-trimpath','-ldflags',flags,'-o',str(target),package]
  run(cmd,'build_'+name,logs,env);binaries[target.name]={'sha256':hashlib.sha256(target.read_bytes()).hexdigest(),'bytes':target.stat().st_size,'source_manifest_sha256':sha,'test_only':name=='scenario'}
 info={'version':'interactive-v1.2','source_manifest_sha256':sha,'source_files':len(files),'binaries':binaries,'created_utc':datetime.datetime.now(datetime.timezone.utc).isoformat(),'go':subprocess.check_output([str(GO),'version'],env=env).decode().strip(),'scope':'New interactive application; old Demo and M5/M6 evidence unchanged'}
 (out/'build_info.json').write_text(json.dumps(info,indent=2),encoding='utf-8');(logs/'build_info.json').write_text(json.dumps(info,indent=2),encoding='utf-8');print(json.dumps({'out':str(out),'source_sha256':sha,'files':len(files)},ensure_ascii=True))
if __name__=='__main__':main()
