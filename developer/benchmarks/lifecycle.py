#!/usr/bin/env python3
"""Run RCC environment-artifact lifecycle commands and retain raw receipts."""

import argparse
import hashlib
import json
import os
import platform
import re
import subprocess
import tempfile
import time
from pathlib import Path

FIXTURE_FILES = {
    "conda.yaml": "channels:\n  - conda-forge\ndependencies:\n  - python=3.11.17\n  - pyyaml=6.0.2\n",
    "task.py": "import yaml\nprint(yaml.__version__)\n",
    "robot.yaml": "tasks:\n  proof:\n    command: [python, task.py]\ncondaConfigFile: conda.yaml\n",
}


class BenchmarkFailure(RuntimeError):
    def __init__(self, report):
        super().__init__("required RCC lifecycle phase or correctness gate failed")
        self.report = report


def _context():
    filesystem = {"temp_root": tempfile.gettempdir(), "block_size": None}
    if hasattr(os, "statvfs"):
        filesystem["block_size"] = os.statvfs(tempfile.gettempdir()).f_bsize
    if platform.system() == "Linux":
        result = subprocess.run(["stat", "-f", "-c", "%T", tempfile.gettempdir()],
                                capture_output=True, text=True, check=False)
        filesystem["type"] = result.stdout.strip() if result.returncode == 0 else "unavailable"
    else:
        filesystem["type"] = "unavailable"
    return {"platform": platform.system().lower(), "platform_release": platform.release(),
            "architecture": platform.machine(), "python": platform.python_version(),
            "cpu_count": os.cpu_count(), "filesystem": filesystem}


def _fixture(root):
    root.mkdir(parents=True, exist_ok=True)
    for name, content in FIXTURE_FILES.items():
        (root / name).write_text(content, encoding="utf-8")
    digest = hashlib.sha256()
    for path in sorted(root.iterdir()):
        digest.update(path.name.encode() + b"\0" + path.read_bytes())
    return {"id": "python-package-v1", "kind": "python-package", "source_digest": digest.hexdigest(),
            "files": FIXTURE_FILES, "robot": str(root / "robot.yaml")}


def _scrub(value, root):
    return value.replace(str(root), "<run-root>") if isinstance(value, str) else value


def _record(phase, command, result, started, usage, scrub_root):
    if usage is None:
        cpu_ns, max_rss_bytes = None, None
    else:
        cpu_ns = int((usage.ru_utime + usage.ru_stime) * 1e9)
        max_rss_bytes = usage.ru_maxrss * (1024 if platform.system() == "Linux" else 1)
    return {"id": phase, "operation": _scrub(" ".join(command), scrub_root),
            "status": "measured" if result["returncode"] == 0 else "failed",
            "wall_ns": time.perf_counter_ns() - started,
            "cpu_ns": cpu_ns, "max_rss_bytes": max_rss_bytes,
            "resource_scope": "wait4-child" if usage is not None else "unavailable",
            "returncode": result["returncode"],
            "stdout": _scrub(result["stdout"], scrub_root), "stderr": _scrub(result["stderr"], scrub_root)}


class RCCRunner:
    def __init__(self, binary, home, scrub_root=None):
        self.binary = str(Path(binary).resolve())
        if not Path(self.binary).is_file() or not os.access(self.binary, os.X_OK):
            raise ValueError("--binary must name an executable RCC candidate")
        self.home = Path(home)
        self.scrub_root = scrub_root or self.home.parent
        self.run_env = {}

    def run(self, args, phase):
        command = [self.binary, *args]
        started = time.perf_counter_ns()
        with tempfile.TemporaryFile(mode="w+t", encoding="utf-8") as stdout, tempfile.TemporaryFile(
                mode="w+t", encoding="utf-8") as stderr:
            process = subprocess.Popen(command, env={**os.environ, **self.run_env, "ROBOCORP_HOME": str(self.home)},
                                       stdout=stdout, stderr=stderr, text=True)
            if hasattr(os, "wait4"):
                _, status, usage = os.wait4(process.pid, 0)
                process.returncode = os.waitstatus_to_exitcode(status)
            else:
                process.wait()
                usage = None
            stdout.seek(0)
            stderr.seek(0)
            result = {"returncode": process.returncode, "stdout": stdout.read().strip(),
                      "stderr": stderr.read().strip()}
        return _record(phase, command, result, started, usage, self.scrub_root)


def _inventory(root):
    """Record logical bytes and file sizes outside timed RCC phases."""
    sizes = []
    symlinks = 0
    if root.exists():
        for path in root.rglob("*"):
            if path.is_symlink():
                symlinks += 1
            elif path.is_file():
                sizes.append(path.stat().st_size)
    return {"files": len(sizes), "symlinks": symlinks, "logical_bytes": sum(sizes),
            "size_buckets": {"under_4k": sum(size < 4096 for size in sizes),
                             "4k_to_64k": sum(4096 <= size < 65536 for size in sizes),
                             "64k_and_over": sum(size >= 65536 for size in sizes)}}


def _resolved_packages(home):
    """Retain Conda package identity without leaking channel URLs or paths."""
    packages = []
    for path in sorted((home / "holotree").glob("*/conda-meta/*.json")):
        raw = path.read_bytes()
        metadata = json.loads(raw)
        packages.append({"name": metadata.get("name"), "version": metadata.get("version"),
                         "build": metadata.get("build"), "package_sha256": metadata.get("sha256"),
                         "metadata_sha256": hashlib.sha256(raw).hexdigest()})
    return {"status": "observed" if packages else "unavailable", "packages": packages}


def _json_output(record):
    try:
        return json.loads(record["stdout"])
    except (json.JSONDecodeError, TypeError):
        return {}


def run_benchmark(work_root, binary, repetitions=5, rcc_sha="unknown", runner_factory=RCCRunner):
    if repetitions < 1:
        raise ValueError("repetitions must be positive")
    if re.fullmatch(r"[0-9a-f]{40}", rcc_sha) is None:
        raise ValueError("--rcc-sha must be an exact 40-character commit SHA")
    runs = []
    with tempfile.TemporaryDirectory(prefix="rcc-lifecycle-", dir=work_root) as directory:
        base = Path(directory)
        fixture = _fixture(base / "fixture")
        producer_home, provider_root = base / "producer-home", base / "provider"
        producer = runner_factory(binary, producer_home, base)
        provider = subprocess.Popen([str(Path(binary).resolve()), "cache", "serve", "--root", str(provider_root), "--json"],
                                    env={**os.environ, "ROBOCORP_HOME": str(producer_home)}, stdout=subprocess.PIPE,
                                    stderr=subprocess.PIPE, text=True)
        provider_url = json.loads(provider.stdout.readline())["url"]
        try:
            provider_args = ["provider", "add", "office", "--type", "http", "--url", provider_url,
                             "--authorization-env", "RCC_TEST_PROVIDER_AUTHORIZATION", "--json"]
            env = {"RCC_TEST_PROVIDER_AUTHORIZATION": "Bearer robot-test"}
            producer.run_env = env
            producer.run(provider_args, "provider-profile")
            published = producer.run(["env", "publish", "--robot", fixture["robot"], "--provider", "office", "--json"], "publish")
            artifact = _json_output(published).get("artifactDigest", "")
            provider_inventory = _inventory(provider_root)
            resolved_packages = _resolved_packages(producer_home)
            pending = []
            for repetition in range(repetitions):
                consumer_home = base / f"consumer-home-{repetition}"
                consumer = runner_factory(binary, consumer_home, base)
                consumer.run_env = env
                consumer.run(provider_args, "provider-profile")
                phases = [published] if repetition == 0 else []
                trust = ["--trust-carrier", str(consumer_home / "artifacts" / "v1" / "trust"),
                         "--trust-carrier-type", "filesystem", "--permissive-local"]
                acquired = consumer.run(["env", "acquire", "--artifact", artifact, "--provider", "office", *trust, "--json"], "acquire")
                acquired_json = _json_output(acquired)
                materialization = consumer_home / "holotree" / acquired_json.get("materializationId", "unavailable")
                materialization_inventory = _inventory(materialization)
                verified = consumer.run(["env", "lifecycle", "verify", "--artifact", artifact, "--json"], "verify")
                phases += [acquired, verified]
                execution = consumer.run(["env", "exec", "--artifact", artifact, *trust, "--json", "--",
                                          "python", "-c", "import yaml; print(yaml.__version__)"], "startup")
                phases.append(execution)
                phases.append({"id": "import", "operation": "child import", "status": "unavailable",
                               "wall_ns": None, "reason": "env exec reports aggregate execution; child output/timing is not separated"})
                warm = consumer.run(["env", "acquire", "--artifact", artifact, "--provider", "office", *trust, "--json"], "warm")
                phases.append(warm)
                exec_json = _json_output(execution)
                warm_json = _json_output(warm)
                phases.append({"id": "lease", "operation": "leaseId from env exec receipt", "status": "observed",
                               "wall_ns": None, "evidence": {"leaseId": exec_json.get("leaseId")}})
                phases.append({"id": "gc", "operation": "RCC environment GC", "status": "unavailable", "wall_ns": None,
                               "reason": "no public environment artifact GC command"})
                run = {"repetition": repetition, "fixture_id": fixture["id"], "phases": phases,
                       "inventories": {"materialization": materialization_inventory},
                       "correctness_gates": {"artifact_digest": bool(artifact),
                                             "verification_receipt": acquired_json.get("verification", {}).get("valid") is True,
                                             "materialization_receipt": bool(acquired_json.get("materializationId")),
                                             "verified_ready": _json_output(verified).get("verified") is True
                                             and _json_output(verified).get("state") == "ready"
                                             and _json_output(verified).get("digest") == artifact,
                                             "exec_completed": exec_json.get("status") == "completed" and exec_json.get("exitCode") == 0,
                                             "artifact_identity_stable": all(_json_output(phase).get("artifactDigest") == artifact
                                                                             for phase in (acquired, execution, warm)),
                                             "materialization_identity_stable": all(receipt.get("materializationId") == acquired_json.get("materializationId")
                                                                                     for receipt in (exec_json, warm_json)),
                                             "clean_cache_provider": acquired_json.get("cacheHit") == "provider",
                                             "warm_cache_local_materialization": warm_json.get("cacheHit") == "local-materialization"}}
                pending.append((run, consumer, trust, acquired_json))
            provider.terminate()
            provider.wait(timeout=10)
            for run, consumer, trust, acquired_json in pending:
                provider_dead = consumer.run(["env", "acquire", "--artifact", artifact, "--provider", "office", *trust, "--json"], "provider-dead")
                run["phases"].append(provider_dead)
                provider_dead_json = _json_output(provider_dead)
                run["correctness_gates"]["provider_dead_cache_local_materialization"] = provider_dead_json.get("cacheHit") == "local-materialization"
                run["correctness_gates"]["provider_dead_identity_stable"] = (
                    provider_dead_json.get("artifactDigest") == artifact
                    and provider_dead_json.get("materializationId") == acquired_json.get("materializationId"))
                runs.append(run)
        finally:
            if provider.poll() is None:
                provider.terminate()
                provider.wait(timeout=10)
    candidate = {"rcc_sha": rcc_sha, "consumer_sha": "external-unavailable",
                 "binary": str(Path(binary).resolve()),
                 "binary_sha256": hashlib.sha256(Path(binary).read_bytes()).hexdigest(),
                 "harness_sha256": hashlib.sha256(Path(__file__).read_bytes()).hexdigest()}
    fixture["robot"] = "robot.yaml"
    report = build_report(candidate, [fixture], runs, _context())
    report["provider_inventory"] = provider_inventory
    report["resolved_packages"] = resolved_packages
    report["unavailable_evidence"] = {
        "phase_breakdown": "RCC CLI does not expose separate resolution, build, Lift, transfer, Drop, lease, and import clocks",
        "actions": "no external Actions package or worker pool is exercised",
        "source_reload": "no consumer reload fixture is exercised",
        "network_bytes": "provider transport byte accounting is not exposed by this harness",
        "profiles": "pprof and syscall traces are not captured",
        "antivirus": "antivirus product and scan activity are not observed",
        "platforms": "measurements apply only to the context platform and filesystem",
    }
    required = {"acquire", "verify", "startup", "warm", "provider-dead"}
    for run in runs:
        statuses = {phase["id"]: phase["status"] for phase in run["phases"]}
        required_for_run = required | ({"publish"} if run["repetition"] == 0 else set())
        if any(statuses.get(phase) != "measured" for phase in required_for_run) or not all(
                run["correctness_gates"].values()):
            raise BenchmarkFailure(report)
    return report


def build_report(candidate, fixtures, runs, context):
    return {"schema_version": 3, "benchmark": "rcc-environment-lifecycle-v3", "candidate": candidate,
            "fixtures": fixtures, "context": context, "runs": runs}


def source_sha():
    repository = Path(__file__).resolve().parents[2]
    result = subprocess.run(["git", "-C", str(repository), "rev-parse", "HEAD"],
                            capture_output=True, text=True, check=True)
    return result.stdout.strip()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--binary", required=True)
    parser.add_argument("--rcc-sha", help="exact candidate commit; defaults to this checkout's HEAD")
    parser.add_argument("--repetitions", type=int, default=5)
    parser.add_argument("--output", type=Path, default=Path("tmp/lifecycle-baseline.json"))
    args = parser.parse_args()
    try:
        report = run_benchmark(Path(tempfile.gettempdir()), args.binary, args.repetitions,
                               args.rcc_sha or source_sha())
    except BenchmarkFailure as error:
        report = error.report
        args.output.parent.mkdir(parents=True, exist_ok=True)
        args.output.write_text(json.dumps(report, indent=2, sort_keys=True) + "\n", encoding="utf-8")
        parser.exit(1, f"{error}\nraw evidence: {args.output}\n")
    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_text(json.dumps(report, indent=2, sort_keys=True) + "\n", encoding="utf-8")
    print(args.output)


if __name__ == "__main__":
    main()
