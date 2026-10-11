"""Run the existing Actions a709 stable-3 real RCC fixture in isolation."""

from __future__ import annotations

import json
import hashlib
import os
import resource
import signal
import subprocess
import sys
import time
import atexit
import stat
import shutil
from pathlib import Path

import psutil


HERE = Path(__file__).resolve().parent
RUN_DIR = HERE / "attempt-10"
CHECKOUT = Path("/workspace/work/rcc-swarm/actions-performance-20261011")
ACTION_SERVER = CHECKOUT / "action_server"
RCC = Path("/workspace/work/rcc-swarm/ownedtools/rcc-v18.19.3")
PRIVATE_HOME = CHECKOUT / "runtime-home-attempt-05"
EVIDENCE = RUN_DIR
EVIDENCE_ROOT = HERE
PROVIDER_ROOT = EVIDENCE / "provider-content"
TMP_ROOT = EVIDENCE / "tmp"
PYTEST_TMP = EVIDENCE / "pytest-basetemp"
DISK_LIMIT_BYTES = 2576980377
DISK_STOP_THRESHOLD_BYTES = 2362232012
DISK_SCAN_INTERVAL_SECONDS = 1.0
PROCESS_SAMPLE_INTERVAL_SECONDS = 0.1
DISK_SCAN_OVERSHOOT_ALLOWANCE_BYTES = DISK_LIMIT_BYTES - DISK_STOP_THRESHOLD_BYTES
MINIMUM_FREE_BYTES = 2684354560
MAX_WALL_SECONDS = 900


def tree_size(path: Path) -> int:
    total = 0
    for directory, dirs, files in os.walk(path, followlinks=False):
        dirs[:] = [name for name in dirs if not (Path(directory) / name).is_symlink()]
        for name in files:
            try:
                info = (Path(directory) / name).lstat()
            except FileNotFoundError:
                continue
            if not stat.S_ISLNK(info.st_mode):
                total += info.st_size
    return total


class ProcessTreeSampler:
    def __init__(self, process: subprocess.Popen):
        self.root = psutil.Process(process.pid)
        self.observed: dict[int, dict] = {}
        self.max_tree_rss = 0
        self.max_individual_rss = 0
        self.sample_times_ns: list[int] = []

    def sample(self) -> None:
        self.sample_times_ns.append(time.monotonic_ns())
        current = [self.root]
        try:
            current.extend(self.root.children(recursive=True))
        except (psutil.NoSuchProcess, psutil.AccessDenied):
            pass
        tree_rss = 0
        for item in current:
            try:
                cpu = item.cpu_times()
                rss = item.memory_info().rss
                self.observed[item.pid] = {
                    "user_seconds_last_seen": cpu.user,
                    "system_seconds_last_seen": cpu.system,
                    "rss_bytes_last_seen": rss,
                }
                tree_rss += rss
                self.max_individual_rss = max(self.max_individual_rss, rss)
            except (psutil.NoSuchProcess, psutil.AccessDenied, ProcessLookupError):
                continue
        self.max_tree_rss = max(self.max_tree_rss, tree_rss)

    def summary(self) -> dict:
        return {
            "requested_sample_interval_ms": 100,
            "actual_sample_count": len(self.sample_times_ns),
            "actual_sample_gaps_ms": [
                round((later - earlier) / 1e6, 3)
                for earlier, later in zip(self.sample_times_ns, self.sample_times_ns[1:])
            ],
            "observed_processes": self.observed,
            "max_sampled_tree_rss_sum_bytes": self.max_tree_rss,
            "max_sampled_individual_rss_bytes": self.max_individual_rss,
            "sampled_cpu_seconds_sum_last_seen": {
                "user": sum(item["user_seconds_last_seen"] for item in self.observed.values()),
                "system": sum(item["system_seconds_last_seen"] for item in self.observed.values()),
            },
        }


def wait_provider_startup(proc: subprocess.Popen, stdout_path: Path, timeout: float = 15.0) -> dict:
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        if proc.poll() is not None:
            raise RuntimeError(f"provider exited before startup with status {proc.returncode}")
        try:
            content = stdout_path.read_text(encoding="utf-8")
        except FileNotFoundError:
            content = ""
        first_line = content.splitlines()[0] if content.splitlines() else ""
        if first_line:
            return json.loads(first_line)
        time.sleep(0.05)
    raise TimeoutError("provider did not emit a startup JSON line")


def main() -> int:
    RUN_DIR.mkdir(parents=True, exist_ok=False)
    for directory in (PRIVATE_HOME, PROVIDER_ROOT, TMP_ROOT, PYTEST_TMP):
        directory.mkdir(parents=True, exist_ok=True)

    binary_hash = subprocess.run(["sha256sum", str(RCC)], check=True, text=True, capture_output=True).stdout.split()[0]
    version_start = time.perf_counter_ns()
    version = subprocess.run([str(RCC), "--version"], text=True, capture_output=True, check=False)
    version_record = {
        "argv": [str(RCC), "--version"],
        "status": version.returncode,
        "stdout": version.stdout.strip(),
        "stderr": version.stderr.strip(),
        "wall_ns": time.perf_counter_ns() - version_start,
        "sha256": binary_hash,
    }
    (EVIDENCE / "stable3-version.json").write_text(
        json.dumps(version_record, indent=2, sort_keys=True) + "\n", encoding="utf-8"
    )
    if version.returncode or version.stdout.strip() != "v18.19.3":
        raise RuntimeError(f"stable RCC identity gate failed: {version_record}")

    provider_stdout = EVIDENCE / "provider.stdout.log"
    provider_stderr = EVIDENCE / "provider.stderr.log"
    provider_out = provider_stdout.open("w", encoding="utf-8")
    provider_err = provider_stderr.open("w", encoding="utf-8")
    base_env = os.environ.copy()
    base_env.update(
        {
            "ROBOCORP_HOME": str(PRIVATE_HOME),
            "RCC_HOLOTREE_MODE": "private",
            "TMPDIR": str(TMP_ROOT),
        }
    )
    provider_argv = [
        str(RCC), "cache", "serve", "--root", str(PROVIDER_ROOT),
        "--listen", "127.0.0.1:0", "--json",
    ]
    provider = subprocess.Popen(
        provider_argv,
        env=base_env,
        stdout=provider_out,
        stderr=provider_err,
        start_new_session=True,
    )
    provider_ps = psutil.Process(provider.pid)

    def stop_provider() -> None:
        cleanup = {"pid": provider.pid, "before_status": provider.poll(), "action": "none"}
        if provider.poll() is None:
            cleanup["action"] = "terminate"
            provider.terminate()
            try:
                provider.wait(timeout=5)
            except subprocess.TimeoutExpired:
                cleanup["action"] = "kill"
                provider.kill()
                try:
                    provider.wait(timeout=5)
                except subprocess.TimeoutExpired:
                    cleanup["action"] = "unreaped_after_bounded_kill_wait"
        cleanup["after_status"] = provider.poll()
        try:
            if provider_ps.is_running():
                cleanup["psutil_status"] = provider_ps.status()
            else:
                cleanup["psutil_status"] = "not-running"
        except (psutil.NoSuchProcess, psutil.AccessDenied):
            cleanup["psutil_status"] = "gone-or-inaccessible"
        try:
            (EVIDENCE / "provider-cleanup.json").write_text(
                json.dumps(cleanup, indent=2, sort_keys=True) + "\n", encoding="utf-8"
            )
        except OSError:
            pass
        provider_out.close()
        provider_err.close()

    atexit.register(stop_provider)
    provider_started_ns = time.perf_counter_ns()
    startup = wait_provider_startup(provider, provider_stdout)
    provider_startup_wall_ns = time.perf_counter_ns() - provider_started_ns
    provider_record = {
        "argv": provider_argv,
        "pid": provider.pid,
        "environment": {
            "ROBOCORP_HOME": str(PRIVATE_HOME),
            "RCC_HOLOTREE_MODE": "private",
            "TMPDIR": str(TMP_ROOT),
        },
        "startup": startup,
        "startup_wall_ns": provider_startup_wall_ns,
        "binary_sha256": binary_hash,
    }
    (EVIDENCE / "provider-startup.json").write_text(
        json.dumps(provider_record, indent=2, sort_keys=True) + "\n", encoding="utf-8"
    )

    provider_cpu_before = provider_ps.cpu_times()
    provider_rss_samples = [provider_ps.memory_info().rss]

    env = base_env.copy()
    env.update(
        {
            "ACTIONS_RUNTIME_RCC_BINARY": str(RCC),
            "ACTIONS_RUNTIME_RCC_PROVIDER": startup["url"],
            "ACTIONS_REAL_RCC_ARTIFACT_TEST": "1",
            "ACTIONS_RUNTIME_RCC_COMMANDS_DIR": str(EVIDENCE / "rcc-commands"),
            "ACTIONS_RUNTIME_PERF_EVENT_FILE": str(EVIDENCE / "events.jsonl"),
            "PYTHONPATH": os.pathsep.join(
                [str(ACTION_SERVER / "src"), str(HERE)]
                + ([os.environ["PYTHONPATH"]] if os.environ.get("PYTHONPATH") else [])
            ),
        }
    )
    argv = [
        str(ACTION_SERVER / ".venv/bin/python"), "-m", "pytest", "-q", "-s",
        "--basetemp", str(PYTEST_TMP),
        "-p", "runtime_probe_attempt10",
        "tests/action_server_tests/test_rcc_runtime_adapter.py::test_real_rcc_artifact_action_vertical",
    ]
    preexisting_home_cache = []
    cache_dir = PRIVATE_HOME / "pkgs" / "cache"
    if cache_dir.exists():
        for path in sorted(cache_dir.iterdir()):
            if path.is_file() and not path.is_symlink():
                preexisting_home_cache.append({
                    "relative_path": str(path.relative_to(PRIVATE_HOME)),
                    "size_bytes": path.stat().st_size,
                })
    command_record = {
        "argv": argv,
        "cwd": str(ACTION_SERVER),
        "environment": {
            key: env[key]
            for key in (
                "ROBOCORP_HOME", "RCC_HOLOTREE_MODE", "TMPDIR",
                "ACTIONS_RUNTIME_RCC_BINARY", "ACTIONS_RUNTIME_RCC_PROVIDER",
                "ACTIONS_REAL_RCC_ARTIFACT_TEST", "ACTIONS_RUNTIME_PERF_EVENT_FILE",
                "ACTIONS_RUNTIME_RCC_COMMANDS_DIR",
                "PYTHONPATH",
            )
        },
        "timeout_seconds": MAX_WALL_SECONDS,
        "checkout_and_evidence_disk_limit_bytes": DISK_LIMIT_BYTES,
        "checkout_and_evidence_disk_stop_threshold_bytes": DISK_STOP_THRESHOLD_BYTES,
        "disk_scan_interval_seconds": DISK_SCAN_INTERVAL_SECONDS,
        "disk_scan_overshoot_allowance_bytes": DISK_SCAN_OVERSHOOT_ALLOWANCE_BYTES,
        "process_sample_interval_seconds": PROCESS_SAMPLE_INTERVAL_SECONDS,
        "environment_preparation_mode": "recovery using attempt-05 private home and cached repodata/solver state; not a cold baseline",
        "preexisting_home_cache_files": preexisting_home_cache,
        "observer_sha256": hashlib.sha256((HERE / "runtime_probe_attempt10.py").read_bytes()).hexdigest(),
        "runner_sha256": hashlib.sha256(Path(__file__).read_bytes()).hexdigest(),
    }
    (EVIDENCE / "test-command.json").write_text(
        json.dumps(command_record, indent=2, sort_keys=True) + "\n", encoding="utf-8"
    )
    disk_scans = []
    dynamic_roots = (PRIVATE_HOME, EVIDENCE)

    def dynamic_size() -> int:
        return sum(tree_size(path) for path in dynamic_roots)

    def scan_disk(label: str) -> int:
        started = time.monotonic_ns()
        checkout_bytes = tree_size(CHECKOUT)
        evidence_bytes = tree_size(EVIDENCE_ROOT)
        dynamic_bytes = dynamic_size()
        finished = time.monotonic_ns()
        total = checkout_bytes + evidence_bytes
        disk_scans.append({
            "label": label,
            "filesystem_free_bytes": shutil.disk_usage(CHECKOUT).free,
            "monotonic_ns": finished,
            "scan_wall_ns": finished - started,
            "checkout_bytes": checkout_bytes,
            "evidence_bytes": evidence_bytes,
            "total_bytes": total,
            "active_dynamic_bytes": dynamic_bytes,
        })
        return total

    initial_total_bytes = scan_disk("initial")
    initial_free_bytes = disk_scans[-1]["filesystem_free_bytes"]
    if initial_free_bytes <= MINIMUM_FREE_BYTES:
        raise RuntimeError(f"initial filesystem free space is below minimum: {initial_free_bytes}")
    if initial_total_bytes >= DISK_STOP_THRESHOLD_BYTES:
        raise RuntimeError(f"initial checkout+evidence footprint exceeds stop threshold: {initial_total_bytes}")
    initial_dynamic_bytes = disk_scans[-1]["active_dynamic_bytes"]
    last_disk_scan_ns = time.monotonic_ns()
    usage_before = resource.getrusage(resource.RUSAGE_CHILDREN)
    test_log = (EVIDENCE / "pytest.stdout.log").open("w", encoding="utf-8")
    test_started_ns = time.perf_counter_ns()
    test = subprocess.Popen(
        argv,
        cwd=ACTION_SERVER,
        env=env,
        stdout=test_log,
        stderr=subprocess.STDOUT,
        start_new_session=True,
    )
    sampler = ProcessTreeSampler(test)
    timeout_hit = False
    disk_limit_hit = False
    free_space_limit_hit = False
    while test.poll() is None:
        elapsed = (time.perf_counter_ns() - test_started_ns) / 1e9
        sampler.sample()
        try:
            provider_rss_samples.append(provider_ps.memory_info().rss)
        except (psutil.NoSuchProcess, psutil.AccessDenied):
            pass
        now_ns = time.monotonic_ns()
        if (now_ns - last_disk_scan_ns) / 1e9 >= DISK_SCAN_INTERVAL_SECONDS:
            active_dynamic_bytes = dynamic_size()
            current_free_bytes = shutil.disk_usage(CHECKOUT).free
            estimated_total_bytes = initial_total_bytes + max(0, active_dynamic_bytes - initial_dynamic_bytes)
            disk_scans.append({
                "label": "periodic-dynamic-only",
                "monotonic_ns": time.monotonic_ns(),
                "active_dynamic_bytes": active_dynamic_bytes,
                "estimated_total_bytes": estimated_total_bytes,
                "filesystem_free_bytes": current_free_bytes,
            })
            last_disk_scan_ns = time.monotonic_ns()
            if current_free_bytes <= MINIMUM_FREE_BYTES:
                free_space_limit_hit = True
                os.killpg(test.pid, signal.SIGTERM)
                try:
                    test.wait(timeout=10)
                except subprocess.TimeoutExpired:
                    os.killpg(test.pid, signal.SIGKILL)
                    test.wait()
                break
            if estimated_total_bytes >= DISK_STOP_THRESHOLD_BYTES:
                disk_limit_hit = True
                os.killpg(test.pid, signal.SIGTERM)
                try:
                    test.wait(timeout=10)
                except subprocess.TimeoutExpired:
                    os.killpg(test.pid, signal.SIGKILL)
                    test.wait()
                break
        if elapsed > MAX_WALL_SECONDS:
            timeout_hit = True
            os.killpg(test.pid, signal.SIGTERM)
            try:
                test.wait(timeout=10)
            except subprocess.TimeoutExpired:
                os.killpg(test.pid, signal.SIGKILL)
                test.wait()
            break
        time.sleep(PROCESS_SAMPLE_INTERVAL_SECONDS)
    sampler.sample()
    status = test.wait()
    test_wall_ns = time.perf_counter_ns() - test_started_ns
    usage_after = resource.getrusage(resource.RUSAGE_CHILDREN)

    try:
        provider_cpu_after = provider_ps.cpu_times()
        provider_cpu = {
            "user_seconds_delta": provider_cpu_after.user - provider_cpu_before.user,
            "system_seconds_delta": provider_cpu_after.system - provider_cpu_before.system,
        }
        provider_max_rss = max(provider_rss_samples)
    except (psutil.NoSuchProcess, psutil.AccessDenied):
        provider_cpu = None
        provider_max_rss = max(provider_rss_samples)

    stop_provider()
    atexit.unregister(stop_provider)
    test_log.close()
    final_total_bytes = scan_disk("final")
    actual_disk_scan_gaps_ms = [
        round((later["monotonic_ns"] - earlier["monotonic_ns"]) / 1e6, 3)
        for earlier, later in zip(disk_scans, disk_scans[1:])
    ]

    result = {
        "source": {
            "commit": subprocess.run(["git", "rev-parse", "HEAD"], cwd=CHECKOUT, text=True, capture_output=True, check=True).stdout.strip(),
            "tree": subprocess.run(["git", "rev-parse", "HEAD^{tree}"], cwd=CHECKOUT, text=True, capture_output=True, check=True).stdout.strip(),
            "adapter_sha256": subprocess.run(["sha256sum", str(ACTION_SERVER / "src/actions/server/_rcc_runtime_adapter.py")], text=True, capture_output=True, check=True).stdout.split()[0],
            "poetry_lock_sha256": subprocess.run(["sha256sum", str(ACTION_SERVER / "poetry.lock")], text=True, capture_output=True, check=True).stdout.split()[0],
        },
        "rcc": version_record,
        "environment_preparation": {
            "mode": "recovery using attempt-05 private home and cached repodata/solver state; not a cold baseline",
            "preexisting_home_cache_files": preexisting_home_cache,
        },
        "provider": {
            **provider_record,
            "cpu_delta_process_scope": "provider process only",
            "cpu_delta": provider_cpu,
            "max_sampled_rss_bytes": provider_max_rss,
            "stderr_log": str(provider_stderr),
        },
        "test": {
            "argv": argv,
            "cwd": str(ACTION_SERVER),
            "status": status,
            "timeout_hit": timeout_hit,
            "disk_limit_hit": disk_limit_hit,
            "free_space_limit_hit": free_space_limit_hit,
            "minimum_free_bytes": MINIMUM_FREE_BYTES,
            "wall_ns_including_pytest_setup_and_teardown": test_wall_ns,
            "initial_checkout_and_evidence_bytes": initial_total_bytes,
            "final_checkout_and_evidence_bytes": final_total_bytes,
            "disk_limit_bytes": DISK_LIMIT_BYTES,
            "disk_stop_threshold_bytes": DISK_STOP_THRESHOLD_BYTES,
            "disk_scan_overshoot_allowance_bytes": DISK_SCAN_OVERSHOOT_ALLOWANCE_BYTES,
            "disk_scans": disk_scans,
            "actual_disk_scan_gaps_ms": actual_disk_scan_gaps_ms,
            "maximum_scan_gap_ms": max(actual_disk_scan_gaps_ms, default=0),
            "disk_limit_exceeded": final_total_bytes > DISK_LIMIT_BYTES,
            "wait4_children_user_seconds_delta": usage_after.ru_utime - usage_before.ru_utime,
            "wait4_children_system_seconds_delta": usage_after.ru_stime - usage_before.ru_stime,
            "wait4_children_maxrss_kib_highwater": usage_after.ru_maxrss,
            "pytest_process_tree_sample": sampler.summary(),
            "stdout_log": str(EVIDENCE / "pytest.stdout.log"),
            "event_log": str(EVIDENCE / "events.jsonl"),
        },
    }
    (EVIDENCE / "runtime-run.json").write_text(
        json.dumps(result, indent=2, sort_keys=True) + "\n", encoding="utf-8"
    )
    print(json.dumps(result, sort_keys=True))
    return status


if __name__ == "__main__":
    raise SystemExit(main())
