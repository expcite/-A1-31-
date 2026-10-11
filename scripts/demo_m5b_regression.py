#!/usr/bin/env python3
"""Re-run only existing M5B offline validators; no new performance runs."""
import argparse
import json
from pathlib import Path, PureWindowsPath
import subprocess


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument('--bin-dir', type=Path, required=True)
    ap.add_argument('--out', type=Path, required=True)
    ap.add_argument('--data-root', type=Path, help='immutable historical smoke_r4_data directory')
    a = ap.parse_args()
    root = Path(__file__).resolve().parents[1]
    controls = (a.data_root or root / 'results/raw/m5b_corrected_20260930/smoke_r4_data').resolve()
    a.out.mkdir(parents=True, exist_ok=False)
    index = json.loads((controls / 'command_index.json').read_text(encoding='utf-8'))
    results = []
    for old in index:
        if '-verify' not in old['command']:
            continue
        original = PureWindowsPath(old['command'][old['command'].index('-dir') + 1])
        parts = original.parts
        relative = Path(*parts[parts.index('smoke_r4_data') + 1:])
        directory = (controls / relative).resolve()
        if not directory.is_relative_to(controls.resolve()):
            raise RuntimeError('control directory outside fixed public controls')
        exe = 'pbench.exe' if old['name'].startswith(('verify_p_', 'reject_p_')) else 'bench.exe'
        cmd = [str(a.bin_dir.resolve() / exe), '-verify', '-dir', str(directory), '-exit-code', '0']
        case = a.out / old['name']
        case.mkdir()
        with (case / 'stdout.txt').open('wb') as stdout, (case / 'stderr.txt').open('wb') as stderr:
            p = subprocess.run(cmd, cwd=root, stdout=stdout, stderr=stderr, timeout=20)
        row = dict(name=old['name'], command=cmd, exit_code=p.returncode,
                   expected_exit=old['expected_exit'], input_kind='historical public offline control',
                   passed=p.returncode == old['expected_exit'])
        (case / 'command.json').write_text(json.dumps(row, ensure_ascii=False, indent=2), encoding='utf-8')
        (case / 'exit.txt').write_text(str(p.returncode) + '\n', encoding='ascii')
        results.append(row)
    result = dict(status='PASS' if results and all(x['passed'] for x in results) else 'FAIL',
                  positive_controls=sum(x['expected_exit'] == 0 for x in results),
                  rejection_controls=sum(x['expected_exit'] == 5 for x in results),
                  new_performance_experiments=False, cases=results)
    (a.out / 'regression.json').write_text(json.dumps(result, ensure_ascii=False, indent=2), encoding='utf-8')
    print(json.dumps({k: v for k, v in result.items() if k != 'cases'}))
    return 0 if result['status'] == 'PASS' else 1


if __name__ == '__main__':
    raise SystemExit(main())
