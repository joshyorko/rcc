#!/usr/bin/env python3
"""Collect deterministic, raw evidence for RCC environment lifecycle work.

The default run is an offline fixture baseline. It measures filesystem work
only; unavailable provider and consumer phases are recorded as such rather
than inferred. A caller may provide candidate IDs and retain the JSON beside
the exact RCC binary used for a real lifecycle run.
"""

import argparse
import hashlib
import json
import os
import platform
import shutil
import tempfile
import time
from pathlib import Path

PHASES = ("publish", "acquire", "verify", "materialize", "lease", "startup", "import", "warm", "provider-dead", "gc")
FIXTURES = (
    {"id": "many-small-files-v1", "kind": "many-small-files", "files": 128},
    {"id": "python-package-v1", "kind": "python-package", "files": 24},
)


def _context():
    stat = os.statvfs(tempfile.gettempdir())
    return {
        "platform": platform.system().lower(),
        "platform_release": platform.release(),
        "architecture": platform.machine(),
        "python": platform.python_version(),
        "filesystem": {"temp_root": tempfile.gettempdir(), "block_size": stat.f_bsize},
    }


def _fixture(root, fixture):
    source = root / "source"
    source.mkdir(parents=True)
    payload = (fixture["id"] + "\n").encode() * 8
    for index in range(fixture["files"]):
        path = source / f"package_{index // 16:04d}" / f"object_{index:06d}.py"
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_bytes(payload)
    return source


def _inventory(source):
    digest = hashlib.sha256()
    files = 0
    total = 0
    for path in sorted(source.rglob("*")):
        if path.is_file():
            data = path.read_bytes()
            digest.update(path.relative_to(source).as_posix().encode() + b"\0" + data)
            files += 1
            total += len(data)
    return {"files": files, "bytes": total, "digest": digest.hexdigest()}


def _timed(phase_id, operation, function):
    started = time.perf_counter_ns()
    cpu_started = time.process_time_ns()
    result = function()
    return {"id": phase_id, "operation": operation, "status": "measured",
            "wall_ns": time.perf_counter_ns() - started,
            "cpu_ns": time.process_time_ns() - cpu_started, "evidence": result}


def _unavailable(phase_id, reason):
    return {"id": phase_id, "operation": phase_id, "status": "unavailable",
            "wall_ns": None, "reason": reason, "evidence": {}}


def _run_fixture(root, fixture):
    source = _fixture(root, fixture)
    target = root / "materialized"
    inventory = _inventory(source)
    phases = [
        _timed("publish", "fixture-catalog", lambda: inventory),
        _timed("acquire", "local-content", lambda: {"cache_hit": False}),
        _timed("verify", "identity-and-content", lambda: _inventory(source)),
        _timed("materialize", "copy", lambda: (shutil.copytree(source, target), inventory)[1]),
        _timed("lease", "process-scope", lambda: {"lease": "fixture-only"}),
        _unavailable("startup", "requires an RCC consumer command"),
        _unavailable("import", "requires an RCC consumer command"),
        _unavailable("warm", "requires repeated consumer requests"),
        _unavailable("provider-dead", "requires provider-backed acquisition"),
        _timed("gc", "fixture-cleanup", lambda: {"removed": False}),
    ]
    gates = {"identity": inventory["digest"] == _inventory(target)["digest"],
             "file_count": inventory["files"] == _inventory(target)["files"],
             "provider": "not-run"}
    return {"fixture_id": fixture["id"], "phases": phases,
            "correctness_gates": gates, "fixture_inventory": inventory}


def build_report(candidate, fixtures, runs, context):
    return {"schema_version": 2, "benchmark": "rcc-environment-lifecycle-v2",
            "candidate": candidate, "fixtures": fixtures, "context": context,
            "runs": runs}


def run_benchmark(work_root, repetitions=1, candidate=None):
    candidate = candidate or {"rcc_sha": "unknown", "consumer_sha": "unknown", "binary": "unknown"}
    fixtures = list(FIXTURES)
    runs = []
    with tempfile.TemporaryDirectory(prefix="rcc-lifecycle-", dir=work_root) as directory:
        base = Path(directory)
        for repetition in range(repetitions):
            for fixture in fixtures:
                runs.append({"repetition": repetition, **_run_fixture(base / str(repetition) / fixture["id"], fixture)})
    return build_report(candidate, fixtures, runs, _context())


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--repetitions", type=int, default=1)
    parser.add_argument("--rcc-sha", default="unknown")
    parser.add_argument("--consumer-sha", default="unknown")
    parser.add_argument("--binary", default="unknown")
    parser.add_argument("--output", type=Path, default=Path("tmp/lifecycle-baseline.json"))
    args = parser.parse_args()
    if args.repetitions < 1:
        parser.error("--repetitions must be positive")
    report = run_benchmark(Path(tempfile.gettempdir()), args.repetitions,
                           {"rcc_sha": args.rcc_sha, "consumer_sha": args.consumer_sha, "binary": args.binary})
    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_text(json.dumps(report, indent=2, sort_keys=True) + "\n", encoding="utf-8")
    print(args.output)


if __name__ == "__main__":
    main()
