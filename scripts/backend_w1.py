#!/usr/bin/env python3
"""Local, bounded W1 validation using already-installed test-only tools.

No downloads, installation, global environment changes, or performance sampling.
Each invocation requires a new output directory; failed evidence is retained.
"""
import argparse
import datetime as dt
import hashlib
import json
import os
from pathlib import Path
import platform
import subprocess
import sys
import tempfile
import zipfile

from source_manifest import collect, sha256_file


def write_json(path, value):
    path.write_text(json.dumps(value, indent=2, ensure_ascii=False) + "\n", encoding="utf-8")


def snapshot(root, folder, name):
    entries = [f"{sha256_file(p)}  {Path(p).relative_to(root).as_posix()}" for p in collect(str(root))]
    data = ("\n".join(entries) + "\n").encode("utf-8")
    (folder / (name + ".sha256")).write_bytes(data)
    return hashlib.sha256(data).hexdigest()


def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument("--out", required=True)
    p.add_argument("--go", default=r"C:\Users\20477\.workbuddy\binaries\go\go\bin\go.exe")
    p.add_argument("--java", default=r"C:\Program Files\JetBrains\PyCharm 2025.2.3\jbr\bin\java.exe")
    p.add_argument("--javac", default=r"C:\Program Files\JetBrains\PyCharm 2025.2.3\jbr\bin\javac.exe")
    p.add_argument("--bc-jar", default=r"C:\Program Files\JetBrains\PyCharm 2025.2.3\lib\bouncy-castle.jar")
    p.add_argument("--bc-sha256", default="cefef94b35315f4b94c2653a99e38c94ca8b312a59dd61b0d5e956839fa858b9")
    p.add_argument("--openssl", default=r"C:\Users\20477\.workbuddy\binaries\PortableGit\versions\1.2.0\usr\bin\openssl.exe")
    p.add_argument("--fuzz-seconds", type=int, default=20)
    a = p.parse_args()
    if a.fuzz_seconds < 1 or a.fuzz_seconds > 60:
        p.error("bounded fuzz budget must be 1..60 seconds per existing public parser")
    root = Path(__file__).resolve().parents[1]
    out = Path(a.out).resolve()
    out.mkdir(parents=True, exist_ok=False)
    started = dt.datetime.now(dt.timezone.utc).isoformat()
    env = os.environ.copy()
    env.update(GOTOOLCHAIN="local", EDGEHYBRID_BC_JAR=a.bc_jar,
               EDGEHYBRID_BC_SHA256=a.bc_sha256, EDGEHYBRID_JAVA=a.java,
               EDGEHYBRID_JAVAC=a.javac, EDGEHYBRID_OPENSSL=a.openssl,
               PYTHONIOENCODING="utf-8", PYTHONUTF8="1")
    env["PATH"] = str(Path(a.go).parent) + os.pathsep + env.get("PATH", "")
    results = []

    def run(name, argv, timeout=180, expected_packages=()):
        folder = out / name
        folder.mkdir()
        before = snapshot(root, folder, "source_start")
        start = dt.datetime.now(dt.timezone.utc).isoformat()
        blocked, expired = None, False
        with (folder / "stdout.txt").open("wb") as stdout, (folder / "stderr.txt").open("wb") as stderr:
            try:
                child = subprocess.Popen(argv, cwd=root, env=env, stdout=stdout, stderr=stderr)
                try:
                    code = child.wait(timeout=timeout)
                except subprocess.TimeoutExpired:
                    expired = True
                    if os.name == "nt":
                        subprocess.run(["taskkill", "/PID", str(child.pid), "/T", "/F"], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, timeout=10)
                    if child.poll() is None:
                        child.kill()
                    code = child.wait(timeout=10)
            except OSError as e:
                blocked, code = str(e), None
                stderr.write((str(e) + "\n").encode("utf-8"))
        info = dict(command=argv, cwd=str(root), platform=platform.platform(),
                    started_utc=start, finished_utc=dt.datetime.now(dt.timezone.utc).isoformat(),
                    real_exit_code=code, timeout_cleanup=expired, launch_error=blocked,
                    source_start_sha256=before, source_end_sha256=snapshot(root, folder, "source_end"),
                    environment_overrides={k: env[k] for k in ("GOTOOLCHAIN", "EDGEHYBRID_BC_JAR", "EDGEHYBRID_BC_SHA256", "EDGEHYBRID_JAVA", "EDGEHYBRID_JAVAC", "EDGEHYBRID_OPENSSL")})
        info["status"] = "ENVIRONMENT_BLOCKED" if blocked else ("PASS" if code == 0 else "FAIL")
        write_json(folder / "command.json", info)
        (folder / "exit.txt").write_text("null\n" if code is None else str(code) + "\n", encoding="ascii")
        if expected_packages and code is not None:
            verifier = [sys.executable, str(root / "scripts" / "summarize-go-test-json.py"), str(folder / "stdout.txt"), "--go-exit-code", str(code), "--require-tests", "--json", str(folder / "verified.json")]
            for package in expected_packages:
                verifier += ["--expect-package", "example.com/edgehybrid/internal/" + package]
            with (folder / "verifier_stdout.txt").open("wb") as stdout, (folder / "verifier_stderr.txt").open("wb") as stderr:
                verified = subprocess.run(verifier, cwd=root, env=env, stdout=stdout, stderr=stderr, timeout=30)
            write_json(folder / "verifier_command.json", dict(command=verifier, real_exit_code=verified.returncode))
            info["verifier_exit_code"] = verified.returncode
            if verified.returncode:
                info["status"] = "FAIL"
            write_json(folder / "command.json", info)
        results.append(dict(name=name, status=info["status"], real_exit_code=code,
                            command_record=str(folder / "command.json"), source_start_sha256=before,
                            verifier_exit_code=info.get("verifier_exit_code")))
        print(json.dumps(results[-1], ensure_ascii=True), flush=True)
        return code

    run("go_version", [a.go, "version"])
    run("java_version", [a.java, "-version"])
    run("javac_version", [a.javac, "-version"])
    run("openssl_version", [a.openssl, "version", "-a"])
    with tempfile.TemporaryDirectory(prefix="edgehybrid-w1-public-bridge-") as classes:
        compiled = run("oracle_compile", [a.javac, "-classpath", a.bc_jar, "-d", classes, str(root / "scripts" / "sm2_independent" / "SM2ExchangeOracle.java")], timeout=30)
        if compiled == 0:
            run("oracle_metadata", [a.java, "-classpath", a.bc_jar + os.pathsep + classes, "SM2ExchangeOracle", "metadata"], timeout=30)
    jar = Path(a.bc_jar)
    tool_info = {"bc_jar_path": str(jar), "provider_declared_version": "1.80",
                 "official_source_tag": "r1rv80", "official_source": "https://github.com/bcgit/bc-java/tree/r1rv80",
                 "local_origin": "existing JetBrains PyCharm 2025.2.3 repackaged bouncy-castle-provider; NOT an official unmodified distribution jar",
                 "official_jar_hash_claimed": False, "installed_or_downloaded": False,
                 "runtime_dependency": False, "license": "MIT-style Bouncy Castle license"}
    if jar.exists():
        tool_info["actual_sha256"] = sha256_file(jar)
        with zipfile.ZipFile(jar) as z:
            tool_info["class_sha256"] = {n: hashlib.sha256(z.read(n)).hexdigest() for n in (
                "org/bouncycastle/crypto/agreement/SM2KeyExchange.class",
                "org/bouncycastle/crypto/params/SM2KeyExchangePrivateParameters.class",
                "org/bouncycastle/crypto/params/SM2KeyExchangePublicParameters.class",
                "org/bouncycastle/jce/provider/BouncyCastleProvider.class")}
        notices = jar.parents[1] / "license" / "third-party-libraries.json"
        if notices.exists():
            notices_data = json.loads(notices.read_text(encoding="utf-8"))
            matches = []
            def visit(value):
                if isinstance(value, dict):
                    if value.get("name") in ("bouncy-castle-provider", "bouncy-castle-pgp"):
                        matches.append(value)
                    for v in value.values(): visit(v)
                elif isinstance(value, list):
                    for v in value: visit(v)
            visit(notices_data)
            tool_info["installed_distribution_notices"] = matches
            tool_info["installed_notices_sha256"] = sha256_file(notices)
    write_json(out / "independent_tool.json", tool_info)
    run("crypto_security", [a.go, "test", "-count=1", "-json", "-timeout=3m", "./internal/cryptosuite", "./internal/handshake", "./internal/record", "./internal/wire"], timeout=200,
        expected_packages=("cryptosuite", "handshake", "record", "wire"))
    for name, package, target in (("fuzz_sm2_public", "cryptosuite", "FuzzSM2PublicInput"), ("fuzz_frame", "wire", "FuzzFrame")):
        run(name, [a.go, "test", "-count=1", "-json", "-run=^$", "-fuzz=^"+target+"$", "-fuzztime="+str(a.fuzz_seconds)+"s", "-parallel=2", "-timeout=2m", "./internal/"+package],
            timeout=150, expected_packages=(package,))
    independent = []
    standard = []
    crypto_out = out / "crypto_security" / "stdout.txt"
    if crypto_out.exists():
        for line in crypto_out.read_text(encoding="utf-8").splitlines():
            event = json.loads(line)
            output = event.get("Output", "")
            if "SM2STANDARD case=" in output:
                standard.append(dict(item.split("=",1) for item in output.split("SM2STANDARD ",1)[1].strip().split()))
            marker = output.find('{"baseline_changed_when_asserted"')
            if marker >= 0:
                independent.append(json.loads(output[marker:]))
    write_json(out / "independent_cases.json", dict(cases=independent, count=len(independent), shared_outputs_saved=False, random_private_inputs_saved=False,
                                                   curve="sm2p256v1", go_adapter_output_bytes=32, independent_length_bits=256,
                                                   scope="14-case core-only test; current confirmation results are separate", standard_S1_S2=False, project_Finished_is_standard_SM2_confirmation=False))
    write_json(out / "standard_confirmation_cases.json", dict(cases=standard, count=len(standard), standard_S1_S2=True, secret_outputs_saved=False, scope="BC confirmation API plus explicit BC constant-time responder S2 verification; finite sm2p256v1 comparison"))
    final = dict(started_utc=started, finished_utc=dt.datetime.now(dt.timezone.utc).isoformat(),
                 tests=results, current_worktree=True, performance_sampling=False,
                 status="PASS" if all(r["status"] == "PASS" for r in results) and len(independent) == 14 and len(standard) == 12 else "FAIL",
                 independent_case_count=len(independent), standard_confirmation_case_count=len(standard),
                 scope="finite component/core and standard-confirmation cross-check and existing security regression; NOT standards certification")
    write_json(out / "W1_READINESS.json", final)
    return 0 if final["status"] == "PASS" else 1


if __name__ == "__main__":
    sys.exit(main())
