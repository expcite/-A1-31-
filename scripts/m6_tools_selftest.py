"""Meaningful negative controls for the new offline evidence contract."""
import argparse,json,shutil,tempfile
from pathlib import Path
from m6_verify import verify,ROOT
SRC=ROOT/'results/raw/m6_20261001/comparison_v2'
def main():
 ap=argparse.ArgumentParser();ap.add_argument('--out',required=True,help='New run output; never overwrite frozen historical evidence');a=ap.parse_args()
 cases=[('wrong_source','9d0576f5ea7210752bd32ff6ed549a58fa99356f1ba5bd70a87cfed4c7fe9f8c','0'*64),('invented_business','"business_accepted":null','"business_accepted":false'),('missing_stage','"actual_stage":"signature_authentication"','"actual_stage":"none"'),('missing_control_channel','"device_channel_created":true','"device_channel_created":null'),('invalid_frame_digest','"original_sha256":"','"original_sha256":"INVALID')]
 checked=[]
 for name,before,after in cases:
  with tempfile.TemporaryDirectory(prefix='m6-evidence-control-') as td:
   dest=Path(td)/'comparison';shutil.copytree(SRC,dest)
   p=dest/'commands/normal_B_1/stdout.jsonl';ev=[json.loads(x) for x in p.read_text(encoding='utf-8').splitlines()]
   changed=False
   for e in ev:
    if before in e.get('Output',''):e['Output']=e['Output'].replace(before,after);changed=True
   assert changed,name
   p.write_text(''.join(json.dumps(e)+'\n' for e in ev),encoding='utf-8')
   try:verify(ROOT,dest)
   except (AssertionError,ValueError,KeyError):checked.append(dict(name=name,rejected=True))
   else:raise AssertionError('invalid evidence accepted: '+name)
 result=dict(status='PASS',negative_controls=checked,scope='offline evidence controls; not protocol attacks')
 out=Path(a.out);out.parent.mkdir(parents=True,exist_ok=True);out.write_text(json.dumps(result,indent=2)+'\n',encoding='utf-8');print('5 malformed M6 evidence copies rejected; original data untouched.')
if __name__=='__main__':main()
