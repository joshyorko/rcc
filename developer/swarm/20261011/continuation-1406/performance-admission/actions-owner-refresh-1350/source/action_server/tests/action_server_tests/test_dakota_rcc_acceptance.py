import hashlib
import importlib.util
import json
import os
import signal
import subprocess
import sys
import time
import tomllib
import urllib.error
import urllib.request
import zipfile
from pathlib import Path
from types import SimpleNamespace

import pytest

SCRIPT = Path(__file__).parents[2] / "scripts" / "verify_dakota_rcc_acceptance.py"
HISTORICAL_RECEIPT = (
    Path(__file__).parents[1] / "acceptance_evidence" / "candidate-wheel-67e82ef7.json"
)
CURRENT_RECEIPT = (
    Path(__file__).parents[1] / "acceptance_evidence" / "candidate-wheel-e33987b6.json"
)


def _harness():
    spec = importlib.util.spec_from_file_location("dakota_rcc_acceptance", SCRIPT)
    assert spec is not None and spec.loader is not None
    module = importlib.util.module_from_spec(spec)
    sys.modules[spec.name] = module
    spec.loader.exec_module(module)
    return module


@pytest.fixture
def parent_receipt_control(tmp_path, monkeypatch):
    harness = _harness()
    # Fault injection belongs to this verifier, not pytest's logging or files.
    for name in ("json", "os", "tempfile"):
        monkeypatch.setattr(
            harness, name, SimpleNamespace(**vars(getattr(harness, name)))
        )
    receipt = tmp_path / "receipt.json"
    for key in (
        "ACTIONS_RUNTIME_RCC_BINARY",
        "ACTIONS_ACCEPTANCE_POETRY",
        "ACTIONS_ACCEPTANCE_ROBOCORP_HOME",
    ):
        monkeypatch.setenv(key, "synthetic-unused")
    monkeypatch.setattr(harness, "_cli_platform_is_supported", lambda: True)
    monkeypatch.setattr(harness, "child_environment", lambda *args, **kwargs: {})

    def invoke(worker):
        monkeypatch.setattr(harness, "run_owned_process", worker)
        return harness.main(["--receipt", str(receipt)])

    return harness, receipt, invoke


def _worker_bytes(status="PASS"):
    return (
        json.dumps(
            {
                "acceptance_status": status,
                "cells": {"process_cleanup": "PASS", "authenticated_action": status},
                "unrecognized_worker_field": {"measured": [3, False, None]},
            },
            separators=(",", ":"),
        )
        + "\n"
    ).encode()


@pytest.mark.parametrize("worker_status", ["PASS", "FAIL"])
def test_parent_deadline_demotes_overall_receipt_preserving_worker_measurements(
    parent_receipt_control, worker_status, capsys
):
    harness, receipt, invoke = parent_receipt_control
    original = _worker_bytes(worker_status)
    calls = []

    def timeout(command, **kwargs):
        calls.append((command, kwargs))
        receipt.write_bytes(original)
        raise subprocess.TimeoutExpired(["secret-command"], 1, stderr="secret-stderr")

    assert invoke(timeout) == 1
    assert len(calls) == 1
    assert calls[0][1]["timeout_seconds"] == 2465 == harness.CLI_WATCHDOG_SECONDS
    assert calls[0][0][-1] == "--_worker"
    updated = json.loads(receipt.read_bytes())
    worker = json.loads(original)
    assert updated["acceptance_status"] == "FAIL"
    assert {
        key: value for key, value in updated.items() if key != "supervisor_deadline"
    } == {
        **worker,
        "acceptance_status": "FAIL",
    }
    assert updated["cells"] == worker["cells"]
    assert updated["unrecognized_worker_field"] == worker["unrecognized_worker_field"]
    deadline = updated["supervisor_deadline"]
    preserved = receipt.parent / deadline["worker_receipt_file"]
    assert deadline == {
        "reason": "watchdog_timeout",
        "cleanup_state": "unknown",
        "worker_acceptance_status": worker_status,
        "worker_receipt_sha256": hashlib.sha256(original).hexdigest(),
        "worker_receipt_file": preserved.name,
    }
    assert preserved.read_bytes() == original
    assert preserved.parent == receipt.parent
    assert preserved.name.startswith(".rcc-worker-original-")
    if os.name == "posix":
        assert receipt.stat().st_mode & 0o777 == 0o600
        assert preserved.stat().st_mode & 0o777 == 0o600
    assert not list(receipt.parent.glob(".rcc-deadline-*"))
    output = capsys.readouterr()
    assert "watchdog_timeout" in output.err
    assert "secret" not in output.err + output.out


def test_parent_deadline_annotation_collision_preserves_unknown_worker_field(
    parent_receipt_control, capsys
):
    _, receipt, invoke = parent_receipt_control
    original = b'{"acceptance_status":"PASS","cells":{},"supervisor_deadline":{"unknown":"secret-worker-value"}}'

    def timeout(*args, **kwargs):
        receipt.write_bytes(original)
        raise subprocess.TimeoutExpired(["secret-command"], 1)

    assert invoke(timeout) == 1
    assert receipt.read_bytes() == original
    assert not list(receipt.parent.glob(".rcc-*"))
    output = capsys.readouterr()
    assert "watchdog_timeout" in output.err
    assert "receipt_state=annotation_conflict" in output.err
    assert "secret" not in output.err + output.out


@pytest.mark.parametrize(
    ("original", "state"),
    [
        (None, "unavailable"),
        (b"not-json-secret", "invalid_json"),
        (b"\xff", "invalid_json"),
        (b"[]", "invalid_shape"),
        (b"null", "invalid_shape"),
        (b'{"acceptance_status":"PASS","cells":[]}', "invalid_shape"),
        (b'{"acceptance_status":"PASS","cells":null}', "invalid_shape"),
    ],
)
def test_parent_deadline_never_fabricates_missing_or_malformed_worker_receipt(
    parent_receipt_control, original, state, capsys
):
    _, receipt, invoke = parent_receipt_control

    def timeout(*args, **kwargs):
        if original is not None:
            receipt.write_bytes(original)
        raise subprocess.TimeoutExpired(["secret-command"], 1)

    assert invoke(timeout) == 1
    assert receipt.exists() is (original is not None)
    if original is not None:
        assert receipt.read_bytes() == original
    assert not list(receipt.parent.glob(".rcc-*"))
    output = capsys.readouterr()
    assert "watchdog_timeout" in output.err
    assert f"receipt_state={state}" in output.err
    assert "secret" not in output.err + output.out


@pytest.mark.parametrize(
    ("failure_phase", "state"),
    [
        ("read", "read_failed"),
        ("preserve", "preservation_failed"),
        ("encode", "encode_failed"),
        ("write", "write_failed"),
        ("fsync", "write_failed"),
        ("replace", "replace_failed"),
    ],
)
def test_parent_deadline_persistence_failure_preserves_primary_and_owned_files(
    parent_receipt_control, monkeypatch, failure_phase, state, capsys
):
    harness, receipt, invoke = parent_receipt_control
    original = _worker_bytes()
    unrelated = receipt.parent / ".rcc-deadline-unrelated"
    unrelated.write_bytes(b"not-owned")

    def fail(*args, **kwargs):
        raise OSError("secret-secondary-path-token")

    def timeout(*args, **kwargs):
        receipt.write_bytes(original)
        if failure_phase == "read":
            real_read = Path.read_bytes
            monkeypatch.setattr(
                Path,
                "read_bytes",
                lambda path: fail() if path == receipt else real_read(path),
            )
        elif failure_phase == "preserve":
            monkeypatch.setattr(harness.tempfile, "mkstemp", fail)
        elif failure_phase == "encode":
            monkeypatch.setattr(harness.json, "dumps", fail)
        elif failure_phase == "write":
            real_fdopen = harness.os.fdopen
            calls = 0

            class FailingWriter:
                def __init__(self, stream):
                    self.stream = stream

                def __enter__(self):
                    return self

                def __exit__(self, *args):
                    self.stream.close()

                def __getattr__(self, name):
                    return getattr(self.stream, name)

                def write(self, payload):
                    fail()

            def fail_aggregate_write(*args, **kwargs):
                nonlocal calls
                calls += 1
                stream = real_fdopen(*args, **kwargs)
                return FailingWriter(stream) if calls == 2 else stream

            monkeypatch.setattr(harness.os, "fdopen", fail_aggregate_write)
        elif failure_phase == "fsync":
            real = harness.os.fsync
            calls = 0

            def fail_aggregate(*args, **kwargs):
                nonlocal calls
                calls += 1
                if calls == 2:
                    fail()
                return real(*args, **kwargs)

            monkeypatch.setattr(harness.os, "fsync", fail_aggregate)
        else:
            monkeypatch.setattr(harness.os, "replace", fail)
        raise subprocess.TimeoutExpired(["secret-command"], 1)

    assert invoke(timeout) == 1
    output = capsys.readouterr()
    assert "watchdog_timeout" in output.err
    assert f"receipt_state={state}" in output.err
    assert "secret" not in output.err + output.out
    monkeypatch.undo()
    assert receipt.read_bytes() == original
    assert unrelated.read_bytes() == b"not-owned"
    assert list(receipt.parent.glob(".rcc-deadline-*")) == [unrelated]
    for preserved in receipt.parent.glob(".rcc-worker-original-*"):
        assert preserved.read_bytes() == original


@pytest.mark.parametrize(
    "outcome", ["completed_zero", "completed_nonzero", "exception"]
)
def test_parent_nondeadline_completion_keeps_original_worker_receipt(
    parent_receipt_control, outcome
):
    _, receipt, invoke = parent_receipt_control
    original = _worker_bytes()

    def worker(command, **kwargs):
        receipt.write_bytes(original)
        if outcome == "exception":
            raise RuntimeError("fixed synthetic exception")
        return subprocess.CompletedProcess(
            command, 0 if outcome == "completed_zero" else 7, "", ""
        )

    assert invoke(worker) == (
        0 if outcome == "completed_zero" else 7 if outcome == "completed_nonzero" else 1
    )
    assert receipt.read_bytes() == original
    assert not list(receipt.parent.glob(".rcc-*"))


def test_dakota_acceptance_harness_describes_candidate_wheel_proof():
    result = subprocess.run(
        [sys.executable, str(SCRIPT), "--help"],
        check=False,
        capture_output=True,
        text=True,
        timeout=10,
    )

    assert result.returncode == 0, result.stderr
    assert "candidate-wheel" in result.stdout
    assert "RCC Environment Artifact" in result.stdout


def test_cli_rejects_existing_receipt_before_creating_supervisor_state(tmp_path):
    receipt_path = tmp_path / "previous-pass.json"
    original = b'{"acceptance_status":"PASS","source_sha":"historical"}\n'
    receipt_path.write_bytes(original)
    env = {
        "PATH": os.environ.get("PATH", os.defpath),
        "ACTIONS_RUNTIME_RCC_BINARY": str(tmp_path / "unused-rcc"),
        "ACTIONS_ACCEPTANCE_POETRY": str(tmp_path / "unused-poetry"),
        "ACTIONS_ACCEPTANCE_ROBOCORP_HOME": str(tmp_path / "unused-rcc-home"),
    }

    result = subprocess.run(
        [sys.executable, str(SCRIPT), "--receipt", str(receipt_path)],
        check=False,
        capture_output=True,
        text=True,
        timeout=5,
        env=env,
    )

    assert result.returncode != 0
    assert "existing acceptance receipt" in result.stderr
    assert receipt_path.read_bytes() == original
    assert not (tmp_path / "cli-supervisor").exists()


def test_cli_help_is_available_before_linux_execution_guard(monkeypatch, capsys):
    harness = _harness()
    monkeypatch.setattr(harness.sys, "platform", "darwin")

    with pytest.raises(SystemExit) as exit_info:
        harness.main(["--help"])

    assert exit_info.value.code == 0
    assert "candidate-wheel" in capsys.readouterr().out


def test_cli_refuses_existing_receipt_before_linux_execution_guard(
    tmp_path, monkeypatch, capsys
):
    harness = _harness()
    monkeypatch.setattr(harness.sys, "platform", "darwin")
    receipt_path = tmp_path / "previous-pass.json"
    original = b'{"acceptance_status":"PASS","source_sha":"historical"}\n'
    receipt_path.write_bytes(original)

    result = harness.main(["--receipt", str(receipt_path)])

    assert result == 1
    assert "existing acceptance receipt" in capsys.readouterr().err
    assert receipt_path.read_bytes() == original
    assert not (tmp_path / "cli-supervisor").exists()


def test_historical_candidate_receipt_keeps_failed_wrapper_cell():
    receipt = json.loads(HISTORICAL_RECEIPT.read_text(encoding="utf-8"))

    assert receipt["source_sha"] == "67e82ef7c2bfd529f36d756c2c97c466bd3952e4"
    assert receipt["runtime_mode"] == "candidate-wheel"
    assert receipt["rcc_receipt"]["status"] == "failed"
    assert receipt["rcc_receipt"]["exitCode"] == -1
    assert receipt["rcc_receipt"]["reason"] == "child exited non-zero"
    assert receipt["authenticated_http_status"] == 200
    assert receipt["sqlite_status"] == "passed"
    assert receipt["rcc_receipt"]["verification"]["valid"] is True


def test_current_live_receipt_keeps_action_pass_and_wrapper_failure_separate():
    receipt = json.loads(CURRENT_RECEIPT.read_text(encoding="utf-8"))

    assert receipt["source_sha"] == "e33987b6224fbc0b444c9b8f26908b3040447223"
    assert receipt["cells"]["authenticated_action"] == "PASS"
    assert receipt["cells"]["sqlite_run"] == "PASS"
    assert receipt["cells"]["artifact_verification"] == "PASS"
    assert receipt["cells"]["process_cleanup"] == "PASS"
    assert receipt["cells"]["wrapper_exit"] == "FAIL"
    assert receipt["acceptance_status"] == "FAIL"
    assert receipt["rcc_receipt"]["status"] == "failed"
    assert receipt["rcc_receipt"]["exitCode"] == -1


@pytest.mark.integration_test
@pytest.mark.real_rcc
def test_dakota_local_rcc_action_over_authenticated_http(tmp_path):
    if os.environ.get("ACTIONS_REAL_RCC_ACCEPTANCE") != "1":
        pytest.skip("set ACTIONS_REAL_RCC_ACCEPTANCE=1 for the live local proof")

    harness = _harness()
    receipt_path = Path(
        os.environ.get(
            "ACTIONS_ACCEPTANCE_RECEIPT",
            str(tmp_path / "candidate-wheel-receipt.json"),
        )
    )
    result = harness.run_owned_process(
        [
            sys.executable,
            str(SCRIPT),
            "--mode",
            "candidate-wheel",
            "--receipt",
            str(receipt_path),
        ],
        timeout_seconds=(
            harness.CLI_WATCHDOG_SECONDS + 4 * harness.CLEANUP_GRACE_SECONDS + 5
        ),
        env=harness.child_environment(
            os.environ,
            task_root=receipt_path.parent / "process-root",
            extra={
                "ACTIONS_RUNTIME_RCC_BINARY": os.environ["ACTIONS_RUNTIME_RCC_BINARY"],
                "ACTIONS_ACCEPTANCE_POETRY": os.environ["ACTIONS_ACCEPTANCE_POETRY"],
                "ACTIONS_ACCEPTANCE_ROBOCORP_HOME": os.environ[
                    "ACTIONS_ACCEPTANCE_ROBOCORP_HOME"
                ],
            },
        ),
        cleanup_grace_seconds=harness.CLEANUP_GRACE_SECONDS,
    )

    live_receipt = json.loads(receipt_path.read_text(encoding="utf-8"))
    assert live_receipt["runtime_mode"] == "candidate-wheel"
    assert len(live_receipt["source_sha"]) == 40
    assert len(live_receipt["candidate_wheels"]["actions-core"]["sha256"]) == 64
    assert len(live_receipt["candidate_wheels"]["actions-http-helper"]["sha256"]) == 64
    assert live_receipt["rcc_receipt"]["verification"]["valid"] is True
    assert live_receipt["cells"]["authenticated_action"] == "PASS"
    assert live_receipt["cells"]["sqlite_run"] == "PASS"
    assert live_receipt["cells"]["artifact_verification"] == "PASS"
    assert live_receipt["cells"]["provider_backed_exec_fail_closed"] == "PASS"
    assert live_receipt["cells"]["process_cleanup"] == "PASS"
    assert live_receipt["offline_warm"]["provider_probe_role"] == (
        "count-and-reject-only; serves no artifacts"
    )
    assert live_receipt["offline_warm"]["provider_probe_requests"] > 0
    assert live_receipt["offline_warm"]["provider_probe_stopped"] is True
    assert live_receipt["cells"]["offline_warm_artifact_ready"] == "PASS"
    assert live_receipt["cells"]["offline_warm_action"] == "FAIL"
    assert live_receipt["cells"]["offline_warm_artifact_verification"] == "FAIL"
    assert live_receipt["cells"]["offline_warm_wrapper_exit"] == "FAIL"
    assert live_receipt["cells"]["provider_unavailable"] == "PASS"
    assert live_receipt["cells"]["zero_requests_during_warm_runtime"] == "FAIL"
    assert live_receipt["offline_warm"]["lifecycle_inspect_request_events"] == []
    assert live_receipt["cells"]["warm_process_cleanup"] == "PASS"
    expected_returncode = 0 if live_receipt["acceptance_status"] == "PASS" else 1
    assert result.returncode == expected_returncode, (
        f"acceptance status {live_receipt['acceptance_status']} did not match "
        f"harness return code {result.returncode}"
    )
    assert live_receipt["acceptance_status"] == "FAIL"
    assert live_receipt["cells"]["provider_backed_exec_fail_closed"] == "PASS"


def test_child_environment_keeps_ca_and_proxy_settings_without_credentials(tmp_path):
    harness = _harness()
    source = {
        "PATH": "/usr/bin:/bin",
        "HTTPS_PROXY": "http://proxy.example:3128",
        "NO_PROXY": "example.test",
        "SSL_CERT_FILE": "/etc/ssl/cert.pem",
        "GITHUB_TOKEN": "must-not-cross-the-boundary",
        "AWS_SECRET_ACCESS_KEY": "must-not-cross-the-boundary",
        "ACTIONS_API_KEY": "must-not-cross-the-boundary",
    }

    result = harness.child_environment(source, task_root=tmp_path / "isolated")

    assert result["HTTPS_PROXY"] == source["HTTPS_PROXY"]
    assert result["NO_PROXY"].startswith("example.test")
    assert result["SSL_CERT_FILE"] == source["SSL_CERT_FILE"]
    assert result["HOME"] == str(tmp_path / "isolated" / "home")
    assert not {"GITHUB_TOKEN", "AWS_SECRET_ACCESS_KEY", "ACTIONS_API_KEY"} & set(
        result
    )


@pytest.mark.parametrize("core_metadata_version", ["1.0.3", "1.0.2"])
def test_candidate_wheels_follow_checked_out_versions_and_validate_metadata(
    tmp_path, monkeypatch, core_metadata_version
):
    harness = _harness()
    source_root = tmp_path / "source"
    (source_root / "actions").mkdir(parents=True)
    (source_root / "actions-http-helper").mkdir()
    script_dir = source_root / "action_server" / "scripts"
    script_dir.mkdir(parents=True)
    monkeypatch.setattr(
        harness,
        "__file__",
        str(script_dir / "verify_dakota_rcc_acceptance.py"),
    )
    source_versions = {
        "actions": ("actions-core", "1.0.3"),
        "actions-http-helper": ("actions-http-helper", "1.0.3"),
    }
    for directory, (distribution, version) in source_versions.items():
        (source_root / directory / "pyproject.toml").write_text(
            f'[tool.poetry]\nname = "{distribution}"\nversion = "{version}"\n',
            encoding="utf-8",
        )

    def fake_poetry(command, *, cwd, **kwargs):
        project = tomllib.loads((Path(cwd) / "pyproject.toml").read_text())["tool"][
            "poetry"
        ]
        distribution = project["name"]
        version = project["version"]
        if command[1] == "version":
            return SimpleNamespace(returncode=0, stdout=f"{version}\n", stderr="")

        wheelhouse = Path(command[command.index("--output") + 1])
        filename_distribution = distribution.replace("-", "_")
        wheel = wheelhouse / (f"{filename_distribution}-{version}-py3-none-any.whl")
        metadata_path = f"{filename_distribution}-{version}.dist-info/METADATA"
        metadata_version = (
            core_metadata_version if distribution == "actions-core" else version
        )
        with zipfile.ZipFile(wheel, "w") as archive:
            archive.writestr(
                metadata_path,
                f"Metadata-Version: 2.1\nName: {distribution}\nVersion: {metadata_version}\n\n",
            )
        return SimpleNamespace(returncode=0, stdout="", stderr="")

    monkeypatch.setattr(harness, "run_owned_process", fake_poetry)
    task_root = tmp_path / "task"
    task_root.mkdir()
    arguments = {
        "root": task_root,
        "source_env": {
            "PATH": os.environ.get("PATH", os.defpath),
            "ACTIONS_ACCEPTANCE_ROBOCORP_HOME": str(tmp_path / "rcc-home"),
        },
        "poetry": Path("/pinned/poetry"),
        "deadline": harness.Deadline.after(30),
    }
    if core_metadata_version != "1.0.3":
        with pytest.raises(RuntimeError, match="wheel METADATA"):
            harness.build_candidate_wheels(**arguments)
        return

    candidate_wheels = harness.build_candidate_wheels(**arguments)

    assert candidate_wheels.core.version == "1.0.3"
    assert candidate_wheels.helper.version == "1.0.3"
    assert candidate_wheels.core.path.name == "actions_core-1.0.3-py3-none-any.whl"
    assert candidate_wheels.helper.path.name == (
        "actions_http_helper-1.0.3-py3-none-any.whl"
    )
    assert harness.candidate_version_fields(candidate_wheels) == {
        "actions_core": "1.0.3",
        "actions_http_helper": "1.0.3",
    }
    wheel_records = harness.candidate_wheel_records(candidate_wheels)
    assert wheel_records.keys() == {"actions-core", "actions-http-helper"}
    assert wheel_records["actions-core"]["version"] == "1.0.3"
    assert wheel_records["actions-http-helper"]["version"] == "1.0.3"
    assert wheel_records["actions-core"]["filename"] == (
        "actions_core-1.0.3-py3-none-any.whl"
    )


def test_child_environment_rejects_proxy_credentials(tmp_path):
    with pytest.raises(ValueError, match="proxy settings"):
        _harness().child_environment(
            {"HTTPS_PROXY": "http://user:secret@proxy.example:3128"},
            task_root=tmp_path / "isolated",
        )


def test_candidate_poetry_requires_explicit_pinned_rcc_toolchain(monkeypatch, tmp_path):
    monkeypatch.delenv("ACTIONS_ACCEPTANCE_POETRY", raising=False)
    monkeypatch.delenv("ACTIONS_ACCEPTANCE_ROBOCORP_HOME", raising=False)

    with pytest.raises(RuntimeError, match="ACTIONS_ACCEPTANCE_POETRY"):
        _harness().resolve_poetry(
            os.environ,
            task_root=tmp_path / "tool-root",
            deadline=_harness().Deadline.after(2),
        )


def test_candidate_poetry_rejects_path_outside_rcc_home(tmp_path):
    tool_home = tmp_path / "rcc-home"
    poetry = tmp_path / "host-bin" / "poetry"
    poetry.parent.mkdir()
    poetry.touch()
    poetry.chmod(0o700)

    with pytest.raises(RuntimeError, match="pinned RCC holotree"):
        _harness().resolve_poetry(
            {
                "ACTIONS_ACCEPTANCE_POETRY": str(poetry),
                "ACTIONS_ACCEPTANCE_ROBOCORP_HOME": str(tool_home),
            },
            task_root=tmp_path / "version-check",
            deadline=_harness().Deadline.after(2),
        )


def test_candidate_poetry_must_resolve_under_toolchain_and_report_2_1_1(
    tmp_path, monkeypatch
):
    harness = _harness()
    tool_home = tmp_path / "rcc-home"
    poetry = tool_home / "holotree" / "toolchain" / "bin" / "poetry"
    poetry.parent.mkdir(parents=True)
    poetry.touch()
    poetry.chmod(0o700)
    env = {
        "ACTIONS_ACCEPTANCE_POETRY": str(poetry),
        "ACTIONS_ACCEPTANCE_ROBOCORP_HOME": str(tool_home),
    }
    monkeypatch.setattr(
        harness,
        "run_owned_process",
        lambda command, **kwargs: subprocess.CompletedProcess(
            command, 0, "Poetry (version 2.1.2)\n", ""
        ),
    )

    with pytest.raises(RuntimeError, match="Poetry 2.1.1"):
        harness.resolve_poetry(
            env,
            task_root=tmp_path / "version-check",
            deadline=harness.Deadline.after(2),
        )


def test_candidate_poetry_resolves_pinned_executable_under_rcc_home(
    tmp_path, monkeypatch
):
    harness = _harness()
    tool_home = tmp_path / "rcc-home"
    poetry = tool_home / "holotree" / "toolchain" / "bin" / "poetry"
    poetry.parent.mkdir(parents=True)
    poetry.touch()
    poetry.chmod(0o700)
    env = {
        "ACTIONS_ACCEPTANCE_POETRY": str(poetry),
        "ACTIONS_ACCEPTANCE_ROBOCORP_HOME": str(tool_home),
    }
    monkeypatch.setattr(
        harness,
        "run_owned_process",
        lambda command, **kwargs: subprocess.CompletedProcess(
            command, 0, "Poetry (version 2.1.1)\n", ""
        ),
    )

    assert (
        harness.resolve_poetry(
            env,
            task_root=tmp_path / "version-check",
            deadline=harness.Deadline.after(2),
        )
        == poetry.resolve()
    )


def test_evidence_is_retained_outside_run_temp(tmp_path):
    harness = _harness()
    temp_root = tmp_path / "disposable"
    temp_root.mkdir()
    receipt = tmp_path / "evidence.json"
    evidence = {"source_sha": "a" * 40, "candidate_wheel_sha256": "b" * 64}

    harness.write_evidence(receipt, evidence, temp_root=temp_root)

    assert json.loads(receipt.read_text(encoding="utf-8")) == evidence
    with pytest.raises(FileExistsError):
        harness.write_evidence(receipt, evidence, temp_root=temp_root)
    with pytest.raises(ValueError, match="outside"):
        harness.write_evidence(temp_root / "inside.json", evidence, temp_root=temp_root)


@pytest.mark.skipif(
    os.name == "nt",
    reason="POSIX permission bits do not establish Windows ACL isolation",
)
def test_evidence_receipt_uses_private_posix_mode(tmp_path):
    harness = _harness()
    temp_root = tmp_path / "disposable"
    temp_root.mkdir()
    receipt = tmp_path / "evidence.json"

    harness.write_evidence(
        receipt,
        {"source_sha": "a" * 40, "candidate_wheel_sha256": "b" * 64},
        temp_root=temp_root,
    )

    assert receipt.stat().st_mode & 0o777 == 0o600


def test_failed_rcc_wrapper_exit_keeps_overall_acceptance_failed():
    harness = _harness()
    cells = {
        "authenticated_action": "PASS",
        "sqlite_run": "PASS",
        "artifact_verification": "PASS",
        "wrapper_exit": harness.classify_wrapper_exit(
            {
                "status": "failed",
                "exitCode": -1,
                "reason": "child exited non-zero",
            }
        ),
        "process_cleanup": "PASS",
    }

    assert cells["wrapper_exit"] == "FAIL"
    assert harness.acceptance_status(cells) == "FAIL"


@pytest.mark.parametrize(
    "receipt",
    [
        {},
        {"status": "running", "exitCode": 0},
        {"status": "failed", "exitCode": 0},
        {"status": "completed"},
        {"status": "completed", "exitCode": True},
        {"status": "completed", "exitCode": "0"},
        {"status": "completed", "exitCode": 1},
    ],
)
def test_wrapper_exit_requires_completed_status_and_numeric_zero(receipt):
    assert _harness().classify_wrapper_exit(receipt) == "FAIL"


def test_wrapper_exit_accepts_only_completed_numeric_zero():
    assert (
        _harness().classify_wrapper_exit({"status": "completed", "exitCode": 0})
        == "PASS"
    )


def test_unavailable_provider_probe_counts_and_rejects_attempted_requests():
    harness = _harness()
    probe = harness.UnavailableProviderProbe("127.0.0.1", 0)
    try:
        methods = (
            "GET",
            "HEAD",
            "OPTIONS",
            "PUT",
            "DELETE",
            "CONNECT",
            "TRACE",
            "PATCH",
            "POST",
        )
        for method in methods:
            request = urllib.request.Request(
                probe.url,
                data=b"" if method in ("PUT", "POST", "PATCH") else None,
                method=method,
            )
            with pytest.raises(urllib.error.HTTPError) as error:
                urllib.request.urlopen(request, timeout=2)
            assert error.value.code == 503
        assert probe.request_count == len(methods)
        assert probe.requests == [
            {"method": method, "path": "/", "status": 503} for method in methods
        ]
        assert harness.classify_zero_provider_requests(probe.request_count) == "FAIL"
        assert harness.classify_zero_provider_requests(0) == "PASS"
        assert harness.classify_zero_provider_requests(True) == "FAIL"
    finally:
        probe.close()


def test_acquire_diagnostic_uses_machine_result_and_optional_provider(
    monkeypatch,
):
    harness = _harness()
    digest = "sha256:" + "a" * 64
    commands = []

    def run(command, **_kwargs):
        commands.append(command)
        return subprocess.CompletedProcess(
            command,
            0,
            json.dumps({"artifact": digest, "verification": {"valid": True}}),
            "",
        )

    monkeypatch.setattr(harness, "run_owned_process", run)
    result = harness.run_acquire_diagnostic(
        "/opt/rcc",
        digest,
        provider=None,
        env={},
        deadline=harness.Deadline.after(5),
        replacements={},
    )

    assert result["exit_code"] == 0
    assert result["exact_digest"] is True
    assert result["verification_valid"] is True
    assert "--provider" not in commands[0]
    assert "--permissive-local" in commands[0]


def test_runtime_tree_refresh_precedes_stop_even_when_stop_fails(monkeypatch):
    harness = _harness()
    events = []
    process = type("Process", (), {"pid": 12345})()

    def refresh(tree, pid):
        events.append(("refresh", tree, pid))
        tree.append("new-owned-child")

    class Server:
        def stop(self):
            events.append(("stop",))
            raise RuntimeError("simulated Runtime stop failure")

    monkeypatch.setattr(harness, "refresh_process_tree", refresh)
    tree = []
    with pytest.raises(RuntimeError, match="simulated Runtime stop failure"):
        harness.stop_runtime_server(Server(), process, tree)

    assert events == [("refresh", tree, 12345), ("stop",)]
    assert tree == ["new-owned-child"]


def test_supervisor_process_enumeration_errors_fail_closed(monkeypatch):
    import psutil

    harness = _harness()

    def denied(self, recursive=False):
        raise psutil.AccessDenied(self.pid)

    monkeypatch.setattr(psutil.Process, "children", denied)
    with pytest.raises(harness.ProcessTreeCleanupError, match="unable to enumerate"):
        harness._supervisor_children(psutil)


@pytest.mark.skipif(
    not hasattr(os, "fchmod"),
    reason="Supervisor cleanup receipt permission update requires POSIX fchmod",
)
def test_supervisor_cleanup_failure_demotes_existing_receipt(tmp_path):
    harness = _harness()
    receipt = tmp_path / "receipt.json"
    evidence = {
        "acceptance_status": "PASS",
        "cells": {"authenticated_action": "PASS", "process_cleanup": "PASS"},
    }
    harness.write_evidence(receipt, evidence, temp_root=tmp_path / "temporary")

    harness.record_supervisor_cleanup_failure(
        receipt,
        harness.ProcessTreeCleanupError(
            "unexpected process",
            command_returncode=0,
            cleanup_disposition="killed_unexpected_descendants",
        ),
    )

    updated = json.loads(receipt.read_text(encoding="utf-8"))
    assert updated["acceptance_status"] == "FAIL"
    assert updated["cells"]["process_cleanup"] == "FAIL"
    assert updated["supervisor_cleanup"] == {
        "disposition": "killed_unexpected_descendants",
        "command_returncode": 0,
    }


@pytest.mark.skipif(sys.platform != "linux", reason="Linux process-supervisor contract")
def test_owned_linux_process_accepts_path_arguments(tmp_path):
    harness = _harness()
    result = harness.run_owned_process(
        [Path(sys.executable), "-c", "print('path-argument-ok')"],
        timeout_seconds=3,
        env=harness.child_environment(os.environ, task_root=tmp_path / "runtime"),
    )

    assert result.returncode == 0
    assert result.stdout.strip() == "path-argument-ok"
    assert result.cleanup_disposition == "clean_no_descendants"
    assert result.reaped_descendants == 0


def test_acceptance_fails_closed_before_side_effects_on_non_linux(monkeypatch):
    harness = _harness()
    monkeypatch.setattr(harness.sys, "platform", "darwin")
    monkeypatch.setattr(
        harness,
        "_run",
        lambda _receipt: pytest.fail("unsupported platform reached acceptance"),
    )

    assert harness.main(["--receipt", "/tmp/unused-dakota-receipt.json"]) == 2


def test_hidden_supervisor_fails_closed_on_non_linux(monkeypatch):
    harness = _harness()
    monkeypatch.setattr(harness.sys, "platform", "darwin")
    monkeypatch.setattr(
        harness,
        "_supervisor_main",
        lambda: pytest.fail("unsupported platform reached the process supervisor"),
    )

    assert harness.main(["--_supervisor"]) == 2


@pytest.mark.skipif(sys.platform != "linux", reason="Linux process-group cleanup proof")
def test_total_timeout_terminates_owned_descendant_processes(tmp_path):
    harness = _harness()
    pid_file = tmp_path / "child.pid"
    child_code = (
        "import signal,time; signal.signal(signal.SIGTERM, signal.SIG_IGN); "
        "time.sleep(60)"
    )
    parent_code = (
        "import subprocess,sys,time; from pathlib import Path; "
        f"child=subprocess.Popen([sys.executable,'-c',{child_code!r}],start_new_session=True); "
        f"Path({str(pid_file)!r}).write_text(str(child.pid)); "
        "time.sleep(60)"
    )
    env = harness.child_environment(os.environ, task_root=tmp_path / "process-root")

    with pytest.raises(subprocess.TimeoutExpired):
        harness.run_owned_process(
            [sys.executable, "-c", parent_code],
            timeout_seconds=0.5,
            env=env,
            cleanup_grace_seconds=0.2,
        )

    import psutil

    child_pid = int(pid_file.read_text())
    deadline = time.monotonic() + 3
    while psutil.pid_exists(child_pid) and time.monotonic() < deadline:
        try:
            if psutil.Process(child_pid).status() == psutil.STATUS_ZOMBIE:
                break
        except psutil.NoSuchProcess:
            break
        time.sleep(0.05)
    try:
        child_stopped = (
            not psutil.Process(child_pid).is_running()
            or psutil.Process(child_pid).status() == psutil.STATUS_ZOMBIE
        )
    except psutil.NoSuchProcess:
        child_stopped = True
    assert child_stopped


@pytest.mark.skipif(sys.platform != "linux", reason="Linux subreaper regression")
def test_early_owner_exit_cleans_detached_inherited_pipe_writer(tmp_path):
    harness = _harness()
    pid_file = tmp_path / "detached-writer.pid"
    child_code = (
        "import os,signal,time; from pathlib import Path; "
        "signal.signal(signal.SIGTERM, signal.SIG_IGN); "
        f"Path({str(pid_file)!r}).write_text(str(os.getpid())); "
        "time.sleep(60)"
    )
    owner_code = (
        "import subprocess,sys,time\nfrom pathlib import Path\n"
        f"subprocess.Popen([sys.executable,'-c',{child_code!r}], "
        "start_new_session=True, stdout=sys.stdout, stderr=sys.stderr)\n"
        f"deadline=time.monotonic()+3\n"
        f"while not Path({str(pid_file)!r}).exists() and time.monotonic()<deadline:\n"
        "    time.sleep(0.01)\n"
    )
    env = harness.child_environment(os.environ, task_root=tmp_path / "isolated")
    invoke = (
        "import importlib.util,json,subprocess,sys\n"
        f"spec=importlib.util.spec_from_file_location('dakota_harness',{str(SCRIPT)!r})\n"
        "module=importlib.util.module_from_spec(spec)\n"
        "spec.loader.exec_module(module)\n"
        "try:\n"
        "    module.run_owned_process([sys.executable,'-c',"
        f"{owner_code!r}], timeout_seconds=5, env={env!r}, cleanup_grace_seconds=0.2)\n"
        "except module.ProcessTreeCleanupError as error:\n"
        "    print(json.dumps({'raised': True, 'returncode': error.command_returncode, "
        "'disposition': error.cleanup_disposition}))\n"
        "else:\n"
        "    print(json.dumps({'raised': False}))\n"
    )
    started = time.monotonic()
    outer = subprocess.Popen(
        [sys.executable, "-c", invoke],
        stdin=subprocess.DEVNULL,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        text=True,
        env=env,
    )
    try:
        stdout, stderr = outer.communicate(timeout=8)
        assert outer.returncode == 0, f"{stdout}\n{stderr}"
        assert time.monotonic() - started < 5
        import psutil

        result = json.loads(stdout)
        assert result == {
            "raised": True,
            "returncode": 0,
            "disposition": "killed_unexpected_descendants",
        }
        child_pid = int(pid_file.read_text())
        with pytest.raises(psutil.NoSuchProcess):
            psutil.Process(child_pid).status()
    finally:
        if outer.poll() is None:
            outer.kill()
            outer.communicate(timeout=2)
        if pid_file.exists():
            try:
                os.kill(int(pid_file.read_text()), signal.SIGKILL)
            except (ProcessLookupError, ValueError):
                pass


@pytest.mark.skipif(sys.platform != "linux", reason="Linux subreaper regression")
def test_success_reaps_fast_exit_detached_child_before_echild(tmp_path):
    harness = _harness()
    pid_file = tmp_path / "fast-exit.pid"
    child_code = (
        "import os; from pathlib import Path; "
        f"Path({str(pid_file)!r}).write_text(str(os.getpid()))"
    )
    owner_code = (
        "import subprocess,sys,time; from pathlib import Path; "
        f"subprocess.Popen([sys.executable,'-c',{child_code!r}], "
        "start_new_session=True, stdout=sys.stdout, stderr=sys.stderr); "
        f"pid_file=Path({str(pid_file)!r}); deadline=time.monotonic()+3; "
        "pid=None\n"
        "while pid is None and time.monotonic()<deadline:\n"
        "    try: pid=int(pid_file.read_text())\n"
        "    except (FileNotFoundError, ValueError): time.sleep(0.01)\n"
        "while pid is not None and Path(f'/proc/{pid}/stat').exists():\n"
        "    state=Path(f'/proc/{pid}/stat').read_text().split()[2]\n"
        "    if state == 'Z': break\n"
        "    time.sleep(0.01)\n"
    )
    env = harness.child_environment(os.environ, task_root=tmp_path / "isolated")
    started = time.monotonic()
    try:
        result = harness.run_owned_process(
            [sys.executable, "-c", owner_code],
            timeout_seconds=3,
            env=env,
            cleanup_grace_seconds=0.5,
        )
        assert time.monotonic() - started < 3
        assert result.returncode == 0
        assert result.cleanup_disposition == "reaped_exited_descendants"
        assert result.reaped_descendants >= 1
        import psutil

        child_pid = int(pid_file.read_text())
        with pytest.raises(psutil.NoSuchProcess):
            psutil.Process(child_pid).status()
    finally:
        if pid_file.exists():
            try:
                os.kill(int(pid_file.read_text()), signal.SIGKILL)
            except (ProcessLookupError, ValueError):
                pass


def test_provider_startup_failure_uses_bounded_kill_fallback(tmp_path, monkeypatch):
    harness = _harness()
    pid_file = tmp_path / "provider.pid"
    fake_server = (
        "import os,signal,time; from pathlib import Path; "
        "signal.signal(signal.SIGTERM, signal.SIG_IGN); "
        f"Path({str(pid_file)!r}).write_text(str(os.getpid())); "
        "print('not-json', flush=True); time.sleep(60)"
    )
    monkeypatch.setattr(
        harness,
        "_provider_command",
        lambda _rcc, _root: [sys.executable, "-c", fake_server],
    )

    with pytest.raises(RuntimeError, match="startup JSON"):
        harness.start_provider(
            "/unused/rcc",
            tmp_path / "provider-root",
            harness.child_environment(os.environ, task_root=tmp_path / "runtime"),
            deadline=harness.Deadline.after(3),
            cleanup_grace_seconds=0.2,
        )

    import psutil

    provider_pid = int(pid_file.read_text())
    try:
        provider_stopped = (
            not psutil.Process(provider_pid).is_running()
            or psutil.Process(provider_pid).status() == psutil.STATUS_ZOMBIE
        )
    except psutil.NoSuchProcess:
        provider_stopped = True
    assert provider_stopped
