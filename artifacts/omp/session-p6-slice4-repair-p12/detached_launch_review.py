#!/usr/bin/env python3
"""Detached launch for the fresh independent P6 Slice 4 hard re-review.

Hard tier, read-only access, GLM-5.2. Session dir and output are placed
OUTSIDE the repo to satisfy the wrapper's non-overlap requirement for
hard-tier reviews. The prompt file stays inside the repo (read-only input).
"""
import os
import subprocess
import sys

BASE = "/Users/yingliangzhang/Projects/ananke-p0a-schema-codegen"
SESS = "/private/tmp/hermes-p6-review-session"
LOG = SESS + "/wrapper-review.log"
PIDFILE = SESS + "/wrapper-review.pid"
OUTPUT = "/private/tmp/hermes-p6-review-output.md"
PROMPT = "/private/tmp/hermes-p6-review-prompt.md"

env = dict(os.environ)
env["HERMES_CODING_WORKFLOW"] = "coupled-v1"
env["OMP_SESSION_ROOT"] = SESS

cmd = [
    "bash",
    os.path.expanduser("~/.hermes/profiles/orchestrator/scripts/omp_with_timeout.sh"),
    "1740",
    PROMPT,
    OUTPUT,
    "--workflow", "coupled-v1",
    "--role", "review",
    "--run-id", "p6-slice4-rereview-20260730",
    "--hermes-provider", "custom:sudo",
    "--hermes-model", "glm-5.2",
    "--task-tier", "hard",
    "--hard-access", "read-only",
    "--session-dir", SESS,
]

os.makedirs(SESS, exist_ok=True)
log = open(LOG, "ab", buffering=0)
proc = subprocess.Popen(
    cmd,
    cwd=BASE,
    env=env,
    stdin=subprocess.DEVNULL,
    stdout=log,
    stderr=subprocess.STDOUT,
    start_new_session=True,
)
with open(PIDFILE, "w", encoding="utf-8") as fh:
    fh.write(str(proc.pid) + "\n")
print("detached_review_pid=%d" % proc.pid)
sys.exit(0)
