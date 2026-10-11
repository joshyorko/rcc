#!/usr/bin/env python3
"""Plan-bound external codec micro. No RCC execution and no payload writes."""
import gzip
import hashlib
import json
import os
from pathlib import Path
import signal
import subprocess
import sys
import time
import traceback
import zlib

ROOT = Path(__file__).resolve().parent
PLAN_PATH = ROOT / "plan.json"
PHASES = ["legacy-gzip-decode", "gzip-level1-encode", "zstd-level3-encode",
          "gzip-level1-decode", "zstd-level3-decode"]
UNTIMED = {"original_gzip_decode_for_input_validation": 0,
           "gzip_roundtrip_decode_after_encode": 0,
           "zstd_roundtrip_decode_after_encode": 0,
           "gzip_frame_encode_before_decode_timing": 0,
           "zstd_frame_encode_before_decode_timing": 0}


def digest(data):
    return hashlib.sha256(data).hexdigest()


def emit(obj):
    print(json.dumps(obj, sort_keys=True, separators=(",", ":")), flush=True)


def object_path(provider_root, stored_digest):
    d = stored_digest.split(":", 1)[1]
    return Path(provider_root) / "objects/sha256" / d[:2] / d[2:4] / d


def file_state(path):
    st = path.stat(follow_symlinks=False)
    if path.is_symlink() or not path.is_file():
        raise RuntimeError(f"expected regular nonsymlink source file: {path}")
    return {"size": st.st_size, "mode": st.st_mode & 0o7777, "mtime_ns": st.st_mtime_ns}


def check_source(plan, entry):
    path = object_path(plan["source"]["provider_root"], entry["storedDigest"])
    state = file_state(path)
    stored = path.read_bytes()
    want_digest = entry["storedDigest"].split(":", 1)[1]
    if len(stored) != entry["storedSize"] or digest(stored) != want_digest:
        raise RuntimeError(f"stored object integrity failure: {want_digest}")
    logical = gzip.decompress(stored)
    UNTIMED["original_gzip_decode_for_input_validation"] += 1
    if len(logical) != entry["logicalSize"] or digest(logical) != entry["legacyObjectId"]:
        raise RuntimeError(f"logical object integrity failure: {entry['legacyObjectId']}")
    return path, state, stored, logical


def compressor_gzip(data):
    return zlib.compress(data, level=1, wbits=31)


def compressor_zstd(data, zstandard):
    return zstandard.ZstdCompressor(level=3, threads=0, write_content_size=True,
                                    write_checksum=True).compress(data)


def decoder_zstd(frame, expected_size, zstandard):
    return zstandard.ZstdDecompressor().decompress(frame, max_output_size=expected_size)


def validate_logical(output, entry, context):
    if len(output) != entry["logicalSize"] or digest(output) != entry["legacyObjectId"]:
        raise RuntimeError(f"logical validation failed ({context}): {entry['legacyObjectId']}")


def worker():
    import zstandard

    plan_bytes = PLAN_PATH.read_bytes()
    plan = json.loads(plan_bytes)
    if sys.executable != plan["toolchain"]["python_executable"]:
        raise RuntimeError("Python executable path differs from admitted plan")
    exe = Path(sys.executable)
    if digest(exe.read_bytes()) != plan["toolchain"]["python_sha256"]:
        raise RuntimeError("Python executable SHA differs from admitted plan")
    if zlib.ZLIB_VERSION != plan["toolchain"]["zlib_version"]:
        raise RuntimeError("zlib version differs from admitted plan")
    for name, expected in plan["toolchain"]["module_hashes"].items():
        module_path = Path(expected["path"])
        if module_path.stat().st_size != expected["bytes"] or digest(module_path.read_bytes()) != expected["sha256"]:
            raise RuntimeError(f"codec module SHA mismatch: {name}")
    if zstandard.__version__ != plan["toolchain"]["python_zstandard_version"]:
        raise RuntimeError("python-zstandard version differs from admitted plan")
    if list(zstandard.ZSTD_VERSION) != plan["toolchain"]["libzstd_version"]:
        raise RuntimeError("libzstd version differs from admitted plan")

    source = plan["source"]
    index_path = Path(source["index_path"])
    index_bytes = index_path.read_bytes()
    if len(index_bytes) != source["index_bytes"] or digest(index_bytes) != source["index_sha256"]:
        raise RuntimeError("frozen object index identity mismatch")
    index = json.loads(index_bytes)
    selected = sorted((e for e in index["entries"] if e["logicalSize"] > 1048576),
                      key=lambda e: e["storedDigest"])
    if len(selected) != source["selected_count"]:
        raise RuntimeError("selected object count mismatch")
    if selected != source["objects"]:
        raise RuntimeError("frozen selector does not match actual index")
    if sum(e["logicalSize"] for e in selected) != source["selected_logical_bytes"]:
        raise RuntimeError("selected logical-byte total mismatch")
    if sum(e["storedSize"] for e in selected) != source["selected_stored_bytes"]:
        raise RuntimeError("selected stored-byte total mismatch")
    manifest_path = Path(source["manifest_path"])
    manifest_bytes = manifest_path.read_bytes()
    if len(manifest_bytes) != source["manifest_bytes"] or digest(manifest_bytes) != source["manifest_sha256"]:
        raise RuntimeError("frozen manifest identity mismatch")

    before = {}
    emit({"event": "stage_start", "stage": "untimed_source_preflight"})
    for entry in selected:
        path, state, _, _ = check_source(plan, entry)
        before[entry["storedDigest"]] = {"path": str(path), **state}
    source_meta = {"index": file_state(index_path), "manifest": file_state(manifest_path)}
    emit({"event": "stage_done", "stage": "untimed_source_preflight", "objects": len(before)})

    # Canonical frames retain only SHA256/size. Every payload is released per object.
    canonical = {"gzip": {}, "zstd": {}}
    rows = []
    calls_by_phase = {phase: 0 for phase in PHASES}
    schedule = plan["schedule"]["phase_order"]
    for rep, order in enumerate(schedule):
        if sorted(order) != sorted(PHASES):
            raise RuntimeError("phase schedule is invalid")
        for phase_index, phase in enumerate(order):
            started = time.monotonic()
            emit({"event": "phase_start", "phase": phase, "repetition": rep})
            rotation = (rep + phase_index) % len(selected)
            entries = selected[rotation:] + selected[:rotation]
            for entry in entries:
                _, _, stored, logical = check_source(plan, entry)
                frame = None
                digest_value = entry["storedDigest"].split(":", 1)[1]
                input_bytes = 0
                frame_sha = digest_value
                frame_size = len(stored)
                if phase == "legacy-gzip-decode":
                    input_bytes = len(stored)
                    t0w, t0c = time.perf_counter_ns(), time.process_time_ns()
                    output = gzip.decompress(stored)
                    t1w, t1c = time.perf_counter_ns(), time.process_time_ns()
                    validate_logical(output, entry, phase)
                    output_bytes = len(output)
                elif phase == "gzip-level1-encode":
                    input_bytes = len(logical)
                    t0w, t0c = time.perf_counter_ns(), time.process_time_ns()
                    output = compressor_gzip(logical)
                    t1w, t1c = time.perf_counter_ns(), time.process_time_ns()
                    output_bytes = len(output)
                    frame_sha = digest(output)
                    frame_size = output_bytes
                    UNTIMED["gzip_roundtrip_decode_after_encode"] += 1
                    validate_logical(gzip.decompress(output), entry, phase + " roundtrip")
                    prev = canonical["gzip"].setdefault(entry["storedDigest"],
                        {"sha256": frame_sha, "size": frame_size})
                    if prev != {"sha256": frame_sha, "size": frame_size}:
                        raise RuntimeError("gzip frame is nondeterministic across repetitions")
                elif phase == "zstd-level3-encode":
                    input_bytes = len(logical)
                    t0w, t0c = time.perf_counter_ns(), time.process_time_ns()
                    output = compressor_zstd(logical, zstandard)
                    t1w, t1c = time.perf_counter_ns(), time.process_time_ns()
                    output_bytes = len(output)
                    frame_sha = digest(output)
                    frame_size = output_bytes
                    UNTIMED["zstd_roundtrip_decode_after_encode"] += 1
                    validate_logical(decoder_zstd(output, entry["logicalSize"], zstandard), entry,
                                     phase + " roundtrip")
                    prev = canonical["zstd"].setdefault(entry["storedDigest"],
                        {"sha256": frame_sha, "size": frame_size})
                    if prev != {"sha256": frame_sha, "size": frame_size}:
                        raise RuntimeError("zstd frame is nondeterministic across repetitions")
                elif phase == "gzip-level1-decode":
                    UNTIMED["gzip_frame_encode_before_decode_timing"] += 1
                    frame = compressor_gzip(logical)
                    frame_sha, frame_size = digest(frame), len(frame)
                    expected = canonical["gzip"].get(entry["storedDigest"])
                    if expected != {"sha256": frame_sha, "size": frame_size}:
                        raise RuntimeError("gzip decode input differs from canonical encoded frame")
                    input_bytes = len(frame)
                    t0w, t0c = time.perf_counter_ns(), time.process_time_ns()
                    output = gzip.decompress(frame)
                    t1w, t1c = time.perf_counter_ns(), time.process_time_ns()
                    validate_logical(output, entry, phase)
                    output_bytes = len(output)
                elif phase == "zstd-level3-decode":
                    UNTIMED["zstd_frame_encode_before_decode_timing"] += 1
                    frame = compressor_zstd(logical, zstandard)
                    frame_sha, frame_size = digest(frame), len(frame)
                    expected = canonical["zstd"].get(entry["storedDigest"])
                    if expected != {"sha256": frame_sha, "size": frame_size}:
                        raise RuntimeError("zstd decode input differs from canonical encoded frame")
                    input_bytes = len(frame)
                    t0w, t0c = time.perf_counter_ns(), time.process_time_ns()
                    output = decoder_zstd(frame, entry["logicalSize"], zstandard)
                    t1w, t1c = time.perf_counter_ns(), time.process_time_ns()
                    validate_logical(output, entry, phase)
                    output_bytes = len(output)
                else:
                    raise RuntimeError(f"unknown phase: {phase}")
                row = {"phase": phase, "repetition": rep, "storedDigest": entry["storedDigest"],
                       "legacyObjectId": entry["legacyObjectId"], "input_bytes": input_bytes,
                       "logical_bytes": entry["logicalSize"], "output_bytes": output_bytes, "frame_sha256": frame_sha,
                       "frame_bytes": frame_size, "logical_output_sha256": entry["legacyObjectId"],
                       "wall_ns": t1w - t0w,
                       "process_cpu_ns": t1c - t0c, "verified": True}
                rows.append(row)
                calls_by_phase[phase] += 1
                emit({"event": "object_sample", **row})
                del output, logical, stored
                if frame is not None:
                    del frame
            elapsed = time.monotonic() - started
            emit({"event": "phase_done", "phase": phase, "repetition": rep,
                  "objects": len(entries), "elapsed_seconds": elapsed})
            if elapsed > plan["limits"]["phase_timeout_seconds"]:
                raise RuntimeError(f"phase watchdog exceeded after completion: {phase}")

    if any(count != 100 for count in calls_by_phase.values()) or len(rows) != 500:
        raise RuntimeError("codec call-count acceptance failed")
    if any(len(canonical[k]) != 20 for k in canonical):
        raise RuntimeError("not all canonical frames were captured")

    phase_summary = {}
    for phase in PHASES:
        phase_rows = [r for r in rows if r["phase"] == phase]
        logical_total = sum(r["logical_bytes"] for r in phase_rows)
        phase_summary[phase] = {
            "object_calls": len(phase_rows),
            "wall_ns_sum": sum(r["wall_ns"] for r in phase_rows),
            "process_cpu_ns_sum": sum(r["process_cpu_ns"] for r in phase_rows),
            "logical_bytes_sum": logical_total,
            "codec_input_bytes_sum": sum(r["input_bytes"] for r in phase_rows),
            "codec_output_bytes_sum": sum(r["output_bytes"] for r in phase_rows),
            "weighted_wall_ns_per_logical_byte": sum(r["wall_ns"] for r in phase_rows) / logical_total,
            "weighted_cpu_ns_per_logical_byte": sum(r["process_cpu_ns"] for r in phase_rows) / logical_total,
        }

    emit({"event": "stage_start", "stage": "untimed_source_postflight"})
    after = {}
    for entry in selected:
        path, state, _, _ = check_source(plan, entry)
        after[entry["storedDigest"]] = {"path": str(path), **state}
    if before != after or source_meta != {"index": file_state(index_path), "manifest": file_state(manifest_path)}:
        raise RuntimeError("source object/index/manifest metadata changed during experiment")
    emit({"event": "stage_done", "stage": "untimed_source_postflight", "objects": len(after)})

    results = {"status": "PASS", "plan_sha256": digest(plan_bytes),
               "runner_sha256": digest(Path(__file__).read_bytes()), "source_before": before,
               "source_after": after, "source_metadata_before": source_meta,
               "source_metadata_after": {"index": file_state(index_path), "manifest": file_state(manifest_path)},
               "canonical_frame_receipts": canonical, "calls_by_phase": calls_by_phase,
               "untimed_codec_calls": dict(UNTIMED), "phase_summary": phase_summary,
               "samples": rows, "sample_count": len(rows),
               "semantic_scope": "20 indexed gzip objects with logicalSize >1MiB; external Python codecs; no RCC execution, no frame persistence"}
    write_json_bounded(ROOT / "results.json", results)
    emit({"event": "complete", "status": "PASS", "sample_count": len(rows)})


def write_json_bounded(path, value, reserved_margin=8192):
    payload = (json.dumps(value, sort_keys=True, separators=(",", ":")) + "\n").encode("utf-8")
    allocated = ((len(payload) + 4095) // 4096) * 4096
    cap = json.loads(PLAN_PATH.read_text())["limits"]["owned_evidence_allocated_bytes_max"]
    if allocated_bytes() + allocated + reserved_margin > cap:
        raise RuntimeError(f"predictive evidence-cap guard refused write: {path.name}")
    with path.open("xb") as f:
        f.write(payload)


def allocated_bytes():
    total = ROOT.lstat().st_blocks * 512
    for base, dirs, files in os.walk(ROOT, followlinks=False):
        for name in dirs + files:
            path = Path(base) / name
            st = path.lstat()
            if path.is_symlink():
                raise RuntimeError(f"unexpected symlink in owned evidence: {path}")
            total += st.st_blocks * 512
    return total


def free_bytes():
    v = os.statvfs(ROOT)
    return v.f_bavail * v.f_frsize


def process_tree_rss(root_pid):
    p = subprocess.run(["ps", "-eo", "pid=,ppid=,rss="], check=True, text=True,
                       stdout=subprocess.PIPE, stderr=subprocess.PIPE)
    rows = {}
    for line in p.stdout.splitlines():
        parts = line.split()
        if len(parts) != 3:
            continue
        pid, ppid, rss_kib = map(int, parts)
        rows[pid] = (ppid, rss_kib * 1024)
    if root_pid not in rows:
        raise RuntimeError("known supervisor PID missing from ps sample")
    members = {root_pid}
    changed = True
    while changed:
        changed = False
        for pid, (ppid, _) in rows.items():
            if ppid in members and pid not in members:
                members.add(pid)
                changed = True
    return sum(rows[pid][1] for pid in members), sorted(members)


def supervisor():
    plan = json.loads(PLAN_PATH.read_text())
    limits = plan["limits"]
    if free_bytes() < limits["minimum_filesystem_free_bytes"] + limits["early_stop_margin_bytes"]:
        raise RuntimeError("free-space admission floor plus early-stop margin not met")
    if allocated_bytes() > limits["owned_evidence_allocated_bytes_max"]:
        raise RuntimeError("owned evidence already exceeds cap")
    existing = {p.name for p in ROOT.iterdir()}
    if existing != {"plan.json", Path(__file__).name}:
        raise RuntimeError(f"refusing nonfresh namespace: {sorted(existing)}")
    started = time.monotonic()
    observations = []
    failure = None
    child = None
    stderr = None
    events = None
    runlog = None
    status = None
    numeric_exit = None
    try:
        runlog = (ROOT / "run.log").open("x", encoding="utf-8")
        runlog.write(json.dumps({"command": [sys.executable, str(Path(__file__).resolve())],
                                 "plan_sha256": digest(PLAN_PATH.read_bytes())}, sort_keys=True) + "\n")
        runlog.flush()
        stderr = (ROOT / "worker.stderr.log").open("x", encoding="utf-8")
        events = (ROOT / "events.jsonl").open("xb", buffering=0)
        env = {**os.environ, "PYTHONDONTWRITEBYTECODE": "1", "PYTHONUNBUFFERED": "1"}
        child = subprocess.Popen([sys.executable, str(Path(__file__).resolve()), "--worker"],
                                stdout=subprocess.PIPE, stderr=stderr, env=env,
                                start_new_session=True)
        deadline = started + limits["total_timeout_seconds"]
        phase_deadline = None
        phase_name = None
        next_sample = started
        linebuf = b""
        while True:
            now = time.monotonic()
            if now >= next_sample:
                rss, pids = process_tree_rss(os.getpid())
                free = free_bytes()
                used = allocated_bytes()
                observations.append({"elapsed_seconds": now - started, "sampled_process_tree_rss_bytes": rss,
                                    "sampled_process_tree_pids": pids, "filesystem_free_bytes": free,
                                    "owned_evidence_allocated_bytes": used})
                if rss >= limits["sampled_process_tree_rss_abort_bytes"]:
                    failure = "sampled process-tree RSS guard exceeded"
                if free < limits["minimum_filesystem_free_bytes"] + limits["early_stop_margin_bytes"]:
                    failure = "filesystem free-space guard crossed early-stop margin"
                if used > limits["owned_evidence_allocated_bytes_max"]:
                    failure = "owned evidence allocation cap exceeded"
                next_sample = now + limits["sample_interval_seconds"]
            if now > deadline:
                failure = "total watchdog exceeded"
            if phase_deadline is not None and now > phase_deadline:
                failure = f"phase watchdog exceeded: {phase_name}"
            if child.stdout is not None:
                import select
                ready, _, _ = select.select([child.stdout], [], [], 0.02)
                if ready:
                    block = os.read(child.stdout.fileno(), 65536)
                    if block:
                        events.write(block)
                        linebuf += block
                        while b"\n" in linebuf:
                            raw, linebuf = linebuf.split(b"\n", 1)
                            event = json.loads(raw)
                            if event.get("event") == "phase_start":
                                phase_name = event["phase"]
                                phase_deadline = time.monotonic() + limits["phase_timeout_seconds"]
                            elif event.get("event") == "phase_done":
                                phase_deadline = None
                                phase_name = None
                    elif child.poll() is not None:
                        break
            code = child.poll()
            if code is not None:
                numeric_exit = code
                break
            if failure:
                os.killpg(child.pid, signal.SIGTERM)
                try:
                    child.wait(timeout=2)
                except subprocess.TimeoutExpired:
                    os.killpg(child.pid, signal.SIGKILL)
                    child.wait()
                numeric_exit = child.returncode
                break
        if child.stdout is not None:
            remaining = child.stdout.read()
            if remaining:
                events.write(remaining)
        stderr.flush()
        events.flush()
        if failure is None and numeric_exit == 0 and (ROOT / "results.json").is_file():
            status = "PASS"
        else:
            status = "FAIL"
    except BaseException as exc:
        failure = f"supervisor exception: {type(exc).__name__}: {exc}"
        if child is not None and child.poll() is None:
            try:
                os.killpg(child.pid, signal.SIGTERM)
                child.wait(timeout=2)
            except Exception:
                try:
                    os.killpg(child.pid, signal.SIGKILL)
                    child.wait()
                except Exception:
                    pass
        numeric_exit = child.returncode if child is not None else None
        status = "FAIL"
        traceback.print_exc(file=sys.stderr)
    finally:
        if stderr:
            stderr.close()
        if events:
            events.close()
    record = {"status": status or "FAIL", "numeric_worker_exit": numeric_exit,
              "failure": failure, "plan_sha256": digest(PLAN_PATH.read_bytes()),
              "runner_sha256": digest(Path(__file__).read_bytes()),
              "elapsed_seconds": time.monotonic() - started, "observations": observations,
              "sampled_process_tree_rss_peak_bytes": max((o["sampled_process_tree_rss_bytes"] for o in observations), default=None),
              "filesystem_free_bytes_start": observations[0]["filesystem_free_bytes"] if observations else None,
              "filesystem_free_bytes_end": observations[-1]["filesystem_free_bytes"] if observations else None,
              "owned_evidence_allocated_bytes_before_supervisor_receipt": allocated_bytes(),
              "observation_scope": "periodic ps process-tree RSS samples, not an absolute transient RSS maximum; disk/RSS guards are observed samples"}
    if runlog:
        runlog.write(json.dumps({"status": record["status"], "numeric_worker_exit": numeric_exit,
                                 "elapsed_seconds": record["elapsed_seconds"], "failure": failure}, sort_keys=True) + "\n")
        runlog.close()
    projected_final = allocated_bytes() + ((len(json.dumps(record, sort_keys=True, separators=(",", ":"))) + 1 + 4095) // 4096) * 4096 + 8192
    cap = limits["owned_evidence_allocated_bytes_max"]
    record["predicted_allocated_bytes_after_supervisor_receipt"] = projected_final
    if projected_final > cap:
        record["status"] = "FAIL"
        record["failure"] = "predictive final evidence-cap guard exceeded"
    write_json_bounded(ROOT / "supervisor.json", record)
    print(json.dumps({"status": record["status"], "numeric_worker_exit": numeric_exit,
                      "elapsed_seconds": record["elapsed_seconds"], "failure": failure}, sort_keys=True))
    return 0 if record["status"] == "PASS" else 1


if __name__ == "__main__":
    if sys.argv[1:] == ["--worker"]:
        try:
            worker()
        except BaseException:
            traceback.print_exc()
            sys.exit(1)
    else:
        try:
            sys.exit(supervisor())
        except BaseException:
            traceback.print_exc()
            sys.exit(1)
