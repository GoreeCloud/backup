import json
import unittest

from scripts.go_module_sbom import build_inventory, objects_from_stream, package_url


class GoModuleInventoryTest(unittest.TestCase):
    def test_parse_concatenated_json(self):
        data = (
            '{"Path":"github.com/kopia/kopia","Main":true}\n'
            '{"Path":"example.org/module","Version":"v1.2.3"}\n'
        )
        result = objects_from_stream(data)
        self.assertEqual([record["Path"] for record in result], [
            "github.com/kopia/kopia", "example.org/module"
        ])
        with self.assertRaises(ValueError):
            objects_from_stream("")
        with self.assertRaises(ValueError):
            objects_from_stream('{"Path":"x","Error":{"Err":"failed"}}')
        with self.assertRaises(json.JSONDecodeError):
            objects_from_stream('{"Path":"x"} {invalid')

    def test_deterministic_deduplicated_inventory(self):
        modules = {
            "rclone": [
                {"Path": "github.com/rclone/rclone", "Main": True},
                {"Path": "example.org/library", "Version": "v2.0.0"},
                {"Path": "example.org/other", "Version": "v1.0.0"},
            ],
            "kopia": [
                {"Path": "github.com/kopia/kopia", "Main": True},
                {"Path": "example.org/library", "Version": "v1.0.0"},
                {"Path": "example.org/library", "Version": "v2.0.0"},
            ],
        }
        result = build_inventory(modules)
        self.assertEqual(result["bomFormat"], "CycloneDX")
        self.assertEqual(result["specVersion"], "1.6")
        self.assertEqual(len(result["components"]), 5)
        resolved = next(c for c in result["components"]
                        if c["name"] == "example.org/library" and c["version"] == "v2.0.0")
        self.assertEqual(
            resolved["properties"],
            [
                {"name": "goreecloud:resolved-engine", "value": "kopia"},
                {"name": "goreecloud:resolved-engine", "value": "rclone"},
            ],
        )
        self.assertEqual(
            json.dumps(result, sort_keys=True),
            json.dumps(build_inventory(dict(reversed(list(modules.items())))), sort_keys=True)
        )

    def test_replacement_and_purl(self):
        record = build_inventory({
            "kopia": [
                {"Path": "github.com/kopia/kopia"},
                {"Path": "example.org/module", "Version": "v1.0.0",
                 "Replace": {"Path": "example.org/fork", "Version": "v1.0.1"}},
            ],
        })
        target = next(item for item in record["components"] if item["name"] == "example.org/module")
        self.assertEqual(target["version"], "v1.0.1")
        self.assertIn(
            {"name": "goreecloud:replacement-target", "value": "example.org/fork"},
            target["properties"]
        )
        self.assertEqual(package_url("example.org/module", "v1.0.1"),
                         "pkg:golang/example.org/module@v1.0.1")


if __name__ == "__main__":
    unittest.main()
