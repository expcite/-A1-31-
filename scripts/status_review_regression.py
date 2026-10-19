"""F09 ordinary regression with immutable evidence; no performance sampling.

Uses only existing local tools. No race/toolchain install, frontend edit, global
encoding change, formal sampling or long-run campaign is performed here.
"""
import argparse
import datetime
import hashlib
import json
import os
from pathlib import Path
import subprocess
import sys

import source_manifest

ROOT = Path(__file__).resolve().parents[1]
GO = Path(os.environ.get('GO', r'C:\Users\[REDACTED_USER]\.local_workspace\binaries\go\go\bin\go.exe'))


def manifest(root):
    return ''.join(source_manifest.sha256_file(p) + '  ' + Path(p).relative_to(root).as_posix() + '\n'
                   for p in source_manifest.collect(str(root))).encode('utf-8')


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument('--out', required=True, type=Path)
    ap.add_argument('--node', required=True, type=Path)
    args = ap.parse_args()
    out = args.out.resolve()
    out.mkdir(parents=True, exist_ok=False)
    initial = manifest(ROOT)
    source_hash = hashlib.sha256(initial).hexdigest()
    (out / 'source_manifest.sha256').write_bytes(initial)
    env = os.environ.copy()
    env.update({
        'EDGEHYBRID_BC_JAR': r'C:\Program Files\JetBrains\PyCharm 2025.2.3\lib\bouncy-castle.jar',
        'EDGEHYBRID_BC_SHA256': 'cefef94b35315f4b94c2653a99e38c94ca8b312a59dd61b0d5e956839fa858b9',
        'EDGEHYBRID_JAVA': r'C:\Program Files\JetBrains\PyCharm 2025.2.3\jbr\bin\java.exe',
        'EDGEHYBRID_JAVAC': r'C:\Program Files\JetBrains\PyCharm 2025.2.3\jbr\bin\javac.exe',
        'EDGEHYBRID_OPENSSL': r'C:\Users\[REDACTED_USER]\.local_workspace\binaries\PortableGit\versions\1.2.0\usr\bin\openssl.exe',
    })
    rows = []

    def save(name, value):
        (out / name).write_text(json.dumps(value, ensure_ascii=False, indent=2) + '\n', encoding='utf-8')

    def run(name, command, timeout=600):
        print('START ' + name, flush=True)
        rc = subprocess.run([sys.executable, ROOT / 'scripts/run_checked.py', '--log-dir', out / 'commands',
                             '--name', name, '--timeout', str(timeout), '--', *map(str, command)],
                            cwd=ROOT, env=env, check=False)
        log = out / 'commands' / name
        record = json.loads((log / 'command.json').read_text(encoding='utf-8'))
        stable = manifest(ROOT) == initial
        row = dict(name=name, source_manifest_sha256=source_hash, source_unchanged=stable,
                   status='PASS' if record['exit_code'] == 0 and not record['timeout_cleanup'] and stable else 'FAIL',
                   driver_exit_code=rc.returncode, **record)
        rows.append(row)
        save('command_index.json', rows)
        return log, record

    run('mod_verify', [GO, 'mod', 'verify'])
    run('vet', [GO, 'vet', './...'])
    run('build_all', [GO, 'build', './...'])
    run('status_target', [GO, 'test', '-json', '-count=1', '-run=^TestSecctlStatusRealCLI', './cmd/secctl'])
    log, _ = run('packages', [GO, 'list', './...'])
    packages = (log / 'stdout.txt').read_text(encoding='utf-8').splitlines()
    log, record = run('go_test', [GO, 'test', '-count=1', '-json', './...'], 900)
    command = [sys.executable, ROOT / 'scripts/summarize-go-test-json.py', log / 'stdout.txt',
               '--go-exit-code', str(record['exit_code']), '--require-tests',
               '--out', out / 'go_summary.txt', '--json', out / 'go_summary.json']
    for pkg in packages:
        command.extend(['--expect-package', pkg])
    run('go_json_acceptance', command)
    for name in ['summarize_selftest', 'fleet_verifier_selftest', 'bench_result_selftest', 'm4case_extractor_selftest']:
        run(name, [sys.executable, ROOT / ('scripts/' + name + '.py')])
    run('frontend_existing_tests', [args.node, ROOT / 'scripts/interactive_frontend_test.mjs'])
    run('go_version', [GO, 'version'])
    run('go_env', [GO, 'env', 'GOOS', 'GOARCH', 'GOVERSION', 'CGO_ENABLED', 'CC', 'GOTOOLCHAIN', 'GOSUMDB'])
    stable = manifest(ROOT) == initial
    result = {
        'status': 'PASS' if stable and all(x['status'] == 'PASS' for x in rows) else 'FAIL',
        'source_manifest_sha256': source_hash, 'source_unchanged': stable, 'commands': len(rows),
        'new_race': 'NOT_EXECUTED; historical native Windows record retains source21a723...',
        'new_formal1000': 'NOT_EXECUTED', 'new_live1800': 'NOT_EXECUTED',
        'external_component_tests': 'Only tests in ordinary Go regression; existing BC/Java/OpenSSL, no installation',
        'finished_utc': datetime.datetime.now(datetime.timezone.utc).isoformat(),
    }
    save('REGRESSION_STATUS.json', result)
    return 0 if result['status'] == 'PASS' else 1


if __name__ == '__main__':
    sys.exit(main())
