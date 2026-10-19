"""Build a phone-only torch patch. Never export or replace pairing keys."""
from pathlib import Path
import argparse,hashlib,json,os,shutil,subprocess,zipfile
ap=argparse.ArgumentParser(description=__doc__);ap.add_argument('--out',required=True,type=Path);a=ap.parse_args()
root=Path(__file__).resolve().parents[1];out=a.out.resolve();assert not out.exists(),'Use a fresh update directory';out.mkdir(parents=True)
go=os.environ.get('GO') or shutil.which('go');assert go
sha=lambda p:hashlib.sha256(p.read_bytes()).hexdigest()
files=sorted(p for p in root.rglob('*') if p.is_file() and p.relative_to(root).parts[0] not in {'.git','report','verification'} and '__pycache__' not in p.parts and p.suffix not in {'.pyc','.exe','.key','.zip'})
manifest=''.join(sha(p)+'  '+p.relative_to(root).as_posix()+'\n' for p in files);source_hash=hashlib.sha256(manifest.encode()).hexdigest();(out/'SOURCE_MANIFEST.sha256').write_text(manifest,encoding='utf-8',newline='\n')
stage=out/'phone-patch';stage.mkdir();version='interactive-v1.7.1-phone27torch'
cmd=[go,'build','-trimpath','-ldflags',f'-s -w -X main.BuildVersion={version} -X main.SourceHash={source_hash}','-o',str(stage/'edgehybrid-phone'),'./cmd/phone']
r=subprocess.run(cmd,cwd=root,env=dict(os.environ,GOOS='android',GOARCH='arm64',CGO_ENABLED='0',GOTOOLCHAIN='local',GOPROXY='off'),capture_output=True,text=True,encoding='utf-8',timeout=180);assert r.returncode==0,r.stderr
(stage/'run-phone.sh').write_text('''#!/data/data/com.termux/files/usr/bin/bash
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
''',encoding='utf-8',newline='\n')
info={'version':version,'source_manifest_sha256':source_hash,'compatible_gateway':'interactive-v1.7.0-phone27','binary_sha256':sha(stage/'edgehybrid-phone'),'notification':'termux-torch on, hold approximately 500ms, then attempt off even after cancellation','scope':'Only phone binary and launcher update; no config or private-key replacement','physical_automatic_torch_result':'PENDING','manual_termux_torch_result':'USER_CONFIRMED_ON_AND_OFF','build_command':cmd,'build_exit_code':r.returncode,'go_version':subprocess.check_output([go,'version'],text=True).strip()}
(stage/'PHONE_UPDATE_INFO.json').write_text(json.dumps(info,ensure_ascii=False,indent=2)+'\n',encoding='utf-8',newline='\n')
archive=out/'EdgeHybrid_Phone_Torch_Update.zip'
with zipfile.ZipFile(archive,'w',compression=zipfile.ZIP_DEFLATED,compresslevel=6) as z:
 for p in sorted(stage.iterdir()):z.write(p,p.name)
with zipfile.ZipFile(archive) as z:
 assert z.testzip() is None and set(z.namelist())=={'edgehybrid-phone','run-phone.sh','PHONE_UPDATE_INFO.json'}
 assert hashlib.sha256(z.read('edgehybrid-phone')).hexdigest()==info['binary_sha256']
with zipfile.ZipFile(out/'EdgeHybrid_Phone_Torch_Source.zip','w',compression=zipfile.ZIP_DEFLATED,compresslevel=6) as z:
 for p in files:z.write(p,p.relative_to(root).as_posix())
(out/'SHA256SUMS.txt').write_text(sha(archive)+'  '+archive.name+'\n'+sha(out/'EdgeHybrid_Phone_Torch_Source.zip')+'  EdgeHybrid_Phone_Torch_Source.zip\n',encoding='utf-8',newline='\n')
(out/'更新说明.md').write_text('''# 手机端手电筒提示更新

已验证原手机的加密温度上报和10秒间隔控制成功。震动API虽返回成功，但用户未观察到震动。手动Termux手电筒开关已由用户确认可用，本更新将间隔控制后的提示改为约半秒闪灯；自动闪灯仍待实机确认。

将EdgeHybrid_Phone_Torch_Update.zip通过USB复制到手机Download。电脑网关保持现有版本运行，手机先Ctrl+C退出设备程序，再执行：

```bash
cd ~/edgehybrid-phone27
unzip -o ~/storage/downloads/EdgeHybrid_Phone_Torch_Update.zip
bash run-phone.sh
```

更新包仅包含设备程序、启动脚本和构建信息，不含phone.json或runtime目录，沿用现有配对身份。新会话启动后，请在电脑重新下发一次上报间隔；网页收到ACK才确认该次设置，观察手机手电筒是否短亮并熄灭。

手机日志会出现build_identity（版本interactive-v1.7.1-phone27torch）、interval_applied_ack_written和torch_api_returned，错误记为torch_api_failed。torch_api_returned只表示开关API完成，实际灯光仍由现场观察确认。每次终端输出同时保存为手机目录中独立命名的phone-events-*.jsonl。
''',encoding='utf-8',newline='\n')
print('TORCH_PHONE_PATCH_READY',str(archive),archive.stat().st_size,'bytes; no pairing secrets included')
