import sys
import tempfile
import unittest
from pathlib import Path
import re
import json

sys.path.insert(0, str(Path(__file__).parent))
from lifecycle import RCCRunner, _fixture, _resolved_packages, build_report, source_sha


class LifecycleBenchmarkTest(unittest.TestCase):
    def test_report_has_schema_three_and_candidate_provenance(self):
        report = build_report({"rcc_sha": "abc", "binary": "/x/rcc"}, [], [], {"platform": "test"})
        self.assertEqual(report["schema_version"], 3)
        self.assertEqual(report["candidate"]["rcc_sha"], "abc")

    def test_runner_rejects_non_executable_candidate(self):
        with self.assertRaises(ValueError):
            RCCRunner("/does/not/exist", Path("/tmp/home"))

    def test_toolkit_can_resolve_exact_checkout_sha(self):
        self.assertIsNotNone(re.fullmatch(r"[0-9a-f]{40}", source_sha()))

    def test_report_avoids_volatile_timestamp(self):
        report = build_report(
            candidate={"rcc_sha": "abc", "consumer_sha": "def"},
            fixtures=[],
            runs=[],
            context={"platform": "test"},
        )
        self.assertNotIn("generated_at", report)

    def test_fixture_digest_is_independent_of_temporary_path(self):
        with tempfile.TemporaryDirectory() as directory:
            first = _fixture(Path(directory) / "one")
            second = _fixture(Path(directory) / "two")
        self.assertEqual(first["source_digest"], second["source_digest"])
        self.assertEqual(first["files"], second["files"])

    def test_resolved_package_receipt_excludes_channel_url(self):
        with tempfile.TemporaryDirectory() as directory:
            home = Path(directory)
            metadata = home / "holotree" / "materialized" / "conda-meta" / "python.json"
            metadata.parent.mkdir(parents=True)
            metadata.write_text(json.dumps({"name": "python", "version": "3.11.17",
                                            "build": "h123_0", "sha256": "abc",
                                            "channel": "https://secret.example/token"}))
            resolved = _resolved_packages(home)
        self.assertEqual(resolved["status"], "observed")
        self.assertEqual(resolved["packages"][0]["version"], "3.11.17")
        self.assertNotIn("channel", resolved["packages"][0])

    @unittest.skipUnless(sys.platform == "linux", "wait4 peak RSS test requires Linux")
    def test_peak_rss_is_per_command_not_cumulative(self):
        with tempfile.TemporaryDirectory() as directory:
            runner = RCCRunner(sys.executable, Path(directory) / "home")
            large = runner.run(["-c", "x = bytearray(64_000_000); print(len(x))"], "large")
            small = runner.run(["-c", "print('small')"], "small")
        self.assertEqual(large["status"], "measured")
        self.assertEqual(small["status"], "measured")
        self.assertGreater(large["max_rss_bytes"], small["max_rss_bytes"])
        self.assertGreater(large["cpu_ns"], 0)


if __name__ == "__main__":
    unittest.main()
