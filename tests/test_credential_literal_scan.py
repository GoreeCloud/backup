import collections
import importlib.util
import json
from pathlib import Path
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]
SPEC = importlib.util.spec_from_file_location(
    "check_credential_literals", ROOT / "scripts/check_credential_literals.py"
)
assert SPEC and SPEC.loader
SCAN = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(SCAN)


class CredentialLiteralScanTest(unittest.TestCase):
    def test_new_aws_and_github_shapes_detected_without_raw_value(self):
        # Assemble synthetic examples rather than storing any contiguous
        # credential-shaped literal in the source of this test.
        aws = ("A" + "KIA" + "Q" * 16).encode()
        github = ("gh" + "p_" + "Z" * 36).encode()
        found = SCAN.scan_blob("fixtures/new.py", b"test = " + aws + b"\npat=" + github)
        self.assertEqual(sum(found.values()), 2)
        self.assertEqual(
            {kind for _, kind, _ in found},
            {"aws_access_key_id", "github_classic_token"},
        )
        for _, _, digest in found:
            self.assertEqual(len(digest), 64)
            self.assertNotIn(aws.decode(), str(found))
            self.assertNotIn(github.decode(), str(found))

    def test_fingerprint_baseline_limits_count_and_file(self):
        value = ("A" + "SIA" + "1" * 16).encode()
        known = SCAN.scan_blob("old/vendor-example.go", value)
        self.assertEqual(SCAN.additions(known, known), [])
        doubled = SCAN.scan_blob("old/vendor-example.go", value + b"\n" + value)
        self.assertEqual(
            SCAN.additions(doubled, known),
            [("old/vendor-example.go", "aws_access_key_id", 1)],
        )
        moved = SCAN.scan_blob("new/file.go", value)
        self.assertEqual(
            SCAN.additions(moved, known),
            [("new/file.go", "aws_access_key_id", 1)],
        )
        self.assertEqual(SCAN.additions(collections.Counter(), known), [])

    def test_binary_ignored_and_invalid_no_prefix(self):
        self.assertFalse(SCAN.scan_blob("binary.dat", b"\x00" + (b"AKIA" + b"A" * 16)))
        self.assertFalse(SCAN.scan_blob("not-a-key", b"AKIA123"))
        self.assertFalse(SCAN.scan_blob("not-a-key", b"junk" + b"ghp_" + b"x" * 12))

    def test_baseline_invalid_entry_rejected(self):
        with tempfile.TemporaryDirectory() as directory:
            file = Path(directory) / "baseline.json"
            invalids = [
                {"version": 0, "entries": []},
                {"version": 1, "entries": [{"path": "../escape", "kind": "aws_access_key_id", "sha256": "0" * 64, "count": 1}]},
                {"version": 1, "entries": [{"path": "source", "kind": "unrecognized", "sha256": "0" * 64, "count": 1}]},
                {"version": 1, "entries": [{"path": "source", "kind": "aws_access_key_id", "sha256": "0" * 64, "count": 0}]},
            ]
            for invalid in invalids:
                file.write_text(json.dumps(invalid), encoding="utf-8")
                with self.assertRaises(ValueError):
                    SCAN.load_baseline(file)

    def test_tracked_baseline_contains_only_fingerprints(self):
        raw = SCAN.BASELINE.read_text(encoding="utf-8")
        expected = SCAN.load_baseline()
        self.assertEqual(sum(expected.values()), 26)
        self.assertNotIn("AKIA", raw)
        self.assertNotIn("ASIA", raw)
        self.assertNotIn("ghp_", raw)
        self.assertIn("third_party/rclone", raw)


if __name__ == "__main__":
    unittest.main()
