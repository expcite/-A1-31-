"""Embed genuine frozen historical evidence with its original source hashes."""
import pathlib,json,hashlib
root=pathlib.Path(__file__).resolve().parents[1]
paths=['results/raw/m5_formal_20261001/report_verified/recomputed_report.json','results/m5_formal_readiness.json','results/raw/m6_20261001/innovation/report.json']
report=json.loads((root/paths[0]).read_text(encoding='utf-8-sig'))
orig=[{'path':p,'sha256':hashlib.sha256((root/p).read_bytes()).hexdigest()} for p in paths[:2]]
raw=root/'results/raw/m5_formal_20261001/data'
zeros={}
for mode in ['T','H','P']:
 rows=[]
 for p in (raw/f'handshake_{mode}_1').rglob('*.jsonl'):
  if 'trials' in p.name:
   for line in p.read_text(encoding='utf-8-sig').splitlines():
    if line.strip():rows.append(json.loads(line))
 zeros[mode]={'count':len(rows),'zero_latency_observations':sum(r.get('handshake_total_ns',-1)==0 for r in rows)}
out={'schema_version':'interactive-historical-m5-v1','label':'旧M5正式冻结版本；不是当前交互版本实时性能','platform':'Windows 11 / amd64 / i7-11800H / loopback；原环境详见来源报告','sources':orig,'report':report,'raw_zero_observations':zeros,'limitations':['保留0ns观测；实际时钟分辨率未测定','1000/1000仅代表本批，不代表未来100%','CPU和峰值内存为批次/进程口径，单次指标为null','P与T/H整体差异不能全部归因于ML-KEM','race与独立SM2交换仍pending；未以图形界面补齐','M6 A/B共同规则覆盖16/16、隔离缺陷检出3/3均相同；证据格式可审计性改善，不宣称检出率或人工工时改善']}
dest=root/'internal/console/assets/data/m5_history.json';dest.parent.mkdir(parents=True,exist_ok=True);dest.write_text(json.dumps(out,ensure_ascii=False,indent=2),encoding='utf-8')
print(json.dumps({'embedded':str(dest),'sources':orig,'zero_counts':zeros},ensure_ascii=False))
