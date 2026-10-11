"""Static/supervisor guard tests; these never invoke Go or the codec runner."""
import importlib.util
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import time
import unittest
from unittest import mock

ROOT = Path(__file__).resolve().parent
SPEC = importlib.util.spec_from_file_location("go_gzip_supervisor", ROOT / "supervise.py")
SUPERVISOR = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(SUPERVISOR)


class StubChild:
    pid = 987654321

    @staticmethod
    def poll():
        return None


class SupervisorTests(unittest.TestCase):
    def test_runner_admission_matches_frozen_plan_revision(self):
        plan = json.loads((ROOT / "plan.json").read_text())
        source = (ROOT / "main.go").read_text()
        expected = plan["status"]
        self.assertEqual(expected, "FROZEN_REVISION_05_ROOT_ADMISSION_REQUIRED")
        self.assertIn(f'if p.Status != "{expected}" {{', source)
        self.assertNotIn("FROZEN_REVISION_02_ROOT_ADMISSION_REQUIRED", source)

    def test_cli_requires_admission_and_returns_64(self):
        env = os.environ.copy()
        env.pop("RCC125_GO_GZIP_ADMISSION", None)
        result = subprocess.run([sys.executable, "-B", str(ROOT / "supervise.py")],
                                cwd=ROOT, env=env, capture_output=True, text=True, timeout=5)
        self.assertEqual(result.returncode, 64, result.stdout + result.stderr)
        self.assertIn("explicit root admission required", result.stderr)
        self.assertFalse((ROOT / "run.log").exists())
        self.assertFalse((ROOT / "launch.json").exists())

    def test_child_numeric_exit_is_preserved_for_zero_and_nonzero(self):
        for expected in (0, 7):
            with self.subTest(expected=expected), tempfile.TemporaryDirectory(prefix="rcc125-supervisor-test-", dir="/dev/shm") as td:
                ramtemp = Path(td)
                (ramtemp / "gocache").mkdir()
                (ramtemp / "gotmp").mkdir()
                env = os.environ.copy()
                record = {"owned_ram_temp": str(ramtemp)}
                with tempfile.TemporaryFile(mode="w+b") as logf:
                    code, failure = SUPERVISOR.run_child(
                        [sys.executable, "-B", "-c", f"import sys; sys.exit({expected})"],
                        env, "stub", time.monotonic(), logf, record)
                self.assertEqual(code, expected)
                self.assertIsNone(failure)
                self.assertEqual(record["stub_exit_code"], expected)

    def test_metadata_run_child_log_is_readable_and_version_is_exact(self):
        metadata = ("./rcc-gzip-go: go1.26.9\n"
                    "\tbuild\tCGO_ENABLED=0\n"
                    "\tbuild\tGOARCH=amd64\n"
                    "\tbuild\tGOOS=linux\n")
        with tempfile.TemporaryDirectory(prefix="rcc125-supervisor-log-test-") as td, \
             tempfile.TemporaryDirectory(prefix="rcc125-supervisor-log-ram-", dir="/dev/shm") as ramtd:
            log_path = Path(td) / "run.log"
            (Path(ramtd) / "gocache").mkdir()
            (Path(ramtd) / "gotmp").mkdir()
            with SUPERVISOR.open_exclusive_log(log_path) as logf:
                offset = logf.tell()
                code, failure = SUPERVISOR.run_child(
                    [sys.executable, "-B", "-c", "import sys; sys.stdout.write(sys.argv[1])", metadata],
                    os.environ.copy(), "metadata_stub", time.monotonic(), logf,
                    {"owned_ram_temp": ramtd})
                size = logf.tell() - offset
                observed = SUPERVISOR.read_log_range(logf, offset, size).decode()
            self.assertEqual(code, 0)
            self.assertIsNone(failure)
            self.assertEqual(observed, metadata)
            self.assertTrue(SUPERVISOR.compiled_metadata_matches(observed))
            wrong = observed.replace("go1.26.9", "go1.26.90")
            self.assertFalse(SUPERVISOR.compiled_metadata_matches(wrong))
            self.assertFalse(SUPERVISOR.compiled_metadata_matches(
                observed.replace("GOOS=linux", "GOOS=windows")))

    def test_missing_live_child_pid_fails_closed(self):
        supervisor_pid = os.getpid()
        fake_ps = subprocess.CompletedProcess(
            args=["ps"], returncode=0,
            stdout=f"{supervisor_pid} 1 {supervisor_pid} S 1234\n", stderr="")
        with mock.patch.object(SUPERVISOR.subprocess, "run", return_value=fake_ps):
            with self.assertRaisesRegex(RuntimeError, "live child PID missing"):
                SUPERVISOR.process_tree_rss(supervisor_pid, StubChild())


if __name__ == "__main__":
    unittest.main()
