"""External, non-source timing probe for the immutable Actions RCC fixture."""

from __future__ import annotations

import json
import hashlib
import itertools
import os
from pathlib import Path
import resource
import threading
import time
from typing import Any


_events_lock = threading.Lock()
_runner_sequence = itertools.count(1)


def _emit(name: str, **fields: Any) -> None:
    output = os.environ.get("ACTIONS_RUNTIME_PERF_EVENT_FILE")
    if not output:
        return
    record = {"event": name, "monotonic_ns": time.monotonic_ns(), **fields}
    with _events_lock, open(output, "a", encoding="utf-8") as stream:
        stream.write(json.dumps(record, sort_keys=True) + "\n")


def _children_usage() -> tuple[float, float, int]:
    usage = resource.getrusage(resource.RUSAGE_CHILDREN)
    return usage.ru_utime, usage.ru_stime, usage.ru_maxrss


def _recording_runner(adapter):
    original_runner = adapter._subprocess_runner
    output_root = Path(os.environ["ACTIONS_RUNTIME_RCC_COMMANDS_DIR"])
    output_root.mkdir(parents=True, exist_ok=True)

    def run(*args):
        sequence = next(_runner_sequence)
        stem = f"rcc-command-{sequence:02d}"
        started = time.perf_counter_ns()
        try:
            code, stdout, stderr = original_runner(*args)
        except BaseException as exc:
            _emit(
                "rcc_subprocess",
                sequence=sequence,
                argv=list(args),
                status="raised",
                exception=type(exc).__name__,
                error=str(exc)[:300],
                subprocess_and_capture_wall_ns=time.perf_counter_ns() - started,
            )
            raise
        subprocess_wall_ns = time.perf_counter_ns() - started
        stdout_bytes = stdout.encode("utf-8", "replace")
        stderr_bytes = stderr.encode("utf-8", "replace")
        capture_started = time.perf_counter_ns()
        stdout_path = output_root / f"{stem}.stdout.log"
        stderr_path = output_root / f"{stem}.stderr.log"
        stdout_path.write_bytes(stdout_bytes)
        stderr_path.write_bytes(stderr_bytes)
        capture_write_wall_ns = time.perf_counter_ns() - capture_started
        _emit(
            "rcc_subprocess",
            sequence=sequence,
            argv=list(args),
            status=code,
            subprocess_wall_ns=subprocess_wall_ns,
            capture_write_wall_ns=capture_write_wall_ns,
            stdout={"path": str(stdout_path), "bytes": len(stdout_bytes), "sha256": hashlib.sha256(stdout_bytes).hexdigest()},
            stderr={"path": str(stderr_path), "bytes": len(stderr_bytes), "sha256": hashlib.sha256(stderr_bytes).hexdigest()},
        )
        return code, stdout, stderr

    return run


def _timed(name: str, function):
    def wrapped(*args, **kwargs):
        probe_start = time.perf_counter_ns()
        started = time.perf_counter_ns()
        before = _children_usage()
        setup_probe_ns = time.perf_counter_ns() - probe_start
        try:
            result = function(*args, **kwargs)
        except BaseException as exc:
            function_end = time.perf_counter_ns()
            after = _children_usage()
            finish_probe_ns = time.perf_counter_ns() - function_end
            _emit(
                name,
                status="error",
                exception=type(exc).__name__,
                error=str(exc)[:300],
                wall_ns=time.perf_counter_ns() - started,
                child_user_seconds=max(0.0, after[0] - before[0]),
                child_system_seconds=max(0.0, after[1] - before[1]),
                child_maxrss_kib_highwater=after[2],
                timing_probe_overhead_ns=setup_probe_ns + finish_probe_ns,
            )
            raise
        function_end = time.perf_counter_ns()
        after = _children_usage()
        finish_probe_ns = time.perf_counter_ns() - function_end
        fields: dict[str, Any] = {
            "status": "ok",
            "wall_ns": function_end - started,
            "child_user_seconds": max(0.0, after[0] - before[0]),
            "child_system_seconds": max(0.0, after[1] - before[1]),
            "child_maxrss_kib_highwater": after[2],
            "timing_probe_overhead_ns": setup_probe_ns + finish_probe_ns,
        }
        if isinstance(result, str) and name == "publish_artifact":
            fields["artifact_digest"] = result
        elif isinstance(result, dict) and name == "acquire_artifact":
            fields["artifact_digest"] = result.get("artifactDigest")
            fields["verification_valid"] = (result.get("verification") or {}).get("valid")
            fields["acquisition_identity_fields"] = {
                key: result[key]
                for key in (
                    "cacheHit", "cache_hit", "materialized", "materialization",
                    "materializationId", "materialization_id", "path", "environmentPath",
                    "environment_path", "artifactDigest",
                )
                if key in result
            }
            fields["acquisition_result_keys"] = sorted(result)
        elif isinstance(result, dict) and name == "read_receipt":
            fields["receipt"] = {
                key: result.get(key)
                for key in ("artifactDigest", "status", "exitCode", "reason", "leaseId")
            }
            fields["verification_valid"] = (result.get("verification") or {}).get("valid")
        elif name in ("get_rcc_location", "read_receipt"):
            fields["binary"] = str(result)
        elif name == "run_action":
            fields["return_code"] = result
        _emit(name, **fields)
        return result

    return wrapped


def pytest_sessionstart(session) -> None:
    usage = _children_usage()
    _emit(
        "pytest_session_start",
        pytest_version=__import__("pytest").__version__,
        python=__import__("sys").version,
        children_user_seconds=usage[0],
        children_system_seconds=usage[1],
        child_maxrss_kib_highwater=usage[2],
    )


def pytest_runtest_call(item) -> None:
    if item.name != "test_real_rcc_artifact_action_vertical":
        return

    started = time.perf_counter_ns()
    import importlib

    adapter = importlib.import_module("actions.server._rcc_runtime_adapter")
    action_import = importlib.import_module("actions.server._actions_import")
    process_pool = importlib.import_module("actions.server._actions_process_pool")
    recording_runner = _recording_runner(adapter)

    for name in ("get_rcc_location", "publish_artifact", "acquire_artifact", "read_receipt"):
        original = getattr(adapter, name)
        if name in ("get_rcc_location", "read_receipt"):
            traced = original
        else:
            def traced(*args, _original=original, **kwargs):
                kwargs.setdefault("runner", recording_runner)
                return _original(*args, **kwargs)
        setattr(adapter, name, _timed(name, traced))
    original_prepare_runtime = adapter.prepare_runtime

    def prepare_runtime_with_recording_runner(*args, **kwargs):
        kwargs.setdefault("runner", recording_runner)
        return original_prepare_runtime(*args, **kwargs)

    adapter.prepare_runtime = prepare_runtime_with_recording_runner
    action_import.import_action_package = _timed(
        "import_action_package", action_import.import_action_package
    )
    process_pool.ProcessHandle.__init__ = _timed(
        "worker_startup", process_pool.ProcessHandle.__init__
    )
    process_pool.ProcessHandle.run_action = _timed(
        "run_action", process_pool.ProcessHandle.run_action
    )
    _emit("probe_installed", setup_imports_wall_ns=time.perf_counter_ns() - started)


def pytest_sessionfinish(session, exitstatus) -> None:
    usage = _children_usage()
    _emit(
        "pytest_session_finish",
        exit_status=exitstatus,
        children_user_seconds=usage[0],
        children_system_seconds=usage[1],
        child_maxrss_kib_highwater=usage[2],
    )
