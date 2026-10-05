"""M6 result classification only. No production code or cryptography."""
from __future__ import annotations
import json
class EvidenceError(ValueError):
    pass

def require(condition: bool, message: str) -> None:
    if not condition:
        raise EvidenceError(message)

def target_match(name: str, target: str) -> bool:
    return name==target or name.startswith(target+'/')

def mutation_outcome(events: list[dict], *, target: str, variant: str, exit_code: int | None,
                     timed_out: bool, healthy: bool) -> dict:
    require(isinstance(events,list)and all(isinstance(e,dict)for e in events),'invalid Go events')
    require(isinstance(target,str)and target and isinstance(variant,str)and variant,'target/variant missing')
    if not healthy:
        return {'outcome':'PREREQUISITE_FAILED','detected':None,'reached':None}
    if timed_out or exit_code not in (0,1):
        return {'outcome':'EXECUTION_ERROR','detected':None,'reached':None}
    scoped=[e for e in events if isinstance(e.get('Test'),str)and target_match(e['Test'],target)]
    ran=any(e.get('Action')=='run' for e in scoped)
    if not ran:
        outcome='BUILD_OR_SELECTION_ERROR'if exit_code else'NOT_EXECUTED'
        return {'outcome':outcome,'detected':None,'reached':None}
    if any(e.get('Action')=='skip' for e in scoped):
        return {'outcome':'SKIPPED','detected':None,'reached':None}
    # Parse a marker token, not a substring: D1 must not match D10.
    marker='M6REACH '+variant
    reached=any(any(line.strip()==marker for line in str(e.get('Output','')).splitlines())for e in scoped)
    failed=any(e.get('Action')=='fail' for e in scoped)
    passed=any(e.get('Action')=='pass' for e in scoped)
    if not (passed or failed):
        return {'outcome':'INCOMPLETE','detected':None,'reached':reached}
    if not reached:
        return {'outcome':'UNREACHABLE','detected':False,'reached':False,'target_failed':failed}
    if failed and exit_code==1:
        return {'outcome':'DETECTED','detected':True,'reached':True,
                'scope':'target assertion failed after target marker; inspect failure reason before making a security claim'}
    if passed and not failed and exit_code==0:
        return {'outcome':'MISSED','detected':False,'reached':True}
    return {'outcome':'EXECUTION_ERROR','detected':None,'reached':reached}


def strict_events(path):
    def pairs(items):
        d={}
        for k,v in items:
            if k in d:raise EvidenceError('duplicate Go event key: '+k)
            d[k]=v
        return d
    out=[]
    for number,line in enumerate(path.read_text(encoding='utf-8-sig',errors='strict').splitlines(),1):
        if not line.strip():continue
        try:
            e=json.loads(line,object_pairs_hook=pairs,parse_constant=lambda v:(_ for _ in ()).throw(EvidenceError('nonfinite Go event')))
        except (ValueError,UnicodeError)as error:
            raise EvidenceError(f'invalid Go JSON line {number}: {error}')from error
        if not isinstance(e,dict)or not isinstance(e.get('Action'),str):
            raise EvidenceError(f'invalid Go event object at line {number}')
        out.append(e)
    return out
