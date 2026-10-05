#!/usr/bin/env python3
"""Recompute the W3 local development checks; does not run formal experiments."""
from collections import Counter
import datetime
import hashlib
import json
from pathlib import Path
import re

ROOT = Path(__file__).resolve().parents[1]
RAW = ROOT / 'results/raw/backend_20261002/w3'
OWNED = [
    'cmd/console/main.go', 'internal/console/server.go', 'internal/console/auth.go',
    'internal/console/jobs.go', 'internal/console/jobs_process.go',
    'internal/console/diagnostics.go', 'internal/console/marker.go',
    'internal/console/handlers_live.go', 'internal/console/handlers_export.go',
    'internal/console/backend_reliability_test.go', 'internal/exports/export.go',
    'internal/exports/export_test.go', 'internal/exports/backend_reliability_test.go',
    'internal/evidence/json.go', 'internal/evidence/atomic.go',
    'internal/evidence/backend_reliability_test.go', 'scripts/backend_w3_report.py',
]

def read_run(name):
    folder = RAW / name
    command = json.loads((folder / 'command.json').read_text(encoding='utf-8'))
    events = [json.loads(line) for line in (folder / 'stdout.txt').read_text(encoding='utf-8').splitlines()]
    counts = Counter()
    for event in events:
        if event.get('Test') and '/' not in event['Test'] and event['Action'] in ('pass', 'fail', 'skip'):
            counts[event['Action'].upper()] += 1
    text = ''.join(event.get('Output', '') for event in events)
    fuzz = [int(x) for x in re.findall(r'execs: (\d+)', text)]
    benchmarks = []
    for line in text.splitlines():
        match = re.search(r'(BenchmarkBackend\w+)-\d+\s+(\d+)\s+(\d+) ns/op.*?([\d]+) B/op\s+([\d]+) allocs/op', line)
        if match:
            benchmarks.append(dict(name=match[1], iterations=int(match[2]), ns_per_op=int(match[3]),
                                   bytes_per_op=int(match[4]), allocs_per_op=int(match[5])))
    return dict(command=command, status='PASS' if command['exit_code'] == 0 else 'FAIL',
                top_test_counts=dict(counts), fuzz_inputs=max(fuzz) if fuzz else None, benchmarks=benchmarks)

def main():
    names = ['before_counterexamples', 'after_counterexamples_and_contracts',
             'after_counterexamples_and_contracts_corrected', 'before_correlation_counterexample',
             'final_w3_tests', 'final_w3_tests_with_bounds', 'integrated_state_diagnostics', 'fuzz_management_5s',
             'fuzz_export_5s', 'fuzz_export_final_5s', 'micro_export_2s_3', 'micro_export_final_1s_3']
    results = {name: read_run(name) for name in names}
    source = {path: hashlib.sha256((ROOT / path).read_bytes()).hexdigest() for path in OWNED}
    report = dict(schema='edgehybrid-backend-w3/1', generated_utc=datetime.datetime.now(datetime.timezone.utc).isoformat(),
                  source_scope='W3-owned files, not the final full-project freeze', source_files=source, checks=results,
                  formal_sampling=False, optimization_claim=None,
                  limitations=['Windows race is separately reported; these are normal Go checks.',
                               'Go fuzz execution counts are parser inputs, not protocol attacks.',
                               'Microbenchmarks measure synthetic 2000-event serialization/parsing only; no before/after speedup claim.',
                               'Receipt persistence and actual child process exit are separate fields.',
                               'Directory durability across power loss is not claimed by atomic marker publication.'])
    destination = RAW / 'W3_RECOMPUTED.json'
    destination.write_text(json.dumps(report, ensure_ascii=False, indent=2)+'\n', encoding='utf-8')
    print(json.dumps({'report': str(destination), 'final_go_status': results['final_w3_tests_with_bounds']['status'],
                      'counts': results['final_w3_tests_with_bounds']['top_test_counts']}, ensure_ascii=True))
    return 0 if results['final_w3_tests_with_bounds']['status'] == 'PASS' else 1

if __name__ == '__main__':
    raise SystemExit(main())
