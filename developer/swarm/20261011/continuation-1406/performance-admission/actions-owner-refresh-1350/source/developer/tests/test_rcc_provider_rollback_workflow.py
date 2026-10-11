"""Keep the opt-in source RCC acceptance workflow narrowly scoped."""

from __future__ import annotations

import importlib.util
import os
import shlex
import subprocess
import sys
from pathlib import Path

import pytest
import yaml

ROOT = Path(__file__).resolve().parents[2]
WORKFLOW_RELATIVE_PATH = ".github/workflows/actions_runtime_rcc_provider_rollback.yml"
WORKFLOW_NAME = "actions_runtime_rcc_provider_rollback.yml"
RCC_SHA256 = "7e588c01751ca2ae15ba13ef67f2f4b7567697a5a8389737059a73936f509428"
CANDIDATE_SHA = "c10380a4d2ebc6f0aca63eb85c8f84a337da4962"
CANDIDATE_TREE = "21676c626a933c2d55761ec85ecf26fe0eef26bb"
TEST_NODE = (
    "tests/action_server_tests/test_current_candidate_import_rollback.py::"
    "test_current_candidate_failed_reload_keeps_last_good_action_usable"
)
STAGED_CONSUMER_NODE = (
    "tests/action_server_tests/test_source_staging_rcc_consumer.py::"
    "test_staged_package_executes_in_managed_rcc_runtime"
)
PUBLISHED_DETAILS_NODE = (
    "tests/action_server_tests/test_source_staging_rcc_consumer.py::"
    "test_published_artifact_details_preserves_one_rcc_identity_pair"
)
RCC_VERSION = "v18.19.3"


def _synthetic_version_writer(tmp_path: Path) -> tuple[Path, Path]:
    marker = tmp_path / "writer-completed"
    writer = tmp_path / "synthetic-rcc-version.py"
    version_line = f"{RCC_VERSION}\n".encode("utf-8")
    writer.write_text(
        "import os, signal, sys\n"
        "from pathlib import Path\n"
        "signal.signal(signal.SIGPIPE, signal.SIG_DFL)\n"
        f"os.write(1, {version_line!r})\n"
        "os.write(1, b'x' * 1048576)\n"
        f"Path({str(marker)!r}).write_text('complete', encoding='utf-8')\n"
        "sys.exit(int(os.environ.get('SYNTHETIC_RCC_EXIT_CODE', '0')))\n",
        encoding="utf-8",
    )
    return writer, marker


def _run_bash(
    command: str, env: dict[str, str] | None = None
) -> subprocess.CompletedProcess:
    return subprocess.run(
        ["bash", "-c", command],
        check=False,
        capture_output=True,
        env=env,
    )


def test_generated_workflow_is_checkout_equivalent(tmp_path: Path) -> None:
    generator_path = ROOT / ".github/workflows/_gen_workflows.py"
    spec = importlib.util.spec_from_file_location("workflow_generator", generator_path)
    assert spec is not None and spec.loader is not None
    generator = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(generator)
    target = next(
        workflow for workflow in generator.TARGETS if workflow.target == WORKFLOW_NAME
    )
    original_workflow_dir = generator.CURDIR
    generator.CURDIR = tmp_path
    try:
        target.generate()
    finally:
        generator.CURDIR = original_workflow_dir

    generated = tmp_path / WORKFLOW_NAME
    checked_in = ROOT / WORKFLOW_RELATIVE_PATH
    assert generated.read_bytes() == checked_in.read_bytes()


def test_rcc_rollback_workflow_is_opt_in_pinned_and_secret_free() -> None:
    path = ROOT / WORKFLOW_RELATIVE_PATH
    workflow = yaml.load(path.read_text(encoding="utf-8"), Loader=yaml.BaseLoader)

    assert workflow["permissions"] == {"contents": "read"}
    assert set(workflow["on"]) == {"push"}
    assert workflow["on"]["push"]["branches"] == [
        "test/rcc-provider-rollback-hosted-20261010",
        "fix/rcc-published-artifact-details-20261010",
        "fix/rcc-candidate-wheel-identities-20261010",
    ]
    assert workflow["on"]["push"]["paths"] == [
        "developer/tests/test_rcc_provider_rollback_workflow.py",
        "developer/tests/test_rcc_provider_rollback_summary.py",
        "action_server/tests/action_server_tests/test_source_staging_rcc_consumer.py",
        "action_server/src/actions/server/deployments/source_staging.py",
        "action_server/src/actions/server/deployments/source_read.py",
        "action_server/src/actions/server/deployments/source_manifest.py",
        ".github/workflows/_gen_workflows.py",
        WORKFLOW_RELATIVE_PATH,
    ]

    job = workflow["jobs"]["build"]
    assert job["runs-on"] == "ubuntu-24.04"
    assert job["timeout-minutes"] == "30"
    steps = job["steps"]
    control_checkout = next(
        step for step in steps if step["name"] == "Checkout workflow control revision"
    )
    candidate_checkout = next(
        step for step in steps if step["name"] == "Checkout immutable Runtime candidate"
    )
    assert control_checkout["with"] == {
        "ref": "${{ github.sha }}",
        "path": "control",
        "fetch-depth": "1",
        "persist-credentials": "false",
    }
    assert candidate_checkout["with"] == {
        "ref": CANDIDATE_SHA,
        "path": "candidate",
        "fetch-depth": "2",
        "persist-credentials": "false",
    }
    verify = next(
        step
        for step in steps
        if step["name"] == "Verify immutable Runtime candidate revision"
    )
    assert verify["env"] == {
        "CANDIDATE_SHA": CANDIDATE_SHA,
        "CANDIDATE_TREE": CANDIDATE_TREE,
        "CONTROL_SHA": "${{ github.sha }}",
    }
    assert 'test "$actual" = "$CANDIDATE_SHA"' in verify["run"]
    assert 'test "$actual_tree" = "$CANDIDATE_TREE"' in verify["run"]
    assert 'test "$control" = "$CONTROL_SHA"' in verify["run"]
    assert (
        'control_tree=$(git -C "$GITHUB_WORKSPACE/control" rev-parse HEAD^{tree})'
        in verify["run"]
    )
    assert "RCC_ROLLBACK_CONTROL_TREE" in verify["run"]
    assert (
        workflow["defaults"]["run"]["working-directory"] == "./candidate/action_server"
    )

    rcc_install = next(
        step
        for step in steps
        if step["name"] == "Install and verify pinned RCC v18.19.3"
    )
    assert rcc_install["env"]["RCC_SHA256"] == RCC_SHA256
    assert "rcc-linux64" in rcc_install["run"]
    assert "sha256sum --check" in rcc_install["run"]
    assert 'version_output="$("$rcc" --version 2>&1)"' in rcc_install["run"]
    assert (
        'grep --fixed-strings --line-regexp "v18.19.3" <<< "$version_output"'
        in rcc_install["run"]
    )
    assert "--quiet" not in rcc_install["run"]
    assert '"$rcc" --version 2>&1 | grep' not in rcc_install["run"]

    libc_check = next(
        step
        for step in steps
        if step["name"] == "Verify runner libc supports the test artifact"
    )
    assert "platform.libc_ver()" in libc_check["run"]
    assert "actual < (2, 36)" in libc_check["run"]
    assert steps.index(libc_check) < steps.index(rcc_install)

    test = next(
        step
        for step in steps
        if step["name"] == "Run current-candidate RCC provider rollback acceptance"
    )
    assert test["env"]["ACTIONS_REAL_RCC_ARTIFACT_TEST"] == "1"
    assert "ACTIONS_RUNTIME_RCC_BINARY" in test["run"]
    assert (
        test["env"]["PYTHONPATH"]
        == "${{ github.workspace }}/candidate/action_server/src:${{ github.workspace }}/candidate/actions/src"
    )
    assert "-n 0" in test["run"]
    assert TEST_NODE in test["run"]
    assert STAGED_CONSUMER_NODE in test["run"]
    assert PUBLISHED_DETAILS_NODE in test["run"]
    assert test["env"]["ACTIONS_RUNTIME_STAGED_CONSUMER_RECEIPT"].endswith(
        "/staged-consumer-receipt.json"
    )
    assert test["env"]["ACTIONS_RUNTIME_PUBLISHED_DETAILS_RECEIPT"].endswith(
        "/published-details-receipt.json"
    )
    assert (
        test["env"]["ACTIONS_HOME"] == "${{ runner.temp }}/rcc-provider-rollback-home"
    )
    assert test["env"]["ROBOCORP_HOME"] == test["env"]["ACTIONS_HOME"]
    assert "pytest" in test["run"] and "inv test" not in test["run"]

    summary = next(
        step
        for step in steps
        if step["name"] == "Write sanitized source and test evidence"
    )
    upload = next(
        step for step in steps if step["name"] == "Upload sanitized acceptance evidence"
    )
    assert summary["if"] == "always()"
    assert summary["working-directory"] == "${{ github.workspace }}"
    admission = next(
        step for step in steps if step["name"] == "Validate RCC rollback admission"
    )
    assert admission["if"] == "always()"
    assert admission["working-directory"] == "${{ github.workspace }}"
    assert admission["env"]["EXPECTED_CANDIDATE_SHA"] == CANDIDATE_SHA
    assert admission["env"]["EXPECTED_CANDIDATE_TREE"] == CANDIDATE_TREE
    assert "receipt_source_commit_mismatch" in admission["run"]
    assert "runner_libc" in admission["run"]
    assert "test_result_not_exactly_three_passes" in admission["run"]
    assert "staged_consumer_receipt_missing_or_invalid" in admission["run"]
    assert "published_details_receipt_missing_or_invalid" in admission["run"]
    assert "raw_json_sha256" in admission["run"]
    assert "sys.exit(0 if not issues else 1)" in admission["run"]
    assert upload["if"] == "always()"
    assert "acceptance-summary.*" in upload["with"]["path"]
    assert "staged-consumer-receipt.json" in upload["with"]["path"]
    assert "published-details-receipt.json" in upload["with"]["path"]
    assert "lifecycle-summary.json" in upload["with"]["path"]
    assert "lifecycle-receipt.json" in upload["with"]["path"]
    assert "rcc-provider-rollback-junit.xml" in upload["with"]["path"]
    assert "rcc-provider-rollback" in upload["with"]["name"]
    assert "workflow_control_sha" in summary["run"]
    assert "candidate_sha" in summary["run"]
    assert "secrets." not in path.read_text(encoding="utf-8")


def test_devinstall_uses_preverified_rcc_binary() -> None:
    path = ROOT / WORKFLOW_RELATIVE_PATH
    workflow = yaml.load(path.read_text(encoding="utf-8"), Loader=yaml.BaseLoader)
    steps = workflow["jobs"]["build"]["steps"]
    names = [step["name"] for step in steps]
    verify = names.index("Install and verify pinned RCC v18.19.3")
    preseed = names.index("Preseed and verify Action Server RCC")
    devinstall = next(
        step
        for step in steps
        if step["name"] == "Install Action Server developer environment"
    )
    assert verify < preseed < names.index(devinstall["name"])
    assert "rcc-18.19.3" in steps[preseed]["run"]
    assert "sha256sum --check" in steps[preseed]["run"]
    assert devinstall["env"] == {"ACTION_SERVER_SKIP_DOWNLOAD_IN_BUILD": "1"}


@pytest.mark.skipif(os.name == "nt", reason="requires POSIX Bash SIGPIPE status")
def test_quiet_grep_pipeline_can_terminate_version_writer_with_sigpipe(
    tmp_path: Path,
) -> None:
    writer, completed = _synthetic_version_writer(tmp_path)
    command = (
        "set -Eeuo pipefail\n"
        f"{shlex.quote(sys.executable)} {shlex.quote(str(writer))} | "
        "grep --fixed-strings --line-regexp --quiet "
        f"{shlex.quote(RCC_VERSION)}\n"
    )

    result = _run_bash(command)

    assert result.returncode == 141, result.stderr.decode(errors="replace")
    assert not completed.exists(), "the early-exit consumer terminated the writer"


@pytest.mark.skipif(os.name == "nt", reason="requires POSIX Bash command substitution")
def test_capture_then_match_waits_for_version_process_and_preserves_failure(
    tmp_path: Path,
) -> None:
    writer, completed = _synthetic_version_writer(tmp_path)
    quoted_writer = f"{shlex.quote(sys.executable)} {shlex.quote(str(writer))}"
    safe_check = (
        "set -Eeuo pipefail\n"
        f'version_output="$({quoted_writer})"\n'
        f"grep --fixed-strings --line-regexp {shlex.quote(RCC_VERSION)} "
        '<<< "$version_output"\n'
    )

    success = _run_bash(safe_check)

    assert success.returncode == 0, success.stderr.decode(errors="replace")
    assert success.stdout.decode().strip() == RCC_VERSION
    assert completed.read_text(encoding="utf-8") == "complete"

    rejected_marker = tmp_path / "nonzero-writer-rejected"
    nonzero_env = os.environ.copy()
    nonzero_env["SYNTHETIC_RCC_EXIT_CODE"] = "23"
    nonzero = _run_bash(
        safe_check + f"touch {shlex.quote(str(rejected_marker))}\n", env=nonzero_env
    )

    assert nonzero.returncode == 23
    assert not rejected_marker.exists(), "set -e must reject failed version commands"
