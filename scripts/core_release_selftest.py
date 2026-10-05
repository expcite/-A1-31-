"""Guard fixtures only; no mocked product success or protocol execution."""
from pathlib import Path
import json,tempfile,unittest,zipfile
import core_release_verify as gate

class Guards(unittest.TestCase):
    def setUp(self):
        self.tmp=tempfile.TemporaryDirectory();self.addCleanup(self.tmp.cleanup);self.root=Path(self.tmp.name)
    def test_relative_paths_accept_normal(self):self.assertEqual(gate.member(self.root,'a/b'),self.root/'a/b')
    def test_path_escape_rejected(self):
        for p in ['../x','C:/x','/x','a\\b','a/../x']:
            with self.assertRaises(ValueError):gate.member(self.root,p)
    def test_duplicate_json_rejected(self):
        p=self.root/'x';p.write_text('{"k":1,"k":2}')
        with self.assertRaises(ValueError):gate.read(p)
    def test_nonfinite_rejected(self):
        p=self.root/'x';p.write_text('{"k":NaN}')
        with self.assertRaises(ValueError):gate.read(p)
    def test_software_json_uses_same_strict_parser(self):
        with self.assertRaises(ValueError):gate.strict_json('{"source":"one","source":"two"}')
    def test_normal_exact_manifest(self):
        p=self.root/'raw';p.write_bytes(b'public fixture');(self.root/'MANIFEST.sha256').write_text(gate.sha(p)+'  raw\n');self.assertEqual(gate.manifest(self.root)['files'],1)
    def test_one_byte_tamper_rejected(self):
        p=self.root/'raw';p.write_bytes(b'public fixture');(self.root/'MANIFEST.sha256').write_text(gate.sha(p)+'  raw\n');p.write_bytes(b'public fixturE')
        with self.assertRaises(ValueError):gate.manifest(self.root)
    def test_missing_raw_rejected(self):
        (self.root/'MANIFEST.sha256').write_text('a'*64+'  raw\n')
        with self.assertRaises(ValueError):gate.manifest(self.root)
    def test_missing_software_dependency_rejected(self):
        with self.assertRaises(ValueError):gate.dependency(self.root,{'archive':'missing.zip','sha256':'a'*64},None)
    def test_archive_hash_rejected(self):
        p=self.root/'wrong.zip';p.write_bytes(b'not an archive')
        with self.assertRaises(ValueError):gate.dependency(self.root,{'archive':'wrong.zip','sha256':'a'*64},p)
    def test_portable_report_keeps_execution_status_and_original_inputs(self):
        data={'freeze.json':{'binaries':{'bench':{'path':'OLD/bench.exe'},'pbench':{'path':'OLD/pbench.exe'}}},'results_index.json':[{'result_dir':'OLD/raw'}],'execution_status.json':{'status':'COMPLETED_WITH_FAILURES','retries':0}}
        for n,v in data.items():(self.root/n).write_text(json.dumps(v),encoding='utf-8')
        before={n:gate.sha(self.root/n)for n in data}
        ix={'campaign':{'freeze':'freeze.json','results_index':'results_index.json','execution_status':'execution_status.json','batches':[{'path':'raw'}]}}
        derived=self.root/'derived';gate.report_inputs(self.root,ix,self.root/'bins',derived)
        self.assertEqual(gate.read(derived/'execution_status.json'),data['execution_status.json'])
        self.assertEqual(gate.read(derived/'results_index.json')[0]['result_dir'],str(self.root/'raw'))
        self.assertEqual(before,{n:gate.sha(self.root/n)for n in data})
    def test_portable_report_missing_status_is_rejected(self):
        (self.root/'freeze.json').write_text('{"binaries":{"bench":{},"pbench":{}}}')
        (self.root/'results_index.json').write_text('[]')
        ix={'campaign':{'freeze':'freeze.json','results_index':'results_index.json','execution_status':'missing-status.json','batches':[]}}
        with self.assertRaises(FileNotFoundError):gate.report_inputs(self.root,ix,self.root/'bins',self.root/'derived')

if __name__=='__main__':unittest.main(verbosity=2)
