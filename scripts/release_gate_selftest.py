"""Counterexamples against the real final staging index; never edits originals."""
import argparse,copy,json,pathlib,shutil,tempfile,subprocess,sys
import release_verify as gate
def main():
 ap=argparse.ArgumentParser();ap.add_argument('--root',type=pathlib.Path,required=True);ap.add_argument('--out',type=pathlib.Path,required=True);a=ap.parse_args();root=a.root.resolve();out=a.out.resolve();out.mkdir(parents=True,exist_ok=False);index=gate.read(root/'RELEASE_INDEX.json');results=[]
 def expected(name,fn,reason):
  try:fn()
  except (ValueError,KeyError,TypeError) as e:
   assert reason in str(e),(name,str(e));results.append({'name':name,'expected_rejection':reason,'observed':str(e),'status':'PASS'});return
  raise AssertionError('incorrectly accepted '+name)
 results.append({'name':'normal_complete_staging','result':gate.validate(root),'status':'PASS'})
 expected('duplicate_json_key',lambda:json.loads('{"x":1,"x":2}',object_pairs_hook=gate.pairs),'DUPLICATE_KEY')
 for bad in ['/tmp/file','C:/author/path','../escape','a/../file','a\\b']:
  expected('unsafe_'+bad,lambda:gate.path(root,bad),'UNSAFE_PATH')
 x=copy.deepcopy(index);x['trees'][1]['id']=x['trees'][0]['id'];expected('duplicate_tree_id',lambda:gate.validate(root,x),'DUPLICATE_OR_INVALID_ID')
 x=copy.deepcopy(index);x['batches'][0]['producer_exit_code']='0';expected('invalid_exit_type',lambda:gate.validate(root,x),'EXIT_TYPE')
 # Hardlinks produce an independent directory with exact bytes without duplicating
 # huge raw logs. Deleted entries and replaced files affect only this copy;
 # truncating/writing an existing hardlink is deliberately forbidden.
 with tempfile.TemporaryDirectory(prefix='edgehybrid-negative-',dir=out) as temp:
  clone=pathlib.Path(temp)/'review';shutil.copytree(root,clone,copy_function=lambda s,d:os_link(s,d))
  raw=next(p for p in (clone/index['batches'][0]['path']).glob('*trials.jsonl'))
  rel=raw.relative_to(clone);saved=raw.read_bytes();raw.unlink();expected('deleted_real_raw',lambda:gate.validate(clone),'TREE_MEMBERS_DIFFER');raw.write_bytes(saved)
  original=raw.read_bytes();raw.unlink();raw.write_bytes(original+b' ');expected('one_byte_raw_tamper',lambda:gate.validate(clone),'MISSING_OR_HASH_MISMATCH');raw.unlink();raw.write_bytes(original)
  dep=clone/index['m6']['root'];moved=dep.with_name(dep.name+'_removed');dep.rename(moved);expected('missing_m6_dependency',lambda:gate.validate(clone),'MISSING_DEPENDENCY');moved.rename(dep)
  assert gate.validate(clone)['status']=='PASS';assert gate.validate(root)['status']=='PASS'
 (out/'results.json').write_text(json.dumps({'status':'PASS','cases':results,'scope':'release dependency/hash counterexamples; not protocol attacks'},indent=2),encoding='utf-8');print(json.dumps({'status':'PASS','cases':len(results)}));return 0
def os_link(s,d):
 import os
 os.link(s,d);return d
if __name__=='__main__':sys.exit(main())
