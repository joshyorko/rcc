import json
import hashlib
import os
import sys
import tempfile
import unittest
import zipfile
from contextlib import ExitStack
from pathlib import Path
from unittest import mock

from invoke import Collection


ROOT = Path(__file__).parents[1]
sys.path.insert(0, str(ROOT))
import tasks  # noqa: E402
from robot_tests.environment_artifacts import library as artifact_robot_library  # noqa: E402


class ArtifactTaskTests(unittest.TestCase):
  def test_self_host_uses_the_archive_legacy_blueprint(self):
    with tempfile.TemporaryDirectory() as directory:
      archive = Path(directory) / "fixture.rcca"
      blueprint = b"channels:\n- conda-forge\ndependencies:\n- python=3.11\n"
      digest = hashlib.sha256(blueprint).hexdigest()
      manifest = {"legacyBlueprint": {"digest": "sha256:" + digest}}
      with zipfile.ZipFile(archive, "w") as carrier:
        carrier.writestr("rcc-environment/manifest.json", json.dumps(manifest))
        carrier.writestr(f"rcc-environment/legacy-blueprints/{digest}", blueprint)
      self.assertEqual(tasks._archive_legacy_blueprint(archive), blueprint)

  def test_self_host_installs_verified_archive_legacy_closure(self):
    with tempfile.TemporaryDirectory() as directory:
      root = Path(directory)
      archive = root / "fixture.rcca"
      catalog = b"catalog"
      stored = b"stored-object"
      catalog_digest = hashlib.sha256(catalog).hexdigest()
      stored_digest = hashlib.sha256(stored).hexdigest()
      legacy_id = "a" * 64
      manifest = {"catalogs": [{"digest": "sha256:" + catalog_digest, "legacyName": "fixture-v12"}]}
      index = {"entries": [{"storedDigest": "sha256:" + stored_digest, "storedSize": len(stored), "legacyObjectId": legacy_id}]}
      with zipfile.ZipFile(archive, "w") as carrier:
        carrier.writestr("rcc-environment/manifest.json", json.dumps(manifest))
        carrier.writestr("rcc-environment/object-index.json", json.dumps(index))
        carrier.writestr(f"rcc-environment/catalogs/{catalog_digest}", catalog)
        carrier.writestr(f"rcc-environment/objects/{stored_digest}", stored)
      home = root / "home"
      tasks._install_archive_legacy_closure(home, archive)
      self.assertEqual((home / "hololib" / "catalog" / "fixture-v12").read_bytes(), catalog)
      self.assertEqual((home / "hololib" / "library" / "aa" / "aa" / "aa" / legacy_id).read_bytes(), stored)

  def test_self_host_archive_uses_explicit_local_trust_policy(self):
    source = (ROOT / "tasks.py").read_text()
    block = source.split('candidate_command =', 1)[1].split('candidate_result =', 1)[0]
    for marker in ('--trust-carrier', '--trust-carrier-type', 'filesystem', '--permissive-local'):
      self.assertIn(marker, block)

  def test_self_host_accepts_exact_pre_feature_env_command_rejection(self):
    source = (ROOT / "tasks.py").read_text()
    self.assertIn("'\"env\"' in old_error", source)

  def test_self_host_legacy_fixture_declares_artifacts_directory(self):
    source = (ROOT / "tasks.py").read_text()
    self.assertIn('condaConfigFile: conda.yaml\\nartifactsDir: output\\n', source)

  def test_artifact_tasks_are_registered_in_invoke_and_toolkit(self):
    collection = Collection.from_module(tasks)
    expected = {
        "artifactFocused",
        "artifactRace",
        "artifactVertical",
        "artifactRobot",
        "goVet",
        "binaryInventory",
        "selfHost",
        "releaseCandidate",
        "coordinationAcceptance",
        "largeStream",
    }
    assert expected <= set(collection.task_names)

    toolkit = (ROOT / "developer" / "toolkit.yaml").read_text()
    for name in expected:
        assert f"  {name}:" in toolkit
        assert f"call_invoke.py {name}" in toolkit

  def test_dagger_workflow_calls_pinned_module_entrypoint(self):
    workflow = (ROOT / ".github" / "workflows" / "dagger.yaml").read_text()
    engine = json.loads((ROOT / "dagger.json").read_text())["engineVersion"]
    self.assertIn("args: run-robot-tests --source .", workflow)
    self.assertIn(f'version: "{engine}"', workflow)


  def test_binary_inventory_matrix_is_exact(self):
    self.assertEqual(tasks._binary_inventory_targets(), [
        ("linux", "amd64", "linux64", ""),
        ("darwin", "amd64", "macos64", ""),
        ("darwin", "arm64", "macosarm64", ""),
        ("windows", "amd64", "windows64", ".exe"),
    ])

  def test_native_robot_matrix_has_explicit_platform_receipts(self):
    workflow = (ROOT / ".github" / "workflows" / "rcc.yaml").read_text()
    self.assertIn("platform: linux-amd64", workflow)
    self.assertIn("platform: macos-amd64", workflow)
    self.assertIn('os: "macos-15-intel"', workflow)
    self.assertNotIn('os: "macos-13"', workflow)
    self.assertIn("platform: macos-arm64", workflow)
    self.assertIn("platform: windows-amd64", workflow)
    self.assertIn("RCC_NATIVE_PLATFORM", workflow)
    self.assertIn("native-runtime-receipt.json", workflow)
    self.assertIn("name: Native Runtime", workflow)
    self.assertIn("- robot", workflow)

  def test_native_matrix_uses_exact_binary_and_test_produced_receipt(self):
    workflow = (ROOT / ".github" / "workflows" / "rcc.yaml").read_text()
    native_job = workflow.split("\n  robot:\n", 1)[1].split("\n  release:\n", 1)[0]
    self.assertIn("needs: build", native_job)
    self.assertIn("actions/download-artifact", native_job)
    self.assertIn("RCC_REAL_BINARY", native_job)
    self.assertIn("RCC_REAL_BINARY_SHA256", native_job)
    self.assertIn("RCC_REAL_RECEIPT_FILE", native_job)
    self.assertIn("RCC_SOURCE_SHA: ${{ github.event.pull_request.head.sha || github.sha }}", native_job)
    self.assertIn("TestRealCurrentRCCAtoBVertical", native_job)
    self.assertIn("Run Windows process supervision and artifact lock acceptance", native_job)
    self.assertIn("TestExecuteCancellationDoesNotWaitForGrandchildInheritedStreams", native_job)
    self.assertIn("TestIndependentProcessArtifactLockContentionBlocks", native_job)
    self.assertIn("Run Windows Robot binary spelling acceptance", native_job)
    self.assertIn("environment_artifact_binary.robot", native_job)
    self.assertIn("Run Windows Environment Artifact Robot acceptance against exact binary", native_job)
    self.assertIn("Run macOS system-alias no-follow acceptance", native_job)
    self.assertIn("TestDarwinSystemAliasPathsPreserveNoFollow", native_job)
    self.assertIn("TestDarwinRPathArtifactRelocatesAndExecutesColdAndWarm", native_job)
    self.assertIn("timeout-minutes: 8", native_job)
    self.assertIn("go test -timeout 30m", native_job)
    self.assertNotIn('"lifecycle": [', workflow)

  def test_promotion_receipt_commit_validation_rejects_unbound_receipts(self):
    validator = getattr(tasks, "_validate_receipt_commit", None)
    self.assertIsNotNone(validator, "promotion receipt commit validator is missing")
    expected = "a" * 40
    self.assertEqual(validator({"commitSha": expected}, expected), expected)
    for payload, candidate in (
        ({}, expected),
        ({"commitSha": "abc"}, expected),
        ({"commitSha": "g" * 40}, expected),
        ({"commitSha": "b" * 40}, expected),
    ):
      with self.assertRaises(ValueError):
        validator(payload, candidate)

  def test_native_acceptance_fixture_exercises_sqlite_extension(self):
    task = (ROOT / "robot_tests" / "environment_artifacts" / "task.py").read_text()
    self.assertIn("import sqlite3", task)
    self.assertIn("nativeImport", task)
    self.assertIn("nativeExtension", task)
    self.assertIn("sqliteVersion", task)

  def test_native_robot_binary_uses_the_platform_executable_suffix(self):
    self.assertTrue(artifact_robot_library.native_rcc_binary("nt").endswith("build/rcc.exe"))
    self.assertTrue(artifact_robot_library.native_rcc_binary("posix").endswith("build/rcc"))

  def test_native_robot_environment_preserves_windows_home_identity(self):
    windows = {
        "PATH": "C:\\Windows\\System32",
        "USERPROFILE": "C:\\Users\\runner",
        "HOMEDRIVE": "C:",
        "HOMEPATH": "\\Users\\runner",
        "USERNAME": "runner",
        "COMSPEC": "C:\\Windows\\System32\\cmd.exe",
    }
    with mock.patch.dict("os.environ", windows, clear=True):
      environment = artifact_robot_library.environment_artifact_process_environment("C:\\rcc-home")
    for name, value in windows.items():
      self.assertEqual(environment[name], value)

  def test_multi_platform_index_includes_the_native_runner_platform(self):
    manifest = {
        "artifactDigest": "sha256:" + "1" * 64,
        "specification": {"digest": "sha256:" + "2" * 64},
    }
    with tempfile.TemporaryDirectory() as directory:
      archive = Path(directory) / "artifact.rcca"
      output = Path(directory) / "index.json"
      with zipfile.ZipFile(archive, "w") as carrier:
        carrier.writestr("rcc-environment/manifest.json", json.dumps(manifest))
      with mock.patch("platform.system", return_value="Darwin"), mock.patch("platform.machine", return_value="arm64"):
        artifact_robot_library.create_multi_platform_index(archive, output)
      index = json.loads(output.read_text())
    platforms = [entry["platform"] for entry in index["artifacts"]]
    self.assertIn({"os": "darwin", "arch": "arm64", "rccPlatform": "darwin_arm64"}, platforms)

  def test_wrong_platform_index_excludes_the_native_runner_platform(self):
    manifest = {
        "artifactDigest": "sha256:" + "1" * 64,
        "specification": {"digest": "sha256:" + "2" * 64},
    }
    with tempfile.TemporaryDirectory() as directory:
      archive = Path(directory) / "artifact.rcca"
      output = Path(directory) / "index.json"
      with zipfile.ZipFile(archive, "w") as carrier:
        carrier.writestr("rcc-environment/manifest.json", json.dumps(manifest))
      with mock.patch("platform.system", return_value="Darwin"), mock.patch("platform.machine", return_value="x86_64"):
        artifact_robot_library.create_wrong_platform_index(archive, output)
      index = json.loads(output.read_text())
    platforms = [entry["platform"] for entry in index["artifacts"]]
    self.assertNotIn({"os": "darwin", "arch": "amd64", "rccPlatform": "darwin_amd64"}, platforms)

  def test_real_vertical_receipt_is_evidence_backed(self):
    source = (ROOT / "environmentlifecycle" / "real_vertical_test.go").read_text()
    for marker in ("LeaseID", "ProviderObjectGets", "runRealMismatchCheck", "RCC_REAL_BINARY_SHA256", "sqlite3"):
      self.assertIn(marker, source)

  def test_large_stream_gate_is_release_wired_and_receipt_backed(self):
    source = (ROOT / "tasks.py").read_text()
    self.assertIn("def largeStream", source)
    self.assertIn("RCC_REAL_LARGE_STREAM", source)
    self.assertNotIn("Skipping largeStream", source)
    self.assertIn('"largeStream"', source.split("def releaseCandidate", 1)[1])
    http_tests = (ROOT / "artifactprovider" / "http_test.go").read_text()
    for marker in ("TestHTTPMultiGiByteStreamingAcceptance", "RCC_LARGE_STREAM_RECEIPT", "restartPolicy", "bufferBytes"):
      self.assertIn(marker, http_tests)

  def test_coordination_receipt_binds_source_and_binary(self):
    source = (ROOT / "tasks.py").read_text()
    self.assertIn('payload["commitSha"]', source)
    self.assertIn('payload["binarySha256"]', source)

  def test_real_receipt_separates_exact_binary_cli_from_source_api(self):
    source = (ROOT / "environmentlifecycle" / "real_vertical_test.go").read_text()
    self.assertIn("exactBinaryCLI", source)
    self.assertIn("sourceAPI", source)
    self.assertIn("runExactBinaryCLIVertical", source)

  def test_native_receipt_binds_robot_exact_binary_evidence(self):
    workflow = (ROOT / ".github" / "workflows" / "rcc.yaml").read_text()
    suite = (ROOT / "robot_tests" / "environment_artifacts.robot").read_text()
    library = (ROOT / "robot_tests" / "environment_artifacts" / "library.py").read_text()
    self.assertIn("robotExactBinary", workflow)
    self.assertIn("Write Native Runtime Robot Evidence", suite)
    self.assertIn("write_native_runtime_robot_evidence", library)
    self.assertNotIn("binding skipped", workflow)
    self.assertIn("missing exact CLI receipt or Robot evidence", workflow)
    for marker in ("materializationId", "nativeImport", "leaseReleased", "providerDeadWarmReuse", "providerObjectGets", "worker capability receipt"):
      self.assertIn(marker, workflow)

  def test_native_receipt_records_platform_specific_runtime_probes(self):
    source = (ROOT / "environmentlifecycle" / "real_vertical_test.go").read_text()
    workflow = (ROOT / ".github" / "workflows" / "rcc.yaml").read_text()
    for marker in ("platformChecks", "exactPlatformProbeProgram", "musl", "rosetta2", "junction", "longPath", "machoRPath"):
      self.assertIn(marker, source)
    self.assertIn("TestExactPlatformProbeProgramParsesForEverySupportedOSBranch", source)
    self.assertIn("expected_checks", workflow)
    self.assertIn("caseCollision", workflow)
    self.assertIn("case-insensitive-filesystem", source)
    self.assertNotIn("failed:case-sensitive-filesystem", source)
    self.assertIn("case-sensitive-filesystem", workflow)


  def test_linux_only_tasks_fail_explicitly(self):
    previous = tasks.sys.platform
    tasks.sys.platform = "darwin"
    self.addCleanup(setattr, tasks.sys, "platform", previous)
    with self.assertRaisesRegex(RuntimeError, "Linux-only"):
        tasks._require_linux("artifactRace")


  def test_self_host_receipt_records_isolated_homes_and_checked_sequence(self):
    with tempfile.TemporaryDirectory() as directory:
      tmp_path = Path(directory)
      proof = tmp_path / "proof.json"
      proof.write_text("proof\n")
      receipt = tasks._write_self_host_receipt(
          tmp_path,
          released_binary="/usr/bin/rcc",
          candidate_binary="build/rcc",
          home_a=tmp_path / "home-a",
          home_b=tmp_path / "home-b",
          commands=[
              {"step": "released-build", "argv": ["rcc", "run", "-t", "selfHostBuild"]},
              {"step": "candidate-probe", "argv": ["build/rcc", "run", "-t", "selfHostProbe"]},
          ],
          binary_metadata={
              "released": {"path": "/usr/bin/rcc"},
              "generationA": {"path": "build/self-host/released/rcc"},
              "candidate": {"path": "build/rcc"},
              "generationB": {"path": "build/self-host/candidate/rcc"},
          },
          evidence_paths=[proof],
      )
      data = json.loads(receipt.read_text())
      self.assertRegex(data["commitSha"], r"^[0-9a-f]{40}$")
      self.assertEqual(data["releasedBinary"], "/usr/bin/rcc")
      self.assertEqual(data["candidateBinary"], "build/rcc")
      self.assertNotEqual(data["homes"]["a"], data["homes"]["b"])
      self.assertEqual(data["commands"][0]["argv"][-1], "selfHostBuild")
      self.assertEqual(receipt.parent, tmp_path)

  def test_self_host_receipt_requires_binary_identity_and_evidence(self):
    with self.assertRaises(ValueError):
      tasks._write_self_host_receipt(
          Path(tempfile.mkdtemp()), released_binary="rcc", candidate_binary="build/rcc",
          home_a="a", home_b="b", commands=[])

  def test_self_host_receipt_requires_both_candidate_generations(self):
    with tempfile.TemporaryDirectory() as directory:
      proof = Path(directory) / "proof.json"
      proof.write_text("proof\n")
      with self.assertRaisesRegex(ValueError, "generationB"):
        tasks._write_self_host_receipt(
            directory,
            released_binary="/usr/bin/rcc",
            candidate_binary="build/rcc",
            home_a=Path(directory) / "home-a",
            home_b=Path(directory) / "home-b",
            commands=[],
            binary_metadata={
                "released": {"path": "/usr/bin/rcc"},
                "generationA": {"path": "build/rcc"},
            },
            evidence_paths=[proof],
        )

  def test_self_host_plan_contains_two_v12_compatibility_consumers(self):
    plan = tasks._self_host_command_plan("released", "candidate", Path("fixture/robot.yaml"))
    argv = [entry["argv"] for entry in plan]
    self.assertTrue(any("holotree" in command and "variables" in command for command in argv))
    self.assertGreaterEqual(sum("selfHostBuild" in command for command in argv), 2)
    self.assertGreaterEqual(sum("selfHostProbe" in command for command in argv), 2)

  def test_generation_a_output_is_promoted_byte_for_byte_to_candidate(self):
    with tempfile.TemporaryDirectory() as directory:
      root = Path(directory)
      source = root / "build" / "self-host" / "released" / "rcc"
      candidate = root / "build" / "rcc"
      source.parent.mkdir(parents=True)
      source.write_bytes(b"generation-a-rcc")
      metadata = tasks._promote_self_host_generation(source, candidate)
      self.assertEqual(source.read_bytes(), candidate.read_bytes())
      self.assertEqual(metadata["sourceSha256"], metadata["candidateSha256"])
      self.assertEqual(metadata["source"], str(source.resolve()))
      self.assertEqual(metadata["candidate"], str(candidate.resolve()))

  def test_artifact_and_self_host_go_tasks_use_contained_safe_environment(self):
    focused = tasks._contained_go_env()
    race = tasks._contained_race_env()
    self.assertEqual(focused["CGO_ENABLED"], "0")
    self.assertEqual(focused["GOARCH"], "amd64")
    self.assertEqual(race["CGO_ENABLED"], "1")
    self.assertEqual(race["GOARCH"], "amd64")
    self.assertEqual(race["CC"], "gcc")

  def test_artifact_focused_runs_task_and_release_contract_tests(self):
    commands = []

    class Context:
      def run(self, command, **_kwargs):
        commands.append(command)

    tasks.artifactFocused.body(Context())
    self.assertTrue(any(
        "-m unittest" in command and "test_environment_artifact_tasks.py" in command
        and "test_validate_release_topology.py" in command
        for command in commands
    ))

  def test_artifact_vertical_builds_candidate_and_enables_real_a_b_proof(self):
    calls = []

    class Context:
      def run(self, command, **kwargs):
        calls.append((command, kwargs))

    context = Context()
    with mock.patch.object(tasks, "local") as local_build:
      tasks.artifactVertical.body(context)

    local_build.assert_called_once_with(context, do_test=False)
    self.assertEqual(len(calls), 1)
    command, kwargs = calls[0]
    self.assertIn("TestRealCurrentRCCAtoBVertical", command)
    self.assertEqual(kwargs["env"]["RCC_REAL_ARTIFACT_TEST"], "1")
    self.assertEqual(kwargs["env"]["RCC_REAL_BINARY"], str((ROOT / "build" / "rcc").resolve()))
    self.assertEqual(kwargs["env"]["RCC_NATIVE_PLATFORM"], "linux-amd64")
    self.assertEqual(kwargs["env"]["RCC_REAL_RECEIPT_FILE"], str((ROOT / "tmp" / "native-runtime-receipt.json").resolve()))
    self.assertIn("RCC_SOURCE_SHA", kwargs["env"])
    self.assertRegex(kwargs["env"]["RCC_SOURCE_SHA"], r"^[0-9a-f]{40}$")

  def test_release_workflow_gates_publication_on_contained_release_candidate(self):
    workflow = (ROOT / ".github" / "workflows" / "rcc.yaml").read_text()
    self.assertIn("  release-candidate:", workflow)
    self.assertIn("rcc run -r developer/toolkit.yaml --dev -t releaseCandidate", workflow)
    self.assertIn("      - release-candidate", workflow)

  def test_native_receipt_aggregation_checks_candidate_commit(self):
    workflow = (ROOT / ".github" / "workflows" / "rcc.yaml").read_text()
    native_job = workflow.split("\n  robot:\n", 1)[1].split("\n  release:\n", 1)[0]
    self.assertIn("_validate_receipt_commit", native_job)
    self.assertIn('os.environ["RCC_CANDIDATE_SHA"]', native_job)

  def test_release_candidate_validates_complete_promotion_receipt_closure(self):
    expected = {
        "native-runtime-receipt.json",
        "self-host-v1.json",
        "large-stream-receipt.json",
        "coordination-blackbox-v1.json",
    }
    self.assertEqual(set(getattr(tasks, "_PROMOTION_RECEIPT_FILES", ())), expected)
    validator = getattr(tasks, "_validate_promotion_receipts", None)
    self.assertIsNotNone(validator, "promotion receipt closure validator is missing")
    commit = "a" * 40
    with tempfile.TemporaryDirectory() as directory:
      root = Path(directory)
      for name in expected:
        (root / name).write_text(json.dumps({"commitSha": commit}))
      validator(root, commit)
      for name in sorted(expected):
        path = root / name
        valid = path.read_text()
        path.write_text(json.dumps({"commitSha": "b" * 40}))
        with self.assertRaisesRegex(ValueError, name):
          validator(root, commit)
        path.write_text(valid)

  def test_release_candidate_uploads_complete_promotion_receipt_closure(self):
    workflow = (ROOT / ".github" / "workflows" / "rcc.yaml").read_text()
    release_job = workflow.split("\n  release-candidate:\n", 1)[1].split("\n  robot:\n", 1)[0]
    self.assertIn("tmp/release-candidate-v1.json", release_job)
    for name in tasks._PROMOTION_RECEIPT_FILES:
      self.assertIn(f"tmp/{name}", release_job)

  def test_job_level_environment_uses_only_planning_contexts(self):
    workflow = (ROOT / ".github" / "workflows" / "rcc.yaml").read_text()
    job_env = False
    for line in workflow.splitlines():
      if line.startswith("    env:"):
        job_env = True
        continue
      if job_env and line.startswith("    ") and not line.startswith("      "):
        job_env = False
      if job_env:
        self.assertNotIn("${{ runner.", line)

  def test_release_workflow_documentation_declares_homebrew_token(self):
    guide = (ROOT / ".github" / "workflows" / "README.md").read_text()
    self.assertIn("HOMEBREW_TOOLS_PAT", guide)

  def test_release_validates_homebrew_handoff_before_publication(self):
    workflow = (ROOT / ".github" / "workflows" / "rcc.yaml").read_text()
    self.assertLess(
        workflow.index("- name: Validate Homebrew Update Token"),
        workflow.index("- name: Publish Release"),
    )

  def test_release_candidate_runs_complete_gate_sequence_via_invoke(self):
    commands = []

    class Context:
      def run(self, command, **_kwargs):
        commands.append(command)

    task_names = [
        "artifactFocused", "artifactRace", "artifactVertical", "artifactRobot",
        "binaryInventory", "largeStream", "selfHost", "robot",
        "coordinationAcceptance",
    ]
    with tempfile.TemporaryDirectory() as directory:
      commit = tasks._exact_commit_sha()
      for name in tasks._PROMOTION_RECEIPT_FILES:
        (Path(directory) / name).write_text(json.dumps({"commitSha": commit}))
      with ExitStack() as stack:
        stack.enter_context(mock.patch.dict(
            os.environ, {"RCC_RELEASE_CANDIDATE_RECEIPT_ROOT": directory}))
        for name in task_names:
          stack.enter_context(mock.patch.object(tasks, name, lambda _context: None))
        tasks.releaseCandidate.body(Context())
      self.assertTrue((Path(directory) / "release-candidate-v1.json").is_file())

    self.assertEqual(commands, [
        tasks._invoke_command("artifactFocused"),
        tasks._invoke_command("artifactRace"),
        tasks._invoke_command("artifactVertical"),
        tasks._invoke_command("artifactRobot"),
        tasks._invoke_command("binaryInventory"),
        tasks._invoke_command("largeStream"),
        tasks._invoke_command("robot"),
        tasks._invoke_command("selfHost"),
        tasks._invoke_command("goVet"),
        tasks._invoke_command("coordinationAcceptance"),
    ])

  def test_release_candidate_removes_stale_receipt_before_running_gates(self):
    with tempfile.TemporaryDirectory() as directory:
      receipt = Path(directory) / "release-candidate-v1.json"
      receipt.write_text('{"stale":true}\n')

      class Context:
        def run(self, _command, **_kwargs):
          raise RuntimeError("gate failed")

      with mock.patch.dict(
          os.environ, {"RCC_RELEASE_CANDIDATE_RECEIPT_ROOT": directory}):
        with self.assertRaisesRegex(RuntimeError, "gate failed"):
          tasks.releaseCandidate.body(Context())
      self.assertFalse(receipt.exists())

  def test_release_candidate_fails_before_dispatch_on_non_linux(self):
    previous = tasks.sys.platform
    tasks.sys.platform = "darwin"
    self.addCleanup(setattr, tasks.sys, "platform", previous)

    class Context:
      def run(self, *_args, **_kwargs):
        self.fail("release candidate dispatched work on an unsupported platform")

    with self.assertRaisesRegex(RuntimeError, "Linux-only"):
      tasks.releaseCandidate.body(Context())

  def test_release_candidate_receipt_records_exact_source_sha(self):
    with tempfile.TemporaryDirectory() as directory:
      source = Path(directory) / "source"
      source.write_bytes(b"release-candidate")
      receipt = tasks._write_release_candidate_receipt(
          directory, source=source, commands=["rcc run -t releaseCandidate"],
          gates={"artifactFocused": "passed", "largeStream": "passed"})
      payload = json.loads(receipt.read_text())
      self.assertEqual(payload["schemaVersion"], 1)
      self.assertRegex(payload["commitSha"], r"^[0-9a-f]{40}$")
      self.assertEqual(payload["source"]["sha256"], hashlib.sha256(source.read_bytes()).hexdigest())
      self.assertEqual(payload["source"]["path"], str(source.resolve()))
      self.assertEqual(payload["commands"], ["rcc run -t releaseCandidate"])
      self.assertEqual(payload["gates"], {"artifactFocused": "passed", "largeStream": "passed"})

  def test_go_vet_accepts_only_clean_or_exact_known_baseline(self):
    baseline = "\n".join(tasks._known_go_vet_findings()) + "\n"
    tasks._validate_go_vet_result(1, baseline)
    tasks._validate_go_vet_result(0, "")
    with self.assertRaisesRegex(RuntimeError, "unexpected go vet findings"):
      tasks._validate_go_vet_result(1, baseline + "new/file.go:1:1: new finding\n")

  def test_artifact_race_scope_is_pinned_to_non_legacy_subpackages(self):
    self.assertEqual(tasks._artifact_race_packages(), [
        "./environmentartifact",
        "./artifactprovider",
        "./environmentlifecycle",
        "./htfs",
        "./cmd",
    ])

  def test_self_host_homes_are_outside_the_module_root(self):
    home_a, home_b = tasks._new_self_host_homes()
    for home in (home_a, home_b):
      self.assertNotEqual(home.resolve(), ROOT.resolve())
      with self.assertRaises(ValueError):
        home.resolve().relative_to(ROOT.resolve())
    self.assertNotEqual(home_a, home_b)
    self.assertEqual(home_a.parent.parent, ROOT.parent.resolve())
