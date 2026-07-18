#!/usr/bin/env python3
"""Mutation proof: verify each mutation is detected by a named test.

Runs baseline (no tags): all named mutation tests pass.
For each mutation: build with the tag, run the named test, verify it fails.
Classifies: behavioral_test_rejection (expected) vs compile_failure or timeout (unexpected).
Outputs reports/mutation-proof.json.
"""
import json
import os
import subprocess
import sys
import time
from pathlib import Path

REPO = Path(__file__).resolve().parent.parent
REPORTS = REPO / "reports"
REPORTS.mkdir(exist_ok=True)

MUTATIONS = [
    {
        "tag": "mutation_reap_before_cleanup",
        "test": "TestMutationReapBeforeCleanupOrder",
        "package": "./internal/supervisor/",
        "description": "Reap worker before group cleanup — resistant child not killed before reap",
    },
    {
        "tag": "mutation_no_outbox",
        "test": "TestCommitTerminalAtomicRollbackOnOutboxFailure",
        "package": "./internal/store/",
        "description": "Commit terminal state without outbox row — atomicity invariant violated",
    },
    {
        "tag": "mutation_signal_after_reap",
        "test": "TestMutationNoGroupSignalAfterReap",
        "package": "./internal/supervisor/",
        "description": "Signal numeric PGID after identity lost — post-reap group signal",
    },
    {
        "tag": "mutation_terminal_while_alive",
        "test": "TestEngineTranscriptCorruption",
        "package": "./internal/lifecycle/",
        "description": "Enter terminal failed while worker group alive — bypasses cleanup_required",
    },
    {
        "tag": "mutation_reset_offset",
        "test": "TestMutationResetOffsetNoDup",
        "package": "./internal/lifecycle/",
        "description": "Reset transcript offset on restart — duplicate events on reconnect",
    },
    {
        "tag": "mutation_cancel_parent_only",
        "test": "TestSupervisorResistantChildEscalation",
        "package": "./internal/supervisor/",
        "description": "Cancel only the parent PID, not the group — resistant child survives",
    },
]


def run_cmd(cmd, timeout=120):
    """Run a command, return (exit_code, stdout, stderr)."""
    try:
        r = subprocess.run(
            cmd,
            cwd=REPO,
            capture_output=True,
            text=True,
            timeout=timeout,
            env={**os.environ, "CGO_ENABLED": "0", "PATH": os.environ.get("PATH", "")},
        )
        return r.returncode, r.stdout, r.stderr
    except subprocess.TimeoutExpired:
        return 124, "", "timeout"


def main():
    results = []
    all_pass = True

    # Baseline: run all mutation tests without tags to confirm they pass.
    print("=== Baseline (no mutation tags) ===")
    for m in MUTATIONS:
        cmd = ["go", "test", "-run", m["test"], "-count=1", "-timeout", "60s", m["package"]]
        code, out, err = run_cmd(cmd)
        baseline_pass = code == 0
        print(f"  {m['test']}: {'PASS' if baseline_pass else 'FAIL'}")
        if not baseline_pass:
            print(f"    stdout: {out[-200:]}")
            print(f"    stderr: {err[-200:]}")
            all_pass = False

    # Mutations: run each test with the mutation tag active.
    print("\n=== Mutations ===")
    for m in MUTATIONS:
        tag = m["tag"]
        test = m["test"]
        pkg = m["package"]
        print(f"\n--- {tag} ---")
        print(f"  test: {test}")
        print(f"  desc: {m['description']}")

        cmd = ["go", "test", "-tags=" + tag, "-run", test, "-count=1", "-timeout", "60s", pkg]
        code, out, err = run_cmd(cmd)

        # Classification:
        # - exit 0: mutation NOT detected (bad — test passed with mutation)
        # - exit 1: test failed (good — behavioral_test_rejection)
        # - exit 2: compile error (bad — compile_failure)
        # - exit 124: timeout (bad — timeout)
        if code == 1:
            classification = "behavioral_test_rejection"
            detected = True
            print(f"  result: DETECTED (exit {code})")
        elif code == 0:
            classification = "not_detected"
            detected = False
            all_pass = False
            print(f"  result: NOT DETECTED (exit 0 — test passed with mutation!)")
        elif code == 2:
            classification = "compile_failure"
            detected = False
            all_pass = False
            print(f"  result: COMPILE FAILURE (exit 2)")
            print(f"  stderr: {err[-300:]}")
        elif code == 124:
            classification = "timeout"
            detected = False
            all_pass = False
            print(f"  result: TIMEOUT")
        else:
            classification = f"unknown_exit_{code}"
            detected = False
            all_pass = False
            print(f"  result: UNKNOWN (exit {code})")

        results.append({
            "tag": tag,
            "test": test,
            "package": pkg,
            "description": m["description"],
            "exit_code": code,
            "classification": classification,
            "detected": detected,
            "stdout_tail": out[-500:] if out else "",
            "stderr_tail": err[-500:] if err else "",
        })

    report = {
        "timestamp": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
        "all_detected": all_pass,
        "mutation_count": len(MUTATIONS),
        "detected_count": sum(1 for r in results if r["detected"]),
        "results": results,
    }

    outpath = REPORTS / "mutation-proof.json"
    outpath.write_text(json.dumps(report, indent=2))
    print(f"\n=== Report: {outpath} ===")
    print(f"Detected: {report['detected_count']}/{report['mutation_count']}")
    if all_pass:
        print("ALL MUTATIONS DETECTED")
    else:
        print("SOME MUTATIONS NOT DETECTED")
        sys.exit(1)


if __name__ == "__main__":
    main()
