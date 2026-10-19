"""Build a phone-only torch patch. Never export or replace pairing keys."""
from pathlib import Path
import argparse,hashlib,json,os,shutil,subprocess,zipfile
ap=argparse.ArgumentParser(description=__doc__);ap.add_argument('--out',required=True,type=Path);a=ap.parse_args()
root=Path(__file__).resolve().parents[1];out=a.out.resolve();assert not out.exists(),'Use a fresh update directory';out.mkdir(parents=True)
go=os.environ.get('GO') or shutil.which('go');assert go
sha=lambda p:hashlib.sha256(p.read_bytes()).hexdigest()
files=sorted(p for p in root.rglob('*') if p.is_file() and p.relative_to(root).parts[0] not in {'.git','report','verification'} and '__pycache__' not in p.parts and p.suffix not in {'.pyc','.exe','.key','.zip'})
manifest=''.join(sha(p)+'  '+p.relative_to(root).as_posix()+'\n' for p in files);source_hash=hashlib.sha256(manifest.encode()).hexdigest();(out/'SOURCE_MANIFEST.sha256').write_text(manifest,encoding='utf-8',newline='\n')
stage=out/'phone-patch';stage.mkdir();version='interactive-v1.7.2-phone27torch'
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
info={'version':version,'source_manifest_sha256':source_hash,'compatible_gateway':'interactive-v1.7.0-phone27','binary_sha256':sha(stage/'edgehybrid-phone'),'notification':'serialized off -> wait 1s -> on -> hold 1s -> off -> wait 2s; independent notification worker, 8 pending hints maximum','scope':'Only phone binary and launcher update; no config or private-key replacement','physical_automatic_torch_result':'PENDING_NEW_BUILD_REPEAT_TEST','previous_1_7_1_result':'USER_OBSERVED_ONLY_FIRST_PULSE_DESPITE_FIVE_API_RETURNS','manual_termux_torch_result':'USER_CONFIRMED_THREE_REPEATED_PULSES_WITH_RESET_AND_DELAYS','build_command':cmd,'build_exit_code':r.returncode,'go_version':subprocess.check_output([go,'version'],text=True).strip()}
(stage/'PHONE_UPDATE_INFO.json').write_text(json.dumps(info,ensure_ascii=False,indent=2)+'\n',encoding='utf-8',newline='\n')
archive=out/'EdgeHybrid_Phone_Torch_RepeatFix.zip'
with zipfile.ZipFile(archive,'w',compression=zipfile.ZIP_DEFLATED,compresslevel=6) as z:
 for p in sorted(stage.iterdir()):z.write(p,p.name)
with zipfile.ZipFile(archive) as z:
 assert z.testzip() is None and set(z.namelist())=={'edgehybrid-phone','run-phone.sh','PHONE_UPDATE_INFO.json'}
 assert hashlib.sha256(z.read('edgehybrid-phone')).hexdigest()==info['binary_sha256']
with zipfile.ZipFile(out/'EdgeHybrid_Phone_Torch_Source.zip','w',compression=zipfile.ZIP_DEFLATED,compresslevel=6) as z:
 for p in files:z.write(p,p.relative_to(root).as_posix())
(out/'SHA256SUMS.txt').write_text(sha(archive)+'  '+archive.name+'\n'+sha(out/'EdgeHybrid_Phone_Torch_Source.zip')+'  EdgeHybrid_Phone_Torch_Source.zip\n',encoding='utf-8',newline='\n')
(out/'更新说明.md').write_text('''# 手机端手电筒重复提示兼容更新

版本：interactive-v1.7.2-phone27torch。

旧版1.7.1的5次不同操作均有手机ACK及torch_api_returned，但用户只观察到首次亮灯且自动熄灭。手机随后手动执行“off、等1秒、on、等1秒、off、等2秒”连续3轮，用户观察到每轮均亮灯。本版采用这套已经手动验证的时序，作为兼容性调整；具体底层原因尚未证实，新版自动重复亮灯仍待实机验收。

每次新的有效间隔设置后，程序将闪灯提示排队处理，最多8个待处理提示。单独的工作线程按顺序执行开关，不阻塞加密ACK或温度上报；同一个操作ID的重试不会重复提示。队列已满时记录torch_api_failed，间隔设置仍可成功。取消时仍用独立的3秒超时尝试关灯；一次正常提示约需4秒加接口调用耗时。

将EdgeHybrid_Phone_Torch_RepeatFix.zip复制到手机Download。手机先CTRL+C退出旧程序，再执行：

```bash
cd ~/edgehybrid-phone27
unzip -o ~/storage/downloads/EdgeHybrid_Phone_Torch_RepeatFix.zip
bash run-phone.sh
```

启动日志build_identity应显示interactive-v1.7.2-phone27torch。程序默认运行10分钟。电脑网关继续使用现有版本；更新包只替换程序、启动脚本和构建信息，沿用原phone.json与runtime配对文件。

验收：在电脑依次下发10秒、5秒、10秒，每次点击“设置上报间隔”，两次点击相隔至少6秒。每次应先收到网页的安全ACK，再观察到手机灯亮约1秒后熄灭。请确认连续3次均亮灯；间隔设置的成功与灯光实际动作分别记录。每次终端输出会保存为phone-events-*.jsonl。

手机日志包括torch_api_queued、torch_api_started和torch_api_returned，失败为torch_api_failed。API返回只表示命令完成。Termux官方实现会捕获相机访问异常并写入Android日志，再完成接口返回，因此命令返回成功不能作为实际亮灯的证明。源码：https://github.com/termux/termux-api/blob/master/app/src/main/java/com/termux/api/apis/TorchAPI.java 。

原有加密温度上报及间隔控制的实机证据保留；竞赛报告v26未修改。软件回归与打包检查结果位于validation目录。
''',encoding='utf-8',newline='\n')
print('TORCH_PHONE_PATCH_READY',str(archive),archive.stat().st_size,'bytes; no pairing secrets included')
