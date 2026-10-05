#!/usr/bin/env python3
"""Create separate executable/user and source/review ZIPs with exact manifests.

No runtime/private/key directory is traversed. Review evidence is selected
explicitly, without historical archives or executables.
"""
import argparse
import hashlib
import importlib.util
import json
from pathlib import Path
import zipfile

DENIED_PARTS = {'runtime', 'private', 'keys', 'secrets', 'certs', '.git', '__pycache__', 'node_modules', '.cache', 'backup', 'backups'}
DENIED_EXT = {'.key', '.pem', '.pfx', '.p12', '.pyc', '.dll', '.so', '.a', '.test', '.zip'}

def digest(b):
    return hashlib.sha256(b).hexdigest()

def safe(name, executable=False):
    p = Path(name)
    return not p.is_absolute() and '..' not in p.parts and not any(x.lower() in DENIED_PARTS for x in p.parts) and p.suffix.lower() not in DENIED_EXT and (p.suffix.lower() != '.exe' or executable and name == 'EdgeHybridDemo.exe')

def write_zip(path, contents, executable):
    if path.exists():
        raise RuntimeError('refusing to overwrite existing ZIP: ' + str(path))
    for name in contents:
        if not safe(name, executable):
            raise RuntimeError('excluded/unsafe package entry: ' + name)
    manifest = ''.join(digest(contents[n]) + '  ' + n + '\n' for n in sorted(contents))
    with zipfile.ZipFile(path, 'x', zipfile.ZIP_DEFLATED) as z:
        for n, b in sorted(contents.items()):
            z.writestr(n, b)
        z.writestr('MANIFEST.sha256', manifest)
    count = verify(path, executable)
    return dict(path=str(path.resolve()), bytes=path.stat().st_size, sha256=digest(path.read_bytes()), manifest_files=count)

def verify(path, executable):
    with zipfile.ZipFile(path) as z:
        names = z.namelist()
        if len(names) != len(set(names)):
            raise RuntimeError('duplicate ZIP entry')
        expected = {}
        for line in z.read('MANIFEST.sha256').decode('utf-8').splitlines():
            h, n = line.split('  ', 1)
            if n in expected or not safe(n, executable):
                raise RuntimeError('bad manifest entry')
            expected[n] = h
        if set(names) != set(expected) | {'MANIFEST.sha256'}:
            raise RuntimeError('manifest coverage mismatch')
        for n, h in expected.items():
            if digest(z.read(n)) != h:
                raise RuntimeError('manifest hash mismatch: ' + n)
        if executable and 'EdgeHybridDemo.exe' not in expected:
            raise RuntimeError('user distribution is missing executable')
        return len(expected)

def add_tree(contents, root, directory):
    # These are explicit source/evidence roots. Filter names before reading.
    for p in sorted(directory.rglob('*')):
        if p.is_file():
            name = p.relative_to(root).as_posix()
            if safe(name):
                if p.is_symlink():
                    raise RuntimeError('symlink cannot be packaged')
                contents[name] = p.read_bytes()

def main():
    ap = argparse.ArgumentParser()
    ap.add_argument('--user-dir', type=Path)
    ap.add_argument('--user-zip', type=Path)
    ap.add_argument('--review-zip', type=Path)
    ap.add_argument('--evidence-dir', type=Path)
    ap.add_argument('--verify', type=Path)
    ap.add_argument('--executable', action='store_true')
    ap.add_argument('--extract-to', type=Path)
    a = ap.parse_args()
    root = Path(__file__).resolve().parents[1]
    if a.verify:
        n = verify(a.verify, a.executable)
        if a.extract_to:
            target = a.extract_to.resolve()
            target.mkdir(parents=True, exist_ok=False)
            with zipfile.ZipFile(a.verify) as z:
                for name in z.namelist():
                    if not (target / name).resolve().is_relative_to(target):
                        raise RuntimeError('extraction escaped target')
                z.extractall(target)
        print(json.dumps(dict(verified=str(a.verify.resolve()), manifest_files=n), ensure_ascii=True))
        return 0
    info = {}
    if a.user_zip:
        contents = {}
        for p in sorted(a.user_dir.rglob('*')):
            if p.is_file():
                n = p.relative_to(a.user_dir).as_posix()
                if not safe(n, True) or p.is_symlink():
                    raise RuntimeError('unsafe distribution entry: ' + n)
                contents[n] = p.read_bytes()
        info['user'] = write_zip(a.user_zip, contents, True)
    if a.review_zip:
        spec = importlib.util.spec_from_file_location('source_manifest', root / 'scripts/source_manifest.py')
        m = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(m)
        contents = {Path(p).relative_to(root).as_posix(): Path(p).read_bytes() for p in m.collect(str(root))}
        for name in ['DEMO_TEST_REPORT.md', 'results/milestone_M5B_DEMO.md']:
            contents[name] = (root / name).read_bytes()
        add_tree(contents, root, a.evidence_dir.resolve())
        # Fixed M5B offline controls are historical regression inputs, not live
        # page data. Keep the originals required to reproduce these verifiers.
        add_tree(contents, root, root / 'results/raw/m5b_corrected_20260930/smoke_r4_data')
        for name in ['source_frozen_r4.sha256', 'source_frozen_r4.json', 'final_summary_r4.json']:
            p = root / 'results/raw/m5b_corrected_20260930' / name
            contents[p.relative_to(root).as_posix()] = p.read_bytes()
        contents['results/M5B_HISTORICAL_CONTROLS.md'] = '这些 M5B Corrected 原始目录仅供离线回归；页面不读取它们，也不把其数据称为本轮实时演示。源码 hash 为原生产源码，不是新演示 hash。\n'.encode('utf-8')
        info['review'] = write_zip(a.review_zip, contents, False)
        # The root manifest is a delivery artifact; not part of source hash.
        with zipfile.ZipFile(a.review_zip) as z:
            (root / 'MANIFEST.sha256').write_bytes(z.read('MANIFEST.sha256'))
    print(json.dumps(info, ensure_ascii=True))
    return 0

if __name__ == '__main__':
    raise SystemExit(main())
