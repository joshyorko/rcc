#!/usr/bin/env python3
"""Run an offline, reproducible Holotree filesystem benchmark."""

import argparse
import hashlib
import json
import os
import platform
import shutil
import tempfile
import time
from pathlib import Path


def timed(name, function):
    started = time.perf_counter_ns()
    cpu_started = time.process_time_ns()
    result = function()
    return {"phase": name, "wall_ns": time.perf_counter_ns() - started,
            "cpu_ns": time.process_time_ns() - cpu_started, **result}


def run_once(root, count):
    root.mkdir()
    source = root / "source"
    target = root / "materialized"
    source.mkdir()
    payload = b"rcc-holotree-benchmark\n" * 32
    for index in range(count):
        path = source / f"package_{index // 100:04d}" / f"object_{index:06d}.py"
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_bytes(payload)

    def inventory(directory):
        digest = hashlib.sha256()
        sizes = []
        for path in sorted(directory.rglob("*")):
            if path.is_file():
                data = path.read_bytes()
                digest.update(path.relative_to(directory).as_posix().encode() + b"\0" + data)
                sizes.append(len(data))
        return {"files": len(sizes), "bytes": sum(sizes), "digest": digest.hexdigest()}

    phases = [timed("inventory_and_hash", lambda: inventory(source))]
    phases.append(timed("materialize_copy", lambda: (
        shutil.copytree(source, target), {"files": count, "bytes": count * len(payload)}
    )[1]))
    phases.append(timed("verify_materialization", lambda: inventory(target)))
    if phases[0]["digest"] != phases[2]["digest"] or phases[0]["files"] != phases[2]["files"]:
        raise RuntimeError("materialized inventory differs from source")
    return phases


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--files", type=int, default=1000)
    parser.add_argument("--repetitions", type=int, default=5)
    parser.add_argument("--output", type=Path, default=Path("tmp/holotree-benchmark.json"))
    args = parser.parse_args()
    if args.files < 1 or args.repetitions < 1:
        parser.error("--files and --repetitions must be positive")
    with tempfile.TemporaryDirectory(prefix="rcc-holotree-benchmark-") as directory:
        runs = [{"repetition": repetition, "phases": run_once(Path(directory) / str(repetition), args.files)}
                for repetition in range(args.repetitions)]
    output = {"schema_version": 2, "benchmark": "holotree-filesystem-v2",
              "fixture": {"kind": "many-small-files", "files": args.files},
              "harness_sha256": hashlib.sha256(Path(__file__).read_bytes()).hexdigest(),
              "repetitions": args.repetitions,
              "system": {"platform": platform.platform(), "python": platform.python_version(),
                          "kernel": platform.release(), "cpu_count": os.cpu_count()},
              "runs": runs}
    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_text(json.dumps(output, indent=2, sort_keys=True) + "\n", encoding="utf-8")
    print(args.output)


if __name__ == "__main__":
    main()
