#!/usr/bin/env python3
"""Benchmark RCC's experimental generic tree-artifact lifecycle."""

from __future__ import annotations

import argparse
import hashlib
import json
import os
import platform
import re
import shutil
import stat
import subprocess
import time
from pathlib import Path
from typing import Any


ROOT = Path(__file__).resolve().parents[2]
DEFAULT_WORKDIR = ROOT / "tmp" / "room-artifact-bench"
DEFAULT_FIXTURE = Path("/workspaces/josh-room-rustic-bench-runner/.benchmark-artifacts/fixtures/full")
DEFAULT_RCC = ROOT / "build" / "rcc"
EXPECTED_FILES = 100_000
EXPECTED_DIRECTORIES = 250
EXPECTED_BYTES = 2_153_884_204
EDIT_PATH = "d0000/f000000.bin"
EDIT_BYTES = 91_687
CHUNK_BYTES = 1024 * 1024
FILE_NAME = re.compile(r"f([0-9]{6})\.bin\Z")


def _exists(path: Path) -> bool:
    return path.exists() or path.is_symlink()


def _hash_file(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as stream:
        for chunk in iter(lambda: stream.read(CHUNK_BYTES), b""):
            digest.update(chunk)
    return digest.hexdigest()


def _manifest(root: Path) -> dict[str, Any]:
    """Hash a sorted full tree outside benchmark engine timers."""
    started = time.perf_counter_ns()
    if root.is_symlink() or not root.is_dir():
        raise ValueError(f"tree root must be a real directory: {root}")

    records: list[dict[str, Any]] = []
    files = 0
    directories = 0
    logical_bytes = 0

    def visit(directory: Path, relative: str) -> None:
        nonlocal files, directories, logical_bytes
        metadata = directory.lstat()
        if not stat.S_ISDIR(metadata.st_mode):
            raise ValueError(f"tree contains a non-directory ancestor: {directory}")
        if relative:
            records.append(
                {
                    "path": relative,
                    "type": "directory",
                    "mode": stat.S_IMODE(metadata.st_mode),
                    "size": None,
                    "sha256": None,
                }
            )
            directories += 1

        try:
            children = sorted(os.scandir(directory), key=lambda entry: entry.name)
        except OSError as error:
            raise RuntimeError(f"cannot read directory {directory}: {error}") from error

        for child in children:
            path = Path(child.path)
            child_relative = f"{relative}/{child.name}" if relative else child.name
            info = path.lstat()
            mode = stat.S_IMODE(info.st_mode)
            if stat.S_ISDIR(info.st_mode):
                visit(path, child_relative)
            elif stat.S_ISREG(info.st_mode):
                flags = os.O_RDONLY | getattr(os, "O_NOFOLLOW", 0)
                descriptor = os.open(path, flags)
                digest = hashlib.sha256()
                count = 0
                with os.fdopen(descriptor, "rb") as stream:
                    opened = os.fstat(stream.fileno())
                    if not stat.S_ISREG(opened.st_mode) or (
                        opened.st_dev,
                        opened.st_ino,
                    ) != (info.st_dev, info.st_ino):
                        raise RuntimeError(f"file changed while opening: {path}")
                    for chunk in iter(lambda: stream.read(CHUNK_BYTES), b""):
                        digest.update(chunk)
                        count += len(chunk)
                    after = os.fstat(stream.fileno())
                if (
                    count != info.st_size
                    or after.st_size != info.st_size
                    or after.st_mtime_ns != info.st_mtime_ns
                    or after.st_mode != info.st_mode
                ):
                    raise RuntimeError(f"file changed while hashing: {path}")
                records.append(
                    {
                        "path": child_relative,
                        "type": "file",
                        "mode": mode,
                        "size": count,
                        "sha256": digest.hexdigest(),
                    }
                )
                files += 1
                logical_bytes += count
            elif stat.S_ISLNK(info.st_mode):
                raise ValueError(f"fixture contains a symlink: {path}")
            else:
                raise ValueError(f"fixture contains a special file: {path}")

    root_info = root.lstat()
    records.append(
        {
            "path": ".",
            "type": "directory",
            "mode": stat.S_IMODE(root_info.st_mode),
            "size": None,
            "sha256": None,
        }
    )
    visit(root, "")
    records.sort(key=lambda record: record["path"])
    aggregate = hashlib.sha256()
    for record in records:
        aggregate.update(
            json.dumps(record, sort_keys=True, separators=(",", ":")).encode(
                "utf-8"
            )
        )
        aggregate.update(b"\n")
    return {
        "entries": records,
        "files": files,
        "directories": directories,
        "logical_bytes": logical_bytes,
        "aggregate_sha256": aggregate.hexdigest(),
        "elapsed_ns": time.perf_counter_ns() - started,
    }


def _assert_fixture(manifest: dict[str, Any]) -> None:
    records = manifest["entries"]
    directories = {row["path"] for row in records if row["type"] == "directory"}
    expected_directories = {"."} | {
        f"d{index:04d}" for index in range(EXPECTED_DIRECTORIES)
    }
    if manifest["files"] != EXPECTED_FILES:
        raise ValueError(f"fixture file count is {manifest['files']}, expected {EXPECTED_FILES}")
    if manifest["directories"] != EXPECTED_DIRECTORIES:
        raise ValueError(
            f"fixture directory count is {manifest['directories']}, expected {EXPECTED_DIRECTORIES}"
        )
    if manifest["logical_bytes"] != EXPECTED_BYTES:
        raise ValueError(
            f"fixture logical bytes are {manifest['logical_bytes']}, expected {EXPECTED_BYTES}"
        )
    if directories != expected_directories:
        raise ValueError("fixture directory names do not match d0000 through d0249")

    indices: set[int] = set()
    by_path = {row["path"]: row for row in records}
    if by_path["."]["mode"] != 0o755:
        raise ValueError("fixture root mode must be 0755")
    for directory in expected_directories - {"."}:
        if by_path[directory]["mode"] != 0o755:
            raise ValueError(f"fixture directory mode is not 0755: {directory}")
    for row in records:
        if row["type"] != "file":
            continue
        match = FILE_NAME.fullmatch(Path(row["path"]).name)
        parent = Path(row["path"]).parent.as_posix()
        if match is None or parent not in expected_directories:
            raise ValueError(f"fixture file path is unexpected: {row['path']}")
        index = int(match.group(1))
        if index >= EXPECTED_FILES or index in indices:
            raise ValueError(f"fixture file index is invalid or duplicated: {row['path']}")
        indices.add(index)
        if row["mode"] != 0o644:
            raise ValueError(f"fixture file mode is not 0644: {row['path']}")
    if len(indices) != EXPECTED_FILES:
        raise ValueError("fixture file indices are incomplete")
    edit = by_path.get(EDIT_PATH)
    if edit is None or edit["size"] != EDIT_BYTES:
        raise ValueError(f"fixture edit target must be {EDIT_BYTES} bytes")


def _manifest_summary(manifest: dict[str, Any]) -> dict[str, Any]:
    return {
        "files": manifest["files"],
        "directories_excluding_root": manifest["directories"],
        "logical_bytes": manifest["logical_bytes"],
        "entry_count_including_root": len(manifest["entries"]),
        "aggregate_sha256": manifest["aggregate_sha256"],
        "root_mode": next(
            row["mode"] for row in manifest["entries"] if row["path"] == "."
        ),
        "elapsed_ns": manifest["elapsed_ns"],
    }


def _assert_same_tree(
    expected: dict[str, Any], actual: dict[str, Any], label: str
) -> None:
    if expected["aggregate_sha256"] == actual["aggregate_sha256"]:
        return
    expected_rows = {row["path"]: row for row in expected["entries"]}
    actual_rows = {row["path"]: row for row in actual["entries"]}
    changed = sorted(
        path
        for path in expected_rows.keys() | actual_rows.keys()
        if expected_rows.get(path) != actual_rows.get(path)
    )
    raise RuntimeError(f"{label} differs at {changed[:10]}")


def _changed_paths(before: dict[str, Any], after: dict[str, Any]) -> list[str]:
    old = {row["path"]: row for row in before["entries"]}
    new = {row["path"]: row for row in after["entries"]}
    return sorted(path for path in old.keys() | new.keys() if old.get(path) != new.get(path))


def _run_json(argv: list[str], phase: str) -> tuple[dict[str, Any], dict[str, Any]]:
    print(f"{phase}: running RCC", flush=True)
    started = time.perf_counter_ns()
    process = subprocess.run(argv, text=True, capture_output=True, check=False)
    elapsed_ns = time.perf_counter_ns() - started
    if process.returncode:
        raise RuntimeError(
            f"{phase} failed ({process.returncode})\nstdout: {process.stdout}\nstderr: {process.stderr}"
        )
    try:
        result = json.loads(process.stdout)
    except json.JSONDecodeError as error:
        raise RuntimeError(
            f"{phase} did not emit JSON\nstdout: {process.stdout}\nstderr: {process.stderr}"
        ) from error
    if not isinstance(result, dict):
        raise RuntimeError(f"{phase} JSON output must be an object")
    return result, {
        "elapsed_ns": elapsed_ns,
        "argv": argv,
    }


def _require_metrics(
    actual: dict[str, Any], expected: dict[str, int], phase: str
) -> dict[str, dict[str, int]]:
    asserted: dict[str, dict[str, int]] = {}
    for key, want in expected.items():
        got = actual.get(key)
        if not isinstance(got, int) or got != want:
            raise RuntimeError(f"{phase} metric {key} is {got!r}; expected {want}")
        asserted[key] = {"expected": want, "actual": got}
    return asserted


def _edit_target(root: Path) -> None:
    target = root / EDIT_PATH
    info = target.lstat()
    if not stat.S_ISREG(info.st_mode) or info.st_size != EDIT_BYTES:
        raise RuntimeError("private edit target is not the expected regular file")
    with target.open("r+b") as stream:
        value = stream.read(1)
        if len(value) != 1:
            raise RuntimeError("private edit target is empty")
        stream.seek(0)
        stream.write(bytes((value[0] ^ 1,)))
        stream.flush()
        os.fsync(stream.fileno())
    if target.stat(follow_symlinks=False).st_size != EDIT_BYTES:
        raise RuntimeError("private edit changed the target file size")


def _new_directory(path: Path) -> None:
    if _exists(path):
        raise FileExistsError(f"benchmark output already exists; refusing rerun: {path}")
    path.mkdir(mode=0o755, parents=False)


def run_benchmark(fixture: Path, workdir: Path, rcc: Path) -> dict[str, Any]:
    fixture = fixture.expanduser()
    if fixture.is_symlink():
        raise ValueError("--fixture must not be a symlink")
    fixture = fixture.resolve(strict=True)
    if not fixture.is_dir():
        raise ValueError("--fixture must name an existing real directory")

    workdir = workdir.expanduser()
    if workdir.is_symlink():
        raise ValueError("--workdir cannot be a symlink")
    workdir = workdir.resolve()
    if not workdir.exists():
        workdir.mkdir(parents=True, exist_ok=True)
    if not workdir.is_dir():
        raise ValueError("--workdir must be a directory")

    private_source = workdir / "working-source"
    store = workdir / "store"
    destination = workdir / "materialized"
    receipt_path = workdir / "receipt.json"
    for output in (private_source, store, destination, receipt_path):
        if fixture == output.resolve() or fixture in output.resolve().parents:
            raise ValueError(f"benchmark output would be inside the source fixture: {output}")
        if _exists(output):
            raise FileExistsError(f"benchmark output already exists; refusing rerun: {output}")

    rcc = rcc.expanduser().resolve(strict=True)
    if not rcc.is_file() or not os.access(rcc, os.X_OK):
        raise ValueError("--rcc must name an executable RCC candidate")
    rcc_hash = _hash_file(rcc)
    version_process = subprocess.run(
        [str(rcc), "--version"], text=True, capture_output=True, check=False
    )
    if version_process.returncode:
        raise RuntimeError(f"cannot read RCC version: {version_process.stderr}")

    print("fixture preflight: hashing the original source tree", flush=True)
    source_before = _manifest(fixture)
    _assert_fixture(source_before)
    original_edit_sha = next(
        row["sha256"] for row in source_before["entries"] if row["path"] == EDIT_PATH
    )

    fixture_after: dict[str, Any] | None = None
    try:
        print("prepare private source copy", flush=True)
        copy_started = time.perf_counter_ns()
        shutil.copytree(fixture, private_source, copy_function=shutil.copy2)
        copy_elapsed_ns = time.perf_counter_ns() - copy_started

        print("private source preflight: hashing the seed copy", flush=True)
        private_seed = _manifest(private_source)
        _assert_fixture(private_seed)
        _assert_same_tree(source_before, private_seed, "private seed copy")

        seed_capture_argv = [
            str(rcc),
            "tree-artifact",
            "capture",
            "--store",
            str(store),
            "--source",
            str(private_source),
        ]
        seed_capture, seed_capture_cli = _run_json(seed_capture_argv, "seed capture")
        seed_capture_stats = seed_capture.get("stats")
        if not isinstance(seed_capture_stats, dict):
            raise RuntimeError("seed capture result is missing stats")
        seed_capture_asserted = _require_metrics(
            seed_capture_stats,
            {
                "files_hashed": EXPECTED_FILES,
                "file_bytes_hashed": EXPECTED_BYTES,
                "directories_scanned": EXPECTED_DIRECTORIES + 1,
                "tree_nodes_rebuilt": EXPECTED_DIRECTORIES + 1,
            },
            "seed capture",
        )
        seed_snapshot = seed_capture.get("snapshot")
        seed_root = seed_capture.get("root")
        if not isinstance(seed_snapshot, str) or not seed_snapshot.startswith("sha256:"):
            raise RuntimeError("seed capture returned an invalid snapshot digest")
        if not isinstance(seed_root, dict) or not isinstance(seed_root.get("digest"), str):
            raise RuntimeError("seed capture returned an invalid root descriptor")
        seed_root_digest = seed_root["digest"]

        _new_directory(destination)
        seed_materialize_argv = [
            str(rcc),
            "tree-artifact",
            "materialize",
            "--store",
            str(store),
            "--destination",
            str(destination),
            "--snapshot",
            seed_snapshot,
        ]
        seed_materialize, seed_materialize_cli = _run_json(
            seed_materialize_argv, "seed materialization"
        )
        seed_materialize_asserted = _require_metrics(
            seed_materialize,
            {"files_touched": EXPECTED_FILES, "bytes_written": EXPECTED_BYTES},
            "seed materialization",
        )
        print("seed verification: hashing the materialized tree", flush=True)
        seed_destination = _manifest(destination)
        _assert_same_tree(private_seed, seed_destination, "seed materialization")

        start_edit_sha = original_edit_sha
        print(f"edit private source: {EDIT_PATH}", flush=True)
        _edit_target(private_source)
        edited_source = _manifest(private_source)
        if edited_source["files"] != EXPECTED_FILES or edited_source["logical_bytes"] != EXPECTED_BYTES:
            raise RuntimeError("private edit changed fixture dimensions")
        changed_paths = _changed_paths(private_seed, edited_source)
        if changed_paths != [EDIT_PATH]:
            raise RuntimeError(f"private edit changed unexpected paths: {changed_paths[:10]}")
        current_edit_sha = next(
            row["sha256"] for row in edited_source["entries"] if row["path"] == EDIT_PATH
        )
        if current_edit_sha == start_edit_sha:
            raise RuntimeError("private edit did not change the edit target bytes")

        incremental_capture_argv = [
            str(rcc),
            "tree-artifact",
            "capture",
            "--store",
            str(store),
            "--source",
            str(private_source),
            "--parent",
            seed_snapshot,
            "--dirty",
            EDIT_PATH,
        ]
        incremental_capture, incremental_capture_cli = _run_json(
            incremental_capture_argv, "incremental capture"
        )
        incremental_stats = incremental_capture.get("stats")
        if not isinstance(incremental_stats, dict):
            raise RuntimeError("incremental capture result is missing stats")
        incremental_capture_asserted = _require_metrics(
            incremental_stats,
            {
                "files_hashed": 1,
                "file_bytes_hashed": EDIT_BYTES,
                "directories_scanned": 0,
                "tree_nodes_rebuilt": 2,
            },
            "incremental capture",
        )
        child_snapshot = incremental_capture.get("snapshot")
        child_root = incremental_capture.get("root")
        if not isinstance(child_snapshot, str) or not child_snapshot.startswith("sha256:"):
            raise RuntimeError("incremental capture returned an invalid snapshot digest")
        if not isinstance(child_root, dict) or not isinstance(child_root.get("digest"), str):
            raise RuntimeError("incremental capture returned an invalid root descriptor")

        incremental_materialize_argv = [
            str(rcc),
            "tree-artifact",
            "materialize",
            "--store",
            str(store),
            "--destination",
            str(destination),
            "--snapshot",
            child_snapshot,
            "--parent",
            seed_snapshot,
        ]
        incremental_materialize, incremental_materialize_cli = _run_json(
            incremental_materialize_argv, "incremental materialization"
        )
        incremental_materialize_asserted = _require_metrics(
            incremental_materialize,
            {
                "files_touched": 1,
                "directories_touched": 0,
                "bytes_written": EDIT_BYTES,
            },
            "incremental materialization",
        )
        print("incremental verification: hashing edited source and materialized tree", flush=True)
        materialized_after = _manifest(destination)
        _assert_same_tree(edited_source, materialized_after, "incremental materialization")

        host = {
            "platform": platform.platform(),
            "system": platform.system(),
            "release": platform.release(),
            "machine": platform.machine(),
            "python": platform.python_version(),
            "logical_cpus": os.cpu_count(),
        }
        receipt = {
            "schema_version": 1,
            "benchmark": "rcc-tree-artifact-v1",
            "candidate": {
                "rcc_path": str(rcc),
                "rcc_sha256": rcc_hash,
                "version": version_process.stdout.strip(),
                "commit_sha": subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=ROOT, text=True).strip(),
                "starting_main_sha": subprocess.check_output(["git", "rev-parse", "origin/main"], cwd=ROOT, text=True).strip(),
                "working_tree_status": subprocess.check_output(["git", "status", "--porcelain"], cwd=ROOT, text=True).splitlines(),
            },
            "host": host,
            "fixture": {
                "kind": "deterministic-synthetic-many-small-files",
                "source_path": str(fixture),
                "private_source_path": str(private_source),
                "files": EXPECTED_FILES,
                "directories_excluding_root": EXPECTED_DIRECTORIES,
                "logical_bytes": EXPECTED_BYTES,
                "edit_path": EDIT_PATH,
                "edit_file_bytes": EDIT_BYTES,
                "edit_start_sha256": start_edit_sha,
                "edit_current_sha256": current_edit_sha,
                "changed_paths": changed_paths,
            },
            "snapshots": {
                "seed": seed_snapshot,
                "seed_root_digest": seed_root_digest,
                "incremental": child_snapshot,
                "incremental_root_digest": child_root["digest"],
            },
            "source_immutability": {
                "manifest_before": _manifest_summary(source_before),
                "manifest_after": None,
            },
            "phases": {
                "private_source_copy": {"elapsed_ns": copy_elapsed_ns},
                "private_seed_source_verification": _manifest_summary(private_seed),
                "seed_capture": {
                    **seed_capture_cli,
                    "engine": seed_capture_stats,
                    "asserted_metrics": seed_capture_asserted,
                    "result": seed_capture,
                },
                "seed_materialization": {
                    **seed_materialize_cli,
                    "engine": seed_materialize,
                    "asserted_metrics": seed_materialize_asserted,
                    "verification": _manifest_summary(seed_destination),
                },
                "incremental_capture": {
                    **incremental_capture_cli,
                    "dirty_paths": [EDIT_PATH],
                    "engine": incremental_stats,
                    "asserted_metrics": incremental_capture_asserted,
                    "result": incremental_capture,
                },
                "incremental_materialization": {
                    **incremental_materialize_cli,
                    "parent": seed_snapshot,
                    "engine": incremental_materialize,
                    "asserted_metrics": incremental_materialize_asserted,
                    "verification": _manifest_summary(materialized_after),
                },
                "edited_source_verification": _manifest_summary(edited_source),
            },
            "verification": {
                "seed_source_vs_materialized": {
                    "source_manifest_sha256": private_seed["aggregate_sha256"],
                    "materialized_manifest_sha256": seed_destination["aggregate_sha256"],
                    "counts": _manifest_summary(seed_destination),
                    "passed": True,
                },
                "edited_source_vs_materialized": {
                    "source_manifest_sha256": edited_source["aggregate_sha256"],
                    "materialized_manifest_sha256": materialized_after["aggregate_sha256"],
                    "counts": _manifest_summary(materialized_after),
                    "passed": True,
                },
            },
        }
    finally:
        print("fixture immutability: hashing the original source tree again", flush=True)
        fixture_after = _manifest(fixture)
        _assert_same_tree(source_before, fixture_after, "original fixture after benchmark")

    assert fixture_after is not None
    receipt["source_immutability"]["manifest_after"] = _manifest_summary(fixture_after)
    receipt["source_immutability"]["unchanged"] = True

    with receipt_path.open("x", encoding="utf-8") as stream:
        json.dump(receipt, stream, indent=2, sort_keys=True)
        stream.write("\n")
    print(f"receipt: {receipt_path}", flush=True)
    print(
        "metrics: "
        f"seed_capture_ns={seed_capture_stats['total_ns']} "
        f"incremental_capture_ns={incremental_stats['total_ns']} "
        f"incremental_materialize_ns={incremental_materialize['total_ns']}",
        flush=True,
    )
    return receipt


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--fixture", type=Path, default=DEFAULT_FIXTURE)
    parser.add_argument("--workdir", type=Path, default=DEFAULT_WORKDIR)
    parser.add_argument("--rcc", type=Path, default=DEFAULT_RCC)
    args = parser.parse_args()
    try:
        run_benchmark(args.fixture, args.workdir, args.rcc)
    except (OSError, RuntimeError, ValueError) as error:
        parser.exit(1, f"benchmark failed: {error}\n")


if __name__ == "__main__":
    main()
