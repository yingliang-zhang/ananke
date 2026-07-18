#!/usr/bin/env python3
"""Stress lifecycle: 20-pass crash/restart and 20-pass cancellation."""
import json, os, subprocess, sys, time, signal
from pathlib import Path

REPO = Path(__file__).resolve().parent.parent
REPORTS = REPO / "reports"
REPORTS.mkdir(exist_ok=True)
N = 20

def build_binaries():
    bins = {}
    for name in ["ananke", "ananke-supervisor", "ananke-fakeworker"]:
        out = REPO / "build" / "stress" / name
        out.parent.mkdir(parents=True, exist_ok=True)
        r = subprocess.run(["go", "build", "-o", str(out), f"./cmd/{name}"], cwd=REPO, capture_output=True, text=True)
        if r.returncode != 0:
            print(f"build {name} failed: {r.stderr}")
            sys.exit(1)
        bins[name] = str(out)
    return bins

def run_daemon(bins, store_path, data_dir, socket_path, token):
    cmd = [bins["ananke"], "-token", token, "-store", store_path, "-socket", socket_path, "-data-dir", data_dir, "-supervisor-bin", bins["ananke-supervisor"]]
    return subprocess.Popen(cmd, stdout=subprocess.PIPE, stderr=subprocess.PIPE)

def api(socket_path, token, cmd, extra=None):
    import socket as _sock
    req = {"cmd": cmd, "token": token}
    if extra:
        req.update(extra)
    data = json.dumps(req).encode()
    s = _sock.socket(_sock.AF_UNIX, _sock.SOCK_STREAM)
    s.settimeout(10)
    s.connect(socket_path)
    s.sendall(data)
    resp = s.recv(65536)
    s.close()
    return json.loads(resp)

def main():
    bins = build_binaries()
    crash_results = []
    cancel_results = []
    token = "stress-token"

    # --- Crash/restart stress ---
    print(f"=== Crash/Restart x{N} ===")
    for i in range(N):
        tmp = REPO / f"build/stress/crash-{i}"
        tmp.mkdir(parents=True, exist_ok=True)
        store_path = str(tmp / "store.sqlite")
        data_dir = str(tmp / "data")
        socket_path = str(tmp / "daemon.sock")
        token_path = str(tmp / "token")

        daemon = run_daemon(bins, store_path, data_dir, socket_path, token)
        time.sleep(1)

        # Create project+workstream
        api(socket_path, token, "create-project", {"id": "p1", "name": "test", "root": "/tmp"})
        api(socket_path, token, "create-workstream", {"id": "w1", "project_id": "p1", "name": "test"})

        # Launch run
        r = api(socket_path, token, "launch-run", {
            "id": f"run-{i}",
            "project_id": "p1",
            "workstream_id": "w1",
            "worker_path": bins["ananke-fakeworker"],
            "worker_env": ["ANANKE_FW_EVENTS=1", "ANANKE_FW_EXIT=0", "ANANKE_FW_EXIT_DELAY_MS=5000"],
        })
        if not r.get("ok"):
            crash_results.append({"pass": i, "ok": False, "error": f"launch: {r}"})
            break

        # Wait for running
        time.sleep(1)

        # Kill daemon
        daemon.send_signal(signal.SIGKILL)
        daemon.wait()

        # Restart
        daemon2 = run_daemon(bins, store_path, data_dir, socket_path, token)
        time.sleep(2)

        # Check run still tracked
        r = api(socket_path, token, "get-run", {"id": f"run-{i}"})
        ok = r.get("ok") and r.get("run", {}).get("state") in ("running", "completed", "cancelling")
        crash_results.append({"pass": i, "ok": ok, "state": r.get("run", {}).get("state", "?")})
        print(f"  pass {i}: {'OK' if ok else 'FAIL'} (state={r.get('run', {}).get('state', '?')})")

        # Cleanup
        daemon2.send_signal(signal.SIGKILL)
        daemon2.wait()
        if not ok:
            break

    # --- Cancellation stress ---
    print(f"\n=== Cancellation x{N} ===")
    for i in range(N):
        tmp = REPO / f"build/stress/cancel-{i}"
        tmp.mkdir(parents=True, exist_ok=True)
        store_path = str(tmp / "store.sqlite")
        data_dir = str(tmp / "data")
        socket_path = str(tmp / "daemon.sock")

        daemon = run_daemon(bins, store_path, data_dir, socket_path, token)
        time.sleep(1)

        api(socket_path, token, "create-project", {"id": "p1", "name": "test", "root": "/tmp"})
        api(socket_path, token, "create-workstream", {"id": "w1", "project_id": "p1", "name": "test"})

        r = api(socket_path, token, "launch-run", {
            "id": f"run-{i}",
            "project_id": "p1",
            "workstream_id": "w1",
            "worker_path": bins["ananke-fakeworker"],
            "worker_env": ["ANANKE_FW_EVENTS=0", "ANANKE_FW_EXIT=0", "ANANKE_FW_EXIT_DELAY_MS=10000"],
        })
        if not r.get("ok"):
            cancel_results.append({"pass": i, "ok": False, "error": f"launch: {r}"})
            break

        time.sleep(1)
        r = api(socket_path, token, "cancel-run", {"id": f"run-{i}"})
        accepted = r.get("accepted") == True

        # Wait for cancelled
        deadline = time.time() + 15
        state = "?"
        while time.time() < deadline:
            r = api(socket_path, token, "get-run", {"id": f"run-{i}"})
            state = r.get("run", {}).get("state", "?")
            if state == "cancelled":
                break
            time.sleep(0.5)

        ok = accepted and state == "cancelled"
        cancel_results.append({"pass": i, "ok": ok, "accepted": accepted, "state": state})
        print(f"  pass {i}: {'OK' if ok else 'FAIL'} (accepted={accepted}, state={state})")

        daemon.send_signal(signal.SIGKILL)
        daemon.wait()
        if not ok:
            break

    report = {
        "timestamp": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
        "crash_restart": {"total": N, "passed": sum(1 for r in crash_results if r["ok"]), "results": crash_results},
        "cancellation": {"total": N, "passed": sum(1 for r in cancel_results if r["ok"]), "results": cancel_results},
    }
    (REPORTS / "stress-lifecycle.json").write_text(json.dumps(report, indent=2))
    all_pass = all(r["ok"] for r in crash_results + cancel_results)
    print(f"\nCrash/restart: {report['crash_restart']['passed']}/{N}")
    print(f"Cancellation: {report['cancellation']['passed']}/{N}")
    print("ALL PASS" if all_pass else "SOME FAILED")
    sys.exit(0 if all_pass else 1)

if __name__ == "__main__":
    main()
