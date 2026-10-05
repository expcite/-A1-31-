#!/usr/bin/env python3
"""Execute the frozen 16-run matrix serially. No retries or denominator edits."""
import argparse
import datetime
import hashlib
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import source_manifest

def sha(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()

def write(path, obj):
    path.write_text(json.dumps(obj, ensure_ascii=False, indent=2), encoding='utf-8')

def current_source(root):
    lines = ''.join(source_manifest.sha256_file(p) + '  ' + Path(p).relative_to(root).as_posix() + '\n' for p in source_manifest.collect(str(root)))
    return hashlib.sha256(lines.encode('utf-8')).hexdigest()

def environment():
    # Read-only OS snapshot. No command lines, environment secrets or key files.
    code = r"$ErrorActionPreference='Stop'; [Console]::OutputEncoding=[System.Text.UTF8Encoding]::new($false); $names=@('EdgeHybridDemo.exe','demo.exe','bench.exe','pbench.exe','gateway.exe','device.exe','fleet.exe','secctl.exe','scenario.exe','EdgeHybridInteractive.exe','console.exe','go.exe'); $all=Get-Process; $conflicts=Get-CimInstance Win32_Process | Where-Object { $_.Name -in $names -or $_.Name -like '*.test.exe' } | Select-Object Name,ProcessId,ExecutablePath; $cpu=Get-CimInstance Win32_Processor | Select-Object Name,NumberOfCores,NumberOfLogicalProcessors,LoadPercentage; $os=Get-CimInstance Win32_OperatingSystem | Select-Object Caption,Version,BuildNumber,TotalVisibleMemorySize,FreePhysicalMemory; [pscustomobject]@{conflicts=@($conflicts);cpu=@($cpu);os=$os;power=(powercfg /getactivescheme | Out-String).Trim();background=@($all | Select-Object Name,Id,CPU,WorkingSet64)} | ConvertTo-Json -Depth 5 -Compress"
    p = subprocess.run(['powershell', '-NoProfile', '-Command', code], stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=40)
    # PowerShell is configured only for this child. Strictly keep malformed
    # output and failed enumeration as failures, never as an empty process list.
    if p.returncode:
        raise RuntimeError('OS background snapshot failed; exit=' + str(p.returncode) +
                           '; stderr_bytes_hex=' + p.stderr.hex())
    try:
        text=p.stdout.decode('utf-8-sig', errors='strict')
        data=json.loads(text)
    except (UnicodeError,ValueError) as error:
        raise RuntimeError('OS snapshot malformed; stdout_bytes_hex='+p.stdout.hex())from error
    if not isinstance(data,dict) or not isinstance(data.get('conflicts'),list) or not isinstance(data.get('background'),list):
        raise RuntimeError('OS snapshot missing required process arrays')
    data['process_query']={'exit_code':p.returncode,'stdout_sha256':hashlib.sha256(p.stdout).hexdigest(),
                           'stderr_sha256':hashlib.sha256(p.stderr).hexdigest(),'raw_stdout_utf8':text,
                           'raw_stderr_utf8':p.stderr.decode('utf-8',errors='strict')}
    return data


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument('--freeze', type=Path, required=True)
    ap.add_argument('--out', type=Path, required=True)
    a = ap.parse_args()
    root = Path(__file__).resolve().parents[1]
    freeze = json.loads(a.freeze.read_text(encoding='utf-8'))
    if freeze['status'] != 'FROZEN' or not freeze['regression_passed']:
        ap.error('completed regression and freeze required')
    out = a.out.resolve()
    out.mkdir(parents=True, exist_ok=False)
    rows = []
    parent = 'm5-formal-' + datetime.datetime.now(datetime.timezone.utc).strftime('%Y%m%dT%H%M%S.%fZ')
    bins = {k: Path(v['path']) for k, v in freeze['binaries'].items()}

    def command(name, cmd, timeout):
        subprocess.run([sys.executable, root / 'scripts/run_checked.py', '--log-dir', out / 'commands', '--name', name, '--timeout', str(timeout), '--', *map(str, cmd)], cwd=root, check=False)
        return json.loads((out / 'commands' / name / 'command.json').read_text(encoding='utf-8'))

    def check_frozen():
        if current_source(root) != freeze['source_manifest_sha256']:
            raise RuntimeError('source differs from frozen regression version')
        if sha(root / 'docs/FORMAL_EXPERIMENT_PLAN.md') != freeze['plan_sha256']:
            raise RuntimeError('frozen plan changed')
        for name, path in bins.items():
            if sha(path) != freeze['binaries'][name]['sha256']:
                raise RuntimeError('binary changed: ' + name)
        if sha(Path(freeze.get('demo_zip_path', root.parent / 'EdgeHybrid_Demo_Windows.zip'))) != freeze['demo_zip_sha256']:
            raise RuntimeError('preserved Demo ZIP changed')

    matrix = [('handshake', mode, 1) for mode in ['T', 'H', 'P']]
    matrix += [('resource', mode, batch) for mode in ['T', 'H'] for batch in range(1, 6)]
    matrix += [('throughput', mode, 1) for mode in ['T', 'H', 'P']]
    try:
        check_frozen()
        write(out / 'matrix.json', dict(parent_run_id=parent, plan_id=freeze['plan_id'], matrix=matrix, source_manifest_sha256=freeze['source_manifest_sha256'], binary_hashes={k:v['sha256'] for k,v in freeze['binaries'].items()}, warmup=0, retries=0))
        with tempfile.TemporaryDirectory(prefix='edgehybrid-m5-formal-p-') as secrets:
            for kind, mode, batch in matrix:
                check_frozen()
                name = f'{kind}_{mode}_{batch}'
                env = environment()
                env.update(captured_at_utc=datetime.datetime.now(datetime.timezone.utc).isoformat(), GOMAXPROCS=os.environ.get('GOMAXPROCS', 'unset: Go runtime default'), logging='production checks and public events enabled; synchronous persistence; unchanged M5B observation channel')
                write(out / (name + '_environment.json'), env)
                if env['conflicts']:
                    raise RuntimeError('sampling blocked by active Demo/experiment/Go process; see ' + name + '_environment.json')
                run_id = parent + '-' + name
                result_root = out / name
                common = ['-experiment', kind, '-run-kind', 'formal', '-timeout', '30s', '-run-id', run_id, '-parent-run-id', parent, '-source-manifest-sha256', freeze['source_manifest_sha256'], '-out', result_root]
                if mode == 'P':
                    binary = bins['pbench']
                    cmd = [binary, *common, '-certs-dir', Path(secrets) / 'certs']
                else:
                    binary = bins['bench']
                    result_root.mkdir()
                    cmd = [binary, *common, '-mode', 'traditional' if mode == 'T' else 'hybrid']
                    if kind != 'handshake':
                        cmd += ['-gateway-bin', bins['gateway']]
                if kind == 'handshake':
                    cmd += ['-trials', '1000']
                elif kind == 'resource':
                    cmd += ['-trials', '100']
                else:
                    cmd += ['-rounds', '5', '-per-round', '200']
                print('START ' + name + ' (fixed requested count, no retries)', flush=True)
                info = command(name, cmd, 3000)
                dirs = [result_root] if mode == 'P' else list(result_root.iterdir())
                actual = dirs[0] if len(dirs) == 1 and dirs[0].is_dir() else None
                row = dict(name=name, mode=mode, experiment=kind, batch=batch, run_id=run_id, parent_run_id=parent, result_dir=str(actual) if actual else None, source_manifest_sha256=freeze['source_manifest_sha256'], binary_sha256=sha(binary), gateway_binary_sha256=sha(bins['gateway']) if mode != 'P' and kind != 'handshake' else None, producer_command=info)
                rows.append(row)
                write(out / 'results_index.json', rows)
                if actual:
                    verification = command('verify_' + name, [binary, '-verify', '-dir', actual, '-exit-code', str(info['exit_code'])], 120)
                    row['verification_command'] = verification
                    manifest = json.loads((actual / 'manifest.json').read_text(encoding='utf-8'))
                    row['config_hash'] = manifest.get('config_sha256', manifest.get('config_hash'))
                    if manifest['run_id'] != run_id or manifest['source_manifest_sha256'] != freeze['source_manifest_sha256']:
                        raise RuntimeError('run/source association mismatch')
                write(out / 'results_index.json', rows)
                print('END ' + name + ': actual producer exit ' + str(info['exit_code']) + ', verifier exit ' + str(row.get('verification_command', {}).get('exit_code')), flush=True)
                if info['timeout_cleanup'] or info['exit_code'] not in [0, 3, 4] or not actual or row['verification_command']['exit_code'] != 0:
                    raise RuntimeError('execution/evidence error; retained this batch and stopped, no automatic retry')
                if info['exit_code'] == 4:
                    raise RuntimeError('interrupted batch retained; remaining matrix not started')
        check_frozen()
        cleanup = environment()
        write(out / 'cleanup_environment.json', cleanup)
        if cleanup['conflicts']:
            raise RuntimeError('remaining experiment process after matrix')
        write(out / 'execution_status.json', dict(status='COMPLETED' if all(x['producer_command']['exit_code']==0 for x in rows) else 'COMPLETED_WITH_FAILURES', runs=len(rows), planned_runs=len(matrix), retries=0, parent_run_id=parent, private_P_directory_removed=True))
        return 0
    except BaseException as e:
        write(out / 'execution_status.json', dict(status='INCOMPLETE', reason=str(e), runs=len(rows), planned_runs=len(matrix), retries=0, parent_run_id=parent))
        print(str(e), file=sys.stderr)
        return 1

if __name__ == '__main__':
    raise SystemExit(main())
