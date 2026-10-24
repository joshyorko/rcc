import sys
import tempfile
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).parent))
from holotree import run_once


class HolotreeBenchmarkTest(unittest.TestCase):
    def test_copy_bytes_and_verified_digest_match_source_files(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory) / "sample"
            phases = run_once(root, 101)
            source_bytes = sum(path.stat().st_size for path in (root / "source").rglob("*") if path.is_file())
        self.assertEqual([phase["phase"] for phase in phases],
                         ["inventory_and_hash", "materialize_copy", "verify_materialization"])
        self.assertEqual(phases[0]["bytes"], source_bytes)
        self.assertEqual(phases[1]["bytes"], source_bytes)
        self.assertEqual(phases[2]["bytes"], source_bytes)
        self.assertEqual(phases[0]["digest"], phases[2]["digest"])


if __name__ == "__main__":
    unittest.main()
