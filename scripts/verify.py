#!/usr/bin/env python3
"""Verification gate: gofmt, vet, test, race, no-CGO test, no-CGO build."""
import json, os, subprocess, sys, time
from pathlib import Path
from harness_support import build_source_manifest, write_bound_json_report

REPO = Path(__file__).resolve().parent.parent
REPORTS = REPO / "reports"
REPORTS.mkdir(exist_ok=True)

def run(cmd, timeout=300):
    env = {**os.environ, "CGO_ENABLED": "0", "PATH": os.environ.get("PATH","")}
    if "CGO_ENABLED" not in cmd[0]:
        env["CGO_ENABLED"] = "1" if "race" in " ".join(cmd) else "0"
    if "-race" in cmd:
        env["CGO_ENABLED"] = "1"
    try:
        r = subprocess.run(cmd, cwd=REPO, capture_output=True, text=True, timeout=timeout, env={**os.environ, **env})
        return r.returncode, r.stdout, r.stderr
    except subprocess.TimeoutExpired:
        return 124, "", "timeout"

gates = [
    {"name": "gofmt",     "cmd": ["gofmt", "-d", "."], "timeout": 30},
    {"name": "vet",       "cmd": ["go", "vet", "./..."], "timeout": 60},
    {"name": "test",      "cmd": ["go", "test", "./...", "-count=1", "-timeout", "300s"], "timeout": 360},
    {"name": "test-race", "cmd": ["go", "test", "-race", "./...", "-count=1", "-timeout", "300s"], "timeout": 360},
    {"name": "test-nocgo","cmd": ["CGO_ENABLED=0", "go", "test", "./...", "-count=1", "-timeout", "300s"], "timeout": 360},
    {"name": "build-nocgo","cmd": ["CGO_ENABLED=0", "go", "build", "./cmd/..."], "timeout": 60},
]

def main():
    candidate = build_source_manifest(REPO)
    results = []
    all_pass = True
    for g in gates:
        # Build proper command (handle env= prefix)
        cmd = g["cmd"]
        env_override = {}
        real_cmd = []
        for c in cmd:
            if "=" in c and c.split("=")[0] in ("CGO_ENABLED",):
                k,v = c.split("=",1)
                env_override[k] = v
            else:
                real_cmd.append(c)
        env = {**os.environ, **env_override}
        if "race" in g["name"]:
            env["CGO_ENABLED"] = "1"
        t0 = time.time()
        try:
            r = subprocess.run(real_cmd, cwd=REPO, capture_output=True, text=True, timeout=g["timeout"], env=env)
            code, out, err = r.returncode, r.stdout, r.stderr
        except subprocess.TimeoutExpired:
            code, out, err = 124, "", "timeout"
        elapsed = time.time() - t0
        passed = code == 0
        if not passed:
            all_pass = False
        # gofmt passes if no diff (exit 0)
        if g["name"] == "gofmt" and code == 0 and len(out.strip()) == 0:
            passed = True
        elif g["name"] == "gofmt" and code == 0 and len(out.strip()) > 0:
            passed = False
            all_pass = False
        print(f"{'PASS' if passed else 'FAIL'} {g['name']} ({elapsed:.1f}s)")
        if not passed:
            print(f"  stdout: {out[-300:]}")
            print(f"  stderr: {err[-300:]}")
        results.append({"gate": g["name"], "passed": passed, "exit_code": code, "elapsed_s": round(elapsed,1), "stdout_tail": out[-500:] if out else "", "stderr_tail": err[-500:] if err else ""})

    report = {"timestamp": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()), "all_pass": all_pass, "gates": results}
    write_bound_json_report(REPORTS / "verification.json", report, REPO, candidate)
    print(f"\n{'ALL GATES PASS' if all_pass else 'SOME GATES FAILED'}")
    sys.exit(0 if all_pass else 1)

if __name__ == "__main__":
    main()
