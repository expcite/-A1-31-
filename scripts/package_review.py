#!/usr/bin/env python3
"""Package public source/evidence with a complete SHA-256 manifest; verify ZIP.

The source root is always this script's project, even when invoked elsewhere.
Runtime, key/certificate directories, caches, binaries and backups are excluded
before file contents are read. Existing archives are never overwritten.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import sys
import zipfile

EXCLUDED_PARTS = {'runtime', 'secrets', 'keys', 'certs', '.git', '__pycache__',
                  'node_modules', '.cache', 'cache', 'backups', 'backup'}
EXCLUDED_EXT = {'.exe', '.dll', '.so', '.a', '.test', '.key', '.pem', '.pfx', '.p12', '.pyc', '.zip'}

def excluded(relative):
    return any(p.lower() in EXCLUDED_PARTS for p in relative.parts) or relative.suffix.lower() in EXCLUDED_EXT

def collect(root):
    for base, dirs, files in os.walk(root):
        dirs[:] = [d for d in dirs if d.lower() not in EXCLUDED_PARTS]
        for name in files:
            path = Path(base) / name
            relative = path.relative_to(root)
            if excluded(relative) or relative.as_posix() == 'MANIFEST.sha256':
                continue
            if path.is_symlink():
                raise RuntimeError('symlink is not packagable: ' + str(relative))
            yield path

def digest(b):
    return hashlib.sha256(b).hexdigest()

def verify(path):
    with zipfile.ZipFile(path) as archive:
        prefix = 'edgehybrid-agent-starter/'
        contents = archive.namelist()
        if len(contents) != len(set(contents)):
            raise RuntimeError('duplicate archive entry')
        expected = {}
        for line in archive.read(prefix + 'MANIFEST.sha256').decode('utf-8').splitlines():
            hash_value, name = line.split('  ', 1)
            if name in expected:
                raise RuntimeError('duplicate manifest entry')
            expected[name] = hash_value
        actual = {x[len(prefix):] for x in contents if x != prefix + 'MANIFEST.sha256'}
        if actual != set(expected):
            raise RuntimeError('manifest does not cover exact archive contents')
        for name, hash_value in expected.items():
            relative = Path(name)
            if relative.is_absolute() or '..' in relative.parts or excluded(relative):
                raise RuntimeError('unsafe or excluded archive entry: ' + name)
            if digest(archive.read(prefix + name)) != hash_value:
                raise RuntimeError('SHA-256 mismatch: ' + name)
        return len(expected)

def main():
    ap = argparse.ArgumentParser()
    ap.add_argument('--zip', type=Path)
    ap.add_argument('--verify', type=Path)
    ap.add_argument('--extract-to', type=Path)
    args = ap.parse_args()
    root = Path(__file__).resolve().parents[1]
    if args.zip:
        out = args.zip.resolve()
        if out.exists():
            ap.error('archive already exists; refusing to overwrite')
        paths = sorted(collect(root), key=lambda p: p.relative_to(root).as_posix())
        manifest = ''.join(digest(p.read_bytes()) + '  ' + p.relative_to(root).as_posix() + '\n' for p in paths)
        # The published root manifest has exactly the same bytes as the ZIP.
        (root / 'MANIFEST.sha256').write_text(manifest, encoding='utf-8', newline='\n')
        with zipfile.ZipFile(out, 'x', zipfile.ZIP_DEFLATED) as archive:
            for path in paths:
                archive.write(path, 'edgehybrid-agent-starter/' + path.relative_to(root).as_posix())
            archive.writestr('edgehybrid-agent-starter/MANIFEST.sha256', manifest)
        n = verify(out)
        print(json.dumps(dict(zip=str(out), bytes=out.stat().st_size, sha256=digest(out.read_bytes()), manifest_files=n), ensure_ascii=True))
        return 0
    if args.verify:
        path = args.verify.resolve()
        n = verify(path)
        if args.extract_to:
            target = args.extract_to.resolve()
            target.mkdir(parents=True, exist_ok=False)
            with zipfile.ZipFile(path) as archive:
                for name in archive.namelist():
                    destination = (target / name).resolve()
                    if not destination.is_relative_to(target):
                        raise RuntimeError('archive destination escaped verification directory')
                archive.extractall(target)
        print(json.dumps(dict(verified=str(path), manifest_files=n), ensure_ascii=True))
        return 0
    ap.error('--zip or --verify required')

if __name__ == '__main__':
    sys.exit(main())
