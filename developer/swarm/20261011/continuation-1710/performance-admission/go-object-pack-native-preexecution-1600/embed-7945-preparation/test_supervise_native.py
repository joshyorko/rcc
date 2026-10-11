#!/usr/bin/env python3
"""Synthetic supervisor controls only; no RCC, fixture, Go, or build access."""
import importlib.util
import os
import pathlib
import sys
import tempfile
import textwrap
import time
import unittest

HERE = pathlib.Path(__file__).resolve().parent
spec = importlib.util.spec_from_file_location("supervise_native", HERE / "supervise_native.py")
sup = importlib.util.module_from_spec(spec)
sys.modules[spec.name] = sup
spec.loader.exec_module(sup)


class SupervisorSyntheticTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix="rcc-supervisor-synthetic-")
        self.root = pathlib.Path(self.temp.name)
        self.logs = self.root / "logs"
        self.logs.mkdir()
        self.ram = self.root / "ram"
        self.ram.mkdir()

    def tearDown(self):
        self.temp.cleanup()

    def run_stub(self, name, code, *, timeout=3, pass_watch=False, budget=None, snapshot=sup.ps_rows):
        script = self.root / f"{name}.py"
        script.write_text(textwrap.dedent(code))
        b = budget or sup.Budget(workspace=self.root, cap=8 * 1024 * 1024, raw_cap=256 * 1024)
        b.preflight(0)
        return sup.run_stage(
            name=name,
            cmd=[sys.executable, str(script)],
            cwd=self.root,
            env={**os.environ, "PYTHONDONTWRITEBYTECODE": "1"},
            stdout_path=self.logs / f"{name}.stdout",
            stderr_path=self.logs / f"{name}.stderr",
            budget=b,
            ram_root=self.ram,
            global_deadline=time.monotonic() + timeout,
            phase_seconds=timeout,
            pass_watch=pass_watch,
            sample_interval=0.01,
            snapshot=snapshot,
        )

    def test_pass_markers_success_numeric_exit_and_quiescence(self):
        code = """
            import sys
            schedule = ['filesystem','pack','pack','filesystem','filesystem','pack','pack','filesystem','filesystem','pack']
            reps = {'filesystem':0,'pack':0}
            for i, arm in enumerate(schedule, 1):
                reps[arm] += 1
                rep = reps[arm]
                print(f'PASS_START {i}/10 arm={arm} replicate={rep}', file=sys.stderr, flush=True)
                print(f'PASS_DONE {i}/10 arm={arm} replicate={rep} wall_ns=123', file=sys.stderr, flush=True)
            raise SystemExit(0)
        """
        result = self.run_stub("markers", code, pass_watch=True)
        self.assertEqual(result["numeric_exit"], 0)
        self.assertIsNone(result["stop_reason"])
        self.assertEqual(len(result["progress_events"]), 20)
        self.assertTrue(result["reader_threads_joined"])
        self.assertTrue(result["process_group_quiescent"])

    def test_exit_between_poll_and_ps_preserves_numeric_exit(self):
        observed = {"called": False}

        def snapshot(proc):
            if not observed["called"]:
                observed["called"] = True
                proc.wait(timeout=2)
            return sup.ps_rows(proc)

        result = self.run_stub("race", "raise SystemExit(7)", snapshot=snapshot)
        self.assertTrue(observed["called"])
        self.assertEqual(result["numeric_exit"], 7)
        self.assertIsNone(result["stop_reason"])
        self.assertTrue(result["reader_threads_joined"])
        self.assertTrue(result["process_group_quiescent"])

    def test_per_pass_deadline_kills_child_and_joins_readers(self):
        prior = sup.PASS_SECONDS
        sup.PASS_SECONDS = 0.06
        try:
            code = """
                import sys, time
                print('PASS_START 1/10 arm=filesystem replicate=1', file=sys.stderr, flush=True)
                time.sleep(1)
            """
            result = self.run_stub("pass-timeout", code, timeout=2, pass_watch=True)
        finally:
            sup.PASS_SECONDS = prior
        self.assertEqual(result["numeric_exit"], -15)
        self.assertIn("timed pass exceeded", result["stop_reason"])
        self.assertTrue(result["reader_threads_joined"])
        self.assertTrue(result["process_group_quiescent"])

    def test_combined_raw_output_cap_fails_before_overflow(self):
        budget = sup.Budget(workspace=self.root, cap=8 * 1024 * 1024, raw_cap=32)
        result = self.run_stub("output-cap", "print('x' * 200000)", budget=budget)
        self.assertIn(result["numeric_exit"], (0, -15, -9))
        self.assertIn("output cap exceeded before write", result["stop_reason"])
        self.assertLessEqual(budget.raw_written, 32)
        self.assertTrue(result["reader_threads_joined"])

    def test_workspace_reservation_rejects_before_artifact_creation(self):
        budget = sup.Budget(workspace=self.root, cap=64)
        with self.assertRaises(sup.GuardFailure):
            budget.preflight(65)
        artifact = self.root / "never-created.bin"
        with self.assertRaises(sup.GuardFailure):
            budget.write_output(artifact, b"x" * 65, 100)
        self.assertFalse(artifact.exists())

    def test_missing_live_pid_snapshot_is_fail_closed(self):
        def missing(_proc):
            return {}
        result = self.run_stub("missing-pid", "import time; time.sleep(1)", snapshot=missing)
        self.assertIn("known live child missing", result["stop_reason"])
        self.assertTrue(result["reader_threads_joined"])
        self.assertTrue(result["process_group_quiescent"])


if __name__ == "__main__":
    unittest.main(verbosity=2)
