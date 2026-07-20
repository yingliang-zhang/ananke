#!/usr/bin/env python3
"""Focused unit tests for lifecycle harness evidence and polling helpers."""

from contextlib import closing
import sqlite3
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path
from unittest import mock

sys.path.insert(0, str(Path(__file__).resolve().parent))

import harness_support


class LifecycleHarnessSupportTests(unittest.TestCase):
    def test_api_request_does_not_half_close_after_complete_json_request(self):
        connection = mock.MagicMock()
        connection.__enter__.return_value = connection
        connection.__exit__.return_value = False
        connection.recv.side_effect = (b'{"ok":true}\n', b"")
        connection.shutdown.side_effect = OSError(57, "Socket is not connected")

        with mock.patch.object(
            harness_support.socket, "socket", return_value=connection
        ):
            response = harness_support.api_request(
                "/tmp/ananke-test.sock", "token", "ping"
            )

        self.assertEqual(response, {"ok": True})
        connection.sendall.assert_called_once()
        connection.shutdown.assert_not_called()

    def test_poll_until_returns_first_truthy_value(self):
        attempts = iter((None, None, {"state": "completed"}))

        with mock.patch.object(harness_support.time, "sleep") as sleep:
            result = harness_support.poll_until(
                "completion", lambda: next(attempts), timeout=1, interval=0.01
            )

        self.assertEqual(result, {"state": "completed"})
        self.assertEqual(sleep.call_count, 2)

    def test_poll_until_times_out_with_context(self):
        with mock.patch.object(
            harness_support.time, "monotonic", side_effect=(10.0, 11.1)
        ), mock.patch.object(harness_support.time, "sleep"):
            with self.assertRaisesRegex(TimeoutError, "outbox acknowledgement"):
                harness_support.poll_until(
                    "outbox acknowledgement", lambda: None, timeout=1
                )

    def test_event_evidence_requires_exact_monotonic_expected_events(self):
        response = {
            "ok": True,
            "events": [
                {
                    "seq": 1,
                    "type": "message",
                    "payload": {
                        "source_seq": 1,
                        "text": "event 1",
                        "timestamp": "2026-07-20T00:00:00Z",
                    },
                },
                {
                    "seq": 2,
                    "type": "message",
                    "payload": {
                        "source_seq": 2,
                        "text": "event 2",
                        "timestamp": "2026-07-20T00:00:01Z",
                    },
                },
            ],
        }
        self.assertEqual(
            harness_support.validate_event_evidence(response, expected_count=2),
            {"event_count": 2, "event_sequences": [1, 2]},
        )

        response["events"][1]["seq"] = 1
        with self.assertRaisesRegex(RuntimeError, "event sequences"):
            harness_support.validate_event_evidence(response, expected_count=2)

    def test_event_evidence_rejects_missing_or_mismatched_payload(self):
        invalid_payloads = (
            None,
            {},
            {
                "source_seq": 2,
                "text": "event 1",
                "timestamp": "2026-07-20T00:00:00Z",
            },
            {
                "source_seq": 1,
                "text": "wrong",
                "timestamp": "2026-07-20T00:00:00Z",
            },
            {"source_seq": 1, "text": "event 1", "timestamp": ""},
        )
        for payload in invalid_payloads:
            with self.subTest(payload=payload):
                response = {
                    "ok": True,
                    "events": [
                        {"seq": 1, "type": "message", "payload": payload}
                    ],
                }
                with self.assertRaises(RuntimeError):
                    harness_support.validate_event_evidence(
                        response, expected_count=1
                    )

    def test_omitted_events_field_means_empty_event_set(self):
        self.assertEqual(
            harness_support.validate_event_evidence({"ok": True}, expected_count=0),
            {"event_count": 0, "event_sequences": []},
        )

    def test_partial_or_failed_result_sets_never_pass(self):
        self.assertFalse(harness_support.all_results_pass([{"ok": True}], 2))
        self.assertFalse(
            harness_support.all_results_pass([{"ok": True}, {"ok": False}], 2)
        )
        self.assertTrue(
            harness_support.all_results_pass([{"ok": True}, {"ok": True}], 2)
        )

    def test_process_group_members_are_exact_and_sorted(self):
        ps_result = subprocess.CompletedProcess(
            args=[], returncode=0, stdout=" 90 700\n 12 42\n 11 42\n 91 420\n", stderr=""
        )
        with mock.patch.object(
            harness_support.subprocess, "run", return_value=ps_result
        ) as run:
            members = harness_support.process_group_members(42)

        self.assertEqual(members, [11, 12])
        run.assert_called_once_with(
            ["ps", "-A", "-o", "pid=,pgid="],
            capture_output=True,
            text=True,
            timeout=5,
        )

    def test_persistence_queries_return_pgid_offset_and_outbox_status(self):
        with tempfile.TemporaryDirectory() as tempdir:
            store_path = Path(tempdir) / "store.sqlite"
            with closing(sqlite3.connect(store_path)) as db, db:
                db.execute(
                    "CREATE TABLE runs (id TEXT PRIMARY KEY, state TEXT, "
                    "supervisor_pgid INTEGER, committed_offset INTEGER)"
                )
                db.execute(
                    "CREATE TABLE finalization_outbox (run_id TEXT PRIMARY KEY, "
                    "terminal_state TEXT, acknowledged INTEGER, "
                    "acknowledged_at TEXT, diagnostic TEXT)"
                )
                db.execute(
                    "INSERT INTO runs VALUES ('run-1', 'completed', 321, 99)"
                )
                db.execute(
                    "INSERT INTO finalization_outbox VALUES "
                    "('run-1', 'completed', 1, 'now', '')"
                )

            self.assertEqual(
                harness_support.read_run_persistence(store_path, "run-1"),
                {
                    "state": "completed",
                    "supervisor_pgid": 321,
                    "committed_offset": 99,
                },
            )
            self.assertEqual(
                harness_support.read_outbox_status(store_path, "run-1"),
                {
                    "terminal_state": "completed",
                    "acknowledged": 1,
                    "acknowledged_at": "now",
                    "diagnostic": "",
                },
            )


if __name__ == "__main__":
    unittest.main()
