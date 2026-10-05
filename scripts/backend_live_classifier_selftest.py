"""Tool guard tests only; no mock protocol success. Optional real raw replay."""
from pathlib import Path
from types import SimpleNamespace
import argparse,copy,json,tempfile,unittest,hashlib,shutil
import backend_live_acceptance as live

class Guard(unittest.TestCase):
    def setUp(self):
        self.e={'run_id':'one','event_id':1,'device_id':'D002','endpoint':'device','stage':'dial_failed','at':'2026-10-04T02:21:18.3252568Z','detail':'dial tcp4 127.0.0.1:3513: operation was canceled'}
        self.w=[{'label':'planned_final_stop','device_ids':list(live.DEVICE_IDS),'started_at':'2026-10-04T02:21:18.250000+00:00','finished_at':'2026-10-04T02:21:18.400000+00:00'}]
    def accepts(self,e=None,w=None,verified=True):return live.planned_shutdown_cancellation(e or self.e,self.w if w is None else w,'one',verified)
    def test_positive_bounded_stop_cancellation(self):self.assertTrue(self.accepts());self.assertNotIn('sid',self.e)
    def test_real_dial_failure_is_not_cancellation(self):
        for detail in ['dial tcp4 127.0.0.1:3513: connection refused','dial tcp4 127.0.0.1:3513: i/o timeout','operation was canceled','dial tcp4 10.0.0.1:3513: operation was canceled']:
            self.assertFalse(self.accepts(dict(self.e,detail=detail)))
    def test_outside_stop_window_not_accepted(self):
        for at in ['2026-10-04T02:21:18.249Z','2026-10-04T02:21:18.401Z']:
            self.assertFalse(self.accepts(dict(self.e,at=at)))
    def test_other_phase_not_accepted(self):self.assertFalse(self.accepts(w=[dict(self.w[0],label='t0900_GW001')]))
    def test_unknown_or_other_identity_run_endpoint_rejected(self):
        for override in [{'device_id':'UNKNOWN'},{'run_id':'old'},{'endpoint':'gateway'},{'stage':'ack_rejected'}]:self.assertFalse(self.accepts(self.e|override))
    def test_missing_cleanup_and_time_facts_rejected(self):
        self.assertFalse(self.accepts(verified=False))
        self.assertFalse(self.accepts(w=[]))
        self.assertFalse(self.accepts(w=[{k:v for k,v in self.w[0].items()if k!='finished_at'}]))
        self.assertFalse(self.accepts(dict(self.e,at='invalid')))
    def test_cleanup_requires_actual_same_run_zero_resources(self):
        with tempfile.TemporaryDirectory()as p:
            p=Path(p);r=SimpleNamespace(out=p,run='one',p=SimpleNamespace(returncode=0));self.assertFalse(live.verified_final_shutdown(r))
            state={'run_id':'one','devices':[{'id':id,'status':'offline'}for id in live.DEVICE_IDS],'live_diagnostics':{k:0 for k in ['connections_active','sessions_active','device_workers_active','local_queue_items','control_queue_items']}}
            cleanup={'app_exit_code':0,'process_exited':True,'reader_exited':True}
            def write():
                (p/'devices_and_workers_stopped.json').write_text(json.dumps(state));(p/'cleanup.json').write_text(json.dumps(cleanup))
            write();self.assertTrue(live.verified_final_shutdown(r))
            state['live_diagnostics']['connections_active']=1;write();self.assertFalse(live.verified_final_shutdown(r))
            state['live_diagnostics']['connections_active']=0;state['run_id']='old';write();self.assertFalse(live.verified_final_shutdown(r))
            state['run_id']='one';cleanup['app_exit_code']=1;write();self.assertFalse(live.verified_final_shutdown(r))

def replay(root,out,exe):
    root=root.resolve();out=out.resolve();assert not out.is_relative_to(root)and not root.is_relative_to(out)
    original=root/'continuous';command=json.loads((original/'command.json').read_text(encoding='utf-8'));acceptance=json.loads((root/'acceptance.json').read_text(encoding='utf-8'));run=command['run_id']
    watched=[original/(run+'.jsonl'),original/'command.json',original/'cleanup.json',original/'devices_and_workers_stopped.json',original/'raw_observations.json',root/'acceptance.json']
    digest=lambda p:hashlib.sha256(p.read_bytes()).hexdigest();before={str(p):digest(p)for p in watched}
    out.mkdir(exist_ok=False)
    for p in watched[:4]:shutil.copy2(p,out/p.name)
    r=SimpleNamespace(run=run,out=out,p=SimpleNamespace(returncode=command['exit_code']),info={'source_manifest_sha256':command['source_manifest_sha256']},exe=exe)
    live.raw_observations(r,acceptance['phase_windows'])
    result=json.loads((out/'raw_observations.json').read_text(encoding='utf-8'))
    assert result['stage_counts_recomputed']['dial_failed']==1 and len(result['planned_shutdown_cancellations'])==1
    assert result['planned_shutdown_cancellations'][0]['observed_sid']is None and result['planned_shutdown_cancellations'][0]['remote_execution_success']is None
    assert all(digest(Path(p))==h for p,h in before.items())
    (out/'offline_recheck.json').write_text(json.dumps({'status':'PASS','original_long_acceptance_status':acceptance['status'],'original_records_unchanged':True,'records':before,'scope':'Same real raw accepted by revised bounded shutdown classifier; original 1800 campaign still FAIL and its missing restart remains NOT_EXECUTED. This is not a new live run.'},indent=2),encoding='utf-8')

if __name__=='__main__':
    ap=argparse.ArgumentParser();ap.add_argument('--recorded-run',type=Path);ap.add_argument('--out',type=Path);ap.add_argument('--exe',type=Path);a=ap.parse_args()
    result=unittest.TextTestRunner(verbosity=2).run(unittest.defaultTestLoader.loadTestsFromTestCase(Guard))
    if not result.wasSuccessful():raise SystemExit(1)
    if a.recorded_run:
        assert a.out and a.exe;replay(a.recorded_run,a.out,a.exe)
