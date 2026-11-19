import importlib.util
from pathlib import Path
import unittest

ROOT = Path(__file__).resolve().parents[1]
SPEC = importlib.util.spec_from_file_location("generate_android_theme", ROOT / "scripts/generate_android_theme.py")
assert SPEC is not None and SPEC.loader is not None
MODULE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(MODULE)


class AndroidThemeTokenTest(unittest.TestCase):
    def test_tracked_gc_ui_colors_match_generated_kotlin(self):
        css = MODULE.SOURCE.read_text(encoding="utf-8")
        expected = MODULE.generate(css)
        actual = MODULE.DESTINATION.read_text(encoding="utf-8")
        self.assertEqual(expected, actual)
        self.assertIn("primary = Color(0xFF5146C8)", actual)
        self.assertIn("primary = Color(0xFFBFB8FF)", actual)
        self.assertIn("background = Color(0xFF0D121D)", actual)

    def test_changed_tokens_require_regeneration(self):
        css = MODULE.SOURCE.read_text(encoding="utf-8")
        mutated = css.replace("--gc-brand: #5146C8;", "--gc-brand: #00697A;", 1)
        self.assertNotEqual(MODULE.generate(css), MODULE.generate(mutated))
        with self.assertRaises(ValueError):
            MODULE.generate(css.replace("--gc-brand: #5146C8;", "", 1))

    def test_css_theme_scopes_must_be_unambiguous(self):
        css = MODULE.SOURCE.read_text(encoding="utf-8")
        with self.assertRaises(ValueError):
            MODULE.generate(css + "\n" + MODULE.DARK_START + "\n}\n")


if __name__ == "__main__":
    unittest.main()
