#!/usr/bin/env python3
"""Mutate public copies of completed formal results, never originals."""
import argparse
import json
from pathlib import Path
import shutil
import subprocess

def main():
    ap=argparse.ArgumentParser()
    ap.add_argument('--freeze',type=Path,required=True)
    ap.add_argument('--data',type=Path,required=True)
    ap.add_argument('--out',type=Path,required=True)
    a=ap.parse_args()
    a.out.mkdir(parents=True,exist_ok=False)
    freeze=json.loads(a.freeze.read_text(encoding='utf-8'))
    index=json.loads((a.data/'results_index.json').read_text(encoding='utf-8'))
    rows=[]
    def check(name,binary,directory,code,expected):
        cmd=[str(binary),'-verify','-dir',str(directory),'-exit-code',str(code)]
        logs=a.out/name
        logs.mkdir()
        with (logs/'stdout.txt').open('wb') as stdout,(logs/'stderr.txt').open('wb') as stderr:
            p=subprocess.run(cmd,stdout=stdout,stderr=stderr,timeout=120)
        row=dict(name=name,command=cmd,exit_code=p.returncode,expected_exit=expected,passed=p.returncode==expected)
        (logs/'command.json').write_text(json.dumps(row,indent=2),encoding='utf-8')
        rows.append(row)
    # One positive and contract-mutation controls for every result type/mode.
    chosen=[x for x in index if x['batch']==1]
    for entry in chosen:
        source=Path(entry['result_dir'])
        binary=Path(freeze['binaries']['pbench' if entry['mode']=='P' else 'bench']['path'])
        code=entry['producer_command']['exit_code']
        check('positive_'+entry['name'],binary,source,code,0)
        public=entry['mode']=='P'
        kind=entry['experiment']
        raw='resource_trials.jsonl' if kind=='resource' else ('p_trials.jsonl' if public else 'trials.jsonl') if kind=='handshake' else ('p_throughput_rounds.jsonl' if public else 'throughput_rounds.jsonl')
        summary='resource_summary.json' if kind=='resource' else ('p_summary.json' if public else 'summary.json') if kind=='handshake' else ('p_throughput_summary.json' if public else 'throughput_summary.json')
        mutations=['missing_status','missing_raw','bad_json','wrong_run','bad_source','missing_plan','wrong_requested','forged_summary','duplicate_row']
        if kind!='resource':
            mutations+=['missing_confirmation']
        if public:
            mutations+=['wrong_tls_group']
        for mutation in mutations:
            target=a.out/'copies'/(entry['name']+'_'+mutation)
            shutil.copytree(source,target)
            if mutation=='missing_status':(target/'status.json').unlink()
            elif mutation=='missing_raw':(target/raw).unlink()
            elif mutation=='bad_json':(target/raw).write_text('{bad}\n',encoding='utf-8')
            elif mutation in ['wrong_run','bad_source','missing_plan','wrong_requested']:
                m=json.loads((target/'manifest.json').read_text(encoding='utf-8'))
                if mutation=='wrong_run':m['run_id']='orphan'
                elif mutation=='bad_source':m['source_manifest_sha256']=''
                elif mutation=='missing_plan':m.pop('formal_plan_id',None)
                elif kind=='throughput':m['rounds']-=1
                else:m['trials' if 'trials' in m else 'requested_trials']-=1
                (target/'manifest.json').write_text(json.dumps(m),encoding='utf-8')
            elif mutation=='forged_summary':
                s=json.loads((target/summary).read_text(encoding='utf-8'))
                field='roles' if kind=='resource' else 'successes' if kind=='handshake' else 'total_completed'
                s[field]+=1
                (target/summary).write_text(json.dumps(s),encoding='utf-8')
            elif mutation=='duplicate_row':
                lines=(target/raw).read_text(encoding='utf-8').splitlines();lines[1]=lines[0]
                (target/raw).write_text('\n'.join(lines)+'\n',encoding='utf-8')
            elif mutation=='missing_confirmation':
                name='p_gateway_events.jsonl' if public else 'gateway_events.jsonl'
                lines=(target/name).read_text(encoding='utf-8').splitlines()
                for i,line in enumerate(lines):
                    if json.loads(line).get('outcome')=='control-confirmed':del lines[i];break
                (target/name).write_text('\n'.join(lines)+'\n',encoding='utf-8')
            elif mutation=='wrong_tls_group':
                lines=(target/raw).read_text(encoding='utf-8').splitlines();r=json.loads(lines[0])
                facts=r['tls'] if kind=='handshake' else r['client_tls']
                facts['curve_id']=29;facts['curve_name']='X25519'
                lines[0]=json.dumps(r);(target/raw).write_text('\n'.join(lines)+'\n',encoding='utf-8')
            # Producer validation applies to originals; this separate command
            # is the only judgement of a mutation copy.
            (target/'validation.json').unlink(missing_ok=True)
            check('reject_'+entry['name']+'_'+mutation,binary,target,code,5)
    result=dict(status='PASS' if rows and all(x['passed'] for x in rows) else 'FAIL',positives=sum(x['expected_exit']==0 for x in rows),negative_controls=sum(x['expected_exit']==5 for x in rows),cases=rows,new_performance_measurements=False)
    (a.out/'controls.json').write_text(json.dumps(result,indent=2),encoding='utf-8')
    print(result['status'],result['positives'],'positive',result['negative_controls'],'negative controls')
    return 0 if result['status']=='PASS' else 1

if __name__=='__main__':raise SystemExit(main())
