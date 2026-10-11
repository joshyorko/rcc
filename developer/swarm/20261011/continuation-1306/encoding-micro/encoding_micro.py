#!/usr/bin/env python3
"""Disposable external codec experiment. Never writes an RCC object or payload."""
import gc
import gzip
import hashlib
import json
import os
from pathlib import Path
import resource
import selectors
import signal
import subprocess
import sys
import time
import traceback
import zlib

ROOT = Path(__file__).resolve().parent
PLAN_PATH = ROOT / "plan.json"


def emit(value):
    print(json.dumps(value, sort_keys=True), flush=True)


def sha(data):
    return hashlib.sha256(data).hexdigest()


def owned_bytes():
    return sum(p.lstat().st_blocks * 512 for p in ROOT.rglob("*") if p.is_file())


def process_info(pid):
    result = {}
    try:
        for line in Path(f"/proc/{pid}/status").read_text().splitlines():
            if line.startswith("VmRSS:"):
                result["rss_bytes"] = int(line.split()[1]) * 1024
        result["io"] = {
            k: int(v) for k, v in (
                line.split(":", 1) for line in Path(f"/proc/{pid}/io").read_text().splitlines()
            )
        }
    except FileNotFoundError:
        pass
    return result


def worker():
    import zstandard

    plan = json.loads(PLAN_PATH.read_text())
    fixture = plan["fixture"]
    index_bytes = Path(fixture["index_file"]).read_bytes()
    assert sha(index_bytes) == fixture["index_descriptor"]["digest"].split(":")[1]
    assert len(index_bytes) == fixture["index_descriptor"]["size"]
    index = json.loads(index_bytes)
    indexed = {entry["legacyObjectId"]: entry for entry in index["entries"]}
    manifest_bytes = Path(fixture["manifest_file"]).read_bytes()
    assert sha(manifest_bytes) == fixture["manifest_sha256"]
    emit({"event": "stage-start", "stage": "untimed-original-validation"})
    payloads, originals, validation = [], [], []
    for entry in fixture["selected"]:
        original_entry = {k: v for k, v in entry.items() if k not in ("sample_bucket", "eligible_sorted_position")}
        assert original_entry == indexed[entry["legacyObjectId"]]
        digest = entry["storedDigest"].split(":")[1]
        path = Path(fixture["provider_root"]) / "objects/sha256" / digest[:2] / digest[2:4] / digest
        assert not path.is_symlink() and path.is_file()
        stored = path.read_bytes()
        assert len(stored) == entry["storedSize"] and sha(stored) == digest
        logical = gzip.decompress(stored)
        assert len(logical) == entry["logicalSize"] and sha(logical) == entry["legacyObjectId"]
        payloads.append(logical)
        originals.append(stored)
        validation.append({"legacyObjectId": entry["legacyObjectId"], "storedDigest": entry["storedDigest"],
                           "storedSize": len(stored), "logicalSize": len(logical), "source_valid": True})

    def encode_gzip(data):
        return zlib.compress(data, level=1, wbits=31)

    def encode_zstd(data):
        return zstandard.ZstdCompressor(level=3, threads=0, write_content_size=True,
                                       write_checksum=True).compress(data)

    def decode_zstd(data):
        return zstandard.ZstdDecompressor().decompress(data)

    gzip_frames = [encode_gzip(data) for data in payloads]
    zstd_frames = [encode_zstd(data) for data in payloads]
    frame_details = []
    for ix, logical in enumerate(payloads):
        assert gzip.decompress(gzip_frames[ix]) == logical
        assert decode_zstd(zstd_frames[ix]) == logical
        frame_details.append({"legacyObjectId": fixture["selected"][ix]["legacyObjectId"],
                              "original_gzip_bytes": len(originals[ix]),
                              "external_gzip_bytes": len(gzip_frames[ix]), "external_gzip_sha256": sha(gzip_frames[ix]),
                              "zstd_bytes": len(zstd_frames[ix]), "zstd_sha256": sha(zstd_frames[ix])})
    emit({"event": "prepared", "original_validation_count": len(validation),
          "logical_bytes": sum(map(len, payloads)), "payloads_written_to_disk": False})
    phases = {
        "encode-gzip": (payloads, encode_gzip, gzip.decompress),
        "encode-zstd": (payloads, encode_zstd, decode_zstd),
        "decode-legacy-gzip": (originals, gzip.decompress, None),
        "decode-external-gzip": (gzip_frames, gzip.decompress, None),
        "decode-zstd": (zstd_frames, decode_zstd, None),
    }
    samples = []
    for repetition, order in enumerate(plan["schedule"]):
        for phase in order:
            source, operation, roundtrip = phases[phase]
            emit({"event": "stage-start", "stage": phase, "repetition": repetition})
            gc.collect()
            output = []
            start_cpu = time.process_time_ns()
            start_wall = time.perf_counter_ns()
            for _ in range(plan["passes_per_sample"]):
                for data in source:
                    output.append(operation(data))
            end_wall = time.perf_counter_ns()
            end_cpu = time.process_time_ns()
            output_bytes = sum(map(len, output))
            for ix, encoded_or_logical in enumerate(output):
                sample_ix = ix % len(payloads)
                logical = roundtrip(encoded_or_logical) if roundtrip else encoded_or_logical
                expected = fixture["selected"][sample_ix]
                assert len(logical) == expected["logicalSize"] and sha(logical) == expected["legacyObjectId"]
                if phase == "encode-gzip":
                    assert sha(encoded_or_logical) == frame_details[sample_ix]["external_gzip_sha256"]
                elif phase == "encode-zstd":
                    assert sha(encoded_or_logical) == frame_details[sample_ix]["zstd_sha256"]
            sample = {"phase": phase, "repetition": repetition, "wall_ns": end_wall - start_wall,
                      "process_cpu_ns": end_cpu - start_cpu, "operations": len(output),
                      "logical_bytes_processed": plan["logical_bytes_per_timed_sample"],
                      "output_bytes": output_bytes, "verified_outputs": len(output),
                      "hash_and_roundtrip_verification_inside_timer": False}
            samples.append(sample)
            emit({"event": "sample", **sample})
            del output
    result = {"status": "PASS", "plan_sha256": sha(PLAN_PATH.read_bytes()),
              "script_sha256": sha(Path(__file__).read_bytes()), "original_validation": validation,
              "frame_details": frame_details, "samples": samples,
              "ru_maxrss_kib_whole_worker": resource.getrusage(resource.RUSAGE_SELF).ru_maxrss,
              "semantic_gate": "Every one of 12000 timed outputs verified outside its batch timer; encoded outputs roundtrip to exact original logical SHA256 and size",
              "limits": "external CPython codecs and <=1MiB eligible sample only; no Go encoder equivalence or E2E inference"}
    (ROOT / "results.json").write_text(json.dumps(result, indent=2, sort_keys=True) + "\n")
    emit({"event": "complete", "status": "PASS", "samples": len(samples),
          "verified_outputs": sum(sample["verified_outputs"] for sample in samples)})


def supervise():
    plan = json.loads(PLAN_PATH.read_text())
    limits = plan["limits"]
    before_free = os.statvfs(ROOT).f_bavail * os.statvfs(ROOT).f_frsize
    assert before_free >= limits["minimum_filesystem_free_bytes"]
    assert owned_bytes() <= limits["new_owned_allocated_bytes_max"]
    env = {**os.environ, "PYTHONDONTWRITEBYTECODE": "1", "PYTHONUNBUFFERED": "1"}
    command = [sys.executable, str(Path(__file__).resolve()), "--worker"]
    samples, failure, usage, exit_code = [], None, None, None
    started = stage_started = time.monotonic()
    with (ROOT / "worker.stderr.log").open("w") as stderr, (ROOT / "events.jsonl").open("wb") as events:
        child = subprocess.Popen(command, env=env, stdout=subprocess.PIPE, stderr=stderr, start_new_session=True)
        selector = selectors.DefaultSelector()
        selector.register(child.stdout, selectors.EVENT_READ)
        buffer = b""
        next_process = next_disk = started
        forced = False
        while exit_code is None:
            now = time.monotonic()
            for key, _ in selector.select(timeout=0.05):
                block = os.read(key.fd, 65536)
                if not block:
                    selector.unregister(key.fileobj)
                    continue
                events.write(block)
                events.flush()
                buffer += block
                while b"\n" in buffer:
                    line, buffer = buffer.split(b"\n", 1)
                    event = json.loads(line)
                    if event.get("event") == "stage-start":
                        stage_started = time.monotonic()
            info = None
            if now >= next_process:
                info = process_info(child.pid)
                samples.append({"monotonic_ns": time.monotonic_ns(), **info})
                next_process = now + limits["process_sampling_interval_seconds"]
                if info.get("rss_bytes", 0) >= limits["worker_RSS_abort_bytes"]:
                    failure = "worker RSS guard exceeded"
            if now >= next_disk:
                free = os.statvfs(ROOT).f_bavail * os.statvfs(ROOT).f_frsize
                owned = owned_bytes()
                samples.append({"monotonic_ns": time.monotonic_ns(), "free_bytes": free, "owned_allocated_bytes": owned})
                next_disk = now + limits["disk_guard_interval_seconds"]
                if free < limits["minimum_filesystem_free_bytes"] or owned > limits["new_owned_allocated_bytes_max"]:
                    failure = "disk guard exceeded"
            if now - started > limits["total_timeout_seconds"]:
                failure = "whole experiment watchdog exceeded"
            elif now - stage_started > limits["stage_timeout_seconds"]:
                failure = "stage watchdog exceeded"
            if failure and not forced:
                os.kill(child.pid, signal.SIGTERM)
                forced = True
                forced_at = now
            elif forced and now - forced_at > 2:
                try:
                    os.kill(child.pid, signal.SIGKILL)
                except ProcessLookupError:
                    pass
            pid, status, rusage = os.wait4(child.pid, os.WNOHANG)
            if pid:
                exit_code = os.waitstatus_to_exitcode(status)
                usage = rusage
                child.returncode = exit_code
        remaining = child.stdout.read()
        if remaining:
            events.write(remaining)
    final_free = os.statvfs(ROOT).f_bavail * os.statvfs(ROOT).f_frsize
    record = {"status": "PASS" if exit_code == 0 and failure is None else "FAIL", "numeric_worker_exit": exit_code,
              "failure": failure, "command": command, "started_unix_ns": time.time_ns() - int((time.monotonic() - started) * 1e9),
              "outer_wall_ns": int((time.monotonic() - started) * 1e9), "plan_sha256": sha(PLAN_PATH.read_bytes()),
              "script_sha256": sha(Path(__file__).read_bytes()), "sampled_observations": samples,
              "before_free_bytes": before_free, "final_free_bytes": final_free,
              "final_owned_allocated_bytes": owned_bytes(),
              "wait4_whole_worker": {"user_seconds": usage.ru_utime, "system_seconds": usage.ru_stime,
                                    "maxrss_kib": usage.ru_maxrss, "inblock": usage.ru_inblock, "outblock": usage.ru_oublock},
              "observer_scope": "separate supervisor proc/disk samples outside worker codec timers; maxRSS whole worker not per phase; process IO observations include untimed prepare/validation/reporting"}
    (ROOT / "supervisor-result.json").write_text(json.dumps(record, indent=2, sort_keys=True) + "\n")
    print(json.dumps({k: record[k] for k in ["status", "numeric_worker_exit", "outer_wall_ns", "final_free_bytes", "final_owned_allocated_bytes"]}))
    return 0 if record["status"] == "PASS" else 1


if __name__ == "__main__":
    if sys.argv[1:] == ["--worker"]:
        try:
            worker()
        except BaseException:
            traceback.print_exc()
            sys.exit(1)
    else:
        sys.exit(supervise())
