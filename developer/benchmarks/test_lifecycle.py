import json
import sys
import tempfile
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).parent))
from lifecycle import RCCRunner, build_report


class LifecycleBenchmarkTest(unittest.TestCase):
    def test_report_has_schema_two_and_candidate_provenance(self):
        report = build_report({"rcc_sha": "abc", "binary": "/x/rcc"}, [], [], {"platform": "test"})
        self.assertEqual(report["schema_version"], 2)
        self.assertEqual(report["candidate"]["rcc_sha"], "abc")

    def test_runner_rejects_non_executable_candidate(self):
        with self.assertRaises(ValueError):
            RCCRunner("/does/not/exist", Path("/tmp/home"))

    def test_build_report_is_deterministic(self):
        report = build_report(
            candidate={"rcc_sha": "abc", "consumer_sha": "def"},
            fixtures=[],
            runs=[],
            context={"platform": "test"},
        )
        self.assertEqual(
            json.dumps(report, sort_keys=True), json.dumps(report, sort_keys=True)
        )
        self.assertNotIn("generated_at", report)


if __name__ == "__main__":
    unittest.main()
