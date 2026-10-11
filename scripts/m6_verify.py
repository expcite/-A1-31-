"""Offline M6 contract verification; does not execute handshakes or alter M5."""
import argparse, hashlib, json, re
from pathlib import Path
from m6_compare import RULES, DEFECTS, events, observations
from m6_evidence_contract import mutation_outcome
ROOT=Path(__file__).resolve().parents[1]
def read(p):return json.loads(p.read_text(encoding='utf-8'))
def sha(p):return hashlib.sha256(p.read_bytes()).hexdigest()
def verify(root,comparison,freeze_dir=None):
 freeze_dir=(freeze_dir or root/'results/raw/m6_20261001/freeze').resolve()
 f=read(freeze_dir/'freeze.json');report=read(comparison/'comparison.json');context=read(comparison/'context.json')
 current=f.get('schema')=='edgehybrid-m6-current-freeze/1'
 if current:
  assert sha(freeze_dir/'baseline.json')==f['baseline_manifest_sha256']
  assert sha(freeze_dir/'production_go.json')==f['production_go_manifest_sha256']
  assert report['measurement_source_manifest_sha256']==f['measurement_source_manifest_sha256']
  ref=read(comparison/'freeze_reference.json')
  assert ref['freeze_sha256']==sha(freeze_dir/'freeze.json') and ref['baseline_sha256']==sha(freeze_dir/'baseline.json')
  registry={k:(v['branch'],v['A_target'],v['B_target'])for k,v in context['defects'].items()}
  raw_registry=json.dumps(registry,sort_keys=True,separators=(',',':'),ensure_ascii=False).encode('utf-8')
  assert hashlib.sha256(raw_registry).hexdigest()==f['defect_registry_sha256'],'registry/denominator differs from pre-registration'
 assert report['run_id']==context['run_id']
 assert report['normal_all_pass'] and report['reproduction']['semantic_match']
 expected={(key,suite)for key in context['defects']for suite in ('A','B')}
 assert len(report['mutants'])==len(expected) and {(x['variant'],x['suite'])for x in report['mutants']}==expected
 assert sha(root/'docs/INNOVATION_PLAN.md')==f['plan_sha256']
 assert sha(comparison/'measurement_source_manifest.sha256')==report['measurement_source_manifest_sha256']
 cases=0
 for name in ['normal_B_1','normal_B_2','normal_B_3','reproduce_B']:
  d=comparison/'commands'/name;aa,bb=observations(events(d/'stdout.jsonl'))
  assert read(d/'process_exit.json')['exit_code']==0
  assert len(bb)==(3 if name=='reproduce_B' else 39)
  assert len({b['case_id'] for b in bb})==len(bb)
  for b in bb:
   assert b['run_id']==context['run_id']+'/'+name
   assert b['source_manifest_sha256']==report['measurement_source_manifest_sha256']
   assert b['business_accepted'] is None and b['plaintext_delivered'] is None
   assert b['actual_stage']==b['expected_stage']
   assert isinstance(b['device_channel_created'],bool) and isinstance(b['gateway_channel_created'],bool)
   assert b['device_stage'] and b['gateway_stage'] and b['mutation_rule']
   assert b['elapsed_ns']>=0 and b['public_frames']
   for trace in b['public_frames']:
    for frame in trace:
     assert re.fullmatch('[0-9a-f]{64}',frame['original_sha256'])
     assert all(re.fullmatch('[0-9a-f]{64}',h) for h in frame['forwarded_sha256'])
   if b['case_id'].endswith('positive'): assert b['device_channel_created'] and b['gateway_channel_created']
   elif b['expected_stage']=='record_rejected': assert b['record_replay_accepted'] is False and b['device_channel_created'] and b['gateway_channel_created']
   else: assert not b['device_channel_created' if b['expected_side']=='client' else 'gateway_channel_created']
   if b['case_id'].endswith('finB'): assert b['gateway_channel_created'] and not b['device_channel_created']
  cases+=len(bb)
 for b in read(freeze_dir/'baseline.json'):assert sha(root/b['path'])==b['sha256']
 # Recalculate coverage and defect detection from the original Go event stream.
 for suite in ['A','B']:
  for rep in range(1,4):
   name=f'normal_{suite}_{rep}';d=comparison/'commands'/name;ev=events(d/'stdout.jsonl');aa,bb=observations(ev)
   passed=[e['Test'] for e in ev if e['Action']=='pass' and 'Test'in e]
   if suite=='A': covered={k for k,targets in RULES.items() if all(any(n==target or n.startswith(target+'/') for n in passed) for target in targets)}
   else:covered={b['rule'] for b in bb if b['rule']in RULES and b['expected_stage']==b['actual_stage']}
   assert covered==set(report['coverage'][suite]['covered'])
   assert read(d/'process_exit.json')['exit_code']==0
   assert read(d/'process_exit.json')['wall_seconds']==report['coverage'][suite]['wall_seconds'][rep-1]
   assert (d/'stdout.jsonl').stat().st_size==report['coverage'][suite]['stdout_bytes'][rep-1]
 for result in report['mutants']:
  d=comparison/'commands'/(result['variant']+'_'+result['suite']);ev=events(d/'stdout.jsonl');target=result['target']
  entry=context['defects'][result['variant']]
  assert target==entry['A_target'if result['suite']=='A'else'B_target']
  proc=read(d/'process_exit.json')
  verdict=mutation_outcome(ev,target=target,variant=result['variant'],exit_code=proc['exit_code'],timed_out=proc.get('timeout',False),healthy=True)
  assert verdict['outcome'] in ('DETECTED','MISSED','UNREACHABLE'),'mutation execution/evidence incomplete'
  assert verdict['outcome'].lower()==result['outcome'] and verdict['detected']==result['detected'] and verdict['reached']==result['reached']
  assert proc['exit_code']==result['exit_code']
 resolution_path=root/'results/raw/m6_20261001/source_resolution.json'
 if not current and resolution_path.is_file():
  for stage,data in read(resolution_path).items():
   assert sha(root/data['manifest_path'])==data['manifest_sha256']
   assert all(sha(root/e['archived_path'])==e['sha256'] for e in data['entries'])
 checks={}
 for label,file in [('M5','m5_preserved.json'),('production_go','production_go.json')]:
  if current and label=='M5':
   checks[label]={'verified_here':False,'required_separate_history_guard':True};continue
  hashes=read(freeze_dir/file)
  changed=[p for p,h in hashes.items() if not (root/p).is_file() or sha(root/p)!=h]
  assert not changed,(label,changed);checks[label]=dict(files=len(hashes),unchanged=True)
 # Frozen absolute author paths are historical metadata, never an offline
 # dependency. Old Demo ZIPs are excluded from the self-contained review pack.
 demo=Path(f.get('demo_path','__historical_demo_not_embedded__'))
 if demo.is_absolute():demo=root/'__historical_demo_not_embedded__'
 checks['Demo']=dict(available=demo.is_file(),sha256=sha(demo) if demo.is_file() else None,expected_sha256=f.get('demo_sha256'))
 if demo.is_file():assert checks['Demo']['sha256']==f['demo_sha256']
 return dict(status='PASS',case_records_checked=cases,preserved=checks,rule_coverage=report['coverage'],known_defect_results=report['mutants'],scope='offline contract/hash verification; no race/independent crypto execution')
def main():
 ap=argparse.ArgumentParser();ap.add_argument('--root',type=Path,default=ROOT);ap.add_argument('--comparison',type=Path);ap.add_argument('--out',type=Path,required=True);ap.add_argument('--freeze-dir',type=Path);a=ap.parse_args()
 result=verify(a.root.resolve(),a.comparison.resolve() if a.comparison else a.root/'results/raw/m6_20261001/comparison_v2',a.freeze_dir.resolve()if a.freeze_dir else None)
 a.out.write_text(json.dumps(result,ensure_ascii=False,indent=2)+'\n',encoding='utf-8');print('M6 raw contract verified; see preserved fields for exact historical-data verification scope.')
if __name__=='__main__':main()
