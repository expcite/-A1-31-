"""Verify an extracted M6 delivery and M5 raw with freshly rebuilt validators."""
import argparse,hashlib,json,subprocess,sys
from pathlib import Path
GO=r'C:\Users\[REDACTED_USER]\.local_workspace\binaries\go\go\bin\go.exe'
def read(p):return json.loads(p.read_text(encoding='utf-8'))
def sha(p):return hashlib.sha256(p.read_bytes()).hexdigest()
def main():
 ap=argparse.ArgumentParser();ap.add_argument('--root',type=Path,required=True);ap.add_argument('--out',type=Path,required=True);a=ap.parse_args();root=a.root.resolve();out=a.out.resolve();out.mkdir(parents=True,exist_ok=False)
 sys.path.insert(0,str(root/'scripts'));from m6_verify import verify
 result=verify(root,root/'results/raw/m6_20261001/comparison_v2')
 delivery=root/'results/raw/m6_20261001/delivery_source_manifest.sha256';matches=0
 for line in delivery.read_text(encoding='utf-8').splitlines():
  h,rel=line.split('  ',1);assert sha(root/rel)==h;matches+=1
 result['delivery_source_files_matched']=matches
 def run(name,cmd):
  d=out/name;d.mkdir();
  with (d/'stdout.txt').open('wb') as so,(d/'stderr.txt').open('wb') as se:r=subprocess.run(cmd,cwd=root,stdout=so,stderr=se,timeout=180)
  (d/'command.json').write_text(json.dumps(dict(command=cmd,cwd=str(root),exit_code=r.returncode),indent=2),encoding='utf-8');(d/'exit.txt').write_text(str(r.returncode)+'\n');assert r.returncode==0,(name,r.returncode)
 for cmd in ['bench','pbench']:run('build_'+cmd,[GO,'build','-trimpath','-o',str(out/(cmd+'.exe')),'./cmd/'+cmd])
 index=read(root/'results/raw/m5_formal_20261001/data/results_index.json')
 for entry in index:
  rel=entry['result_dir'].replace('\\','/').split('/results/raw/m5_formal_20261001/',1)[1]
  directory=root/'results/raw/m5_formal_20261001'/rel
  run('verify_'+entry['name'],[str(out/('pbench.exe' if entry['mode']=='P' else 'bench.exe')),'-verify','-dir',str(directory),'-exit-code',str(entry['producer_command']['exit_code'])])
 # Actual normal B execution from the extracted source, no formal performance sampling.
 run('structured_from_extracted',[GO,'test','-json','-count=1','-run','^TestM6Structured$','./internal/handshake'])
 result['rebuilt_validators']=dict(bench_sha256=sha(out/'bench.exe'),pbench_sha256=sha(out/'pbench.exe'),scope='verification binaries, not the original sampling binaries')
 result['m5_batches_revalidated']=len(index);result['structured_extracted_exit_code']=0
 (out/'verification.json').write_text(json.dumps(result,ensure_ascii=False,indent=2)+'\n',encoding='utf-8');print('Extracted source, M6 rules and all 16 M5 batches verified.')
if __name__=='__main__':main()
