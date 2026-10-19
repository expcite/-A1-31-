"""Build a public desktop-first release with optional, freshly paired Android support."""
from pathlib import Path
import argparse
import hashlib
import json
import os
import shutil
import subprocess
import sys
import zipfile

import source_manifest


def main():
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument('--out', type=Path, required=True)
    ap.add_argument('--evidence', type=Path, required=True)
    a = ap.parse_args()
    root = Path(__file__).resolve().parents[1]
    out, evidence = a.out.resolve(), a.evidence.resolve()
    if out.exists() or evidence.exists():
        raise SystemExit('Use new output and evidence directories; existing deployments are never overwritten')
    out.mkdir(parents=True)
    evidence.mkdir(parents=True)
    version = (root / 'VERSION').read_text(encoding='utf-8').strip()
    go = os.environ.get('GO') or shutil.which('go')
    if not go:
        raise SystemExit('Go required')
    env = dict(os.environ, GOTOOLCHAIN='local', GOPROXY='off', CGO_ENABLED='0', GOOS='windows', GOARCH='amd64')
    subprocess.run([sys.executable, '-B', str(root / 'scripts/backend_build.py'),
                    '--out', str(out / 'runtime'), '--evidence', str(evidence / 'desktop'),
                    '--version', version], cwd=root, env=env, check=True, timeout=600)
    info = json.loads((out / 'runtime/build_info.json').read_text(encoding='utf-8'))
    source_hash = info['source_manifest_sha256']
    flags = f'-s -w -X main.BuildVersion={version} -X main.SourceHash={source_hash} -X example.com/edgehybrid/internal/console.Version={version} -X example.com/edgehybrid/internal/console.SourceHash={source_hash}'
    for exe, field in [('bench.exe', 'BenchSHA'), ('pbench.exe', 'PBenchSHA'), ('scenario.exe', 'ScenarioSHA')]:
        flags += f' -X example.com/edgehybrid/internal/console.{field}={info["binaries"][exe]["sha256"]}'
    template = out / 'phone-template'
    template.mkdir()
    commands = []
    for target, dest, pkg in [('windows', out / 'runtime/EdgeHybridPhoneGateway.exe', './cmd/phonegateway'),
                              ('android', template / 'edgehybrid-phone', './cmd/phone')]:
        cmd = [go, 'build', '-trimpath', '-ldflags', flags, '-o', str(dest), pkg]
        r = subprocess.run(cmd, cwd=root, env=dict(env, GOOS=target, GOARCH='arm64' if target == 'android' else 'amd64'), capture_output=True, text=True, encoding='utf-8', timeout=180)
        commands.append({'target': target, 'command': cmd, 'exit_code': r.returncode, 'stderr': r.stderr})
        if r.returncode:
            raise RuntimeError(r.stderr)
    (template / 'run-phone.sh').write_text('''#!/data/data/com.termux/files/usr/bin/bash
set -euo pipefail
cd "$(dirname "$0")"
umask 077
chmod 700 . runtime runtime/keys runtime/keys/D001
chmod 600 phone.json runtime/trust.json runtime/keys/D001/*.key
chmod 700 edgehybrid-phone
termux-wake-lock
trap 'termux-wake-unlock' EXIT
log="phone-events-$(date -u +%Y%m%dT%H%M%SZ)-$$.jsonl"
if [ "$#" -eq 0 ]; then
  ./edgehybrid-phone -config phone.json -torch-on-control -run-for 10m | tee "$log"
else
  ./edgehybrid-phone -config phone.json -gateway "$1" -torch-on-control -run-for 10m | tee "$log"
fi
''', encoding='utf-8', newline='\n')
    (template / 'PHONE_BUILD_INFO.json').write_text(json.dumps({'version': version, 'source_manifest_sha256': source_hash, 'target': 'android/arm64', 'binary_sha256': sha(template / 'edgehybrid-phone')}, indent=2) + '\n', encoding='utf-8')
    ps(out / 'prepare_phone.ps1', '''param([string]$HotspotIP='192.168.137.1', [int]$Port=10443)
$ErrorActionPreference='Stop'
$bindProbe=[Net.Sockets.TcpListener]::new([Net.IPAddress]::Parse($HotspotIP),$Port)
try { $bindProbe.Start() } catch { throw "热点业务端口$HotspotIP`:$Port无法绑定。请检查端口占用和Windows保留范围：netsh interface ipv4 show excludedportrange protocol=tcp。不要关闭防火墙或删除系统保留范围。原始错误：$($_.Exception.Message)" } finally { $bindProbe.Stop() }
$localRoot=Join-Path $PSScriptRoot 'local-phone'
if (Test-Path -LiteralPath $localRoot) { throw 'local-phone已存在，请保留原身份；首次验收请把公开ZIP解压到另一个新目录。' }
$phoneDir=Join-Path $localRoot 'phone'
$privateDir=Join-Path $localRoot 'pc-private'
$exe=Join-Path $PSScriptRoot 'runtime/EdgeHybridPhoneGateway.exe'
& $exe -phone-out $phoneDir -private $privateDir -listen "$HotspotIP`:$Port"
if ($LASTEXITCODE -ne 0) { throw '配对生成失败；不要继续启动。' }
Copy-Item -LiteralPath (Join-Path $PSScriptRoot 'phone-template/edgehybrid-phone') -Destination $phoneDir
Copy-Item -LiteralPath (Join-Path $PSScriptRoot 'phone-template/run-phone.sh') -Destination $phoneDir
Copy-Item -LiteralPath (Join-Path $PSScriptRoot 'phone-template/PHONE_BUILD_INFO.json') -Destination $phoneDir
Add-Type -AssemblyName System.IO.Compression.FileSystem
$archive=Join-Path $localRoot 'EdgeHybrid_Phone_Fresh_v28.zip'
[IO.Compression.ZipFile]::CreateFromDirectory($phoneDir,$archive)
Write-Host "已生成完整手机安装包：$archive"
Write-Host '此包含新建的D001设备私钥，仅复制到配对手机；GitHub与公开交付使用原始公开包。'
''')
    ps(out / 'start_phone.ps1', '''param([string]$HotspotIP='192.168.137.1', [int]$Port=10443)
$ErrorActionPreference='Stop'
Set-Location -LiteralPath $PSScriptRoot
if (-not (Get-NetIPAddress -AddressFamily IPv4 -IPAddress $HotspotIP -ErrorAction SilentlyContinue)) { throw "未检测到热点地址$HotspotIP，请先开启电脑移动热点。" }
$bindProbe=[Net.Sockets.TcpListener]::new([Net.IPAddress]::Parse($HotspotIP),$Port)
try { $bindProbe.Start() } catch { throw "热点业务端口$HotspotIP`:$Port无法绑定。请检查端口占用和Windows保留范围：netsh interface ipv4 show excludedportrange protocol=tcp。不要关闭防火墙或删除系统保留范围。原始错误：$($_.Exception.Message)" } finally { $bindProbe.Stop() }
$privateDir=Join-Path $PSScriptRoot 'local-phone/pc-private'
if (-not (Test-Path -LiteralPath (Join-Path $privateDir 'trust.json'))) { throw '请先运行01_生成本机手机配对包.cmd。' }
& (Join-Path $PSScriptRoot 'runtime/EdgeHybridPhoneGateway.exe') -private $privateDir -listen "$HotspotIP`:$Port"
exit $LASTEXITCODE
''')
    ps(out / 'allow_phone.ps1', '''param([string]$HotspotIP='192.168.137.1', [int]$Port=10443)
$ErrorActionPreference='Stop'
if (-not ([Security.Principal.WindowsPrincipal][Security.Principal.WindowsIdentity]::GetCurrent()).IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) { throw '请右键02_允许手机连接.cmd，选择以管理员身份运行。' }
if (-not (Get-NetIPAddress -AddressFamily IPv4 -IPAddress $HotspotIP -ErrorAction SilentlyContinue)) { throw "未检测到热点地址$HotspotIP。" }
$exe=Join-Path $PSScriptRoot 'runtime/EdgeHybridPhoneGateway.exe'
$name='EdgeHybridPhone28-' + (Get-FileHash -LiteralPath $exe -Algorithm SHA256).Hash.Substring(0,12) + "-$Port"
if (-not (Get-NetFirewallRule -Name $name -ErrorAction SilentlyContinue)) {
  New-NetFirewallRule -Name $name -DisplayName 'EdgeHybrid 手机验证（热点子网）' -Direction Inbound -Action Allow -Program $exe -Protocol TCP -LocalPort $Port -LocalAddress $HotspotIP -RemoteAddress LocalSubnet -Profile Private,Public | Out-Null
}
Write-Host '本程序的热点子网规则已准备。网页管理端仍仅监听本机。'
''')
    for name, command in [('00_启动电脑演示.cmd', '"%~dp0runtime\\EdgeHybridInteractive.exe" -devices 1'),
                          ('00_启动20台电脑演示.cmd', '"%~dp0runtime\\EdgeHybridInteractive.exe" -devices 20'),
                          ('停止电脑演示.cmd', '"%~dp0runtime\\EdgeHybridInteractive.exe" -stop')]:
        cmdfile(out / name, command)
    for name, script in [('01_生成本机手机配对包.cmd', 'prepare_phone.ps1'), ('02_允许手机连接.cmd', 'allow_phone.ps1'), ('03_启动手机验证.cmd', 'start_phone.ps1')]:
        cmdfile(out / name, f'powershell.exe -NoProfile -ExecutionPolicy Bypass -File "%~dp0{script}" %*')
    for name in ['LICENSE.txt', 'THIRD_PARTY_NOTICES.md']:
        shutil.copy2(root / name, out / name)
    shutil.copytree(root / 'docs/licenses', out / 'licenses')
    shutil.copy2(root / 'docs/PHONE_DEMO.md', out / '手机部署与验收.md')
    (out / 'README.md').write_text(f'''# EdgeHybrid 完整软件包

版本{version}。双击00_启动电脑演示.cmd即可运行，无需手机。手机为可选实机验证终端，按手机部署与验收.md在首次使用时生成本机配对，再安装到独立目录。

公开包内没有预置身份。运行后生成的local-phone包含本机私钥，不作为公开包的一部分。手机依赖Termux及同来源的Termux:API，首次安装可联网；局域网演示不依赖外网。

源码位于EdgeHybrid_Source_v28.zip。BUILD_INFO.json与runtime/build_info.json记录实际构建身份。旧报告的性能结果对应原始实验批次，本软件重建不产生新的性能统计。完整包的物理独立部署与视频按另行验收记录理解。
''', encoding='utf-8')
    files = [Path(p) for p in source_manifest.collect(str(root))]
    lines = ''.join(sha(p) + '  ' + p.relative_to(root).as_posix() + '\n' for p in files).encode()
    if hashlib.sha256(lines).hexdigest() != source_hash:
        raise RuntimeError('Source changed while building')
    with zipfile.ZipFile(out / 'EdgeHybrid_Source_v28.zip', 'w', zipfile.ZIP_DEFLATED, compresslevel=6) as z:
        for p in files:
            z.write(p, p.relative_to(root).as_posix())
    (out / 'BUILD_INFO.json').write_text(json.dumps({'version': version, 'source_manifest_sha256': source_hash, 'phone_builds': commands, 'phone_binary_sha256': sha(template / 'edgehybrid-phone'), 'gateway_binary_sha256': sha(out / 'runtime/EdgeHybridPhoneGateway.exe'), 'preinstalled_private_keys': 0, 'fresh_physical_deployment': 'PENDING', 'historical_1_7_2_repeat_torch': 'PASS_USER_OBSERVED'}, ensure_ascii=False, indent=2) + '\n', encoding='utf-8')
    public_files = sorted(p for p in out.rglob('*') if p.is_file())
    assert not any(p.suffix == '.key' or 'local-phone' in p.parts for p in public_files)
    (out / 'SHA256SUMS.txt').write_text(''.join(sha(p) + '  ' + p.relative_to(out).as_posix() + '\n' for p in public_files), encoding='utf-8')
    archive = out.parent / 'EdgeHybrid_Complete_v28.zip'
    if archive.exists():
        raise RuntimeError('Public archive already exists')
    with zipfile.ZipFile(archive, 'w', zipfile.ZIP_DEFLATED, compresslevel=6) as z:
        for p in sorted(out.rglob('*')):
            if p.is_file():
                z.write(p, out.name + '/' + p.relative_to(out).as_posix())
    print(json.dumps({'public_archive': str(archive), 'sha256': sha(archive), 'private_keys': 0, 'version': version}, ensure_ascii=False), flush=True)


def sha(p):
    return hashlib.sha256(p.read_bytes()).hexdigest()


def ps(p, text):
    p.write_text(text, encoding='utf-8-sig', newline='\r\n')


def cmdfile(p, command):
    p.write_text('@echo off\nchcp 65001 >nul\n' + command + '\nif errorlevel 1 pause\n', encoding='utf-8', newline='\r\n')


if __name__ == '__main__':
    main()
