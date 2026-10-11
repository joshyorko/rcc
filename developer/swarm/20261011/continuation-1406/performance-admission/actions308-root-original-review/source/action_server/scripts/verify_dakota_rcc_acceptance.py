#!/usr/bin/env python3
"""Run a disposable local RCC Action through authenticated Runtime HTTP."""

from __future__ import annotations

import argparse
import ctypes
from email.parser import Parser
import hashlib
import http.server
import json
import math
import os
import queue
import signal
import subprocess
import sys
import tempfile
import threading
import time
import zipfile
from pathlib import Path
from typing import NamedTuple
from urllib.parse import urlsplit

RCC_VERSION = "v18.19.3"
RCC_SHA256 = "7e588c01751ca2ae15ba13ef67f2f4b7567697a5a8389737059a73936f509428"
POETRY_VERSION = "Poetry (version 2.1.1)"
PROOF_TIMEOUT_SECONDS = 2400
CLEANUP_GRACE_SECONDS = 10
CLI_WATCHDOG_SECONDS = PROOF_TIMEOUT_SECONDS + 6 * CLEANUP_GRACE_SECONDS + 5
_ENV_ALLOWLIST = {
    "PATH",
    "LANG",
    "LC_ALL",
    "TZ",
    "SSL_CERT_FILE",
    "SSL_CERT_DIR",
    "REQUESTS_CA_BUNDLE",
    "CURL_CA_BUNDLE",
    "PIP_CERT",
    "CONDA_SSL_VERIFY",
    "NODE_EXTRA_CA_CERTS",
    "HTTP_PROXY",
    "HTTPS_PROXY",
    "ALL_PROXY",
    "NO_PROXY",
    "http_proxy",
    "https_proxy",
    "all_proxy",
    "no_proxy",
    "SYSTEMROOT",
    "WINDIR",
    "COMSPEC",
    "PATHEXT",
}


class CandidateWheel(NamedTuple):
    distribution: str
    version: str
    path: Path


class CandidateWheels(NamedTuple):
    core: CandidateWheel
    helper: CandidateWheel


_SAFE_EXTRA_ENV = {
    "ACTIONS_ACCEPTANCE_POETRY",
    "ACTIONS_ACCEPTANCE_ROBOCORP_HOME",
    "ACTIONS_RUNTIME_RCC_BINARY",
    "ACTIONS_RUNTIME_RCC_TIMEOUT",
    "ACTIONS_REAL_RCC_ARTIFACT_TEST",
    "ACTIONS_RUNTIME_RCC_PROVIDER",
    "ROBOCORP_HOME",
    "POETRY_CACHE_DIR",
}


class Deadline:
    def __init__(self, timeout_seconds: float):
        self._expires_at = time.monotonic() + timeout_seconds

    @classmethod
    def after(cls, timeout_seconds: float) -> "Deadline":
        return cls(timeout_seconds)

    def remaining(self, cap: float | None = None) -> float:
        remaining = self._expires_at - time.monotonic()
        if remaining <= 0:
            raise TimeoutError("RCC acceptance exceeded its total deadline")
        return min(remaining, cap) if cap is not None else remaining

    def remaining_int(self) -> int:
        return max(1, math.ceil(self.remaining()))


class ProcessTreeCleanupError(RuntimeError):
    """An owned command or descendant violated bounded cleanup."""

    def __init__(
        self,
        message: str,
        *,
        command_returncode: int | None = None,
        cleanup_disposition: str = "incomplete",
        command_stdout: str = "",
        command_stderr: str = "",
    ) -> None:
        super().__init__(message)
        self.command_returncode = command_returncode
        self.cleanup_disposition = cleanup_disposition
        self.command_stdout = command_stdout
        self.command_stderr = command_stderr


def child_environment(
    source: dict[str, str], *, task_root: Path, extra: dict[str, str] | None = None
) -> dict[str, str]:
    """Copy only execution, network, and trust settings needed by the proof."""

    task_root.mkdir(parents=True, exist_ok=True)
    home = task_root / "home"
    temp = task_root / "tmp"
    home.mkdir(parents=True, exist_ok=True)
    temp.mkdir(parents=True, exist_ok=True)
    env = {key: value for key, value in source.items() if key in _ENV_ALLOWLIST}
    env["PATH"] = source.get("PATH", os.defpath)
    env["HOME"] = str(home)
    env["TMPDIR"] = str(temp)
    env["TMP"] = str(temp)
    env["TEMP"] = str(temp)
    for key in (
        "HTTP_PROXY",
        "HTTPS_PROXY",
        "ALL_PROXY",
        "http_proxy",
        "https_proxy",
        "all_proxy",
    ):
        value = env.get(key)
        if value:
            parsed = urlsplit(value)
            if (
                not parsed.scheme
                or not parsed.hostname
                or parsed.username
                or parsed.password
                or parsed.query
                or parsed.fragment
                or parsed.path not in ("", "/")
            ):
                raise ValueError(
                    "proxy settings must not contain credentials or URL data"
                )
    no_proxy_keys = ("NO_PROXY", "no_proxy")
    for key in no_proxy_keys:
        existing = env.get(key)
        additions = ["127.0.0.1", "localhost"]
        env[key] = ",".join(filter(None, [existing, *additions]))
    if extra:
        forbidden = set(extra) - _SAFE_EXTRA_ENV
        if forbidden:
            raise ValueError("unsupported child environment override")
        env.update(extra)
    return env


def resolve_poetry(
    source_env: dict[str, str], *, task_root: Path, deadline: Deadline
) -> Path:
    poetry_value = source_env.get("ACTIONS_ACCEPTANCE_POETRY")
    tool_home_value = source_env.get("ACTIONS_ACCEPTANCE_ROBOCORP_HOME")
    if not poetry_value or not tool_home_value:
        raise RuntimeError(
            "Set ACTIONS_ACCEPTANCE_POETRY and ACTIONS_ACCEPTANCE_ROBOCORP_HOME "
            "from the pinned RCC toolchain"
        )
    poetry = Path(poetry_value).expanduser().resolve()
    tool_home = Path(tool_home_value).expanduser().resolve()
    try:
        poetry.relative_to(tool_home / "holotree")
    except ValueError as exc:
        raise RuntimeError("Poetry must come from this pinned RCC holotree") from exc
    if not poetry.is_file() or not os.access(poetry, os.X_OK):
        raise RuntimeError("the pinned RCC Poetry executable is unavailable")
    env = child_environment(
        source_env,
        task_root=task_root,
        extra={"ROBOCORP_HOME": str(tool_home)},
    )
    completed = run_owned_process(
        [str(poetry), "--version"],
        timeout_seconds=deadline.remaining(cap=10),
        env=env,
    )
    if completed.returncode or completed.stdout.strip() != POETRY_VERSION:
        raise RuntimeError("candidate wheels require pinned RCC Poetry 2.1.1")
    return poetry


def terminate_process_tree(
    process: subprocess.Popen,
    *,
    grace_seconds: float = CLEANUP_GRACE_SECONDS,
) -> None:
    import psutil

    process_group = None
    if os.name == "posix":
        try:
            group_id = os.getpgid(process.pid)
            if group_id == process.pid:
                process_group = group_id
        except (ProcessLookupError, PermissionError):
            pass
    try:
        root = psutil.Process(process.pid)
    except psutil.NoSuchProcess:
        root = None
    owned: dict[tuple[int, float], psutil.Process] = {}

    def refresh_owned() -> None:
        if root is not None:
            try:
                descendants = root.children(recursive=True)
            except psutil.NoSuchProcess:
                descendants = []
            for child in descendants:
                try:
                    owned[(child.pid, child.create_time())] = child
                except psutil.Error:
                    continue
        if process_group is not None:
            for candidate in psutil.process_iter(attrs=["pid", "status"]):
                try:
                    if os.getpgid(candidate.pid) == process_group:
                        owned[(candidate.pid, candidate.create_time())] = candidate
                except (OSError, psutil.Error):
                    continue

    def active_owned() -> list[psutil.Process]:
        active = []
        for child in owned.values():
            try:
                if child.is_running() and child.status() != psutil.STATUS_ZOMBIE:
                    active.append(child)
            except psutil.Error:
                continue
        return active

    refresh_owned()
    for child in reversed(list(owned.values())):
        try:
            child.terminate()
        except psutil.Error:
            continue
    if root is not None:
        try:
            root.terminate()
        except psutil.Error:
            pass
    if process_group is not None:
        try:
            os.killpg(process_group, signal.SIGTERM)
        except ProcessLookupError:
            pass

    grace_deadline = time.monotonic() + grace_seconds
    while time.monotonic() < grace_deadline:
        refresh_owned()
        if not active_owned():
            break
        time.sleep(min(0.05, max(0.0, grace_deadline - time.monotonic())))

    survivors = active_owned()
    if survivors:
        for child in survivors:
            try:
                child.kill()
            except psutil.Error:
                continue
        if process_group is not None:
            try:
                os.killpg(process_group, signal.SIGKILL)
            except ProcessLookupError:
                pass

    reap_deadline = time.monotonic() + grace_seconds
    while time.monotonic() < reap_deadline:
        refresh_owned()
        survivors = active_owned()
        if not survivors:
            break
        time.sleep(min(0.05, max(0.0, reap_deadline - time.monotonic())))
    survivors = active_owned()
    remaining = max(0.01, reap_deadline - time.monotonic())
    try:
        process.wait(timeout=remaining)
    except subprocess.TimeoutExpired:
        try:
            process.kill()
        except ProcessLookupError:
            pass
        try:
            process.wait(timeout=remaining)
        except subprocess.TimeoutExpired:
            survivors.append(root)
    if survivors:
        raise ProcessTreeCleanupError(
            "owned process tree did not stop within cleanup grace: "
            f"{sorted({child.pid for child in survivors})}"
        )


def run_owned_process(
    command: list[str],
    *,
    timeout_seconds: float,
    env: dict[str, str],
    cwd: Path | None = None,
    cleanup_grace_seconds: float = CLEANUP_GRACE_SECONDS,
) -> subprocess.CompletedProcess:
    command = [os.fspath(argument) for argument in command]
    if sys.platform == "linux":
        payload = json.dumps(
            {
                "command": command,
                "cwd": str(cwd) if cwd else None,
                "env": env,
                "timeout_seconds": timeout_seconds,
                "cleanup_grace_seconds": cleanup_grace_seconds,
            }
        )
        supervisor = subprocess.Popen(
            [sys.executable, str(Path(__file__).resolve()), "--_supervisor"],
            stdin=subprocess.PIPE,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            text=True,
            env=env,
            start_new_session=True,
        )
        parent_timeout = timeout_seconds + (4 * cleanup_grace_seconds) + 5
        try:
            stdout, stderr = supervisor.communicate(payload, timeout=parent_timeout)
        except subprocess.TimeoutExpired as exc:
            terminate_process_tree(supervisor, grace_seconds=cleanup_grace_seconds)
            raise ProcessTreeCleanupError(
                "Linux process supervisor exceeded its independent hard deadline"
            ) from exc
        if supervisor.returncode != 0:
            raise ProcessTreeCleanupError(
                "Linux process supervisor failed: " + stderr[-2000:]
            )
        try:
            result = json.loads(stdout)
        except json.JSONDecodeError as exc:
            raise ProcessTreeCleanupError(
                "Linux process supervisor returned malformed output"
            ) from exc
        completed = subprocess.CompletedProcess(
            command, result["returncode"], result["stdout"], result["stderr"]
        )
        completed.cleanup_disposition = result["cleanup_disposition"]
        completed.reaped_descendants = result["reaped_descendants"]
        if result.get("unexpected_live_descendants") and not result["timed_out"]:
            raise ProcessTreeCleanupError(
                "command exited while owned descendants were still live; "
                f"cleanup disposition={result['cleanup_disposition']}",
                command_returncode=completed.returncode,
                cleanup_disposition=result["cleanup_disposition"],
                command_stdout=completed.stdout,
                command_stderr=completed.stderr,
            )
        if result["timed_out"]:
            raise subprocess.TimeoutExpired(
                command,
                timeout_seconds,
                output=completed.stdout,
                stderr=completed.stderr,
            )
        return completed

    process = subprocess.Popen(
        command,
        cwd=cwd,
        env=env,
        stdin=subprocess.DEVNULL,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        text=True,
        start_new_session=(os.name == "posix"),
    )
    try:
        stdout, stderr = process.communicate(timeout=timeout_seconds)
    except subprocess.TimeoutExpired as exc:
        terminate_process_tree(process, grace_seconds=cleanup_grace_seconds)
        try:
            stdout, stderr = process.communicate(timeout=cleanup_grace_seconds)
        except subprocess.TimeoutExpired as pipe_error:
            for stream in (process.stdout, process.stderr):
                if stream is not None:
                    stream.close()
            raise ProcessTreeCleanupError(
                "captured output pipes still had descendant writers after cleanup"
            ) from pipe_error
        raise subprocess.TimeoutExpired(
            command,
            timeout_seconds,
            output=stdout or exc.output,
            stderr=stderr or exc.stderr,
        ) from exc
    return subprocess.CompletedProcess(command, process.returncode, stdout, stderr)


def _supervisor_children(psutil_module):
    try:
        return psutil_module.Process(os.getpid()).children(recursive=True)
    except psutil_module.Error as exc:
        raise ProcessTreeCleanupError(
            "unable to enumerate subreaper-owned descendants"
        ) from exc


def _process_is_live(process, psutil_module) -> bool:
    try:
        return process.status() != psutil_module.STATUS_ZOMBIE
    except psutil_module.NoSuchProcess:
        return False
    except psutil_module.Error as exc:
        raise ProcessTreeCleanupError(
            f"unable to inspect owned process {process.pid}"
        ) from exc


def _reap_adopted_children(*, deadline: float) -> int:
    reaped = 0
    while True:
        try:
            pid, _status = os.waitpid(-1, os.WNOHANG)
        except ChildProcessError:  # waitpid(-1) confirmed ECHILD
            return reaped
        if pid > 0:
            reaped += 1
            continue
        if time.monotonic() >= deadline:
            raise ProcessTreeCleanupError(
                "subreaper could not reach ECHILD before the reap deadline"
            )
        time.sleep(0.01)


def _proc_descendants(root_pid: int) -> list[int]:
    descendants: list[int] = []
    pending = [root_pid]
    while pending:
        parent_pid = pending.pop()
        children_path = Path(f"/proc/{parent_pid}/task/{parent_pid}/children")
        try:
            child_pids = [int(value) for value in children_path.read_text().split()]
        except FileNotFoundError:
            continue
        except OSError as exc:
            raise ProcessTreeCleanupError(
                f"unable to enumerate Linux child list for pid {parent_pid}"
            ) from exc
        descendants.extend(child_pids)
        pending.extend(child_pids)
    return descendants


def _fallback_reap_after_error(child, *, grace: float) -> None:
    """Use Linux /proc ownership when normal supervisor inspection fails."""

    deadline = time.monotonic() + (2 * grace)
    while time.monotonic() < deadline:
        try:
            owned = _proc_descendants(os.getpid())
        except ProcessTreeCleanupError:
            owned = []
        for pid in reversed(owned):
            try:
                os.kill(pid, signal.SIGKILL)
            except ProcessLookupError:
                pass
        if child.poll() is None:
            try:
                os.killpg(child.pid, signal.SIGKILL)
            except ProcessLookupError:
                pass
            try:
                child.kill()
            except ProcessLookupError:
                pass
        if child.poll() is None:
            try:
                child.wait(timeout=min(0.05, max(0.01, deadline - time.monotonic())))
            except subprocess.TimeoutExpired:
                pass
    child.wait(timeout=max(0.05, grace))
    _reap_adopted_children(deadline=time.monotonic() + grace)


def _stop_supervised_processes(child, psutil_module, *, grace: float) -> str:
    descendants = _supervisor_children(psutil_module)
    if child.poll() is not None and not any(
        _process_is_live(process, psutil_module) for process in descendants
    ):
        child.wait()
        return "reaped_exited_descendants" if descendants else "clean_no_descendants"

    for process in reversed(descendants):
        if not _process_is_live(process, psutil_module):
            continue
        try:
            process.terminate()
        except psutil_module.NoSuchProcess:
            pass
        except psutil_module.Error as exc:
            raise ProcessTreeCleanupError(
                f"unable to terminate owned process {process.pid}"
            ) from exc
    if child.poll() is None:
        try:
            child.terminate()
        except ProcessLookupError:
            pass

    grace_deadline = time.monotonic() + grace
    while time.monotonic() < grace_deadline:
        active = [
            process
            for process in _supervisor_children(psutil_module)
            if _process_is_live(process, psutil_module)
        ]
        if child.poll() is not None and not active:
            break
        time.sleep(0.01)

    active = [
        process
        for process in _supervisor_children(psutil_module)
        if _process_is_live(process, psutil_module)
    ]
    escalated = bool(active or child.poll() is None)
    for process in reversed(active):
        try:
            process.kill()
        except psutil_module.NoSuchProcess:
            pass
        except psutil_module.Error as exc:
            raise ProcessTreeCleanupError(
                f"unable to kill owned process {process.pid}"
            ) from exc
    if child.poll() is None:
        try:
            child.kill()
        except ProcessLookupError:
            pass
    try:
        child.wait(timeout=max(0.05, grace))
    except subprocess.TimeoutExpired as exc:
        raise ProcessTreeCleanupError(
            "owned command process could not be reaped"
        ) from exc

    reap_deadline = time.monotonic() + grace
    _reap_adopted_children(deadline=reap_deadline)
    if not descendants:
        return (
            "timed_out_owner_terminated"
            if child.returncode is not None
            else "clean_no_descendants"
        )
    return (
        "killed_unexpected_descendants"
        if escalated
        else "terminated_unexpected_descendants"
    )


def _supervisor_main() -> int:
    """Contain one Linux command and all descendants in a dedicated subreaper."""

    if sys.platform != "linux":
        return 2
    libc = ctypes.CDLL(None, use_errno=True)
    if libc.prctl(36, 1, 0, 0, 0) != 0:  # PR_SET_CHILD_SUBREAPER
        return 2
    import psutil

    payload = json.load(sys.stdin)
    command = payload["command"]
    grace = float(payload["cleanup_grace_seconds"])
    command_deadline = time.monotonic() + float(payload["timeout_seconds"])
    timed_out = False
    child = None
    with tempfile.TemporaryDirectory(prefix="dakota-owned-process-") as scratch:
        stdout_path = Path(scratch) / "stdout"
        stderr_path = Path(scratch) / "stderr"
        with (
            stdout_path.open("w+b") as stdout_file,
            stderr_path.open("w+b") as stderr_file,
        ):
            try:
                child = subprocess.Popen(
                    command,
                    cwd=payload["cwd"],
                    env=payload["env"],
                    stdin=subprocess.DEVNULL,
                    stdout=stdout_file,
                    stderr=stderr_file,
                    start_new_session=True,
                )
                while child.poll() is None and time.monotonic() < command_deadline:
                    time.sleep(0.02)
                timed_out = child.poll() is None
                descendants = _supervisor_children(psutil)
                unexpected_live = any(
                    _process_is_live(process, psutil) for process in descendants
                )
                disposition = _stop_supervised_processes(child, psutil, grace=grace)
                reaped_descendants = _reap_adopted_children(
                    deadline=time.monotonic() + grace
                )
                # The second drain is deliberate: success, timeout, and cleanup
                # all require waitpid(-1) to observe ECHILD before evidence returns.
                if child.poll() is None:
                    raise ProcessTreeCleanupError("supervised owner remains unreaped")
                stdout_file.flush()
                stderr_file.flush()
                stdout_file.seek(0)
                stderr_file.seek(0)
                result = {
                    "returncode": child.returncode,
                    "timed_out": timed_out,
                    "stdout": stdout_file.read().decode(errors="replace"),
                    "stderr": stderr_file.read().decode(errors="replace"),
                    "cleanup_disposition": disposition,
                    "reaped_descendants": reaped_descendants,
                    "unexpected_live_descendants": unexpected_live,
                }
            except BaseException:
                if child is not None:
                    _fallback_reap_after_error(child, grace=grace)
                raise
    print(json.dumps(result), flush=True)
    return 0


def capture_process_tree(root_pid: int):
    import psutil

    try:
        root = psutil.Process(root_pid)
    except psutil.NoSuchProcess:
        return []
    try:
        return [root, *root.children(recursive=True)]
    except psutil.NoSuchProcess:
        return [root]


def refresh_process_tree(processes, root_pid: int) -> None:
    known = {(proc.pid, proc.create_time()) for proc in processes}
    for process in capture_process_tree(root_pid):
        identity = (process.pid, process.create_time())
        if identity not in known:
            processes.append(process)
            known.add(identity)


def stop_runtime_server(server, server_popen, server_tree) -> None:
    if server_popen is not None:
        refresh_process_tree(server_tree, server_popen.pid)
    server.stop()


def wait_for_process_tree_reap(
    processes, *, timeout_seconds: float = CLEANUP_GRACE_SECONDS
) -> None:
    import psutil

    def active():
        running = []
        for child in processes:
            try:
                if child.is_running() and child.status() != psutil.STATUS_ZOMBIE:
                    running.append(child)
            except psutil.Error:
                continue
        return running

    deadline = time.monotonic() + timeout_seconds
    while time.monotonic() < deadline:
        if not active():
            return
        time.sleep(min(0.05, max(0.0, deadline - time.monotonic())))
    survivors = active()
    for child in survivors:
        try:
            child.kill()
        except psutil.Error:
            continue
    kill_deadline = time.monotonic() + timeout_seconds
    while time.monotonic() < kill_deadline:
        survivors = active()
        if not survivors:
            return
        time.sleep(min(0.05, max(0.0, kill_deadline - time.monotonic())))
    survivors = active()
    if survivors:
        raise ProcessTreeCleanupError(
            f"Runtime process tree was not reaped: {sorted(child.pid for child in survivors)}"
        )


def classify_wrapper_exit(receipt: dict[str, object]) -> str:
    return (
        "PASS"
        if receipt.get("status") == "completed"
        # JSON booleans must not satisfy the RCC integer exit-code contract.
        and type(receipt.get("exitCode")) is int  # noqa: E721
        and receipt.get("exitCode") == 0
        else "FAIL"
    )


def _source_root() -> Path:
    return Path(__file__).resolve().parents[1]


def _parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(
        description=(
            "Prove the source Runtime's local RCC Environment Artifact and process "
            "lease with a synthetic Action over authenticated HTTP."
        )
    )
    parser.add_argument(
        "--mode",
        choices=("candidate-wheel",),
        default="candidate-wheel",
        help="Use local candidate Core/Helper wheels with the source Runtime.",
    )
    parser.add_argument(
        "--receipt",
        type=Path,
        required=True,
        help="Write the sanitized proof receipt outside the disposable run directory.",
    )
    parser.add_argument("--_worker", action="store_true", help=argparse.SUPPRESS)
    return parser


def _readline_with_timeout(stream, timeout_seconds: float) -> str:
    result: queue.Queue[str] = queue.Queue(maxsize=1)
    reader = threading.Thread(target=lambda: result.put(stream.readline()), daemon=True)
    reader.start()
    try:
        return result.get(timeout=timeout_seconds)
    except queue.Empty as exc:
        raise TimeoutError("RCC cache serve startup timed out") from exc


def _provider_command(rcc_binary: str, provider_root: Path) -> list[str]:
    return [
        rcc_binary,
        "cache",
        "serve",
        "--root",
        str(provider_root),
        "--listen",
        "127.0.0.1:0",
        "--json",
    ]


def start_provider(
    rcc_binary: str,
    root: Path,
    env: dict[str, str],
    *,
    deadline: Deadline,
    cleanup_grace_seconds: float = CLEANUP_GRACE_SECONDS,
) -> tuple[subprocess.Popen, str]:
    provider_root = root / "provider"
    process = subprocess.Popen(
        _provider_command(rcc_binary, provider_root),
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        text=True,
        env=env,
        stdin=subprocess.DEVNULL,
    )
    try:
        if process.stdout is None:
            raise RuntimeError("RCC cache serve stdout is unavailable")
        startup = _readline_with_timeout(process.stdout, deadline.remaining(cap=10))
        try:
            payload = json.loads(startup)
        except json.JSONDecodeError as exc:
            raise RuntimeError("RCC cache serve returned invalid startup JSON") from exc
        url = payload["url"]
        parsed = urlsplit(url)
        if (
            parsed.scheme != "http"
            or parsed.hostname != "127.0.0.1"
            or parsed.username
            or parsed.password
            or parsed.query
            or parsed.fragment
        ):
            raise RuntimeError(
                "RCC cache serve did not bind to credential-free loopback"
            )
        return process, url
    except BaseException:
        terminate_process_tree(process, grace_seconds=cleanup_grace_seconds)
        raise


class UnavailableProviderProbe:
    """Count requests to the retired provider origin without serving artifacts."""

    def __init__(self, host: str, port: int) -> None:
        self._count = 0
        self._requests: list[dict[str, object]] = []
        self._count_lock = threading.Lock()
        owner = self

        class Handler(http.server.BaseHTTPRequestHandler):
            def _reject(self) -> None:
                with owner._count_lock:
                    owner._count += 1
                    owner._requests.append(
                        {
                            "method": self.command,
                            "path": urlsplit(self.path).path[:512],
                            "status": 503,
                        }
                    )
                body = b'{"error":"provider unavailable"}'
                self.send_response(503)
                self.send_header("Content-Type", "application/json")
                self.send_header("Content-Length", str(len(body)))
                self.end_headers()
                if self.command != "HEAD":
                    self.wfile.write(body)

            do_DELETE = _reject
            do_GET = _reject
            do_HEAD = _reject
            do_OPTIONS = _reject
            do_PATCH = _reject
            do_POST = _reject
            do_PUT = _reject
            do_CONNECT = _reject
            do_TRACE = _reject

            def log_message(self, _format: str, *_args: object) -> None:
                return

        class Server(http.server.ThreadingHTTPServer):
            daemon_threads = True
            allow_reuse_address = True

        self._server = Server((host, port), Handler)
        self.url = f"http://{host}:{self._server.server_port}"
        self._thread = threading.Thread(
            target=self._server.serve_forever,
            name="rcc-unavailable-provider-probe",
            daemon=True,
        )
        self._thread.start()

    @property
    def request_count(self) -> int:
        with self._count_lock:
            return self._count

    @property
    def requests(self) -> list[dict[str, object]]:
        with self._count_lock:
            return list(self._requests)

    def reset_requests(self) -> list[dict[str, object]]:
        with self._count_lock:
            previous = list(self._requests)
            self._requests.clear()
            self._count = 0
            return previous

    def close(self, timeout_seconds: float = CLEANUP_GRACE_SECONDS) -> None:
        self._server.shutdown()
        self._server.server_close()
        self._thread.join(timeout_seconds)
        if self._thread.is_alive():
            raise RuntimeError("provider request probe did not stop")


def build_candidate_wheels(
    root: Path,
    *,
    source_env: dict[str, str],
    poetry: Path,
    deadline: Deadline,
    repo_root: Path | None = None,
) -> CandidateWheels:
    repo_root = repo_root or Path(__file__).resolve().parents[2]
    wheelhouse = root / "candidate-wheelhouse"
    wheelhouse.mkdir()
    env = child_environment(
        source_env,
        task_root=root / "poetry-build",
        extra={
            "ACTIONS_ACCEPTANCE_POETRY": str(poetry),
            "ACTIONS_ACCEPTANCE_ROBOCORP_HOME": source_env[
                "ACTIONS_ACCEPTANCE_ROBOCORP_HOME"
            ],
            "ROBOCORP_HOME": source_env["ACTIONS_ACCEPTANCE_ROBOCORP_HOME"],
            "POETRY_CACHE_DIR": str(root / "poetry-cache"),
        },
    )
    package_specs = (
        (repo_root / "actions-http-helper", "actions-http-helper"),
        (repo_root / "actions", "actions-core"),
    )
    versions: dict[str, str] = {}
    for package_dir, distribution in package_specs:
        result = run_owned_process(
            [poetry, "version", "--short"],
            cwd=package_dir,
            env=env,
            timeout_seconds=deadline.remaining(),
        )
        version = (result.stdout or "").strip()
        if result.returncode or not version or any(c.isspace() for c in version):
            raise RuntimeError(
                f"pinned Poetry could not read {distribution} candidate version"
            )
        versions[distribution] = version

    for package_dir, _distribution in package_specs:
        result = run_owned_process(
            [poetry, "build", "--format", "wheel", "--output", str(wheelhouse)],
            cwd=package_dir,
            env=env,
            timeout_seconds=deadline.remaining(),
        )
        if result.returncode:
            raise RuntimeError(
                "pinned Poetry candidate wheel build failed: "
                f"{(result.stderr or result.stdout)[-1000:]}"
            )

    candidates: dict[str, CandidateWheel] = {}
    for _package_dir, distribution in package_specs:
        version = versions[distribution]
        filename_distribution = distribution.replace("-", "_")
        wheel = wheelhouse / (
            f"{filename_distribution}-{version}-py3-none-any.whl"
        )
        if not wheel.is_file():
            raise RuntimeError(
                f"Poetry did not produce {distribution} {version} candidate wheel"
            )
        validate_candidate_wheel_metadata(
            wheel, expected_distribution=distribution, expected_version=version
        )
        candidates[distribution] = CandidateWheel(
            distribution=distribution, version=version, path=wheel
        )
    return CandidateWheels(
        core=candidates["actions-core"],
        helper=candidates["actions-http-helper"],
    )


def validate_candidate_wheel_metadata(
    wheel: Path, *, expected_distribution: str, expected_version: str
) -> None:
    try:
        with zipfile.ZipFile(wheel) as archive:
            metadata_members = [
                member
                for member in archive.namelist()
                if member.endswith(".dist-info/METADATA")
            ]
            if len(metadata_members) != 1:
                raise RuntimeError(
                    f"candidate wheel METADATA inventory is invalid: {wheel.name}"
                )
            metadata_text = archive.read(metadata_members[0]).decode("utf-8")
    except (OSError, UnicodeDecodeError, zipfile.BadZipFile) as error:
        raise RuntimeError(
            f"candidate wheel METADATA could not be read: {wheel.name}"
        ) from error

    metadata = Parser().parsestr(metadata_text)
    actual_distribution = metadata.get("Name")
    actual_version = metadata.get("Version")
    if (
        actual_distribution != expected_distribution
        or actual_version != expected_version
    ):
        raise RuntimeError(
            f"candidate wheel METADATA mismatch for {wheel.name}: expected "
            f"{expected_distribution}=={expected_version}, got "
            f"{actual_distribution}=={actual_version}"
        )


def candidate_version_fields(wheels: CandidateWheels) -> dict[str, str]:
    return {
        "actions_core": wheels.core.version,
        "actions_http_helper": wheels.helper.version,
    }


def sha256_file(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as stream:
        for block in iter(lambda: stream.read(1024 * 1024), b""):
            digest.update(block)
    return digest.hexdigest()


def candidate_wheel_records(wheels: CandidateWheels) -> dict[str, dict[str, str]]:
    return {
        wheel.distribution: {
            "filename": wheel.path.name,
            "version": wheel.version,
            "sha256": sha256_file(wheel.path),
        }
        for wheel in (wheels.core, wheels.helper)
    }


def source_revision(repo_root: Path, *, env: dict[str, str], deadline: Deadline) -> str:
    status = run_owned_process(
        ["git", "status", "--porcelain", "--untracked-files=all"],
        cwd=repo_root,
        env=env,
        timeout_seconds=deadline.remaining(cap=10),
    )
    if status.returncode or status.stdout.strip():
        raise RuntimeError("source checkout must be clean before recording its SHA")
    result = run_owned_process(
        ["git", "rev-parse", "HEAD"],
        cwd=repo_root,
        env=env,
        timeout_seconds=deadline.remaining(cap=10),
    )
    source_sha = result.stdout.strip()
    if (
        result.returncode
        or len(source_sha) != 40
        or any(c not in "0123456789abcdef" for c in source_sha)
    ):
        raise RuntimeError("unable to determine the exact source commit")
    return source_sha


def write_evidence(path: Path, evidence: dict[str, object], *, temp_root: Path) -> None:
    target = path.expanduser().resolve()
    try:
        target.relative_to(temp_root.resolve())
    except ValueError:
        pass
    else:
        raise ValueError("evidence receipt must be outside the disposable temp root")
    target.parent.mkdir(parents=True, exist_ok=True)
    payload = (json.dumps(evidence, sort_keys=True, indent=2) + "\n").encode()
    fd = os.open(target, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    with os.fdopen(fd, "wb") as stream:
        stream.write(payload)
        stream.flush()
        os.fsync(stream.fileno())


def record_supervisor_cleanup_failure(
    receipt_path: Path, error: ProcessTreeCleanupError
) -> None:
    target = receipt_path.expanduser().resolve()
    if not target.is_file():
        return
    evidence = json.loads(target.read_text(encoding="utf-8"))
    cells = evidence.get("cells")
    if not isinstance(cells, dict):
        raise ValueError("cannot record supervisor cleanup without acceptance cells")
    cells["process_cleanup"] = "FAIL"
    evidence["acceptance_status"] = "FAIL"
    evidence["supervisor_cleanup"] = {
        "disposition": error.cleanup_disposition,
        "command_returncode": error.command_returncode,
    }
    payload = (json.dumps(evidence, sort_keys=True, indent=2) + "\n").encode()
    fd, temporary_path = tempfile.mkstemp(prefix=f".{target.name}.", dir=target.parent)
    try:
        with os.fdopen(fd, "wb") as stream:
            os.fchmod(stream.fileno(), 0o600)
            stream.write(payload)
            stream.flush()
            os.fsync(stream.fileno())
        os.replace(temporary_path, target)
    except BaseException:
        try:
            os.unlink(temporary_path)
        except FileNotFoundError:
            pass
        raise


def _private_deadline_receipt_file(
    payload: bytes, *, parent: Path, prefix: str
) -> Path:
    fd, temporary_path = tempfile.mkstemp(prefix=prefix, suffix=".json", dir=parent)
    try:
        with os.fdopen(fd, "wb") as stream:
            if os.name == "posix":
                os.fchmod(stream.fileno(), 0o600)
            stream.write(payload)
            stream.flush()
            os.fsync(stream.fileno())
    except BaseException as exc:
        try:
            os.unlink(temporary_path)
        except FileNotFoundError:
            pass
        except OSError:
            exc.add_note("owned deadline temporary cleanup incomplete")
        raise
    return Path(temporary_path)

def record_supervisor_deadline_failure(receipt_path: Path) -> str:
    """Demote parent acceptance without changing the worker's measured cells."""
    target = receipt_path.expanduser().resolve()
    if not target.is_file():
        return "unavailable"
    try:
        original = target.read_bytes()
    except OSError:
        return "read_failed"
    try:
        evidence = json.loads(original)
    except (ValueError, UnicodeDecodeError):
        return "invalid_json"
    if not isinstance(evidence, dict) or not isinstance(evidence.get("cells"), dict):
        return "invalid_shape"
    # This is the only new top-level annotation; never overwrite worker fields.
    if "supervisor_deadline" in evidence:
        return "annotation_conflict"
    try:
        preserved = _private_deadline_receipt_file(
            original, parent=target.parent, prefix=".rcc-worker-original-"
        )
    except Exception:
        return "preservation_failed"
    evidence["supervisor_deadline"] = {
        "reason": "watchdog_timeout",
        "cleanup_state": "unknown",
        "worker_acceptance_status": evidence.get("acceptance_status"),
        "worker_receipt_sha256": hashlib.sha256(original).hexdigest(),
        "worker_receipt_file": preserved.name,
    }
    evidence["acceptance_status"] = "FAIL"
    try:
        payload = (json.dumps(evidence, sort_keys=True, indent=2) + "\n").encode()
    except Exception:
        return "encode_failed"
    try:
        temporary = _private_deadline_receipt_file(
            payload, parent=target.parent, prefix=".rcc-deadline-"
        )
    except Exception:
        return "write_failed"
    try:
        os.replace(temporary, target)
    except Exception:
        try:
            os.unlink(temporary)
        except OSError:
            pass
        return "replace_failed"
    return "recorded"


def acceptance_status(cells: dict[str, str]) -> str:
    return (
        "PASS" if cells and all(value == "PASS" for value in cells.values()) else "FAIL"
    )


def classify_zero_provider_requests(request_count: object) -> str:
    return (
        "PASS"
        if type(request_count) is int  # noqa: E721 - reject bool as a count
        and request_count == 0
        else "FAIL"
    )


def sanitize_diagnostic_output(output: str, *, replacements: dict[str, str]) -> str:
    for original, replacement in replacements.items():
        if original:
            output = output.replace(original, replacement)
    return output[-4000:]


def run_acquire_diagnostic(
    rcc_binary: str,
    digest: str,
    *,
    provider: str | None,
    env: dict[str, str],
    deadline: Deadline,
    replacements: dict[str, str],
) -> dict[str, object]:
    command = [
        rcc_binary,
        "env",
        "acquire",
        "--artifact",
        digest,
        "--json",
        "--permissive-local",
    ]
    if provider:
        command.extend(["--provider", provider])
    result = run_owned_process(
        command,
        timeout_seconds=deadline.remaining(cap=30),
        env=env,
    )
    payload = None
    exact_digest = None
    verification_valid = None
    if result.stdout.strip():
        try:
            payload = json.loads(result.stdout)
        except json.JSONDecodeError:
            payload = None
    if isinstance(payload, dict):
        try:
            from actions.server._rcc_runtime_adapter import parse_artifact_digest

            exact_digest = parse_artifact_digest(payload) == digest
        except Exception:
            exact_digest = False
        verification = payload.get("verification")
        if isinstance(verification, dict):
            verification_valid = verification.get("valid") is True
    return {
        "exit_code": result.returncode,
        "exact_digest": exact_digest,
        "verification_valid": verification_valid,
        "stdout": sanitize_diagnostic_output(result.stdout, replacements=replacements),
        "stderr": sanitize_diagnostic_output(result.stderr, replacements=replacements),
    }


def _verify_rcc(
    source_env: dict[str, str], *, task_root: Path, deadline: Deadline
) -> tuple[str, str]:
    rcc_value = source_env.get("ACTIONS_RUNTIME_RCC_BINARY")
    if not rcc_value:
        raise RuntimeError("ACTIONS_RUNTIME_RCC_BINARY must name pinned RCC v18.19.3")
    rcc = Path(rcc_value).expanduser().resolve()
    if not rcc.is_file() or not os.access(rcc, os.X_OK):
        raise RuntimeError("the pinned RCC executable is unavailable")
    env = child_environment(
        source_env,
        task_root=task_root,
        extra={"ACTIONS_RUNTIME_RCC_BINARY": str(rcc)},
    )
    result = run_owned_process(
        [str(rcc), "version"],
        timeout_seconds=deadline.remaining(cap=10),
        env=env,
    )
    if result.returncode or result.stdout.strip() != RCC_VERSION:
        raise RuntimeError("candidate acceptance requires RCC v18.19.3")
    digest = sha256_file(rcc)
    if digest != RCC_SHA256:
        raise RuntimeError("RCC binary does not match the pinned Linux release digest")
    return str(rcc), digest


def _sanitize_runtime_receipt(receipt: dict[str, object]) -> dict[str, object]:
    verification = receipt.get("verification")
    if not isinstance(verification, dict):
        raise AssertionError("RCC receipt verification is not an object")
    return {
        "artifactDigest": receipt["artifactDigest"],
        "verification": {"valid": verification.get("valid")},
        "leaseId": receipt["leaseId"],
        **{
            key: receipt[key]
            for key in ("status", "exitCode", "reason")
            if key in receipt
        },
    }


def _run(receipt_path: Path) -> dict[str, object]:
    deadline = Deadline.after(PROOF_TIMEOUT_SECONDS)
    source_env = dict(os.environ)
    repo_root = Path(__file__).resolve().parents[2]
    if receipt_path.expanduser().resolve().exists():
        raise FileExistsError("refusing to overwrite an existing acceptance receipt")

    with tempfile.TemporaryDirectory(prefix="dakota-rcc-acceptance-") as temp:
        root = Path(temp)
        rcc_binary, rcc_sha256 = _verify_rcc(
            source_env, task_root=root / "rcc-version", deadline=deadline
        )
        poetry = resolve_poetry(
            source_env,
            task_root=root / "poetry-version",
            deadline=deadline,
        )
        tool_home = source_env["ACTIONS_ACCEPTANCE_ROBOCORP_HOME"]
        tool_extra = {
            "ACTIONS_ACCEPTANCE_POETRY": str(poetry),
            "ACTIONS_ACCEPTANCE_ROBOCORP_HOME": tool_home,
            "ACTIONS_RUNTIME_RCC_BINARY": rcc_binary,
            "ROBOCORP_HOME": tool_home,
        }
        tool_env = child_environment(
            source_env, task_root=root / "source-inspection", extra=tool_extra
        )
        source_sha = source_revision(repo_root, env=tool_env, deadline=deadline)
        candidate_wheels = build_candidate_wheels(
            root, source_env=source_env, poetry=poetry, deadline=deadline
        )
        core_wheel = candidate_wheels.core
        helper_wheel = candidate_wheels.helper
        candidate_versions = candidate_version_fields(candidate_wheels)
        wheel_records = candidate_wheel_records(candidate_wheels)
        package_dir = root / "package"
        package_dir.mkdir()
        (package_dir / "package.yaml").write_text(
            f"""version: 0.1
spec-version: v2
name: dakota-rcc-acceptance
dependencies:
  conda-forge:
    - python=3.12.15
  pypi:
    - actions-core @ {core_wheel.path.as_uri()}
    - actions-http-helper @ {helper_wheel.path.as_uri()}
""",
            encoding="utf-8",
        )
        (package_dir / "action.py").write_text(
            "import json\n"
            "from importlib.metadata import version\n"
            "from actions import action\n"
            "from actions.server_integration import ManagedParameters\n\n"
            "@action\n"
            "def answer() -> str:\n"
            "    return json.dumps({\n"
            "        'result': 'dakota-rcc-local-acceptance',\n"
            "        'actions_core': version('actions-core'),\n"
            "        'actions_http_helper': version('actions-http-helper'),\n"
            "        'server_integration': ManagedParameters.__name__,\n"
            "    }, sort_keys=True)\n",
            encoding="utf-8",
        )

        runtime_root = root / "runtime-state"
        runtime_env = child_environment(
            source_env,
            task_root=runtime_root / "environment",
            extra={
                "ACTIONS_REAL_RCC_ARTIFACT_TEST": "1",
                "ACTIONS_RUNTIME_RCC_BINARY": rcc_binary,
                "ACTIONS_RUNTIME_RCC_TIMEOUT": str(
                    max(1, math.floor(deadline.remaining()))
                ),
                "ROBOCORP_HOME": str(runtime_root / "rcc-home"),
            },
        )
        os.environ.clear()
        os.environ.update(runtime_env)
        sys.path.insert(0, str(_source_root() / "src"))
        import requests

        from actions.server._models import ActionPackage, Run, RunStatus, load_db
        from actions.server._rcc_runtime_adapter import (
            RccRuntimeDescriptor,
            build_exec_command,
            new_receipt_path,
            read_receipt,
        )
        from actions.server._selftest import ActionServerProcess

        provider = None
        server = None
        server_handle = None
        server_popen = None
        server_tree = []
        server_exit_code = None
        server_reaped = False
        provider_reaped = False
        run_id = ""
        try:
            provider, provider_url = start_provider(
                rcc_binary,
                runtime_root,
                runtime_env,
                deadline=deadline,
            )
            runtime_env["ACTIONS_RUNTIME_RCC_PROVIDER"] = provider_url
            os.environ["ACTIONS_RUNTIME_RCC_PROVIDER"] = provider_url
            api_key = "synthetic-dakota-rcc-acceptance-key"
            datadir = runtime_root / "runtime-data"
            server = ActionServerProcess(datadir)
            server.start(
                timeout=deadline.remaining_int(),
                db_file="server.db",
                actions_sync=True,
                cwd=package_dir,
                min_processes=0,
                max_processes=1,
                additional_args=[
                    "--address=127.0.0.1",
                    "--api-key",
                    api_key,
                ],
                env=runtime_env,
                port=0,
            )
            server_handle = server.process
            server_popen = getattr(server_handle, "_proc", None)
            if server_popen is None:
                raise RuntimeError("Runtime CLI process handle was not created")
            server_tree = capture_process_tree(server_popen.pid)
            base_url = f"http://{server.host}:{server.port}"
            action_url = f"{base_url}/api/actions/dakota-rcc-acceptance/answer/run"
            unauthenticated = requests.post(
                action_url, json={}, timeout=deadline.remaining(cap=20)
            )
            if unauthenticated.status_code not in (401, 403):
                raise AssertionError(
                    "Runtime did not reject the unauthenticated synthetic Action"
                )

            response = requests.post(
                action_url,
                json={},
                headers={"Authorization": f"Bearer {api_key}"},
                timeout=deadline.remaining(),
            )
            if response.status_code >= 400:
                raise RuntimeError(
                    f"Action HTTP {response.status_code}: {response.text[:1000]}\n"
                    f"Runtime diagnostics:\n{server.get_stderr()[-12000:]}"
                )
            response.raise_for_status()
            expected_result = {
                "result": "dakota-rcc-local-acceptance",
                **candidate_versions,
                "server_integration": "ManagedParameters",
            }
            candidate_result = json.loads(response.json())
            if candidate_result != expected_result:
                raise AssertionError("Action returned an unexpected result")
            run_id = response.headers.get("x-action-server-run-id", "")
            if not run_id:
                raise AssertionError("Runtime did not return a run identity")

            detail = requests.get(
                f"{base_url}/api/runs/{run_id}",
                headers={"Authorization": f"Bearer {api_key}"},
                timeout=deadline.remaining(cap=20),
            )
            detail.raise_for_status()
            if detail.json().get("id") != run_id:
                raise AssertionError("SQLite-backed run lookup returned another run")
            known = {(item.pid, item.create_time()) for item in server_tree}
            for item in capture_process_tree(server_popen.pid):
                identity = (item.pid, item.create_time())
                if identity not in known:
                    known.add(identity)
                    server_tree.append(item)
        finally:
            try:
                try:
                    if server is not None:
                        if server_handle is None:
                            server_handle = getattr(server, "_process", None)
                            server_popen = getattr(server_handle, "_proc", None)
                        stop_runtime_server(server, server_popen, server_tree)
                finally:
                    if server_handle is not None and server_popen is not None:
                        wait_for_process_tree_reap(
                            server_tree, timeout_seconds=CLEANUP_GRACE_SECONDS
                        )
                        server_exit_code = server_handle.returncode
                        server_reaped = (
                            server_exit_code is not None
                            and not server_handle.is_alive()
                        )
                        if not server_reaped:
                            raise ProcessTreeCleanupError(
                                "Runtime CLI process was signalled but not reaped"
                            )
            finally:
                if provider is not None:
                    terminate_process_tree(provider)
                    provider_reaped = provider.poll() is not None
                    if not provider_reaped:
                        raise ProcessTreeCleanupError(
                            "RCC provider process was signalled but not reaped"
                        )

        db_path = datadir / "server.db"
        with load_db(db_path) as db:
            with db.connect():
                package = db.all(ActionPackage)[0]
                runtime = json.loads(package.env_json)["runtime"]
        digest = runtime["artifact_digest"]
        if not digest.startswith("sha256:"):
            raise AssertionError("Runtime did not persist the RCC Artifact digest")
        initial_receipts = sorted((datadir / "rcc-receipts").glob("*.json"))
        if not initial_receipts:
            raise AssertionError("Runtime produced no initial RCC process receipt")
        initial_receipt_path = initial_receipts[-1]
        rcc_receipt = read_receipt(initial_receipt_path, digest)
        if not rcc_receipt.get("leaseId"):
            raise AssertionError("initial RCC receipt has no process lease identity")

        # Reuse the exact provider origin after its owner has exited. The probe
        # only counts and rejects traffic; it cannot satisfy an artifact fetch.
        provider_port = urlsplit(provider_url).port
        if provider_port is None:
            raise AssertionError("RCC provider did not expose a TCP port")
        provider_probe = UnavailableProviderProbe("127.0.0.1", provider_port)
        if provider_probe.url != provider_url:
            provider_probe.close()
            raise AssertionError("unavailable probe did not bind the retired origin")
        warm_server = None
        warm_server_handle = None
        warm_server_popen = None
        warm_server_tree = []
        warm_server_exit_code = None
        warm_server_reaped = False
        warm_run_id = ""
        warm_response = None
        warm_candidate_result = None
        warm_receipt = None
        warm_receipt_path = None
        provider_probe_stopped = False
        lifecycle = None
        warm_failure_class = None
        provider_acquire_diagnostic = None
        provider_acquire_requests = []
        local_acquire_diagnostic = None
        local_acquire_requests = []
        lifecycle_requests = []
        warm_provider_requests = []
        provider_backed_exec = None
        provider_backed_exec_requests = []
        try:
            offline_env = dict(runtime_env)
            offline_env["ACTIONS_RUNTIME_RCC_PROVIDER"] = provider_probe.url
            os.environ["ACTIONS_RUNTIME_RCC_PROVIDER"] = provider_probe.url
            rcc_env = child_environment(
                source_env,
                task_root=root / "offline-lifecycle",
                extra={
                    "ACTIONS_RUNTIME_RCC_BINARY": rcc_binary,
                    "ROBOCORP_HOME": runtime_env["ROBOCORP_HOME"],
                },
            )
            descriptor = RccRuntimeDescriptor.from_dict({"runtime": runtime})
            if descriptor.provider_reference != provider_probe.url:
                raise AssertionError(
                    "persisted runtime lost its selected provider trust context"
                )
            initial_receipt_bytes = initial_receipt_path.read_bytes()
            child_marker = root / "provider-unavailable-child-ran"
            child_receipt_path = new_receipt_path(datadir)
            child_command = build_exec_command(
                Path(rcc_binary),
                descriptor,
                [
                    "python",
                    "-c",
                    "from pathlib import Path; Path("
                    + repr(str(child_marker))
                    + ").write_text('executed')",
                ],
                receipt_file=child_receipt_path,
            )
            # The initial Action already proved successful acquire and saved its
            # terminal receipt. The same configured provider now rejects trust
            # attachment requests before RCC is allowed to start this child.
            provider_probe.reset_requests()
            child_result = run_owned_process(
                child_command,
                timeout_seconds=deadline.remaining(cap=30),
                env=rcc_env,
            )
            provider_backed_exec_requests = provider_probe.reset_requests()
            child_receipt = None
            if child_receipt_path.is_file():
                try:
                    child_receipt = json.loads(
                        child_receipt_path.read_text(encoding="utf-8")
                    )
                except (OSError, json.JSONDecodeError):
                    child_receipt = None
            successful_child_receipt = (
                isinstance(child_receipt, dict)
                and child_receipt.get("status") == "completed"
                and type(child_receipt.get("exitCode")) is int  # noqa: E721 - reject bool
                and child_receipt.get("exitCode") == 0
            )
            expected_provenance_request = f"/{digest}/provenance.json"
            provider_arg_matches = (
                "--provider" in child_command
                and child_command[child_command.index("--provider") + 1]
                == descriptor.provider_reference
            )
            provider_backed_exec = {
                "exit_code": child_result.returncode,
                "provider_reference_matches_persisted": (
                    descriptor.provider_reference == provider_probe.url
                ),
                "provider_argument_matches_descriptor": provider_arg_matches,
                "provider_requests": provider_backed_exec_requests,
                "provenance_503_observed": any(
                    event.get("path") == expected_provenance_request
                    and event.get("status") == 503
                    for event in provider_backed_exec_requests
                ),
                "child_side_effect_observed": child_marker.exists(),
                "successful_child_receipt": successful_child_receipt,
                "initial_receipt_unchanged": (
                    initial_receipt_path.read_bytes() == initial_receipt_bytes
                ),
            }
            provider_backed_exec["passed"] = (
                child_result.returncode != 0
                and provider_arg_matches
                and provider_backed_exec["provenance_503_observed"] is True
                and provider_backed_exec["child_side_effect_observed"] is False
                and successful_child_receipt is False
                and provider_backed_exec["initial_receipt_unchanged"] is True
            )
            inspect = run_owned_process(
                [
                    rcc_binary,
                    "env",
                    "lifecycle",
                    "inspect",
                    "--artifact",
                    digest,
                    "--json",
                ],
                timeout_seconds=deadline.remaining(cap=15),
                env=rcc_env,
            )
            if inspect.returncode:
                raise RuntimeError("RCC could not inspect the materialized artifact")
            lifecycle = json.loads(inspect.stdout)
            if lifecycle.get("ready") is not True:
                raise AssertionError("local RCC artifact is not ready before warm run")
            lifecycle_requests = provider_probe.reset_requests()

            diagnostic_replacements = {
                str(root): "<acceptance-root>",
                str(runtime_root): "<runtime-root>",
                str(runtime_root / "rcc-home"): "<rcc-home>",
                provider_probe.url: "<provider-origin>",
                tool_home: "<tool-home>",
            }
            provider_acquire_diagnostic = run_acquire_diagnostic(
                rcc_binary,
                digest,
                provider=provider_probe.url,
                env=rcc_env,
                deadline=deadline,
                replacements=diagnostic_replacements,
            )
            provider_acquire_requests = provider_probe.reset_requests()
            local_acquire_diagnostic = run_acquire_diagnostic(
                rcc_binary,
                digest,
                provider=None,
                env=rcc_env,
                deadline=deadline,
                replacements=diagnostic_replacements,
            )
            local_acquire_requests = provider_probe.reset_requests()
            if (
                local_acquire_diagnostic["exit_code"] != 0
                or local_acquire_diagnostic["exact_digest"] is not True
                or local_acquire_diagnostic["verification_valid"] is not True
            ):
                raise AssertionError(
                    "provider-free RCC acquire did not verify the exact artifact"
                )

            previous_receipts = {
                path.resolve() for path in (datadir / "rcc-receipts").glob("*.json")
            }
            warm_server = ActionServerProcess(datadir)
            warm_server.start(
                timeout=deadline.remaining_int(),
                db_file="server.db",
                actions_sync=True,
                cwd=package_dir,
                min_processes=0,
                max_processes=1,
                additional_args=["--address=127.0.0.1", "--api-key", api_key],
                env=offline_env,
                port=0,
            )
            warm_server_handle = warm_server.process
            warm_server_popen = getattr(warm_server_handle, "_proc", None)
            if warm_server_popen is None:
                raise RuntimeError("warm Runtime CLI process handle was not created")
            warm_server_tree = capture_process_tree(warm_server_popen.pid)
            warm_url = (
                f"http://{warm_server.host}:{warm_server.port}"
                "/api/actions/dakota-rcc-acceptance/answer/run"
            )
            warm_response = requests.post(
                warm_url,
                json={},
                headers={"Authorization": f"Bearer {api_key}"},
                timeout=deadline.remaining(),
            )
            if warm_response.status_code >= 400:
                raise RuntimeError(
                    f"offline warm Action HTTP {warm_response.status_code}: "
                    f"{warm_response.text[:1000]}\nRuntime diagnostics:\n"
                    f"{warm_server.get_stderr()[-12000:]}"
                )
            warm_response.raise_for_status()
            warm_candidate_result = json.loads(warm_response.json())
            if warm_candidate_result != expected_result:
                raise AssertionError("offline warm Action returned unexpected result")
            warm_run_id = warm_response.headers.get("x-action-server-run-id", "")
            if not warm_run_id:
                raise AssertionError("offline warm run has no identity")
            known = {(item.pid, item.create_time()) for item in warm_server_tree}
            for item in capture_process_tree(warm_server_popen.pid):
                identity = (item.pid, item.create_time())
                if identity not in known:
                    known.add(identity)
                    warm_server_tree.append(item)
        except Exception as exc:
            warm_failure_class = type(exc).__name__
        finally:
            try:
                try:
                    if warm_server is not None:
                        if warm_server_handle is None:
                            warm_server_handle = getattr(warm_server, "_process", None)
                            warm_server_popen = getattr(
                                warm_server_handle, "_proc", None
                            )
                        stop_runtime_server(
                            warm_server, warm_server_popen, warm_server_tree
                        )
                finally:
                    if warm_server_handle is not None and warm_server_popen is not None:
                        wait_for_process_tree_reap(
                            warm_server_tree, timeout_seconds=CLEANUP_GRACE_SECONDS
                        )
                        warm_server_exit_code = warm_server_handle.returncode
                        warm_server_reaped = (
                            warm_server_exit_code is not None
                            and not warm_server_handle.is_alive()
                        )
                        if not warm_server_reaped:
                            raise ProcessTreeCleanupError(
                                "warm Runtime CLI process was signalled but not reaped"
                            )
            finally:
                provider_probe.close()
                provider_probe_stopped = True
        warm_provider_requests = provider_probe.requests

        if warm_failure_class is not None:
            with load_db(db_path) as db:
                with db.connect():
                    run = next(
                        (item for item in db.all(Run) if item.id == run_id), None
                    )
                    package = db.all(ActionPackage)[0]
                    runtime = json.loads(package.env_json)["runtime"]
            cells = {
                "unauthenticated_rejection": "PASS"
                if unauthenticated.status_code in (401, 403)
                else "FAIL",
                "authenticated_action": "PASS"
                if response.status_code == 200 and candidate_result == expected_result
                else "FAIL",
                "sqlite_run": "PASS"
                if run is not None and run.status == RunStatus.PASSED
                else "FAIL",
                "artifact_verification": "PASS"
                if rcc_receipt.get("verification", {}).get("valid") is True
                else "FAIL",
                "provider_backed_exec_fail_closed": "PASS"
                if isinstance(provider_backed_exec, dict)
                and provider_backed_exec.get("passed") is True
                else "FAIL",
                "wrapper_exit": classify_wrapper_exit(rcc_receipt),
                "process_cleanup": "PASS"
                if server_reaped and provider_reaped and initial_receipts
                else "FAIL",
                "offline_warm_artifact_ready": "PASS"
                if isinstance(lifecycle, dict) and lifecycle.get("ready") is True
                else "FAIL",
                "offline_warm_action": "FAIL",
                "offline_warm_artifact_verification": "FAIL",
                "offline_warm_wrapper_exit": "FAIL",
                "provider_unavailable": "PASS" if provider_reaped else "FAIL",
                "zero_requests_during_warm_runtime": (
                    classify_zero_provider_requests(len(warm_provider_requests))
                ),
                "warm_process_cleanup": "PASS"
                if warm_server_reaped and provider_probe_stopped
                else "FAIL",
            }
            evidence = {
                "schema_version": 1,
                "runtime_mode": "candidate-wheel",
                "action_server_mode": "source",
                "source_sha": source_sha,
                "rcc": {
                    "version": runtime["rcc_version"],
                    "sha256": rcc_sha256,
                },
                "candidate_wheels": wheel_records,
                **candidate_versions,
                "server_integration": candidate_result["server_integration"],
                "artifact_digest": digest,
                "run_id": run_id,
                "sqlite_status": "passed"
                if cells["sqlite_run"] == "PASS"
                else "failed",
                "unauthenticated_http_status": unauthenticated.status_code,
                "authenticated_http_status": response.status_code,
                "provider": "rcc-cache-serve-loopback",
                "cells": cells,
                "acceptance_status": acceptance_status(cells),
                "runtime_process_exit_code": server_exit_code,
                "provider_process_reaped": provider_reaped,
                "rcc_receipt": _sanitize_runtime_receipt(rcc_receipt)
                if rcc_receipt
                else None,
                "offline_warm": {
                    "provider_reference": provider_probe.url,
                    "provider_probe_role": "count-and-reject-only; serves no artifacts",
                    "provider_probe_requests": provider_probe.request_count,
                    "provider_probe_request_events": provider_probe.requests,
                    "lifecycle_inspect_request_events": lifecycle_requests,
                    "provider_acquire": provider_acquire_diagnostic,
                    "provider_acquire_request_events": provider_acquire_requests,
                    "provider_backed_exec": provider_backed_exec,
                    "provider_free_acquire": local_acquire_diagnostic,
                    "provider_free_acquire_request_events": local_acquire_requests,
                    "warm_runtime_request_events": warm_provider_requests,
                    "warm_runtime_provider_requests": len(warm_provider_requests),
                    "artifact_lifecycle_inspect": lifecycle,
                    "failure_class": warm_failure_class,
                    "run_id": warm_run_id or None,
                    "runtime_process_exit_code": warm_server_exit_code,
                    "process_reaped": warm_server_reaped,
                    "provider_probe_stopped": provider_probe_stopped,
                    "rcc_receipt": None,
                },
            }
            write_evidence(receipt_path, evidence, temp_root=root)
            evidence["receipt_path"] = str(receipt_path.expanduser().resolve())
            return evidence

        with load_db(db_path) as db:
            with db.connect():
                warm_run = next(
                    (item for item in db.all(Run) if item.id == warm_run_id), None
                )
                if warm_run is None or warm_run.status != RunStatus.PASSED:
                    raise AssertionError("SQLite did not persist a passed warm run")
                package = db.all(ActionPackage)[0]
                warm_runtime = json.loads(package.env_json)["runtime"]
        if warm_runtime.get("artifact_digest") != digest:
            raise AssertionError("warm Runtime changed the persisted artifact digest")
        new_receipts = sorted(
            path
            for path in (datadir / "rcc-receipts").glob("*.json")
            if path.resolve() not in previous_receipts
        )
        if len(new_receipts) != 1:
            raise AssertionError("warm run did not produce exactly one new RCC receipt")
        warm_receipt_path = new_receipts[0]
        warm_receipt = read_receipt(warm_receipt_path, digest)

        db_path = datadir / "server.db"
        with load_db(db_path) as db:
            with db.connect():
                run = next((item for item in db.all(Run) if item.id == run_id), None)
                if run is None or run.status != RunStatus.PASSED:
                    raise AssertionError("SQLite did not persist a passed Action run")
                if json.loads(run.result or "null") != json.dumps(
                    expected_result, sort_keys=True
                ):
                    raise AssertionError("SQLite persisted an unexpected Action result")
                package = db.all(ActionPackage)[0]
                runtime = json.loads(package.env_json)["runtime"]

        digest = runtime["artifact_digest"]
        if not digest.startswith("sha256:"):
            raise AssertionError("Runtime did not persist the RCC Artifact digest")
        receipts = sorted((datadir / "rcc-receipts").glob("*.json"))
        cells = {
            "unauthenticated_rejection": "PASS"
            if unauthenticated.status_code in (401, 403)
            else "FAIL",
            "authenticated_action": "PASS"
            if response.status_code == 200 and candidate_result == expected_result
            else "FAIL",
            "sqlite_run": "PASS"
            if run is not None and run.status == RunStatus.PASSED
            else "FAIL",
            "artifact_verification": "PASS"
            if rcc_receipt["verification"].get("valid") is True
            else "FAIL",
            "provider_backed_exec_fail_closed": "PASS"
            if isinstance(provider_backed_exec, dict)
            and provider_backed_exec.get("passed") is True
            else "FAIL",
            "wrapper_exit": classify_wrapper_exit(rcc_receipt),
            "process_cleanup": "PASS"
            if server_reaped and provider_reaped and receipts
            else "FAIL",
            "offline_warm_artifact_ready": "PASS"
            if lifecycle.get("ready") is True
            else "FAIL",
            "offline_warm_action": "PASS"
            if warm_response is not None
            and warm_response.status_code == 200
            and warm_candidate_result == expected_result
            and warm_run is not None
            and warm_run.status == RunStatus.PASSED
            else "FAIL",
            "offline_warm_artifact_verification": "PASS"
            if warm_receipt["verification"].get("valid") is True
            else "FAIL",
            "offline_warm_wrapper_exit": classify_wrapper_exit(warm_receipt),
            "provider_unavailable": "PASS" if provider_reaped else "FAIL",
            "zero_requests_during_warm_runtime": classify_zero_provider_requests(
                len(warm_provider_requests)
            ),
            "warm_process_cleanup": "PASS"
            if warm_server_reaped and provider_probe_stopped
            else "FAIL",
        }

        evidence = {
            "schema_version": 1,
            "runtime_mode": "candidate-wheel",
            "action_server_mode": "source",
            "source_sha": source_sha,
            "rcc": {"version": runtime["rcc_version"], "sha256": rcc_sha256},
            "candidate_wheels": wheel_records,
            **candidate_versions,
            "server_integration": candidate_result["server_integration"],
            "artifact_digest": digest,
            "run_id": run_id,
            "sqlite_status": "passed",
            "unauthenticated_http_status": unauthenticated.status_code,
            "authenticated_http_status": response.status_code,
            "provider": "rcc-cache-serve-loopback",
            "cells": cells,
            "acceptance_status": acceptance_status(cells),
            "runtime_process_exit_code": server_exit_code,
            "provider_process_reaped": provider_reaped,
            "rcc_receipt": _sanitize_runtime_receipt(rcc_receipt),
            "offline_warm": {
                "provider_reference": provider_probe.url,
                "provider_probe_role": "count-and-reject-only; serves no artifacts",
                "provider_probe_requests": provider_probe.request_count,
                "provider_probe_request_events": provider_probe.requests,
                "lifecycle_inspect_request_events": lifecycle_requests,
                "provider_acquire": provider_acquire_diagnostic,
                "provider_acquire_request_events": provider_acquire_requests,
                "provider_backed_exec": provider_backed_exec,
                "provider_free_acquire": local_acquire_diagnostic,
                "provider_free_acquire_request_events": local_acquire_requests,
                "warm_runtime_request_events": warm_provider_requests,
                "warm_runtime_provider_requests": len(warm_provider_requests),
                "artifact_lifecycle_inspect": lifecycle,
                "run_id": warm_run_id,
                "runtime_process_exit_code": warm_server_exit_code,
                "process_reaped": warm_server_reaped,
                "rcc_receipt": _sanitize_runtime_receipt(warm_receipt),
                "provider_probe_stopped": provider_probe_stopped,
            },
        }
        write_evidence(receipt_path, evidence, temp_root=root)
        evidence["receipt_path"] = str(receipt_path.expanduser().resolve())
        return evidence


def require_supported_platform() -> None:
    if sys.platform != "linux":
        raise RuntimeError("Dakota RCC acceptance cleanup is verified on Linux only")


def _cli_platform_is_supported() -> bool:
    try:
        require_supported_platform()
    except RuntimeError as exc:
        print(str(exc), file=sys.stderr)
        return False
    return True


def main(argv: list[str] | None = None) -> int:
    if argv is None:
        argv = sys.argv[1:]
    if argv == ["--_supervisor"]:
        if not _cli_platform_is_supported():
            return 2
        return _supervisor_main()
    args = _parser().parse_args(argv)
    if args.mode != "candidate-wheel":
        raise AssertionError("unreachable unsupported mode")
    if not args._worker and args.receipt.expanduser().resolve().exists():
        print(
            "Dakota RCC acceptance failed: refusing to overwrite an existing "
            "acceptance receipt",
            file=sys.stderr,
        )
        return 1
    if not _cli_platform_is_supported():
        return 2
    if not args._worker:
        try:
            source_env = dict(os.environ)
            extra = {
                key: source_env[key]
                for key in (
                    "ACTIONS_RUNTIME_RCC_BINARY",
                    "ACTIONS_ACCEPTANCE_POETRY",
                    "ACTIONS_ACCEPTANCE_ROBOCORP_HOME",
                )
            }
            worker_env = child_environment(
                source_env,
                task_root=args.receipt.expanduser().parent / "cli-supervisor",
                extra=extra,
            )
            completed = run_owned_process(
                [
                    sys.executable,
                    str(Path(__file__).resolve()),
                    "--mode",
                    args.mode,
                    "--receipt",
                    str(args.receipt.expanduser().resolve()),
                    "--_worker",
                ],
                timeout_seconds=CLI_WATCHDOG_SECONDS,
                env=worker_env,
            )
        except ProcessTreeCleanupError as exc:
            try:
                record_supervisor_cleanup_failure(args.receipt, exc)
            except Exception as receipt_error:
                print(
                    "Unable to mark acceptance receipt as cleanup failure: "
                    f"{type(receipt_error).__name__}: {receipt_error}",
                    file=sys.stderr,
                )
            print(
                f"Dakota RCC watchdog failed: {type(exc).__name__}: {exc}",
                file=sys.stderr,
            )
            return 1
        except subprocess.TimeoutExpired:
            print("Dakota RCC watchdog failed: watchdog_timeout", file=sys.stderr)
            try:
                state = record_supervisor_deadline_failure(args.receipt)
            except Exception:
                state = "update_failed"
            print(
                f"Dakota RCC watchdog receipt: receipt_state={state}", file=sys.stderr
            )
            return 1
        except Exception as exc:
            print(
                f"Dakota RCC watchdog failed: {type(exc).__name__}: {exc}",
                file=sys.stderr,
            )
            return 1
        if completed.stdout:
            sys.stdout.write(completed.stdout)
        if completed.stderr:
            sys.stderr.write(completed.stderr)
        return completed.returncode
    try:
        result = _run(args.receipt)
    except Exception as exc:
        print(
            f"Dakota RCC acceptance failed: {type(exc).__name__}: {exc}",
            file=sys.stderr,
        )
        return 1
    print(json.dumps(result, sort_keys=True))
    return 0 if result["acceptance_status"] == "PASS" else 1


if __name__ == "__main__":
    raise SystemExit(main())
