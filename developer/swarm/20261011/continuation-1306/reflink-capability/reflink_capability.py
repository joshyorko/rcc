#!/usr/bin/env python3
"""Isolated, admitted Linux clone capability probe. No RCC/source mutation."""
import argparse
import errno
import fcntl
import gzip
import hashlib
import json
import os
from pathlib import Path
import platform
import resource
import signal
import stat
import subprocess
import sys
import time

BASE = Path(__file__).resolve().parent
PLAN = BASE / "plan.json"
FROZEN_PLAN_SHA256 = "d61052a7ec7597b43173e8383d5f32b688ee66bd62f596cc80acf4f24ba8a1ca"
FICLONE = 0x40049409  # Linux x86_64 _IOW(0x94, 9, int)
START = time.monotonic()


def digest(data):
    return hashlib.sha256(data).hexdigest()


def save(name, value):
    path = BASE / name
    if path.exists():
        raise RuntimeError(f"refusing overwrite: {path.name}")
    with path.open("x") as out:
        json.dump(value, out, indent=2, sort_keys=True)
        out.write("\n")


def allocated():
    return BASE.stat().st_blocks * 512 + sum(
        p.lstat().st_blocks * 512 for p in BASE.rglob("*")
    )


def free_bytes():
    s = os.statvfs(BASE)
    return s.f_bavail * s.f_frsize


def rss_tree(pid, seen=None):
    seen = set() if seen is None else seen
    if pid in seen:
        return 0
    seen.add(pid)
    try:
        text = Path(f"/proc/{pid}/status").read_text()
        rss = next(int(x.split()[1]) * 1024 for x in text.splitlines() if x.startswith("VmRSS:"))
        children = Path(f"/proc/{pid}/task/{pid}/children").read_text().split()
        return rss + sum(rss_tree(int(x), seen) for x in children)
    except (OSError, StopIteration, ValueError):
        return 0


def read_plan():
    raw = PLAN.read_bytes()
    assert digest(raw) == FROZEN_PLAN_SHA256, "plan changed"
    p = json.loads(raw)
    assert Path(p["namespace"]) == BASE
    return p


def original_receipt(path):
    s = path.lstat()
    assert stat.S_ISREG(s.st_mode) and not path.is_symlink()
    return {"size": s.st_size, "mode": stat.S_IMODE(s.st_mode),
            "mtime_ns": s.st_mtime_ns, "nlink": s.st_nlink,
            "sha256": digest(path.read_bytes())}


def file_receipt(path):
    s = path.lstat()
    assert stat.S_ISREG(s.st_mode) and not path.is_symlink()
    return {"name": path.name, "size": s.st_size, "mode": stat.S_IMODE(s.st_mode),
            "dev": s.st_dev, "ino": s.st_ino, "nlink": s.st_nlink,
            "allocated_accounting_bytes": s.st_blocks * 512,
            "sha256": digest(path.read_bytes())}


def worker():
    p = read_plan()
    limits = p["limits"]
    resource.setrlimit(resource.RLIMIT_AS, (limits["process_tree_rss_bytes_max"],) * 2)
    result = {"schema": "rcc-cow-capability-result-v1", "plan_sha256": FROZEN_PLAN_SHA256,
              "pid": os.getpid(), "operations": [], "assertions": [], "status": "ERROR"}
    result["stage_observer"] = {"peak_allocated_accounting_bytes": 0, "minimum_free_bytes": free_bytes(), "peak_worker_rss_bytes": 0}
    original = Path(p["fixture"]["original_path"])
    before = None
    expected = {}
    paths = {}

    def guard():
        alloc, free, rss = allocated(), free_bytes(), rss_tree(os.getpid())
        observer = result["stage_observer"]
        observer["peak_allocated_accounting_bytes"] = max(observer["peak_allocated_accounting_bytes"], alloc)
        observer["minimum_free_bytes"] = min(observer["minimum_free_bytes"], free)
        observer["peak_worker_rss_bytes"] = max(observer["peak_worker_rss_bytes"], rss)
        assert alloc <= limits["namespace_allocated_bytes_max"], "owned allocated cap"
        assert free >= limits["global_free_floor_bytes"], "global free floor"
        assert rss <= limits["process_tree_rss_bytes_max"], "worker RSS cap"
        assert time.monotonic() - START <= limits["whole_experiment_seconds_max"], "worker time cap"

    def verify_all(label):
        guard()
        receipts = [file_receipt(paths[name]) for name in expected]
        for r in receipts:
            assert r["size"] == len(expected[r["name"]])
            assert r["sha256"] == digest(expected[r["name"]]), f"unexpected bytes {r['name']}"
            assert r["nlink"] == 1
        assert len({(r["dev"], r["ino"]) for r in receipts}) == len(receipts)
        result["assertions"].append({"phase": label, "files": receipts})

    def mutate(name, offset, mask):
        path = paths[name]
        data = bytearray(expected[name])
        data[offset] ^= mask
        fd = os.open(path, os.O_RDWR | os.O_NOFOLLOW)
        try:
            assert os.pwrite(fd, data[offset:offset + 1], offset) == 1
            os.fsync(fd)
        finally:
            os.close(fd)
        expected[name] = bytes(data)
        verify_all(f"mutated {name} offset {offset} xor {mask}")
        assert original_receipt(original) == before, "original CAS changed"

    try:
        guard()
        assert sys.platform == "linux" and platform.machine() == "x86_64"
        frozen = Path(p["fixture"]["frozen_plan"])
        assert digest(frozen.read_bytes()) == p["fixture"]["frozen_plan_sha256"]
        manifest = Path(p["fixture"]["manifest_file"])
        assert digest(manifest.read_bytes()) == p["fixture"]["manifest_sha256"]
        index_raw = Path(p["fixture"]["index_file"]).read_bytes()
        desc = p["fixture"]["index_descriptor"]
        assert len(index_raw) == desc["size"] and digest(index_raw) == desc["digest"].split(":")[1]
        index = json.loads(index_raw)
        selected = p["fixture"]["selected"]
        # The frozen actual object index owns the descriptor, not the sampling plan.
        entries = index["entries"]
        matches = [x for x in entries if x["storedDigest"] == selected["storedDigest"]]
        assert len(matches) == 1
        for key in ("storedDigest", "storedSize", "legacyObjectId", "logicalSize", "encoding"):
            assert matches[0][key] == selected[key]
        before = original_receipt(original)
        result["original_pre"] = before
        assert before["sha256"] == selected["storedDigest"].split(":")[1]
        assert before["size"] == selected["storedSize"] <= limits["payload_bytes_per_file_max"]
        original_bytes = original.read_bytes()
        logical = gzip.decompress(original_bytes)
        assert len(logical) == selected["logicalSize"] and digest(logical) == selected["legacyObjectId"]
        del logical, index, index_raw, entries, matches
        scratch = BASE / "scratch"
        scratch.mkdir(mode=0o700)
        source = scratch / "source.bin"
        # Explicit userspace read/write; never ask the filesystem to clone CAS.
        source_fd = os.open(source, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
        original_fd = os.open(original, os.O_RDONLY | os.O_NOFOLLOW)
        try:
            while True:
                chunk = os.read(original_fd, 65536)
                if not chunk:
                    break
                at = 0
                while at < len(chunk):
                    at += os.write(source_fd, chunk[at:])
            os.fsync(source_fd)
        finally:
            os.close(original_fd)
            os.close(source_fd)
        paths["source.bin"] = source
        expected["source.bin"] = original_bytes
        verify_all("owned ordinary source copy")
        clone = scratch / "clone.bin"
        fd = os.open(clone, os.O_RDWR | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
        source_fd = os.open(source, os.O_RDONLY | os.O_NOFOLLOW)
        ioctl_result = {"operation": "fcntl.ioctl", "request": FICLONE, "source": "scratch/source.bin", "dest": "scratch/clone.bin"}
        try:
            ioctl_result["return"] = fcntl.ioctl(fd, FICLONE, source_fd)
            ioctl_result["errno"] = 0
            os.fsync(fd)
        except OSError as exc:
            ioctl_result.update({"return": None, "errno": exc.errno, "errno_name": errno.errorcode.get(exc.errno), "error": str(exc)})
        finally:
            os.close(source_fd)
            os.close(fd)
        result["operations"].append(ioctl_result)
        if ioctl_result["errno"]:
            assert ioctl_result["errno"] in (errno.EOPNOTSUPP, errno.EXDEV, errno.ENOTTY, errno.EINVAL), "unexpected clone error"
            result["status"] = "CLONE_UNAVAILABLE_ON_TESTED_PATH"
            result["scope"] = "Exact owned overlay path only; no ordinary-vs-clone comparison, mutation, chmod or unlink controls performed. No automatic fallback."
        else:
            paths["clone.bin"] = clone
            expected["clone.bin"] = original_bytes
            for name, flag in [("ordinary.bin", "--reflink=never"), ("cpclone.bin", "--reflink=always")]:
                target = scratch / name
                assert not target.exists()
                argv = ["/usr/bin/cp", flag, "--sparse=never", "--", str(source), str(target)]
                completed = subprocess.run(argv, text=True, capture_output=True, timeout=3)
                result["operations"].append({"argv": argv, "exit": completed.returncode, "stdout": completed.stdout, "stderr": completed.stderr})
                assert completed.returncode == 0, "cp control failed; no retry"
                paths[name] = target
                expected[name] = original_bytes
            verify_all("four independent inodes, identical bytes")
            for name, offset, mask in [("clone.bin", 0, 1), ("source.bin", 4096, 2), ("ordinary.bin", 8192, 4), ("cpclone.bin", 12288, 8)]:
                mutate(name, offset, mask)
            modes = {"source.bin": 0o640, "clone.bin": 0o400, "ordinary.bin": 0o600, "cpclone.bin": 0o440}
            for name, mode in modes.items():
                os.chmod(paths[name], mode, follow_symlinks=False)
                assert all(stat.S_IMODE(paths[n].stat().st_mode) == (modes[n] if list(modes).index(n) <= list(modes).index(name) else 0o600) for n in modes)
                verify_all(f"independent chmod {name} {oct(mode)}")
            result["effective_uid"] = os.geteuid()
            result["mode_scope"] = "Independent inode modes only; no unprivileged permission-denial claim."
            for name in ("clone.bin", "source.bin", "ordinary.bin", "cpclone.bin"):
                paths[name].unlink()
                del expected[name]
                verify_all(f"unlink owned {name}; remaining bytes unchanged")
            result["status"] = "CLONE_AND_OWNED_ISOLATION_CONTROLS_PASS"
            result["scope"] = "Linux primitive/tool capability on this filesystem only; no RCC implementation, performance or unique-extent-space claim."
        guard()
    except BaseException as exc:
        result["error"] = {"type": type(exc).__name__, "message": str(exc)}
        result["status"] = "ERROR"
    finally:
        if before is not None:
            result["original_post"] = original_receipt(original)
            result["original_pre_post_equal"] = result["original_post"] == before
            if not result["original_pre_post_equal"]:
                result["status"] = "ERROR"
        result["whole_worker_wall_seconds"] = time.monotonic() - START
        usage = resource.getrusage(resource.RUSAGE_SELF)
        result["worker_usage"] = {"user_seconds": usage.ru_utime, "system_seconds": usage.ru_stime, "maxrss_kib": usage.ru_maxrss}
        result["allocated_accounting_bytes_final"] = allocated()
        result["free_bytes_final"] = free_bytes()
        save("result.json", result)
    return 1 if result["status"] == "ERROR" else 0


def supervisor():
    p = read_plan()
    limits = p["limits"]
    assert not (BASE / "result.json").exists() and not (BASE / "scratch").exists(), "single execution only"
    assert free_bytes() >= limits["global_free_floor_bytes"]
    run = {"schema": "rcc-cow-capability-supervisor-v1", "plan_sha256": FROZEN_PLAN_SHA256,
           "started_monotonic": START, "rss_sampling_scope": "supervisor plus worker and observed named descendant PIDs; /proc children/status only", "samples": 0,
           "peak_process_tree_rss_bytes": 0, "peak_allocated_accounting_bytes": allocated(), "minimum_free_bytes": free_bytes(), "stop_reason": None}
    command = [sys.executable, str(Path(__file__).resolve()), "--worker"]
    run["argv"] = command
    with (BASE / "worker.stdout").open("x") as stdout, (BASE / "worker.stderr").open("x") as stderr:
        proc = subprocess.Popen(command, stdout=stdout, stderr=stderr, start_new_session=True, env={**os.environ, "PYTHONDONTWRITEBYTECODE": "1"})
        while True:
            seen = set()
            rss = rss_tree(os.getpid(), seen)
            alloc = allocated()
            free = free_bytes()
            run["samples"] += 1
            run["peak_process_tree_rss_bytes"] = max(run["peak_process_tree_rss_bytes"], rss)
            run["peak_allocated_accounting_bytes"] = max(run["peak_allocated_accounting_bytes"], alloc)
            run["minimum_free_bytes"] = min(run["minimum_free_bytes"], free)
            elapsed = time.monotonic() - START
            reason = next((name for hit, name in [(elapsed > limits["whole_experiment_seconds_max"], "30s wall guard"), (rss > limits["process_tree_rss_bytes_max"], "512MiB process-tree RSS guard"), (alloc > limits["namespace_allocated_bytes_max"], "4MiB owned allocation guard"), (free < limits["global_free_floor_bytes"], "2.5GiB free floor guard")] if hit), None)
            if reason:
                run["stop_reason"] = reason
                try:
                    os.killpg(proc.pid, signal.SIGTERM)
                except ProcessLookupError:
                    pass
                try:
                    proc.wait(timeout=0.5)
                except subprocess.TimeoutExpired:
                    os.killpg(proc.pid, signal.SIGKILL)
                break
            if proc.poll() is not None:
                break
            time.sleep(limits["observer_interval_seconds"])
        run["worker_exit"] = proc.wait()
    run["whole_supervisor_wall_seconds"] = time.monotonic() - START
    run["supervisor_exit"] = 0 if run["worker_exit"] == 0 and run["stop_reason"] is None else 1
    save("supervisor-result.json", run)
    print(json.dumps({"worker_exit": run["worker_exit"], "supervisor_exit": run["supervisor_exit"], "stop_reason": run["stop_reason"]}))
    return run["supervisor_exit"]


if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("--worker", action="store_true")
    parser.add_argument("--admitted", action="store_true", help="Set only after root explicit admission")
    args = parser.parse_args()
    if args.worker:
        raise SystemExit(worker())
    if not args.admitted:
        parser.error("explicit root admission required; --admitted does not grant authorization")
    raise SystemExit(supervisor())
