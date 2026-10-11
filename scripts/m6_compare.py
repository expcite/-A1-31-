"""Execute the frozen A/B matrix. Defective client copies exist only in _test.go."""
import argparse, datetime, hashlib, json, os, re, shutil, subprocess, tempfile, time
from pathlib import Path
import source_manifest
from m6_evidence_contract import mutation_outcome, strict_events
ROOT=Path(__file__).resolve().parents[1]
GO=os.environ.get('GO',r'C:\Users\20477\.workbuddy\binaries\go\go\bin\go.exe')
def read(p): return json.loads(p.read_text(encoding='utf-8'))
def write(p,x): p.write_text(json.dumps(x,ensure_ascii=False,indent=2)+'\n',encoding='utf-8')
def sha(p): return hashlib.sha256(p.read_bytes()).hexdigest()
RULES={
 'selection':['TestDowngradeScenariosAreRejected/replace_H1_offer_with_suite_1_only'],
 'offer_binding':['TestDowngradeScenariosAreRejected/reorder_H1_offer_to_1_2'],
 'support_binding':['TestDowngradeScenariosAreRejected/reorder_H2_supported_suites'],
 'identity':['TestReencodedServerCoreFieldsStillRejected/responder_identity_swapped'],
 'key_id':['TestReencodedServerCoreFieldsStillRejected/key_identifier_swapped'],
 'nonce':['TestReencodedServerCoreFieldsStillRejected/server_nonce_changed'],
 'sm2_ephemeral':['TestReencodedServerCoreFieldsStillRejected/gateway_ephemeral_public_key_changed'],
 'kem_presence':['TestDowngradeScenariosAreRejected/delete_ekA_while_offering_hybrid'],
 'kem_public':['TestDowngradeScenariosAreRejected/modify_ekA_same_length'],
 'kem_ciphertext':['TestDowngradeScenariosAreRejected/modify_kem_ct_same_length'],
 'signature':['TestTamperedGatewaySignatureRejected','TestTamperedDeviceSignatureRejected'],
 'finished':['TestTamperedFinishedARejected','TestTamperedFinishedBRejected','TestImplicitRejectionIsCaughtByFinished'],
 'sequence':['TestMessageSequenceAndInjectionViolations'],
 'old_session':['TestWireReplayOfOldSessionRecordsIsRejected'],
 'cross_device':['TestWireCrossDeviceCiphertextIsRejected'],
 'same_record':['TestRecordReplayRejected'],
}
DEFECTS={
 'D1':('return nil, fmt.Errorf("%w: gateway signature: %v", ErrAuthentication, err)','TestTamperedGatewaySignatureRejected','TestM6Structured/T_sigB'),
 'D2':('return nil, fmt.Errorf("%w: gateway Finished did not match", ErrTranscript)','TestTamperedFinishedBRejected','TestM6Structured/T_finB'),
 'D3':('return nil, fmt.Errorf("%w: gateway selected suite %d but %d was expected", ErrProtocol, core.SelectedSuite, uint16(sel))','TestDowngradeScenariosAreRejected/replace_H1_offer_with_suite_1_only','TestM6Structured/H_downgrade'),
}
def events(path):
 return strict_events(path)
def observations(ev):
 b=[];a=[]
 buffers={}
 for e in ev:
  key=e.get('Test','');buffers[key]=buffers.get(key,'')+e.get('Output','')
 lines=[dict(Output=s,Test=key) for key,buf in buffers.items() for s in buf.splitlines()]
 for e in lines:
  s=e['Output']
  if 'M6CASE ' in s: b.append(json.loads(s.split('M6CASE ',1)[1]))
  if 'M2NEG ' in s or 'M4CASE ' in s:
   fields={k:json.loads(v) for k,v in re.findall(r'(\w+)=("(?:\\.|[^"\\])*"|true|false|null)',s)}
   a.append(dict(test=e.get('Test'),**fields))
 return a,b
def execute(out,name,root,pattern,freeze,source):
 d=out/'commands'/name;d.mkdir(parents=True,exist_ok=False)
 cmd=[GO,'test','-json','-count=1','-timeout=150s','-run',pattern,'./internal/handshake']
 env=os.environ.copy();env['GOTOOLCHAIN']='local';env['EDGEHYBRID_M6_RUN_ID']=freeze['run_id']+'/'+name;env['EDGEHYBRID_M6_SOURCE']=source
 started=datetime.datetime.now(datetime.timezone.utc).isoformat();t=time.perf_counter();timeout=False
 with (d/'stdout.jsonl').open('wb') as so,(d/'stderr.txt').open('wb') as se:
  p=subprocess.Popen(cmd,cwd=root,env=env,stdout=so,stderr=se)
  try: code=p.wait(timeout=180)
  except subprocess.TimeoutExpired:
   timeout=True;subprocess.run(['taskkill','/PID',str(p.pid),'/T','/F'],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL);code=p.wait()
 wall=time.perf_counter()-t
 (d/'exit.txt').write_text(str(code)+'\n',encoding='ascii')
 write(d/'process_exit.json',dict(exit_code=code,wall_seconds=wall,timeout=timeout,started_utc=started,command=cmd))
 ev=events(d/'stdout.jsonl');aa,bb=observations(ev)
 meta=dict(name=name,command=cmd,cwd=str(root),started_utc=started,exit_code=code,timeout=timeout,wall_seconds=wall,stdout_bytes=(d/'stdout.jsonl').stat().st_size,package_elapsed=[e['Elapsed'] for e in ev if e['Action'] in ['pass','fail'] and 'Test' not in e and 'Elapsed'in e],passed=[e['Test'] for e in ev if e['Action']=='pass' and 'Test'in e],failed=[e['Test'] for e in ev if e['Action']=='fail' and 'Test'in e],reach=[dict(test=e.get('Test'),marker=e['Output'].strip()) for e in ev if 'M6REACH 'in e.get('Output','')],a_records=aa,b_records=bb)
 write(d/'command.json',{k:v for k,v in meta.items() if k not in ['a_records','b_records','passed','failed','reach']})
 write(d/'observations.json',dict(a=aa,b=bb,passed=meta['passed'],failed=meta['failed'],reach=meta['reach']))
 print(f'{name}: exit={code}, wall={wall:.3f}s, A records={len(aa)}, B records={len(bb)}',flush=True)
 return meta
def main():
 ap=argparse.ArgumentParser();ap.add_argument('--out',type=Path,required=True);ap.add_argument('--run-id');ap.add_argument('--freeze-dir',type=Path);a=ap.parse_args();out=a.out.resolve();out.mkdir(parents=True,exist_ok=False)
 freeze_dir=(a.freeze_dir or ROOT/'results/raw/m6_20261001/freeze').resolve()
 f=read(freeze_dir/'freeze.json');baseline=read(freeze_dir/'baseline.json')
 if f.get('schema')=='edgehybrid-m6-current-freeze/1':
  assert sha(freeze_dir/'baseline.json')==f['baseline_manifest_sha256']
  reg=json.dumps(DEFECTS,sort_keys=True,separators=(',',':'),ensure_ascii=False).encode('utf-8')
  assert hashlib.sha256(reg).hexdigest()==f['defect_registry_sha256'],'defect registry differs from pre-registration'
 if a.run_id: f['run_id']=a.run_id
 assert sha(ROOT/'docs/INNOVATION_PLAN.md')==f['plan_sha256']
 for b in baseline: assert sha(ROOT/b['path'])==b['sha256']
 manifest=''.join(f'{sha(Path(p))}  {Path(p).relative_to(ROOT).as_posix()}\n' for p in source_manifest.collect(str(ROOT)))
 (out/'measurement_source_manifest.sha256').write_text(manifest,encoding='utf-8',newline='\n');source=hashlib.sha256(manifest.encode()).hexdigest()
 if f.get('schema')=='edgehybrid-m6-current-freeze/1': assert source==f['measurement_source_manifest_sha256'],'source changed since current M6 freeze'
 write(out/'freeze_reference.json',{'freeze_dir':str(freeze_dir),'freeze_sha256':sha(freeze_dir/'freeze.json'),'baseline_sha256':sha(freeze_dir/'baseline.json')})
 write(out/'context.json',dict(f,measurement_source_manifest_sha256=source,platform=os.name,go=subprocess.check_output([GO,'version'],text=True).strip(),common_rules=RULES,defects={k:dict(branch=v[0],A_target=v[1],B_target=v[2]) for k,v in DEFECTS.items()}))
 patternA='^('+ '|'.join(n for b in baseline for n in b['tests'])+')$';patternB='^TestM6Structured$'
 runs=[]
 for rep,order in enumerate([['A','B'],['B','A'],['A','B']],1):
  for suite in order: runs.append(execute(out,f'normal_{suite}_{rep}',ROOT,patternA if suite=='A' else patternB,f,source))
 mutant=[]
 for defect,(branch,ta,tb) in DEFECTS.items():
  with tempfile.TemporaryDirectory(prefix='edgehybrid-m6-testonly-') as temp:
   root=Path(temp)
   for p in source_manifest.collect(str(ROOT)):
    p=Path(p);dest=root/p.relative_to(ROOT);dest.parent.mkdir(parents=True,exist_ok=True);shutil.copy2(p,dest)
   for p in (root/'internal/handshake').glob('*_test.go'):
    p.write_text(re.sub(r'\bClient\(', 'm6DefectClient(',p.read_text(encoding='utf-8')),encoding='utf-8',newline='\n')
   text=(ROOT/'internal/handshake/client.go').read_text(encoding='utf-8')
   assert text.count(branch)==1
   text=text.replace('func Client(', 'func m6DefectClient(').replace('clientHandshake(', 'm6DefectClientHandshake(')
   text=text.replace(branch,f'fmt.Printf("M6REACH {defect}\\n") // TEST-ONLY removed rejection, branch reached')
   p=root/'internal/handshake/m6_defect_client_test.go';p.write_text(text,encoding='utf-8',newline='\n')
   (out/f'{defect}_client_test.go.txt').write_text(text,encoding='utf-8')
   hashes={p.relative_to(root).as_posix():sha(p) for p in root.rglob('*_test.go')}
   write(out/f'{defect}_isolation.json',dict(variant=defect,test_source_hashes=hashes,production_go_unchanged=True,temporary_build='go test only; no application binaries'))
   for suite,target in [('A',ta),('B',tb)]:
    r=execute(out,f'{defect}_{suite}',root,patternA if suite=='A' else patternB,f,source);runs.append(r)
    verdict=mutation_outcome(events(out/'commands'/f'{defect}_{suite}'/'stdout.jsonl'),target=target,variant=defect,exit_code=r['exit_code'],timed_out=r['timeout'],healthy=all(x['exit_code']==0 for x in runs if x['name'].startswith('normal_')))
    mutant.append(dict(variant=defect,suite=suite,target=target,reached=verdict['reached'],detected=verdict['detected'],outcome=verdict['outcome'].lower(),exit_code=r['exit_code']))
 r=execute(out,'reproduce_B',ROOT,'^TestM6Structured$/(T_sigB|H_finB|H_implicit)$',f,source);runs.append(r)
 normal=[r for r in runs if r['name'].startswith('normal_')]
 # Coverage from actual assertions, one rule counted once regardless of case count.
 coverage={}
 for suite in ['A','B']:
  rs=[r for r in normal if f'_{suite}_' in r['name']]
  if suite=='A': covered=[k for k,targets in RULES.items() if all(any(n==target or n.startswith(target+'/') for n in rs[0]['passed']) for target in targets)]
  else: covered=sorted({o['rule'] for o in rs[0]['b_records'] if o['rule']in RULES and o['expected_stage']==o['actual_stage']})
  coverage[suite]=dict(covered=covered,missing=sorted(set(RULES)-set(covered)),denominator=len(RULES),wall_seconds=[r['wall_seconds'] for r in rs],package_elapsed=[r['package_elapsed'] for r in rs],stdout_bytes=[r['stdout_bytes'] for r in rs],top_level_tests=sum('/'not in n for n in rs[0]['passed']),leaf_cases=sum(not any(other.startswith(n+'/') for other in rs[0]['passed']) for n in rs[0]['passed']))
 report=dict(run_id=f['run_id'],measurement_source_manifest_sha256=source,normal_all_pass=all(r['exit_code']==0 for r in normal),coverage=coverage,mutants=mutant,reproduction=dict(exit_code=r['exit_code'],cases=[o['case_id'] for o in r['b_records']],semantic_match=all(any(o['case_id']==p['case_id'] and o['actual_stage']==p['actual_stage'] and o['device_channel_created']==p['device_channel_created'] and o['gateway_channel_created']==p['gateway_channel_created'] for p in normal[1]['b_records']) for o in r['b_records'])),diagnostic_fields=dict(A_records=len(normal[0]['a_records']),B_records=len(normal[1]['b_records']),A_frame_digest_records=sum('public_frames'in o for o in normal[0]['a_records']),B_frame_digest_records=sum(bool(o['public_frames']) for o in normal[1]['b_records']),A_both_endpoint_stages=sum('device_stage'in o and 'gateway_stage'in o for o in normal[0]['a_records']),B_both_endpoint_stages=sum('device_stage'in o and 'gateway_stage'in o for o in normal[1]['b_records'])),all_commands=[{k:v for k,v in r.items() if k not in ['a_records','b_records','passed','failed','reach']} for r in runs])
 write(out/'comparison.json',report)
 if not report['normal_all_pass'] or r['exit_code']!=0 or any(x['outcome'] not in ('detected','missed','unreachable')for x in mutant): raise SystemExit(1)
 print('Frozen A/B comparison recorded; mutant failures are expected evidence.')
if __name__=='__main__': main()
