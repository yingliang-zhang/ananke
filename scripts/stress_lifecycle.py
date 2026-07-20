#!/usr/bin/env python3
"""Stress lifecycle durability, finalization, and exact process-group cleanup."""

import argparse
import shutil
import subprocess
import sys
import time
from pathlib import Path

from harness_support import (
    all_results_pass,
    api_request,
    build_source_manifest,
    cleanup_process_group,
    kill_process,
    poll_until,
    process_group_members,
    read_run_persistence,
    require_api_ok,
    stop_process,
    validate_event_evidence,
    wait_for_daemon,
    wait_for_outbox_acknowledgement,
    wait_for_persisted_pgid,
    wait_for_process_group_quiescence,
    wait_for_run_state,
    write_bound_json_report,
)

REPO = Path(__file__).resolve().parent.parent
REPORTS = REPO / "reports"
REPORTS.mkdir(exist_ok=True)
DEFAULT_ITERATIONS = 20


def positive_iteration_count(value):
    try:
        count = int(value)
    except ValueError:
        raise argparse.ArgumentTypeError("iterations must be an integer") from None
    if count <= 0:
        raise argparse.ArgumentTypeError("iterations must be positive")
    return count


def parse_args(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument(
        "--iterations",
        type=positive_iteration_count,
        default=DEFAULT_ITERATIONS,
        help="focused smoke iteration count (default: 20)",
    )
    return parser.parse_args(argv)


def build_binaries():
    bins = {}
    for name in ("ananke", "ananke-supervisor", "ananke-fakeworker"):
        output = REPO / "build" / "stress" / name
        output.parent.mkdir(parents=True, exist_ok=True)
        try:
            result = subprocess.run(
                ["go", "build", "-o", str(output), f"./cmd/{name}"],
                cwd=REPO,
                capture_output=True,
                text=True,
                timeout=120,
            )
        except subprocess.TimeoutExpired:
            raise RuntimeError(f"build {name} timed out") from None
        if result.returncode != 0:
            raise RuntimeError(f"build {name} failed: {result.stderr[-1000:]}")
        bins[name] = str(output)
    return bins


def fresh_directory(path):
    if path.exists():
        shutil.rmtree(path)
    path.mkdir(parents=True)


def start_daemon(bins, store_path, data_dir, socket_path, token, log_path):
    command = [
        bins["ananke"],
        "-token",
        token,
        "-store",
        str(store_path),
        "-socket",
        str(socket_path),
        "-data-dir",
        str(data_dir),
        "-supervisor-bin",
        bins["ananke-supervisor"],
    ]
    with Path(log_path).open("ab") as log:
        return subprocess.Popen(command, stdout=log, stderr=subprocess.STDOUT)


def setup_workspace(socket_path, token):
    require_api_ok(
        api_request(
            socket_path,
            token,
            "create-project",
            {"id": "p1", "name": "test", "root": "/tmp"},
        ),
        "create project",
    )
    require_api_ok(
        api_request(
            socket_path,
            token,
            "create-workstream",
            {"id": "w1", "project_id": "p1", "name": "test"},
        ),
        "create workstream",
    )


def wait_for_events(socket_path, token, run_id, expected_count, timeout=10):
    def events_ready():
        response = require_api_ok(
            api_request(
                socket_path,
                token,
                "list-events",
                {"id": run_id, "after_seq": 0},
            ),
            f"list events for {run_id}",
        )
        events = response.get("events", [])
        if isinstance(events, list) and len(events) < expected_count:
            return None
        return validate_event_evidence(response, expected_count)

    return poll_until(f"{expected_count} durable events for {run_id}", events_ready, timeout)


def empty_evidence(iteration):
    return {
        "pass": iteration,
        "ok": False,
        "state": "unknown",
        "event_count": 0,
        "event_sequences": [],
        "committed_offset": 0,
        "outbox_status": None,
        "supervisor_pgid": 0,
        "survivor_pids": [],
    }


def finalize_iteration(row, daemon, store_path, run_id, pgid, error):
    cleanup_errors = []
    if not pgid and Path(store_path).exists():
        try:
            persistence = wait_for_persisted_pgid(store_path, run_id, timeout=2)
            pgid = persistence["supervisor_pgid"]
            row["supervisor_pgid"] = pgid
        except Exception as cleanup_error:
            cleanup_errors.append(f"PGID recovery before daemon stop: {cleanup_error}")

    try:
        stop_process(daemon)
    except Exception as cleanup_error:
        cleanup_errors.append(f"daemon cleanup: {cleanup_error}")

    if not pgid and Path(store_path).exists():
        try:
            persistence = read_run_persistence(store_path, run_id)
            if persistence and persistence["supervisor_pgid"] > 0:
                pgid = persistence["supervisor_pgid"]
                row["supervisor_pgid"] = pgid
        except Exception as cleanup_error:
            cleanup_errors.append(f"PGID recovery after daemon stop: {cleanup_error}")

    if pgid:
        try:
            observed = process_group_members(pgid)
            if error and observed:
                row["survivor_pids"] = observed
            remaining = cleanup_process_group(pgid)
            if remaining:
                row["survivor_pids"] = remaining
                cleanup_errors.append(f"process-group survivors after cleanup: {remaining}")
        except Exception as cleanup_error:
            cleanup_errors.append(f"process-group cleanup: {cleanup_error}")

    messages = ([error] if error else []) + cleanup_errors
    if messages:
        row["ok"] = False
        row["error"] = "; ".join(messages)
    return row


def run_crash_iteration(iteration, bins, token):
    run_id = f"crash-run-{iteration}"
    directory = REPO / "build" / "stress" / f"crash-{iteration}"
    store_path = directory / "store.sqlite"
    data_dir = directory / "data"
    socket_path = directory / "daemon.sock"
    log_path = directory / "daemon.log"
    row = empty_evidence(iteration)
    daemon = None
    pgid = 0
    error = None

    try:
        fresh_directory(directory)
        daemon = start_daemon(
            bins, store_path, data_dir, socket_path, token, log_path
        )
        wait_for_daemon(daemon, socket_path, token, log_path)
        setup_workspace(socket_path, token)
        require_api_ok(
            api_request(
                socket_path,
                token,
                "launch-run",
                {
                    "id": run_id,
                    "project_id": "p1",
                    "workstream_id": "w1",
                    "worker_path": bins["ananke-fakeworker"],
                    "worker_env": [
                        "ANANKE_FW_EVENTS=1",
                        "ANANKE_FW_EXIT=0",
                        "ANANKE_FW_EXIT_DELAY_MS=5000",
                    ],
                },
            ),
            f"launch {run_id}",
        )
        wait_for_run_state(socket_path, token, run_id, "running", timeout=10)
        pre_crash_events = wait_for_events(
            socket_path, token, run_id, expected_count=1, timeout=10
        )
        persistence = wait_for_persisted_pgid(store_path, run_id, timeout=10)
        pgid = persistence["supervisor_pgid"]
        row["supervisor_pgid"] = pgid
        if persistence["committed_offset"] <= 0:
            raise RuntimeError(
                f"run {run_id} has non-positive pre-crash committed offset "
                f"{persistence['committed_offset']}"
            )

        kill_process(daemon)
        daemon = None
        daemon = start_daemon(
            bins, store_path, data_dir, socket_path, token, log_path
        )
        wait_for_daemon(daemon, socket_path, token, log_path)
        state_response = wait_for_run_state(
            socket_path, token, run_id, "completed", timeout=20
        )
        event_evidence = validate_event_evidence(
            api_request(
                socket_path,
                token,
                "list-events",
                {"id": run_id, "after_seq": 0},
            ),
            expected_count=1,
        )
        if event_evidence != pre_crash_events:
            raise RuntimeError(
                f"durable event evidence changed across restart: "
                f"before={pre_crash_events}, after={event_evidence}"
            )
        persistence = read_run_persistence(store_path, run_id)
        if persistence is None or persistence["state"] != "completed":
            raise RuntimeError(f"missing completed persistence for {run_id}: {persistence}")
        if persistence["supervisor_pgid"] != pgid:
            raise RuntimeError(
                f"persisted PGID changed for {run_id}: "
                f"{pgid} -> {persistence['supervisor_pgid']}"
            )
        if persistence["committed_offset"] <= 0:
            raise RuntimeError(
                f"run {run_id} has non-positive committed offset "
                f"{persistence['committed_offset']}"
            )
        outbox_status = wait_for_outbox_acknowledgement(
            store_path, run_id, "completed", timeout=10
        )
        survivors = wait_for_process_group_quiescence(pgid, timeout=10)

        row.update(
            {
                "ok": True,
                "state": state_response["run"]["state"],
                **event_evidence,
                "committed_offset": persistence["committed_offset"],
                "outbox_status": outbox_status,
                "supervisor_pgid": pgid,
                "survivor_pids": survivors,
            }
        )
    except Exception as run_error:
        error = f"{type(run_error).__name__}: {run_error}"
    finally:
        finalize_iteration(row, daemon, store_path, run_id, pgid, error)
    return row


def run_cancellation_iteration(iteration, bins, token):
    run_id = f"cancel-run-{iteration}"
    directory = REPO / "build" / "stress" / f"cancel-{iteration}"
    store_path = directory / "store.sqlite"
    data_dir = directory / "data"
    socket_path = directory / "daemon.sock"
    log_path = directory / "daemon.log"
    row = {**empty_evidence(iteration), "accepted": False}
    daemon = None
    pgid = 0
    error = None

    try:
        fresh_directory(directory)
        daemon = start_daemon(
            bins, store_path, data_dir, socket_path, token, log_path
        )
        wait_for_daemon(daemon, socket_path, token, log_path)
        setup_workspace(socket_path, token)
        require_api_ok(
            api_request(
                socket_path,
                token,
                "launch-run",
                {
                    "id": run_id,
                    "project_id": "p1",
                    "workstream_id": "w1",
                    "worker_path": bins["ananke-fakeworker"],
                    "worker_env": [
                        "ANANKE_FW_EVENTS=0",
                        "ANANKE_FW_EXIT=0",
                        "ANANKE_FW_EXIT_DELAY_MS=10000",
                        "ANANKE_FW_SPAWN_CHILD=1",
                        "ANANKE_FW_CHILD_MODE=resistant",
                    ],
                },
            ),
            f"launch {run_id}",
        )
        wait_for_run_state(socket_path, token, run_id, "running", timeout=10)
        persistence = wait_for_persisted_pgid(store_path, run_id, timeout=10)
        pgid = persistence["supervisor_pgid"]
        row["supervisor_pgid"] = pgid
        cancellation = require_api_ok(
            api_request(socket_path, token, "cancel-run", {"id": run_id}),
            f"cancel {run_id}",
        )
        accepted = cancellation.get("accepted") is True
        row["accepted"] = accepted
        if not accepted:
            raise RuntimeError(f"cancellation not accepted for {run_id}: {cancellation}")
        state_response = wait_for_run_state(
            socket_path, token, run_id, "cancelled", timeout=30
        )
        event_evidence = validate_event_evidence(
            api_request(
                socket_path,
                token,
                "list-events",
                {"id": run_id, "after_seq": 0},
            ),
            expected_count=0,
        )
        persistence = read_run_persistence(store_path, run_id)
        if persistence is None or persistence["state"] != "cancelled":
            raise RuntimeError(f"missing cancelled persistence for {run_id}: {persistence}")
        if persistence["supervisor_pgid"] != pgid:
            raise RuntimeError(
                f"persisted PGID changed for {run_id}: "
                f"{pgid} -> {persistence['supervisor_pgid']}"
            )
        outbox_status = wait_for_outbox_acknowledgement(
            store_path, run_id, "cancelled", timeout=10
        )
        survivors = wait_for_process_group_quiescence(pgid, timeout=10)

        row.update(
            {
                "ok": True,
                "state": state_response["run"]["state"],
                **event_evidence,
                "committed_offset": persistence["committed_offset"],
                "outbox_status": outbox_status,
                "supervisor_pgid": pgid,
                "survivor_pids": survivors,
            }
        )
    except Exception as run_error:
        error = f"{type(run_error).__name__}: {run_error}"
    finally:
        finalize_iteration(row, daemon, store_path, run_id, pgid, error)
    return row


def main(argv=None):
    candidate = build_source_manifest(REPO)
    iterations = parse_args(argv).iterations
    bins = build_binaries()
    token = "stress-token"

    print(f"Crash/restart x{iterations}")
    crash_results = []
    for iteration in range(iterations):
        row = run_crash_iteration(iteration, bins, token)
        crash_results.append(row)
        print(f"  {iteration + 1}/{iterations} {'PASS' if row['ok'] else 'FAIL'}")

    print(f"Cancellation x{iterations}")
    cancel_results = []
    for iteration in range(iterations):
        row = run_cancellation_iteration(iteration, bins, token)
        cancel_results.append(row)
        print(f"  {iteration + 1}/{iterations} {'PASS' if row['ok'] else 'FAIL'}")

    crash_pass = all_results_pass(crash_results, iterations)
    cancel_pass = all_results_pass(cancel_results, iterations)
    all_pass = crash_pass and cancel_pass
    report = {
        "timestamp": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
        "all_pass": all_pass,
        "crash_restart": {
            "total": iterations,
            "passed": sum(row["ok"] is True for row in crash_results),
            "complete": len(crash_results) == iterations,
            "results": crash_results,
        },
        "cancellation": {
            "total": iterations,
            "passed": sum(row["ok"] is True for row in cancel_results),
            "complete": len(cancel_results) == iterations,
            "results": cancel_results,
        },
    }
    write_bound_json_report(
        REPORTS / "stress-lifecycle.json", report, REPO, candidate
    )
    print(f"Crash/restart: {report['crash_restart']['passed']}/{iterations}")
    print(f"Cancellation: {report['cancellation']['passed']}/{iterations}")
    print("ALL PASS" if all_pass else "SOME FAILED")
    return 0 if all_pass else 1


if __name__ == "__main__":
    try:
        sys.exit(main())
    except Exception as error:
        print(f"HARNESS ERROR: {type(error).__name__}: {error}", file=sys.stderr)
        sys.exit(1)
