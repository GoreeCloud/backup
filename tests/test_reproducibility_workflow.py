"""Safe shape and preflight checks for opt-in dual-runner source-build workflow.

The CLI unit tests do not build the multi-hundred-MB pinned engines. Hosted
manual CI is responsible for the actual independent runner hash comparison.
"""
from pathlib import Path
import os
import subprocess
import unittest

ROOT=Path(__file__).resolve().parents[1]
SCRIPT=ROOT/"scripts/rebuild-linux-amd64.sh"
WORKFLOW=ROOT/".github/workflows/reproducibility-manual.yml"

class ReproducibilityWorkflowTest(unittest.TestCase):
    def test_script_is_valid_shell_and_has_fail_closed_build_settings(self):
        completed=subprocess.run(
            ["bash","-n",str(SCRIPT)],
            capture_output=True,timeout=10,check=False
        )
        self.assertEqual(completed.returncode,0)
        raw=SCRIPT.read_text(encoding="utf-8")
        for required in (
            "set -euo pipefail",
            "GOTOOLCHAIN=local","CGO_ENABLED=0","GOOS=linux","GOARCH=amd64",
            "GOCACHE=","-trimpath -buildvcs=false",
            "sha256sum -c --status third_party/sources.sha256",
            "go version -m",
        ):
            self.assertIn(required,raw)
        self.assertNotIn("curl | bash",raw)
        self.assertNotIn("wget | bash",raw)

    def test_wrong_toolchain_fails_without_building(self):
        env=os.environ.copy()
        env["GCB_REPRO_GO_VERSION"]="go0.0.0"
        process=subprocess.run(
            ["bash",str(SCRIPT)],cwd=ROOT,env=env,
            text=True,capture_output=True,timeout=20,check=False,
        )
        self.assertNotEqual(process.returncode,0)
        self.assertIn("Incorrect Go toolchain version",process.stderr)
        self.assertNotIn("Building pinned",process.stdout)

    def test_workflow_two_fresh_build_jobs_and_separate_comparison(self):
        raw=WORKFLOW.read_text(encoding="utf-8")
        for required in (
            "workflow_dispatch:",
            "permissions:\n  contents: read",
            "first:\n    name: build-first-runner",
            "second:\n    name: build-second-runner",
            "compare:\n    name: compare-independent-binary-hashes",
            "needs: [first, second]",
            "go-version: '1.26.0'",
            "runs-on: ubuntu-24.04",
            "scripts/rebuild-linux-amd64.sh",
            "KOPIA_FIRST", "KOPIA_SECOND", "RCLONE_FIRST", "RCLONE_SECOND",
            "if [[ ! \"$value\" =~ ^[a-f0-9]{64}$ ]]",
        ):
            self.assertIn(required,raw)
        self.assertEqual(raw.count("name: Rebuild pinned sources with a fresh compiled object cache"),2)
        self.assertNotIn("actions/upload-artifact@",raw)
        self.assertNotIn("pull_request:",raw)
        self.assertNotIn("push:",raw)

if __name__=="__main__":
    unittest.main()
