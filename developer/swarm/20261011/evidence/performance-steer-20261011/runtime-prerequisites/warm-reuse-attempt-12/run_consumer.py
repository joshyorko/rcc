"""Own-process watchdog for the one bounded stable3 consumer/reuse control."""
from __future__ import annotations

import hashlib
import json
import os
from pathlib import Path
import shutil
import signal
import stat
import subprocess
import sys
import time

import psutil

RUN_DIR = Path(__file__).resolve().parent
EVIDENCE_ROOT = RUN_DIR.parents[0]
CHECKOUT = Path("/workspace/work/rcc-swarm/actions-performance-20261011")
ACTION_SERVER = CHECKOUT / "action_server"
RCC = Path("/workspace/work/rcc-swarm/ownedtools/rcc-v18.19.3")
SOURCE_FIXTURE = Path("/workspace/work/rcc-swarm/evidence/performance-steer-20261011/runtime-prerequisites/attempt-10/attempt-10/pytest-basetemp/test_real_rcc_artifact_action_0")
PROVIDER_CONTENT = Path("/workspace/work/rcc-swarm/evidence/performance-steer-20261011/runtime-prerequisites/attempt-10/attempt-10/provider-content")
HARD_TOTAL_BYTES = 2576980377
STOP_TOTAL_BYTES = 2362232012
MINIMUM_FREE_BYTES = 2684354560
DISK_SCAN_SECONDS = 1.0
PROCESS_SAMPLE_SECONDS = 0.1
TOTAL_TIMEOUT_SECONDS = 300
STAGE_TIMEOUT_SECONDS = 180
PROVIDER_CONTENT_BYTES = 73516903
PRODUCER_MATERIALIZATION_BYTES = 172156889


def tree_size(path: Path) -> int:
    total = 0
    for root, dirs, files in os.walk(path, followlinks=False):
        dirs[:] = [d for d in dirs if not (Path(root) / d).is_symlink()]
        for name in files:
            item = Path(root) / name
            try:
                mode = item.lstat()
            except OSError:
                continue
            if stat.S_ISLNK(mode.st_mode):
                total += len(os.readlink(item).encode())
            elif stat.S_ISREG(mode.st_mode):
                total += mode.st_size
    return total


def process_snapshot(proc: psutil.Process) -> dict:
    processes = [proc]
    try:
        processes.extend(proc.children(recursive=True))
    except (psutil.NoSuchProcess, psutil.AccessDenied):
        pass
    rows = []
    total_rss = 0
    total_cpu_user = total_cpu_system = 0.0
    for item in processes:
        try:
            cpu = item.cpu_times()
            rss = item.memory_info().rss
            row = {"pid": item.pid, "create_time": item.create_time(),
                   "status": item.status(), "rss_bytes": rss,
                   "user_seconds": cpu.user, "system_seconds": cpu.system,
                   "cmdline": item.cmdline()}
            rows.append(row)
            total_rss += rss
            total_cpu_user += cpu.user
            total_cpu_system += cpu.system
        except (psutil.NoSuchProcess, psutil.AccessDenied):
            continue
    return {"processes": rows, "sum_sampled_rss_bytes": total_rss,
            "sum_user_seconds": total_cpu_user, "sum_system_seconds": total_cpu_system}


def emit_result(path: Path, result: dict) -> None:
    path.write_text(json.dumps(result, indent=2, sort_keys=True) + "\n", encoding="utf-8")


def inventory_fixture() -> dict:
    from shutil import copy2, copytree
    fixture = RUN_DIR / "fixture"
    fixture.mkdir(exist_ok=False)
    copy2(SOURCE_FIXTURE / "server.db", fixture / "server.db")
    copytree(SOURCE_FIXTURE / "package", fixture / "package")
    copytree(SOURCE_FIXTURE / "data", fixture / "data")
    package_hashes = {}
    content = bytearray()
    for name in ("action.py", "package.yaml"):
        value = (fixture / "package" / name).read_bytes()
        package_hashes[name] = hashlib.sha256(value).hexdigest()
        content.extend(name.encode("utf-8") + b"\0" + value)
    return {"copied_from": str(SOURCE_FIXTURE), "copy_root": str(fixture),
            "package_file_sha256": package_hashes,
            "fixture_inventory_sha256": hashlib.sha256(content).hexdigest(),
            "expected_fixture_inventory_sha256": "c9cfe7c5226005eef2fa83d7ecbe0f55920c72af24487fce624a7a61ae5e3ca5",
            "database_sha256": hashlib.sha256((fixture / "server.db").read_bytes()).hexdigest(),
            "database_bytes": (fixture / "server.db").stat().st_size,
            "data_tree_bytes": tree_size(fixture / "data")}


def main() -> int:
    RUN_DIR.mkdir(parents=True, exist_ok=True)
    if any((RUN_DIR / name).exists() for name in ("consumer-home", "fixture", "supervisor-result.json", "consumer-result.json")):
        raise RuntimeError("attempt-12 already contains run state; refuse to reuse or overwrite it")
    (RUN_DIR / "consumer-home").mkdir()
    (RUN_DIR / "tmp").mkdir()
    (RUN_DIR / "rcc-commands").mkdir()
    fixture = inventory_fixture()
    if fixture["fixture_inventory_sha256"] != fixture["expected_fixture_inventory_sha256"]:
        raise RuntimeError(f"copied fixture identity mismatch: {fixture}")
    (RUN_DIR / "fixture-inventory.json").write_text(json.dumps(fixture, indent=2, sort_keys=True) + "\n")

    provider_bytes = tree_size(PROVIDER_CONTENT)
    producer_mat_bytes = tree_size(Path("/workspace/work/rcc-swarm/actions-performance-20261011/runtime-home-attempt-05/holotree/c2fd1aa_5a1fac3_02c4107a"))
    rcc_bytes = RCC.stat().st_size
    fixed_total = tree_size(CHECKOUT) + tree_size(EVIDENCE_ROOT) + rcc_bytes
    dynamic_initial = tree_size(RUN_DIR)
    projected_consumer_growth = provider_bytes + producer_mat_bytes
    projected_total = fixed_total + projected_consumer_growth
    if projected_total >= STOP_TOTAL_BYTES:
        preflight = {"status": "not_started_insufficient_space", "fixed_total_bytes": fixed_total,
                     "provider_content_bytes": provider_bytes, "producer_materialization_bytes": producer_mat_bytes,
                     "projected_consumer_growth_bytes": projected_consumer_growth,
                     "projected_total_bytes": projected_total,
                     "stop_threshold_bytes": STOP_TOTAL_BYTES,
                     "margin_below_stop_bytes": STOP_TOTAL_BYTES - projected_total,
                     "fixture": fixture}
        emit_result(RUN_DIR / "preflight.json", preflight)
        return 2
    free = shutil.disk_usage(CHECKOUT).free
    if free <= MINIMUM_FREE_BYTES:
        preflight = {"status": "not_started_insufficient_free_space", "free_bytes": free,
                     "minimum_free_bytes": MINIMUM_FREE_BYTES, "fixed_total_bytes": fixed_total,
                     "fixture": fixture}
        emit_result(RUN_DIR / "preflight.json", preflight)
        return 2

    preflight = {"status": "admitted_by_preflight", "paths": {"checkout": str(CHECKOUT),
                 "complete_runtime_prerequisites_evidence": str(EVIDENCE_ROOT), "rcc_binary": str(RCC)},
                 "fixed_bytes_before_cold_acquire": {"checkout": tree_size(CHECKOUT),
                     "complete_runtime_prerequisites_evidence": tree_size(EVIDENCE_ROOT),
                     "rcc_binary": rcc_bytes, "combined": fixed_total},
                 "artifact_inventory_estimate": {"provider_object_store_bytes": provider_bytes,
                     "producer_materialization_bytes": producer_mat_bytes,
                     "estimated_consumer_growth_bytes": projected_consumer_growth},
                 "projected_total_bytes": projected_total,
                 "stop_threshold_bytes": STOP_TOTAL_BYTES,
                 "margin_below_stop_bytes": STOP_TOTAL_BYTES - projected_total,
                 "hard_total_bytes": HARD_TOTAL_BYTES,
                 "minimum_free_bytes": MINIMUM_FREE_BYTES,
                 "free_before_run_bytes": free,
                 "dynamic_run_tree_initial_bytes": dynamic_initial,
                 "fixture": fixture}
    emit_result(RUN_DIR / "preflight.json", preflight)

    env = os.environ.copy()
    env.update({"PYTHONPATH": str(ACTION_SERVER / "src"),
                "PYTHONUNBUFFERED": "1",
                "ROBOCORP_HOME": str(RUN_DIR / "consumer-home"),
                "RCC_HOLOTREE_MODE": "private",
                "TMPDIR": str(RUN_DIR / "tmp"),
                "ACTIONS_RUNTIME_RCC_BINARY": str(RCC)})
    worker_path = RUN_DIR / "consumer_worker.py"
    stdout_path = RUN_DIR / "worker.stdout.log"
    stderr_path = RUN_DIR / "worker.stderr.log"
    started_ns = time.monotonic_ns()
    worker = subprocess.Popen([sys.executable, str(worker_path)], cwd=ACTION_SERVER,
                              env=env, stdout=stdout_path.open("wb"),
                              stderr=stderr_path.open("wb"), start_new_session=True)
    root = psutil.Process(worker.pid)
    scans = []
    process_samples = []
    last_disk_scan = time.monotonic()
    start_time = time.monotonic()
    initial_fixed_total = fixed_total
    while worker.poll() is None:
        now = time.monotonic()
        try:
            process_samples.append({"monotonic_ns": time.monotonic_ns(), **process_snapshot(root)})
        except (psutil.NoSuchProcess, psutil.AccessDenied):
            pass
        if now - last_disk_scan >= DISK_SCAN_SECONDS:
            dyn = tree_size(RUN_DIR)
            total = initial_fixed_total + max(0, dyn - dynamic_initial)
            free_now = shutil.disk_usage(CHECKOUT).free
            scans.append({"monotonic_ns": time.monotonic_ns(), "dynamic_run_tree_bytes": dyn,
                          "estimated_full_owned_total_bytes": total, "filesystem_free_bytes": free_now})
            last_disk_scan = now
            if free_now <= MINIMUM_FREE_BYTES or total >= STOP_TOTAL_BYTES or total >= HARD_TOTAL_BYTES:
                reason = "minimum_free_space" if free_now <= MINIMUM_FREE_BYTES else "owned_disk_limit"
                try:
                    os.killpg(worker.pid, signal.SIGINT)
                except ProcessLookupError:
                    pass
                try:
                    worker.wait(timeout=10)
                except subprocess.TimeoutExpired:
                    try: os.killpg(worker.pid, signal.SIGTERM)
                    except ProcessLookupError: pass
                    try: worker.wait(timeout=5)
                    except subprocess.TimeoutExpired:
                        try: os.killpg(worker.pid, signal.SIGKILL)
                        except ProcessLookupError: pass
                        worker.wait()
                scans[-1]["stop_trigger"] = reason
                break
        elapsed = now - start_time
        if elapsed > TOTAL_TIMEOUT_SECONDS:
            try: os.killpg(worker.pid, signal.SIGINT)
            except ProcessLookupError: pass
            try: worker.wait(timeout=10)
            except subprocess.TimeoutExpired:
                try: os.killpg(worker.pid, signal.SIGTERM)
                except ProcessLookupError: pass
                try: worker.wait(timeout=5)
                except subprocess.TimeoutExpired:
                    try: os.killpg(worker.pid, signal.SIGKILL)
                    except ProcessLookupError: pass
                    worker.wait()
            scans.append({"monotonic_ns": time.monotonic_ns(), "stop_trigger": "whole_run_timeout"})
            break
        # Enforce the 180-second per acquire or warm-session budget from worker events.
        events_path = RUN_DIR / "consumer-events.jsonl"
        if events_path.exists():
            try:
                rows = [json.loads(line) for line in events_path.read_text().splitlines()]
                stage_start = next((r for r in reversed(rows) if r.get("event") == "stage_start"), None)
                warm_start = next((r for r in rows if r.get("event") == "warm_call_start"), None)
                cleanup_start = next((r for r in reversed(rows) if r.get("event") == "cleanup_start"), None)
                stage_expired = bool(stage_start and (time.monotonic_ns() - stage_start["monotonic_ns"]) > STAGE_TIMEOUT_SECONDS * 1e9 and
                                     not any(r.get("event") == "stage_end" and r.get("stage") == stage_start.get("stage") for r in rows))
                warm_expired = bool(warm_start and (time.monotonic_ns() - warm_start["monotonic_ns"]) > STAGE_TIMEOUT_SECONDS * 1e9 and
                                    not any(r.get("event") == "warm_session_end" for r in rows))
                cleanup_expired = bool(cleanup_start and (time.monotonic_ns() - cleanup_start["monotonic_ns"]) > 10 * 1e9 and
                                        not any(r.get("event") == "cleanup_end" for r in rows))
                if stage_expired or warm_expired or cleanup_expired:
                    try: os.killpg(worker.pid, signal.SIGINT)
                    except ProcessLookupError: pass
                    try: worker.wait(timeout=10)
                    except subprocess.TimeoutExpired:
                        try: os.killpg(worker.pid, signal.SIGTERM)
                        except ProcessLookupError: pass
                        try: worker.wait(timeout=5)
                        except subprocess.TimeoutExpired:
                            try: os.killpg(worker.pid, signal.SIGKILL)
                            except ProcessLookupError: pass
                            worker.wait()
                    scans.append({"monotonic_ns": time.monotonic_ns(),
                                  "stop_trigger": "stage_timeout" if stage_expired else
                                  "warm_session_timeout" if warm_expired else "cleanup_timeout"})
                    break
            except (OSError, json.JSONDecodeError):
                pass
        time.sleep(PROCESS_SAMPLE_SECONDS)

    status = worker.wait()
    final_checkout = tree_size(CHECKOUT)
    final_evidence = tree_size(EVIDENCE_ROOT)
    final_free = shutil.disk_usage(CHECKOUT).free
    elapsed_ns = time.monotonic_ns() - started_ns
    max_rss = max((p["sum_sampled_rss_bytes"] for p in process_samples), default=0)
    max_individual = max((max((item["rss_bytes"] for item in p["processes"]), default=0) for p in process_samples), default=0)
    result = {"status": status, "worker_pid": worker.pid,
              "worker_exit_code": status, "wall_ns_including_setup_and_cleanup": elapsed_ns,
              "limits": {"hard_total_bytes": HARD_TOTAL_BYTES, "stop_total_bytes": STOP_TOTAL_BYTES,
                         "minimum_free_bytes": MINIMUM_FREE_BYTES,
                         "disk_scan_interval_seconds": DISK_SCAN_SECONDS,
                         "process_sample_interval_seconds": PROCESS_SAMPLE_SECONDS,
                         "total_timeout_seconds": TOTAL_TIMEOUT_SECONDS,
                         "stage_timeout_seconds": STAGE_TIMEOUT_SECONDS},
              "preflight": preflight,
              "final_full_owned_bytes": {"checkout": final_checkout, "complete_runtime_prerequisites_evidence": final_evidence,
                                          "rcc_binary": RCC.stat().st_size,
                                          "combined": final_checkout + final_evidence + RCC.stat().st_size},
              "final_free_bytes": final_free,
              "disk_scans": scans,
              "max_actual_disk_scan_gap_ms": max(((b["monotonic_ns"] - a["monotonic_ns"]) / 1e6 for a, b in zip(scans, scans[1:])), default=None),
              "process_samples": process_samples,
              "max_sampled_process_tree_rss_sum_bytes": max_rss,
              "max_sampled_individual_process_rss_bytes": max_individual,
              "cpu_scope": "outer supervisor samples worker process and descendants, including loopback provider and RCC children when present; summed simultaneous sampled RSS may double-count shared pages",
              "stdout_path": str(stdout_path), "stderr_path": str(stderr_path),
              "worker_result_path": str(RUN_DIR / "consumer-result.json"),
              "events_path": str(RUN_DIR / "consumer-events.jsonl"),
              "source_mutation": False,
              "status_note": "one invocation only; no retries"}
    emit_result(RUN_DIR / "supervisor-result.json", result)
    return status


if __name__ == "__main__":
    raise SystemExit(main())
