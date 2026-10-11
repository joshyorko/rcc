"""Execute explicitly staged source through the real RCC Runtime consumer."""

from __future__ import annotations

import hashlib
import importlib.util
import json
import os
import sqlite3
import stat
import subprocess
import sys
from pathlib import Path

import pytest


def _create_runtime_catalog_database(data_dir: Path):
    """Create the consumer test catalog using the complete Runtime registry."""
    from actions.server._database import Database
    from actions.server._models import get_all_model_classes, get_model_db_rules

    data_dir.mkdir(mode=0o700, parents=True, exist_ok=True)
    database = Database(data_dir / "catalog.sqlite")
    database.initialize(get_all_model_classes())
    database.create_tables(get_model_db_rules())
    return database


def test_runtime_catalog_database_fixture_creates_directory_and_current_schema(
    tmp_path: Path,
):
    """Exercise the SQLite consumer fixture without an RCC-managed environment."""
    database = _create_runtime_catalog_database(tmp_path / "runtime-data")

    assert database.db_path.is_file()
    with sqlite3.connect(database.db_path) as connection:
        # This table is part of the current registry and must be available to
        # the real staged-source consumer before package admission begins.
        table = connection.execute(
            "SELECT name FROM sqlite_master WHERE type='table' AND name=?",
            ("mcp_catalog_name",),
        ).fetchone()
    assert table == ("mcp_catalog_name",)


@pytest.mark.integration_test
@pytest.mark.real_rcc
@pytest.mark.skipif(
    sys.platform != "linux", reason="RCC publication proof requires Linux"
)
def test_published_artifact_details_preserves_one_rcc_identity_pair(tmp_path: Path):
    """Capture both canonical digests from one real RCC environment publication."""
    if os.environ.get("ACTIONS_REAL_RCC_ARTIFACT_TEST") != "1":
        pytest.skip("set ACTIONS_REAL_RCC_ARTIFACT_TEST=1 for RCC publication proof")

    from actions.server import _rcc_runtime_adapter
    from actions.server._rcc_runtime_adapter import publish_artifact_details

    rcc_value = os.environ.get("ACTIONS_RUNTIME_RCC_BINARY")
    if not rcc_value:
        pytest.fail("ACTIONS_RUNTIME_RCC_BINARY is required for RCC publication proof")
    rcc_binary = Path(rcc_value).resolve(strict=True)
    rcc_sha256 = hashlib.sha256(rcc_binary.read_bytes()).hexdigest()
    assert (
        rcc_sha256 == "7e588c01751ca2ae15ba13ef67f2f4b7567697a5a8389737059a73936f509428"
    )
    rcc_version = subprocess.run(
        [str(rcc_binary), "--version"], capture_output=True, text=True, check=True
    ).stdout.strip()
    assert rcc_version == "v18.19.3"

    package_dir = tmp_path / "minimal-package"
    package_dir.mkdir(mode=0o700)
    environment = package_dir / "package.yaml"
    environment.write_text(
        "version: 0.1\nspec-version: v2\ndependencies:\n"
        "  conda-forge:\n    - python=3.12.15\n",
        encoding="utf-8",
    )

    helper_path = (
        Path(__file__).resolve().parents[2] / "scripts/verify_dakota_rcc_acceptance.py"
    )
    helper_spec = importlib.util.spec_from_file_location(
        "rcc_details_provider_harness", helper_path
    )
    assert helper_spec is not None and helper_spec.loader is not None
    provider_harness = importlib.util.module_from_spec(helper_spec)
    helper_spec.loader.exec_module(provider_harness)
    provider_tmp = tmp_path / "provider-tmp"
    provider_home = tmp_path / "provider-home"
    provider_tmp.mkdir(mode=0o700)
    provider_home.mkdir(mode=0o700)
    provider_environment = os.environ.copy()
    provider_environment.update(
        {
            "ACTIONS_HOME": str(provider_home),
            "ROBOCORP_HOME": str(provider_home),
            "TMPDIR": str(provider_tmp),
        }
    )
    provider, provider_url = provider_harness.start_provider(
        str(rcc_binary),
        tmp_path / "selected-provider",
        provider_environment,
        deadline=provider_harness.Deadline.after(900),
    )

    invocations: list[dict[str, object]] = []

    def recording_runner(*args: str) -> tuple[int, str, str]:
        result = _rcc_runtime_adapter._subprocess_runner(*args)
        invocations.append(
            {
                "args": list(args),
                "returncode": result[0],
                "stdout": result[1],
                "stderr": result[2],
            }
        )
        return result

    try:
        details = publish_artifact_details(
            environment, rcc_binary, provider=provider_url, runner=recording_runner
        )
        assert len(invocations) == 1
        invocation = invocations[0]
        assert invocation["args"] == [
            str(rcc_binary),
            "env",
            "publish",
            "--environment",
            str(environment),
            "--json",
            "--provider",
            provider_url,
        ]
        assert invocation["returncode"] == 0
        raw_json = invocation["stdout"]
        assert isinstance(raw_json, str)
        payload = json.loads(raw_json)
        assert payload["specificationDigest"] == details.specification_digest
        assert payload["artifactDigest"] == details.artifact_digest
    finally:
        provider_harness.terminate_process_tree(provider)
        assert provider.poll() is not None

    repository_root = Path(__file__).resolve().parents[3]
    source_commit = subprocess.check_output(
        ["git", "-C", str(repository_root), "rev-parse", "HEAD"], text=True
    ).strip()
    source_tree = subprocess.check_output(
        ["git", "-C", str(repository_root), "rev-parse", "HEAD^{tree}"], text=True
    ).strip()
    assert source_commit == os.environ.get("RCC_ROLLBACK_CANDIDATE_SHA")
    assert source_tree == os.environ.get("RCC_ROLLBACK_CANDIDATE_TREE")
    control_commit = os.environ.get("RCC_ROLLBACK_CONTROL_SHA")
    control_tree = os.environ.get("RCC_ROLLBACK_CONTROL_TREE")
    assert control_commit and len(control_commit) == 40
    assert control_tree and len(control_tree) == 40

    receipt_path_value = os.environ.get("ACTIONS_RUNTIME_PUBLISHED_DETAILS_RECEIPT")
    if not receipt_path_value:
        pytest.fail("ACTIONS_RUNTIME_PUBLISHED_DETAILS_RECEIPT is required")
    receipt = {
        "schema_version": 1,
        "status": "PASS",
        "source": {"commit": source_commit, "tree": source_tree},
        "control": {"commit": control_commit, "tree": control_tree},
        "rcc": {"version": rcc_version, "sha256": rcc_sha256},
        "publication": {
            "invocation_count": len(invocations),
            "provider": provider_url,
            "args": invocation["args"],
            "specification_digest": details.specification_digest,
            "artifact_digest": details.artifact_digest,
            "raw_json": raw_json,
            "raw_json_sha256": hashlib.sha256(raw_json.encode("utf-8")).hexdigest(),
        },
    }
    receipt_path = Path(receipt_path_value)
    receipt_path.parent.mkdir(parents=True, exist_ok=True)
    receipt_path.write_text(
        json.dumps(receipt, indent=2, sort_keys=True) + "\n", encoding="utf-8"
    )


@pytest.mark.integration_test
@pytest.mark.real_rcc
@pytest.mark.skipif(sys.platform != "linux", reason="source staging requires Linux")
def test_staged_package_executes_in_managed_rcc_runtime(tmp_path: Path, monkeypatch):
    """Consume the exact staged bytes through RCC and verify worker provenance."""
    if os.environ.get("ACTIONS_REAL_RCC_ARTIFACT_TEST") != "1":
        pytest.skip("set ACTIONS_REAL_RCC_ARTIFACT_TEST=1 for staged RCC proof")

    rcc_binary_value = os.environ.get("ACTIONS_RUNTIME_RCC_BINARY")
    if not rcc_binary_value:
        pytest.fail("ACTIONS_RUNTIME_RCC_BINARY is required for staged RCC proof")
    rcc_binary = Path(rcc_binary_value).resolve(strict=True)
    assert rcc_binary.is_file()
    rcc_sha256 = hashlib.sha256(rcc_binary.read_bytes()).hexdigest()
    assert (
        rcc_sha256 == "7e588c01751ca2ae15ba13ef67f2f4b7567697a5a8389737059a73936f509428"
    )
    rcc_version = subprocess.run(
        [str(rcc_binary), "--version"], capture_output=True, text=True, check=True
    ).stdout.strip()
    assert rcc_version == "v18.19.3"

    # The acceptance process owns both homes. Never inherit or mutate another
    # task's RCC cache, even when this test is run outside the hosted workflow.
    managed_home = tmp_path / "managed-home"
    runtime_tmp = tmp_path / "runtime-tmp"
    provider_tmp = tmp_path / "provider-tmp"
    for directory in (managed_home, runtime_tmp, provider_tmp):
        directory.mkdir(mode=0o700)
    monkeypatch.setenv("ACTIONS_HOME", str(managed_home))
    monkeypatch.setenv("ROBOCORP_HOME", str(managed_home))
    monkeypatch.setenv("TMPDIR", str(runtime_tmp))
    monkeypatch.setenv("ACTIONS_RUNTIME_RCC_BINARY", str(rcc_binary))
    monkeypatch.setenv("ACTIONS_REAL_RCC_ARTIFACT_TEST", "1")

    source_package = tmp_path / "selected-source"
    source_package.mkdir(mode=0o700)
    manifest_bytes = (
        b"version: 0.1\nspec-version: v2\ndependencies:\n"
        b"  conda-forge:\n    - python=3.12.15\n"
        b"  pypi:\n    - actions-core=1.0.2\n"
    )
    action_bytes = (
        "from actions import action\n"
        "import hashlib\n"
        "import actions\n"
        "import sys\n"
        "from importlib.metadata import version\n"
        "from pathlib import Path\n\n"
        "@action\n"
        "def staged_source_probe() -> dict[str, str]:\n"
        "    source = Path(__file__).resolve()\n"
        "    core = Path(actions.__file__).resolve()\n"
        "    return {\n"
        "        'action_source_sha256': hashlib.sha256(source.read_bytes()).hexdigest(),\n"
        "        'action_source_path': str(source),\n"
        "        'core_origin': str(core),\n"
        "        'core_version': version('actions-core'),\n"
        "        'python_executable': str(Path(sys.executable).resolve()),\n"
        "    }\n"
    ).encode("utf-8")
    (source_package / "package.yaml").write_bytes(manifest_bytes)
    (source_package / "action.py").write_bytes(action_bytes)

    staged_package = tmp_path / "staged-package"
    staged_package.mkdir(mode=0o700)
    source_root_fd = os.open(source_package, os.O_RDONLY | getattr(os, "O_DIRECTORY"))
    staged_root_fd = os.open(staged_package, os.O_RDONLY | getattr(os, "O_DIRECTORY"))
    try:
        from actions.server.deployments.source_staging import stage_selected_files

        staged = stage_selected_files(
            source_root_fd,
            staged_root_fd,
            ["action.py", "package.yaml"],
            protected_input_names=["package.yaml"],
        )
    finally:
        os.close(source_root_fd)
        os.close(staged_root_fd)

    source_digests = {
        "action.py": hashlib.sha256(action_bytes).hexdigest(),
        "package.yaml": hashlib.sha256(manifest_bytes).hexdigest(),
    }
    staged_entries = {entry.path: entry for entry in staged.inventory.entries}
    assert set(staged_entries) == set(source_digests)
    assert {
        name: entry.sha256 for name, entry in staged_entries.items()
    } == source_digests
    assert staged.source.inventory == staged.inventory
    assert all(
        stat.S_IMODE((staged_package / name).stat().st_mode) == 0o644
        for name in source_digests
    )

    helper_path = (
        Path(__file__).resolve().parents[2] / "scripts/verify_dakota_rcc_acceptance.py"
    )
    helper_spec = importlib.util.spec_from_file_location(
        "staged_rcc_provider_harness", helper_path
    )
    assert helper_spec is not None and helper_spec.loader is not None
    provider_harness = importlib.util.module_from_spec(helper_spec)
    helper_spec.loader.exec_module(provider_harness)
    provider_environment = os.environ.copy()
    provider_environment.update(
        {
            "ACTIONS_HOME": str(tmp_path / "provider-home"),
            "ROBOCORP_HOME": str(tmp_path / "provider-home"),
            "TMPDIR": str(provider_tmp),
        }
    )
    Path(provider_environment["ACTIONS_HOME"]).mkdir(mode=0o700)
    provider = None
    pool = None
    repository_root = Path(__file__).resolve().parents[3]
    source_commit = subprocess.check_output(
        ["git", "-C", str(repository_root), "rev-parse", "HEAD"], text=True
    ).strip()
    source_tree = subprocess.check_output(
        ["git", "-C", str(repository_root), "rev-parse", "HEAD^{tree}"], text=True
    ).strip()
    evidence: dict[str, object] = {
        "status": "NOT_RUN",
        "source_commit": source_commit,
        "source_tree": source_tree,
        "rcc_version": rcc_version,
        "rcc_sha256": rcc_sha256,
        "source_inventory": staged.source.inventory.canonical_json.decode("utf-8"),
        "staged_inventory": staged.inventory.canonical_json.decode("utf-8"),
        "source_sha256": source_digests,
        "staged_sha256": {name: entry.sha256 for name, entry in staged_entries.items()},
    }
    execution_passed = False
    try:
        provider, provider_url = provider_harness.start_provider(
            str(rcc_binary),
            tmp_path / "selected-provider",
            provider_environment,
            deadline=provider_harness.Deadline.after(900),
        )
        monkeypatch.setenv("ACTIONS_RUNTIME_RCC_PROVIDER", provider_url)

        import actions.server._models as models
        from actions.server._actions_import import import_action_package
        from actions.server._actions_process_pool import ActionsProcessPool
        from actions.server._models import Action, ActionPackage, Run, RunStatus
        from actions.server._rcc_runtime_adapter import load_descriptor
        from actions.server._settings import Settings

        data_dir = tmp_path / "runtime-data"
        database = _create_runtime_catalog_database(data_dir)
        with database.connect():
            models._global_db = database
            try:
                import_action_package(
                    datadir=data_dir,
                    action_package_dir=str(staged_package),
                    disable_not_imported=False,
                    skip_lint=True,
                    whitelist="",
                )
                package = database.all(ActionPackage)[0]
                action = database.all(Action)[0]
                output_schema = json.loads(action.output_schema)
                assert output_schema.get("type") == "object"
                assert output_schema.get("additionalProperties") == {"type": "string"}
                expected_result_fields = {
                    "action_source_sha256",
                    "action_source_path",
                    "core_origin",
                    "core_version",
                    "python_executable",
                }
                descriptor = load_descriptor(package.env_json)
                assert descriptor is not None
                assert descriptor.rcc_version == "v18.19.3"
                assert descriptor.provider_reference == provider_url
                settings = Settings(
                    datadir=data_dir, artifacts_dir=tmp_path / "artifacts"
                )
                settings.reuse_processes = False
                settings.min_processes = 0
                settings.max_processes = 1
                pool = ActionsProcessPool(settings, {package.id: package}, [action])
                run_dir = tmp_path / "run"
                run_dir.mkdir()
                input_json = run_dir / "input.json"
                result_json = run_dir / "result.json"
                output_file = run_dir / "output.txt"
                input_json.write_text("{}", encoding="utf-8")
                run = Run(
                    id="staged-source-rcc-run",
                    status=RunStatus.NOT_RUN,
                    action_id=action.id,
                    start_time="",
                    run_time=None,
                    inputs="{}",
                    result=None,
                    error_message=None,
                    relative_artifacts_dir="",
                    numbered_id=1,
                )
                with pool.obtain_process_for_action(action) as handle:
                    assert (
                        handle.run_action(
                            run,
                            package,
                            action,
                            input_json,
                            run_dir,
                            output_file,
                            result_json,
                            {},
                            {},
                            False,
                        )
                        == 0
                    )
                payload = json.loads(result_json.read_text(encoding="utf-8"))
                result = payload["result"]
                assert isinstance(result, dict)
                assert set(result) == expected_result_fields
                assert all(
                    isinstance(key, str) and isinstance(value, str)
                    for key, value in result.items()
                )
                assert result["action_source_sha256"] == source_digests["action.py"]
                assert result["core_version"] == "1.0.2"
                managed_root = (managed_home / "holotree").resolve()
                assert Path(result["python_executable"]).is_relative_to(managed_root)
                assert Path(result["core_origin"]).is_relative_to(managed_root)
                assert Path(result["action_source_path"]).is_relative_to(
                    (data_dir / ".rcc-runtime-sources").resolve()
                )
                evidence.update(
                    {
                        "artifact_digest": descriptor.artifact_digest,
                        "typed_action_result": result,
                        "managed_root": str(managed_root),
                    }
                )
                execution_passed = True
            finally:
                models._global_db = None
    finally:
        cleanup_errors = []
        if pool is not None:
            try:
                pool.dispose()
            except Exception as error:
                cleanup_errors.append(f"Runtime pool cleanup: {type(error).__name__}")
        if provider is not None:
            try:
                provider_harness.terminate_process_tree(provider)
                if provider.poll() is None:
                    cleanup_errors.append("selected RCC provider was not reaped")
            except Exception as error:
                cleanup_errors.append(
                    f"selected RCC provider cleanup: {type(error).__name__}"
                )
        evidence["status"] = (
            "PASS" if execution_passed and not cleanup_errors else "FAIL"
        )
        if cleanup_errors:
            evidence["cleanup_errors"] = cleanup_errors
        receipt_path = os.environ.get("ACTIONS_RUNTIME_STAGED_CONSUMER_RECEIPT")
        if receipt_path:
            path = Path(receipt_path)
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text(
                json.dumps(evidence, indent=2, sort_keys=True) + "\n", encoding="utf-8"
            )
    assert evidence["status"] == "PASS"
