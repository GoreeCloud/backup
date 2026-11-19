import hashlib
import importlib.util
from pathlib import Path
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]
SPEC = importlib.util.spec_from_file_location(
    "build_provenance", ROOT / "scripts/build_provenance.py"
)
assert SPEC is not None and SPEC.loader is not None
PROVENANCE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(PROVENANCE)


def build_info(dep_version="v1.2.3", build_flags=True):
    # Deliberately synthetic metadata; the first line is a private local path
    # which must NEVER appear in the resulting provenance JSON.
    result = (
        "/home/private-user/do-not-publish/bin/kopia: go1.26.0\n"
        "\tpath\tgithub.com/kopia/kopia\n"
        "\tmod\tgithub.com/kopia/kopia\t(devel)\t\n"
        "\tdep\texample.org/dependency\t" + dep_version + "\th1:" + "A" * 43 + "=\n"
        "\tdep\texample.org/other\tv0.3.0\n"
        "\tbuild\tGOOS=linux\n"
        "\tbuild\tGOARCH=amd64\n"
        "\tbuild\tCGO_ENABLED=0\n"
    )
    if build_flags:
        result += "\tbuild\t-trimpath=true\n"
    return result


class BuildProvenanceTest(unittest.TestCase):
    def test_parser_keeps_compiled_metadata_but_never_paths(self):
        info = PROVENANCE.parse_build_info(build_info(), "github.com/kopia/kopia")
        self.assertEqual(info["goToolchain"], "go1.26.0")
        self.assertEqual(info["module"], "github.com/kopia/kopia")
        self.assertEqual(len(info["compiledModules"]), 2)
        self.assertEqual(info["settings"]["-trimpath"], "true")
        self.assertEqual(info["compiledModules"][0]["version"], "v1.2.3")
        self.assertNotIn("private-user", str(info))
        self.assertNotIn("do-not-publish", str(info))
        self.assertEqual(info["compiledModules"][0]["sum"], "h1:" + "A" * 43 + "=")

    def test_wrong_main_missing_module_or_build_flags_fail(self):
        for output, expected in [
            (build_info(), "github.com/other/main"),
            (build_info(build_flags=False), "github.com/kopia/kopia"),
            (build_info().replace("\tpath\tgithub.com/kopia/kopia\n", ""), "github.com/kopia/kopia"),
            (build_info().replace(": go1.26.0", ": unknown-toolchain"), "github.com/kopia/kopia"),
            (build_info().replace("h1:" + "A" * 43 + "=", "h1:invalid"), "github.com/kopia/kopia"),
            (build_info().replace("example.org/dependency\tv1.2.3", "example.org/dependency\tbad version"), "github.com/kopia/kopia"),
        ]:
            with self.subTest(output=output[-80:], expected=expected):
                with self.assertRaises(ValueError):
                    PROVENANCE.parse_build_info(output, expected)

    def test_replacement_is_explicit(self):
        data = build_info().replace(
            "\tdep\texample.org/other\tv0.3.0\n",
            "\tdep\texample.org/other\tv0.3.0\n"
            "\t=>\texample.org/other-fork\tv0.3.1\n",
        )
        info = PROVENANCE.parse_build_info(data, "github.com/kopia/kopia")
        record = next(v for v in info["compiledModules"] if v["module"] == "example.org/other")
        self.assertEqual(record["replacement"], "example.org/other-fork")
        self.assertEqual(record["replacementVersion"], "v0.3.1")

    def test_file_digest_changes_on_tampering(self):
        with tempfile.TemporaryDirectory() as base:
            target = Path(base) / "artifact"
            target.write_bytes(b"synthetic bytes only")
            self.assertEqual(
                PROVENANCE.sha256_file(target),
                hashlib.sha256(b"synthetic bytes only").hexdigest(),
            )
            target.write_bytes(b"synthetic bytes modified")
            self.assertNotEqual(PROVENANCE.sha256_file(target), hashlib.sha256(b"synthetic bytes only").hexdigest())

    def test_engine_manifest_scope(self):
        self.assertEqual(
            PROVENANCE.ENGINES,
            {"kopia": "github.com/kopia/kopia", "rclone": "github.com/rclone/rclone"},
        )
        self.assertEqual(len(PROVENANCE.COMMIT_PATTERN.pattern), len(r"^[0-9a-f]{40}$"))


if __name__ == "__main__":
    unittest.main()
