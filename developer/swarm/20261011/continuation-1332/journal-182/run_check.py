#!/usr/bin/env python3
"""Resource-guarded one-shot journal package test admitted by the root agent."""
import json
import os
import secrets
import shutil
import signal
import subprocess
import sys
import tempfile
import time
from pathlib import Path

REPO = Path('/workspace/work/rcc-swarm/journal-182')
EVIDENCE = Path(__file__).resolve().parent
GO = Path('/workspace/work/rcc-swarm/tools/go1.26.9/bin/go')
GOCACHE = Path('/home/agent/.cache/go-build')
FLOOR = 2_684_354_560
EARLY_MARGIN = 4 * 1024 * 1024
CACHE_LIMIT = 16 * 1024 * 1024
TMP_LIMIT = 128 * 1024 * 1024
RSS_LIMIT = 512 * 1024 * 1024
TIME_LIMIT = 120.0
COMMAND = [str(GO), 'test', '-p', '1', '-count=1', '-cover', './journal']

def allocated_tree(path):
    total = 0
    if path.exists():
        total += path.lstat().st_blocks * 512
        if path.is_dir():
            for root, dirs, files in os.walk(path, followlinks=False):
                for name in dirs + files:
                    total += (Path(root) / name).lstat().st_blocks * 512
    return total

def available(path):
    return shutil.disk_usage(path).free

def process_rows():
    result = subprocess.run(
        ['ps', '-eo', 'pid=,ppid=,rss=,pcpu=,stat=,etime=,args='],
        text=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE, check=False,
    )
    if result.returncode != 0:
        raise RuntimeError(f'ps failed: exit={result.returncode}: {result.stderr.strip()}')
    rows = {}
    for raw in result.stdout.splitlines():
        parts = raw.strip().split(None, 6)
        if len(parts) < 6:
            continue
        try:
            pid, ppid, rss_kib = map(int, parts[:3])
            pcpu = float(parts[3])
        except ValueError:
            continue
        rows[pid] = {'ppid': ppid, 'rss': rss_kib * 1024, 'pcpu': pcpu,
                     'state': parts[4], 'etime': parts[5],
                     'args': parts[6] if len(parts) > 6 else ''}
    return rows

def descendants(rows, root_pid):
    if root_pid not in rows:
        raise RuntimeError(f'live Go PID {root_pid} is missing from ps output')
    found = {root_pid}
    changed = True
    while changed:
        changed = False
        for pid, row in rows.items():
            if row['ppid'] in found and pid not in found:
                found.add(pid)
                changed = True
    return found

def stop_group(proc):
    try:
        os.killpg(proc.pid, signal.SIGTERM)
    except ProcessLookupError:
        pass
    try:
        proc.wait(timeout=2)
    except subprocess.TimeoutExpired:
        try:
            os.killpg(proc.pid, signal.SIGKILL)
        except ProcessLookupError:
            pass
        proc.wait(timeout=2)

def main():
    if not REPO.is_dir() or not GO.is_file() or not GOCACHE.is_dir():
        raise RuntimeError('required repository/tool/cache path is unavailable')
    if subprocess.run(['git', '-C', str(REPO), 'status', '--porcelain'],
                      text=True, stdout=subprocess.PIPE, check=True).stdout:
        raise RuntimeError('journal worktree is not clean')
    head = subprocess.run(['git', '-C', str(REPO), 'rev-parse', 'HEAD'],
                          text=True, stdout=subprocess.PIPE, check=True).stdout.strip()
    if head != '36a84430083b3af6f9758408b94054e5197b7f8b':
        raise RuntimeError(f'unexpected source HEAD {head}')
    version = subprocess.run([str(GO), 'version'], text=True,
                             stdout=subprocess.PIPE, stderr=subprocess.STDOUT,
                             check=True).stdout.strip()
    if version != 'go version go1.26.9 linux/amd64':
        raise RuntimeError(f'unexpected official toolchain version: {version}')

    free_before = available(REPO)
    if free_before < FLOOR + EARLY_MARGIN:
        raise RuntimeError(f'free space lacks required floor plus margin: {free_before}')
    cache_before = allocated_tree(GOCACHE)
    nonce = secrets.token_hex(8)
    tmpdir = Path('/dev/shm') / f'rcc-journal-182-{nonce}'
    tmpdir.mkdir(mode=0o700)
    output_path = tmpdir / 'go-test-output.log'
    output = output_path.open('xb')
    command_log = EVIDENCE / f'go-test-{nonce}.log'
    receipt_path = EVIDENCE / f'resource-{nonce}.json'
    started = time.monotonic()
    deadline = started + TIME_LIMIT
    env = os.environ.copy()
    env.update({
        'PATH': f'{GO.parent}:{env.get("PATH", "")}',
        'CC': '/usr/bin/gcc',
        'CGO_ENABLED': '1',
        'GOTOOLCHAIN': 'local',
        'GOPROXY': 'off',
        'GOSUMDB': 'off',
        'GOCACHE': str(GOCACHE),
        'GOTMPDIR': str(tmpdir),
        'TMPDIR': str(tmpdir),
    })
    proc = subprocess.Popen(COMMAND, cwd=REPO, env=env, stdout=output,
                            stderr=subprocess.STDOUT, start_new_session=True)
    max_rss = 0
    samples = []
    stop_reason = None
    observer_error = None
    returncode = None
    try:
        while True:
            returncode = proc.poll()
            if returncode is not None:
                break
            now = time.monotonic()
            rows = process_rows()
            pids = descendants(rows, proc.pid)
            live = [pid for pid in pids if not rows[pid]['state'].startswith('Z')]
            if proc.poll() is None and proc.pid not in rows:
                raise RuntimeError(f'live Go PID {proc.pid} disappeared from ps')
            rss = sum(rows[pid]['rss'] for pid in live)
            max_rss = max(max_rss, rss)
            free = available(REPO)
            tmp_bytes = allocated_tree(tmpdir)
            consumed = max(0, free_before - free)
            samples.append({'elapsed_seconds': now - started, 'free_bytes': free,
                            'filesystem_consumed_bytes': consumed,
                            'process_tree_pids': sorted(live),
                            'process_tree_rss_bytes': rss,
                            'tmp_allocated_bytes': tmp_bytes})
            if now >= deadline:
                stop_reason = '120-second process-group timeout'
            elif free < FLOOR + EARLY_MARGIN:
                stop_reason = 'free-space early-stop floor+4MiB'
            elif consumed > CACHE_LIMIT:
                stop_reason = 'work-filesystem growth exceeded 16MiB guard'
            elif tmp_bytes > TMP_LIMIT:
                stop_reason = 'owned RAM temp exceeded 128MiB guard'
            elif rss >= RSS_LIMIT:
                stop_reason = 'process-tree RSS reached 512MiB guard'
            if stop_reason:
                stop_group(proc)
                returncode = proc.returncode
                break
            time.sleep(0.2)
    except Exception as exc:
        if proc.poll() is None:
            stop_group(proc)
        returncode = proc.returncode
        observer_error = f'{type(exc).__name__}: {exc}'
    finally:
        output.flush()
        os.fsync(output.fileno())
        output.close()

    elapsed = time.monotonic() - started
    free_after = available(REPO)
    cache_after = allocated_tree(GOCACHE)
    cache_growth = max(0, cache_after - cache_before)
    tmp_peak = allocated_tree(tmpdir)
    final_sample = {'elapsed_seconds': elapsed, 'free_bytes': free_after,
                    'filesystem_consumed_bytes': max(0, free_before - free_after),
                    'process_tree_pids': [], 'process_tree_rss_bytes': max_rss,
                    'tmp_allocated_bytes': tmp_peak}
    samples.append(final_sample)
    log_text = output_path.read_text(errors='replace')
    command_log.write_text(
        f'toolchain: {version}\nsource_head: {head}\n'
        f'command: CC=/usr/bin/gcc {" ".join(COMMAND)}\n'
        f'GOTMPDIR/TMPDIR: {tmpdir}\n\n{log_text}'
        f'\ncommand_exit_code: {returncode}\n', encoding='utf-8')
    receipt = {
        'source_head': head, 'source_tree': '2fa0167aa679a4d60c7a5c0002801dc4bdb94ff7',
        'toolchain': version, 'command': COMMAND, 'cc': '/usr/bin/gcc',
        'gocache': str(GOCACHE), 'gocache_allocated_bytes_before': cache_before,
        'gocache_allocated_bytes_after': cache_after,
        'gocache_growth_bytes': cache_growth,
        'gocache_growth_limit_bytes': CACHE_LIMIT,
        'free_bytes_before': free_before, 'free_bytes_after': free_after,
        'free_floor_bytes': FLOOR, 'early_stop_margin_bytes': EARLY_MARGIN,
        'owned_ram_tmp': str(tmpdir), 'owned_ram_tmp_peak_allocated_bytes': tmp_peak,
        'owned_ram_tmp_limit_bytes': TMP_LIMIT, 'process_tree_rss_peak_bytes': max_rss,
        'process_tree_rss_limit_bytes': RSS_LIMIT, 'elapsed_seconds': elapsed,
        'timeout_seconds': TIME_LIMIT, 'process_pid': proc.pid,
        'process_exit_code': returncode, 'guard_stop_reason': stop_reason,
        'observer_error': observer_error,
        'resource_samples': samples, 'command_log': str(command_log),
        'status': 'PASS' if returncode == 0 and stop_reason is None and observer_error is None and
                  cache_growth <= CACHE_LIMIT and free_after >= FLOOR and
                  tmp_peak <= TMP_LIMIT and max_rss < RSS_LIMIT else 'FAIL',
        'runtime_scope': 'journal package only; full repository and hosted coverage remain outstanding',
    }
    receipt_path.write_text(json.dumps(receipt, sort_keys=True, indent=2) + '\n')
    shutil.rmtree(tmpdir)
    receipt['owned_ram_tmp_removed_after_log_capture'] = not tmpdir.exists()
    receipt_path.write_text(json.dumps(receipt, sort_keys=True, indent=2) + '\n')
    print(json.dumps({'status': receipt['status'], 'exit_code': returncode,
                      'elapsed_seconds': elapsed, 'max_rss_bytes': max_rss,
                      'gocache_growth_bytes': cache_growth,
                      'free_bytes_before': free_before,
                      'free_bytes_after': free_after,
                      'tmp_peak_bytes': tmp_peak, 'stop_reason': stop_reason,
                      'command_log': str(command_log),
                      'resource_receipt': str(receipt_path)}, sort_keys=True))
    return 0 if receipt['status'] == 'PASS' else 1

if __name__ == '__main__':
    sys.exit(main())
