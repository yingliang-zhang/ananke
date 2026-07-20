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
from harness_support import build_source_manifest, write_bound_json_report

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
        "test": "TestEngineTranscriptCorruptionStaysNonterminalWhileAlive",
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


def _go_test_json_events(output):
    """Return valid machine-readable go test events from NDJSON output."""
    events = []
    for line in output.splitlines():
        if not line.strip():
            continue
        try:
            event = json.loads(line)
        except json.JSONDecodeError:
            continue
        if isinstance(event, dict) and isinstance(event.get("Action"), str):
            events.append(event)
    return events


def named_test_passed(exit_code, output, test_name):
    """Require the requested test's own pass event and a successful command."""
    if exit_code != 0:
        return False
    events = _go_test_json_events(output)
    saw_run = any(
        event.get("Test") == test_name and event["Action"] == "run"
        for event in events
    )
    saw_pass = any(
        event.get("Test") == test_name and event["Action"] == "pass"
        for event in events
    )
    return saw_run and saw_pass


def classify_mutation_result(exit_code, output, test_name):
    """Classify only the named test's own terminal go test JSON event."""
    if exit_code == 124:
        return "timeout", False

    events = _go_test_json_events(output)
    named_actions = [
        event["Action"] for event in events if event.get("Test") == test_name
    ]
    if exit_code != 0 and "run" in named_actions and "fail" in named_actions:
        return "behavioral_test_rejection", True
    if "pass" in named_actions:
        return "not_detected", False
    if "skip" in named_actions:
        return "named_test_skipped", False
    if "run" in named_actions:
        return "named_test_incomplete", False

    if "fail" in named_actions:
        return "named_test_incomplete", False
    if any(event["Action"] == "build-fail" for event in events):
        return "compile_failure", False
    if any(event.get("Test") for event in events):
        return "test_not_run", False
    if any(event["Action"] == "fail" for event in events):
        return "setup_failure", False
    if exit_code == 0:
        return "test_not_run", False
    return "invalid_test_output", False


def main():
    candidate = build_source_manifest(REPO)
    results = []
    all_pass = True

    # Baseline: run all mutation tests without tags to confirm they pass.
    print("=== Baseline (no mutation tags) ===")
    for m in MUTATIONS:
        cmd = [
            "go",
            "test",
            "-json",
            "-run",
            "^" + m["test"] + "$",
            "-count=1",
            "-timeout",
            "60s",
            m["package"],
        ]
        code, out, err = run_cmd(cmd)
        baseline_pass = named_test_passed(code, out, m["test"])
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

        cmd = [
            "go",
            "test",
            "-json",
            "-tags=" + tag,
            "-run",
            "^" + test + "$",
            "-count=1",
            "-timeout",
            "60s",
            pkg,
        ]
        code, out, err = run_cmd(cmd)

        classification, detected = classify_mutation_result(code, out, test)
        if detected:
            print(f"  result: DETECTED ({classification}, exit {code})")
        else:
            all_pass = False
            print(f"  result: NOT DETECTED ({classification}, exit {code})")

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
    write_bound_json_report(outpath, report, REPO, candidate)
    print(f"\n=== Report: {outpath} ===")
    print(f"Detected: {report['detected_count']}/{report['mutation_count']}")
    if all_pass:
        print("ALL MUTATIONS DETECTED")
    else:
        print("SOME MUTATIONS NOT DETECTED")
        sys.exit(1)


if __name__ == "__main__":
    main()
