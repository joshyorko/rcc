"""Current-candidate last-good behavior across a failed source reload."""

from __future__ import annotations

import hashlib
import importlib.util
import json
import os
import sqlite3
import subprocess
import sys
import time
import uuid
from pathlib import Path

import pytest


def _wait_for(label, read, timeout: float = 90.0):
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        value = read()
        if value:
            return value
        time.sleep(0.05)
    raise AssertionError(f"timed out waiting for {label}")


def _read_run(database: Path, run_id: str):
    with sqlite3.connect(database, timeout=3) as connection:
        row = connection.execute(
            "SELECT id, status, result, error_message FROM run WHERE id = ?",
            (run_id,),
        ).fetchone()
    if row is None:
        return None
    return {
        "id": row[0],
        "status": row[1],
        "result": json.loads(row[2]) if row[2] else None,
        "error_message": row[3],
    }


def _read_package_source(database: Path, package_name: str):
    with sqlite3.connect(database, timeout=3) as connection:
        row = connection.execute(
            "SELECT directory FROM action_package WHERE name = ?", (package_name,)
        ).fetchone()
    return row[0] if row else None


def _source_evidence() -> dict[str, object]:
    root = Path(__file__).resolve().parents[3]
    import actions.server._action_package_handler as package_handler_module
    import actions.server._actions_import as actions_import_module
    import actions.server._rcc_runtime_adapter as runtime_adapter_module

    def git(*args: str) -> str:
        return subprocess.check_output(
            ["git", "-C", str(root), *args], text=True
        ).strip()

    changed_paths = git("diff", "--name-only", "HEAD^", "HEAD").splitlines()
    runtime_modules = {
        "action_package_handler": Path(package_handler_module.__file__).resolve(),
        "actions_import": Path(actions_import_module.__file__).resolve(),
        "runtime_adapter": Path(runtime_adapter_module.__file__).resolve(),
    }
    return {
        "commit": git("rev-parse", "HEAD"),
        "tree": git("rev-parse", "HEAD^{tree}"),
        "dirty_paths": [
            line[3:]
            for line in git(
                "status", "--porcelain", "--untracked-files=no"
            ).splitlines()
        ],
        "parent_delta_file_sha256": {
            path: hashlib.sha256((root / path).read_bytes()).hexdigest()
            for path in changed_paths
            if (root / path).is_file()
        },
        "module_origins": {name: str(path) for name, path in runtime_modules.items()},
        "runtime_module_sha256": {
            name: hashlib.sha256(path.read_bytes()).hexdigest()
            for name, path in runtime_modules.items()
        },
    }


def _action_server_exit_failure(
    returncode_before_forced_cleanup: int | None,
    shutdown_http_status: int | None,
) -> str | None:
    shutdown_succeeded = (
        shutdown_http_status is not None and 200 <= shutdown_http_status < 300
    )
    if not shutdown_succeeded:
        return "controlled shutdown request did not succeed"
    if returncode_before_forced_cleanup is None:
        return (
            "Action Server natural return code was not observed before forced cleanup"
        )
    if returncode_before_forced_cleanup not in (0, 1):
        return (
            "Action Server exited abnormally before forced cleanup with return code "
            f"{returncode_before_forced_cleanup}"
        )
    return None


def _action_server_exit_receipt(
    returncode_before_forced_cleanup: int | None,
    shutdown_http_status: int | None,
    returncode_after_forced_cleanup: int | None,
) -> dict[str, int | str | bool | None]:
    shutdown_succeeded = (
        shutdown_http_status is not None and 200 <= shutdown_http_status < 300
    )
    natural_exit_failure = _action_server_exit_failure(
        returncode_before_forced_cleanup, shutdown_http_status
    )

    return {
        "shutdown_http_status": shutdown_http_status,
        "shutdown_request_succeeded": shutdown_succeeded,
        "returncode_before_forced_cleanup": returncode_before_forced_cleanup,
        "natural_exit_status": "PASS" if natural_exit_failure is None else "FAIL",
        "natural_exit_failure": natural_exit_failure,
        "returncode_after_forced_cleanup": returncode_after_forced_cleanup,
        "returncode_after_forced_cleanup_is_natural_evidence": False,
    }


def test_forced_cleanup_poll_cannot_prove_natural_server_exit() -> None:
    receipt = _action_server_exit_receipt(None, 200, 0)

    assert receipt["natural_exit_status"] == "FAIL"
    assert receipt["returncode_before_forced_cleanup"] is None
    assert receipt["returncode_after_forced_cleanup"] == 0
    assert receipt["returncode_after_forced_cleanup_is_natural_evidence"] is False


@pytest.mark.parametrize(
    ("returncode", "shutdown_status", "expected_status"),
    [
        (None, 200, "FAIL"),
        (1, None, "FAIL"),
        (1, 500, "FAIL"),
        (1, 200, "PASS"),
        (0, 200, "PASS"),
        (-11, 200, "FAIL"),
    ],
)
def test_action_server_natural_exit_receipt_contract(
    returncode: int | None, shutdown_status: int | None, expected_status: str
) -> None:
    receipt = _action_server_exit_receipt(returncode, shutdown_status, 0)

    assert receipt["natural_exit_status"] == expected_status
    assert receipt["returncode_after_forced_cleanup_is_natural_evidence"] is False


@pytest.mark.real_rcc
@pytest.mark.integration_test
def test_current_candidate_failed_reload_keeps_last_good_action_usable(
    action_server_process, client, tmp_path: Path
):
    """Reproduce failed-import recovery against this checkout's actual source."""
    if os.environ.get("ACTIONS_REAL_RCC_ARTIFACT_TEST") != "1":
        pytest.skip(
            "set ACTIONS_REAL_RCC_ARTIFACT_TEST=1 for current-candidate RCC proof"
        )

    real_rcc = Path(os.environ["ACTIONS_RUNTIME_RCC_BINARY"]).resolve()
    real_rcc_sha = hashlib.sha256(real_rcc.read_bytes()).hexdigest()
    assert (
        real_rcc_sha
        == "7e588c01751ca2ae15ba13ef67f2f4b7567697a5a8389737059a73936f509428"
    )
    rcc_version = subprocess.run(
        [str(real_rcc), "--version"], capture_output=True, text=True, check=True
    ).stdout.strip()
    assert rcc_version == "v18.19.3"

    trace_path = tmp_path / "rcc-argv.jsonl"
    rcc_wrapper = tmp_path / "rcc-trace"
    rcc_wrapper.write_text(
        "#!"
        + sys.executable
        + "\n"
        + "import json, os, sys\n"
        + f"with open({str(trace_path)!r}, 'a', encoding='utf-8') as stream:\n"
        + "    stream.write(json.dumps({'pid': os.getpid(), 'args': sys.argv[1:]}) + '\\n')\n"
        + f"os.execv({str(real_rcc)!r}, [{str(real_rcc)!r}, *sys.argv[1:]])\n",
        encoding="utf-8",
    )
    rcc_wrapper.chmod(0o700)

    package_dir = tmp_path / "package"
    package_dir.mkdir()
    (package_dir / "package.yaml").write_text(
        "version: 0.1\nspec-version: v2\ndependencies:\n"
        "  conda-forge:\n    - python=3.12.15\n"
        "  pypi:\n    - actions-core=1.0.2\n",
        encoding="utf-8",
    )
    action_file = package_dir / "action.py"
    started = tmp_path / "last-good-entered"
    release = tmp_path / "release-last-good"
    action_file.write_text(
        "from pathlib import Path\n"
        "import time\n"
        "from actions import action\n\n"
        "@action\n"
        "def generation_probe() -> str:\n"
        f"    Path({str(started)!r}).write_text('entered', encoding='utf-8')\n"
        "    deadline = time.monotonic() + 60\n"
        f"    while not Path({str(release)!r}).exists():\n"
        "        if time.monotonic() >= deadline:\n"
        "            raise TimeoutError('release barrier timed out')\n"
        "        time.sleep(0.02)\n"
        "    return 'last-good'\n",
        encoding="utf-8",
    )

    runtime_env = os.environ.copy()
    runtime_env.update(
        {
            "ACTIONS_RUNTIME_RCC_BINARY": str(rcc_wrapper),
            "ACTIONS_REAL_RCC_ARTIFACT_TEST": "1",
            "ROBOCORP_HOME": str(tmp_path / "rcc-home"),
            "TMPDIR": str(tmp_path / "tmp"),
            "NO_PROXY": "127.0.0.1,localhost",
            "no_proxy": "127.0.0.1,localhost",
        }
    )
    Path(runtime_env["TMPDIR"]).mkdir()
    database = action_server_process.datadir / "server.db"
    run_ids: list[str] = []
    evidence: dict[str, object] = {
        "source": _source_evidence(),
        "rcc_version": rcc_version,
        "rcc_sha256": real_rcc_sha,
        "status": "NOT_RUN",
        "run_ids": run_ids,
    }
    helper_path = (
        Path(__file__).resolve().parents[3]
        / "action_server/scripts/verify_dakota_rcc_acceptance.py"
    )
    helper_spec = importlib.util.spec_from_file_location(
        "rcc_acceptance_provider", helper_path
    )
    assert helper_spec is not None and helper_spec.loader is not None
    provider_harness = importlib.util.module_from_spec(helper_spec)
    helper_spec.loader.exec_module(provider_harness)
    provider_environment = os.environ.copy()
    provider_environment.update(
        {
            "ROBOCORP_HOME": str(tmp_path / "provider-rcc-home"),
            "TMPDIR": str(tmp_path / "provider-tmp"),
        }
    )
    Path(provider_environment["TMPDIR"]).mkdir()
    provider = None
    started_server = False
    shutdown_http_status = None
    natural_returncode_before_forced_cleanup = None
    forced_stop_used = False
    server_cleanup_failure = None
    primary_failure = False
    cleanup_failures: list[str] = []
    try:
        provider, provider_url = provider_harness.start_provider(
            str(real_rcc),
            tmp_path / "selected-provider",
            provider_environment,
            deadline=provider_harness.Deadline.after(900),
        )
        runtime_env["ACTIONS_RUNTIME_RCC_PROVIDER"] = provider_url
        evidence["provider"] = provider_url
        action_server_process.start(
            timeout=900,
            actions_sync=True,
            cwd=package_dir,
            db_file="server.db",
            min_processes=0,
            max_processes=1,
            reuse_processes=True,
            additional_args=["--auto-reload"],
            env=runtime_env,
            port=0,
            verbose="",
            add_shutdown_api=True,
        )
        started_server = True
        runtime_row = _wait_for(
            "initial RCC Runtime descriptor",
            lambda: _read_package_runtime(database, package_dir.name),
        )
        evidence["initial_runtime"] = runtime_row
        initial_source = Path(_read_package_source(database, package_dir.name))
        source_store = action_server_process.datadir / ".rcc-runtime-sources"
        package_store = initial_source.parent
        assert initial_source.is_absolute() and initial_source.is_relative_to(
            source_store
        )
        assert "return 'last-good'" in (initial_source / "action.py").read_text(
            encoding="utf-8"
        )

        def submit(label: str):
            response = client.post_get_response(
                "api/actions/package/generation-probe/run",
                {},
                {
                    "x-actions-async-timeout": "0",
                    "x-actions-request-id": f"rollback-{label}-{uuid.uuid4().hex}",
                },
            )
            run_id = response.headers.get("x-action-server-run-id")
            assert run_id, response.headers
            run_ids.append(run_id)
            return run_id

        first_run_id = submit("before")
        _wait_for("first Action entered", started.is_file)
        _wait_for(
            "first Run running",
            lambda: (
                row
                if (row := _read_run(database, first_run_id)) and row["status"] == 1
                else None
            ),
        )
        before_ops = _rcc_environment_operations(trace_path)
        evidence["provider_ops_before_failure"] = before_ops

        invalid_source = tmp_path / "action.py.invalid"
        invalid_source.write_text(
            "from actions import action\n\n@action\ndef generation_probe(:\n    return 'broken'\n",
            encoding="utf-8",
        )
        invalid_source.replace(action_file)
        server_log = action_server_process.datadir / "server_log.txt"

        def import_failure_seen():
            output = (
                action_server_process.get_stdout() + action_server_process.get_stderr()
            )
            if server_log.is_file():
                output += server_log.read_text(encoding="utf-8", errors="replace")
            return "It was not possible to list the actions." in output

        _wait_for("failed source import", import_failure_seen)
        descriptor_after_failure = _read_package_runtime(database, package_dir.name)
        evidence["descriptor_preserved_after_failure"] = (
            descriptor_after_failure == runtime_row
        )
        assert Path(_read_package_source(database, package_dir.name)) == initial_source
        assert [path for path in package_store.iterdir() if path.is_dir()] == [
            initial_source
        ]
        release.write_text("release", encoding="utf-8")
        first_run = _wait_for(
            "first last-good Run passed",
            lambda: (
                row
                if (row := _read_run(database, first_run_id))
                and row["status"] in (2, 3)
                else None
            ),
        )
        evidence["first_run"] = first_run

        second_run_id = submit("after-failure")
        second_run = _wait_for(
            "second persisted Run terminal",
            lambda: (
                row
                if (row := _read_run(database, second_run_id))
                and row["status"] in (2, 3)
                else None
            ),
        )
        evidence["second_run"] = second_run
        evidence["provider_ops_after_failure"] = _rcc_environment_operations(trace_path)
        evidence["final_runtime"] = _read_package_runtime(database, package_dir.name)
        assert first_run["status"] == 2 and first_run["result"] == "last-good"
        assert second_run["status"] == 2 and second_run["result"] == "last-good"
        assert evidence["descriptor_preserved_after_failure"]
        assert evidence["provider_ops_after_failure"] == before_ops

        recovered_source = tmp_path / "action.py.recovered"
        recovered_source.write_text(
            "from actions import action\n\n"
            "@action\n"
            "def generation_probe() -> str:\n"
            "    return 'recovered'\n",
            encoding="utf-8",
        )
        recovered_source.replace(action_file)
        recovered_runtime = _wait_for(
            "watcher to import repaired source",
            lambda: (
                runtime
                if (runtime := _read_package_runtime(database, package_dir.name))
                and runtime["source_generation"] != runtime_row["source_generation"]
                else None
            ),
        )
        recovered_run_id = submit("after-repair")
        recovered_run = _wait_for(
            "repaired-source Run passed",
            lambda: (
                row
                if (row := _read_run(database, recovered_run_id))
                and row["status"] in (2, 3)
                else None
            ),
        )
        evidence["recovered_runtime"] = recovered_runtime
        evidence["recovered_run"] = recovered_run
        evidence["provider_ops_after_recovery"] = _rcc_environment_operations(
            trace_path
        )
        evidence["status"] = (
            "PASS"
            if (
                recovered_run["status"] == 2
                and recovered_run["result"] == "recovered"
                and evidence["provider_ops_after_recovery"] == before_ops
            )
            else "FAIL"
        )
        assert (
            evidence["status"] == "PASS"
        ), f"watcher did not recover after the failed reload: {recovered_run}"
        import requests

        shutdown_response = requests.post(
            client.build_full_url("api/shutdown/"),
            params={"timeout": 5},
            timeout=10,
        )
        shutdown_http_status = shutdown_response.status_code
        evidence["shutdown_http_status"] = shutdown_http_status
        assert 200 <= shutdown_http_status < 300, (
            "controlled Action Server shutdown failed with HTTP "
            f"{shutdown_http_status}"
        )
        owned_server_process = action_server_process.process
        natural_exit_deadline = time.monotonic() + 10
        while (
            owned_server_process.returncode is None
            and time.monotonic() < natural_exit_deadline
        ):
            time.sleep(0.05)
        natural_returncode_before_forced_cleanup = owned_server_process.returncode
    except Exception as exc:
        primary_failure = True
        evidence["status"] = "FAIL"
        evidence["failure_type"] = type(exc).__name__
        evidence["failure"] = str(exc)[:1000]
        raise
    finally:
        if started_server:
            owned_server_process = action_server_process.process
            server_pid = owned_server_process.pid
            if natural_returncode_before_forced_cleanup is None:
                natural_returncode_before_forced_cleanup = (
                    owned_server_process.returncode
                )
            try:
                import psutil

                server_tree_before_stop: list[dict[str, int | float]] = [
                    {"pid": child.pid, "create_time": child.create_time()}
                    for child in psutil.Process(server_pid).children(recursive=True)
                ]
            except psutil.Error:
                server_tree_before_stop = []
            stop_failure = None
            shutdown_succeeded = (
                shutdown_http_status is not None and 200 <= shutdown_http_status < 300
            )
            if (
                not shutdown_succeeded
                or natural_returncode_before_forced_cleanup is None
            ):
                forced_stop_used = True
                try:
                    action_server_process.stop()
                except Exception as exc:
                    stop_failure = (
                        f"Action Server forced-stop fallback failed: "
                        f"{type(exc).__name__}: {exc}"
                    )
                exit_deadline = time.monotonic() + 10
                while (
                    owned_server_process.returncode is None
                    and time.monotonic() < exit_deadline
                ):
                    time.sleep(0.05)
            returncode_after_forced_cleanup = owned_server_process.returncode
            remaining_descendants = []
            for descendant in server_tree_before_stop:
                try:
                    process = psutil.Process(int(descendant["pid"]))
                    if process.create_time() == descendant["create_time"]:
                        remaining_descendants.append(descendant)
                except psutil.Error:
                    pass
            evidence["action_server_process_exit"] = {
                "pid": server_pid,
                **_action_server_exit_receipt(
                    natural_returncode_before_forced_cleanup,
                    shutdown_http_status,
                    returncode_after_forced_cleanup,
                ),
                "forced_stop_used": forced_stop_used,
                "forced_cleanup_returncode_observed": returncode_after_forced_cleanup
                is not None,
                "owned_descendants_before_stop": server_tree_before_stop,
                "same_owned_descendants_remaining_after_stop": remaining_descendants,
                "descendant_observation_scope": "captured tree only; not a complete descendant-reaping claim",
                "argv": owned_server_process._args,
                "interpreter": sys.executable,
                "pythonpath": runtime_env.get("PYTHONPATH"),
            }
            server_cleanup_failure = _action_server_exit_failure(
                natural_returncode_before_forced_cleanup, shutdown_http_status
            )
            if (
                server_cleanup_failure is None
                and returncode_after_forced_cleanup is None
            ):
                server_cleanup_failure = (
                    "Action Server forced cleanup return code was not observed "
                    "within the bounded wait"
                )
            if server_cleanup_failure:
                evidence["status"] = "FAIL"
                evidence["cleanup_failure"] = server_cleanup_failure
                cleanup_failures.append(server_cleanup_failure)
            if stop_failure:
                evidence["status"] = "FAIL"
                evidence["action_server_stop_failure"] = stop_failure
                cleanup_failures.append(stop_failure)
        if provider is not None:
            try:
                provider_harness.terminate_process_tree(provider)
            except Exception as exc:
                cleanup_failures.append(
                    f"selected RCC provider cleanup failed: {type(exc).__name__}: {exc}"
                )
            if provider.poll() is None:
                cleanup_failures.append("selected RCC provider was not reaped")
        if cleanup_failures:
            evidence["cleanup_failures"] = cleanup_failures
            evidence["status"] = "FAIL"
        evidence["rcc_trace"] = _summarize_rcc_trace(trace_path)
        receipt_path = os.environ.get("ACTIONS_RUNTIME_LIFECYCLE_RECEIPT")
        if receipt_path:
            path = Path(receipt_path)
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text(json.dumps(evidence, indent=2, sort_keys=True) + "\n")
        if cleanup_failures and not primary_failure:
            raise AssertionError("; ".join(cleanup_failures))


def _read_package_runtime(database: Path, package_name: str):
    with sqlite3.connect(database, timeout=3) as connection:
        row = connection.execute(
            "SELECT env_json FROM action_package WHERE name = ?", (package_name,)
        ).fetchone()
    return json.loads(row[0]).get("runtime") if row else None


def _read_rcc_trace(path: Path):
    if not path.is_file():
        return []
    return [json.loads(line) for line in path.read_text().splitlines() if line]


def _rcc_environment_operations(path: Path):
    return [
        _summarize_rcc_record(record)
        for record in _read_rcc_trace(path)
        if len(record["args"]) > 1
        and record["args"][0] == "env"
        and record["args"][1] in {"publish", "acquire", "build"}
    ]


def _summarize_rcc_record(record):
    args = record["args"]
    summary = {"pid": record["pid"], "phase": args[1] if len(args) > 1 else None}
    for flag, key in (
        ("--provider", "provider"),
        ("--artifact", "artifact"),
    ):
        if flag in args and args.index(flag) + 1 < len(args):
            summary[key] = args[args.index(flag) + 1]
    return summary


def _summarize_rcc_trace(path: Path):
    return [
        {"pid": record["pid"], "phase": record["args"][1:2]}
        for record in _read_rcc_trace(path)
    ]
