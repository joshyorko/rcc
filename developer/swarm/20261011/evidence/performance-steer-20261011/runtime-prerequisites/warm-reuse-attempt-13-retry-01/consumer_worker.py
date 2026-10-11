"""One disposable RCC v18.19.3 cold-acquire + sequential reuse measurement."""
from __future__ import annotations

import datetime
import hashlib
import json
import os
from pathlib import Path
import signal
import subprocess
import sys
import threading
import time
import traceback
from typing import Any

import psutil

HERE = Path(__file__).resolve().parent
RUN_DIR = HERE
CONSUMER_HOME = Path("/workspace/work/rcc-swarm/evidence/performance-steer-20261011/runtime-prerequisites/warm-reuse-attempt-12/consumer-home")
FIXTURE = HERE / "fixture"
ACTION_SERVER = Path("/workspace/work/rcc-swarm/actions-performance-20261011/action_server")
RCC = Path("/workspace/work/rcc-swarm/ownedtools/rcc-v18.19.3")
PRIOR_RESULT = Path("/workspace/work/rcc-swarm/evidence/performance-steer-20261011/runtime-prerequisites/warm-reuse-attempt-12/consumer-result.json")
ARTIFACT = "sha256:ec3795182e7261439a8b9a9a9b7bd0834b7a7d65202067b17e5589e06f41b83e"
EVENTS = RUN_DIR / "consumer-events.jsonl"
COMMANDS = RUN_DIR / "rcc-commands"
POOL = None
_events_lock = threading.Lock()


def emit(event: str, **data: Any) -> None:
    row = {"event": event, "monotonic_ns": time.monotonic_ns(), **data}
    with _events_lock, EVENTS.open("a", encoding="utf-8") as stream:
        stream.write(json.dumps(row, sort_keys=True) + "\n")


def sha256(data: bytes) -> str:
    return hashlib.sha256(data).hexdigest()


def run_recording(*argv: str):
    started = time.perf_counter_ns()
    result = subprocess.run(argv, text=True, capture_output=True, check=False)
    wall = time.perf_counter_ns() - started
    n = len(list(COMMANDS.glob("rcc-*.stdout.log"))) + 1
    stdout_path = COMMANDS / f"rcc-{n:02d}.stdout.log"
    stderr_path = COMMANDS / f"rcc-{n:02d}.stderr.log"
    stdout_bytes = result.stdout.encode("utf-8", "replace")
    stderr_bytes = result.stderr.encode("utf-8", "replace")
    stdout_path.write_bytes(stdout_bytes)
    stderr_path.write_bytes(stderr_bytes)
    emit("rcc_subprocess", argv=list(argv), status=result.returncode, wall_ns=wall,
         stdout={"path": str(stdout_path), "bytes": len(stdout_bytes), "sha256": sha256(stdout_bytes)},
         stderr={"path": str(stderr_path), "bytes": len(stderr_bytes), "sha256": sha256(stderr_bytes)})
    return result.returncode, result.stdout, result.stderr


def write_json(name: str, data: dict) -> None:
    (RUN_DIR / name).write_text(json.dumps(data, indent=2, sort_keys=True) + "\n", encoding="utf-8")


def wait_provider(proc: subprocess.Popen, stdout_path: Path, timeout: float = 15.0) -> dict:
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        if proc.poll() is not None:
            raise RuntimeError(f"provider exited during startup: {proc.returncode}")
        lines = stdout_path.read_text(encoding="utf-8").splitlines() if stdout_path.exists() else []
        if lines:
            return json.loads(lines[0])
        time.sleep(0.05)
    raise TimeoutError("loopback provider startup exceeded 15 seconds")


def descendants(pid: int) -> list[dict]:
    try:
        parent = psutil.Process(pid)
        children = parent.children(recursive=True)
    except (psutil.NoSuchProcess, psutil.AccessDenied):
        return []
    found = []
    for proc in children:
        try:
            cmd = proc.cmdline()
            if any("preload_actions_server_main" in arg for arg in cmd):
                found.append({"pid": proc.pid, "create_time": proc.create_time(), "cmdline": cmd})
        except (psutil.NoSuchProcess, psutil.AccessDenied):
            continue
    return found


def process_identity(handle) -> dict:
    wrapper = psutil.Process(handle.pid)
    process = handle._rcc_wrapper.process
    return {
        "handle_object_id": id(handle),
        "handle_key": str(handle.key),
        "generation": getattr(POOL, "generation", None),
        "wrapper_pid": handle.pid,
        "wrapper_create_time": wrapper.create_time(),
        "wrapper_args": process.args,
        "wrapper_returncode": process.poll(),
        "worker_processes": descendants(handle.pid),
        "handle_alive": handle.is_alive(),
    }


def lease_snapshot(materialization_id: str, wrapper_pid: int) -> dict:
    digest_hex = ARTIFACT.removeprefix("sha256:")
    lease_dir = CONSUMER_HOME / "artifacts" / "v1" / "materializations" / digest_hex / "leases"
    entries = []
    if lease_dir.is_dir():
        for path in sorted(lease_dir.glob("*.json")):
            if path.is_symlink() or not path.is_file():
                continue
            raw = path.read_bytes()
            record = json.loads(raw)
            entries.append({
                "path": str(path), "id": record.get("id"),
                "materializationId": record.get("materializationId"),
                "artifactDigest": record.get("artifactDigest"),
                "ownerPid": record.get("ownerPid"), "ownerStart": record.get("ownerStart"),
                "sha256": sha256(raw), "bytes": len(raw),
            })
    matching = [entry for entry in entries if entry["ownerPid"] == wrapper_pid and
                entry["artifactDigest"] == ARTIFACT and entry["materializationId"] == materialization_id]
    return {"directory": str(lease_dir), "all_regular_entries": entries, "matching_owner_entries": matching}


def pool_counts(pool) -> dict:
    return {
        "running": sum(len(v) for v in pool._running_processes.values()),
        "idle": sum(len(v) for v in pool._idle_processes.values()),
    }


def live_tree_snapshot() -> dict:
    root = psutil.Process(os.getpid())
    processes = [root]
    try:
        processes += root.children(recursive=True)
    except (psutil.NoSuchProcess, psutil.AccessDenied):
        pass
    found = {}
    for proc in processes:
        try:
            key = f"{proc.pid}:{proc.create_time():.6f}"
            cpu = proc.cpu_times()
            found[key] = {"pid": proc.pid, "create_time": proc.create_time(),
                          "user_seconds": cpu.user, "system_seconds": cpu.system,
                          "rss_bytes": proc.memory_info().rss}
        except (psutil.NoSuchProcess, psutil.AccessDenied):
            pass
    return found


def stage_cpu(before: dict, after: dict) -> dict:
    user = system = 0.0
    changed = []
    for key, now in after.items():
        old = before.get(key)
        if old:
            user += max(0.0, now["user_seconds"] - old["user_seconds"])
            system += max(0.0, now["system_seconds"] - old["system_seconds"])
            changed.append(key)
        else:
            user += now["user_seconds"]
            system += now["system_seconds"]
            changed.append(key)
    return {"surviving_or_new_processes_user_seconds_delta": user,
            "surviving_or_new_processes_system_seconds_delta": system,
            "process_keys_in_after_snapshot": changed,
            "scope": "processes still present at stage end plus new processes; exited child CPU is omitted"}


def main() -> int:
    global POOL
    result = {"status": "started", "source": {}, "stages": [], "calls": [], "cleanup": {}}
    first_lease = None
    materialization_id = None
    try:
        if not CONSUMER_HOME.is_dir():
            raise RuntimeError(f"prior acquired consumer home is missing: {CONSUMER_HOME}")
        own_env_before = sorted(str(p.relative_to(CONSUMER_HOME)) for p in CONSUMER_HOME.rglob("*") if not p.is_symlink())
        prior = json.loads(PRIOR_RESULT.read_text(encoding="utf-8"))
        if prior.get("acquisition", {}).get("result", {}).get("artifactDigest") != ARTIFACT:
            raise RuntimeError("preserved attempt-12 acquire does not identify the expected artifact")
        acquired = prior["acquisition"]["result"]
        materialization_id = acquired["materializationId"]
        acquired_path = Path(acquired["path"])
        if not acquired_path.is_dir() or acquired_path.resolve() != (CONSUMER_HOME / "holotree" / materialization_id).resolve():
            raise RuntimeError(f"attempt-12 materialization identity/path mismatch: {acquired_path}")
        if not acquired_path.resolve().is_relative_to(CONSUMER_HOME.resolve()):
            raise RuntimeError(f"acquired materialization escaped consumer home: {acquired_path}")
        if acquired.get("verification", {}).get("valid") is not True:
            raise RuntimeError("preserved attempt-12 materialization was not verified")
        result["warm_home_before_pool"] = {
            "path": str(CONSUMER_HOME), "state": "already_acquired_warm_home_from_attempt_12",
            "prior_acquire_wall_ns": prior["acquisition"]["wall_ns"],
            "prior_cacheHit": acquired.get("cacheHit"), "materializationId": materialization_id,
            "artifactDigest": acquired.get("artifactDigest"), "verification": acquired.get("verification"),
            "ordinary_home_entries": own_env_before,
        }
        os.environ.update({"ROBOCORP_HOME": str(CONSUMER_HOME), "RCC_HOLOTREE_MODE": "private",
                           "TMPDIR": str(RUN_DIR / "tmp"), "ACTIONS_RUNTIME_RCC_BINARY": str(RCC)})
        (RUN_DIR / "tmp").mkdir(exist_ok=True)
        expected_version = "v18.19.3"
        from actions.server import _models as models
        from actions.server._database import Database
        from actions.server._models import Action, ActionPackage, Run, RunStatus
        from actions.server._rcc_runtime_adapter import read_receipt
        from actions.server._settings import Settings
        from actions.server._actions_process_pool import ActionsProcessPool

        adapter_path = ACTION_SERVER / "src/actions/server/_rcc_runtime_adapter.py"
        lock_path = ACTION_SERVER / "poetry.lock"
        result["source"] = {
            "actions_commit": subprocess.run(["git", "rev-parse", "HEAD"], cwd=ACTION_SERVER.parent, text=True, capture_output=True, check=True).stdout.strip(),
            "actions_tree": subprocess.run(["git", "rev-parse", "HEAD^{tree}"], cwd=ACTION_SERVER.parent, text=True, capture_output=True, check=True).stdout.strip(),
            "adapter_sha256": sha256(adapter_path.read_bytes()), "poetry_lock_sha256": sha256(lock_path.read_bytes()),
            "rcc_path": str(RCC), "rcc_sha256": sha256(RCC.read_bytes()),
            "fixture_inventory_sha256": "c9cfe7c5226005eef2fa83d7ecbe0f55920c72af24487fce624a7a61ae5e3ca5",
            "artifact_digest": ARTIFACT,
        }
        result["version_check"] = prior.get("version_check")
        if result["version_check"] != {"status": 0, "stdout": expected_version, "stderr": prior.get("version_check", {}).get("stderr", "")}:
            if not result["version_check"] or result["version_check"].get("status") != 0 or result["version_check"].get("stdout") != expected_version:
                raise RuntimeError(f"preserved stable3 version check is not valid: {result['version_check']}")

        artifact_state = {}
        for relative in ("artifacts/v1/content/objects", "artifacts/v1/materializations"):
            path = CONSUMER_HOME / relative
            files = []
            if path.exists():
                for child in path.rglob("*"):
                    if child.is_file() and not child.is_symlink():
                        files.append({"path": str(child.relative_to(CONSUMER_HOME)), "bytes": child.stat().st_size})
            artifact_state[relative] = {"exists": path.exists(), "regular_file_count": len(files), "files": files[:10]}
        if not all(state["exists"] and state["regular_file_count"] > 0 for state in artifact_state.values()):
            raise RuntimeError(f"attempt-12 warm artifact state is incomplete: {artifact_state}")
        materialization_root = CONSUMER_HOME / "artifacts/v1/materializations"
        prior_lease_files = []
        if materialization_root.exists():
            for path in materialization_root.rglob("leases/*.json"):
                if path.is_file() and not path.is_symlink():
                    prior_lease_files.append(str(path.relative_to(CONSUMER_HOME)))
        result["home_state_before_pool"] = {"artifact_state": artifact_state, "lease_files": prior_lease_files,
                                             "meaning": "warm materialization and content CAS retained from attempt 12; no explicit acquire or RCC command is run in attempt 13"}

        db = Database(FIXTURE / "server.db")
        db.initialize([ActionPackage, Action, Run])
        with db.connect():
            package = db.all(ActionPackage)[0]
            action = db.all(Action)[0]
        package.directory = str(FIXTURE / "package")
        if action.action_package_id != package.id:
            raise RuntimeError("copied fixture action/package identity mismatch")
        descriptor = json.loads(package.env_json)["runtime"]
        if descriptor.get("artifact_digest") != ARTIFACT or descriptor.get("rcc_version") != expected_version:
            raise RuntimeError("copied database runtime descriptor does not match producer artifact/version")
        models._global_db = db
        settings = Settings(datadir=FIXTURE / "data", artifacts_dir=FIXTURE / "artifacts")
        settings.reuse_processes = True
        settings.min_processes = 1
        settings.max_processes = 1

        pool_start = time.perf_counter_ns()
        pool_before = live_tree_snapshot()
        emit("stage_start", stage="warm_pool_construct_and_prewarm")
        POOL = ActionsProcessPool(settings, {package.id: package}, [action])
        pool_wall = time.perf_counter_ns() - pool_start
        pool_after = live_tree_snapshot()
        result["pool"] = {"constructor_prewarm_wall_ns": pool_wall,
                          "settings": {"reuse_processes": True, "min_processes": 1, "max_processes": 1},
                          "runtime_descriptor": descriptor,
                          "copied_fixture": {"database": str(FIXTURE / "server.db"),
                                             "package_directory": str(FIXTURE / "package"),
                                             "data_directory": str(FIXTURE / "data"),
                                             "package_id": package.id, "action_id": action.id,
                                             "action_name": action.name},
                          "process_cpu_delta": stage_cpu(pool_before, pool_after),
                          "pool_counts_after_constructor": pool_counts(POOL)}
        if pool_counts(POOL) != {"running": 0, "idle": 1}:
            raise RuntimeError(f"unexpected pool after min1 prewarm: {pool_counts(POOL)}")
        emit("stage_end", stage="warm_pool_construct_and_prewarm", wall_ns=pool_wall,
             counts=pool_counts(POOL))
        first_handle = None
        first_identity = None
        first_lease = None
        call_total = 6
        session_start_ns = time.perf_counter_ns()
        for index in range(call_total):
            run_dir = RUN_DIR / "calls" / f"call-{index + 1:02d}"
            run_dir.mkdir(parents=True, exist_ok=False)
            input_json = run_dir / "input.json"
            result_json = run_dir / "result.json"
            output_file = run_dir / "output.txt"
            input_json.write_text("{}", encoding="utf-8")
            started = time.perf_counter_ns()
            before_cpu = live_tree_snapshot()
            emit("warm_call_start", call=index + 1)
            with POOL.obtain_process_for_action(action) as handle:
                identity = process_identity(handle)
                if first_handle is None:
                    first_handle = handle
                    first_identity = identity
                elif handle is not first_handle:
                    raise RuntimeError(f"call {index + 1} received a different ProcessHandle object")
                if identity["handle_key"] != first_identity["handle_key"] or identity["generation"] != first_identity["generation"]:
                    raise RuntimeError(f"call {index + 1} changed key/generation")
                if identity["wrapper_pid"] != first_identity["wrapper_pid"] or identity["wrapper_create_time"] != first_identity["wrapper_create_time"]:
                    raise RuntimeError(f"call {index + 1} changed RCC wrapper identity")
                if identity["worker_processes"] != first_identity["worker_processes"]:
                    raise RuntimeError(f"call {index + 1} changed Python worker identity")
                if not identity["worker_processes"] or not identity["handle_alive"] or identity["wrapper_returncode"] is not None:
                    raise RuntimeError(f"call {index + 1} worker is not live")
                counts_inside = pool_counts(POOL)
                if counts_inside != {"running": 1, "idle": 0}:
                    raise RuntimeError(f"call {index + 1} bad in-context pool counts {counts_inside}")
                lease = lease_snapshot(materialization_id, identity["wrapper_pid"])
                if len(lease["matching_owner_entries"]) != 1:
                    raise RuntimeError(f"call {index + 1} expected exactly one live owner lease: {lease}")
                if first_lease is None:
                    first_lease = lease["matching_owner_entries"][0]
                elif lease["matching_owner_entries"][0] != first_lease:
                    raise RuntimeError(f"call {index + 1} live lease changed: {lease}")
                action_started = time.perf_counter_ns()
                code = handle.run_action(
                    Run(id=f"rcc-warm-{index + 1}", status=RunStatus.NOT_RUN,
                        action_id=action.id, start_time=datetime.datetime.now(datetime.timezone.utc).isoformat(),
                        run_time=None, inputs="{}", result=None, error_message=None,
                        relative_artifacts_dir="", numbered_id=index + 1),
                    package, action, input_json, run_dir, output_file, result_json,
                    {}, {}, True,
                )
                run_action_wall_ns = time.perf_counter_ns() - action_started
                if code != 0:
                    raise RuntimeError(f"call {index + 1} action return code {code}")
                if not result_json.is_file():
                    raise RuntimeError(f"call {index + 1} did not write a new result file")
                body = json.loads(result_json.read_text(encoding="utf-8"))
                if body.get("result") != "rcc-v18.19.3":
                    raise RuntimeError(f"call {index + 1} returned unexpected result: {body}")
                identity_after = process_identity(handle)
                lease_after = lease_snapshot(materialization_id, identity["wrapper_pid"])
                if identity_after != identity:
                    raise RuntimeError(f"call {index + 1} worker identity changed during call")
                if lease_after["matching_owner_entries"] != [first_lease]:
                    raise RuntimeError(f"call {index + 1} lease changed during call")
            wall_ns = time.perf_counter_ns() - started
            after_cpu = live_tree_snapshot()
            counts_idle = pool_counts(POOL)
            if counts_idle != {"running": 0, "idle": 1}:
                raise RuntimeError(f"call {index + 1} failed to return one worker idle: {counts_idle}")
            if index < call_total - 1:
                idle_start = time.perf_counter_ns()
                time.sleep(0.5)
                idle_wall_ns = time.perf_counter_ns() - idle_start
                lease_idle = lease_snapshot(materialization_id, first_identity["wrapper_pid"])
                if lease_idle["matching_owner_entries"] != [first_lease]:
                    raise RuntimeError(f"call {index + 1} lease did not survive idle interval")
            else:
                idle_wall_ns = 0
                lease_idle = lease_snapshot(materialization_id, first_identity["wrapper_pid"])
            record = {"call": index + 1, "wall_ns_including_obtain_and_return": wall_ns,
                      "run_action_wall_ns_excluding_obtain_return": run_action_wall_ns,
                      "idle_interval_after_wall_ns": idle_wall_ns, "return_code": code,
                      "result": body, "result_file": str(result_json),
                      "result_file_sha256": sha256(result_json.read_bytes()),
                      "handle_identity": identity, "handle_identity_after": identity_after,
                      "pool_counts_inside": counts_inside, "pool_counts_after": counts_idle,
                      "lease_inside": lease, "lease_after_idle": lease_idle,
                      "process_cpu_delta": stage_cpu(before_cpu, after_cpu)}
            result["calls"].append(record)
            emit("warm_call_end", call=index + 1, wall_ns=wall_ns,
                 return_code=code, result=body, wrapper_pid=identity["wrapper_pid"],
                 worker_pid=identity["worker_processes"][0]["pid"],
                 lease_id=first_lease["id"], lease_sha256=first_lease["sha256"],
                 idle_interval_ns=idle_wall_ns)
        emit("warm_session_end", call_count=call_total, wall_ns=time.perf_counter_ns() - session_start_ns)
        result["warm_pool_construct_first_action_stages"] = {"pool_constructor_prewarm_ns": pool_wall,
                                                            "first_action_obtain_return_wall_ns": result["calls"][0]["wall_ns_including_obtain_and_return"],
                                                            "first_run_action_ns": result["calls"][0]["run_action_wall_ns_excluding_obtain_return"],
                                                            "sum_is_not_exact_end_to_end": True}
        result["live_lease_before_dispose"] = first_lease
        result["status"] = "passed_actions_and_warm_reuse"
    except BaseException as exc:
        result["status"] = "failed"
        result["error"] = {"type": type(exc).__name__, "message": str(exc), "traceback": traceback.format_exc()}
    finally:
        if POOL is not None:
            cleanup_start = time.perf_counter_ns()
            handles = []
            try:
                for values in list(POOL._idle_processes.values()) + list(POOL._running_processes.values()):
                    handles.extend(list(values))
                for handle in handles:
                    try:
                        result["cleanup"].setdefault("pre_dispose_identities", []).append(process_identity(handle))
                    except BaseException as exc:
                        result["cleanup"].setdefault("identity_errors", []).append(str(exc))
                receipt_paths = [str(h._rcc_wrapper.receipt_file) for h in handles if getattr(h, "_rcc_wrapper", None)]
                result["cleanup"]["receipt_paths"] = receipt_paths
                emit("cleanup_start", wall_deadline_seconds=10)
                POOL.dispose()
                result["cleanup"]["pool_dispose_wall_ns"] = time.perf_counter_ns() - cleanup_start
                result["cleanup"]["wrapper_returncodes"] = [h._rcc_wrapper.process.poll() for h in handles]
                identities = []
                for item in result["cleanup"].get("pre_dispose_identities", []):
                    identities.append({"wrapper_pid": item["wrapper_pid"], "wrapper_create_time": item["wrapper_create_time"],
                                       "workers": item["worker_processes"]})
                time.sleep(0.5)
                result["cleanup"]["process_identities_after_dispose"] = identities
                result["cleanup"]["remaining_owned_identities"] = []
                for item in identities:
                    for proc_id in [{"pid": item["wrapper_pid"], "create_time": item["wrapper_create_time"]}] + item["workers"]:
                        try:
                            proc = psutil.Process(proc_id["pid"])
                            if abs(proc.create_time() - proc_id["create_time"]) < 0.01:
                                result["cleanup"]["remaining_owned_identities"].append(proc_id)
                        except psutil.NoSuchProcess:
                            pass
                for receipt_path in receipt_paths:
                    path = Path(receipt_path)
                    deadline = time.monotonic() + 10
                    while not path.exists() and time.monotonic() < deadline:
                        time.sleep(0.05)
                    if path.is_file():
                        from actions.server._rcc_runtime_adapter import read_receipt
                        receipt = read_receipt(path, ARTIFACT)
                        result["cleanup"].setdefault("receipts", []).append({
                            "path": str(path), "artifactDigest": receipt.get("artifactDigest"),
                            "leaseId": receipt.get("leaseId"), "status": receipt.get("status"),
                            "exitCode": receipt.get("exitCode"), "reason": receipt.get("reason"),
                            "verificationValid": (receipt.get("verification") or {}).get("valid"),
                            "sha256": sha256(path.read_bytes()),
                        })
                if first_lease is not None:
                    lease_path = Path(first_lease["path"])
                    result["cleanup"]["lease_exists_after_dispose"] = lease_path.exists()
                    result["cleanup"]["lease_path"] = str(lease_path)
                    receipts = result["cleanup"].get("receipts", [])
                    result["cleanup"]["receipt_lease_matches_live_lease"] = bool(
                        len(receipts) == 1 and receipts[0].get("leaseId") == first_lease.get("id")
                    )
                    result["cleanup"]["acceptance"] = (
                        "verified" if not result["cleanup"].get("remaining_owned_identities")
                        and not result["cleanup"].get("lease_exists_after_dispose")
                        and result["cleanup"].get("receipt_lease_matches_live_lease")
                        else "failed"
                    )
                emit("cleanup_end", acceptance=result["cleanup"]["acceptance"])
            except BaseException as exc:
                result["cleanup"]["error"] = {"type": type(exc).__name__, "message": str(exc), "traceback": traceback.format_exc()}
        write_json("consumer-result.json", result)
        emit("worker_finished", status=result.get("status"))
    if result.get("status") == "passed_actions_and_warm_reuse" and result.get("cleanup", {}).get("acceptance") != "verified":
        result["status"] = "failed_cleanup"
        write_json("consumer-result.json", result)
    return 0 if result.get("status") == "passed_actions_and_warm_reuse" else 1


if __name__ == "__main__":
    raise SystemExit(main())
