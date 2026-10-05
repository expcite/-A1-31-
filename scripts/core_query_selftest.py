"""Explicit subprocess fixtures for fail-closed orchestration, not product results."""
import unittest
from unittest.mock import patch
from types import SimpleNamespace
import m5_formal_run as runner

class Query(unittest.TestCase):
    def call(self,code,out,err=b''):
        with patch.object(runner.subprocess,'run',return_value=SimpleNamespace(returncode=code,stdout=out,stderr=err))as run:
            result=runner.environment()
            self.assertIn("$ErrorActionPreference='Stop'",run.call_args.args[0][-1])
            return result
    def test_actual_empty_fixture_is_positive(self):self.assertEqual(self.call(0,b'{"conflicts":[],"background":[]}')['conflicts'],[])
    def test_nonzero_never_means_no_conflicts(self):
        with self.assertRaises(RuntimeError):self.call(1,b'{"conflicts":[],"background":[]}',b'error')
    def test_malformed_output_rejected(self):
        for out in [b'',b'null',b'{}',b'[]',b'\xff']:
            with self.assertRaises(RuntimeError):self.call(0,out)
    def test_conflict_preserved(self):self.assertEqual(len(self.call(0,b'{"conflicts":[{"Name":"bench.exe"}],"background":[]}')['conflicts']),1)

if __name__=='__main__':unittest.main(verbosity=2)
