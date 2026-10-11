#!/usr/bin/env python3
"""Synthetic supervisor controls only; no RCC, fixture, Go, or build access."""
import importlib.util
import hashlib
import json
import os
import pathlib
import sys
import tempfile
import textwrap
import time
import unittest

HERE = pathlib.Path(__file__).resolve().parent
spec = importlib.util.spec_from_file_location("supervise_native", HERE / "supervise_native_v8.py")
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

    def test_failed_final_census_does_not_claim_quiescence(self):
        calls = {"n":0}
        def snapshot(proc):
            calls["n"] += 1
            if proc.poll() is not None:
                raise OSError("synthetic final ps failure")
            return sup.ps_rows(proc)
        result = self.run_stub("final-census-error", "import time; time.sleep(1)", snapshot=snapshot)
        self.assertIn("final process census failed", result["stop_reason"])
        self.assertFalse(result["process_group_quiescent"])
        self.assertTrue(result["reader_threads_joined"])

    def test_owned_path_rejects_symlink_ancestor(self):
        outside = self.root / "outside"
        outside.mkdir()
        file = outside / "input"
        file.write_text("x")
        alias = self.root / "alias"
        alias.symlink_to(outside, target_is_directory=True)
        with self.assertRaises(sup.GuardFailure):
            sup.checked_path(alias / "input", self.root, "file")

    def test_readonly_module_cache_mode_projection_preserves_manifest_identity(self):
        original=self.root/"cache-original"; copied=self.root/"cache-readonly"
        file=original/"sub"/"metadata.bin";file.parent.mkdir(parents=True);file.write_bytes(b"ziphash")
        os.chmod(file,0o600)
        size,digest=len(b"ziphash"),hashlib.sha256(b"ziphash").hexdigest()
        tree=hashlib.sha256(b"sub/metadata.bin\0"+str(size).encode()+b"\0"+str(0o600).encode()+b"\0"+digest.encode()+b"\n").hexdigest()
        manifest=self.logs/"tiny-cache-manifest.json"
        manifest.write_text(json.dumps({"tree_sha256":tree,"files":[{"path":"sub/metadata.bin","size":size,"sha256":digest,"mode":0o600}]}))
        copied.mkdir();(copied/"sub").mkdir();target=copied/"sub"/"metadata.bin";target.write_bytes(b"ziphash");os.chmod(target,0o400)
        original_receipt=sup.verify_module_cache(original,manifest,expected_tree=tree,expected_count=1,expected_total=size,source_anchor=self.root,manifest_anchor=self.logs)
        projected_receipt=sup.verify_module_cache(copied,manifest,readonly_projection=True,expected_tree=tree,expected_count=1,expected_total=size,source_anchor=self.root,manifest_anchor=self.logs)
        self.assertEqual(original_receipt["tree_sha256"],projected_receipt["tree_sha256"])
        self.assertNotEqual(original_receipt["observed_modes_tree_sha256"],projected_receipt["observed_modes_tree_sha256"])
        with self.assertRaises(sup.GuardFailure):
            sup.verify_module_cache(copied,manifest,expected_tree=tree,expected_count=1,expected_total=size,source_anchor=self.root,manifest_anchor=self.logs)

    def test_runtime_home_is_private_and_inherited_rcc_controls_are_removed(self):
        prior=sup.RAM
        sup.RAM=self.root
        try:
            runtime=self.root/"runtime";runtime.mkdir(mode=0o700)
            base=dict(os.environ);base["RCC_TEST_SECRET"]="discard-me";base["ROBOCORP_HOME"]="/shared/old-home";base["RCC_HOLOTREE_MODE"]="shared"
            home_env,binding=sup.private_runtime_environment(runtime,base)
        finally:sup.RAM=prior
        self.assertEqual(home_env["HOME"],os.environ["HOME"])
        self.assertEqual(home_env["RCC_HOLOTREE_MODE"],"private")
        self.assertEqual(home_env["ROBOCORP_HOME"],str(runtime/"rcc-home"))
        self.assertNotIn("RCC_TEST_SECRET",home_env)
        self.assertIn("ROBOCORP_HOME",binding["removed_inherited_variable_names"])
        self.assertIn("RCC_TEST_SECRET",binding["removed_inherited_variable_names"])
        self.assertEqual(binding["mode"],0o700)

    def test_frozen_three_input_paths_are_exact_and_wrong_pack_path_fails(self):
        binding=sup.validate_input_paths(sup.INDEX_SOURCE,sup.OFFSET_SOURCE,sup.PACK_SOURCE)
        self.assertEqual(binding["index_path"],str(sup.INDEX_SOURCE))
        self.assertEqual(binding["offset_index_path"],str(sup.OFFSET_SOURCE))
        self.assertEqual(binding["pack_path"],str(sup.PACK_SOURCE))
        with self.assertRaises(sup.GuardFailure):
            sup.validate_input_paths(sup.INDEX_SOURCE,sup.OFFSET_SOURCE,sup.PACK_LAYOUT/"wrong.concat")

    def test_binary_metadata_parser_accepts_exact_tuple_and_rejects_wrong_go(self):
        good = "/tmp/rcc-go-packprobe: go1.26.9\n\tbuild\tCGO_ENABLED=0\n\tbuild\tGOARCH=amd64\n\tbuild\tGOOS=linux\n"
        sup.verify_binary_build_metadata(good)
        for bad in (
            "/tmp/rcc-go-packprobe: go1.26.90\n\tbuild\tCGO_ENABLED=0\n\tbuild\tGOARCH=amd64\n\tbuild\tGOOS=linux\n",
            "/tmp/rcc-go-packprobe: go1.26.9\n\tbuild\tCGO_ENABLED=0\n\tbuild\tGOARCH=amd64\n\tbuild\tGOOS=windows\n",
            "/tmp/rcc-go-packprobe: go1.26.9\n\tbuild\tCGO_ENABLED=1\n\tbuild\tGOARCH=amd64\n\tbuild\tGOOS=linux\n",
        ):
            with self.assertRaises(sup.GuardFailure):
                sup.verify_binary_build_metadata(bad)

    def test_inherited_file_size_limit_rejects_small_soft_or_hard_limit(self):
        required=max(sup.PACK_BYTES,sup.BINARY_CAP)
        accepted=sup.check_inherited_file_size_limit((required,required),required)
        self.assertEqual(accepted['soft_limit_bytes'],required)
        self.assertEqual(accepted['hard_limit_bytes'],required)
        self.assertTrue(accepted['checked_without_changing_parent_limits'])
        for limits in ((required-1,required),(required,required-1)):
            with self.assertRaises(sup.GuardFailure):
                sup.check_inherited_file_size_limit(limits,required)
        unlimited=sup.check_inherited_file_size_limit((sup.resource.RLIM_INFINITY,sup.resource.RLIM_INFINITY),required)
        self.assertIsNone(unlimited['soft_limit_bytes'])
        self.assertIsNone(unlimited['hard_limit_bytes'])

    def test_final_jsonl_validator_checks_all_events_and_closures(self):
        digest = "a" * 64
        schedule = sup.SCHEDULE
        start = {"event":"run_start","result":{"status":"RUNNING","source_commit":sup.SOURCE_COMMIT,"index_sha256":sup.INDEX_SHA,"offset_index_sha256":sup.OFFSET_SHA,"pack_sha256":sup.PACK_SHA,"untimed_pack_slice_count":4649,"untimed_pack_slice_bytes":sup.PACK_BYTES,"untimed_pack_ordered_sha256":digest,"selected_count":4649,"selected_bytes":sup.PACK_BYTES,"pack_file_opens_before_timing":1,"schedule":schedule,"untimed_controls_passed":["a","b","c","d","e"]}}
        events=[start];passes=[];reps={"filesystem":0,"pack":0}
        for position,arm in enumerate(schedule,1):
            reps[arm]+=1
            p={"arm":arm,"replicate":reps[arm],"wall_ns":1,"cpu_user_ns":0,"cpu_system_ns":0,"objects":4649,"bytes":sup.PACK_BYTES,"provider_getobject_calls":4649 if arm=="filesystem" else 0,"pack_file_opens_during_pass":0,"read_at_calls":4649 if arm=="pack" else 0,"proc_io_rchar_delta":0,"proc_io_read_bytes_delta":0,"proc_io_syscr_delta":0,"rss_bytes_sampled_after_pass":1,"ordered_result_sha256":digest}
            passes.append(p);events.append({"event":"pass_complete","position":position,"pass":p})
        final={"status":"PASS","source_commit":sup.SOURCE_COMMIT,"index_sha256":sup.INDEX_SHA,"index_after_sha256":sup.INDEX_SHA,"offset_index_sha256":sup.OFFSET_SHA,"offset_index_after_sha256":sup.OFFSET_SHA,"pack_sha256":sup.PACK_SHA,"pack_after_sha256":sup.PACK_SHA,"untimed_pack_slice_count":4649,"untimed_pack_slice_bytes":sup.PACK_BYTES,"untimed_pack_ordered_sha256":digest,"selected_count":4649,"selected_bytes":sup.PACK_BYTES,"pack_file_opens_before_timing":1,"schedule":schedule,"untimed_controls_passed":["a","b","c","d","e"],"passes":passes,"source_closure_before_sha256":digest,"source_closure_after_sha256":digest}
        events.append({"event":"run_complete","result":final})
        path=self.logs/"synthetic.jsonl"
        path.write_text("".join(__import__("json").dumps(event)+"\n" for event in events))
        receipt=sup.validate_result_jsonl(path)
        self.assertEqual(receipt["event_count"],12)
        final["status"]="FAILED"
        path2=self.logs/"synthetic-bad.jsonl"
        path2.write_text("".join(__import__("json").dumps(event)+"\n" for event in events))
        with self.assertRaises(sup.GuardFailure):
            sup.validate_result_jsonl(path2)


if __name__ == "__main__":
    unittest.main(verbosity=2)
