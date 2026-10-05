#!/usr/bin/env python3
"""Native Windows T/H E2E and Fleet harness using the existing strict verifiers.

It builds this source tree into a private temporary directory, provisions fresh
test keys there, and preserves only public events/command evidence. No WSL is
used; historical result files are never overwritten.
"""
import argparse
import contextlib
import datetime
import json
import os
from pathlib import Path
import platform
import subprocess
import sys
import tempfile
import time

def main():
    ap = argparse.ArgumentParser()
    ap.add_argument('--mode', choices=['traditional', 'hybrid'], default='hybrid')
    ap.add_argument('--devices', type=int, default=1)
    ap.add_argument('--sessions', type=int, default=1)
    ap.add_argument('--samples', type=int, default=3)
    ap.add_argument('--bad-device', default='')
    ap.add_argument('--out', required=True)
    ap.add_argument('--go', default='go')
    ap.add_argument('--bin-dir', help='optional existing binaries built from this tree')
    ap.add_argument('--source-hash', default='', help='manifest hash of the producing tree')
    a = ap.parse_args()
    if os.name != 'nt':
        ap.error('this harness requires native Windows')
    root = Path(__file__).resolve().parents[1]
    out = Path(a.out).resolve()
    out.mkdir(parents=True, exist_ok=False)
    run_id = 'windows-' + a.mode + '-' + datetime.datetime.now(datetime.timezone.utc).strftime('%Y%m%dT%H%M%S.%fZ')
    commands = []
    status = 'FAIL'
    gw = None
    forced = False
    def run(name, cmd, timeout=60):
        with (out / (name + '.stdout')).open('wb') as stdout, (out / (name + '.stderr')).open('wb') as stderr:
            result = subprocess.run([str(x) for x in cmd], cwd=root, stdout=stdout, stderr=stderr, timeout=timeout)
        commands.append(dict(name=name, command=[str(x) for x in cmd], exit_code=result.returncode))
        return result.returncode
    def write(path, obj):
        path.write_text(json.dumps(obj, ensure_ascii=False, indent=2), encoding='utf-8')
    def check(code, name):
        if code:
            raise RuntimeError(name + ' exit=' + str(code))
    def cleanup_gateway():
        nonlocal forced
        if gw is not None and gw.poll() is None:
            forced = True
            gw.kill()
            gw.wait(timeout=10)
    try:
        with contextlib.ExitStack() as resources:
            work = Path(resources.enter_context(tempfile.TemporaryDirectory(prefix='edgehybrid-windows-')))
            resources.callback(cleanup_gateway)
            binaries = Path(a.bin_dir).resolve() if a.bin_dir else work / 'bin'
            if not a.bin_dir:
                binaries.mkdir()
                check(run('build', [a.go, 'build', '-o', str(binaries) + os.sep, './cmd/...']), 'build')
            runtime = work / 'runtime'
            check(run('init', [binaries / 'secctl.exe', 'init-demo', '-dir', runtime, '-gateway', 'GW001',
                               '-devices', str(a.devices), '-id-pattern', 'D%03d']), 'provision')
            common = dict(minimum_mode=a.mode, allow_legacy=a.mode == 'traditional',
                          supported_suites=[1 if a.mode == 'traditional' else 2], handshake_timeout_ms=15000,
                          max_frame_bytes=16384, trust_store=str(runtime / 'trust.json'), key_directory=str(runtime / 'keys'))
            cfg = work / 'gateway.json'
            write(cfg, dict(common, role='gateway', id='GW001', address='127.0.0.1:0'))
            # A rejected bad identity finishes one session, good devices finish
            # all requested sessions. The production gateway stops after they
            # finish, waits for workers, then syncs/closes its public events.
            cap = a.devices * a.sessions if not a.bad_device else (a.devices - 1) * a.sessions + 1
            gw_cmd = [str(binaries / 'gateway.exe'), '-config', str(cfg), '-max-sessions', str(cap),
                      '-run-for', '45s', '-run-id', run_id, '-events', str(out / 'gateway.jsonl')]
            with (out / 'gateway.stdout').open('wb') as stdout, (out / 'gateway.stderr').open('wb') as stderr:
                gw = subprocess.Popen(gw_cmd, cwd=root, stdout=stdout, stderr=stderr)
                address = ''
                limit = time.monotonic() + 10
                while time.monotonic() < limit:
                    if gw.poll() is not None:
                        raise RuntimeError('gateway stopped before readiness')
                    events = out / 'gateway.jsonl'
                    if events.exists():
                        for line in events.read_text(encoding='utf-8').splitlines():
                            try:
                                ev = json.loads(line)
                            except json.JSONDecodeError:
                                continue  # an in-progress last write; final verifier is strict
                            if ev.get('run_id') == run_id and ev.get('stage') == 'listen' and ev.get('outcome') == 'ready':
                                address = ev['detail']
                    if address:
                        break
                    time.sleep(.01)
                if not address:
                    raise RuntimeError('readiness timeout')
                device_cfg = work / 'device.json'
                write(device_cfg, dict(common, role='device', id='D001', address=address, expected_peer_id='GW001'))
                if a.devices == 1 and a.sessions == 1 and not a.bad_device:
                    device_code = run('device', [binaries / 'device.exe', '-config', device_cfg, '-samples', str(a.samples),
                                                '-run-for', '30s', '-run-id', run_id, '-events', out / 'device.jsonl'])
                    check(device_code, 'device')
                    verify = [sys.executable, root / 'scripts/verify_e2e_events.py', '--gateway', out / 'gateway.jsonl',
                              '--device', out / 'device.jsonl', '--run-id', run_id, '--samples', str(a.samples),
                              '--device-id', 'D001', '--gateway-id', 'GW001', '--expect-suite', str(common['supported_suites'][0]),
                              '--out', out / 'verify.json']
                else:
                    device_code = run('fleet', [binaries / 'fleet.exe', '-config', device_cfg, '-devices', str(a.devices),
                                               '-first', '1', '-id-pattern', 'D%03d', '-gateway', 'GW001', '-samples', str(a.samples),
                                               '-sessions', str(a.sessions), '-rekey-records', str(a.samples * 3),
                                               '-bad-device', a.bad_device, '-run-id', run_id, '-events', out / 'device.jsonl',
                                               '-summary', out / 'fleet_summary.json'])
                    if (device_code == 0) == bool(a.bad_device):
                        raise RuntimeError('fleet exit did not match the expected good/bad scenario')
                    verify = [sys.executable, root / 'scripts/verify_fleet_events.py', '--gateway', out / 'gateway.jsonl',
                              '--summary', out / 'fleet_summary.json', '--run-id', run_id, '--expect-devices', str(a.devices),
                              '--expect-suite', str(common['supported_suites'][0]), '--expect-samples', str(a.samples),
                              '--expect-sessions', str(a.sessions), '--expect-bad-device', a.bad_device, '--out', out / 'verify.json']
                gw_code = gw.wait(timeout=50)
                commands.append(dict(name='gateway', command=gw_cmd, exit_code=gw_code, forced_cleanup=False, waited=True))
                check(gw_code, 'gateway')
                check(run('verify', verify), 'strict event verifier')
                status = 'PASS'
    except Exception as e:
        (out / 'diagnostics.txt').write_text(str(e), encoding='utf-8')
        print(str(e), file=sys.stderr)
    finally:
        if gw is not None and gw.poll() is None:
            forced = True
            gw.kill()
            gw.wait(timeout=10)
        write(out / 'status.json', dict(status=status, run_id=run_id, platform=platform.platform(),
                                       source_manifest_sha256=a.source_hash, run_kind='smoke',
                                       formal_results_eligible=False, forced_cleanup=forced, commands=commands))
    print(json.dumps(dict(status=status, out=str(out)), ensure_ascii=True))
    return 0 if status == 'PASS' else 1

if __name__ == '__main__':
    sys.exit(main())
