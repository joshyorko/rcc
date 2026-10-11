#!/usr/bin/env python3
"""One bounded current-head #181 race gate; no retries or cleanup."""
import hashlib
import json
import os
from pathlib import Path
import selectors
import signal
import subprocess
import sys
import time
import uuid

REPO = Path('/workspace/work/rcc-swarm/scale-181-current')
EVIDENCE = Path(__file__).resolve().parent
GO = Path('/workspace/work/rcc-swarm/tools/go1.26.9/bin/go')
GO_SHA = 'e90c4e12df236ea0c2e7536fd68da8a47fc0b585c19588bf6c1a70cfc296a0f9'
EXPECTED_HEAD = 'fa464a23ce5bc8c2b903bfc62d53b71205379812'
EXPECTED_MAIN = '7945f956b428359928908a9f42e36417b113dafd'
EXPECTED_CHECKPOINT = 'e53624a359c1e73a6ef01e5daf7c2138b693d3a3'
EXPECTED_BASE = '727c1ff8b679cdbbe12836e53c87734a1edb0071'
FREE_MIN = 2688548864
RSS_MAX = 536870912
CACHE_MAX = 268435456
SCRATCH_MAX = 67108864
TIMEOUT = 120
SAMPLE = 0.25
LOG_MAX = 4 * 1024 * 1024
TESTS = (
    'TestAutoScaleGrowsToConfiguredWorkerCount',
    'TestScaleMatchesConcurrentWorkerCapacity',
    'TestSyncDrainsTasksQueuedWhilePriorWorkIsHeld',
    'TestWorkerPoolSurvivesPanicsFromEveryWorker',
)


def sha(path):
    h = hashlib.sha256()
    with path.open('rb') as f:
        for chunk in iter(lambda: f.read(1024 * 1024), b''):
            h.update(chunk)
    return h.hexdigest()


def git(*args):
    p = subprocess.run(['git', '-C', str(REPO), *args], capture_output=True, text=True, check=True)
    return p.stdout.strip()


def allocated(root):
    total = 0
    for p in (root, *root.rglob('*')):
        try:
            total += p.lstat().st_blocks * 512
        except FileNotFoundError:
            pass
    return total


def free_bytes():
    st = os.statvfs(REPO)
    return st.f_bavail * st.f_frsize


def process_rss(supervisor_pid, child):
    cp = subprocess.run(['ps', '-eo', 'pid=,ppid=,pgid=,stat=,rss='], capture_output=True, text=True, timeout=2)
    if cp.returncode:
        raise RuntimeError(f'ps exited {cp.returncode}: {cp.stderr.strip()}')
    rows = {}
    for line in cp.stdout.splitlines():
        f = line.split()
        if len(f) != 5:
            raise RuntimeError(f'malformed ps row: {line!r}')
        pid, ppid, pgid = map(int, f[:3])
        rows[pid] = (ppid, pgid, f[3], int(f[4]) * 1024)
    if supervisor_pid not in rows:
        raise RuntimeError('supervisor PID missing from ps')
    live = child.poll() is None
    if live and child.pid not in rows and child.poll() is None:
        raise RuntimeError('live Go-test PID missing from ps')
    pids = {supervisor_pid}
    pids.update(pid for pid, (_ppid, pgid, _state, _rss) in rows.items() if pgid == child.pid)
    changed = True
    while changed:
        changed = False
        for pid, (ppid, _pgid, _state, _rss) in rows.items():
            if ppid in pids and pid not in pids:
                pids.add(pid)
                changed = True
    return sum(rows[pid][3] for pid in pids if pid in rows), sorted(pids)


def main():
    if git('rev-parse', 'HEAD') != EXPECTED_HEAD:
        raise RuntimeError('worktree HEAD mismatch')
    if git('status', '--porcelain'):
        raise RuntimeError('worktree is dirty')
    if git('diff', '--binary', EXPECTED_BASE, EXPECTED_CHECKPOINT, '--', 'anywork/worker_test.go') != git('diff', '--binary', EXPECTED_BASE, EXPECTED_HEAD, '--', 'anywork/worker_test.go'):
        raise RuntimeError('test patch bytes differ from accepted checkpoint')
    if git('diff', '--quiet', EXPECTED_MAIN, EXPECTED_HEAD, '--', '.', ':(exclude)anywork/worker_test.go') != '':
        raise RuntimeError('unexpected non-test source difference')
    if sha(GO) != GO_SHA:
        raise RuntimeError('official Go binary SHA mismatch')
    version = subprocess.run([str(GO), 'version'], capture_output=True, text=True, timeout=5)
    if version.returncode or version.stdout.strip() != 'go version go1.26.9 linux/amd64':
        raise RuntimeError(f'official Go version check failed: {version.returncode} {version.stdout!r} {version.stderr!r}')
    gcc = Path('/usr/bin/gcc')
    if not gcc.is_file():
        raise RuntimeError('required CC=/usr/bin/gcc missing')
    free_pre = free_bytes()
    if free_pre < FREE_MIN:
        raise RuntimeError(f'workspace free-space preflight below floor: {free_pre}')

    nonce = uuid.uuid4().hex[:12]
    cache = Path('/dev/shm') / f'rcc181-235-go-cache-{nonce}'
    scratch = EVIDENCE / f'gotmp-{nonce}'
    cache.mkdir(mode=0o700)
    scratch.mkdir(mode=0o700)
    logpath = EVIDENCE / 'go-test.log'
    resultpath = EVIDENCE / 'command-result.json'
    for p in (logpath, resultpath):
        if p.exists():
            raise RuntimeError(f'refusing existing output {p.name}')

    cmd = [str(GO), 'test', '-race', './anywork', '-count=1', '-run', '^(' + '|'.join(TESTS) + ')$']
    env = os.environ.copy()
    env.update({
        'GOROOT': '/workspace/work/rcc-swarm/tools/go1.26.9',
        'GOTOOLCHAIN': 'local',
        'GOWORK': 'off',
        'GOFLAGS': '',
        'GOOS': 'linux',
        'GOARCH': 'amd64',
        'CGO_ENABLED': '1',
        'CC': '/usr/bin/gcc',
        'GOPROXY': 'off',
        'GOCACHE': str(cache),
        'GOTMPDIR': str(scratch),
    })
    record = {
        'status': 'RUNNING', 'head': EXPECTED_HEAD, 'tree': git('rev-parse', 'HEAD^{tree}'),
        'parents': git('rev-list', '--parents', '-n', '1', 'HEAD').split()[1:],
        'go_path': str(GO), 'go_sha256': GO_SHA, 'go_version': version.stdout.strip(),
        'gcc_path': str(gcc), 'gcc_version_first_line': subprocess.run([str(gcc), '--version'], capture_output=True, text=True, timeout=5).stdout.splitlines()[0],
        'patch_sha256_base_to_checkpoint': hashlib.sha256(git('diff', '--binary', EXPECTED_BASE, EXPECTED_CHECKPOINT, '--', 'anywork/worker_test.go').encode()).hexdigest(),
        'patch_sha256_base_to_head': hashlib.sha256(git('diff', '--binary', EXPECTED_BASE, EXPECTED_HEAD, '--', 'anywork/worker_test.go').encode()).hexdigest(),
        'command': cmd,
        'environment': {k: env[k] for k in ('GOROOT', 'GOTOOLCHAIN', 'GOWORK', 'GOFLAGS', 'GOOS', 'GOARCH', 'CGO_ENABLED', 'CC', 'GOPROXY', 'GOCACHE', 'GOTMPDIR')},
        'cache_path': str(cache), 'scratch_path': str(scratch),
        'limits': {'workspace_free_floor_bytes': FREE_MIN, 'tree_rss_bytes': RSS_MAX, 'gocache_allocated_bytes': CACHE_MAX, 'scratch_allocated_bytes': SCRATCH_MAX, 'timeout_seconds': TIMEOUT},
        'workspace_free_bytes_pre': free_pre,
        'sample_interval_seconds': SAMPLE,
        'retry_performed': False,
    }
    started = time.monotonic()
    record['start_utc_ns'] = time.time_ns()
    min_free = None
    peak_rss = peak_cache = peak_scratch = 0
    pids_last = []
    failure = None
    proc = None
    with logpath.open('xb', buffering=0) as logf:
        proc = subprocess.Popen(cmd, cwd=REPO, env=env, stdin=subprocess.DEVNULL, stdout=subprocess.PIPE,
                                stderr=subprocess.STDOUT, start_new_session=True, bufsize=0)
        record['pid'] = proc.pid
        os.set_blocking(proc.stdout.fileno(), False)
        sel = selectors.DefaultSelector()
        sel.register(proc.stdout, selectors.EVENT_READ)
        try:
            while True:
                elapsed = time.monotonic() - started
                if elapsed > TIMEOUT:
                    failure = '120-second command watchdog exceeded'
                    break
                try:
                    free = free_bytes()
                    min_free = free if min_free is None else min(min_free, free)
                    if free < FREE_MIN:
                        failure = f'workspace free-space floor breached: {free}'
                        break
                    rss, pids_last = process_rss(os.getpid(), proc)
                    peak_rss = max(peak_rss, rss)
                    if rss > RSS_MAX:
                        failure = f'sampled process-tree RSS cap breached: {rss}'
                        break
                    cache_bytes = allocated(cache)
                    scratch_bytes = allocated(scratch)
                    peak_cache = max(peak_cache, cache_bytes)
                    peak_scratch = max(peak_scratch, scratch_bytes)
                    if cache_bytes > CACHE_MAX:
                        failure = f'own Go cache cap breached: {cache_bytes}'
                        break
                    if scratch_bytes > SCRATCH_MAX:
                        failure = f'own GOTMPDIR cap breached: {scratch_bytes}'
                        break
                except Exception as exc:
                    failure = f'resource observer failed closed: {type(exc).__name__}: {exc}'
                    break
                events = sel.select(SAMPLE)
                if events:
                    try:
                        chunk = os.read(proc.stdout.fileno(), 65536)
                    except BlockingIOError:
                        chunk = b''
                    if chunk:
                        if logf.tell() + len(chunk) > LOG_MAX:
                            failure = 'test output log cap exceeded'
                            break
                        logf.write(chunk)
                        logf.flush()
                code = proc.poll()
                if code is not None:
                    alive = [pid for pid in pids_last if pid != os.getpid()]
                    if not alive:
                        try:
                            while True:
                                chunk = os.read(proc.stdout.fileno(), 65536)
                                if not chunk:
                                    break
                                if logf.tell() + len(chunk) > LOG_MAX:
                                    failure = 'test output log cap exceeded while draining'
                                    break
                                logf.write(chunk)
                        except (BlockingIOError, OSError):
                            pass
                        break
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
            code = proc.wait()
        finally:
            sel.close()
            proc.stdout.close()
    record.update({
        'numeric_exit_code': code,
        'failure': failure,
        'elapsed_seconds': time.monotonic() - started,
        'workspace_free_bytes_min': min_free,
        'workspace_free_bytes_final': free_bytes(),
        'process_tree_sampled_rss_peak_bytes': peak_rss,
        'process_tree_last_pids': pids_last,
        'gocache_allocated_peak_bytes': peak_cache,
        'gocache_allocated_final_bytes': allocated(cache),
        'scratch_allocated_peak_bytes': peak_scratch,
        'scratch_allocated_final_bytes': allocated(scratch),
        'log_bytes': logpath.stat().st_size,
        'log_sha256': sha(logpath),
        'finished_utc_ns': time.time_ns(),
    })
    record['status'] = 'PASS' if code == 0 and failure is None else 'FAIL'
    fd = os.open(resultpath, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    with os.fdopen(fd, 'w') as f:
        json.dump(record, f, indent=2, sort_keys=True)
        f.write('\n')
    print(json.dumps({k: record[k] for k in ('status', 'numeric_exit_code', 'failure', 'elapsed_seconds', 'workspace_free_bytes_min', 'process_tree_sampled_rss_peak_bytes', 'gocache_allocated_peak_bytes', 'scratch_allocated_peak_bytes')}, sort_keys=True))
    return 0 if record['status'] == 'PASS' else 1


if __name__ == '__main__':
    raise SystemExit(main())
