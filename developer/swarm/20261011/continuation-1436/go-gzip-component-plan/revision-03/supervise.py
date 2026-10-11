"""External guard for one compile+runtime invocation; no subprocess until admitted."""
import hashlib
import json
import os
from pathlib import Path
import selectors
import signal
import subprocess
import sys
import tempfile
import time

ROOT = Path(__file__).resolve().parent
GO = "/workspace/work/rcc-swarm/tools/go1.26.9/bin/go"
GO_SHA = "e90c4e12df236ea0c2e7536fd68da8a47fc0b585c19588bf6c1a70cfc296a0f9"
GOROOT = Path("/workspace/work/rcc-swarm/tools/go1.26.9")
GO_SOURCE_HASHES = {
    "src/compress/gzip/gzip.go": "9f5cef3dff42000c6df57de93429c10121c20afdda99b0c883fa60f94617a497",
    "src/compress/gzip/gunzip.go": "5886344b7fd61358d430872ab735653b16a314c87dab1f0bde4d8d9db0381ec4",
    "src/compress/flate/deflate.go": "4da50a0ab834a5e152ae337f9fea35f3527e29ea415e629dfe84749dfec07388",
    "src/compress/flate/inflate.go": "28b241ba6b66cc14ac93cc17a49756cd78daaade60afb47d7cd6aed2489dc5cb",
}
RAM_ROOT = Path("/dev/shm")
ADMISSION = "root-admitted-go-gzip-component-v1"
FREE_MIN = 2_688_548_864
RSS_MAX = 536_870_912
EVIDENCE_MAX = 1_048_576
EXEC_MAX = 8_388_608
RAM_MAX = 268_435_456
TOTAL_SECONDS = 120
PHASE_SECONDS = 30
SAMPLE_SECONDS = 0.1
LOG_MAX = 262_144


def sha(path):
    h = hashlib.sha256()
    with open(path, "rb") as f:
        for block in iter(lambda: f.read(65536), b""):
            h.update(block)
    return h.hexdigest()


def allocated(path):
    st = path.lstat()
    return st.st_blocks * 512


def tree_allocated(root):
    total = 0
    for p in [root, *root.rglob("*")]:
        try:
            if p.name == "rcc-gzip-go":
                continue
            total += allocated(p)
        except FileNotFoundError:
            continue
    return total


def ram_allocated(root):
    return sum(allocated(p) for p in [root, *root.rglob("*")] if p.exists())


def free_bytes():
    s = os.statvfs(ROOT)
    return s.f_bavail * s.f_frsize


def process_tree_rss(supervisor_pid, child_proc):
    child_pid = child_proc.pid
    cp = subprocess.run(["ps", "-eo", "pid=,ppid=,pgid=,stat=,rss="], capture_output=True, text=True, check=False, timeout=1)
    if cp.returncode != 0:
        raise RuntimeError(f"ps failed: {cp.returncode}: {cp.stderr.strip()}")
    rows = {}
    for line in cp.stdout.splitlines():
        fields = line.split()
        if len(fields) != 5:
            raise RuntimeError(f"malformed ps row: {line!r}")
        pid, ppid, pgid = map(int, fields[:3])
        rows[pid] = (ppid, pgid, fields[3], int(fields[4]) * 1024)
    if supervisor_pid not in rows:
        raise RuntimeError("supervisor PID missing from ps snapshot")
    child_live = child_proc.poll() is None
    if child_live and child_pid not in rows and child_proc.poll() is None:
        raise RuntimeError("live child PID missing from ps snapshot")
    pids = {supervisor_pid}
    # The child starts a new session, so its PGID also catches descendants
    # that outlive the Go driver briefly after its PID exits.
    pids.update(pid for pid, (_ppid, pgid, _state, _rss) in rows.items() if pgid == child_pid)
    changed = True
    while changed:
        changed = False
        for pid, (ppid, _pgid, _state, _rss) in rows.items():
            if ppid in pids and pid not in pids:
                pids.add(pid)
                changed = True
    live_group = any(pid in rows and not rows[pid][2].startswith("Z") for pid in pids if rows[pid][1] == child_pid)
    return sum(rows[p][3] for p in pids if p in rows), sorted(pids), live_group


def live_process_group(group_id):
    cp = subprocess.run(["ps", "-eo", "pid=,pgid=,stat="], capture_output=True, text=True, check=False, timeout=1)
    if cp.returncode != 0:
        raise RuntimeError(f"ps process-group query failed: {cp.returncode}")
    active = []
    for line in cp.stdout.splitlines():
        fields = line.split()
        if len(fields) != 3:
            raise RuntimeError(f"malformed process-group ps row: {line!r}")
        pid, pgid = int(fields[0]), int(fields[1])
        if pgid == group_id and not fields[2].startswith("Z"):
            active.append(pid)
    return active


def read_phase_receipt(path):
    if not path.exists():
        return [], "phase event file missing"
    raw = path.read_bytes()
    if raw and not raw.endswith(b"\n"):
        return [], "phase event file ended with a partial record"
    parsed = [json.loads(line) for line in raw.splitlines() if line]
    expected = {(p, r) for p in ("legacy-gzip-decode", "gzip-bestspeed-encode", "gzip-bestspeed-decode") for r in range(5)}
    starts = {}
    normalized = []
    for event in parsed:
        state = event.get("state")
        key = (event.get("phase"), event.get("repetition"))
        tick = int(event.get("unix_nano", 0))
        if state == "phase_start":
            if key not in expected or key in starts:
                return [], f"invalid/duplicate phase start: {key}"
            starts[key] = tick
            normalized.append({"state": state, "phase": key[0], "repetition": key[1], "marker_unix_nano": tick})
        elif state == "phase_done":
            if key not in starts:
                return [], f"phase completion without start: {key}"
            duration_ns = tick - starts.pop(key)
            if duration_ns < 0 or duration_ns > PHASE_SECONDS * 1_000_000_000:
                return [], f"phase duration invalid/exceeded: {key} {duration_ns}"
            normalized.append({"state": state, "phase": key[0], "repetition": key[1],
                               "marker_unix_nano": tick, "duration_ns": duration_ns})
        else:
            return [], f"unexpected phase state: {state!r}"
    if starts or {(e["phase"], e["repetition"]) for e in normalized if e["state"] == "phase_start"} != expected or len(normalized) != 30:
        return [], "phase event coverage incomplete"
    return normalized, None


def write_json_exclusive(path, data):
    encoded = (json.dumps(data, sort_keys=True, indent=2) + "\n").encode()
    predicted = tree_allocated(ROOT) + ((len(encoded) + 4095) // 4096) * 4096
    if predicted > EVIDENCE_MAX:
        raise RuntimeError(f"evidence allocation cap would be exceeded: {predicted}")
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    with os.fdopen(fd, "wb") as f:
        f.write(encoded)


class GuardFailure(RuntimeError):
    pass


def run_child(argv, env, stage, started, logf, record):
    proc = subprocess.Popen(argv, cwd=ROOT, env=env, stdin=subprocess.DEVNULL,
                            stdout=subprocess.PIPE, stderr=subprocess.STDOUT,
                            start_new_session=True, bufsize=0)
    record[stage + "_pid"] = proc.pid
    os.set_blocking(proc.stdout.fileno(), False)
    sel = selectors.DefaultSelector()
    sel.register(proc.stdout, selectors.EVENT_READ)
    phase_active = None
    phase_event_offset = 0
    phase_event_pending = b""
    record.setdefault("phase_events", [])
    peak_rss = 0
    min_free = None
    peak_ram = 0
    peak_evidence = 0
    failure = None
    try:
        while True:
            now = time.monotonic()
            if now - started > TOTAL_SECONDS:
                failure = f"total watchdog exceeded during {stage}"
                break
            try:
                free = free_bytes()
                min_free = free if min_free is None else min(min_free, free)
                if free < FREE_MIN:
                    failure = f"free-space floor breached during {stage}: {free}"
                    break
                rtree, pids, live_group = process_tree_rss(os.getpid(), proc)
                peak_rss = max(peak_rss, rtree)
                record[stage + "_last_process_tree_pids"] = pids
                if rtree > RSS_MAX:
                    failure = f"sampled process-tree RSS exceeded during {stage}: {rtree}"
                    break
                ram = ram_allocated(Path(record["owned_ram_temp"]))
                peak_ram = max(peak_ram, ram)
                if ram > RAM_MAX:
                    failure = f"owned RAM temp cap exceeded during {stage}: {ram}"
                    break
                evidence = tree_allocated(ROOT)
                peak_evidence = max(peak_evidence, evidence)
                if evidence > EVIDENCE_MAX:
                    failure = f"evidence cap exceeded during {stage}: {evidence}"
                    break
                executable = ROOT / "rcc-gzip-go"
                if executable.exists() and executable.stat().st_size > EXEC_MAX:
                    failure = f"executable cap exceeded: {executable.stat().st_size}"
                    break
                if stage == "runtime":
                    event_path = ROOT / "phase-events.jsonl"
                    if event_path.exists():
                        with open(event_path, "rb") as events_file:
                            events_file.seek(phase_event_offset)
                            new = events_file.read()
                            phase_event_offset += len(new)
                        chunks = (phase_event_pending + new).split(b"\n")
                        phase_event_pending = chunks.pop()
                        for raw_event in chunks:
                            if not raw_event:
                                continue
                            try:
                                m = json.loads(raw_event)
                            except Exception as exc:
                                failure = f"invalid phase event: {exc}"
                                break
                            state = m.get("state")
                            key = (m.get("phase"), m.get("repetition"))
                            if state == "phase_start":
                                if phase_active is not None:
                                    failure = f"nested or missing phase completion: {key}"
                                    break
                                elapsed_since_start = max(0.0, (time.time_ns() - int(m["unix_nano"])) / 1e9)
                                phase_active = (key, now - elapsed_since_start, int(m["unix_nano"]))
                                record["phase_events"].append({"state": state, "phase": key[0],
                                    "repetition": key[1], "marker_unix_nano": int(m["unix_nano"]),
                                    "observed_monotonic_seconds": now - started})
                            elif state == "phase_done":
                                if phase_active is None or phase_active[0] != key:
                                    failure = f"phase completion without matching start: {key}"
                                    break
                                duration_ns = int(m["unix_nano"]) - phase_active[2]
                                record["phase_events"].append({"state": state, "phase": key[0],
                                    "repetition": key[1], "marker_unix_nano": int(m["unix_nano"]),
                                    "duration_ns": duration_ns,
                                    "observed_monotonic_seconds": now - started})
                                if duration_ns > PHASE_SECONDS * 1_000_000_000:
                                    failure = f"phase watchdog exceeded: {key}, {duration_ns} ns"
                                    break
                                phase_active = None
                            else:
                                failure = f"unexpected phase event state: {state!r}"
                                break
                        if failure is not None:
                            break
                    if phase_active is not None and now - phase_active[1] > PHASE_SECONDS:
                        failure = f"phase watchdog exceeded while active: {phase_active[0]}"
                        break
            except Exception as exc:
                failure = f"resource observer failed during {stage}: {exc}"
                break
            ready = sel.select(SAMPLE_SECONDS)
            if ready:
                try:
                    chunk = os.read(proc.stdout.fileno(), 65536)
                except BlockingIOError:
                    chunk = b""
                if chunk:
                    if logf.tell() + len(chunk) > LOG_MAX:
                        failure = f"captured log cap exceeded during {stage}"
                        break
                    logf.write(chunk)
                    logf.flush()
            code = proc.poll()
            if code is not None and not live_group:
                # Drain any final bytes without waiting for an unbounded descendant-held pipe.
                try:
                    while True:
                        chunk = os.read(proc.stdout.fileno(), 65536)
                        if not chunk:
                            break
                        if logf.tell() + len(chunk) > LOG_MAX:
                            failure = f"captured log cap exceeded while draining {stage}"
                            break
                        logf.write(chunk)
                except (BlockingIOError, OSError):
                    pass
                break
            if code is not None and live_group:
                # Keep guarding descendants in the child's process group after
                # the Go driver exits; the child group must fully drain.
                time.sleep(SAMPLE_SECONDS)
        if failure is not None and proc.poll() is None:
            try:
                os.killpg(proc.pid, signal.SIGTERM)
            except ProcessLookupError:
                pass
            try:
                proc.wait(timeout=5)
            except subprocess.TimeoutExpired:
                try:
                    os.killpg(proc.pid, signal.SIGKILL)
                except ProcessLookupError:
                    pass
                proc.wait(timeout=5)
        if failure is not None:
            reap_deadline = time.monotonic() + 5
            while True:
                try:
                    live = live_process_group(proc.pid)
                except Exception as exc:
                    record[stage + "_group_reap_failure"] = str(exc)
                    break
                if not live:
                    break
                if time.monotonic() >= reap_deadline:
                    try:
                        os.killpg(proc.pid, signal.SIGKILL)
                    except ProcessLookupError:
                        pass
                    record[stage + "_group_kill_escalated"] = True
                    break
                time.sleep(0.05)
        code = proc.wait()
        if stage == "runtime":
            try:
                phase_events, phase_error = read_phase_receipt(ROOT / "phase-events.jsonl")
                record["phase_events"] = phase_events
                if phase_error:
                    failure = failure or phase_error
            except Exception as exc:
                failure = failure or f"phase event receipt invalid: {exc}"
        record[stage + "_exit_code"] = code
        record[stage + "_failure"] = failure
        record[stage + "_sampled_process_tree_rss_peak_bytes"] = peak_rss
        record[stage + "_minimum_free_bytes"] = min_free
        record[stage + "_owned_ram_temp_peak_allocated_bytes"] = peak_ram
        record[stage + "_evidence_peak_allocated_bytes"] = peak_evidence
        return code, failure
    finally:
        sel.close()
        proc.stdout.close()


def main():
    if os.environ.get("RCC125_GO_GZIP_ADMISSION") != ADMISSION:
        print("execution blocked: explicit root admission required", file=sys.stderr)
        return 64
    started = time.monotonic()
    now_free = free_bytes()
    if now_free < FREE_MIN:
        print(f"execution blocked: free bytes {now_free} below guarded floor {FREE_MIN}", file=sys.stderr)
        return 65
    if tree_allocated(ROOT) > EVIDENCE_MAX:
        print("execution blocked: evidence directory exceeds cap", file=sys.stderr)
        return 66
    if sha(GO) != GO_SHA:
        print("execution blocked: official Go binary hash mismatch", file=sys.stderr)
        return 68
    for rel, expected in GO_SOURCE_HASHES.items():
        if sha(GOROOT / rel) != expected:
            print(f"execution blocked: Go standard-library source changed: {rel}", file=sys.stderr)
            return 69
    binary = ROOT / "rcc-gzip-go"
    for existing in (binary, ROOT / "run.log", ROOT / "phase.json", ROOT / "phase.json.tmp",
                     ROOT / "phase-events.jsonl", ROOT / "results.json", ROOT / "launch.json"):
        if existing.exists():
            print(f"execution blocked: refusing existing output {existing.name}", file=sys.stderr)
            return 67
    ramtemp = Path(tempfile.mkdtemp(prefix="rcc125-go-gzip-r2-", dir=RAM_ROOT))
    (ramtemp / "gocache").mkdir()
    (ramtemp / "gotmp").mkdir()
    env = os.environ.copy()
    env.update({"GOTOOLCHAIN": "local", "GO111MODULE": "off", "GOWORK": "off",
                "GOPROXY": "off", "GOCACHE": str(ramtemp / "gocache"),
                "GOTMPDIR": str(ramtemp / "gotmp"), "GOMEMLIMIT": "320MiB",
                "GOMAXPROCS": "2", "CGO_ENABLED": "0", "GOOS": "linux",
                "GOARCH": "amd64", "GOFLAGS": ""})
    record = {"status": "RUNNING", "single_invocation": True,
              "build_environment": {k: env[k] for k in ("GOTOOLCHAIN", "GO111MODULE", "GOWORK", "GOPROXY", "GOCACHE", "GOTMPDIR", "GOMEMLIMIT", "GOMAXPROCS", "CGO_ENABLED", "GOOS", "GOARCH", "GOFLAGS")},
              "started_utc_ns": time.time_ns(), "owned_ram_temp": str(ramtemp),
              "admission_token_seen": True, "go_version_command": [GO, "version"],
              "build_command": [GO, "build", "-p", "1", "-o", "./rcc-gzip-go", "./main.go"],
              "metadata_command": [GO, "version", "-m", "./rcc-gzip-go"],
              "runtime_command": ["./rcc-gzip-go", "--plan", "./plan.json", "--phase-marker", "./phase.json", "--output", "./results.json"],
              "free_floor_bytes": FREE_MIN, "rss_limit_bytes": RSS_MAX,
              "evidence_limit_allocated_bytes": EVIDENCE_MAX,
              "executable_limit_bytes": EXEC_MAX, "ram_temp_limit_allocated_bytes": RAM_MAX,
              "total_timeout_seconds": TOTAL_SECONDS, "phase_timeout_seconds": PHASE_SECONDS,
              "sample_interval_seconds": SAMPLE_SECONDS, "retry_performed": False}
    record["input_hashes_before"] = {name: sha(ROOT / name) for name in ("plan.json", "main.go", "supervise.py", "run.sh", "test_supervise.py")}
    record["go_binary_sha256"] = GO_SHA
    record["go_stdlib_source_sha256"] = {rel: sha(GOROOT / rel) for rel in GO_SOURCE_HASHES}
    plan = json.loads((ROOT / "plan.json").read_text())
    rcc_files = plan["implementation_scope"]["rcc_source_sha256"]
    rcc_root = Path(plan["source"]["repository"])
    record["rcc_source_sha256_before"] = {rel: sha(rcc_root / rel) for rel in rcc_files}
    if record["rcc_source_sha256_before"] != rcc_files:
        print("execution blocked: main727 source hash mismatch", file=sys.stderr)
        return 71
    index_path = Path(plan["source"]["index_path"])
    manifest_path = Path(plan["source"]["manifest_path"])
    record["fixture_preflight"] = {
        "index_size": index_path.stat().st_size, "index_sha256": sha(index_path),
        "manifest_size": manifest_path.stat().st_size, "manifest_sha256": sha(manifest_path),
    }
    if (record["fixture_preflight"]["index_size"] != plan["source"]["index_bytes"] or
            record["fixture_preflight"]["index_sha256"] != plan["source"]["index_sha256"] or
            record["fixture_preflight"]["manifest_size"] != plan["source"]["manifest_bytes"] or
            record["fixture_preflight"]["manifest_sha256"] != plan["source"]["manifest_sha256"]):
        print("execution blocked: fixture index/manifest preflight mismatch", file=sys.stderr)
        return 70
    final_failure = None
    try:
        with open(ROOT / "run.log", "xb", buffering=0) as logf:
            go_version = subprocess.run([GO, "version"], cwd=ROOT, env=env,
                                        capture_output=True, text=True, timeout=5, check=False)
            record["go_version_exit_code"] = go_version.returncode
            record["go_version_stdout"] = go_version.stdout.strip()
            record["go_version_stderr"] = go_version.stderr.strip()
            if go_version.returncode != 0 or go_version.stdout.strip() != "go version go1.26.9 linux/amd64":
                final_failure = "official Go 1.26.9 identity check failed"
            if final_failure is None:
                code, final_failure = run_child(record["build_command"], env, "build", started, logf, record)
                if final_failure is None and code != 0:
                    final_failure = f"Go build failed with exit {code}"
            if final_failure is None:
                if not binary.is_file():
                    final_failure = "build returned success but executable missing"
                elif binary.stat().st_size > EXEC_MAX:
                    final_failure = f"executable exceeds cap: {binary.stat().st_size}"
                else:
                    record["executable_size_bytes"] = binary.stat().st_size
                    record["executable_sha256"] = sha(binary)
                    os.chmod(binary, 0o700)
            if final_failure is None:
                metadata_offset = logf.tell()
                code, final_failure = run_child(record["metadata_command"], env, "metadata", started, logf, record)
                metadata_size = logf.tell() - metadata_offset
                metadata_text = os.pread(logf.fileno(), metadata_size, metadata_offset).decode("utf-8", "replace")
                record["compiled_metadata_output"] = metadata_text
                required_metadata = {"build CGO_ENABLED=0", "build GOARCH=amd64", "build GOOS=linux"}
                present_metadata = {" ".join(line.split()) for line in metadata_text.splitlines()}
                record["compiled_metadata"] = sorted(required_metadata & present_metadata)
                record["compiled_go_version_verified"] = any("go1.26.9" in line for line in metadata_text.splitlines())
                if final_failure is None and code != 0:
                    final_failure = f"compiled metadata command failed with exit {code}"
                if final_failure is None and (not record["compiled_go_version_verified"] or present_metadata & required_metadata != required_metadata):
                    final_failure = "compiled binary Go/GOOS/GOARCH/CGO metadata mismatch"
            if final_failure is None:
                code, final_failure = run_child(record["runtime_command"], env, "runtime", started, logf, record)
                if final_failure is None and code != 0:
                    final_failure = f"Go runtime returned exit {code}"
            record["runtime_results_present"] = (ROOT / "results.json").is_file()
            if record["runtime_results_present"]:
                result_path = ROOT / "results.json"
                result = json.loads(result_path.read_text())
                record["runtime_results_sha256"] = sha(result_path)
                record["runtime_result_status"] = result.get("status")
                record["runtime_sample_count"] = len(result.get("samples", []))
                if result.get("status") != "PASS" or len(result.get("samples", [])) != 300:
                    final_failure = final_failure or "runtime result acceptance fields failed"
                expected_calls = {"legacy-gzip-decode": 100, "gzip-bestspeed-encode": 100,
                                  "gzip-bestspeed-decode": 100}
                if result.get("calls_by_phase") != expected_calls:
                    final_failure = final_failure or "runtime phase call counts mismatch"
                if len(result.get("canonical_frames", {})) != 20:
                    final_failure = final_failure or "canonical frame receipts incomplete"
                if result.get("source_before") != result.get("source_after"):
                    final_failure = final_failure or "stored object pre/post receipts differ"
                if result.get("index_before") != result.get("index_after") or result.get("manifest_before") != result.get("manifest_after"):
                    final_failure = final_failure or "index/manifest pre/post receipts differ"
                plan_sha = sha(ROOT / "plan.json")
                runner_sha = sha(ROOT / "main.go")
                if result.get("plan_sha256") != plan_sha or result.get("runner_sha256") != runner_sha:
                    final_failure = final_failure or "runtime source receipt hashes mismatch"
                if result.get("go_version") != "go1.26.9":
                    final_failure = final_failure or "runtime Go version mismatch"
                selected = plan["source"]["objects"]
                expected_digests = {entry["storedDigest"] for entry in selected}
                if (result.get("selected_objects") != 20 or
                        result.get("selected_logical_bytes") != plan["source"]["selected_logical_bytes"] or
                        result.get("selected_stored_bytes") != plan["source"]["selected_stored_bytes"] or
                        set(result.get("source_before", {})) != expected_digests or
                        set(result.get("source_after", {})) != expected_digests or
                        set(result.get("canonical_frames", {})) != expected_digests):
                    final_failure = final_failure or "runtime input/source receipt coverage mismatch"
            phase_events = record.get("phase_events", [])
            starts = [e for e in phase_events if e.get("state") == "phase_start"]
            dones = [e for e in phase_events if e.get("state") == "phase_done"]
            expected = {(phase, rep) for phase in ("legacy-gzip-decode", "gzip-bestspeed-encode", "gzip-bestspeed-decode") for rep in range(5)}
            got_starts = {(e.get("phase"), e.get("repetition")) for e in starts}
            got_dones = {(e.get("phase"), e.get("repetition")) for e in dones}
            if got_starts != expected or got_dones != expected or len(starts) != 15 or len(dones) != 15:
                final_failure = final_failure or "runtime phase-marker coverage incomplete"
            if record.get("build_exit_code") != 0:
                final_failure = final_failure or "build exit not zero"
            if record.get("runtime_exit_code") != 0:
                final_failure = final_failure or "runtime exit not zero"
            record["input_hashes_after"] = {name: sha(ROOT / name) for name in ("plan.json", "main.go", "supervise.py", "run.sh", "test_supervise.py")}
            if record["input_hashes_after"] != record["input_hashes_before"]:
                final_failure = final_failure or "plan/source/launcher changed during invocation"
            record["rcc_source_sha256_after"] = {rel: sha(rcc_root / rel) for rel in rcc_files}
            if record["rcc_source_sha256_after"] != record["rcc_source_sha256_before"]:
                final_failure = final_failure or "main727 RCC source changed during invocation"
            record["go_stdlib_source_sha256_after"] = {rel: sha(GOROOT / rel) for rel in GO_SOURCE_HASHES}
            if record["go_stdlib_source_sha256_after"] != record["go_stdlib_source_sha256"]:
                final_failure = final_failure or "Go standard-library source changed during invocation"
            if binary.is_file() and sha(binary) != record.get("executable_sha256"):
                final_failure = final_failure or "compiled executable hash changed during runtime"
            if len(record.get("phase_events", [])) != 30 or any(e.get("duration_ns", 0) > PHASE_SECONDS * 1_000_000_000 for e in record.get("phase_events", [])):
                final_failure = final_failure or "phase watchdog/event receipt incomplete"
            if free_bytes() < FREE_MIN:
                final_failure = final_failure or "final free-space floor failed"
            if ram_allocated(ramtemp) > RAM_MAX:
                final_failure = final_failure or "final RAM temp cap failed"
            if tree_allocated(ROOT) > EVIDENCE_MAX:
                final_failure = final_failure or "final evidence cap failed"
            record["finished_utc_ns"] = time.time_ns()
            record["elapsed_seconds"] = time.monotonic() - started
            if record["elapsed_seconds"] > TOTAL_SECONDS:
                final_failure = final_failure or "overall build+runtime+verification timeout exceeded"
            record["final_free_bytes"] = free_bytes()
            record["final_ram_temp_allocated_bytes"] = ram_allocated(ramtemp)
            record["final_evidence_allocated_bytes"] = tree_allocated(ROOT)
            record["status"] = "PASS" if final_failure is None else "FAIL"
            record["failure"] = final_failure
            write_json_exclusive(ROOT / "launch.json", record)
    except Exception as exc:
        print(f"supervisor failed: {type(exc).__name__}: {exc}", file=sys.stderr)
        return 1
    print(json.dumps({"status": record["status"], "build_exit_code": record.get("build_exit_code"),
                      "runtime_exit_code": record.get("runtime_exit_code"), "failure": record["failure"]}, sort_keys=True))
    return 0 if record["status"] == "PASS" else 1


if __name__ == "__main__":
    raise SystemExit(main())
