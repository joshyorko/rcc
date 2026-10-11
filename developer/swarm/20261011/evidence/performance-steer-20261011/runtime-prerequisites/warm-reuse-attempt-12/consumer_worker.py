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
CONSUMER_HOME = HERE / "consumer-home"
FIXTURE = HERE / "fixture"
ACTION_SERVER = Path("/workspace/work/rcc-swarm/actions-performance-20261011/action_server")
RCC = Path("/workspace/work/rcc-swarm/ownedtools/rcc-v18.19.3")
PRODUCER_HOME = Path("/workspace/work/rcc-swarm/actions-performance-20261011/runtime-home-attempt-05")
PROVIDER_CONTENT = Path("/workspace/work/rcc-swarm/evidence/performance-steer-20261011/runtime-prerequisites/attempt-10/attempt-10/provider-content")
ARTIFACT = "sha256:ec3795182e7261439a8b9a9a9b7bd0834b7a7d65202067b17e5589e06f41b83e"
EVENTS = RUN_DIR / "consumer-events.jsonl"
COMMANDS = RUN_DIR / "rcc-commands"
PROVIDER = None
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
    global PROVIDER, POOL
    result = {"status": "started", "source": {}, "stages": [], "calls": [], "cleanup": {}}
    first_lease = None
    materialization_id = None
    provider_out_path = RUN_DIR / "provider.stdout.log"
    provider_err_path = RUN_DIR / "provider.stderr.log"
    provider_out = provider_out_path.open("w", encoding="utf-8")
    provider_err = provider_err_path.open("w", encoding="utf-8")
    provider_args = [str(RCC), "cache", "serve", "--root", str(PROVIDER_CONTENT), "--listen", "127.0.0.1:0", "--json"]
    try:
        home_before_any_rcc_command_or_actions_import = sorted(
            str(p.relative_to(CONSUMER_HOME)) for p in CONSUMER_HOME.rglob("*")
        )
        if home_before_any_rcc_command_or_actions_import:
            raise RuntimeError("fresh consumer home had entries before any RCC command/import: "
                               f"{home_before_any_rcc_command_or_actions_import[:20]}")
        result["consumer_home_before_any_rcc_command_or_actions_import"] = []

        env = os.environ.copy()
        env.update({"ROBOCORP_HOME": str(PRODUCER_HOME), "RCC_HOLOTREE_MODE": "private"})
        PROVIDER = subprocess.Popen(provider_args, env=env, stdout=provider_out, stderr=provider_err)
        provider_start_ns = time.perf_counter_ns()
        provider_info = wait_provider(PROVIDER, provider_out_path)
        result["provider"] = {"argv": provider_args, "pid": PROVIDER.pid,
                              "startup_wall_ns": time.perf_counter_ns() - provider_start_ns,
                              "startup": provider_info, "content_root": str(PROVIDER_CONTENT)}
        provider_url = provider_info["url"]
        emit("provider_started", **result["provider"])

        os.environ.update({"ROBOCORP_HOME": str(CONSUMER_HOME), "RCC_HOLOTREE_MODE": "private",
                           "TMPDIR": str(RUN_DIR / "tmp"), "ACTIONS_RUNTIME_RCC_BINARY": str(RCC)})
        (RUN_DIR / "tmp").mkdir(exist_ok=True)
        expected_version = "v18.19.3"
        from actions.server import _models as models
        from actions.server._database import Database
        from actions.server._models import Action, ActionPackage, Run, RunStatus, get_model_db_rules
        from actions.server._rcc_runtime_adapter import acquire_artifact, read_receipt, verify_rcc_version
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
        check = run_recording(str(RCC), "--version")
        if check[0] != 0 or check[1].strip() != expected_version:
            raise RuntimeError(f"stable3 identity check failed: status={check[0]} version={check[1]!r}")
        result["version_check"] = {"status": check[0], "stdout": check[1].strip(), "stderr": check[2].strip()}

        own_env_before = sorted(str(p.relative_to(CONSUMER_HOME)) for p in CONSUMER_HOME.rglob("*") if not p.is_symlink())
        artifact_state = {}
        for relative in ("artifacts/v1/content/objects", "artifacts/v1/materializations"):
            path = CONSUMER_HOME / relative
            files = []
            if path.exists():
                for child in path.rglob("*"):
                    if child.is_file() and not child.is_symlink():
                        files.append({"path": str(child.relative_to(CONSUMER_HOME)), "bytes": child.stat().st_size})
            artifact_state[relative] = {"exists": path.exists(), "regular_file_count": len(files), "files": files[:20]}
        materialization_root = CONSUMER_HOME / "artifacts/v1/materializations"
        lease_files = []
        if materialization_root.exists():
            for path in materialization_root.rglob("leases/*.json"):
                if path.is_file() and not path.is_symlink():
                    lease_files.append(str(path.relative_to(CONSUMER_HOME)))
        if any(state["regular_file_count"] for state in artifact_state.values()) or lease_files:
            raise RuntimeError(f"consumer home acquired artifact state before adapter acquire: {artifact_state}, leases={lease_files}")
        result["consumer_home_after_version_before_acquire"] = {
            "ordinary_settings_skeleton": own_env_before,
            "artifact_state": artifact_state,
            "lease_files": lease_files,
            "meaning": "RCC --version initialized ordinary settings/templates only; no CAS object, materialization, or lease existed",
        }
        acquire_start = time.perf_counter_ns()
        acquire_before = live_tree_snapshot()
        emit("stage_start", stage="explicit_acquire")
        acquired = acquire_artifact(ARTIFACT, RCC, provider=provider_url, runner=run_recording)
        acquire_wall = time.perf_counter_ns() - acquire_start
        acquire_after = live_tree_snapshot()
        acquired_path = Path(acquired["path"])
        if not acquired_path.resolve().is_relative_to(CONSUMER_HOME.resolve()):
            raise RuntimeError(f"acquired materialization escaped fresh consumer home: {acquired_path}")
        if acquired.get("artifactDigest") != ARTIFACT:
            raise RuntimeError("explicit consumer acquire returned the wrong artifact digest")
        if acquired.get("cacheHit") != "provider":
            raise RuntimeError(f"expected provider cache hit for fresh home, got {acquired.get('cacheHit')!r}")
        result["acquisition"] = {"wall_ns": acquire_wall, "result": acquired,
                                 "own_home_before": own_env_before,
                                 "materialization_path": str(acquired_path),
                                 "process_cpu_delta": stage_cpu(acquire_before, acquire_after)}
        emit("stage_end", stage="explicit_acquire", wall_ns=acquire_wall,
             artifact_digest=ARTIFACT, cache_hit=acquired.get("cacheHit"),
             materialization_id=acquired.get("materializationId"),
             path=str(acquired_path), verification=(acquired.get("verification") or {}).get("valid"))

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
        emit("stage_start", stage="pool_construct_and_prewarm")
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
        emit("stage_end", stage="pool_construct_and_prewarm", wall_ns=pool_wall,
             counts=pool_counts(POOL))
        materialization_id = acquired["materializationId"]
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
                code = handle.run_action(
                    Run(id=f"rcc-warm-{index + 1}", status=RunStatus.NOT_RUN,
                        action_id=action.id, start_time=datetime.datetime.now(datetime.timezone.utc).isoformat(),
                        run_time=None, inputs="{}", result=None, error_message=None,
                        relative_artifacts_dir="", numbered_id=index + 1),
                    package, action, input_json, run_dir, output_file, result_json,
                    {}, {}, True,
                )
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
        result["first_acquire_construct_action_wall_ns"] = acquire_wall + pool_wall + result["calls"][0]["wall_ns_including_obtain_and_return"]
        result["first_acquire_construct_action_stages"] = {"explicit_acquire_ns": acquire_wall,
                                                          "pool_constructor_prewarm_ns": pool_wall,
                                                          "first_action_ns": result["calls"][0]["wall_ns_including_obtain_and_return"]}
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
        if PROVIDER is not None:
            try:
                if PROVIDER.poll() is None:
                    PROVIDER.terminate()
                    try:
                        PROVIDER.wait(timeout=5)
                    except subprocess.TimeoutExpired:
                        PROVIDER.kill()
                        PROVIDER.wait(timeout=5)
                result["provider_cleanup"] = {"pid": PROVIDER.pid, "returncode": PROVIDER.poll()}
            except BaseException as exc:
                result["provider_cleanup"] = {"error": str(exc), "pid": PROVIDER.pid, "returncode": PROVIDER.poll()}
        provider_out.close()
        provider_err.close()
        write_json("consumer-result.json", result)
        emit("worker_finished", status=result.get("status"), provider_cleanup=result.get("provider_cleanup"))
    if result.get("status") == "passed_actions_and_warm_reuse" and result.get("cleanup", {}).get("acceptance") != "verified":
        result["status"] = "failed_cleanup"
        write_json("consumer-result.json", result)
    return 0 if result.get("status") == "passed_actions_and_warm_reuse" else 1


if __name__ == "__main__":
    raise SystemExit(main())
