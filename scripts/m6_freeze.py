"""Freeze the declared comparison and pre-existing public evidence, once."""
import datetime, hashlib, json, re, shutil
from pathlib import Path
ROOT = Path(__file__).resolve().parents[1]
OUT = ROOT/'results/raw/m6_20261001'
def digest(p): return hashlib.sha256(p.read_bytes()).hexdigest()
def write(p,x): p.write_text(json.dumps(x,ensure_ascii=False,indent=2)+'\n',encoding='utf-8')
def main():
    f=OUT/'freeze'; f.mkdir(parents=True,exist_ok=False)
    files=['tamper','downgrade','kem_implicit','wire_replay','channel']
    baseline=[]
    for n in files:
        p=ROOT/f'internal/handshake/{n}_test.go'
        baseline.append(dict(path=p.relative_to(ROOT).as_posix(),sha256=digest(p),tests=re.findall(r'^func (Test\w+)\(',p.read_text(encoding='utf-8'),re.M)))
    write(f/'baseline.json',baseline)
    preserved={p.relative_to(ROOT).as_posix():digest(p) for p in (ROOT/'results/raw/m5_formal_20261001').rglob('*') if p.is_file()}
    for rel in ['docs/FORMAL_EXPERIMENT_PLAN.md','results/milestone_M5.md','results/m5_formal_readiness.json']:
        preserved[rel]=digest(ROOT/rel)
    write(f/'m5_preserved.json',preserved)
    write(f/'production_go.json',{p.relative_to(ROOT).as_posix():digest(p) for p in ROOT.rglob('*.go') if not p.name.endswith('_test.go') and 'runtime' not in p.parts})
    demo=ROOT.parent/'EdgeHybrid_Demo_Windows.zip'
    write(f/'freeze.json',dict(plan_id='m6-structured-tamper-v1',plan_sha256=digest(ROOT/'docs/INNOVATION_PLAN.md'),run_id='m6-'+datetime.datetime.now(datetime.timezone.utc).strftime('%Y%m%dT%H%M%SZ'),demo_path=str(demo),demo_sha256=digest(demo),created_utc=datetime.datetime.now(datetime.timezone.utc).isoformat()))
    audit=f/'audit'; audit.mkdir()
    for n in ['REVIEW.md','AGENT_NEXT_M6.md','M6_ACCEPTANCE.md']: shutil.copy2(ROOT.parent/'EdgeHybrid_M5_Formal_Audit'/n,audit/n)
    print('Plan, baseline, production Go and M5/Demo hashes frozen.')
if __name__=='__main__': main()
