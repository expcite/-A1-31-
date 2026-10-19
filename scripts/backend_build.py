"""Build current embedded GUI assets with source provenance; no sampling."""
import pathlib,sys,os,json,hashlib,subprocess,argparse,datetime,shutil
import source_manifest
R=pathlib.Path(__file__).resolve().parents[1]
GO=pathlib.Path(os.environ.get('GO') or shutil.which('go') or 'go')
def sha(p):return hashlib.sha256(p.read_bytes()).hexdigest()
def main():
 ap=argparse.ArgumentParser();ap.add_argument('--out',type=pathlib.Path,required=True);ap.add_argument('--evidence',type=pathlib.Path,required=True);ap.add_argument('--version',default=(R/'VERSION').read_text().strip());a=ap.parse_args();out=a.out.resolve();logs=a.evidence.resolve();out.mkdir(parents=True,exist_ok=False);logs.mkdir(parents=True,exist_ok=False)
 lines=''.join(source_manifest.sha256_file(p)+'  '+pathlib.Path(p).relative_to(R).as_posix()+'\n'for p in source_manifest.collect(str(R))).encode();h=hashlib.sha256(lines).hexdigest();(logs/'source_manifest.sha256').write_bytes(lines);(out/'CURRENT_SOURCE_MANIFEST.sha256').write_bytes(lines)
 env=os.environ.copy();env['GOTOOLCHAIN']='local';env['PATH']=str(GO.parent)+os.pathsep+env.get('PATH','');version=a.version;flags=f'-X example.com/edgehybrid/internal/console.SourceHash={h} -X example.com/edgehybrid/internal/console.Version={version}';binaries={}
 for n,pkg in [('bench','./cmd/bench'),('pbench','./cmd/pbench'),('gateway','./cmd/gateway'),('device','./cmd/device'),('fleet','./cmd/fleet'),('secctl','./cmd/secctl'),('scenario','./internal/handshake'),('EdgeHybridInteractive','./cmd/console')]:
  if n=='EdgeHybridInteractive':
   for b,f in [('bench.exe','BenchSHA'),('pbench.exe','PBenchSHA'),('scenario.exe','ScenarioSHA')]:flags+=f' -X example.com/edgehybrid/internal/console.{f}={binaries[b]["sha256"]}'
  dest=out/(n+'.exe');cmd=[str(GO),'test','-c','-trimpath','-o',str(dest),pkg]if n=='scenario'else[str(GO),'build','-trimpath','-ldflags',flags+(f' -X main.StatusVersion={version} -X main.StatusSourceHash={h}' if n=='secctl' else ''),'-o',str(dest),pkg];log=logs/n;log.mkdir(parents=True,exist_ok=True);start=datetime.datetime.now(datetime.timezone.utc).isoformat()
  with(log/'stdout.txt').open('wb')as so,(log/'stderr.txt').open('wb')as se:proc=subprocess.run(cmd,cwd=R,env=env,stdout=so,stderr=se,timeout=180)
  (log/'command.json').write_text(json.dumps({'command':cmd,'cwd':str(R),'exit_code':proc.returncode,'started_utc':start,'finished_utc':datetime.datetime.now(datetime.timezone.utc).isoformat()},indent=2),encoding='utf-8');assert proc.returncode==0,n
  binaries[dest.name]={'sha256':sha(dest),'bytes':dest.stat().st_size,'source_manifest_sha256':h,'test_only':n=='scenario'}
 after=''.join(source_manifest.sha256_file(p)+'  '+pathlib.Path(p).relative_to(R).as_posix()+'\n'for p in source_manifest.collect(str(R))).encode()
 if after!=lines:raise RuntimeError('Source changed during build; no complete build_info published')
 info={'source_unchanged_during_build':True,'version':version,'source_manifest_sha256':h,'source_files':len(lines.splitlines()),'binaries':binaries,'go':subprocess.check_output([str(GO),'version'],env=env).decode().strip(),'frontend':'MINIMAL-UI-v1.0; embedded native ES modules; shared authenticated API state; bounded G3 rekey receipts; no runtime CDN','created_utc':datetime.datetime.now(datetime.timezone.utc).isoformat()}
 for p in [out/'build_info.json',logs/'build_info.json']:p.write_text(json.dumps(info,indent=2),encoding='utf-8')
 print(json.dumps({'source_hash':h,'version':version,'binaries':len(binaries)},ensure_ascii=True))
if __name__=='__main__':main()
