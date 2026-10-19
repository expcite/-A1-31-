"""Targeted friend-review evidence runner; never changes historical measurements."""
import argparse, datetime, hashlib, json, os, pathlib, shutil, subprocess, sys, zipfile
import source_manifest
R = pathlib.Path(__file__).resolve().parents[1]
GO = r'C:\Users\[REDACTED_USER]\.local_workspace\binaries\go\go\bin\go.exe'
OUT = R / 'results/raw/backend_friend_20261002'

def write(p, v):
    p.parent.mkdir(parents=True, exist_ok=True)
    p.write_text(json.dumps(v, ensure_ascii=False, indent=2)+'\n', encoding='utf-8')

def snapshot():
    files = {pathlib.Path(p).relative_to(R).as_posix(): source_manifest.sha256_file(p) for p in source_manifest.collect(R)}
    manifest = ''.join(f'{h}  {p}\n' for p,h in files.items())
    return files, manifest, hashlib.sha256(manifest.encode()).hexdigest()

def main():
    ap=argparse.ArgumentParser(); ap.add_argument('--baseline',action='store_true'); ap.add_argument('--name'); ap.add_argument('--timeout',type=int,default=600); ap.add_argument('command',nargs=argparse.REMAINDER); a=ap.parse_args()
    OUT.mkdir(parents=True,exist_ok=True)
    files,manifest,source=snapshot()
    if a.baseline:
        p=OUT/'baseline.json'
        if p.exists(): raise RuntimeError('baseline exists')
        front={q.relative_to(R).as_posix():source_manifest.sha256_file(q) for d in ['internal/console/assets','internal/demo/assets'] for q in (R/d).rglob('*') if q.is_file()}
        history={q.relative_to(R).as_posix():source_manifest.sha256_file(q) for q in (R/'results/raw').rglob('*') if q.is_file() and OUT not in q.parents}
        packages={str(q):source_manifest.sha256_file(q) for q in R.parent.glob('EdgeHybrid*.zip')}
        write(p,{'source_manifest_sha256':source,'files':files,'frontend':front,'protected_history':history,'packages':packages,'root':str(R),'git_present':(R/'.git').exists()})
        (OUT/'baseline_source_manifest.sha256').write_text(manifest,encoding='utf-8')
        work=R.parent/'EdgeHybrid_FriendFix_Work';work.mkdir(exist_ok=True)
        with zipfile.ZipFile(work/'baseline_workspace_source.zip','w',zipfile.ZIP_DEFLATED) as z:
            for rel in files:z.write(R/rel,rel)
        shutil.copytree(R.parent/'EdgeHybrid_FriendReview_Followup',OUT/'review_input',dirs_exist_ok=True)
        with zipfile.ZipFile(r'C:\Users\[REDACTED_USER]\Downloads\EdgeHybrid_Friend_Review.zip') as z:
            names=[n for n in z.namelist() if '/Source/' not in n and not n.endswith('/')]
            write(OUT/'friend_zip_index.json',names)
            for n in names:
                if n.endswith(('.go','.json','.md','.log','.py')):
                    q=work/'original_friend'/n;q.parent.mkdir(parents=True,exist_ok=True);q.write_bytes(z.read(n))
        print(json.dumps({'source':source,'count':len(files),'frontend':len(front),'history':len(history),'friend_non_source':len(names)}));return 0
    if not a.name:ap.error('--name required')
    cmd=a.command[1:] if a.command[:1]==['--'] else a.command
    p=OUT/'commands'/a.name;p.mkdir(parents=True,exist_ok=False)
    (p/'source_manifest.sha256').write_text(manifest,encoding='utf-8')
    env=os.environ.copy();env['PYTHONUTF8']='1';env['PYTHONIOENCODING']='utf-8'
    subprocess.run([sys.executable,str(R/'scripts/run_checked.py'),'--log-dir',str(p),'--name','execution','--timeout',str(a.timeout),'--',*cmd],cwd=R,env=env,check=False)
    d=json.loads((p/'execution/command.json').read_text('utf-8'))
    write(p/'association.json',{'run_id':'friend-'+a.name,'source_manifest_sha256':source,'status':'PASS' if d['exit_code']==0 and not d['timeout_cleanup'] else 'FAIL',**d})
    return 0 if d['exit_code']==0 and not d['timeout_cleanup'] else 1

if __name__=='__main__':sys.exit(main())
