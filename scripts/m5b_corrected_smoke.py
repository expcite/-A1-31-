#!/usr/bin/env python3
"""Native smoke and real CLI validation; keep each original result immutable.

Build binaries from the current tree first. Test keys exist only in a fresh
temporary directory. Mutation copies contain public evidence, never runtime.
"""
import argparse
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument('--bin-dir', required=True)
    ap.add_argument('--out', required=True)
    ap.add_argument('--source-hash', required=True)
    a = ap.parse_args()
    root = Path(__file__).resolve().parents[1]
    out = Path(a.out).resolve()
    out.mkdir(parents=True, exist_ok=False)
    bins = Path(a.bin_dir).resolve()
    rows = []

    def command(name, cmd, expected=0):
        log = out / 'commands'
        wrapper = [sys.executable, str(root / 'scripts/run_checked.py'), '--log-dir', str(log),
                   '--name', name, '--timeout', '240', '--'] + [str(x) for x in cmd]
        subprocess.run(wrapper, cwd=root, check=False)
        code = int((log / name / 'exit.txt').read_text())
        rows.append(dict(name=name, command=[str(x) for x in cmd], exit_code=code, expected_exit=expected))
        (out / 'command_index.json').write_text(json.dumps(rows, indent=2), encoding='utf-8')
        if code != expected:
            raise RuntimeError(name + ': exit ' + str(code) + ', expected ' + str(expected))

    results = []
    try:
        for kind in ['resource', 'throughput']:
            for mode in ['traditional', 'hybrid']:
                name = kind + '_' + mode
                parent = out / name
                parent.mkdir()
                cmd = [bins / 'bench.exe', '-experiment', kind, '-mode', mode, '-run-kind', 'smoke',
                       '-out', parent, '-gateway-bin', bins / 'gateway.exe', '-timeout', '30s',
                       '-trials', '10', '-rounds', '3', '-per-round', '200', '-run-id', 'corrected-' + name,
                       '-source-manifest-sha256', a.source_hash]
                command(name, cmd)
                dirs = list(parent.iterdir())
                if len(dirs) != 1:
                    raise RuntimeError('result directory is ambiguous')
                results.append((name, bins / 'bench.exe', dirs[0]))
        with tempfile.TemporaryDirectory(prefix='edgehybrid-corrected-p-') as tmp:
            for kind in ['handshake', 'throughput', 'rejections']:
                name = 'p_' + kind
                result = out / name
                command(name, [bins / 'pbench.exe', '-experiment', kind, '-out', result,
                               '-certs-dir', Path(tmp) / 'certs', '-trials', '10', '-rounds', '3',
                               '-per-round', '200', '-timeout', '30s', '-run-id', 'corrected-' + name,
                               '-source-manifest-sha256', a.source_hash])
                if kind != 'rejections':
                    results.append((name, bins / 'pbench.exe', result))

        def verify(name, binary, result, expected=0):
            command(name, [binary, '-verify', '-dir', result, '-exit-code', '0'], expected)

        for name, binary, result in results:
            verify('verify_' + name, binary, result)
            manifest = json.loads((result / 'manifest.json').read_text(encoding='utf-8'))
            if manifest['source_manifest_sha256'] != a.source_hash:
                raise RuntimeError('source hash mismatch')
            validation = json.loads((result / 'validation.json').read_text(encoding='utf-8'))
            if validation['problems'] or not validation['acceptance_passed']:
                raise RuntimeError('producer validation disagrees with CLI')
            mutations = ['missing_status', 'bad_json', 'wrong_run', 'forged_summary']
            if name.startswith('resource'):
                mutations += ['missing_trials']
                summary_file, raw_file, events_file = 'resource_summary.json', 'resource_trials.jsonl', 'gateway_events.jsonl'
            elif name == 'p_handshake':
                mutations += ['duplicate_id', 'delete_confirmed', 'wrong_tls', 'empty_trials']
                summary_file, raw_file, events_file = 'p_summary.json', 'p_trials.jsonl', 'p_gateway_events.jsonl'
            else:
                mutations += ['duplicate_id', 'delete_confirmed']
                summary_file, raw_file = 'throughput_summary.json', 'throughput_rounds.jsonl'
                if name.startswith('p_'):
                    summary_file, raw_file = 'p_throughput_summary.json', 'p_throughput_rounds.jsonl'
                events_file = 'p_gateway_events.jsonl' if name.startswith('p_') else 'gateway_events.jsonl'
            for mutation in mutations:
                copy = out / 'mutations' / (name + '_' + mutation)
                shutil.copytree(result, copy)
                if mutation == 'missing_status':
                    (copy / 'status.json').unlink()
                elif mutation == 'missing_trials':
                    (copy / 'device/device_trials.jsonl').unlink()
                elif mutation == 'empty_trials':
                    (copy / raw_file).write_text('', encoding='utf-8')
                elif mutation == 'bad_json':
                    (copy / raw_file).write_text('{bad json}\n', encoding='utf-8')
                elif mutation == 'wrong_run':
                    m = json.loads((copy / 'manifest.json').read_text(encoding='utf-8'))
                    m['run_id'] = 'unrelated-run'
                    (copy / 'manifest.json').write_text(json.dumps(m), encoding='utf-8')
                elif mutation == 'forged_summary':
                    s = json.loads((copy / summary_file).read_text(encoding='utf-8'))
                    field = 'roles' if name.startswith('resource') else 'successes' if name == 'p_handshake' else 'total_completed'
                    s[field] += 1
                    (copy / summary_file).write_text(json.dumps(s), encoding='utf-8')
                elif mutation == 'duplicate_id':
                    lines = (copy / raw_file).read_text(encoding='utf-8').splitlines()
                    lines[1] = lines[0]
                    (copy / raw_file).write_text('\n'.join(lines) + '\n', encoding='utf-8')
                elif mutation == 'delete_confirmed':
                    lines = (copy / events_file).read_text(encoding='utf-8').splitlines()
                    lines = [x for x in lines if json.loads(x).get('outcome') != 'control-confirmed']
                    (copy / events_file).write_text('\n'.join(lines) + '\n', encoding='utf-8')
                elif mutation == 'wrong_tls':
                    lines = (copy / raw_file).read_text(encoding='utf-8').splitlines()
                    trial = json.loads(lines[0])
                    trial['tls']['did_resume'] = True
                    lines[0] = json.dumps(trial)
                    (copy / raw_file).write_text('\n'.join(lines) + '\n', encoding='utf-8')
                # The immutable producer verdict belongs to the unmodified
                # original only. Publish a distinct mutation verification.
                (copy / 'validation.json').unlink()
                verify('reject_' + name + '_' + mutation, binary, copy, 5)
        (out / 'results_index.json').write_text(json.dumps(
            [dict(name=n, binary=str(b), dir=str(d)) for n, b, d in results], indent=2), encoding='utf-8')
        print('M5B smoke and CLI rejection controls PASS')
        return 0
    except Exception as e:
        (out / 'failure.txt').write_text(str(e), encoding='utf-8')
        print(str(e), file=sys.stderr)
        return 1


if __name__ == '__main__':
    sys.exit(main())
