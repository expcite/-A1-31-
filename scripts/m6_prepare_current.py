"""Freeze a NEW M6 A/B study against current inputs. No Go test is executed.
Historical M5/M6 data are guarded externally; never rewrite the old M6 freeze.
"""
import argparse
import ast
import datetime
import hashlib
import json
from pathlib import Path
import re
import sys
import source_manifest

A_FILES=('tamper_test.go','downgrade_test.go','kem_implicit_test.go','wire_replay_test.go','channel_test.go')

def sha(path):return hashlib.sha256(path.read_bytes()).hexdigest()
def write(path,value):
    with path.open('x',encoding='utf-8',newline='\n')as f:
        json.dump(value,f,ensure_ascii=False,indent=2,allow_nan=False);f.write('\n')

def prepare(root,out,run_id):
    root=root.resolve();out=out.resolve()
    if out.is_relative_to(root)or root.is_relative_to(out):
        raise ValueError('new M6 freeze must be external to immutable source inputs')
    if not re.fullmatch(r'[A-Za-z0-9_.-]{1,120}',run_id):raise ValueError('invalid study run_id')
    baseline=[]
    for name in A_FILES:
        path=root/'internal/handshake'/name;text=path.read_text(encoding='utf-8')
        tests=re.findall(r'^func (Test\w+)\s*\(\s*\w+\s+\*testing\.T\s*\)',text,re.M)
        if not tests:raise ValueError('no baseline tests enumerated from '+name)
        baseline.append({'path':path.relative_to(root).as_posix(),'sha256':sha(path),'tests':tests})
    scope={}
    for node in ast.parse((root/'scripts/m6_compare.py').read_text(encoding='utf-8')).body:
        if isinstance(node,ast.Assign):
            for t in node.targets:
                if isinstance(t,ast.Name)and t.id in ('RULES','DEFECTS'):scope[t.id]=ast.literal_eval(node.value)
    if set(scope)!= {'RULES','DEFECTS'} or not scope['DEFECTS']:raise ValueError('missing fixed rule/defect registry')
    inputs=source_manifest.collect(str(root))
    manifest=''.join(sha(Path(p))+'  '+Path(p).relative_to(root).as_posix()+'\n'for p in inputs).encode('utf-8')
    current=root/'CURRENT_SOURCE_MANIFEST.sha256'
    if not current.is_file()or current.read_bytes()!=manifest:raise ValueError('regenerate and verify current source manifest BEFORE freezing')
    out.mkdir(parents=True,exist_ok=False)
    (out/'measurement_source_manifest.sha256').write_bytes(manifest)
    write(out/'baseline.json',baseline)
    production={Path(p).relative_to(root).as_posix():sha(Path(p))for p in inputs if str(p).endswith('.go')and not str(p).endswith('_test.go')}
    write(out/'production_go.json',production)
    registry=json.dumps(scope['DEFECTS'],sort_keys=True,separators=(',',':'),ensure_ascii=False).encode('utf-8')
    record={'schema':'edgehybrid-m6-current-freeze/1','run_id':run_id,'created_utc':datetime.datetime.now(datetime.timezone.utc).isoformat(),
            'plan_sha256':sha(root/'docs/INNOVATION_PLAN.md'),'measurement_source_manifest_sha256':hashlib.sha256(manifest).hexdigest(),
            'baseline_manifest_sha256':sha(out/'baseline.json'),'production_go_manifest_sha256':sha(out/'production_go.json'),
            'defect_registry_sha256':hashlib.sha256(registry).hexdigest(),'expected_normal_b_records':39,'expected_reproduction_b_records':3,
            'historical_preservation':'separate preflight history receipts required; this freeze does not verify old M5 data',
            'normal_repetitions':3,'mutation_policy':'record detected/missed/unreachable; never require all detected as evidence validity',
            'go_tests_executed_by_prepare':False}
    write(out/'freeze.json',record)
    return record

def main():
    ap=argparse.ArgumentParser(description=__doc__);ap.add_argument('--src',type=Path,required=True);ap.add_argument('--out',type=Path,required=True);ap.add_argument('--run-id',required=True);a=ap.parse_args()
    result=prepare(a.src,a.out,a.run_id);print(json.dumps(result,ensure_ascii=True));return 0
if __name__=='__main__':
    try:raise SystemExit(main())
    except (OSError,ValueError,SyntaxError)as e:print('M6_FREEZE_BLOCKED: '+str(e),file=sys.stderr);raise SystemExit(2)
