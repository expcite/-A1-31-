#!/usr/bin/env python3
"""Revalidate with production CLIs and recompute reporting from immutable raw."""
import argparse
from collections import Counter
import hashlib
import json
import math
from pathlib import Path
import subprocess
import sys

def read(path):
    return json.loads(path.read_text(encoding='utf-8'))

def lines(path):
    return [json.loads(x) for x in path.read_text(encoding='utf-8').splitlines() if x.strip()]

def write(path, data):
    path.write_text(json.dumps(data, ensure_ascii=False, indent=2), encoding='utf-8')

def distribution(values):
    v = sorted(values)
    if not v:
        return dict(samples=0, mean_ns=None, median_ns=None, p95_ns=None, unavailable_reason='no authenticated successful handshake samples')
    n = len(v)
    med = v[n//2] if n%2 else (v[n//2-1] + v[n//2])//2
    return dict(samples=n, mean_ns=sum(v)//n, median_ns=med, p95_ns=v[math.ceil(.95*n)-1])

def tls_ok(f, peer):
    return f and f['handshake_complete'] and not f['did_resume'] and f['version_name']=='TLS1.3' and f['curve_name']=='X25519MLKEM768' and f['verified_chain_len'] >= 2 and f['peer_common_name']==peer

def main():
    ap = argparse.ArgumentParser()
    ap.add_argument('--freeze', type=Path, required=True)
    ap.add_argument('--data', type=Path, required=True)
    ap.add_argument('--out', type=Path, required=True)
    a = ap.parse_args()
    root = Path(__file__).resolve().parents[1]
    f = read(a.freeze)
    index = read(a.data / 'results_index.json')
    out = a.out.resolve()
    out.mkdir(parents=True, exist_ok=False)
    report = dict(plan_id=f['plan_id'], source_manifest_sha256=f['source_manifest_sha256'], binary_hashes={k:v['sha256'] for k,v in f['binaries'].items()}, handshake=[], resource=[], throughput=[], production_verifications=[])
    for entry in index:
        directory = Path(entry['result_dir'])
        binary = Path(f['binaries']['pbench' if entry['mode']=='P' else 'bench']['path'])
        cmd = [str(binary), '-verify', '-dir', str(directory), '-exit-code', str(entry['producer_command']['exit_code'])]
        case = out / ('verify_' + entry['name'])
        case.mkdir()
        with (case / 'stdout.txt').open('wb') as stdout, (case / 'stderr.txt').open('wb') as stderr:
            p = subprocess.run(cmd, cwd=root, stdout=stdout, stderr=stderr, timeout=120)
        verification = dict(name=entry['name'], command=cmd, exit_code=p.returncode)
        write(case / 'command.json', verification)
        report['production_verifications'].append(verification)
        if p.returncode:
            raise RuntimeError('production validator rejected ' + entry['name'])
        m = read(directory / 'manifest.json')
        if m['source_manifest_sha256']!=f['source_manifest_sha256'] or m['run_id']!=entry['run_id'] or m['run_kind']!='formal':
            raise RuntimeError('source/run/formal mismatch')
        common = dict(mode=entry['mode'], run_id=entry['run_id'], result_dir=str(directory), config_hash=entry['config_hash'], producer_exit_code=entry['producer_command']['exit_code'], status=read(directory / 'status.json')['status'], evidence_status=read(directory / 'validation.json')['evidence_status'])
        if entry['experiment']=='handshake':
            public = entry['mode']=='P'
            trials = lines(directory / ('p_trials.jsonl' if public else 'trials.jsonl'))
            successful = [t for t in trials if (tls_ok(t['business'].get('client_tls'), 'gateway.edgehybrid.test') and tls_ok(t['business'].get('server_tls'), 'D001')) if public] if public else [t for t in trials if t['handshake_success']]
            hs = len(successful)
            business = sum(t['success'] for t in trials)
            dist = distribution([t['handshake_total_ns'] for t in successful])
            summary = read(directory / ('p_summary.json' if public else 'summary.json'))
            if summary['successes'] != business:
                raise RuntimeError('raw business count differs from validated summary')
            if not public:
                for raw, field in [('mean_ns','handshake_mean_ns'), ('median_ns','handshake_median_ns'), ('p95_ns','handshake_p95_ns')]:
                    if dist[raw]!=summary[field]:
                        raise RuntimeError('raw handshake distribution differs from validated summary')
            wire = [dict(trial_id=t['trial_id'], client_to_server_bytes=t['business']['wire_c2s_base'] if public else t['handshake_client_to_server_bytes'], server_to_client_bytes=t['business']['wire_s2c_base'] if public else t['handshake_server_to_client_bytes']) for t in successful]
            avg_wire = dict(client_to_server_bytes=sum(x['client_to_server_bytes'] for x in wire)/len(wire) if wire else None, server_to_client_bytes=sum(x['server_to_client_bytes'] for x in wire)/len(wire) if wire else None, layer='user-space framing/TLS; excludes IP/TCP')
            report['handshake'].append(dict(common, requested=1000, executed=len(trials), not_started=1000-len(trials), observed_failed_attempts=len(trials)-business, handshake_successes=hs, handshake_success_pct=hs/1000*100, business_successes=business, business_success_pct=business/1000*100, all_1000_handshakes_successful=hs==1000, all_1000_business_transactions_successful=business==1000, handshake_latency=dist, handshake_wire_mean=avg_wire, failure_stages=dict(Counter(t.get('error_stage','unspecified') for t in trials if not t['success'])), per_handshake_cpu_ns=None, per_handshake_peak_working_set_bytes=None, resource_unavailable_reason='role-shared process in handshake experiment; resource batches are separate, no per-trial attribution'))
        elif entry['experiment']=='resource':
            role_rows = lines(directory / 'resource_trials.jsonl')
            trials = lines(directory / 'device/device_trials.jsonl')
            report['resource'].append(dict(common, batch=entry['batch'], requested_handshakes=100, executed_handshakes=len(trials), successful_handshakes=sum(t['handshake_success'] for t in trials), roles=role_rows, per_handshake_cpu_ns=None, per_handshake_peak_working_set_bytes=None, unavailable_reason='100-handshake process batch; no division into per-handshake CPU/peak', empty_process_baseline=None, empty_process_baseline_reason='not implemented; no subtraction'))
        else:
            public=entry['mode']=='P'
            rounds=lines(directory / ('p_throughput_rounds.jsonl' if public else 'throughput_rounds.jsonl'))
            summary=read(directory / ('p_throughput_summary.json' if public else 'throughput_summary.json'))
            completed=sum(r['completed_transactions'] for r in rounds)
            accepted_bytes=sum(r['accepted_telemetry_plaintext_bytes'] for r in rounds)
            if completed!=summary['total_completed'] or accepted_bytes!=summary['total_accepted_telemetry_plaintext_bytes']:
                raise RuntimeError('raw throughput counts differ from validated summary')
            rates=[r for r in rounds if r['application_goodput_bytes_per_sec'] is not None and r['transactions_per_sec'] is not None]
            avg_g=sum(r['application_goodput_bytes_per_sec'] for r in rates)/len(rates) if rates else None
            avg_t=sum(r['transactions_per_sec'] for r in rates)/len(rates) if rates else None
            # JSON float round-trips and language summation can differ by one
            # ULP. Production Go validation already checks the exact summary.
            same=lambda a,b: a is None and b is None or a is not None and b is not None and math.isclose(a,b,rel_tol=1e-12,abs_tol=1e-9)
            if not same(avg_g,summary['avg_application_goodput_bytes_per_sec']) or not same(avg_t,summary['avg_transactions_per_sec']):
                raise RuntimeError('raw throughput rates differ from validated summary')
            report['throughput'].append(dict(common, requested_rounds=5, requested_transactions=1000, executed_rounds=len(rounds), completed_transactions=completed, accepted_telemetry_plaintext_bytes=accepted_bytes, avg_application_goodput_bytes_per_sec=avg_g, avg_transactions_per_sec=avg_t, aggregation='arithmetic mean of valid per-round rates; unchanged M5B', rounds=rounds))
    report['unsupported']=dict(P_role_cpu_ns=None, P_role_peak_working_set_bytes=None, reason='P roles share one process; no attribution', ip_tcp_overhead_bytes=None, ip_tcp_reason='user-space counters exclude kernel headers', gateway_receive_window_ns=None, gateway_reason='not measured on gateway monotonic clock', Linux_resources='NOT_TESTED', race='historical validation gap; no new race execution', independent_SM2_key_exchange='pending; no new independent exchange validation')
    report['execution_status']=read(a.data / 'execution_status.json')
    write(out / 'recomputed_report.json', report)
    text = ['# M5 正式实验报告', '', '以下数值由逐次 raw 重算，并调用生产完整验收器再次核验。观察结果仅适用于本机软件原型；formal 资格不等于安全认证。', '', '|模式|执行/计划|握手成功率|业务成功率|握手均值 ms|中位数 ms|P95 ms|状态|', '|---|---:|---:|---:|---:|---:|---:|---|']
    for h in report['handshake']:
        d=h['handshake_latency']
        fmt=lambda x:'null' if x is None else f'{x/1e6:.4f}'
        text.append(f"|{h['mode']}|{h['executed']}/1000|{h['handshake_success_pct']:.2f}%|{h['business_success_pct']:.2f}%|{fmt(d['mean_ns'])}|{fmt(d['median_ns'])}|{fmt(d['p95_ns'])}|{h['status']}/{h['evidence_status']}|")
    text += ['', '全部 1000 次成功仅是本批次事实，不推断未来成功概率。分母固定 1000；失败、超时、中断和未启动分别保存。详细失败阶段、wire、配置/run_id/hash、资源原始快照与逐轮窗口见 recomputed_report.json 和原始目录。', '', '|模式|完成事务/计划|业务 goodput B/s（有效轮算术平均）|TPS|状态|', '|---|---:|---:|---:|---|']
    for t in report['throughput']:
        text.append(f"|{t['mode']}|{t['completed_transactions']}/1000|{t['avg_application_goodput_bytes_per_sec']}|{t['avg_transactions_per_sec']}|{t['status']}/{t['evidence_status']}|")
    text += ['', '资源为每模式 5 个独立批次，各 100 次完整握手、gateway/device 分进程。CPU 是整批进程启动至采样累计；设备循环窗口另列。工作集峰值是进程高水位。它们均不作为单次握手 CPU/峰值。', '', '|模式/批次|角色|PID|整批进程 CPU ms|进程峰值工作集 bytes|', '|---|---|---:|---:|---:|']
    for r in report['resource']:
        for role in r['roles']:
            cpu=role['process_cpu_total_ns']
            text.append(f"|{r['mode']}/{r['batch']}|{role['role']}|{role['pid']}|{'null' if cpu is None else cpu/1e6}|{role['peak_working_set_bytes']}|")
    text += ['', 'T/H 使用国密协议，P 使用 TLS1.3、X25519MLKEM768、ECDSA 证书和 TLS KDF/AES-GCM；认证载体、记录协议、日志以及进程组织存在差异，不将整体差异全部归因于 ML-KEM。吞吐含真实业务策略、日志可见性及约 1ms 本机确认事件观察成本，非算法上限或跨机结果。', '', '单次握手 CPU/RSS、P 分角色 CPU/RSS、IP/TCP 开销、网关接收窗口、空进程基线均 null+原因；Linux NOT_TESTED。race、独立 SM2 交换验证保持历史缺口，没有新增安全审计/证明/合规认证。', '', '源码清单 SHA-256：`' + f['source_manifest_sha256'] + '`。本轮全部实际命令、退出码、二进制构建信息、配置哈希与 run_id 均在冻结及执行索引中。M5 完成后停止，未执行 M6。', '']
    (out / 'REPORT.md').write_text('\n'.join(text), encoding='utf-8')
    print('Raw report recomputed; ' + str(len(index)) + ' production CLI verifications passed.')
    return 0

if __name__ == '__main__':
    raise SystemExit(main())
