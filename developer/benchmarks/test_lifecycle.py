import json
import sys
import tempfile
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).parent))
from lifecycle import build_report, run_benchmark


class LifecycleBenchmarkTest(unittest.TestCase):
    def test_report_has_schema_two_phases_and_correctness_gates(self):
        with tempfile.TemporaryDirectory() as directory:
            report = run_benchmark(Path(directory), repetitions=1)

        self.assertEqual(report["schema_version"], 2)
        self.assertEqual(report["candidate"]["rcc_sha"], "unknown")
        self.assertEqual(
            report["fixtures"][0]["id"], "many-small-files-v1"
        )
        phases = {phase["id"] for phase in report["runs"][0]["phases"]}
        self.assertTrue({"publish", "acquire", "verify", "materialize", "lease", "gc"} <= phases)
        self.assertIn("identity", report["runs"][0]["correctness_gates"])

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
