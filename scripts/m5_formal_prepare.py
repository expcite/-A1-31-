#!/usr/bin/env python3
"""Regression and source/binary freeze. Never executes the formal matrix."""
import argparse
import datetime
import hashlib
import json
import os
import re
from pathlib import Path
import subprocess
import sys
import source_manifest

GO = Path(os.environ.get('GO', r'C:\Users\20477\.workbuddy\binaries\go\go\bin\go.exe'))

def sha(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()

def write(path, obj):
    path.write_text(json.dumps(obj, ensure_ascii=False, indent=2), encoding='utf-8')

def main():
    ap = argparse.ArgumentParser()
    ap.add_argument('--out', type=Path, required=True)
    ap.add_argument('--bin-dir', type=Path, required=True)
    ap.add_argument('--campaign-id', required=True, help='pre-registered ID for this NEW campaign')
    ap.add_argument('--demo-zip', type=Path, required=True, help='preserved historical Demo ZIP, not a new executable')
    ap.add_argument('--demo-sha256', required=True, help='expected hash from preserved release evidence')
    ap.add_argument('--historical-smoke-root', type=Path, required=True)
    ap.add_argument('--baseline-source-manifest-sha256', help='actual supplied input baseline; omit only when unknown')
    a = ap.parse_args()
    root = Path(__file__).resolve().parents[1]
    out = a.out.resolve()
    out.mkdir(parents=True, exist_ok=False)
    bins = a.bin_dir.resolve()
    bins.mkdir(parents=True, exist_ok=False)
    logs = out / 'commands'
    rows = []

    def command(name, cmd, timeout=300):
        p = subprocess.run([sys.executable, root / 'scripts/run_checked.py', '--log-dir', logs,
                            '--name', name, '--timeout', str(timeout), '--', *map(str, cmd)], cwd=root)
        info = json.loads((logs / name / 'command.json').read_text(encoding='utf-8'))
        rows.append(dict(name=name, **info))
        write(out / 'command_index.json', rows)
        if p.returncode != 0 or info['exit_code'] != 0:
            raise RuntimeError(name + ' failed; original logs preserved')
        return logs / name

    try:
        if a.baseline_source_manifest_sha256 is not None and not re.fullmatch('[0-9a-f]{64}',a.baseline_source_manifest_sha256):
            raise ValueError('baseline source hash must be lowercase SHA-256 or omitted as unknown')
        if not a.campaign_id.strip() or len(a.campaign_id) > 160:
            raise ValueError('invalid campaign ID')
        if sha(a.demo_zip.resolve()) != a.demo_sha256.lower():
            raise ValueError('preserved historical Demo does not match its expected hash')
        campaign_plan_hash = sha(root / 'docs/BACKEND_FORMAL_EXPERIMENT_PLAN.md')
        command('candidate_manifest', [sys.executable, root / 'scripts/source_manifest.py', '--out', out / 'candidate.sha256', '--json', out / 'candidate.json'])
        source_hash = sha(out / 'candidate.sha256')
        command('mod_verify', [GO, 'mod', 'verify'])
        command('vet', [GO, 'vet', './...'])
        command('build_all_commands', [GO, 'build', '-buildvcs=false', '-o', str(bins) + os.sep, './cmd/...'])
        test = command('go_test_json', [GO, 'test', '-count=1', '-json', './...'])
        packages = command('packages', [GO, 'list', './...'])
        expected = (packages / 'stdout.txt').read_text(encoding='utf-8').splitlines()
        args = [sys.executable, root / 'scripts/summarize-go-test-json.py', test / 'stdout.txt', '--go-exit-code', '0', '--require-tests', '--out', out / 'test_summary.txt', '--json', out / 'test_summary.json']
        for package in expected:
            args += ['--expect-package', package]
        command('test_log_acceptance', args)
        for name in ['summarize_selftest', 'fleet_verifier_selftest', 'bench_result_selftest', 'm4case_extractor_selftest']:
            command(name, [sys.executable, root / ('scripts/' + name + '.py')])
        for name, mode, count, bad in [('e2e_t', 'traditional', 1, ''), ('e2e_h', 'hybrid', 1, ''), ('fleet_20', 'hybrid', 20, ''), ('fleet_19_plus_1', 'hybrid', 20, 'D020')]:
            args = [sys.executable, root / 'scripts/windows_integration.py', '--mode', mode, '--devices', str(count), '--samples', '3', '--bin-dir', bins, '--out', out / name, '--source-hash', source_hash]
            if bad:
                args += ['--bad-device', bad]
            command(name, args, 180)
        command('current_smoke', [sys.executable, root / 'scripts/m5b_corrected_smoke.py', '--bin-dir', bins, '--out', out / 'smoke_data', '--source-hash', source_hash], 600)
        command('historical_smoke_offline', [sys.executable, root / 'scripts/demo_m5b_regression.py', '--bin-dir', bins, '--data-root', a.historical_smoke_root.resolve(), '--out', out / 'historical_smoke_offline'], 300)
        command('final_manifest', [sys.executable, root / 'scripts/source_manifest.py', '--out', out / 'source_manifest.sha256', '--json', out / 'source_info.json'])
        if sha(out / 'source_manifest.sha256') != source_hash:
            raise RuntimeError('source changed during regression; no final freeze published')
        binaries = {}
        for name in ['bench', 'pbench', 'gateway', 'device', 'fleet', 'secctl']:
            path = bins / (name + '.exe')
            meta = command('build_metadata_' + name, [GO, 'version', '-m', path])
            binaries[name] = dict(path=str(path), sha256=sha(path), metadata=str(meta / 'stdout.txt'))
        command('go_version', [GO, 'version'])
        command('go_env', [GO, 'env', 'GOOS', 'GOARCH', 'GOVERSION', 'GOMAXPROCS', 'CGO_ENABLED', 'GOTOOLCHAIN', 'GOSUMDB'])
        command('modules', [GO, 'list', '-m', 'all'])
        write(out / 'freeze.json', dict(status='FROZEN', frozen_at_utc=datetime.datetime.now(datetime.timezone.utc).isoformat(), root=str(root),
              version=(root / 'VERSION').read_text(encoding='utf-8').strip(), plan_id='m5-formal-20261001-v1', plan_sha256=sha(root / 'docs/FORMAL_EXPERIMENT_PLAN.md'), source_manifest_sha256=source_hash,
              source_manifest=str(out / 'source_manifest.sha256'), binaries=binaries, regression_passed=True,
              regression_scope='commands in this preparation only; no claim of Windows race or S1/S2 confirmation',
              regression_command_index_sha256=sha(out / 'command_index.json'),
              campaign_plan_id=a.campaign_id, campaign_plan_sha256=campaign_plan_hash,
              baseline_source_manifest_sha256=a.baseline_source_manifest_sha256,
              historical_contract_source_manifest_sha256='f79964fff00733e40c4b58b1eb1eb65b60928c09afb8736a81fe467bd3ce8db8',
              demo_zip_path=str(a.demo_zip.resolve()), demo_zip_sha256=a.demo_sha256.lower()))
        print('Regression PASS; source and binaries FROZEN. Formal sampling has not started.', flush=True)
        return 0
    except Exception as e:
        write(out / 'preparation_failure.json', dict(status='FAIL', error=str(e), formal_sampling_started=False))
        print(str(e), file=sys.stderr)
        return 1

if __name__ == '__main__':
    raise SystemExit(main())
