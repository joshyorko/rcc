#!/usr/bin/env python3
"""Root-owned new, counterbalanced RCC persistent-child diagnostic.

Consumes the already accepted gzip-1 fixture. Does not replay a completed wave.
Stops before subsequent calls on any supervision, integrity, or mode failure.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import shutil
import stat
import statistics
import subprocess
import time

p = Path(__file__).parent
old = p.parent / 'successor-1955'
source = p / 'persistent-worker/source/main.go'
binary = Path('/workspace/rcc-successor-tools/persistent-worker')
fixture_root = Path('/workspace/rcc-successor-compression-2018/gzip-1')
root = Path('/workspace/rcc-successor-persistent-2040')
out = p / 'persistent-worker/results'
global_ht = Path('/workspace/rcc-successor-lifecycle/global-ht')


def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def guard(path, mode=0o700):
    s = path.lstat()
    assert stat.S_ISDIR(s.st_mode) and stat.S_IMODE(s.st_mode) == mode, (str(path), oct(s.st_mode))
    assert (s.st_uid, s.st_gid) == (os.geteuid(), os.getegid()), str(path)
    return {'path': str(path), 'mode': oct(mode), 'uid': s.st_uid, 'gid': s.st_gid}


def closure():
    rows = []
    for prefix in ('filesystem-provider', 'producer-home', 'packed-sidecar', 'fixture.json', 'prepared.json'):
        path = fixture_root / prefix
        paths = sorted(path.rglob('*')) if path.is_dir() else [path]
        for f in paths:
            s = f.lstat()
            if stat.S_ISREG(s.st_mode):
                rows.append({'path': str(f.relative_to(fixture_root)), 'bytes': s.st_size, 'mode': oct(stat.S_IMODE(s.st_mode)), 'sha256': digest(f)})
            elif not stat.S_ISDIR(s.st_mode):
                raise AssertionError(f)
    return rows


def append(row):
    with (out / 'controller.jsonl').open('a') as f:
        f.write(json.dumps(row) + '\n')
        f.flush()
        os.fsync(f.fileno())


def run(label, mode, arm, expected_source, before):
    assert digest(source) == expected_source
    worker = root / label
    worker.mkdir(mode=0o700)
    home = worker / 'home'
    home.mkdir(mode=0o700)
    guards = (root, out, fixture_root, fixture_root / 'filesystem-provider', fixture_root / 'producer-home', worker, home)
    pre = [guard(x) for x in guards] + [guard(global_ht, 0o750)]
    fixture = json.loads((fixture_root / 'fixture.json').read_text())
    args = [str(binary), '-mode=' + mode, '-outer=' + str(root), '-provider-root=' + str(fixture_root / 'filesystem-provider'), '-fixture=' + str(fixture_root / 'fixture.json'), '-artifact=' + fixture['artifact'], '-receipt=' + str(worker / 'worker.jsonl')]
    if mode == 'worker':
        args += ['-arm=' + arm, '-tasks=6', '-profile=' + str(worker / 'cpu.pprof')]
    command = ['python3', str(old / 'run_bounded_v3.py'), '--out', str(out / label), '--seconds', '30', '--scratch-root', str(root)] + args
    env = dict(os.environ, ROBOCORP_HOME=str(home), RCC_HOLOTREE_MODE='private', GOMAXPROCS='2', GOMEMLIMIT='1GiB', GOTELEMETRY='off')
    started = time.monotonic()
    result = subprocess.run(command, env=env, capture_output=True, text=True, timeout=40)
    (out / (label + '.launcher.stdout')).write_text(result.stdout)
    (out / (label + '.launcher.stderr')).write_text(result.stderr)
    post = [guard(x) for x in guards] + [guard(global_ht, 0o750)]
    for f in worker.iterdir():
        if f.is_file():
            shutil.copyfile(f, out / (label + '-' + f.name))
    append({'type': 'invocation', 'label': label, 'mode': mode, 'arm': arm, 'returncode': result.returncode, 'elapsed_seconds': time.monotonic() - started, 'command': command, 'pre': pre, 'post': post})
    assert result.returncode == 0, (label, result.stdout, result.stderr)
    bound = json.loads((out / label / 'result.json').read_text())
    assert bound['returncode'] == 0 and not bound['stop'] and not bound['final_observed_live_identities'], bound
    row = json.loads((worker / 'worker.jsonl').read_text())
    assert row['status'] == 'PASS' and row['leasesReleased'], row
    if mode == 'worker':
        assert row['tasks'] == 6 and row['outputParity'] and len(row['requests']) == 6
        assert [x['sequence'] for x in row['requests']] == list(range(1, 7))
        assert all(x['count'] == 512 and x['aggregateSha256'] == fixture['expectedOutput'] for x in row['requests'])
        assert row['distinctPids'] == (6 if arm == 'fresh' else 1)
        assert len(set(x['leaseId'] for x in row['requests'])) == (6 if arm == 'fresh' else 1)
        assert row['executeCalls'] == (6 if arm == 'fresh' else 1)
    leases = list(home.rglob('leases/*.json'))
    assert not leases, leases
    assert closure() == before, 'accepted producer/provider bytes changed'
    assert digest(source) == expected_source
    append({'type': 'accepted', 'label': label, 'receipt': row, 'fixture_unchanged': True, 'live_leases': 0})
    return row


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument('--source-sha256', required=True)
    a = ap.parse_args()
    assert digest(source) == a.source_sha256
    binding = json.loads((p / 'persistent-worker/build-binding.json').read_text())
    assert binding['source_sha256'] == a.source_sha256
    assert binding['binary_sha256'] == digest(binary)
    for f in binding['files']:
        assert digest(Path(f['path'])) == f['sha256'], f['path']
    assert json.loads((p / binding['build_result']).read_text())['returncode'] == 0
    out.mkdir(mode=0o700)
    root.mkdir(mode=0o700)
    guard(out)
    guard(root)
    before = closure()
    (out / 'fixture-closure-before.json').write_text(json.dumps(before, indent=2) + '\n')
    for name in ('fixture.json', 'prepared.json'):
        shutil.copyfile(fixture_root / name, out / name)
    run('nonzero-control', 'control', '', a.source_sha256, before)
    schedule = [(1, 'fresh'), (1, 'resident'), (2, 'resident'), (2, 'fresh'), (3, 'fresh'), (3, 'resident')]
    append({'type': 'schedule', 'arms': schedule, 'tasks_per_arm': 6, 'payload_verifications_per_task': 512, 'cold_acquire': 'separate fresh consumer CAS; excluded from Execute batch timer', 'batch_timing': 'sum of ExecuteWithStreams durations; same workload independently re-read/hashed six times', 'OS_cache': 'warm/uncontrolled; no drop_caches', 'classification': 'finite streamed Python batch; RCC diagnostic on synthetic fixture, not Actions acceptance or persistent daemon'})
    rows = {}
    for pair, arm in schedule:
        rows[pair, arm] = run(f'pair-{pair}-{arm}', 'worker', arm, a.source_sha256, before)
    summary = {'source_sha256': a.source_sha256, 'binary_sha256': digest(binary), 'arms': {}}
    for arm in ('fresh', 'resident'):
        r = [rows[pair, arm] for pair in range(1, 4)]
        summary['arms'][arm] = {'samples': 3, 'batch_ms': [x['batchNs'] / 1e6 for x in r], 'median_batch_ms': statistics.median(x['batchNs'] / 1e6 for x in r), 'median_acquire_ms': statistics.median(x['acquireNs'] / 1e6 for x in r), 'median_setup_acquire_batch_ms': statistics.median((x['providerSetupNs'] + x['acquireNs'] + x['batchNs']) / 1e6 for x in r), 'median_child_cpu_ms': statistics.median((x.get('childCpuUserNs', 0) + x.get('childCpuSystemNs', 0)) / 1e6 for x in r)}
    summary['paired_fresh_minus_resident_batch_ms'] = [(rows[i, 'fresh']['batchNs'] - rows[i, 'resident']['batchNs']) / 1e6 for i in range(1, 4)]
    summary['limitations'] = 'Three synthetic warm-OS-cache pairs, host Python wrapper; no long-lived worker daemon, Actions workflow, solver, actual customer workload, production change or release qualification. CPU/profile/IO scope must be read from source; Go profile excludes child Python.'
    (out / 'summary.json').write_text(json.dumps(summary, indent=2) + '\n')
    append({'type': 'complete', 'status': 'PASS', 'root': guard(root), 'evidence_root': guard(out), 'source_unchanged': closure() == before, 'source_sha256': digest(source), 'binary_sha256': digest(binary)})
    print(json.dumps(summary))


if __name__ == '__main__':
    main()
