#!/usr/bin/env python3
"""Private trusted-task protocol driver; source preparation only, not run yet."""
from __future__ import annotations

import argparse
import hashlib
import importlib.util
import io
import json
import os
from pathlib import Path
import shutil
import stat
import subprocess
import sys
import tempfile
import time
import unittest

MAX_TREE_FILES = 10000
MAX_TREE_BYTES = 64 * 1024 * 1024
EXPECTED_TESTS = {"agentdocs": 5, "topology": 25}


def fail(message: str) -> None:
    raise RuntimeError(message)


def owned_dir(path: Path, mode: int = 0o700) -> None:
    st = path.lstat()
    if not stat.S_ISDIR(st.st_mode) or stat.S_IMODE(st.st_mode) != mode:
        fail(f"directory mode/type mismatch: {path} mode={oct(stat.S_IMODE(st.st_mode))}")
    if st.st_uid != os.geteuid() or st.st_gid != os.getegid():
        fail(f"directory owner mismatch: {path}")


def source_bytes_sha256(root: Path) -> str:
    root_st = root.lstat()
    if not stat.S_ISDIR(root_st.st_mode):
        fail(f"source root is not a real directory: {root}")
    if (root / ".git").exists():
        fail("source snapshot unexpectedly contains .git")
    digest = hashlib.sha256()
    count = 0
    total = 0

    def visit(path: Path) -> None:
        nonlocal count, total
        st = path.lstat()
        relative = path.relative_to(root).as_posix().encode("utf-8")
        digest.update(relative + b"\0")
        if stat.S_ISDIR(st.st_mode):
            digest.update(b"D\0")
            for child in sorted(path.iterdir(), key=lambda p: p.name):
                visit(child)
        elif stat.S_ISREG(st.st_mode):
            count += 1
            total += st.st_size
            if count > MAX_TREE_FILES or total > MAX_TREE_BYTES:
                fail("source tree exceeds bounded hash limits")
            digest.update(b"F\0")
            with path.open("rb") as stream:
                while True:
                    block = stream.read(1024 * 1024)
                    if not block:
                        break
                    digest.update(block)
        elif stat.S_ISLNK(st.st_mode):
            digest.update(b"L\0" + os.readlink(path).encode("utf-8") + b"\0")
        else:
            fail(f"unsupported source tree entry: {path}")

    for child in sorted(root.iterdir(), key=lambda p: p.name):
        visit(child)
    return digest.hexdigest()


def active_lease(lease_dir: Path, artifact: str) -> str:
    st = lease_dir.lstat()
    if not stat.S_ISDIR(st.st_mode) or stat.S_ISLNK(st.st_mode):
        fail(f"lease root is not a real directory: {lease_dir}")
    files = sorted(lease_dir.glob("*.json"))
    if len(files) != 1:
        fail(f"expected exactly one active lease, observed {len(files)}")
    file = files[0]
    fst = file.lstat()
    if not stat.S_ISREG(fst.st_mode) or stat.S_ISLNK(fst.st_mode) or fst.st_size > 1024 * 1024:
        fail("active lease is not a bounded regular file")
    data = json.loads(file.read_text(encoding="utf-8"))
    lease_id = data.get("id")
    if not isinstance(lease_id, str) or lease_id != file.stem or data.get("artifactDigest") != artifact:
        fail("active lease identity/artifact mismatch")
    return lease_id


def project_modules(root: Path) -> set[str]:
    found = set()
    root = root.resolve()
    for name, module in tuple(sys.modules.items()):
        filename = getattr(module, "__file__", None)
        if not filename:
            continue
        try:
            Path(filename).resolve().relative_to(root)
        except (OSError, ValueError):
            continue
        found.add(name)
    return found


def state_snapshot(root: Path) -> dict:
    return {
        "cwd": os.getcwd(),
        "environment": dict(os.environ),
        "argv": list(sys.argv),
        "path": list(sys.path),
        "tempfileTempdir": tempfile.tempdir,
        "dontWriteBytecode": sys.dont_write_bytecode,
        "projectModules": sorted(project_modules(root)),
    }


def state_matches(expected: dict, root: Path) -> bool:
    return state_snapshot(root) == expected


def remove_project_modules(root: Path, baseline: set[str]) -> None:
    for name in project_modules(root) - baseline:
        sys.modules.pop(name, None)


class ReceiptResult(unittest.TestResult):
    def __init__(self, stream=None, descriptions=None, verbosity=None) -> None:
        super().__init__(stream, descriptions, verbosity)
        self.outcomes = []

    def addSuccess(self, test):
        super().addSuccess(test)
        self.outcomes.append({"id": test.id(), "status": "PASS"})

    def addFailure(self, test, err):
        super().addFailure(test, err)
        self.outcomes.append({"id": test.id(), "status": "FAIL", "errorType": err[0].__name__})

    def addError(self, test, err):
        super().addError(test, err)
        self.outcomes.append({"id": test.id(), "status": "ERROR", "errorType": err[0].__name__})

    def addSkip(self, test, reason):
        super().addSkip(test, reason)
        self.outcomes.append({"id": test.id(), "status": "SKIP", "reason": str(reason)})

    def addUnexpectedSuccess(self, test):
        super().addUnexpectedSuccess(test)
        self.outcomes.append({"id": test.id(), "status": "UNEXPECTED_SUCCESS"})


def tests_in(suite):
    for item in suite:
        if isinstance(item, unittest.TestSuite):
            yield from tests_in(item)
        else:
            yield item


def load_suite(job: str, source_root: Path):
    scripts = source_root / "scripts"
    if job == "agentdocs":
        test_path = scripts / "test_validate_agent_docs.py"
        module_name = f"_rcc_task_agentdocs_{os.getpid()}"
    elif job == "topology":
        test_path = scripts / "test_validate_release_topology.py"
        module_name = f"_rcc_task_topology_{os.getpid()}"
        sys.path.insert(0, str(scripts))
    else:
        fail(f"unsupported workload job: {job}")
    spec = importlib.util.spec_from_file_location(module_name, test_path)
    if spec is None or spec.loader is None:
        fail(f"cannot load maintained test module: {test_path}")
    module = importlib.util.module_from_spec(spec)
    sys.modules[module_name] = module
    spec.loader.exec_module(module)
    suite = unittest.defaultTestLoader.loadTestsFromModule(module)
    return module_name, module, suite


def write_task_log(log_root: Path, sequence: int, text: str) -> str:
    if len(text.encode("utf-8")) > 1024 * 1024:
        fail("unittest log exceeded 1 MiB bound")
    path = log_root / f"task-{sequence:02d}.log"
    flags = os.O_WRONLY | os.O_CREAT | os.O_EXCL
    if hasattr(os, "O_NOFOLLOW"):
        flags |= os.O_NOFOLLOW
    fd = os.open(path, flags, 0o600)
    with os.fdopen(fd, "w", encoding="utf-8") as stream:
        stream.write(text)
        stream.flush()
        os.fsync(stream.fileno())
    return path.name


def run_suite(job: str, source_root: Path, task_path: Path, sequence: int) -> dict:
    expected = EXPECTED_TESTS[job]
    old_state = state_snapshot(source_root)
    old_modules = set(old_state["projectModules"])
    before_hash = source_bytes_sha256(source_root)
    before_lease = active_lease(LEASE_DIR, ARTIFACT)
    buffer = io.StringIO()
    task_module = None
    suite = None
    result = None
    validation_errors = None
    status = "ERROR"
    restore_error = None
    driver_error = None
    task_tmp = None
    log_name = None
    try:
        task_path.mkdir(mode=0o700)
        owned_dir(task_path)
        task_tmp = task_path / "tmp"
        task_tmp.mkdir(mode=0o700)
        owned_dir(task_tmp)
        os.environ["TMPDIR"] = str(task_tmp)
        os.environ["PYTHONDONTWRITEBYTECODE"] = "1"
        sys.dont_write_bytecode = True
        tempfile.tempdir = None
        os.chdir(task_path)
        sys.argv[:] = [str(source_root / "scripts" / f"test_validate_{'agent_docs' if job == 'agentdocs' else 'release_topology'}.py")]
        module_name, task_module, suite = load_suite(job, source_root)
        tests = list(tests_in(suite))
        runner = unittest.TextTestRunner(stream=buffer, verbosity=2, resultclass=ReceiptResult)
        result = runner.run(suite)
        if len(tests) != expected or result.testsRun != expected:
            fail(f"unexpected test count for {job}: discovered={len(tests)} ran={result.testsRun} expected={expected}")
        if job == "agentdocs":
            validation_errors = task_module.validate_agent_docs.validate()
        status = "PASS" if result.wasSuccessful() and not result.skipped and not result.failures and not result.errors and (validation_errors in (None, [])) else "ERROR"
    except Exception as exc:
        driver_error = f"{type(exc).__name__}: {exc}"
        buffer.write(f"driver error: {driver_error}\n")
        status = "ERROR"
    finally:
        # Restore task-visible process state and remove only modules loaded from
        # the coordinator's immutable, owned source snapshot.
        try:
            os.chdir(old_state["cwd"])
            os.environ.clear()
            os.environ.update(old_state["environment"])
            sys.argv[:] = old_state["argv"]
            sys.path[:] = old_state["path"]
            tempfile.tempdir = old_state["tempfileTempdir"]
            sys.dont_write_bytecode = old_state["dontWriteBytecode"]
            remove_project_modules(source_root, old_modules)
            if task_path.exists():
                shutil.rmtree(task_path)
        except Exception as exc:
            restore_error = f"{type(exc).__name__}: {exc}"
        try:
            log_name = write_task_log(LOG_ROOT, sequence, buffer.getvalue())
        except Exception as exc:
            restore_error = restore_error or f"task log: {type(exc).__name__}: {exc}"

    after_hash = source_bytes_sha256(source_root)
    after_lease = active_lease(LEASE_DIR, ARTIFACT)
    clean = restore_error is None and state_matches(old_state, source_root) and not (task_path.exists())
    if before_hash != after_hash or before_lease != after_lease or not clean:
        status = "ERROR"
    outcomes = result.outcomes if result is not None else []
    row = {
        "sequence": sequence,
        "job": job,
        "pid": os.getpid(),
        "leaseId": before_lease,
        "status": status,
        "testsRun": result.testsRun if result is not None else 0,
        "testIds": outcomes,
        "failures": len(result.failures) if result is not None else 0,
        "errors": len(result.errors) if result is not None else (1 if status == "ERROR" else 0),
        "skips": len(result.skipped) if result is not None else 0,
        "validationErrors": validation_errors,
        "sourceBytesSha256Before": before_hash,
        "sourceBytesSha256After": after_hash,
        "leaseIdAfter": after_lease,
        "cleanState": clean,
        "taskLog": log_name,
        "cleanupError": restore_error,
        "error": driver_error,
    }
    return row


def poison_error(sequence: int, source_root: Path, task_root: Path, expected: dict) -> dict:
    initial_hash = source_bytes_sha256(source_root)
    lease_id = active_lease(LEASE_DIR, ARTIFACT)
    poison_cwd = task_root / f"poison-cwd-{sequence:02d}"
    poison_cwd.mkdir(mode=0o700)
    try:
        os.environ["RCC_POISON_MARKER"] = "mutated"
        sys.argv[:] = ["poisoned-argv"]
        sys.path.insert(0, str(source_root / "scripts"))
        tempfile.tempdir = str(poison_cwd)
        os.chdir(poison_cwd)
        module = type(sys)("representative_poison_canary")
        module.__file__ = str(source_root / "scripts" / "poison.py")
        sys.modules[module.__name__] = module
        raise RuntimeError("intentional poison control error")
    except RuntimeError as exc:
        expected_error = str(exc) == "intentional poison control error"
    finally:
        os.chdir(expected["cwd"])
        os.environ.clear()
        os.environ.update(expected["environment"])
        sys.argv[:] = expected["argv"]
        sys.path[:] = expected["path"]
        tempfile.tempdir = expected["tempfileTempdir"]
        sys.modules.pop("representative_poison_canary", None)
        shutil.rmtree(poison_cwd)
    after_hash = source_bytes_sha256(source_root)
    after_lease = active_lease(LEASE_DIR, ARTIFACT)
    clean = state_matches(expected, source_root)
    return {
        "sequence": sequence, "job": "poison-error", "pid": os.getpid(), "leaseId": lease_id,
        "status": "ERROR" if expected_error and clean and lease_id == after_lease and initial_hash == after_hash else "FAIL",
        "expectedError": expected_error, "sourceBytesSha256Before": initial_hash,
        "sourceBytesSha256After": after_hash, "leaseIdAfter": after_lease, "cleanState": clean,
    }


def proc_start_tick(pid: int) -> str:
    fields = Path(f"/proc/{pid}/stat").read_text(encoding="ascii")
    tail = fields[fields.rfind(")") + 2:].split()
    if len(tail) <= 19:
        fail(f"could not parse /proc/{pid}/stat start tick")
    return tail[19]


def cancel_ready(sequence: int, source_root: Path) -> None:
    lease_id = active_lease(LEASE_DIR, ARTIFACT)
    parent_pid = os.getpid()
    child = subprocess.Popen(
        [sys.executable, "-c", "import time; time.sleep(60)"],
        stdin=subprocess.DEVNULL, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL,
        close_fds=True,
    )
    row = {
        "sequence": sequence, "control": "cancel-ready", "pid": parent_pid,
        "start": proc_start_tick(parent_pid), "childPid": child.pid,
        "childStart": proc_start_tick(child.pid), "leaseId": lease_id,
        "processGroup": os.getpgid(child.pid),
    }
    sys.stdout.write(json.dumps(row, sort_keys=True, separators=(",", ":")) + "\n")
    sys.stdout.flush()
    time.sleep(60)


def parse_args():
    parser = argparse.ArgumentParser()
    parser.add_argument("mode", choices=("worker", "control"))
    parser.add_argument("input_root", type=Path)
    parser.add_argument("task_root", type=Path)
    parser.add_argument("lease_dir", type=Path)
    parser.add_argument("artifact")
    parser.add_argument("start_sequence", type=int)
    return parser.parse_args()


ARGS = parse_args()
INPUT_ROOT = ARGS.input_root
TASK_ROOT = ARGS.task_root
LEASE_DIR = ARGS.lease_dir
ARTIFACT = ARGS.artifact
START_SEQUENCE = ARGS.start_sequence
LOG_ROOT = TASK_ROOT / "logs"


def main() -> int:
    if not INPUT_ROOT.is_absolute() or not TASK_ROOT.is_absolute() or not LEASE_DIR.is_absolute():
        fail("all input/task/lease paths must be absolute")
    if not ARTIFACT.startswith("sha256:"):
        fail("artifact identity must be a sha256 digest")
    if not 1 <= START_SEQUENCE <= 6:
        fail("start_sequence must be in [1, 6]")
    if ARGS.mode == "control" and START_SEQUENCE != 1:
        fail("control mode must start at sequence 1")
    owned_dir(TASK_ROOT)
    # The admitted prototype's 0077 umask masks RCC's requested 0750 to 0700.
    owned_dir(LEASE_DIR, mode=0o700)
    if LOG_ROOT.exists():
        owned_dir(LOG_ROOT)
    else:
        LOG_ROOT.mkdir(mode=0o700)
        owned_dir(LOG_ROOT)
    INPUT_ROOT.lstat()
    sys.dont_write_bytecode = True
    initial_state = state_snapshot(INPUT_ROOT)
    if project_modules(INPUT_ROOT):
        fail("project modules were loaded before first task")
    next_sequence = START_SEQUENCE
    for raw in sys.stdin:
        try:
            request = json.loads(raw)
            if not isinstance(request, dict):
                fail("each protocol row must be a JSON object")
            sequence = request.get("sequence")
            job = request.get("job")
            if type(sequence) is not int or not 1 <= sequence <= 6:
                fail(f"task sequence must be an integer in [1, 6], got {sequence!r}")
            if sequence != next_sequence:
                fail(f"unexpected task sequence {sequence}, expected {next_sequence}")
            if ARGS.mode == "worker":
                expected_job = "agentdocs" if sequence % 2 else "topology"
                if job != expected_job:
                    fail(f"task order mismatch at {sequence}: got {job}, expected {expected_job}")
                row = run_suite(job, INPUT_ROOT, TASK_ROOT / f"task-{sequence:02d}", sequence)
                sys.stdout.write(json.dumps(row, sort_keys=True, separators=(",", ":")) + "\n")
                sys.stdout.flush()
                next_sequence += 1
                if row["status"] != "PASS":
                    return 1
            else:
                if job == "poison-error":
                    if sequence != 1:
                        fail("poison-error control must be sequence 1")
                    row = poison_error(sequence, INPUT_ROOT, TASK_ROOT, initial_state)
                elif job == "probe":
                    if sequence != 2:
                        fail("clean-state probe must be sequence 2")
                    before_hash = source_bytes_sha256(INPUT_ROOT)
                    before_lease = active_lease(LEASE_DIR, ARTIFACT)
                    clean = state_matches(initial_state, INPUT_ROOT)
                    after_hash = source_bytes_sha256(INPUT_ROOT)
                    after_lease = active_lease(LEASE_DIR, ARTIFACT)
                    clean = clean and before_hash == after_hash and before_lease == after_lease
                    row = {"sequence": sequence, "job": "probe", "pid": os.getpid(), "status": "PASS" if clean else "FAIL", "cleanState": clean, "leaseId": before_lease, "leaseIdAfter": after_lease, "sourceBytesSha256Before": before_hash, "sourceBytesSha256After": after_hash}
                elif job == "cancel":
                    if sequence != 1:
                        fail("cancel control must be sequence 1")
                    cancel_ready(sequence, INPUT_ROOT)
                    return 0
                else:
                    row = {"sequence": sequence, "job": job, "pid": os.getpid(), "status": "FAIL", "error": "unknown control job"}
                sys.stdout.write(json.dumps(row, sort_keys=True, separators=(",", ":")) + "\n")
                sys.stdout.flush()
                next_sequence += 1
                if job == "probe" and row["status"] != "PASS":
                    return 1
        except Exception as exc:
            row = {"sequence": next_sequence, "job": "protocol-error", "pid": os.getpid(), "status": "ERROR", "error": f"{type(exc).__name__}: {exc}"}
            sys.stdout.write(json.dumps(row, sort_keys=True, separators=(",", ":")) + "\n")
            sys.stdout.flush()
            if ARGS.mode == "worker":
                return 1
            next_sequence += 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
