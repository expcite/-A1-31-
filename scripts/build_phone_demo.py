"""Build the isolated Windows-hotspot/Android demo without touching v26."""
from pathlib import Path
import argparse,hashlib,json,os,shutil,subprocess,zipfile

ap=argparse.ArgumentParser(description=__doc__);ap.add_argument('--out',required=True,type=Path);args=ap.parse_args()
root=Path(__file__).resolve().parents[1];out=args.out.resolve();assert not out.exists(),'Output already exists; use a new directory'
out.mkdir(parents=True);version='interactive-v1.7.0-phone27';go=os.environ.get('GO') or shutil.which('go');assert go,'Go required'
def digest(p):return hashlib.sha256(p.read_bytes()).hexdigest()
def save(p,v):p.parent.mkdir(parents=True,exist_ok=True);p.write_text(json.dumps(v,ensure_ascii=False,indent=2)+'\n',encoding='utf-8',newline='\n')
files=sorted(p for p in root.rglob('*') if p.is_file() and p.relative_to(root).parts[0] not in {'.git','report','verification'} and '__pycache__' not in p.parts and p.suffix not in {'.pyc','.exe','.key','.zip'})
manifest=''.join(digest(p)+'  '+p.relative_to(root).as_posix()+'\n' for p in files);source_hash=hashlib.sha256(manifest.encode()).hexdigest();(out/'BUILD_SOURCE_MANIFEST.sha256').write_text(manifest,encoding='utf-8',newline='\n')
env=dict(os.environ,GOTOOLCHAIN='local',GOPROXY='off',CGO_ENABLED='0')
commands=[]
def build(target,arch,dest,package):
 cmd=[go,'build','-trimpath','-ldflags',f'-s -w -X example.com/edgehybrid/internal/console.Version={version} -X example.com/edgehybrid/internal/console.SourceHash={source_hash}','-o',str(dest),package]
 result=subprocess.run(cmd,cwd=root,env=dict(env,GOOS=target,GOARCH=arch),capture_output=True,text=True,encoding='utf-8',timeout=180)
 commands.append({'target':target+'/'+arch,'package':package,'command':cmd,'exit_code':result.returncode,'stderr':result.stderr});assert result.returncode==0,result.stderr
 print('BUILT',target,arch,dest.name,flush=True)
exe=out/'EdgeHybridPhoneGateway.exe';build('windows','amd64',exe,'./cmd/phonegateway')
kit=out/'phone-package';prepare=[str(exe),'-phone-out',str(kit),'-private',str(out/'pc-private'),'-listen','192.168.137.1:9443']
result=subprocess.run(prepare,cwd=out,capture_output=True,text=True,encoding='utf-8',timeout=30);assert result.returncode==0,(result.stdout,result.stderr)
assert not (kit/'runtime/keys/GW001').exists()
build('android','arm64',kit/'edgehybrid-phone','./cmd/phone')
(kit/'run-phone.sh').write_text('''#!/data/data/com.termux/files/usr/bin/bash
set -euo pipefail
cd "$(dirname "$0")"
umask 077
chmod 700 . runtime runtime/keys runtime/keys/D001
chmod 600 phone.json runtime/trust.json runtime/keys/D001/*.key
chmod 700 edgehybrid-phone
termux-wake-lock
trap 'termux-wake-unlock' EXIT
if [ "$#" -eq 0 ]; then
  ./edgehybrid-phone -config phone.json -vibrate-on-control -run-for 10m
else
  ./edgehybrid-phone -config phone.json -gateway "$1" -vibrate-on-control -run-for 10m
fi
''',encoding='utf-8',newline='\n')
with zipfile.ZipFile(out/'EdgeHybrid_Phone_v27.zip','w',compression=zipfile.ZIP_DEFLATED,compresslevel=6) as z:
 for p in sorted(kit.rglob('*')):
  if p.is_file():z.write(p,p.relative_to(kit).as_posix())
(out/'phone_gateway.ps1').write_text('''param([string]$HotspotIP='192.168.137.1')
$ErrorActionPreference='Stop'
Set-Location -LiteralPath $PSScriptRoot
if (-not (Get-NetIPAddress -AddressFamily IPv4 -IPAddress $HotspotIP -ErrorAction SilentlyContinue)) { throw "未检测到热点地址 $HotspotIP。请先开启电脑移动热点，让手机连接。" }
Write-Host "手机应连接 $HotspotIP`:9443；电脑网页使用启动窗口的一次口令。"
& (Join-Path $PSScriptRoot 'EdgeHybridPhoneGateway.exe') -listen "$HotspotIP`:9443" -private (Join-Path $PSScriptRoot 'pc-private')
exit $LASTEXITCODE
''',encoding='utf-8-sig',newline='\r\n')
(out/'allow_phone.ps1').write_text('''param([string]$HotspotIP='192.168.137.1')
$ErrorActionPreference='Stop'
if (-not ([Security.Principal.WindowsPrincipal][Security.Principal.WindowsIdentity]::GetCurrent()).IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) { throw '请右键 02_允许手机连接.cmd，选择以管理员身份运行。' }
if (-not (Get-NetIPAddress -AddressFamily IPv4 -IPAddress $HotspotIP -ErrorAction SilentlyContinue)) { throw "未检测到热点地址 $HotspotIP，请先开启移动热点。" }
$program=Join-Path $PSScriptRoot 'EdgeHybridPhoneGateway.exe'
$name='EdgeHybridPhone27-' + (Get-FileHash -LiteralPath $program -Algorithm SHA256).Hash.Substring(0,12)
if (-not (Get-NetFirewallRule -Name $name -ErrorAction SilentlyContinue)) {
 New-NetFirewallRule -Name $name -DisplayName 'EdgeHybrid 手机验证 TCP9443（热点子网）' -Direction Inbound -Action Allow -Program $program -Protocol TCP -LocalPort 9443 -LocalAddress $HotspotIP -RemoteAddress LocalSubnet -Profile Private,Public | Out-Null
}
Write-Host '已允许本验证程序通过热点子网接收手机连接。网页管理端仍只绑定本机。'
''',encoding='utf-8-sig',newline='\r\n')
for name,script in [('01_启动手机验证.cmd','phone_gateway.ps1'),('02_允许手机连接.cmd','allow_phone.ps1')]:
 (out/name).write_text('@echo off\nchcp 65001 >nul\npowershell.exe -NoProfile -ExecutionPolicy Bypass -File "%~dp0'+script+'" %*\npause\n',encoding='utf-8',newline='\r\n')
shutil.copy2(root/'docs/PHONE_DEMO.md',out/'手机联调说明.md');shutil.copytree(root/'docs/licenses',out/'licenses')
with zipfile.ZipFile(out/'EdgeHybrid_Phone_Source_v27.zip','w',compression=zipfile.ZIP_DEFLATED,compresslevel=6) as z:
 for p in files:z.write(p,p.relative_to(root).as_posix())
with zipfile.ZipFile(out/'EdgeHybrid_Phone_v27.zip') as z:assert z.testzip() is None and not any('GW001' in n for n in z.namelist())
save(out/'BUILD_INFO.json',{'version':version,'go_version':subprocess.check_output([go,'version'],text=True).strip(),'source_manifest_sha256':source_hash,'source_files':len(files),'binaries':{'EdgeHybridPhoneGateway.exe':{'sha256':digest(exe),'bytes':exe.stat().st_size},'edgehybrid-phone':{'sha256':digest(kit/'edgehybrid-phone'),'bytes':(kit/'edgehybrid-phone').stat().st_size}},'builds':commands,'phone_private_keys_exported':2,'gateway_private_keys_exported_to_phone':0,'physical_android_execution':'PENDING','cross_hotspot_acceptance':'PENDING','report_v26_modified':False})
targets=['EdgeHybridPhoneGateway.exe','EdgeHybrid_Phone_v27.zip','EdgeHybrid_Phone_Source_v27.zip'];(out/'SHA256SUMS.txt').write_text(''.join(digest(out/name)+'  '+name+'\n' for name in targets),encoding='utf-8',newline='\n')
print('PHONE_DEMO_READY',str(out),flush=True)
