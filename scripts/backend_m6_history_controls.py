"""Positive-first offline controls against the resolved ORIGINAL M6 source.

This does not execute protocol attacks, alter M6 raw, or attribute old runs to
the backend build. The original complete validator is reused unchanged.
"""
import argparse,pathlib,sys,json,hashlib
import m6_verify,m6_tools_selftest
def main():
 ap=argparse.ArgumentParser();ap.add_argument('--root',type=pathlib.Path,required=True);ap.add_argument('--out',type=pathlib.Path,required=True);a=ap.parse_args();root=a.root.resolve();out=a.out.resolve();out.mkdir(parents=True,exist_ok=False);comparison=root/'results/raw/m6_20261001/comparison_v2'
 watched=[p for p in comparison.rglob('*')if p.is_file()];before={str(p):hashlib.sha256(p.read_bytes()).hexdigest()for p in watched}
 positive=m6_verify.verify(root,comparison);(out/'positive.json').write_text(json.dumps(positive,ensure_ascii=False,indent=2)+'\n',encoding='utf-8')
 m6_tools_selftest.ROOT=root;m6_tools_selftest.SRC=comparison;sys.argv=['m6_tools_selftest.py','--out',str(out/'negative_controls.json')];m6_tools_selftest.main()
 assert all(hashlib.sha256(pathlib.Path(p).read_bytes()).hexdigest()==h for p,h in before.items())
 (out/'association.json').write_text(json.dumps({'status':'PASS','original_m6_context':str(root),'positive_verified_before_negatives':True,'comparison_files':len(before),'comparison_unchanged':True,'scope':'historical M6 offline evidence controls; no current backend protocol attacks or resampling','current_script_sha256':hashlib.sha256(pathlib.Path(__file__).read_bytes()).hexdigest()},ensure_ascii=False,indent=2)+'\n',encoding='utf-8');return 0
if __name__=='__main__':sys.exit(main())
