"""Preserve the public starting tree without runtime secrets or historical ZIPs."""
import hashlib,json,pathlib,zipfile,datetime
root=pathlib.Path(__file__).resolve().parents[1]
out=root.parent/'EdgeHybrid_Interactive_Work'
out.mkdir(exist_ok=True)
excluded={'.git','runtime','keys','private','node_modules','__pycache__','.cache'}
files=[p for p in root.rglob('*') if p.is_file() and not excluded.intersection(p.relative_to(root).parts) and p.suffix.lower() not in {'.zip','.exe','.key','.pyc'}]
manifest={p.relative_to(root).as_posix():hashlib.sha256(p.read_bytes()).hexdigest() for p in sorted(files)}
target=out/'baseline_public.zip'
if target.exists(): raise SystemExit('baseline already exists; preserve it')
with zipfile.ZipFile(target,'w',zipfile.ZIP_DEFLATED) as z:
 for p in files:z.write(p,p.relative_to(root).as_posix())
(out/'baseline_hashes.json').write_text(json.dumps(manifest,indent=2),encoding='utf-8')
frozen={str(p.name):hashlib.sha256(p.read_bytes()).hexdigest() for p in root.parent.glob('EdgeHybrid_*Windows.zip')}
(out/'old_packages.json').write_text(json.dumps(frozen,indent=2),encoding='utf-8')
print(json.dumps({'cwd':str(root),'backup':str(target),'files':len(files),'old_packages':frozen},ensure_ascii=False))
