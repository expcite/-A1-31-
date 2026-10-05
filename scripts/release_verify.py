"""Portable strict release gate and read-only historical result acceptance.

Paths in the active index are relative to the REVIEW PACKAGE root. Producer
absolute paths in immutable old records are metadata only, never fallbacks.
"""
from pathlib import Path,PurePosixPath
import argparse,datetime,hashlib,json,os,re,subprocess,sys

def require(ok,msg):
 if not ok:raise ValueError(msg)
def sha(p):return hashlib.sha256(p.read_bytes()).hexdigest()
def pairs(items):
 d={}
 for k,v in items:
  require(k not in d,'DUPLICATE_KEY '+k);d[k]=v
 return d
def read(p):return json.loads(p.read_text(encoding='utf-8'),object_pairs_hook=pairs,parse_constant=lambda x:(_ for _ in ()).throw(ValueError('NONFINITE '+x)))
def path(root,rel):
 require(isinstance(rel,str) and rel and '\\' not in rel and ':' not in rel,'UNSAFE_PATH '+str(rel))
 p=PurePosixPath(rel);require(not p.is_absolute() and '..' not in p.parts and str(p)==rel,'UNSAFE_PATH '+rel)
 dest=root.joinpath(*p.parts);require(dest.resolve().is_relative_to(root.resolve()),'ESCAPING_PATH '+rel)
 require(not any(q.is_symlink() for q in [dest,*dest.parents] if q.is_relative_to(root)),'SYMLINK '+rel)
 return dest
def digest(h):require(isinstance(h,str) and re.fullmatch('[0-9a-f]{64}',h),'INVALID_HASH '+str(h))
def validate(root,index=None):
 index=index or read(root/'RELEASE_INDEX.json')
 require(index['schema']=='edgehybrid-release-index/1' and index['relative_root']=='review package root','SCHEMA_OR_ROOT')
 ids=set();checked=set()
 def unique(k):require(isinstance(k,str) and k and k not in ids,'DUPLICATE_OR_INVALID_ID '+str(k));ids.add(k)
 for tree in index['trees']:
  unique(tree['id']);folder=path(root,tree['path']);require(folder.is_dir(),'MISSING_DEPENDENCY '+tree['path'])
  require(isinstance(tree['files'],dict) and tree['files'],'EMPTY_OR_INVALID_TREE '+tree['id'])
  actual={p.relative_to(folder).as_posix() for p in folder.rglob('*') if p.is_file()}
  require(actual==set(tree['files']),'TREE_MEMBERS_DIFFER '+tree['id'])
  for rel,h in tree['files'].items():
   digest(h);p=path(folder,rel);require(p.is_file() and sha(p)==h,'MISSING_OR_HASH_MISMATCH '+tree['path']+'/'+rel);checked.add(p)
 for row in index['required_files']:
  unique(row['id']);p=path(root,row['path']);digest(row['sha256']);require(p.is_file() and sha(p)==row['sha256'],'MISSING_OR_HASH_MISMATCH '+row['path']);checked.add(p)
 for row in index['requirements']:
  unique('requirement/'+row['requirement_id'])
  for rel in row['source']:require(path(root,rel).exists(),'MISSING_SOURCE '+rel)
  for e in row['evidence']:
   require(e['exists'] is True,'INVALID_EXISTS');p=path(root,e['path']);require(p.exists(),'MISSING_EVIDENCE '+e['path'])
   if p.is_file():digest(e['sha256']);require(sha(p)==e['sha256'],'EVIDENCE_HASH '+e['path'])
   else:require(any(t['id']==e['directory_manifest_tree'] and t['path']==e['path'] for t in index['trees']),'DIRECTORY_NOT_MANIFESTED '+e['path'])
 require(len(index['batches'])==32,'EXPECTED_32_BATCHES')
 for row in index['batches']:
  unique(row['id']);require(row['mode'] in ['T','H','P'],'MODE');require(type(row['producer_exit_code']) is int,'EXIT_TYPE')
  d=path(root,row['path']);m=read(d/'manifest.json');digest(row['produced_by_source_hash'])
  require(m['run_id']==row['run_id'] and m['source_manifest_sha256']==row['produced_by_source_hash'] and m['run_kind']=='formal','PRODUCER_ASSOCIATION '+row['id'])
  require(path(root,row['sampling_source_manifest']).is_file(),'MISSING_DEPENDENCY sampling source')
  require(sha(path(root,row['sampling_source_manifest']))==row['produced_by_source_hash'],'SAMPLING_SOURCE_HASH '+row['id'])
  original=read(path(root,row['original_index']))
  candidates=[v for v in original if v['name']==row['name']]
  require(len(candidates)==1,'AMBIGUOUS_OR_MISSING_BATCH '+row['id']);v=candidates[0]
  require(v['run_id']==row['run_id'] and v['source_manifest_sha256']==row['produced_by_source_hash'] and v['mode']==row['mode'] and v['producer_command']['exit_code']==row['producer_exit_code'],'INDEX_ASSOCIATION '+row['id'])
  old=v['result_dir'].replace('\\','/');require(old.endswith('/'+row['historical_relative_result_dir']),'OLD_TO_PORTABLE_MAPPING '+row['id'])
 require(index['m6']['produced_by_source_hash']=='9d0576f5ea7210752bd32ff6ed549a58fa99356f1ba5bd70a87cfed4c7fe9f8c','M6_SOURCE')
 m6root=path(root,index['m6']['root']);require(m6root.is_dir(),'MISSING_DEPENDENCY M6');comparison=path(root,index['m6']['comparison']);require(comparison.is_dir(),'MISSING_DEPENDENCY M6 comparison')
 return {'status':'PASS','files_checked':len(checked),'batches':len(index['batches']),'trees':len(index['trees'])}
def manifest(root):
 f=root/'MANIFEST.sha256';require(f.is_file(),'MISSING_MANIFEST');entries={}
 for l in f.read_text(encoding='utf-8').splitlines():
  h,rel=l.split('  ',1);digest(h);require(rel not in entries and rel!='MANIFEST.sha256','DUPLICATE_OR_SELF_MANIFEST');entries[rel]=h
 actual={p.relative_to(root).as_posix() for p in root.rglob('*') if p.is_file() and p!=f}
 require(actual==set(entries),'MANIFEST_SET_MISMATCH')
 for rel,h in entries.items():require(sha(path(root,rel))==h,'MANIFEST_HASH_MISMATCH '+rel)
 return {'status':'PASS','files':len(entries),'sha256':sha(f)}
def main():
 ap=argparse.ArgumentParser();ap.add_argument('--root',type=Path,required=True);ap.add_argument('--out',type=Path,required=True);ap.add_argument('--go',default='go');ap.add_argument('--check-only',action='store_true');a=ap.parse_args();root=a.root.resolve();out=a.out.resolve();out.mkdir(parents=True,exist_ok=False);rows=[]
 def persist(name,value):(out/name).write_text(json.dumps(value,ensure_ascii=False,indent=2)+'\n',encoding='utf-8')
 def run(name,args,cwd,timeout=180):
  folder=out/name;folder.mkdir();start=datetime.datetime.now(datetime.timezone.utc).isoformat();env=os.environ.copy();env['GOTOOLCHAIN']='local';env['PATH']=str(Path(a.go).resolve().parent)+os.pathsep+env.get('PATH','')
  with(folder/'stdout.txt').open('wb')as so,(folder/'stderr.txt').open('wb')as se:
   try:result=subprocess.run(list(map(str,args)),cwd=cwd,env=env,stdout=so,stderr=se,timeout=timeout);rc=result.returncode
   except subprocess.TimeoutExpired:rc=None
  row={'name':name,'command':list(map(str,args)),'cwd':str(cwd),'exit_code':rc,'started_utc':start,'finished_utc':datetime.datetime.now(datetime.timezone.utc).isoformat(),'status':'PASS' if rc==0 else 'FAIL'};rows.append(row);(folder/'command.json').write_text(json.dumps(row,indent=2),encoding='utf-8');persist('commands.json',rows);require(rc==0,'COMMAND_REJECTED '+name);return folder
 try:
  if(root/'MANIFEST.sha256').exists():persist('manifest_validation.json',manifest(root))
  persist('index_validation.json',validate(root));index=read(root/'RELEASE_INDEX.json')
  source=path(root,index['current_source_root']);source_manifest=source/'CURRENT_SOURCE_MANIFEST.sha256';require(source_manifest.is_file(),'MISSING_CURRENT_SOURCE_MANIFEST')
  require(read(source/'docs/FINAL_DELIVERY_INDEX.json')==index,'ACTIVE_INDEX_DIFFERS')
  for md in [source/'README.md',source/'docs/user/USER_MANUAL.md']:
   for rel in re.findall(r'\]\(([^)]+)\)',md.read_text(encoding='utf-8')):
    if rel.startswith(('http:','https:','#')):continue
    target=(md.parent/rel.split('#')[0]).resolve();require(target.is_relative_to(root) and target.is_file(),'DEAD_OR_EXTERNAL_ACTIVE_LINK '+str(md)+' '+rel)
  sys.path.insert(0,str(source/'scripts'));import source_manifest as sm
  content=''.join(sm.sha256_file(p)+'  '+Path(p).relative_to(source).as_posix()+'\n'for p in sm.collect(str(source))).encode()
  require(content==source_manifest.read_bytes(),'CURRENT_SOURCE_MISMATCH');current_hash=sha(source_manifest);build=read(root/'build_info.json');require(build['source_manifest_sha256']==current_hash and build['version']==index['version'],'BUILD_SOURCE_VERSION_MISMATCH')
  if not a.check_only:
   run('build_all',[a.go,'build','./...'],source)
   run('go_mod_verify',[a.go,'mod','verify'],source)
   run('go_vet',[a.go,'vet','./...'],source)
   run('friend_recheck',[a.go,'test','-json','-count=1','-run=TestFriend|TestRecheck|TestSecctlStatusRealCLI','./...'],source,600)
   bins=out/'binaries';bins.mkdir()
   for n in ['bench','pbench']:run('build_'+n,[a.go,'build','-trimpath','-o',bins/(n+'.exe'),'./cmd/'+n],source)
   for batch in index['batches']:
    run('verify_'+batch['id'].replace('/','_'),[bins/('pbench.exe' if batch['mode']=='P' else 'bench.exe'),'-verify','-dir',path(root,batch['path']),'-exit-code',str(batch['producer_exit_code'])],source,180)
   run('m6_offline_recompute',[sys.executable,source/'scripts/m6_verify.py','--root',path(root,index['m6']['root']),'--comparison',path(root,index['m6']['comparison']),'--out',out/'m6_recomputed.json'],source)
  persist('verification.json',{'status':'PASS','version':index['version'],'source_manifest_sha256':current_hash,'offline_formal_batches_accepted':32 if not a.check_only else 0,'m6_recomputed':not a.check_only,'scope':'Strict self-contained release checks and read-only acceptance; historical samples are not new performance measurements; no security certification','commands':rows});print('PASS portable release gate'+(' (integrity only)' if a.check_only else ': 32 historical batches and M6 accepted'));return 0
 except Exception as e:
  persist('verification.json',{'status':'FAIL','reason':str(e),'commands':rows});print('FAIL '+str(e),file=sys.stderr);return 1
if __name__=='__main__':sys.exit(main())
