"""Static/supervisor guard tests; these never invoke Go or the codec runner."""
import importlib.util
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

    def test_compiled_metadata_whitespace_is_normalized_and_fail_closed(self):
        expected = {"build CGO_ENABLED=0", "build GOARCH=amd64", "build GOOS=linux"}
        output = "\n".join(("./rcc-gzip-go: go1.26.9", "\tbuild\tCGO_ENABLED=0",
                              "\tbuild\tGOARCH=amd64", "\tbuild\tGOOS=linux"))
        actual = {" ".join(line.split()) for line in output.splitlines()}
        self.assertIn("go1.26.9", output)
        self.assertTrue(expected <= actual)
        self.assertFalse(expected <= {" ".join(line.split()) for line in output.replace("GOOS=linux", "GOOS=windows").splitlines()})

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
