#!/usr/bin/env python3
"""Run one command with immutable raw stdout/stderr, exit code and platform.

Usage: python run_checked.py --log-dir DIR --name NAME -- COMMAND ARG ...
Only the child process gets the existing Go PATH and GOTOOLCHAIN=local.
"""
import argparse
import datetime
import json
import os
from pathlib import Path
import platform
import subprocess
import sys

GO = r"C:\Users\20477\.workbuddy\binaries\go\go\bin\go.exe"

def main():
    p = argparse.ArgumentParser()
    p.add_argument('--log-dir', required=True)
    p.add_argument('--name', required=True)
    p.add_argument('--timeout', type=float, default=300)
    p.add_argument('command', nargs=argparse.REMAINDER)
    a = p.parse_args()
    cmd = a.command[1:] if a.command[:1] == ['--'] else a.command
    if not cmd:
        p.error('command required')
    out = Path(a.log_dir) / a.name
    out.mkdir(parents=True, exist_ok=False)
    env = os.environ.copy()
    env['GOTOOLCHAIN'] = 'local'
    env['PATH'] = str(Path(GO).parent) + os.pathsep + env.get('PATH', '')
    env['GO'] = GO
    env['PYTHONIOENCODING'] = 'utf-8'
    env['PYTHONUTF8'] = '1'
    started = datetime.datetime.now(datetime.timezone.utc).isoformat()
    timed_out = False
    with (out / 'stdout.txt').open('wb') as stdout, (out / 'stderr.txt').open('wb') as stderr:
        child = subprocess.Popen(cmd, cwd=Path.cwd(), env=env, stdout=stdout, stderr=stderr)
        try:
            code = child.wait(timeout=a.timeout)
        except subprocess.TimeoutExpired:
            timed_out = True
            if os.name == 'nt':
                subprocess.run(['taskkill', '/PID', str(child.pid), '/T', '/F'], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, timeout=10)
            if child.poll() is None:
                child.kill()
            code = child.wait(timeout=10)
    info = dict(command=cmd, cwd=str(Path.cwd().resolve()), platform=platform.platform(),
                started_utc=started, finished_utc=datetime.datetime.now(datetime.timezone.utc).isoformat(),
                exit_code=code, timeout_cleanup=timed_out, goto_toolchain='local')
    (out / 'command.json').write_text(json.dumps(info, ensure_ascii=False, indent=2), encoding='utf-8')
    (out / 'exit.txt').write_text(str(code) + '\n', encoding='ascii')
    print(json.dumps(dict(log=str(out), exit_code=code, timeout_cleanup=timed_out), ensure_ascii=True))
    return code if code >= 0 else 1

if __name__ == '__main__':
    sys.exit(main())
