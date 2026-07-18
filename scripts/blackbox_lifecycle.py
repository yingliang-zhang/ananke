#!/usr/bin/env python3
"""Black-box lifecycle: build production binaries, drive API, verify no survivors."""
import json, os, signal, subprocess, sys, time
from pathlib import Path

REPO = Path(__file__).resolve().parent.parent
REPORTS = REPO / "reports"
REPORTS.mkdir(exist_ok=True)

def build_binaries():
    bins = {}
    for name in ["ananke", "ananke-supervisor", "ananke-fakeworker"]:
        out = REPO / "build" / "release" / name
        out.parent.mkdir(parents=True, exist_ok=True)
        r = subprocess.run(["go", "build", "-trimpath", "-ldflags=-s -w", "-o", str(out), f"./cmd/{name}"], cwd=REPO, capture_output=True, text=True)
        if r.returncode != 0:
            print(f"build {name} failed: {r.stderr}")
            sys.exit(1)
        bins[name] = str(out)
    return bins

def api(socket_path, token, cmd, extra=None):
    import socket as _sock
    req = {"cmd": cmd, "token": token}
    if extra: req.update(extra)
    s = _sock.socket(_sock.AF_UNIX, _sock.SOCK_STREAM)
    s.settimeout(10)
    s.connect(socket_path)
    s.sendall(json.dumps(req).encode())
    resp = s.recv(65536)
    s.close()
    return json.loads(resp)

def count_survivors(pgid):
    r = subprocess.run(["ps", "-A", "-o", "pid=,pgid="], capture_output=True, text=True)
    count = 0
    for line in r.stdout.strip().split("\n"):
        parts = line.split()
        if len(parts) == 2 and parts[1] == str(pgid):
            count += 1
    return count

def main():
    bins = build_binaries()
    token = "blackbox-token"
    results = []

    for scenario in ["success", "cancellation"]:
        tmp = REPO / f"build/blackbox-{scenario}"
        tmp.mkdir(parents=True, exist_ok=True)
        store_path = str(tmp / "store.sqlite")
        data_dir = str(tmp / "data")
        socket_path = str(tmp / "daemon.sock")

        daemon = subprocess.Popen(
            [bins["ananke"], "-token", token, "-store", store_path, "-socket", socket_path, "-data-dir", data_dir, "-supervisor-bin", bins["ananke-supervisor"]],
            stdout=subprocess.PIPE, stderr=subprocess.PIPE
        )
        time.sleep(1)

        api(socket_path, token, "create-project", {"id": "p1", "name": "test", "root": "/tmp"})
        api(socket_path, token, "create-workstream", {"id": "w1", "project_id": "p1", "name": "test"})

        if scenario == "success":
            r = api(socket_path, token, "launch-run", {
                "id": "run-1", "project_id": "p1", "workstream_id": "w1",
                "worker_path": bins["ananke-fakeworker"],
                "worker_env": ["ANANKE_FW_EVENTS=3", "ANANKE_FW_EXIT=0", "ANANKE_FW_EXIT_DELAY_MS=100"],
            })
            # Wait for completion
            deadline = time.time() + 30
            state = "?"
            while time.time() < deadline:
                r = api(socket_path, token, "get-run", {"id": "run-1"})
                state = r.get("run", {}).get("state", "?")
                if state == "completed": break
                time.sleep(0.5)
            ok = state == "completed"
        else:
            r = api(socket_path, token, "launch-run", {
                "id": "run-1", "project_id": "p1", "workstream_id": "w1",
                "worker_path": bins["ananke-fakeworker"],
                "worker_env": ["ANANKE_FW_EVENTS=0", "ANANKE_FW_EXIT=0", "ANANKE_FW_EXIT_DELAY_MS=10000", "ANANKE_FW_SPAWN_CHILD=1", "ANANKE_FW_CHILD_MODE=resistant"],
            })
            time.sleep(1)
            r = api(socket_path, token, "cancel-run", {"id": "run-1"})
            accepted = r.get("accepted") == True
            deadline = time.time() + 30
            state = "?"
            while time.time() < deadline:
                r = api(socket_path, token, "get-run", {"id": "run-1"})
                state = r.get("run", {}).get("state", "?")
                if state == "cancelled": break
                time.sleep(0.5)
            ok = accepted and state == "cancelled"

        # Check for survivors (any process in the data dir's process group)
        # We check via ps for any lingering ananke-fakeworker or ananke-supervisor
        time.sleep(1)
        ps = subprocess.run(["ps", "-A", "-o", "pid=,command="], capture_output=True, text=True)
        survivors = [l for l in ps.stdout.split("\n") if "ananke-fakeworker" in l and "grep" not in l]

        result = {"scenario": scenario, "ok": ok, "state": state, "survivors": len(survivors)}
        results.append(result)
        print(f"{'PASS' if ok and len(survivors)==0 else 'FAIL'} {scenario}: state={state}, survivors={len(survivors)}")

        daemon.send_signal(signal.SIGKILL)
        daemon.wait()

    report = {"timestamp": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()), "results": results}
    (REPORTS / "blackbox-lifecycle.json").write_text(json.dumps(report, indent=2))
    all_pass = all(r["ok"] and r["survivors"]==0 for r in results)
    print(f"\n{'ALL PASS' if all_pass else 'SOME FAILED'}")
    sys.exit(0 if all_pass else 1)

if __name__ == "__main__":
    main()
