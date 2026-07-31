#!/usr/bin/env python3
"""Detached resume launch for the P6 Slice 4 P1/P2 repair (fresh2 UUID).

Internal deadline raised to 1740s so the GLM implementation can complete
in one bounded run; external watchdog adds 60s grace.
"""
import os
import subprocess
import sys

BASE = "/Users/yingliangzhang/Projects/ananke-p0a-schema-codegen"
SESS = BASE + "/artifacts/omp/session-p6-slice4-repair-p12-fresh2"
LOG = SESS + "/wrapper-resume.log"
PIDFILE = SESS + "/wrapper-resume.pid"

env = dict(os.environ)
env["HERMES_CODING_WORKFLOW"] = "coupled-v1"
env["OMP_SESSION_ROOT"] = SESS

cmd = [
    "bash",
    os.path.expanduser("~/.hermes/profiles/orchestrator/scripts/omp_with_timeout.sh"),
    "1740",
    BASE + "/artifacts/omp/p6-slice4-repair-p12-verifier-installation-completion-prompt.md",
    BASE + "/artifacts/omp/p6-slice4-repair-p12-verifier-installation-output.md",
    "--workflow", "coupled-v1",
    "--role", "implement",
    "--run-id", "p6-slice4-repair-p12-fresh2-20260730",
    "--hermes-provider", "custom:sudo-kimi-k3",
    "--hermes-model", "t9s/kimi-k3",
    "--task-tier", "normal",
    "--session-dir", SESS,
    "--resume", "019fb390-9f63-7000-bf23-ef4ef8989afe",
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
print("detached_resume_pid=%d" % proc.pid)
sys.exit(0)
