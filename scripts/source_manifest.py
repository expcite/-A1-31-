#!/usr/bin/env python3
"""Write a SHA-256 manifest of the project SOURCE for one run.

Why this exists: the M2 review pointed out that matching hashes prove only that
files match a list, and that FILE MODIFICATION TIMES ARE NOT A SUBSTITUTE for
tying a log to the source that produced it. So each run gets its own manifest of
the source tree, and the run info records the manifest's own hash.

Scope: source, tests, configuration templates, scripts, protocol documents and
the project metadata. It deliberately EXCLUDES:
  * results/raw/          - logs produced BY the run; including them would make
                            the manifest describe its own output
  * runtime/              - runtime key material
  * .git/                 - not present
  * build caches, binaries

Usage:
  source_manifest.py [--root DIR] [--out FILE] [--json FILE]
"""
import argparse
import hashlib
import json
import os
import sys

INCLUDE_DIRS = ("cmd", "internal", "configs", "scripts", "docs", ".github")
INCLUDE_ROOT_FILES = (
    "go.mod", "go.sum", "README.md", "VERSION", ".gitignore", ".gitattributes",
    "main.go", "LICENSE.txt", "THIRD_PARTY_NOTICES.md",
)

EXCLUDE_PARTS = (".git", "runtime", "__pycache__", "node_modules")
EXCLUDE_EXT = {".exe", ".test", ".dll", ".so", ".a", ".key", ".pem", ".zip", ".pyc"}


def sha256_file(path):
    h = hashlib.sha256()
    with open(path, "rb") as fh:
        for chunk in iter(lambda: fh.read(1 << 20), b""):
            h.update(chunk)
    return h.hexdigest()


def looks_binary(path):
    try:
        with open(path, "rb") as fh:
            return b"\x00" in fh.read(8192)
    except OSError:
        return True


def collect(root):
    out = []
    for name in INCLUDE_ROOT_FILES:
        p = os.path.join(root, name)
        if os.path.isfile(p):
            out.append(p)
    for d in INCLUDE_DIRS:
        base = os.path.join(root, d)
        if not os.path.isdir(base):
            continue
        for dirpath, dirnames, filenames in os.walk(base):
            dirnames[:] = [x for x in dirnames if x not in EXCLUDE_PARTS]
            for fn in filenames:
                p = os.path.join(dirpath, fn)
                rel = os.path.relpath(p, root).replace(os.sep, "/")
                if any(part in EXCLUDE_PARTS for part in rel.split("/")):
                    continue
                if os.path.splitext(fn)[1].lower() in EXCLUDE_EXT:
                    continue
                if looks_binary(p):
                    continue
                out.append(p)
    return sorted(set(out))


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--root", default=".")
    ap.add_argument("--out")
    ap.add_argument("--json")
    args = ap.parse_args()

    root = os.path.abspath(args.root)
    files = collect(root)
    lines = []
    for p in files:
        rel = os.path.relpath(p, root).replace(os.sep, "/")
        lines.append(f"{sha256_file(p)}  {rel}")
    text = "\n".join(lines) + "\n"

    manifest_hash = hashlib.sha256(text.encode("utf-8")).hexdigest()
    if args.out:
        with open(args.out, "w", encoding="utf-8", newline="\n") as fh:
            fh.write(text)
    payload = {"root": root, "files": len(lines), "manifest_sha256": manifest_hash}
    if args.json:
        with open(args.json, "w", encoding="utf-8") as fh:
            json.dump(payload, fh, indent=2)
    print(f"source files: {len(lines)}")
    print(f"manifest sha256: {manifest_hash}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
