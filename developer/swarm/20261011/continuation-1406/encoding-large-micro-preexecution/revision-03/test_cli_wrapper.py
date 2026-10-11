"""Lightweight wrapper tests; never imports or runs codec measurement code."""
import importlib.util
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

HERE = Path(__file__).resolve().parent
SPEC = importlib.util.spec_from_file_location("cli_wrapper", HERE / "cli_wrapper.py")
MOD = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(MOD)


class CLIWrapperChecks(unittest.TestCase):
    def test_supervisor_zero_stays_zero_in_process(self):
        self.assertEqual(MOD.supervisor_exit_code(lambda: 0), 0)

    def test_supervisor_nonzero_is_preserved(self):
        self.assertEqual(MOD.supervisor_exit_code(lambda: 7), 7)

    def test_ordinary_supervisor_error_fails_closed(self):
        def fail():
            raise RuntimeError("stub failure")
        self.assertEqual(MOD.supervisor_exit_code(fail), 1)

    def test_process_exit_zero_is_zero(self):
        with tempfile.TemporaryDirectory() as td:
            script = Path(td) / "stub.py"
            script.write_text(
                "import sys\n"
                f"sys.path.insert(0, {str(HERE)!r})\n"
                "from cli_wrapper import supervisor_exit_code\n"
                "sys.exit(supervisor_exit_code(lambda: 0))\n"
            )
            result = subprocess.run([sys.executable, str(script)], capture_output=True, text=True)
        self.assertEqual(result.returncode, 0, result.stderr)

    def test_process_error_exit_is_one(self):
        with tempfile.TemporaryDirectory() as td:
            script = Path(td) / "stub.py"
            script.write_text(
                "import sys\n"
                f"sys.path.insert(0, {str(HERE)!r})\n"
                "from cli_wrapper import supervisor_exit_code\n"
                "def fail(): raise RuntimeError('stub failure')\n"
                "sys.exit(supervisor_exit_code(fail))\n"
            )
            result = subprocess.run([sys.executable, str(script)], capture_output=True, text=True)
        self.assertEqual(result.returncode, 1)
        self.assertIn("RuntimeError: stub failure", result.stderr)


if __name__ == "__main__":
    unittest.main(verbosity=2)
