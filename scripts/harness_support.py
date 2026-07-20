#!/usr/bin/env python3
"""Shared source binding and lifecycle harness support."""

from __future__ import annotations

from contextlib import closing
from datetime import datetime
import hashlib
import json
import os
import signal
import socket
import sqlite3
import subprocess
import time
from pathlib import Path
from typing import Iterable, Mapping, Any

SOURCE_MANIFEST_SCHEMA = "ananke-source-manifest-v1"
_REQUIRED_FILES = ("go.mod", "go.sum")
_REQUIRED_DIRECTORIES = ("cmd", "internal", "scripts")


def _resolved_inside(root: Path, path: Path) -> Path:
    try:
        resolved = path.resolve(strict=True)
    except FileNotFoundError:
        raise FileNotFoundError(f"candidate source path is missing: {path}") from None
    try:
        resolved.relative_to(root)
    except ValueError:
        raise ValueError(f"candidate source path escapes repository root: {path}") from None
    return resolved


def _validate_repository_layout(repo_root: Path) -> Path:
    root = repo_root.resolve(strict=True)
    if not root.is_dir():
        raise NotADirectoryError(f"repository root is not a directory: {repo_root}")

    for relative_path in _REQUIRED_FILES:
        path = root / relative_path
        resolved = _resolved_inside(root, path)
        if not resolved.is_file():
            raise FileNotFoundError(f"required source file is missing: {relative_path}")

    for relative_path in _REQUIRED_DIRECTORIES:
        path = root / relative_path
        try:
            resolved = _resolved_inside(root, path)
        except FileNotFoundError:
            raise FileNotFoundError(
                f"required source root is missing: {relative_path}"
            ) from None
        if not resolved.is_dir():
            raise FileNotFoundError(f"required source root is missing: {relative_path}")

    return root


def candidate_source_paths(repo_root: Path | str) -> Iterable[Path]:
    """Return the candidate source set; callers must not rely on traversal order."""
    root = _validate_repository_layout(Path(repo_root))
    yield root / "go.mod"
    yield root / "go.sum"
    yield from (root / "cmd").rglob("*.go")
    yield from (root / "internal").rglob("*.go")
    yield from (root / "scripts").glob("*.py")


def _sha256_file(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as source:
        for chunk in iter(lambda: source.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def _manifest_entries(repo_root: Path, paths: Iterable[Path]) -> list[dict[str, str]]:
    entries: list[dict[str, str]] = []
    seen: set[str] = set()
    for path in paths:
        candidate = Path(path)
        if not candidate.is_absolute():
            candidate = repo_root / candidate
        resolved = _resolved_inside(repo_root, candidate)
        if not resolved.is_file():
            raise ValueError(f"candidate source path is not a file: {candidate}")
        relative_path = resolved.relative_to(repo_root).as_posix()
        if relative_path in seen:
            raise ValueError(f"duplicate candidate source path: {relative_path}")
        seen.add(relative_path)
        entries.append({"path": relative_path, "sha256": _sha256_file(resolved)})
    entries.sort(key=lambda entry: entry["path"])
    return entries


def build_source_manifest(repo_root: Path | str) -> dict[str, Any]:
    """Compute the deterministic binding for the repository's candidate sources."""
    root = _validate_repository_layout(Path(repo_root))
    entries = _manifest_entries(root, candidate_source_paths(root))
    canonical_payload = {"schema": SOURCE_MANIFEST_SCHEMA, "files": entries}
    canonical_json = json.dumps(
        canonical_payload,
        ensure_ascii=True,
        separators=(",", ":"),
        sort_keys=True,
    ).encode("utf-8")
    return {
        "schema": SOURCE_MANIFEST_SCHEMA,
        "aggregate_sha256": hashlib.sha256(canonical_json).hexdigest(),
        "file_count": len(entries),
        "files": entries,
    }


def assert_source_manifest_unchanged(
    repo_root: Path | str, expected: Mapping[str, Any]
) -> None:
    """Fail closed when candidate sources changed after a harness started."""
    current = build_source_manifest(repo_root)
    if current != expected:
        raise RuntimeError(
            "candidate source changed during harness run: "
            f"started={expected.get('aggregate_sha256')} "
            f"current={current.get('aggregate_sha256')}"
        )


def write_bound_json_report(
    report_path: Path | str,
    report: Mapping[str, Any],
    repo_root: Path | str,
    candidate_binding: Mapping[str, Any],
) -> dict[str, Any]:
    """Assert candidate stability, bind the report, then write formatted JSON."""
    assert_source_manifest_unchanged(repo_root, candidate_binding)
    bound_report = dict(report)
    bound_report["candidate"] = dict(candidate_binding)
    Path(report_path).write_text(
        json.dumps(bound_report, indent=2) + "\n",
        encoding="utf-8",
    )
    return bound_report


def poll_until(description, predicate, timeout, interval=0.05):
    """Return the first truthy predicate result or raise a contextual timeout."""
    if timeout <= 0 or interval <= 0:
        raise ValueError("poll timeout and interval must be positive")
    deadline = time.monotonic() + timeout
    while True:
        value = predicate()
        if value:
            return value
        remaining = deadline - time.monotonic()
        if remaining <= 0:
            raise TimeoutError(f"timed out waiting for {description} after {timeout}s")
        time.sleep(min(interval, remaining))


def api_request(socket_path, token, command, extra=None, timeout=2):
    """Send one authenticated daemon API request over a Unix socket."""
    request = {"cmd": command, "token": token}
    if extra:
        request.update(extra)
    payload = json.dumps(request, separators=(",", ":")).encode("utf-8") + b"\n"
    chunks = []
    total = 0
    with socket.socket(socket.AF_UNIX, socket.SOCK_STREAM) as connection:
        connection.settimeout(timeout)
        connection.connect(str(socket_path))
        connection.sendall(payload)
        while True:
            chunk = connection.recv(65536)
            if not chunk:
                break
            total += len(chunk)
            if total > 1024 * 1024:
                raise RuntimeError("daemon API response exceeds 1 MiB")
            chunks.append(chunk)
    if not chunks:
        raise RuntimeError(f"empty daemon API response for {command}")
    response = json.loads(b"".join(chunks))
    if not isinstance(response, dict):
        raise RuntimeError(f"invalid daemon API response for {command}: {response!r}")
    return response


def _log_tail(log_path, limit=1000):
    try:
        data = Path(log_path).read_bytes()
    except OSError:
        return ""
    return data[-limit:].decode("utf-8", errors="replace")


def wait_for_daemon(daemon, socket_path, token, log_path, timeout=10):
    """Wait for an authenticated ping, failing immediately on daemon exit."""
    last_error = None

    def ready():
        nonlocal last_error
        if daemon.poll() is not None:
            raise RuntimeError(
                f"daemon exited before readiness with code {daemon.returncode}: "
                f"{_log_tail(log_path)}"
            )
        try:
            response = api_request(socket_path, token, "ping")
        except (OSError, json.JSONDecodeError) as error:
            last_error = error
            return None
        if response.get("ok") is True:
            return response
        last_error = RuntimeError(f"ping rejected: {response}")
        return None

    try:
        return poll_until("authenticated daemon readiness", ready, timeout)
    except TimeoutError as error:
        raise TimeoutError(f"{error}; last error: {last_error}") from None


def require_api_ok(response, operation):
    if response.get("ok") is not True:
        raise RuntimeError(f"{operation} failed: {response}")
    return response


def wait_for_run_state(socket_path, token, run_id, expected_state, timeout=30):
    """Poll the daemon until a run reaches exactly the requested state."""
    terminal_states = {"completed", "failed", "cancelled"}

    def state_reached():
        response = require_api_ok(
            api_request(socket_path, token, "get-run", {"id": run_id}),
            f"get run {run_id}",
        )
        state = response.get("run", {}).get("state")
        if state == expected_state:
            return response
        if state in terminal_states:
            raise RuntimeError(
                f"run {run_id} reached terminal state {state}, expected {expected_state}"
            )
        return None

    return poll_until(f"run {run_id} state {expected_state}", state_reached, timeout)


def _read_one(store_path, query, run_id):
    path = Path(store_path).resolve(strict=True)
    with closing(
        sqlite3.connect(f"file:{path}?mode=ro", uri=True, timeout=1)
    ) as database:
        database.row_factory = sqlite3.Row
        row = database.execute(query, (run_id,)).fetchone()
    return dict(row) if row is not None else None


def read_run_persistence(store_path, run_id):
    """Read persisted lifecycle identity and transcript progress from SQLite."""
    return _read_one(
        store_path,
        "SELECT state, supervisor_pgid, committed_offset FROM runs WHERE id = ?",
        run_id,
    )


def read_outbox_status(store_path, run_id):
    """Read the durable terminal-finalization acknowledgement for a run."""
    return _read_one(
        store_path,
        "SELECT terminal_state, acknowledged, acknowledged_at, diagnostic "
        "FROM finalization_outbox WHERE run_id = ?",
        run_id,
    )


def wait_for_persisted_pgid(store_path, run_id, timeout=10):
    def pgid_recorded():
        record = read_run_persistence(store_path, run_id)
        if record is not None and record["supervisor_pgid"] > 0:
            return record
        return None

    return poll_until(f"persisted PGID for run {run_id}", pgid_recorded, timeout)


def wait_for_outbox_acknowledgement(
    store_path, run_id, expected_terminal_state, timeout=10
):
    def acknowledged():
        status = read_outbox_status(store_path, run_id)
        if status is None:
            return None
        if status["terminal_state"] != expected_terminal_state:
            raise RuntimeError(
                f"run {run_id} outbox state {status['terminal_state']}, "
                f"expected {expected_terminal_state}"
            )
        if status["acknowledged"] < 0:
            raise RuntimeError(f"run {run_id} finalization was rejected: {status}")
        if status["acknowledged"] == 1:
            return status
        return None

    return poll_until(
        f"terminal outbox acknowledgement for run {run_id}", acknowledged, timeout
    )


def validate_event_evidence(response, expected_count):
    """Require the fake worker's exact durable event set and monotonic sequences."""
    require_api_ok(response, "list events")
    events = response.get("events", [])
    if not isinstance(events, list):
        raise RuntimeError(f"list events returned invalid events: {events!r}")
    if len(events) != expected_count:
        raise RuntimeError(f"event count {len(events)}, expected {expected_count}")
    sequences = [event.get("seq") for event in events]
    expected_sequences = list(range(1, expected_count + 1))
    if sequences != expected_sequences:
        raise RuntimeError(
            f"event sequences {sequences}, expected {expected_sequences}"
        )
    for expected_sequence, event in zip(expected_sequences, events):
        if event.get("type") != "message":
            raise RuntimeError(f"event {expected_sequence} has type {event.get('type')}")
        payload = event.get("payload")
        if not isinstance(payload, dict):
            raise RuntimeError(
                f"event {expected_sequence} has invalid payload {payload!r}"
            )
        source_sequence = payload.get("source_seq")
        if type(source_sequence) is not int or source_sequence != expected_sequence:
            raise RuntimeError(
                f"event {expected_sequence} payload source_seq {source_sequence!r}, "
                f"expected {expected_sequence}"
            )
        expected_text = f"event {expected_sequence}"
        if payload.get("text") != expected_text:
            raise RuntimeError(
                f"event {expected_sequence} payload text {payload.get('text')!r}, "
                f"expected {expected_text!r}"
            )
        timestamp = payload.get("timestamp")
        if not isinstance(timestamp, str) or not timestamp:
            raise RuntimeError(
                f"event {expected_sequence} payload timestamp {timestamp!r} is invalid"
            )
        try:
            parsed_timestamp = datetime.fromisoformat(timestamp.replace("Z", "+00:00"))
        except ValueError as exc:
            raise RuntimeError(
                f"event {expected_sequence} payload timestamp {timestamp!r} is invalid"
            ) from exc
        if parsed_timestamp.tzinfo is None:
            raise RuntimeError(
                f"event {expected_sequence} payload timestamp {timestamp!r} lacks timezone"
            )
    return {"event_count": len(events), "event_sequences": sequences}


def process_group_members(pgid):
    """Return exact PIDs whose current process-group ID equals pgid."""
    if not isinstance(pgid, int) or pgid <= 0:
        raise ValueError(f"invalid process group ID: {pgid!r}")
    result = subprocess.run(
        ["ps", "-A", "-o", "pid=,pgid="],
        capture_output=True,
        text=True,
        timeout=5,
    )
    if result.returncode != 0:
        raise RuntimeError(f"ps failed with code {result.returncode}: {result.stderr}")
    members = []
    for line in result.stdout.splitlines():
        fields = line.split()
        if len(fields) != 2:
            if fields:
                raise RuntimeError(f"unexpected ps row: {line!r}")
            continue
        try:
            pid, row_pgid = (int(field) for field in fields)
        except ValueError:
            raise RuntimeError(f"unexpected ps row: {line!r}") from None
        if row_pgid == pgid:
            members.append(pid)
    return sorted(members)


def wait_for_process_group_quiescence(pgid, timeout=10):
    def quiescent():
        return True if not process_group_members(pgid) else None

    poll_until(f"process group {pgid} quiescence", quiescent, timeout)
    return []


def stop_process(process, terminate_timeout=2, kill_timeout=2):
    """Boundedly terminate a child process, escalating to SIGKILL."""
    if process is None or process.poll() is not None:
        return
    process.terminate()
    try:
        process.wait(timeout=terminate_timeout)
        return
    except subprocess.TimeoutExpired:
        process.kill()
    try:
        process.wait(timeout=kill_timeout)
    except subprocess.TimeoutExpired:
        raise RuntimeError(f"process {process.pid} resisted SIGKILL") from None


def kill_process(process, timeout=3):
    """Boundedly SIGKILL a child process for an intentional crash scenario."""
    if process is None or process.poll() is not None:
        return
    process.kill()
    try:
        process.wait(timeout=timeout)
    except subprocess.TimeoutExpired:
        raise RuntimeError(f"process {process.pid} did not exit after SIGKILL") from None


def cleanup_process_group(pgid, terminate_timeout=1, kill_timeout=2):
    """Best-effort bounded cleanup of exact current members of a known run PGID."""
    if not pgid:
        return []
    members = process_group_members(pgid)
    if os.getpid() in members:
        raise RuntimeError(f"refusing to signal harness process group {pgid}")
    for pid in members:
        try:
            os.kill(pid, signal.SIGTERM)
        except ProcessLookupError:
            pass
    try:
        wait_for_process_group_quiescence(pgid, terminate_timeout)
        return []
    except TimeoutError:
        pass
    for pid in process_group_members(pgid):
        try:
            os.kill(pid, signal.SIGKILL)
        except ProcessLookupError:
            pass
    try:
        wait_for_process_group_quiescence(pgid, kill_timeout)
    except TimeoutError:
        return process_group_members(pgid)
    return []


def all_results_pass(results, expected_count):
    return len(results) == expected_count and all(
        result.get("ok") is True for result in results
    )
