"""Host an actual EXE for browser review. Temporary credentials stay outside evidence."""
import argparse,pathlib,subprocess,json,datetime,os,hashlib
def main():
 ap=argparse.ArgumentParser();ap.add_argument('--exe',required=True);ap.add_argument('--out',required=True);ap.add_argument('--auth-file',required=True);ap.add_argument('--devices',type=int,default=1);a=ap.parse_args()
 exe=pathlib.Path(a.exe).resolve();out=pathlib.Path(a.out).resolve();out.mkdir(parents=True,exist_ok=False);auth=pathlib.Path(a.auth_file).resolve();assert auth.suffix=='.json' and '.private.' in auth.name
 cmd=[str(exe),'-devices',str(a.devices),'-no-browser'];start=datetime.datetime.now(datetime.timezone.utc).isoformat();p=subprocess.Popen(cmd,cwd=exe.parent,stdout=subprocess.PIPE,stderr=subprocess.STDOUT,text=True,encoding='utf-8',errors='replace');public=[];info={}
 for line in p.stdout:
  if line.startswith('PAGE_URL='):info['url']=line.strip().split('=',1)[1]
  if line.startswith('RUN_ID='):info['run_id']=line.strip().split('=',1)[1]
  if '口令（' in line:
   info['code']=line.strip().rsplit(': ',1)[1];auth.write_text(json.dumps(info),encoding='utf-8')
  public.append('[REDACTED LOCAL CODE]\n' if '口令（' in line else line)
 p.wait();(out/'startup_redacted.txt').write_text(''.join(public),encoding='utf-8')
 (out/'command.json').write_text(json.dumps({'command':cmd,'started_utc':start,'finished_utc':datetime.datetime.now(datetime.timezone.utc).isoformat(),'run_id':info.get('run_id'),'exit_code':p.returncode,'binary_sha256':hashlib.sha256(exe.read_bytes()).hexdigest(),'source_manifest_sha256':json.loads((exe.parent/'build_info.json').read_text())['source_manifest_sha256']},indent=2),encoding='utf-8')
 if info.get('run_id'):
  for active in (pathlib.Path(os.environ['LOCALAPPDATA'])/'EdgeHybridDemo').glob('*/interactive/active.json'):
   if json.loads(active.read_text())['run_id']==info['run_id']:
    log=active.parent/'logs'/(info['run_id']+'.jsonl');(out/log.name).write_bytes(log.read_bytes())
 if auth.exists():auth.unlink()
 print(json.dumps({'exit_code':p.returncode,'run_id':info.get('run_id')}));return p.returncode
if __name__=='__main__':raise SystemExit(main())
