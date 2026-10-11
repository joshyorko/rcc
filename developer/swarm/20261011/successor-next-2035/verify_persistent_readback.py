#!/usr/bin/env python3
"""Fresh coordinator verification of retained bytes, without a timed rerun."""
import hashlib
import json
import os
from pathlib import Path
import stat
import subprocess

p = Path(__file__).parent
out = p / 'persistent-worker/results'
root = Path('/workspace/rcc-successor-persistent-2040')
fixture_root = Path('/workspace/rcc-successor-compression-2018/gzip-1')
fixture = json.loads((out / 'fixture.json').read_text())
sha = lambda f: hashlib.sha256(f.read_bytes()).hexdigest()
binding = json.loads((p / 'persistent-worker/build-binding.json').read_text())
for f in binding['files']:
    path = Path(f['path'])
    assert path.stat().st_size == f['bytes'] and sha(path) == f['sha256'], f['path']
assert json.loads((p / binding['build_result']).read_text())['returncode'] == 0
assert sha(p / 'persistent-worker/build-1/source.go.txt') == '82d9f176ab237f2a417b3d5ec43e2e5f3e0551c36c7cd0361672908b8efcc0ad'
assert json.loads((p / 'persistent-worker/build-1/result.json').read_text())['returncode'] == 1
for f in json.loads((out / 'fixture-closure-before.json').read_text()):
    path = fixture_root / f['path']
    assert path.stat().st_size == f['bytes'] and sha(path) == f['sha256'], f['path']
    assert oct(stat.S_IMODE(path.lstat().st_mode)) == f['mode'], f['path']
counts = {'materializations': 0, 'logical_files': 0, 'payload_requests': 0, 'payload_verifications': 0, 'mismatches': 0, 'active_leases': 0}
inventory = []
for worker in sorted(root.iterdir()):
    home = worker / 'home'
    row = json.loads((worker / 'worker.jsonl').read_text())
    assert row['status'] == 'PASS' and row['leasesReleased']
    for path in (root, worker, home):
        s = path.lstat()
        assert stat.S_ISDIR(s.st_mode) and stat.S_IMODE(s.st_mode) == 0o700
        assert (s.st_uid, s.st_gid) == (os.geteuid(), os.getegid())
    record = home / 'artifacts/v1/materializations' / fixture['artifact'].split(':')[1]
    ready = json.loads((record / 'ready.json').read_text())
    assert ready['materializationId'] == row['materialization']
    for f in fixture['files']:
        path = Path(ready['path']) / f['path']
        assert path.stat().st_size == f['size'] and sha(path) == f['sha256'], str(path)
        counts['logical_files'] += 1
    assert not list((record / 'leases').glob('*.json'))
    bound = json.loads((out / worker.name / 'result.json').read_text())
    assert bound['returncode'] == 0 and bound['stop'] is None and not bound['final_observed_live_identities']
    counts['materializations'] += 1
    if row['type'] == 'persistent-worker-arm':
        expected = 6 if row['arm'] == 'fresh' else 1
        requests = row['requests']
        assert [r['sequence'] for r in requests] == list(range(1, 7))
        assert len({r['pid'] for r in requests}) == expected
        assert len({r['leaseId'] for r in requests}) == expected
        assert row['executeCalls'] == expected and row['distinctPids'] == expected and row['outputParity']
        assert all(r['count'] == 512 and r['aggregateSha256'] == fixture['expectedOutput'] for r in requests)
        counts['payload_requests'] += len(requests)
        counts['payload_verifications'] += sum(r['count'] for r in requests)
    for path in sorted(worker.rglob('*')):
        s = path.lstat()
        r = {'path': str(path.relative_to(root)), 'mode': oct(stat.S_IMODE(s.st_mode)), 'uid': s.st_uid, 'gid': s.st_gid}
        if stat.S_ISREG(s.st_mode):
            r.update(bytes=s.st_size, sha256=sha(path))
        elif stat.S_ISLNK(s.st_mode):
            r.update(link_target=os.readlink(path))
        inventory.append(r)
assert counts['materializations'] == 7 and counts['logical_files'] == 3591 and counts['payload_requests'] == 36
assert stat.S_IMODE(Path('/workspace/rcc-successor-lifecycle/baseline/rcc-home').stat().st_mode) == 0o750
assert subprocess.run(['git', '-C', '/workspace/rcc-frozen', 'diff', '--quiet']).returncode == 0
(out / 'owned-scratch-inventory.json').write_text(json.dumps(inventory, indent=2) + '\n')
result = {'exit': 0, 'counts': counts, 'source_sha256': binding['source_sha256'], 'binary_sha256': binding['binary_sha256'], 'failed_compile_source_preserved': True, 'original_baseline_failure_preserved': True, 'frozen_tracked_source_unchanged': True, 'scope': 'Readback only of exact source/build/provider/materialization bytes, accepted child responses, supervision and guard receipts; no workload replay. Sampling cannot prove absence of unobserved escaped processes.'}
(p / 'coordinator-verification.json').write_text(json.dumps(result, indent=2) + '\n')
print(json.dumps(result))
