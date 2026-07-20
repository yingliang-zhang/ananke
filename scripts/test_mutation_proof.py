#!/usr/bin/env python3
"""Focused tests for mutation proof attribution from go test JSON events."""

import json
import sys
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))

import mutation_proof


NAMED_TEST = "TestNamedMutation"
PACKAGE = "example.com/ananke/internal/example"


def go_event(action, test=None):
    event = {"Action": action, "Package": PACKAGE}
    if test is not None:
        event["Test"] = test
    return json.dumps(event, separators=(",", ":"))


def event_stream(*events):
    return "\n".join(events) + "\n"


class MutationProofAttributionTests(unittest.TestCase):
    def test_named_test_own_fail_event_is_detected(self):
        output = event_stream(
            go_event("start"),
            go_event("run", NAMED_TEST),
            go_event("output", NAMED_TEST),
            go_event("fail", NAMED_TEST),
            go_event("fail"),
        )

        self.assertEqual(
            mutation_proof.classify_mutation_result(1, output, NAMED_TEST),
            ("behavioral_test_rejection", True),
        )

    def test_named_fail_without_run_event_is_not_detected(self):
        output = event_stream(go_event("fail", NAMED_TEST), go_event("fail"))

        self.assertEqual(
            mutation_proof.classify_mutation_result(1, output, NAMED_TEST),
            ("named_test_incomplete", False),
        )

    def test_exit_one_without_named_test_is_not_detected(self):
        output = event_stream(
            go_event("start"),
            go_event("run", "TestUnrelated"),
            go_event("fail", "TestUnrelated"),
            go_event("fail"),
        )

        self.assertEqual(
            mutation_proof.classify_mutation_result(1, output, NAMED_TEST),
            ("test_not_run", False),
        )

    def test_named_test_pass_plus_unrelated_failure_is_not_detected(self):
        output = event_stream(
            go_event("start"),
            go_event("run", NAMED_TEST),
            go_event("pass", NAMED_TEST),
            go_event("run", "TestUnrelated"),
            go_event("fail", "TestUnrelated"),
            go_event("fail"),
        )

        self.assertEqual(
            mutation_proof.classify_mutation_result(1, output, NAMED_TEST),
            ("not_detected", False),
        )

    def test_compile_and_setup_failures_remain_distinct(self):
        compile_output = event_stream(
            json.dumps(
                {
                    "ImportPath": PACKAGE,
                    "Action": "build-output",
                    "Output": "compiler diagnostic\n",
                },
                separators=(",", ":"),
            ),
            json.dumps(
                {"ImportPath": PACKAGE, "Action": "build-fail"},
                separators=(",", ":"),
            ),
        )
        setup_output = event_stream(go_event("start"), go_event("fail"))

        self.assertEqual(
            mutation_proof.classify_mutation_result(1, compile_output, NAMED_TEST),
            ("compile_failure", False),
        )
        self.assertEqual(
            mutation_proof.classify_mutation_result(1, setup_output, NAMED_TEST),
            ("setup_failure", False),
        )

    def test_timeout_remains_distinct(self):
        output = event_stream(go_event("start"), go_event("run", NAMED_TEST))

        self.assertEqual(
            mutation_proof.classify_mutation_result(124, output, NAMED_TEST),
            ("timeout", False),
        )


if __name__ == "__main__":
    unittest.main()
