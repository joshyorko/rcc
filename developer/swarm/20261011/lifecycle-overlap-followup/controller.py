#!/usr/bin/env python3
"""Four bounded build/control phases. No performance workload or automatic retries."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import stat
import subprocess

p = Path(__file__).resolve().parent
prior = p.parent / 'representative-2110'
root = Path('/workspace/rcc-close-barrier-followup')
provider = Path('/workspace/rcc-successor-compression-2018/gzip-1')
go = Path('/workspace/rcc-successor-tools/mod/golang.org/toolchain@v0.0.1-go1.26.9.linux-amd64/bin/go')
sha = lambda x: hashlib.sha256(x.read_bytes()).hexdigest()


def guard(x, mode=0o700):
    s = x.lstat()
    assert stat.S_ISDIR(s.st_mode) and stat.S_IMODE(s.st_mode) == mode
    assert s.st_uid == os.geteuid() and s.st_gid == os.getegid()
    return {'path': str(x), 'mode': oct(mode), 'uid': s.st_uid, 'gid': s.st_gid}


def main():
    os.umask(0o077)
    ap = argparse.ArgumentParser()
    ap.add_argument('--phase', choices=['build-negative', 'run-negative', 'build-fixed', 'run-fixed'], required=True)
    phase = ap.parse_args().phase
    binding = json.loads((p / 'source-binding.json').read_text())
    for f in binding['files']:
        assert sha(Path(f['path'])) == f['sha256'], f['path']
    admission = json.loads((p / 'native-admission.json').read_text())
    assert admission['status'] == 'GO' and phase in admission['allowed_phases']
    assert admission['source_binding_sha256'] == sha(p / 'source-binding.json')
    if phase.endswith('fixed'):
        receipt = json.loads((p / 'results-negative' / 'readback.json').read_text())
        assert receipt['status'] == 'UNSAFE_BASELINE_OBSERVED'
        assert receipt['worker']['closeFailurePropagated'] is False
        assert receipt['worker']['leasesReleased'] is True
    variant = 'negative' if phase.endswith('negative') else 'fixed'
    binary = Path('/workspace/rcc-successor-tools') / ('close-barrier-' + variant)
    env = dict(os.environ, GOPATH='/workspace/rcc-successor-tools', GOMODCACHE='/workspace/rcc-successor-tools/mod', GOCACHE='/workspace/rcc-successor-tools/cache', GOTOOLCHAIN='local', GOMAXPROCS='2', GOMEMLIMIT='256MiB', GOTELEMETRY='off', PYTHONDONTWRITEBYTECODE='1', CGO_ENABLED='0', GOOS='linux', GOARCH='amd64')
    if phase.startswith('build-'):
        assert not binary.exists()
        out = p / phase
        command = [str(go), 'build', '-p', '2', '-mod=readonly', '-overlay=' + str(p / ('overlay-negative.json' if variant == 'negative' else 'overlay-fail-closed.json')), '-o', str(binary), str(p / 'source/main.go')]
        bound = subprocess.run(['python3', str(prior / 'run_bounded_v4.py'), '--out', str(out), '--seconds', '60'] + command, cwd='/workspace/rcc-frozen', env=env, capture_output=True)
        assert bound.returncode == 0, bound.stderr.decode()
        result = json.loads((out / 'result.json').read_text())
        assert result['returncode'] == 0 and result['stop'] is None and not result['final_observed_live_identities']
        (p / ('binary-binding-' + variant + '.json')).write_text(json.dumps({'source_binding_sha256': sha(p / 'source-binding.json'), 'binary_sha256': sha(binary), 'binary_bytes': binary.stat().st_size, 'build_result_sha256': sha(out / 'result.json')}, indent=2) + '\n')
        print(phase, 'PASS', sha(binary))
        return
    bb = json.loads((p / ('binary-binding-' + variant + '.json')).read_text())
    assert bb['source_binding_sha256'] == sha(p / 'source-binding.json') and bb['binary_sha256'] == sha(binary)
    if not root.exists():
        root.mkdir(mode=0o700)
    guard(root)
    work = root / variant
    work.mkdir(mode=0o700)
    for name in ['home', 'tasks']:
        (work / name).mkdir(mode=0o700)
    out = p / ('results-' + variant)
    paths = [root, work, work / 'home', work / 'tasks', provider, provider / 'producer-home', provider / 'filesystem-provider', Path('/workspace/rcc-representative-2110/inputs')]
    pre = [guard(x) for x in paths]
    fixture = json.loads((provider / 'fixture.json').read_text())
    env['ROBOCORP_HOME'] = str(work / 'home')
    env['RCC_HOLOTREE_MODE'] = 'private'
    args = [str(binary), '-mode=cancel-control', '-outer=' + str(root), '-inputs=/workspace/rcc-representative-2110/inputs', '-task-root=' + str(work / 'tasks'), '-driver=' + str(prior / 'source/task_driver.py'), '-provider=' + str(provider / 'filesystem-provider'), '-artifact=' + fixture['artifact'], '-receipt=' + str(work / 'worker.jsonl'), '-responses=' + str(work / 'responses.jsonl')]
    if variant == 'fixed':
        args.append('-expect-retained-lease')
    r = subprocess.run(['python3', str(prior / 'run_bounded_v4.py'), '--out', str(out), '--seconds', '30', '--scratch-root', str(root)] + args, env=env, capture_output=True)
    assert r.returncode == 0, r.stderr.decode()
    bound = json.loads((out / 'result.json').read_text())
    assert bound['returncode'] == 0 and bound['stop'] is None and not bound['final_observed_live_identities']
    rows = [json.loads(x) for x in (work / 'worker.jsonl').read_text().splitlines()]
    result = rows[-1]
    status = 'UNSAFE_BASELINE_OBSERVED' if variant == 'negative' else 'PASS_PRIVATE_FAIL_CLOSED'
    assert result['status'] == status and result['leasesReleased'] and result['cancelObserved']
    assert result['closeFailurePropagated'] == (variant == 'fixed')
    assert result['leaseRetainedBeforeRecovery'] == (variant == 'fixed')
    assert result['recoveryReleased'] == (variant == 'fixed')
    if variant == 'fixed':
        barrier = [x for x in rows if x['status'] == 'RETAINED_LEASE_BEFORE_RECOVERY']
        assert len(barrier) == 1 and barrier[0]['closeFailurePropagated'] and barrier[0]['leaseRetainedBeforeRecovery'] and not barrier[0]['recoveryReleased']
    assert not list((work / 'home').rglob('leases/*.json'))
    ready = json.loads(next((work / 'home').rglob('ready.json')).read_text())
    tree = Path(ready['path'])
    assert tree == work / 'home' / 'holotree' / result['materialization']
    assert {str(x.relative_to(tree)) for x in tree.rglob('*') if x.is_file()} == {x['path'] for x in fixture['files']}
    for f in fixture['files']:
        x = tree / f['path']
        assert stat.S_ISREG(x.lstat().st_mode) and not x.is_symlink()
        assert x.stat().st_size == f['size'] and sha(x) == f['sha256']
    post = [guard(x) for x in paths]
    assert pre == post
    readback = {'status': status, 'scope': 'Untimed close-failure barrier only; no performance sample', 'worker': result, 'pre': pre, 'post': post, 'materialized_files': len(fixture['files']), 'mismatches': 0, 'remaining_leases': 0, 'binary_sha256': sha(binary), 'source_binding_sha256': sha(p / 'source-binding.json')}
    for name in ['worker.jsonl', 'responses.jsonl']:
        (out / name).write_bytes((work / name).read_bytes())
    (out / 'readback.json').write_text(json.dumps(readback, indent=2) + '\n')
    print(phase, status, 'materialized513,zero leases, no outer fallback')


if __name__ == '__main__':
    main()
