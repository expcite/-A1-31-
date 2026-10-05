#!/usr/bin/env python3
"""Test the packaged exe, using only native Windows in the demo child PATH.

Python is the development verifier, not a runtime dependency of the demo.
Only fixed public log names are copied; test private directories are never read.
"""
import argparse
import contextlib
import ctypes
import json
import os
from pathlib import Path
import platform
import shutil
import socket
import subprocess
import sys
import tempfile
import time
import urllib.error
import urllib.request

PUBLIC = ('gateway.jsonl', 'device.jsonl', 'events.jsonl', 'lifecycle.jsonl',
          'manifest.json', 'completed.json', 'final_state.json')

def main():
    ap = argparse.ArgumentParser()
    ap.add_argument('--exe', required=True)
    ap.add_argument('--out', required=True)
    a = ap.parse_args()
    if os.name != 'nt':
        ap.error('native Windows required')
    root = Path(__file__).resolve().parents[1]
    exe = Path(a.exe).resolve()
    out = Path(a.out).resolve()
    out.mkdir(parents=True, exist_ok=False)
    cases = []
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))

    def fetch(url, headers=None, method='GET'):
        request = urllib.request.Request(url, headers=headers or {}, method=method)
        try:
            with opener.open(request, timeout=3) as r:
                return r.status, r.read(), dict(r.headers)
        except urllib.error.HTTPError as e:
            return e.code, e.read(), dict(e.headers)

    def record():
        (out / 'acceptance.json').write_text(json.dumps(dict(platform=platform.platform(),
            test_type='native Windows; no development tools in demo PATH', exe=str(exe), cases=cases),
            ensure_ascii=False, indent=2), encoding='utf-8')

    with tempfile.TemporaryDirectory(prefix='EdgeHybrid 演示 验收 ') as tmp:
        env = os.environ.copy()
        env['PATH'] = str(Path(os.environ['SystemRoot']) / 'System32')
        env['LOCALAPPDATA'] = str(Path(tmp) / '独立 用户数据')
        (out / 'environment.json').write_text(json.dumps(dict(child_PATH=env['PATH'],
            child_LOCALAPPDATA=env['LOCALAPPDATA'], developer_tools_in_child_path=False,
            coverage='this Windows host only, not all Windows PCs', executable=str(exe)),
            ensure_ascii=False, indent=2), encoding='utf-8')
        sentinel = subprocess.Popen([sys.executable, '-c', 'import time; time.sleep(240)'],
                                    stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        try:
            def start(name, preset='1', extra=()):
                folder = out / name
                folder.mkdir()
                stdout = (folder / 'stdout.txt').open('wb')
                stderr = (folder / 'stderr.txt').open('wb')
                command = [str(exe), '-preset', preset, '-no-browser'] + list(extra)
                p = subprocess.Popen(command, cwd=tmp, env=env, stdin=subprocess.PIPE,
                                     stdout=stdout, stderr=stderr)
                limit = time.monotonic() + 20
                values = {}
                while time.monotonic() < limit:
                    text = (folder / 'stdout.txt').read_text(encoding='utf-8', errors='replace')
                    for line in text.splitlines():
                        if '=' in line:
                            key, value = line.split('=', 1)
                            if key in ('RUN_ID', 'PAGE_URL', 'LOG_DIR'):
                                values[key] = value
                    if 'PAGE_URL' in values or p.poll() is not None:
                        break
                    time.sleep(.02)
                if 'PAGE_URL' not in values:
                    if p.poll() is None:
                        p.kill(); p.wait(timeout=10)
                    stdout.close(); stderr.close()
                    raise RuntimeError(name + ' startup failed: ' + text)
                info = dict(name=name, command=command, pid=p.pid, values=values,
                            child_path=env['PATH'], cwd=tmp)
                return p, folder, stdout, stderr, info

            def wait_complete(p, info):
                limit = time.monotonic() + 35
                while time.monotonic() < limit:
                    if p.poll() is not None:
                        raise RuntimeError('demo exited while page should be retained')
                    code, body, _ = fetch(info['values']['PAGE_URL'] + 'api/state')
                    s = json.loads(body)
                    if code != 200 or s['run_id'] != info['values']['RUN_ID']:
                        raise RuntimeError('wrong run page')
                    if s['workers_joined']:
                        return s
                    time.sleep(.03)
                raise RuntimeError('demo work timeout')

            def copy_logs(folder, info):
                logs = Path(info['values']['LOG_DIR'])
                public = folder / 'public_logs'
                public.mkdir()
                for name in PUBLIC:
                    if (logs / name).is_file():
                        shutil.copyfile(logs / name, public / name)
                return public

            def end(p, folder, stdout, stderr, info, expected, use_stdin=False):
                if use_stdin:
                    p.stdin.write(b'\n'); p.stdin.flush()
                    stop_code = None
                else:
                    stop = subprocess.run([str(exe), '-stop'], cwd=tmp, env=env,
                                          stdin=subprocess.DEVNULL, capture_output=True, timeout=10)
                    stop_code = stop.returncode
                    (folder / 'stop.stdout').write_bytes(stop.stdout)
                    (folder / 'stop.stderr').write_bytes(stop.stderr)
                    assert stop_code == 0, stop.stdout
                code = p.wait(timeout=15)
                p.stdin.close(); stdout.close(); stderr.close()
                assert code == expected, (info['name'], code, expected)
                public = copy_logs(folder, info)
                final = json.loads((public / 'final_state.json').read_text(encoding='utf-8'))
                assert final['status'] == '已停止' and final['online'] == 0 and final['workers_joined']
                private = Path(env['LOCALAPPDATA']) / 'EdgeHybridDemo'
                assert not list(private.glob('*/private/' + info['values']['RUN_ID'])), 'owned keys remained'
                with contextlib.suppress(urllib.error.URLError, OSError):
                    fetch(info['values']['PAGE_URL'])
                    raise AssertionError('page listener remained after stop')
                assert sentinel.poll() is None, 'unrelated sentinel was terminated'
                info.update(exit_code=code, stop_exit_code=stop_code, owned_process_exited=True,
                            unrelated_process_untouched=True, keys_cleaned=True, public_logs=str(public), status='PASS')
                (folder / 'command.json').write_text(json.dumps(info, ensure_ascii=False, indent=2), encoding='utf-8')
                (folder / 'exit.txt').write_text(str(code), encoding='ascii')
                cases.append(info); record()
                return public

            for name, preset, good in [('one', '1', 1), ('twenty', '20', 20), ('fault20', '19+1', 19)]:
                p, folder, stdout, stderr, info = start(name, preset)
                try:
                    s = wait_complete(p, info)
                    assert s['online'] == 0 and s['accepted'] == s['sent'] == s['confirmed'] == good * 3
                    assert s['handshake_devices'] == good and s['completed_devices'] == good
                    sessions = [d['session_id'] for d in s['devices'] if d['worker_ok']]
                    assert len(set(sessions)) == good and all(len(x) == 32 for x in sessions)
                    assert all(d['suite'] == 2 and d['kem_used'] and d['device_authenticated']
                               and d['gateway_authenticated'] for d in s['devices'] if d['worker_ok'])
                    if preset == '19+1':
                        bad = s['devices'][19]
                        assert s['failed_devices'] == 1 and bad['id'] == 'D020' and not bad['session_id']
                        assert bad['error_stage'] == '本地信任预检（未发起握手）'
                    (folder / 'page_state.json').write_text(json.dumps(s, ensure_ascii=False, indent=2), encoding='utf-8')
                    gateway = [json.loads(x) for x in (Path(info['values']['LOG_DIR']) / 'gateway.jsonl').read_text(encoding='utf-8').splitlines()]
                    for outcome, key in [('telemetry-accepted', 'accepted'), ('control-sent', 'sent'), ('control-confirmed', 'confirmed')]:
                        assert sum(e['outcome'] == outcome for e in gateway) == s[key]
                    if name == 'one':
                        dup = subprocess.run([str(exe), '-preset', '20', '-no-browser'], cwd=tmp, env=env,
                                             stdin=subprocess.DEVNULL, capture_output=True, timeout=8)
                        (folder / 'duplicate.stdout').write_bytes(dup.stdout)
                        assert dup.returncode == 2 and '已经运行' in dup.stdout.decode('utf-8')
                        info['duplicate_exit_code'] = dup.returncode
                        for path, headers, method, expected in [
                            ('api/state', {'Host': 'evil.example'}, 'GET', 403),
                            ('api/state', {'Origin': 'https://evil.example'}, 'GET', 403),
                            ('api/state', {'Sec-Fetch-Site': 'cross-site'}, 'GET', 403),
                            ('api/stop', {}, 'POST', 405), ('runtime/keys', {}, 'GET', 404),
                            ('api/state?path=private', {}, 'GET', 400)]:
                            status, _, hdrs = fetch(info['values']['PAGE_URL'] + path, headers, method)
                            assert status == expected and 'Access-Control-Allow-Origin' not in hdrs
                    public = end(p, folder, stdout, stderr, info, 3 if preset == '19+1' else 0)
                    # Reuse the original strict real E2E verifier for each good
                    # device, filtering only original public event envelopes.
                    events = [json.loads(x) for x in (public / 'events.jsonl').read_text(encoding='utf-8').splitlines()]
                    for d in s['devices']:
                        if not d['worker_ok']:
                            continue
                        checkdir = folder / ('verify_' + d['id'])
                        checkdir.mkdir()
                        gw = [x['event'] for x in events if x['source'] == 'GW001' and (x['event'].get('peer_id') == d['id'] or x['event']['stage'] == 'listen')]
                        dev = [x['event'] for x in events if x['source'] == d['id']]
                        for fn, records in [('gateway.jsonl', gw), ('device.jsonl', dev)]:
                            (checkdir / fn).write_text(''.join(json.dumps(x) + '\n' for x in records), encoding='utf-8')
                        cmd = [sys.executable, str(root / 'scripts/verify_e2e_events.py'), '--gateway', str(checkdir / 'gateway.jsonl'),
                               '--device', str(checkdir / 'device.jsonl'), '--run-id', s['run_id'], '--samples', '3',
                               '--device-id', d['id'], '--gateway-id', 'GW001', '--expect-suite', '2', '--out', str(checkdir / 'verdict.json')]
                        result = subprocess.run(cmd, capture_output=True, timeout=10)
                        (checkdir / 'stdout.txt').write_bytes(result.stdout)
                        (checkdir / 'stderr.txt').write_bytes(result.stderr)
                        assert result.returncode == 0, result.stdout.decode('utf-8', errors='replace')
                finally:
                    if p.poll() is None:
                        subprocess.run([str(exe), '-stop'], env=env, capture_output=True, timeout=10)
                        p.wait(timeout=15)
                    stdout.close(); stderr.close()

            p, folder, stdout, stderr, info = start('manual_stop', '20')
            end(p, folder, stdout, stderr, info, 3, use_stdin=True)
            p, folder, stdout, stderr, info = start('abnormal_exit', '20')
            p.kill(); code = p.wait(timeout=10); p.stdin.close(); stdout.close(); stderr.close()
            public = copy_logs(folder, info)
            info.update(status='PASS', expected_forced_test_exit=True, actual_exit_code=code,
                        owned_process_exited=True, public_logs=str(public))
            cases.append(info); record()
            old_id = info['values']['RUN_ID']
            p, folder, stdout, stderr, info = start('restart_after_abnormal', '1')
            s = wait_complete(p, info)
            assert s['run_id'] != old_id and s['confirmed'] == 3
            end(p, folder, stdout, stderr, info, 0)
            assert '上次演示未记录正常退出' in (folder / 'stdout.txt').read_text(encoding='utf-8')
            with socket.socket() as blocker:
                blocker.bind(('127.0.0.1', 0)); blocker.listen()
                port = blocker.getsockname()[1]
                failure = subprocess.run([str(exe), '-http-port', str(port), '-no-browser'], cwd=tmp, env=env,
                                         stdin=subprocess.DEVNULL, capture_output=True, timeout=10)
                assert failure.returncode == 2 and '端口不可用' in failure.stdout.decode('utf-8')
                folder = out / 'port_conflict'; folder.mkdir()
                (folder / 'stdout.txt').write_bytes(failure.stdout); (folder / 'stderr.txt').write_bytes(failure.stderr)
                cases.append(dict(name='port_conflict', status='PASS', exit_code=failure.returncode,
                                  owned_process_exited=True)); record()
            print('Windows packaged exe acceptance PASS; 1/20/fault, boundaries, stop/restart/abnormal, isolated tool-free child PATH')
            return 0
        finally:
            if sentinel.poll() is None:
                sentinel.kill(); sentinel.wait(timeout=5)

if __name__ == '__main__':
    sys.exit(main())
