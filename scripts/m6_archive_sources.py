"""Resolve every measured M5/M6 source file to an immutable, hash checked copy."""
import hashlib,json
from pathlib import Path
ROOT=Path(__file__).resolve().parents[1];OUT=ROOT/'results/raw/m6_20261001'
def sha(p):return hashlib.sha256(p.read_bytes()).hexdigest()
def main():
 archive=OUT/'source_archive';archive.mkdir(exist_ok=True)
 manifests=dict(M5_measurement=ROOT/'results/raw/m5_formal_20261001/freeze/source_manifest.sha256',M5_delivery=ROOT/'results/raw/m5_formal_20261001/delivery_source_manifest.sha256',M6_measurement=OUT/'comparison_v2/measurement_source_manifest.sha256')
 resolution={}
 for label,manifest in manifests.items():
  entries=[]
  for line in manifest.read_text(encoding='utf-8').splitlines():
   expected,rel=line.split('  ',1);dest=archive/(expected+'.txt')
   if not dest.is_file():
    candidates=[ROOT/rel,ROOT/'results/raw/m5_formal_20261001/freeze/m5_formal_report_measured_version.py']
    src=next((p for p in candidates if p.is_file() and sha(p)==expected),None)
    if src is None:raise RuntimeError('unresolved measured source: '+label+' '+rel)
    dest.write_bytes(src.read_bytes())
   assert sha(dest)==expected
   entries.append(dict(path=rel,sha256=expected,archived_path=dest.relative_to(ROOT).as_posix()))
  resolution[label]=dict(manifest_path=manifest.relative_to(ROOT).as_posix(),manifest_sha256=sha(manifest),files=len(entries),entries=entries)
 (OUT/'source_resolution.json').write_text(json.dumps(resolution,ensure_ascii=False,indent=2)+'\n',encoding='utf-8')
 print({k:v['files'] for k,v in resolution.items()})
if __name__=='__main__':main()
