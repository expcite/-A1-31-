"""Save actual complete Go regression, original Python controls and M5 revalidation."""
import subprocess,sys
from pathlib import Path
ROOT=Path(__file__).resolve().parents[1];GO=r'C:\Users\20477\.workbuddy\binaries\go\go\bin\go.exe';OUT=ROOT/'results/raw/m6_20261001/regression'
def run(name,cmd,timeout=360):
 r=subprocess.run([sys.executable,str(ROOT/'scripts/run_checked.py'),'--log-dir',str(OUT),'--name',name,'--timeout',str(timeout),'--',*cmd],cwd=ROOT)
 if r.returncode:raise SystemExit(r.returncode)
def main():
 run('go_test_json',[GO,'test','-json','-count=1','./...'],600)
 run('go_mod_verify',[GO,'mod','verify']);run('go_vet',[GO,'vet','./...'])
 for name in ['summarize_selftest','fleet_verifier_selftest','bench_result_selftest','m4case_extractor_selftest']:run(name,[sys.executable,str(ROOT/'scripts'/f'{name}.py')])
 run('m5_recompute',[sys.executable,str(ROOT/'scripts/m5_formal_report.py'),'--freeze',str(ROOT/'results/raw/m5_formal_20261001/freeze/freeze.json'),'--data',str(ROOT/'results/raw/m5_formal_20261001/data'),'--out',str(OUT/'m5_recomputed')],600)
 run('m6_raw_verify',[sys.executable,str(ROOT/'scripts/m6_verify.py'),'--out',str(OUT/'m6_verified.json')])
 print('All regression commands exited 0; M5 was revalidated without sampling.')
if __name__=='__main__':main()
