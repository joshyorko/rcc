#!/usr/bin/env python3
"""Run RCC environment-artifact lifecycle commands and retain raw receipts."""

import argparse
import hashlib
import json
import os
import platform
import resource
import subprocess
import tempfile
import time
from pathlib import Path

FIXTURES = ({"id": "python-package-v1", "kind": "python-package", "package": "pyyaml"},)


class BenchmarkFailure(RuntimeError):
    def __init__(self, report):
        super().__init__("required RCC lifecycle phase or correctness gate failed")
        self.report = report


def _context():
    stat = os.statvfs(tempfile.gettempdir())
    return {"platform": platform.system().lower(), "platform_release": platform.release(),
            "architecture": platform.machine(), "python": platform.python_version(),
            "filesystem": {"temp_root": tempfile.gettempdir(), "block_size": stat.f_bsize}}


def _fixture(root):
    root.mkdir(parents=True, exist_ok=True)
    (root / "conda.yaml").write_text("channels:\n  - conda-forge\ndependencies:\n  - python=3.11\n  - pyyaml=6.0.2\n", encoding="utf-8")
    (root / "task.py").write_text("import yaml\nprint(yaml.__version__)\n", encoding="utf-8")
    (root / "robot.yaml").write_text("tasks:\n  proof:\n    command: [python, task.py]\ncondaConfigFile: conda.yaml\n", encoding="utf-8")
    digest = hashlib.sha256()
    for path in sorted(root.iterdir()):
        digest.update(path.name.encode() + b"\0" + path.read_bytes())
    return {"id": "python-package-v1", "kind": "python-package", "source_digest": digest.hexdigest(),
            "robot": str(root / "robot.yaml")}


def _scrub(value, root):
    return value.replace(str(root), "<run-root>") if isinstance(value, str) else value


def _record(phase, command, result, started, usage_before, scrub_root):
    usage = resource.getrusage(resource.RUSAGE_CHILDREN)
    return {"id": phase, "operation": _scrub(" ".join(command), scrub_root),
            "status": "measured" if result["returncode"] == 0 else "failed",
            "wall_ns": time.perf_counter_ns() - started,
            "cpu_ns": int((usage.ru_utime - usage_before.ru_utime + usage.ru_stime - usage_before.ru_stime) * 1e9),
            "max_rss": usage.ru_maxrss, "returncode": result["returncode"],
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
        before = resource.getrusage(resource.RUSAGE_CHILDREN)
        process = subprocess.run(command, env={**os.environ, **self.run_env, "ROBOCORP_HOME": str(self.home)},
                                 text=True, capture_output=True, check=False)
        result = {"returncode": process.returncode, "stdout": process.stdout.strip(), "stderr": process.stderr.strip()}
        return _record(phase, command, result, started, before, self.scrub_root)


def _json_output(record):
    try:
        return json.loads(record["stdout"])
    except (json.JSONDecodeError, TypeError):
        return {}


def run_benchmark(work_root, binary, repetitions=1, rcc_sha="unknown", runner_factory=RCCRunner):
    if repetitions < 1:
        raise ValueError("repetitions must be positive")
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
                phases += [acquired, consumer.run(["env", "lifecycle", "verify", "--artifact", artifact, "--json"], "verify")]
                execution = consumer.run(["env", "exec", "--artifact", artifact, *trust, "--json", "--",
                                          "python", "-c", "import yaml; print(yaml.__version__)"], "startup")
                phases.append(execution)
                phases.append({**execution, "id": "import", "operation": "child import (aggregate env exec)",
                               "status": "unavailable", "wall_ns": None, "reason": "RCC reports aggregate exec timing"})
                warm = consumer.run(["env", "acquire", "--artifact", artifact, "--provider", "office", *trust, "--json"], "warm")
                phases.append(warm)
                exec_json = _json_output(execution)
                phases.append({"id": "lease", "operation": "leaseId from env exec receipt", "status": "observed",
                               "wall_ns": None, "evidence": {"leaseId": exec_json.get("leaseId")}})
                phases.append({"id": "gc", "operation": "RCC environment GC", "status": "unavailable", "wall_ns": None,
                               "reason": "no public environment artifact GC command"})
                acquired_json = _json_output(acquired)
                run = {"repetition": repetition, "fixture_id": fixture["id"], "phases": phases,
                       "correctness_gates": {"artifact_digest": bool(artifact),
                                             "verification_receipt": bool(acquired_json.get("verification")),
                                             "materialization_receipt": bool(acquired_json.get("materializationId")),
                                             "clean_cache_provider": acquired_json.get("cacheHit") == "provider",
                                             "warm_cache_local_materialization": _json_output(warm).get("cacheHit") == "local-materialization"}}
                pending.append((run, consumer, trust))
            provider.terminate()
            provider.wait(timeout=10)
            for run, consumer, trust in pending:
                provider_dead = consumer.run(["env", "acquire", "--artifact", artifact, "--provider", "office", *trust, "--json"], "provider-dead")
                run["phases"].append(provider_dead)
                run["correctness_gates"]["provider_dead_cache_local_materialization"] = _json_output(provider_dead).get("cacheHit") == "local-materialization"
                runs.append(run)
        finally:
            if provider.poll() is None:
                provider.terminate()
                provider.wait(timeout=10)
    report = build_report({"rcc_sha": rcc_sha, "consumer_sha": "external-unavailable", "binary": str(Path(binary).resolve())},
                          [fixture], runs, _context())
    required = {"acquire", "verify", "startup", "warm", "provider-dead"}
    for run in runs:
        statuses = {phase["id"]: phase["status"] for phase in run["phases"]}
        required_for_run = required | ({"publish"} if run["repetition"] == 0 else set())
        if any(statuses.get(phase) != "measured" for phase in required_for_run) or not all(
                run["correctness_gates"].values()):
            raise BenchmarkFailure(report)
    return report


def build_report(candidate, fixtures, runs, context):
    return {"schema_version": 2, "benchmark": "rcc-environment-lifecycle-v2", "candidate": candidate,
            "fixtures": fixtures, "context": context, "runs": runs}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--binary", required=True)
    parser.add_argument("--rcc-sha", required=True)
    parser.add_argument("--repetitions", type=int, default=1)
    parser.add_argument("--output", type=Path, default=Path("tmp/lifecycle-baseline.json"))
    args = parser.parse_args()
    try:
        report = run_benchmark(Path(tempfile.gettempdir()), args.binary, args.repetitions, args.rcc_sha)
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
