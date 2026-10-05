"""Current plan-driven portable gate. Old release_verify remains historical.

Index paths are relative to the review root. Original absolute producer paths
are metadata, never fallback dependencies. Outputs must be external and new.
"""
from pathlib import Path, PurePosixPath
import argparse, hashlib, json, os, subprocess, sys, zipfile

def require(ok, reason):
    if not ok: raise ValueError(reason)

def sha(p): return hashlib.sha256(p.read_bytes()).hexdigest()

def strict_json(text):
    def pairs(items):
        d={}
        for k,v in items:
            require(k not in d,'duplicate JSON key '+k); d[k]=v
        return d
    return json.loads(text,object_pairs_hook=pairs,
        parse_constant=lambda x: (_ for _ in ()).throw(ValueError('nonfinite '+x)))

def read(p):return strict_json(p.read_text(encoding='utf-8'))

def member(root,rel):
    require(isinstance(rel,str)and rel and '\\'not in rel and ':'not in rel,'invalid relative path')
    q=PurePosixPath(rel)
    require(not q.is_absolute()and '..'not in q.parts and str(q)==rel,'unsafe relative path '+rel)
    p=root.joinpath(*q.parts)
    require(p.resolve().is_relative_to(root.resolve()),'escaping path '+rel)
    for t in [p,*p.parents]:
        if t==root:break
        require(not t.is_symlink()and not getattr(t,'is_junction',lambda:False)(),'reparse point '+rel)
    return p

def hashed(root,rel,h):
    p=member(root,rel); require(p.is_file()and sha(p)==h,'missing/hash mismatch '+rel);return p

def manifest(root):
    m=root/'MANIFEST.sha256';require(m.is_file(),'missing release manifest');declared={}
    for line in m.read_text(encoding='utf-8').splitlines():
        h,p=line.split('  ',1);require(p not in declared and p!='MANIFEST.sha256','duplicate/self manifest');declared[p]=h
    actual={p.relative_to(root).as_posix()for p in root.rglob('*')if p.is_file()and p!=m}
    require(actual==set(declared),'manifest member set differs')
    for p,h in declared.items():hashed(root,p,h)
    return {'files':len(declared),'sha256':sha(m)}

def dependency(root,row,archive):
    require(row['archive']==Path(row['archive']).name,'software archive must be a filename')
    p=archive or root.parent/row['archive']
    require(p.is_file()and sha(p)==row['sha256'],'required software archive missing/hash mismatch')
    with zipfile.ZipFile(p)as z:
        names=z.namelist();require(len(names)==len(set(names)),'duplicate archive member')
        require(z.testzip()is None,'software CRC failure')
        prefix=row['root']+'/'
        declared={}
        for line in z.read(prefix+'MANIFEST.sha256').decode('utf-8').splitlines():
            h,n=line.split('  ',1);member(root,n);require(n not in declared,'duplicate software manifest');declared[n]=h
        require(set(names)=={prefix+'MANIFEST.sha256'}|{prefix+n for n in declared},'software member set mismatch')
        for n,h in declared.items():require(hashlib.sha256(z.read(prefix+n)).hexdigest()==h,'software member hash '+n)
        build=strict_json(z.read(prefix+'build_info.json').decode('utf-8'))
        require(build['version']==row['version']and build['source_manifest_sha256']==row['source_manifest_sha256'],'software source/version mismatch')
        require(hashlib.sha256(z.read(prefix+'CURRENT_SOURCE_MANIFEST.sha256')).hexdigest()==build['source_manifest_sha256'],'software source manifest association')
        for n,v in build['binaries'].items():require(declared.get(n)==v['sha256'],'binary metadata mismatch '+n)
    return build

def validate(root,archive=None):
    ix=read(root/'CURRENT_DELIVERY_INDEX.json')
    require(ix['schema']=='edgehybrid-core-delivery/1'and ix['relative_root']=='review package root','index schema/root')
    ids=set()
    for tree in ix['trees']:
        require(tree['id']not in ids,'duplicate tree ID');ids.add(tree['id']);folder=member(root,tree['path'])
        require(folder.is_dir(),'missing tree '+tree['path'])
        actual={p.relative_to(folder).as_posix()for p in folder.rglob('*')if p.is_file()}
        require(actual==set(tree['files']),'tree member set mismatch '+tree['id'])
        for p,h in tree['files'].items():hashed(folder,p,h)
    for f in ix['required_files']:hashed(root,f['path'],f['sha256'])
    source=member(root,ix['source_root']);sys.path.insert(0,str(source/'scripts'))
    import source_manifest as sm
    content=''.join(sm.sha256_file(p)+'  '+Path(p).relative_to(source).as_posix()+'\n'for p in sm.collect(str(source))).encode()
    require(hashlib.sha256(content).hexdigest()==ix['source_manifest_sha256'],'current source differs')
    require(content==(source/'CURRENT_SOURCE_MANIFEST.sha256').read_bytes(),'source manifest bytes differ')
    require((source/'VERSION').read_text().strip()==ix['version'],'source version differs')
    plan=read(member(root,ix['campaign']['registered_matrix']))
    f=read(member(root,ix['campaign']['freeze']))
    require(f['version']==ix['version']and f['source_manifest_sha256']==ix['source_manifest_sha256'],'freeze source/version')
    require(f['campaign_plan_sha256']==sha(source/'docs/BACKEND_FORMAL_EXPERIMENT_PLAN.md')and f['plan_sha256']==sha(source/'docs/FORMAL_EXPERIMENT_PLAN.md'),'plan hash differs')
    require(f['campaign_plan_id']==plan['campaign_id'],'campaign plan ID')
    rows=ix['campaign']['batches'];require(len(rows)==len(plan['matrix']),'batch denominator differs')
    require([[r['experiment'],r['mode'],r['batch']]for r in rows]==plan['matrix'],'batch members/order differ')
    original=read(member(root,ix['campaign']['results_index']))
    status=read(member(root,ix['campaign']['execution_status']))
    require(status['status']in ('COMPLETED','COMPLETED_WITH_FAILURES')and status['runs']==status['planned_runs']==len(rows)and status['retries']==0,'campaign incomplete')
    require(len(set(r['run_id']for r in rows))==len(rows),'duplicate run ID')
    failures=0
    for row,old in zip(rows,original,strict=True):
        require(row['run_id']==old['run_id']and old['source_manifest_sha256']==ix['source_manifest_sha256'],'producer source/run')
        require(all(row[k]==old[k]for k in ['name','experiment','mode','batch']),'producer batch identity differs')
        require(old['parent_run_id']==status['parent_run_id'],'parent run mismatch')
        code=old['producer_command']['exit_code'];v=old['verification_command']
        require(row['producer_exit_code']==code,'portable index producer exit differs')
        require(type(code)is int and code in (0,3)and old['producer_command']['timeout_cleanup']is False,'producer execution incomplete')
        require(v['exit_code']==0 and v['timeout_cleanup']is False,'original validator did not accept')
        binary='pbench'if row['mode']=='P'else'bench'
        require(old['binary_sha256']==f['binaries'][binary]['sha256'],'measurement binary differs')
        d=member(root,row['path']);m=read(d/'manifest.json')
        require(m['run_id']==row['run_id']and m['source_manifest_sha256']==ix['source_manifest_sha256']and m['run_kind']=='formal','raw source/run/formal')
        failures+=code==3
    require(status['status']==('COMPLETED_WITH_FAILURES'if failures else'COMPLETED'),'experiment status contradicts producer exits')
    ms=read(member(root,ix['m6']['freeze'])/'freeze.json')
    require(ms['measurement_source_manifest_sha256']==ix['source_manifest_sha256'],'M6 freeze source differs')
    build=dependency(root,ix['software'],archive)
    require(build['source_manifest_sha256']==ix['source_manifest_sha256']and build['version']==ix['version'],'release source association')
    # Historical index and its entire transitive tree remain original bytes.
    import release_verify as old_gate
    historical=old_gate.validate(member(root,ix['history_root']))
    return ix,{'status':'PASS','current_batches':len(rows),'current_failed_trial_batches':failures,'historical_batches':historical['batches'],'manifest':manifest(root)}

def comparable(rows):
    return [{k:v for k,v in r.items()if k!='result_dir'}for r in rows]

def report_inputs(root,ix,bins,derived):
    """Only remap derived metadata; preserve the original execution status."""
    derived.mkdir()
    f=read(member(root,ix['campaign']['freeze']))
    for n in ['bench','pbench']:f['binaries'][n]['path']=str(bins/(n+'.exe'))
    rows=read(member(root,ix['campaign']['results_index']))
    for old,row in zip(rows,ix['campaign']['batches'],strict=True):old['result_dir']=str(member(root,row['path']))
    (derived/'freeze.json').write_text(json.dumps(f,allow_nan=False),encoding='utf-8')
    (derived/'results_index.json').write_text(json.dumps(rows,allow_nan=False),encoding='utf-8')
    original_status=member(root,ix['campaign']['execution_status'])
    read(original_status) # Same strict JSON contract as all other inputs.
    (derived/'execution_status.json').write_bytes(original_status.read_bytes())

def main():
    ap=argparse.ArgumentParser();ap.add_argument('--root',type=Path,required=True);ap.add_argument('--out',type=Path,required=True);ap.add_argument('--go',required=True);ap.add_argument('--software-archive',type=Path);ap.add_argument('--check-only',action='store_true');a=ap.parse_args()
    root=a.root.resolve();out=a.out.resolve();require(not out.is_relative_to(root)and not root.is_relative_to(out),'output overlaps package');out.mkdir(parents=True,exist_ok=False);commands=[]
    def save(n,x):(out/n).write_text(json.dumps(x,ensure_ascii=False,indent=2)+'\n',encoding='utf-8')
    def run(n,args,cwd,timeout=900):
        d=out/n;env=os.environ.copy();env['PYTHONDONTWRITEBYTECODE']='1';env['GOTOOLCHAIN']='local';env['GO']=a.go
        subprocess.run([sys.executable,cwd/'scripts/run_checked.py','--log-dir',out,'--name',n,'--timeout',str(timeout),'--',*map(str,args)],cwd=cwd,env=env,check=False)
        row={'name':n,**read(d/'command.json')};commands.append(row);save('commands.json',commands)
        require(row['exit_code']==0 and row['timeout_cleanup']is False,'command rejected '+n)
    try:
        ix,check=validate(root,a.software_archive);save('integrity.json',check);src=member(root,ix['source_root'])
        if not a.check_only:
            bins=out/'binaries';bins.mkdir()
            run('build_all',[a.go,'build','./...'],src)
            run('mod_verify',[a.go,'mod','verify'],src)
            run('vet',[a.go,'vet','./...'],src)
            run('friend_recheck',[a.go,'test','-p=1','-json','-count=1','-run=TestFriend|TestRecheck|TestSecctlStatusRealCLI','./...'],src,1200)
            for n in ['bench','pbench']:run('build_'+n,[a.go,'build','-trimpath','-o',bins/(n+'.exe'),'./cmd/'+n],src)
            for row in ix['campaign']['batches']:
                run('current_'+row['name'],[bins/('pbench.exe'if row['mode']=='P'else'bench.exe'),'-verify','-dir',member(root,row['path']),'-exit-code',str(row['producer_exit_code'])],src)
            hist=member(root,ix['history_root']);hi=read(hist/'RELEASE_INDEX.json')
            for row in hi['batches']:run('historical_'+row['id'].replace('/','_'),[bins/('pbench.exe'if row['mode']=='P'else'bench.exe'),'-verify','-dir',member(hist,row['path']),'-exit-code',str(row['producer_exit_code'])],src)
            run('historical_m6',[sys.executable,src/'scripts/m6_verify.py','--root',member(hist,hi['m6']['root']),'--comparison',member(hist,hi['m6']['comparison']),'--out',out/'historical_m6_recomputed.json'],src)
            run('current_m6',[sys.executable,src/'scripts/m6_verify.py','--root',src,'--freeze-dir',member(root,ix['m6']['freeze']),'--comparison',member(root,ix['m6']['comparison']),'--out',out/'m6_recomputed.json'],src)
            # Read-only portable recomputation: map only path metadata in derived
            # copies; original freezes, indices and historical bytes stay intact.
            derived=out/'derived_inputs';report_inputs(root,ix,bins,derived)
            run('current_report_recompute',[sys.executable,src/'scripts/m5_formal_report.py','--freeze',derived/'freeze.json','--data',derived,'--out',out/'r5_recomputed'],src)
            actual=read(out/'r5_recomputed/recomputed_report.json');expected=read(member(root,ix['report']))
            for field in ['handshake','resource','throughput']:require(comparable(actual[field])==comparable(expected[field]),'report numeric mismatch '+field)
        save('verification.json',{'status':'PASS','version':ix['version'],'source_manifest_sha256':ix['source_manifest_sha256'],'scope':'integrity only'if a.check_only else'current/historical raw accepted and M6/R5 recalculated; no new sampling or security certification','checks':check,'commands':commands});print('PASS current portable gate');return 0
    except Exception as e:
        save('verification.json',{'status':'FAIL','reason':str(e),'commands':commands});print('FAIL '+str(e),file=sys.stderr);return 2

if __name__=='__main__':raise SystemExit(main())
