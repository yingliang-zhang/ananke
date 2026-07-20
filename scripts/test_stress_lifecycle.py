#!/usr/bin/env python3
"""Focused tests for stress harness execution controls."""

import contextlib
import io
import sys
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))

import stress_lifecycle


class StressLifecycleConfigurationTests(unittest.TestCase):
    def test_default_iteration_count_remains_twenty(self):
        self.assertEqual(stress_lifecycle.parse_args([]).iterations, 20)

    def test_one_iteration_focused_smoke_override(self):
        self.assertEqual(
            stress_lifecycle.parse_args(["--iterations", "1"]).iterations,
            1,
        )

    def test_iteration_count_must_be_positive(self):
        with contextlib.redirect_stderr(io.StringIO()):
            with self.assertRaises(SystemExit):
                stress_lifecycle.parse_args(["--iterations", "0"])


if __name__ == "__main__":
    unittest.main()
