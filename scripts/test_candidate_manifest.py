#!/usr/bin/env python3
"""Focused tests for deterministic source-candidate binding."""

import hashlib
import json
import sys
import tempfile
import unittest
from pathlib import Path
from unittest import mock

sys.path.insert(0, str(Path(__file__).resolve().parent))

import harness_support


class SourceManifestTests(unittest.TestCase):
    def setUp(self):
        self.tempdir = tempfile.TemporaryDirectory()
        self.addCleanup(self.tempdir.cleanup)
        self.repo = Path(self.tempdir.name)
        self._write("go.mod", "module example.test/ananke\n")
        self._write("go.sum", "example.test/mod v1.0.0 h1:sum\n")
        self._write("cmd/ananke/main.go", "package main\n")
        self._write("internal/store/store.go", "package store\n")
        self._write("scripts/tool.py", "VALUE = 1\n")

    def _write(self, relative_path, content):
        path = self.repo / relative_path
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(content, encoding="utf-8")
        return path

    def test_ordering_independence(self):
        paths = list(harness_support.candidate_source_paths(self.repo))
        with mock.patch.object(
            harness_support,
            "candidate_source_paths",
            side_effect=(iter(paths), iter(reversed(paths))),
        ):
            forward = harness_support.build_source_manifest(self.repo)
            reverse = harness_support.build_source_manifest(self.repo)

        self.assertEqual(forward, reverse)
        self.assertEqual(
            [entry["path"] for entry in forward["files"]],
            sorted(entry["path"] for entry in forward["files"]),
        )

    def test_included_content_change_changes_aggregate(self):
        before = harness_support.build_source_manifest(self.repo)
        self._write("internal/store/store.go", "package store\n\nconst Version = 2\n")
        after = harness_support.build_source_manifest(self.repo)

        self.assertNotEqual(before["aggregate_sha256"], after["aggregate_sha256"])
        self.assertNotEqual(before["files"], after["files"])

    def test_reports_and_build_are_excluded(self):
        before = harness_support.build_source_manifest(self.repo)
        self._write("reports/generated.py", "not candidate material\n")
        self._write("build/generated/main.go", "package generated\n")
        after_create = harness_support.build_source_manifest(self.repo)
        self._write("reports/generated.py", "changed report\n")
        self._write("build/generated/main.go", "package changed\n")
        after_change = harness_support.build_source_manifest(self.repo)

        self.assertEqual(before, after_create)
        self.assertEqual(before, after_change)

    def test_aggregate_matches_independent_canonical_recomputation(self):
        manifest = harness_support.build_source_manifest(self.repo)
        canonical_payload = {
            "schema": "ananke-source-manifest-v1",
            "files": manifest["files"],
        }
        canonical_json = json.dumps(
            canonical_payload,
            ensure_ascii=True,
            separators=(",", ":"),
            sort_keys=True,
        ).encode("utf-8")

        self.assertEqual(
            manifest["aggregate_sha256"],
            hashlib.sha256(canonical_json).hexdigest(),
        )
        self.assertEqual(manifest["file_count"], len(manifest["files"]))

    def test_symlink_escape_is_rejected(self):
        outside = Path(self.tempdir.name).parent / "outside-candidate.py"
        outside.write_text("outside = True\n", encoding="utf-8")
        self.addCleanup(outside.unlink, missing_ok=True)
        (self.repo / "scripts" / "escape.py").symlink_to(outside)

        with self.assertRaisesRegex(ValueError, "escapes repository root"):
            harness_support.build_source_manifest(self.repo)

    def test_missing_required_root_is_rejected(self):
        (self.repo / "internal" / "store" / "store.go").unlink()
        (self.repo / "internal" / "store").rmdir()
        (self.repo / "internal").rmdir()

        with self.assertRaisesRegex(FileNotFoundError, "required source root"):
            harness_support.build_source_manifest(self.repo)

    def test_bound_report_embeds_the_exact_candidate(self):
        candidate = harness_support.build_source_manifest(self.repo)
        report_path = self.repo / "reports" / "proof.json"
        report_path.parent.mkdir()

        bound = harness_support.write_bound_json_report(
            report_path, {"all_pass": True}, self.repo, candidate
        )

        self.assertEqual(bound["candidate"], candidate)
        self.assertEqual(json.loads(report_path.read_text())["candidate"], candidate)

    def test_source_drift_prevents_report_overwrite(self):
        candidate = harness_support.build_source_manifest(self.repo)
        report_path = self.repo / "reports" / "proof.json"
        report_path.parent.mkdir()
        report_path.write_text("sentinel\n", encoding="utf-8")
        self._write("scripts/tool.py", "VALUE = 2\n")

        with self.assertRaisesRegex(RuntimeError, "changed during harness run"):
            harness_support.write_bound_json_report(
                report_path, {"all_pass": True}, self.repo, candidate
            )

        self.assertEqual(report_path.read_text(encoding="utf-8"), "sentinel\n")


if __name__ == "__main__":
    unittest.main()
