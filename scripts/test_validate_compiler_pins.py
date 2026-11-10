import re
import tempfile
import unittest
from pathlib import Path

from scripts.validate_compiler_pins import COMPILER_PINS, validate

ROOT = Path(__file__).resolve().parents[1]


class CompilerPinTests(unittest.TestCase):
    def setUp(self):
        self.scratch = tempfile.TemporaryDirectory()
        self.addCleanup(self.scratch.cleanup)
        self.root = Path(self.scratch.name)
        for name in COMPILER_PINS:
            dest = self.root / name
            dest.parent.mkdir(parents=True, exist_ok=True)
            dest.write_text((ROOT / name).read_text())

    def test_current_checkout(self):
        self.assertEqual(validate(ROOT), [])

    def test_each_missing_or_stale_selector_fails(self):
        for name, (pattern, _) in COMPILER_PINS.items():
            path = self.root / name
            original = path.read_text()
            for match in re.finditer(pattern, original):
                with self.subTest(name=name, offset=match.start()):
                    path.write_text(original[:match.start(1)] + "9.99.99" + original[match.end(1):])
                    self.assertTrue(validate(self.root))
            path.write_text("")
            self.assertTrue(validate(self.root))
            path.write_text(original)

    def test_coordinated_upgrade_passes_without_changing_bootstrap(self):
        for name, (pattern, _) in COMPILER_PINS.items():
            path = self.root / name
            path.write_text(re.sub(pattern, lambda m: m[0].replace(m[1], "1.27.3"), path.read_text()))
        self.assertEqual(validate(self.root), [])

    def test_unreadable_selector_fails(self):
        (self.root / "scripts/release_publication.py").unlink()
        self.assertTrue(validate(self.root))


if __name__ == "__main__":
    unittest.main()
