#!/usr/bin/env python3
"""Frozen local 20-device live stability campaign, NOT handshake performance.

Default: 1800 actual seconds from first observed all-20 Suite2 admission.
Review Python is external; the app receives only Windows system directories in
PATH. Reuses the current gateway acceptance Run authentication fixture.
No credentials/private keys are saved, no mutation is retried, no quota is reset.
Use --resource-probe for static compilation + current Python PID sampling only.
"""
import argparse
from collections import Counter, defaultdict
import ctypes
from ctypes import wintypes
import datetime as dt
import hashlib
import json
import os
from pathlib import Path
import platform
import py_compile
import re
import socket
import subprocess
import sys
import tempfile
import time
import traceback
import urllib.error
import urllib.request

from interactive_gateway_acceptance import Run

PLAN_ID = "edgehybrid-core-final-20261004-v3b"
DEVICE_IDS = tuple("D%03d" % i for i in range(1, 21))
PHASES = ((300, "D001"), (600, "D010"), (900, "GW001"), (1200, "D020"))


def stamp():
    return dt.datetime.now(dt.timezone.utc).isoformat()


def save(path, value):
    with Path(path).open("x", encoding="utf-8", newline="\n") as f:
        json.dump(value, f, ensure_ascii=False, indent=2)
        f.write("\n")
        f.flush()
        os.fsync(f.fileno())


def append_record(path, value):
    with Path(path).open("a", encoding="utf-8", newline="\n") as f:
        f.write(json.dumps(value, ensure_ascii=False, separators=(",", ":")) + "\n")
        f.flush()
        os.fsync(f.fileno())


def sha(path):
    h = hashlib.sha256()
    with Path(path).open("rb") as f:
        for chunk in iter(lambda: f.read(1 << 20), b""):
            h.update(chunk)
    return h.hexdigest()


class ProcessMeter:
    """PID-specific Windows API observations; no installing/altering OS tools."""
    FIELDS = ("cpu_user_ns", "cpu_kernel_ns", "cpu_total_ns", "working_set_bytes",
              "peak_working_set_bytes", "private_commit_bytes", "handles")

    def __init__(self):
        self.available = os.name == "nt"
        if not self.available:
            return
        self.k32 = ctypes.WinDLL("kernel32", use_last_error=True)
        self.psapi = ctypes.WinDLL("psapi", use_last_error=True)
        self.k32.OpenProcess.argtypes = [wintypes.DWORD, wintypes.BOOL, wintypes.DWORD]
        self.k32.OpenProcess.restype = wintypes.HANDLE
        self.k32.CloseHandle.argtypes = [wintypes.HANDLE]
        self.k32.CloseHandle.restype = wintypes.BOOL
        self.k32.GetProcessTimes.argtypes = [wintypes.HANDLE] + [ctypes.POINTER(wintypes.FILETIME)] * 4
        self.k32.GetProcessTimes.restype = wintypes.BOOL
        self.k32.GetProcessHandleCount.argtypes = [wintypes.HANDLE, ctypes.POINTER(wintypes.DWORD)]
        self.k32.GetProcessHandleCount.restype = wintypes.BOOL

        class Memory(ctypes.Structure):
            _fields_ = [("cb", wintypes.DWORD), ("page_faults", wintypes.DWORD)] + [
                (name, ctypes.c_size_t) for name in ("peak_working_set", "working_set",
                    "quota_peak_paged", "quota_paged", "quota_peak_nonpaged", "quota_nonpaged",
                    "pagefile", "peak_pagefile", "private_usage")]
        self.Memory = Memory
        self.psapi.GetProcessMemoryInfo.argtypes = [wintypes.HANDLE, ctypes.POINTER(Memory), wintypes.DWORD]
        self.psapi.GetProcessMemoryInfo.restype = wintypes.BOOL
        self.ip = ctypes.WinDLL("iphlpapi", use_last_error=True)
        self.ip.GetExtendedTcpTable.argtypes = [ctypes.c_void_p, ctypes.POINTER(wintypes.DWORD),
                                               wintypes.BOOL, wintypes.ULONG, ctypes.c_int, wintypes.ULONG]
        self.ip.GetExtendedTcpTable.restype = wintypes.DWORD

    def tcp(self, pid):
        if not self.available:
            return {"tcp_rows": None, "tcp_reason": "Windows GetExtendedTcpTable not available"}
        size = wintypes.DWORD()
        code = self.ip.GetExtendedTcpTable(None, ctypes.byref(size), False, 2, 5, 0)
        if code not in (0, 122) or size.value > 16 << 20:
            return {"tcp_rows": None, "tcp_reason": "GetExtendedTcpTable size returned %d" % code}
        buffer = ctypes.create_string_buffer(max(4, size.value))
        code = self.ip.GetExtendedTcpTable(buffer, ctypes.byref(size), False, 2, 5, 0)
        if code:
            return {"tcp_rows": None, "tcp_reason": "GetExtendedTcpTable returned %d" % code}
        count = ctypes.cast(buffer, ctypes.POINTER(wintypes.DWORD))[0]
        if 4 + count * 24 > size.value:
            return {"tcp_rows": None, "tcp_reason": "TCP owner table length inconsistent"}
        rows = []
        for i in range(count):
            row = (wintypes.DWORD * 6).from_buffer(buffer, 4 + i * 24)
            if row[5] == pid:
                rows.append({"state": int(row[0]), "local_port": socket.ntohs(row[2] & 0xffff),
                             "remote_port": socket.ntohs(row[4] & 0xffff), "pid": pid})
        return {"tcp_rows": rows, "tcp_established": sum(r["state"] == 5 for r in rows),
                "tcp_listening": sum(r["state"] == 2 for r in rows), "tcp_reason": None}

    def sample(self, pid):
        result = {k: None for k in self.FIELDS}
        result.update(pid=pid, observed_at=stamp(), reasons={},
                      scope="whole current process cumulative CPU/current working set/private commit/handles; not per-handshake or leak proof")
        if not self.available:
            result["reasons"]["all"] = "Windows PID resource API not available on this platform"
            return result
        handle = self.k32.OpenProcess(0x1000 | 0x400 | 0x10, False, pid)
        if not handle:
            result["reasons"]["all"] = "OpenProcess failed WinError=%d" % ctypes.get_last_error()
            result.update(self.tcp(pid))
            return result
        try:
            times = [wintypes.FILETIME() for _ in range(4)]
            if self.k32.GetProcessTimes(handle, *(ctypes.byref(x) for x in times)):
                ns = lambda x: ((int(x.dwHighDateTime) << 32) | x.dwLowDateTime) * 100
                result.update(cpu_kernel_ns=ns(times[2]), cpu_user_ns=ns(times[3]),
                              cpu_total_ns=ns(times[2]) + ns(times[3]))
            else:
                result["reasons"]["cpu"] = "GetProcessTimes failed WinError=%d" % ctypes.get_last_error()
            memory = self.Memory()
            memory.cb = ctypes.sizeof(memory)
            if self.psapi.GetProcessMemoryInfo(handle, ctypes.byref(memory), memory.cb):
                result.update(working_set_bytes=int(memory.working_set),
                              peak_working_set_bytes=int(memory.peak_working_set),
                              private_commit_bytes=int(memory.private_usage))
            else:
                result["reasons"]["memory"] = "GetProcessMemoryInfo failed WinError=%d" % ctypes.get_last_error()
            handles = wintypes.DWORD()
            if self.k32.GetProcessHandleCount(handle, ctypes.byref(handles)):
                result["handles"] = handles.value
            else:
                result["reasons"]["handles"] = "GetProcessHandleCount failed WinError=%d" % ctypes.get_last_error()
        finally:
            if not self.k32.CloseHandle(handle):
                result["reasons"]["close"] = "CloseHandle failed WinError=%d" % ctypes.get_last_error()
        result.update(self.tcp(pid))
        return result


def state_summary(state):
    return {k: state[k] for k in ("run_id", "mode", "gateway", "devices", "operations", "capacity",
            "diagnostics", "live_diagnostics", "cursor", "events_omitted", "maintenance", "error") if k in state} | {
                "events_in_snapshot": len(state.get("events", [])),
                "note": "full raw event file retained separately; this request receipt does not duplicate the event ring"}


class ObservedRun(Run):
    def __init__(self, exe, out, count=20):
        self.public_log = None
        super().__init__(exe, out, count)
        if not re.fullmatch(r"interactive-[0-9a-f]{32}", self.run):
            raise AssertionError("unexpected actual run identifier")
        self.info = json.loads((exe.parent / "build_info.json").read_text(encoding="utf-8"))
        for active in (Path(os.environ["LOCALAPPDATA"]) / "EdgeHybridDemo").glob("*/interactive/active.json"):
            if json.loads(active.read_text(encoding="utf-8")).get("run_id") == self.run:
                self.public_log = active.parent / "logs" / (self.run + ".jsonl")
                break
        if self.public_log is None:
            raise AssertionError("current run public event file location unavailable")

    def req(self, path, body=None, expected=200):
        headers = {"Origin": self.url, "Content-Type": "application/json"}
        if body is not None and self.csrf:
            headers["X-CSRF-Token"] = self.csrf
        request = urllib.request.Request(self.url + "/api/v1" + path, headers=headers,
                   data=json.dumps(body).encode("utf-8") if body is not None else None)
        begun = stamp()
        try:
            response = self.client.open(request, timeout=12)
        except urllib.error.HTTPError as e:
            response = e
        raw = response.read()
        status = response.code
        response.close()
        try:
            data = json.loads(raw)
        except (UnicodeError, ValueError):
            data = raw.decode("utf-8", "replace")
        record = {"at": begun, "finished_at": stamp(), "path": path, "status": status,
                  "request": "[REDACTED]" if path == "/login" else body,
                  "response": "[REDACTED]" if path in ("/login", "/session") else
                      (state_summary(data) if path == "/state" and isinstance(data, dict) else data)}
        if len(self.api) >= 4096:
            raise AssertionError("review API receipt budget reached; not silently truncating")
        self.api.append(record)
        append_record(self.out / "api.jsonl", record)
        if status != expected:
            # Login response is never included even if authorization fails.
            raise AssertionError("API %s returned %d, expected %d" % (path, status, expected))
        return data

    def wait_observed(self, predicate, timeout=20):
        deadline = time.monotonic() + timeout
        while True:
            state = self.req("/state")
            if predicate(state):
                return state
            if self.p.poll() is not None or time.monotonic() >= deadline:
                raise AssertionError("observable state wait failed; no mutation retried")
            time.sleep(.1)

    def finish(self):
        cleanup = {}
        if self.p.poll() is None:
            env = os.environ.copy()
            system = os.environ["SystemRoot"]
            env["PATH"] = str(Path(system) / "System32") + os.pathsep + system
            command = [str(self.exe), "-stop"]
            try:
                stopped = subprocess.run(command, cwd=self.exe.parent, env=env,
                          capture_output=True, timeout=12)
                cleanup = {"command": command, "exit_code": stopped.returncode,
                           "stdout": stopped.stdout.decode("utf-8", "replace"),
                           "stderr": stopped.stderr.decode("utf-8", "replace")}
                self.p.wait(timeout=20)
            except Exception as e:
                cleanup.update(error=type(e).__name__ + ": " + str(e), forced_cleanup=True)
                if self.p.poll() is None:
                    subprocess.run(["taskkill", "/PID", str(self.p.pid), "/T", "/F"],
                                   capture_output=True, timeout=10)
                    if self.p.poll() is None:
                        self.p.kill()
                    self.p.wait(timeout=10)
        reader = getattr(self, "thread", None)
        if reader is not None:
            reader.join(timeout=3)
        cleanup.update(app_exit_code=self.p.returncode, reader_exited=reader is not None and not reader.is_alive(),
                       process_exited=self.p.poll() is not None)
        save(self.out / "cleanup.json", cleanup)
        (self.out / "startup_redacted.txt").write_text("".join(getattr(self, "lines", [])), encoding="utf-8")
        try:
            info = json.loads((self.exe.parent / "build_info.json").read_text(encoding="utf-8"))
        except (OSError, ValueError) as e:
            info = {"source_manifest_sha256": None, "source_reason": "build info unavailable: " + str(e)}
        current_run = getattr(self, "run", None)
        # Fixture startup may fail after RUN_ID but before login/build checks.
        # Still collect only that actual run's PUBLIC log, never the active marker.
        if self.public_log is None and current_run and re.fullmatch(r"interactive-[0-9a-f]{32}", current_run):
            for active in (Path(os.environ["LOCALAPPDATA"]) / "EdgeHybridDemo").glob("*/interactive/active.json"):
                try:
                    if json.loads(active.read_text(encoding="utf-8")).get("run_id") == current_run:
                        self.public_log = active.parent / "logs" / (current_run + ".jsonl")
                        break
                except (OSError, ValueError):
                    continue
        save(self.out / "command.json", {"command": self.command, "cwd": str(self.exe.parent),
             "started_utc": self.start, "finished_utc": stamp(), "run_id": current_run,
             "pid": self.p.pid, "exit_code": self.p.returncode,
             "source_manifest_sha256": info["source_manifest_sha256"], "binary_sha256": sha(self.exe),
             "app_PATH": str(Path(os.environ["SystemRoot"]) / "System32") + os.pathsep + os.environ["SystemRoot"],
             "credentials_saved": False})
        if self.public_log and self.public_log.exists():
            destination = self.out / (self.run + ".jsonl")
            destination.write_bytes(self.public_log.read_bytes())
            save(self.out / "public_log_receipt.json", {"run_id": self.run,
                 "relative_file": destination.name, "bytes": destination.stat().st_size,
                 "sha256": sha(destination), "source_manifest_sha256": info["source_manifest_sha256"],
                 "binary_sha256": sha(self.exe), "scope": "public event log only; no active marker or private directory copied"})
        return cleanup


def devices(state):
    return {d["id"]: d for d in state["devices"]}


def all_online(state):
    view = devices(state)
    return set(view) == set(DEVICE_IDS) and all(view[id]["status"] == "online" and
           view[id]["suite"] == 2 and view[id]["sid"] for id in DEVICE_IDS)


def validate_state(state, run, configured=True):
    if state["run_id"] != run or not all_online(state) or state.get("error"):
        raise AssertionError("current run not healthy all-20 real Suite2 online")
    if any(d.get("error") for d in state["devices"]):
        raise AssertionError("device error retained in actual state")
    if configured and any(d["interval_ms"] != 5000 or not d["auto"] or d["temperature_c"] != 25 for d in state["devices"]):
        raise AssertionError("actual timer/automatic source differs from frozen live load")
    cap = state["capacity"]
    if (cap["operations_limit"], cap["queue_limit"], cap["events_limit"], cap["log_bytes_limit"]) != (256, 32, 2000, 20 << 20):
        raise AssertionError("production quotas changed")
    if cap["operations_used"] > 256 or len(state["events"]) > 2000 or cap["log_bytes"] > 20 << 20:
        raise AssertionError("actual quota overflow")
    if any(o["status"] in ("failed", "cancelled", "unconfirmed", "late_confirmed") for o in state["operations"]):
        raise AssertionError("normal live campaign contains failed/cancelled/unconfirmed/late operation")


def healthy_observation(r, label):
    state = r.req("/state")
    if all_online(state):
        return state
    missing = {d["id"] for d in state["devices"] if d["status"] != "online"}
    now = dt.datetime.now(dt.timezone.utc)
    rekeys = [e for e in state["events"] if e["stage"] == "rekey" and e["device_id"] in missing and
              dt.datetime.fromisoformat(e["at"].replace("Z", "+00:00")) >= now - dt.timedelta(seconds=15)]
    observed_ids = {e["device_id"] for e in rekeys}
    record = {"at": stamp(), "label": label, "before": state_summary(state),
              "actual_rekey_events": rekeys, "missing_ids": sorted(missing),
              "automatic_rekey_wait_limit_seconds": 15,
              "explicit_connect_or_retry_issued": False}
    if not missing.issubset(observed_ids):
        append_record(r.out / "automatic_rekey_observations.jsonl", record | {"result": "FAIL"})
        raise AssertionError("unplanned offline sample without actual threshold-rekey evidence")
    try:
        after = r.wait_observed(all_online, timeout=15)
        current = devices(after)
        for id in missing:
            old_sids = {e.get("sid") for e in rekeys if e["device_id"] == id}
            if current[id]["sid"] in old_sids:
                raise AssertionError("automatic rekey recovered using an old SID")
            for endpoint in ("device", "gateway"):
                if not any(e["device_id"] == id and e["endpoint"] == endpoint and e["stage"] == "authenticated"
                           and e.get("sid") == current[id]["sid"] for e in after["events"]):
                    raise AssertionError("automatic rekey lacks dual-end actual new-SID authentication")
        append_record(r.out / "automatic_rekey_observations.jsonl", record | {
             "finished_at": stamp(), "after": state_summary(after), "result": "PASS"})
        return after
    except BaseException:
        append_record(r.out / "automatic_rekey_observations.jsonl", record | {"finished_at": stamp(), "result": "FAIL"})
        raise


def resource_record(r, meter, state, elapsed, phase):
    process = meter.sample(r.p.pid)
    diag = state.get("diagnostics", {})
    live = state.get("live_diagnostics", {})
    states = dict(Counter(o["status"] for o in state["operations"]))
    return {"observed_at": stamp(), "elapsed_seconds": elapsed, "phase": phase, "run_id": r.run,
            "source_manifest_sha256": r.info["source_manifest_sha256"], "binary_sha256": sha(r.exe),
            "windows_process": process, "diagnostics": diag or None,
            "diagnostics_reason": None if diag else "current API does not expose process diagnostics",
            "live_diagnostics": live or None,
            "live_diagnostics_reason": None if live else "current API does not expose live diagnostics",
            "device_states": state["devices"], "gateway": state["gateway"],
            "authenticated_sessions_observed": sum(d["status"] == "online" for d in state["devices"]),
            "operations_by_observed_state": states,
            "pending_ack_count": live.get("pending_ack_count"),
            "pending_ack_reason": live.get("pending_ack_reason", "no independent pending ACK mapping is exposed; operation states are recorded separately"),
            "capacity": state["capacity"], "cursor": state["cursor"],
            "events_omitted": state["events_omitted"],
            "public_log_file_bytes": r.public_log.stat().st_size if r.public_log and r.public_log.exists() else None}


def phase_disconnect(r, id, label, t0, trace, windows):
    before = healthy_observation(r, label + "_before")
    validate_state(before, r.run)
    old = devices(before)[id]["sid"]
    window = {"label": label, "device_ids": [id], "started_at": stamp(),
              "started_elapsed": time.monotonic() - t0, "old_sids": [old]}
    windows.append(window)
    save(r.out / (label + "_before.json"), before)
    r.req("/devices/" + id + "/disconnect", {})
    offline = r.wait_observed(lambda s: devices(s)[id]["status"] == "offline")
    save(r.out / (label + "_offline.json"), offline)
    if any(d["status"] != "online" for d in offline["devices"] if d["id"] != id):
        raise AssertionError("single-device disconnect affected another identity")
    r.req("/devices/" + id + "/connect", {}, 202)
    after = r.wait_observed(lambda s: all_online(s) and devices(s)[id]["sid"] != old)
    validate_state(after, r.run)
    start_count = devices(after)[id]["accepted_samples"]
    resumed = r.wait_observed(lambda s: devices(s)[id]["accepted_samples"] > start_count, timeout=12)
    if not all_online(resumed):
        # A different device may legally hit its unchanged 15-minute age while
        # this target's first 5-second sample arrives. Never explicitly reconnect
        # that other identity; demand its actual rekey/new-SID observations.
        resumed = healthy_observation(r, label + "_resumed")
    validate_state(resumed, r.run)
    save(r.out / (label + "_after.json"), resumed)
    window.update(finished_at=stamp(), finished_elapsed=time.monotonic() - t0,
                  new_sid=devices(resumed)[id]["sid"], result="PASS",
                  accepted_before=devices(before)[id]["accepted_samples"],
                  accepted_after=devices(resumed)[id]["accepted_samples"])
    append_record(trace, window)


def phase_gateway(r, label, t0, trace, windows):
    before = healthy_observation(r, label + "_before")
    validate_state(before, r.run)
    window = {"label": label, "device_ids": list(DEVICE_IDS), "started_at": stamp(),
              "started_elapsed": time.monotonic() - t0,
              "old_sids": [d["sid"] for d in before["devices"]]}
    windows.append(window)
    save(r.out / (label + "_before.json"), before)
    receipt = r.req("/identities/GW001/rotate", {"confirm": "rotate:GW001:ALL"})
    save(r.out / (label + "_receipt.json"), receipt)
    if not (receipt.get("all_sessions_closed") is True and receipt.get("admission_reopened") is True
            and receipt.get("commit", {}).get("registry_committed") is True):
        raise AssertionError("gateway maintenance not actually committed/drained/reopened")
    drained = r.req("/state")
    save(r.out / (label + "_drained.json"), drained)
    if any(d["status"] != "offline" for d in drained["devices"]):
        raise AssertionError("old gateway sessions not fully offline before explicit reconnect")
    live = drained.get("live_diagnostics", {})
    if any(live.get(k) != 0 for k in ("connections_active", "sessions_active", "device_workers_active")):
        raise AssertionError("gateway maintenance does not expose completed resource drain")
    if drained["gateway"]["generation"] != before["gateway"]["generation"] + 1:
        raise AssertionError("gateway generation did not advance once")
    batch = r.req("/run/start", {})
    if batch["attempted"] != 20 or batch["rejected"] != 0:
        raise AssertionError("all 20 identities not explicitly attempted after gateway maintenance")
    after = r.wait_observed(all_online)
    validate_state(after, r.run)
    old_by_id = devices(before)
    new_by_id = devices(after)
    if any(new_by_id[id]["sid"] == old_by_id[id]["sid"] for id in DEVICE_IDS):
        raise AssertionError("gateway rotation reused an old SID")
    gateway = after["gateway"]
    for id in DEVICE_IDS:
        for endpoint in ("device", "gateway"):
            proof = [e for e in after["events"] if e["device_id"] == id and e["endpoint"] == endpoint
                     and e["stage"] == "authenticated" and e.get("sid") == new_by_id[id]["sid"]]
            if not any(e.get("gateway_authenticated", {}).get("KeyGeneration") == gateway["generation"] and
                       e["gateway_authenticated"].get("SignKeyID") == gateway["sign_key_id"] and
                       e["gateway_authenticated"].get("AgreementKeyID") == gateway["agreement_key_id"] for e in proof):
                raise AssertionError("actual new gateway generation lacks dual-end authentication evidence")
    counts = {id: new_by_id[id]["accepted_samples"] for id in DEVICE_IDS}
    resumed = r.wait_observed(lambda s: all(devices(s)[id]["accepted_samples"] > counts[id] for id in DEVICE_IDS), timeout=12)
    validate_state(resumed, r.run)
    save(r.out / (label + "_after.json"), resumed)
    window.update(finished_at=stamp(), finished_elapsed=time.monotonic() - t0,
                  new_sids={id: devices(resumed)[id]["sid"] for id in DEVICE_IDS},
                  old_gateway_generation=before["gateway"]["generation"],
                  new_gateway_generation=gateway["generation"], result="PASS")
    append_record(trace, window)


def controlled_export(r, state):
    # GET /state itself adds a real API audit event after its snapshot. Retain
    # an explicitly excluded 100-event headroom so a full ring's oldest event
    # does not expire before the POST. This is still one fixed, declared range.
    start_index = min(100, len(state["events"]) - 1)
    selected = {"run_id": r.run, "from_event_id": state["events"][start_index]["event_id"],
                "to_event_id": state["cursor"]}
    receipt = r.req("/exports", {"format": "json", "selection": selected}, 201)
    doc = r.req(receipt["url"].removeprefix("/api/v1"))
    # Preserve exact downloaded bytes for the actual returned digest, without
    # creating a second simplified result parser. The server used exports.Validate.
    response = r.client.open(r.url + receipt["url"], timeout=12)
    raw = response.read()
    response.close()
    if hashlib.sha256(raw).hexdigest() != receipt["sha256"] or len(raw) != receipt["bytes"]:
        raise AssertionError("controlled export download differs from actual receipt")
    if doc["selection"] != selected or doc["run_id"] != r.run or doc["source_manifest_sha256"] != r.info["source_manifest_sha256"]:
        raise AssertionError("controlled export source/run/range differs")
    (r.out / "selected_export.json").write_bytes(raw)
    save(r.out / "selected_export_receipt.json", receipt | {
         "scope": "only retained event ring in explicit selection; earlier omitted raw events remain in whole public log",
         "snapshot_events_explicitly_excluded_as_headroom": start_index,
         "earlier_events_omitted": doc["earlier_events_omitted"]})


def verified_final_shutdown(r):
    """Require actual same-run cleanup facts, not a planned stop alone."""
    try:
        stopped = json.loads((r.out / "devices_and_workers_stopped.json").read_text(encoding="utf-8"))
        cleanup = json.loads((r.out / "cleanup.json").read_text(encoding="utf-8"))
        live = stopped["live_diagnostics"]
        return (type(r.p.returncode) is int and r.p.returncode == 0 and
                type(cleanup["app_exit_code"]) is int and cleanup["app_exit_code"] == 0 and
                cleanup["process_exited"] is True and cleanup["reader_exited"] is True and
                stopped["run_id"] == r.run and
                len(stopped["devices"]) == len(DEVICE_IDS) and
                {d["id"] for d in stopped["devices"]} == set(DEVICE_IDS) and
                all(d["status"] == "offline" for d in stopped["devices"]) and
                all(type(live[k]) is int and live[k] == 0 for k in
                    ("connections_active", "sessions_active", "device_workers_active", "local_queue_items", "control_queue_items")))
    except (OSError, ValueError, KeyError, TypeError, AttributeError):
        return False


def planned_shutdown_cancellation(event, windows, run_id, cleanup_verified):
    """Retain a cancelled dial as cancellation, never as successful admission."""
    if (not cleanup_verified or event.get("run_id") != run_id or
            event.get("stage") != "dial_failed" or event.get("endpoint") != "device" or
            event.get("device_id") not in DEVICE_IDS or
            not str(event.get("detail", "")).startswith("dial tcp4 127.0.0.1:") or
            not str(event.get("detail", "")).endswith(": operation was canceled")):
        return False
    try:
        at = dt.datetime.fromisoformat(event["at"].replace("Z", "+00:00"))
        return any(w.get("label") == "planned_final_stop" and
                   event["device_id"] in w["device_ids"] and
                   dt.datetime.fromisoformat(w["started_at"]) <= at <= dt.datetime.fromisoformat(w["finished_at"])
                   for w in windows if "finished_at" in w)
    except (ValueError, TypeError, KeyError):
        return False


def raw_observations(r, windows):
    source = r.out / (r.run + ".jsonl")
    if not source.exists():
        raise AssertionError("final complete public log missing")
    writes, accepts, stages, unexpected, planned, cancellations = {}, {}, Counter(), [], [], []
    shutdown_verified = verified_final_shutdown(r)
    previous = 0
    events = []
    with source.open(encoding="utf-8") as f:
        for line in f:
            event = json.loads(line)
            events.append(event)
    parse_time = lambda value: dt.datetime.fromisoformat(value.replace("Z", "+00:00"))
    automatic_rekey_times = defaultdict(list)
    for e in events:
        if e["stage"] == "rekey":
            automatic_rekey_times[(e["device_id"], e.get("sid"))].append(parse_time(e["at"]))
    for event in events:
            if event["run_id"] != r.run or event["event_id"] <= previous:
                raise AssertionError("whole public log run/order mismatch")
            previous = event["event_id"]
            stages[event["stage"]] += 1
            key = (event["device_id"], event.get("sid"), event.get("sample_id"))
            if event["stage"] == "telemetry_written":
                if event.get("detail") != "Channel.Send返回: <nil>":
                    unexpected.append(event)
                writes[key] = event
            if event["stage"] == "telemetry_accepted":
                if key in accepts:
                    raise AssertionError("duplicate accepted sample within identity/SID")
                accepts[key] = event
            if event["stage"] in ("handshake_rejected", "session_ended", "receive_ended", "offline"):
                expected = any(parse_time(w["started_at"]) <= parse_time(event["at"]) <= parse_time(w.get("finished_at", stamp())) and
                            (event.get("device_id") in w["device_ids"] or not event.get("device_id")) for w in windows)
                # Existing automatic 15-minute/record rekeys are retained as
                # actual events; receive close on the same old SID is expected.
                automatic = any(abs((parse_time(event["at"]) - at).total_seconds()) <= 15 for at in
                                automatic_rekey_times.get((event["device_id"], event.get("sid")), []))
                if expected or (automatic and event["stage"] == "receive_ended"):
                    planned.append(event)
                elif event["stage"] != "offline" or event.get("device_id"):
                    unexpected.append(event)
            if event["stage"] in ("ack_rejected", "business_rejected", "business_admission_rejected",
                                  "local_precheck_rejected", "dial_failed", "rekey_failed",
                                  "admission_rejected", "registry_reload_failed", "unconfirmed"):
                if planned_shutdown_cancellation(event, windows, r.run, shutdown_verified):
                    planned.append(event)
                    cancellations.append({"event_id": event["event_id"], "device_id": event["device_id"],
                                          "observed_sid": event.get("sid"), "classification": "planned_shutdown_cancellation",
                                          "remote_execution_success": None, "reason": "verified final same-run clean stop; cancelled loopback dial retained"})
                else:
                    unexpected.append(event)
    missing = [key for key in accepts if key not in writes]
    wrong = [key for key in accepts if key in writes and accepts[key].get("temperature_c") != writes[key].get("temperature_c")]
    per_device = Counter(key[0] for key in accepts)
    result = {"run_id": r.run, "source_manifest_sha256": r.info["source_manifest_sha256"],
              "binary_sha256": sha(r.exe), "public_log_sha256": sha(source),
              "stage_counts_recomputed": dict(stages), "accepted_per_device": dict(per_device),
              "accepted_matches_actual_channel_write": not missing and not wrong,
              "missing_matching_writes": len(missing), "temperature_mismatches": len(wrong),
              "planned_close_or_interrupt_observations": planned,
              "planned_shutdown_cancellations": cancellations,
              "final_shutdown_cleanup_verified": shutdown_verified,
              "unexpected_failure_or_disconnect_observations": unexpected,
              "scope": "raw event association and observed business progress; NOT another general result-contract validator or performance benchmark"}
    save(r.out / "raw_observations.json", result)
    if missing or wrong or unexpected or any(per_device[id] < 1 for id in DEVICE_IDS):
        raise AssertionError("actual raw communication contains unexpected failures or lacks matching evidence")


def campaign(exe, out, duration):
    formal = duration == 1800
    meter = ProcessMeter()
    checks, windows, resources = {}, [], []
    r, restart, status, failure = None, None, "FAIL", None
    t0 = None
    actual_duration = None
    save(out / "frozen_load.json", {"campaign_plan_id": PLAN_ID, "devices": 20,
         "suite": 2, "automatic_temperature_c": 25, "interval_ms": 5000,
         "duration_seconds": duration, "formal_30min": formal,
         "duration_label": "formal-1800-seconds" if formal else "DEBUG_NOT_FORMAL_30MIN",
         "sample_every_seconds": 30, "planned_phases": list(PHASES),
         "existing_automatic_rekey_policy": "default 15-minute age/512 record thresholds unchanged; actual new SID and dual-end authentication required, bounded observation wait at most 15 seconds, never explicit retry",
         "mutation_retries": 0, "operations_limit": 256, "queue_limit": 32,
         "event_ring_limit": 2000, "public_log_bytes_limit": 20 << 20,
         "physical_network_disconnected": None, "network_reason": "loopback campaign; no network adapter changed"})
    try:
        if os.name != "nt":
            status = "ENVIRONMENT_BLOCKED"
            raise RuntimeError("this campaign is the actual Windows EXE target")
        # Keep partially started objects available for cleanup if fixture init fails.
        r = ObservedRun.__new__(ObservedRun)
        r.__init__(exe, out / "continuous", 20)
        batch = r.req("/run/start", {})
        if batch["attempted"] != 20 or batch["rejected"]:
            raise AssertionError("initial all-20 admission not attempted/accepted")
        initial = r.wait_observed(all_online)
        validate_state(initial, r.run, configured=False)
        t0 = time.monotonic()
        origin_utc = stamp()
        save(r.out / "all20_start.json", initial)
        save(r.out / "window_start.json", {"at": origin_utc, "monotonic": t0,
             "boundary": "first actual state observing all 20 admitted Suite2 channels, before setting the 20 initial timers"})
        requests = []
        for id in DEVICE_IDS:
            operation = "live5s-" + id
            requests.append(operation)
            r.req("/gateway/commands", {"request_id": operation, "device_id": id, "interval_ms": 5000}, 202)
        configured = r.wait_observed(lambda s: all(any(o["operation_id"] == id and o["status"] == "confirmed"
                     and o["applied"] is True and o["peer_processed_at"] is not None and o["confirmed_at"] is not None
                     for o in s["operations"]) for id in requests))
        validate_state(configured, r.run)
        save(r.out / "timer5000_confirmed.json", configured)
        checks["initial20_real_hybrid_and_actual_timer_confirmed"] = True
        trace = out / "phases.jsonl"
        next_sample, phase_index, previous_counts = 0, 0, None
        while True:
            elapsed = time.monotonic() - t0
            if phase_index < len(PHASES) and PHASES[phase_index][0] < duration and elapsed >= PHASES[phase_index][0]:
                scheduled, id = PHASES[phase_index]
                label = "t%04d_%s" % (scheduled, id)
                if id == "GW001":
                    phase_gateway(r, label, t0, trace, windows)
                else:
                    phase_disconnect(r, id, label, t0, trace, windows)
                checks[label] = True
                phase_index += 1
                elapsed = time.monotonic() - t0
            if elapsed >= next_sample or elapsed >= duration:
                state = healthy_observation(r, "sample_%04d" % next_sample)
                validate_state(state, r.run)
                counts = {d["id"]: d["accepted_samples"] for d in state["devices"]}
                if previous_counts is not None and any(counts[id] <= previous_counts[id] for id in DEVICE_IDS):
                    raise AssertionError("a device's actual accepted samples did not advance over the sampled window")
                record = resource_record(r, meter, state, time.monotonic() - t0, "running")
                append_record(out / "resources.jsonl", record)
                resources.append(record)
                previous_counts = counts
                print(json.dumps({"elapsed_seconds": round(record["elapsed_seconds"], 3), "run_id": r.run,
                      "online": record["authenticated_sessions_observed"], "accepted_total": sum(counts.values()),
                      "log_bytes": state["capacity"]["log_bytes"], "status": "RUNNING"}), flush=True)
                next_sample += 30
            if time.monotonic() - t0 >= duration:
                break
            wake = min(float(duration), float(next_sample),
                       float(PHASES[phase_index][0]) if phase_index < len(PHASES) and PHASES[phase_index][0] < duration else float(duration))
            time.sleep(max(.01, min(1.0, t0 + wake - time.monotonic())))
        actual_duration = time.monotonic() - t0
        last = healthy_observation(r, "window_end")
        validate_state(last, r.run)
        save(r.out / "window_end.json", {"at": stamp(), "elapsed_seconds": actual_duration,
             "planned_seconds": duration, "duration_label": "formal-1800-seconds" if formal else "DEBUG_NOT_FORMAL_30MIN"})
        save(r.out / "last_running.json", last)
        controlled_export(r, last)
        checks["controlled_export_same_current_run_source_range"] = True
        stop_window = {"label": "planned_final_stop", "device_ids": list(DEVICE_IDS),
                       "started_at": stamp(), "old_sids": [d["sid"] for d in last["devices"]]}
        windows.append(stop_window)
        r.req("/run/stop", {})
        stopped = r.wait_observed(lambda s: all(d["status"] == "offline" for d in s["devices"]) and
                  all(s.get("live_diagnostics", {}).get(k) == 0 for k in
                      ("connections_active", "sessions_active", "device_workers_active", "local_queue_items", "control_queue_items")))
        save(r.out / "devices_and_workers_stopped.json", stopped)
        append_record(out / "resources.jsonl", resource_record(r, meter, stopped, time.monotonic() - t0, "stopped_devices"))
        r.req("/run/exit", {"confirm": "exit:" + r.run})
        r.p.wait(timeout=20)
        stop_window["finished_at"] = stamp()
        cleanup = r.finish()
        checks["original_process_stopped"] = cleanup["app_exit_code"] == 0 and cleanup["reader_exited"]
        if not checks["original_process_stopped"]:
            raise AssertionError("original process did not stop cleanly")
        append_record(out / "resources.jsonl", {"observed_at": stamp(), "phase": "process_exited", "pid": r.p.pid,
                      "process_exited": True, "app_exit_code": r.p.returncode, **meter.tcp(r.p.pid)})
        raw_observations(r, windows)
        main_run, generation = r.run, last["gateway"]["generation"]
        r = None
        restart = ObservedRun.__new__(ObservedRun)
        restart.__init__(exe, out / "restart", 20)
        if restart.run == main_run or restart.req("/state")["gateway"]["generation"] != generation:
            raise AssertionError("restart run not new or persisted gateway generation differs")
        restart.req("/run/start", {})
        restart_state = restart.wait_observed(lambda s: all_online(s) and all(d["accepted_samples"] > 0 for d in s["devices"]), timeout=20)
        save(restart.out / "all20_actual_restart_communication.json", restart_state)
        restart.req("/run/stop", {})
        restart_stop = restart.wait_observed(lambda s: all(d["status"] == "offline" for d in s["devices"]) and
                       s.get("live_diagnostics", {}).get("device_workers_active") == 0)
        save(restart.out / "restart_devices_stopped.json", restart_stop)
        restart.req("/run/exit", {"confirm": "exit:" + restart.run})
        restart.p.wait(timeout=20)
        restart_cleanup = restart.finish()
        checks["restart_new_run_all20_real_communication_clean_stop"] = restart_cleanup["app_exit_code"] == 0 and restart_cleanup["reader_exited"]
        if not checks["restart_new_run_all20_real_communication_clean_stop"]:
            raise AssertionError("restart process did not stop cleanly")
        restart = None
        complete_resources = all(all(v["windows_process"][k] is not None for k in ProcessMeter.FIELDS) for v in resources)
        checks["windows_resource_observations_complete"] = complete_resources
        checks["full_frozen_1800_seconds_observed"] = formal and actual_duration >= 1800
        status = "PASS" if complete_resources else "ENVIRONMENT_BLOCKED"
    except BaseException as e:
        failure = {"type": type(e).__name__, "message": str(e), "at": stamp(),
                   "elapsed_seconds": time.monotonic() - t0 if t0 is not None else None}
        save(out / "failure.json", failure)
        (out / "failure_traceback.txt").write_text(traceback.format_exc(), encoding="utf-8")
    finally:
        cleanup_failures = []
        for run in (r, restart):
            if run is not None and hasattr(run, "p"):
                try:
                    if not (run.out / "cleanup.json").exists():
                        run.finish()
                except BaseException as e:
                    cleanup_failures.append({"type": type(e).__name__, "message": str(e)})
        if cleanup_failures:
            status = "FAIL"
            save(out / "cleanup_failures.json", cleanup_failures)
        try:
            info = json.loads((exe.parent / "build_info.json").read_text(encoding="utf-8"))
        except (OSError, ValueError) as e:
            info = {"source_manifest_sha256": None, "source_reason": "build info unavailable: " + str(e)}
        save(out / "acceptance.json", {"status": status, "checks": checks, "failure": failure,
             "formal_30min_status": status if formal else "NOT_EXECUTED",
             "duration_label": "formal-1800-seconds" if formal else "DEBUG_NOT_FORMAL_30MIN",
             "planned_seconds": duration, "observed_seconds": actual_duration,
             "source_manifest_sha256": info["source_manifest_sha256"], "binary_sha256": sha(exe) if exe.exists() else None,
             "campaign_plan_id": PLAN_ID, "phase_windows": windows,
             "review_command": sys.argv, "python_platform": platform.platform(),
             "resource_series_count": len(resources), "credentials_saved": False,
             "race_or_formal_benchmark_in_parallel": "NOT_AUTHORIZED; scheduler must ensure none",
             "scope": "current local Windows live load; not a handshake benchmark, hardware/physical-network test, or proof of absence of leaks"})
    return 0 if status == "PASS" and failure is None else (2 if status == "ENVIRONMENT_BLOCKED" else 1)


def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument("--exe")
    p.add_argument("--out", required=True)
    p.add_argument("--duration", type=int, default=1800)
    p.add_argument("--resource-probe", action="store_true")
    a = p.parse_args()
    out = Path(a.out).resolve()
    out.mkdir(parents=True, exist_ok=False)
    if a.resource_probe:
        began = stamp()
        with tempfile.TemporaryDirectory(prefix="edgehybrid-live-script-compile-") as temp:
            py_compile.compile(__file__, cfile=str(Path(temp) / "probe.pyc"), doraise=True)
        observed = ProcessMeter().sample(os.getpid())
        passed = all(observed[k] is not None for k in ProcessMeter.FIELDS)
        save(out / "resource_self_probe.json", {"status": "PASS" if passed else "ENVIRONMENT_BLOCKED",
             "observed_process": "current review Python only, NO App launched", "resource": observed,
             "script_sha256": sha(__file__), "static_py_compile": "PASS", "started_at": began,
             "finished_at": stamp(), "command": sys.argv, "real_exit_code": 0 if passed else 2,
             "live_app_stability": "NOT_EXECUTED", "formal_30min": "NOT_EXECUTED"})
        print(json.dumps({"resource_probe": "PASS" if passed else "ENVIRONMENT_BLOCKED", "app_started": False}))
        return 0 if passed else 2
    if not a.exe or not 1 <= a.duration <= 1800:
        p.error("--exe required and --duration must be 1..1800; shorter debug is never formal 30min")
    return campaign(Path(a.exe).resolve(), out, a.duration)


if __name__ == "__main__":
    sys.exit(main())
