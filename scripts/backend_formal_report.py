"""Current report: measurement status and crypto verification are separate inputs."""
import argparse
import hashlib
import json
from pathlib import Path
import sys
import m5_formal_report


def strict_json_text(text):
    def pairs(items):
        value = {}
        for key, item in items:
            if key in value:
                raise ValueError('duplicate JSON key: ' + key)
            value[key] = item
        return value
    def invalid_constant(value):
        raise ValueError('non-finite JSON value: ' + value)
    value = json.loads(text, object_pairs_hook=pairs, parse_constant=invalid_constant)
    if not isinstance(value, dict):
        raise ValueError('expected one JSON object')
    return value


def verification_contract(freeze, document, base):
    if not isinstance(document, dict) or document.get('schema') != 'edgehybrid-verification-status/1':
        raise ValueError('--verification-status needs crypto/race evidence, NOT matrix execution_status.json')
    if (not freeze.get('version') or document.get('version') != freeze['version']
            or document.get('source_manifest_sha256') != freeze['source_manifest_sha256']):
        raise ValueError('verification/source/version association missing or different')
    for field in ('race', 'independent_sm2'):
        result = document.get(field)
        if not isinstance(result, dict) or result.get('status') not in ('PASS','FAIL','BLOCKED','NOT_EXECUTED'):
            raise ValueError('missing typed verification status: ' + field)
        if not isinstance(result.get('scope'), str) or not result['scope']:
            raise ValueError('verification scope required: ' + field)
        if result['status'] == 'NOT_EXECUTED':
            if not result.get('reason'):
                raise ValueError('not-executed reason required')
            continue
        receipts = result.get('evidence')
        if not isinstance(receipts,list) or not receipts:
            raise ValueError('actual attempt evidence required: ' + field)
        for entry in receipts:
            if not isinstance(entry,dict) or not isinstance(entry.get('path'),str):
                raise ValueError('invalid verification evidence reference')
            path = Path(entry['path'])
            if not path.is_absolute():
                path = base / path
            if not path.is_file() or hashlib.sha256(path.read_bytes()).hexdigest() != entry.get('sha256'):
                raise ValueError('verification receipt missing or hash mismatch: '+entry['path'])
    return document


def main():
    ap=argparse.ArgumentParser()
    for name in ('freeze','data','out','verification-status'):
        ap.add_argument('--'+name,required=True,type=Path)
    args=ap.parse_args()
    freeze=strict_json_text(args.freeze.read_text(encoding='utf-8'))
    status=verification_contract(freeze,strict_json_text(args.verification_status.read_text(encoding='utf-8')),args.verification_status.parent)
    # Do not create a partial report before detecting a mis-specified status file.
    sys.argv=['m5_formal_report.py','--freeze',str(args.freeze),'--data',str(args.data),'--out',str(args.out)]
    code=m5_formal_report.main()
    if code != 0:
        raise RuntimeError('measurement revalidation failed')
    path=args.out/'recomputed_report.json'
    data=strict_json_text(path.read_text(encoding='utf-8'))
    data['campaign_plan_id']=freeze['campaign_plan_id'];data['version']=freeze['version']
    data['unsupported']['race']=status['race'];data['unsupported']['independent_SM2_key_exchange']=status['independent_sm2']
    data['verification_status_sha256']=hashlib.sha256(args.verification_status.read_bytes()).hexdigest()
    data['validation_scope']='Measured benchmark observations and separately referenced verification evidence; not security certification.'
    path.write_text(json.dumps(data,ensure_ascii=False,indent=2)+'\n',encoding='utf-8')
    path=args.out/'REPORT.md';text=path.read_text(encoding='utf-8')
    text=text.replace('# M5 正式实验报告','# 当前后端构建正式实验报告').replace('race、独立 SM2 交换验证保持历史缺口，没有新增安全审计/证明/合规认证。','当前race与独立SM2状态见单独验证回执；不由性能结果推断，不构成安全认证。').replace('M5 完成后停止，未执行 M6。','本报告仅覆盖冻结的R5实验；创新实验使用独立记录。')
    text+='\n当前campaign_plan_id：'+freeze['campaign_plan_id']+'；formal_contract_id是测量格式，不是历史采样身份。\n'
    path.write_text(text,encoding='utf-8')
    return 0

if __name__=='__main__':
    try:raise SystemExit(main())
    except (ValueError,KeyError,OSError,RuntimeError) as error:
        print('REPORT_BLOCKED: '+str(error),file=sys.stderr);raise SystemExit(2)
