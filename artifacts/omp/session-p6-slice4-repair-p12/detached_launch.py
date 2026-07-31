#!/usr/bin/env python3
"""Detach the OMP wrapper for the P6 Slice 4 P1/P2 repair resume.

The orchestrator terminal clamps foreground commands at 60s and the
background channel is unreliable here, so launch the wrapper in a new
session (start_new_session=True) with its own log/pid files, then exit.
The wrapper owns its 900s internal deadline and 60s watchdog grace.
"""
import os
import subprocess
import sys

BASE = "/Users/yingliangzhang/Projects/ananke-p0a-schema-codegen"
SESS = BASE + "/artifacts/omp/session-p6-slice4-repair-p12"
LOG = SESS + "/wrapper.log"
PIDFILE = SESS + "/wrapper.pid"

env = dict(os.environ)
env["HERMES_CODING_WORKFLOW"] = "coupled-v1"
env["OMP_SESSION_ROOT"] = SESS

cmd = [
    "bash",
    os.path.expanduser("~/.hermes/profiles/orchestrator/scripts/omp_with_timeout.sh"),
    "900",
    BASE + "/artifacts/omp/p6-slice4-repair-p12-verifier-installation-prompt.md",
    BASE + "/artifacts/omp/p6-slice4-repair-p12-verifier-installation-output.md",
    "--workflow", "coupled-v1",
    "--role", "implement",
    "--run-id", "p6-slice4-repair-p12-20260730",
    "--hermes-provider", "custom:sudo-kimi-k3",
    "--hermes-model", "t9s/kimi-k3",
    "--task-tier", "normal",
    "--session-dir", SESS,
    "--resume", "019fb338-a160-7000-92d7-0024c8edd664",
]

log = open(LOG, "ab", buffering=0)
proc = subprocess.Popen(
    cmd,
    cwd=SESS,
    env=env,
    stdin=subprocess.DEVNULL,
    stdout=log,
    stderr=subprocess.STDOUT,
    start_new_session=True,
)
with open(PIDFILE, "w", encoding="utf-8") as fh:
    fh.write(str(proc.pid) + "\n")
print("detached_pid=%d" % proc.pid)
sys.exit(0)
