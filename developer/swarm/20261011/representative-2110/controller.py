#!/usr/bin/env python3
"""New real-repository workload; controls first, stop at the first failure."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import shutil
import stat
import statistics
import signal
import subprocess
import time

p = Path(__file__).parent
old = p.parent / 'successor-1955'
root = Path('/workspace/rcc-representative-2110')
out = p / 'results-group-2'
campaign_label = 'group-2'
fixture_root = Path('/workspace/rcc-successor-compression-2018/gzip-1')
inputs = root / 'inputs'
binary = Path('/workspace/rcc-successor-tools/representative-worker-group')
global_ht = Path('/workspace/rcc-successor-lifecycle/global-ht')
sha = lambda path: hashlib.sha256(path.read_bytes()).hexdigest()


def guard(path, mode=0o700):
    s = path.lstat()
    assert stat.S_ISDIR(s.st_mode) and stat.S_IMODE(s.st_mode) == mode, (str(path), oct(s.st_mode))
    assert (s.st_uid, s.st_gid) == (os.geteuid(), os.getegid()), str(path)
    return {'path': str(path), 'mode': oct(mode), 'uid': s.st_uid, 'gid': s.st_gid}


def append(row):
    with (out / 'controller.jsonl').open('a') as f:
        f.write(json.dumps(row) + '\n')
        f.flush()
        os.fsync(f.fileno())


def check_inputs():
    inventory = json.loads((p / 'input-snapshot-inventory.json').read_text())
    for f in inventory['files']:
        path = inputs / f['path']
        assert path.stat().st_size == f['bytes'] and sha(path) == f['sha256'], f['path']
        assert oct(stat.S_IMODE(path.lstat().st_mode)) == f['mode'], f['path']
    assert len(list(inputs.rglob('*'))) == len(inventory['files']) + len([f for f in inputs.rglob('*') if f.is_dir()])


def verify_binding():
    native_umask = next(x.split()[1] for x in Path('/proc/self/status').read_text().splitlines() if x.startswith('Umask:'))
    assert int(native_umask, 8) == 0o077, native_umask
    b = json.loads((p / 'build-binding.json').read_text())
    for f in b['files']:
        assert sha(Path(f['path'])) == f['sha256'], f['path']
    assert json.loads((p / b['build_result']).read_text())['returncode'] == 0
    admission = json.loads((p / 'native-admission.json').read_text())
    assert admission['status'] == 'GO' and admission['go_sha256'] == sha(p / 'source/main.go') and admission['driver_sha256'] == sha(p / 'source/task_driver.py')
    assert admission['controller_sha256'] == sha(p / 'controller.py')
    assert admission['build_binding_sha256'] == sha(p / 'build-binding.json')
    assert admission['binary_sha256'] == sha(binary)


def readiness_cleanup(work):
    """Exact recorded PID/start fallback; a failure stays failed after cleanup."""
    path = work / 'responses.jsonl'
    rows = [json.loads(x) for x in path.read_text().splitlines()] if path.exists() else []
    receipts = []
    for row in rows:
        if row.get('control') != 'cancel-ready':
            continue
        for pid_key, start_key in [('pid', 'start'), ('childPid', 'childStart')]:
            pid, expected = row[pid_key], row[start_key]
            assert type(pid) is int and pid > 0 and isinstance(expected, str)
            def identity():
                try:
                    text = Path(f'/proc/{pid}/stat').read_text()
                except FileNotFoundError:
                    return None
                fields = text[text.rfind(')') + 2:].split()
                return fields[19], fields[0]
            before = identity()
            sent = []
            if before is not None and before[0] == expected and before[1] != 'Z':
                fd = os.pidfd_open(pid)
                try:
                    assert identity() == before, 'PID changed before pidfd binding'
                    for sig in (signal.SIGTERM, signal.SIGKILL):
                        try:
                            signal.pidfd_send_signal(fd, sig)
                            sent.append(sig.name)
                        except ProcessLookupError:
                            pass
                        deadline = time.monotonic() + 2
                        while time.monotonic() < deadline:
                            current = identity()
                            if current is None or current[0] != expected or current[1] == 'Z':
                                break
                            time.sleep(.02)
                        else:
                            continue
                        break
                finally:
                    os.close(fd)
            after = identity()
            assert after is None or after[0] != expected or after[1] == 'Z', 'recorded cancelled identity remains live'
            receipts.append({'pid': pid, 'start': expected, 'before': before, 'signals': sent, 'after': after})
    (out / 'readiness-fallback.json').write_text(json.dumps(receipts, indent=2) + '\n')


def run(label, mode, arm):
    verify_binding()
    work = root / (campaign_label + '-' + label)
    work.mkdir(mode=0o700)
    home = work / 'home'
    home.mkdir(mode=0o700)
    tasks = work / 'tasks'
    tasks.mkdir(mode=0o700)
    guards = [root, inputs, out, work, home, tasks, fixture_root, fixture_root / 'producer-home', fixture_root / 'filesystem-provider']
    pre = [guard(x) for x in guards] + [guard(global_ht, 0o750)]
    fixture = json.loads((fixture_root / 'fixture.json').read_text())
    args = [str(binary), '-mode=' + mode, '-arm=' + arm, '-outer=' + str(root), '-inputs=' + str(inputs), '-task-root=' + str(tasks), '-driver=' + str(p.resolve() / 'source/task_driver.py'), '-provider=' + str(fixture_root / 'filesystem-provider'), '-artifact=' + fixture['artifact'], '-receipt=' + str(work / 'worker.jsonl'), '-responses=' + str(work / 'responses.jsonl')]
    if mode == 'worker':
        args.append('-profile=' + str(work / 'cpu.pprof'))
    command = ['python3', str(p / 'run_bounded_v4.py'), '--out', str(out / label), '--seconds', '30', '--scratch-root', str(root)] + args
    env = dict(os.environ, ROBOCORP_HOME=str(home), RCC_HOLOTREE_MODE='private', GOMAXPROCS='2', GOMEMLIMIT='1GiB', GOTELEMETRY='off', PYTHONDONTWRITEBYTECODE='1')
    started = time.monotonic()
    result = subprocess.run(command, env=env, capture_output=True, text=True)
    (out / (label + '.launcher.stdout')).write_text(result.stdout)
    (out / (label + '.launcher.stderr')).write_text(result.stderr)
    if mode == 'cancel-control':
        readiness_cleanup(work)
        cleanup = json.loads((out / 'readiness-fallback.json').read_text())
        assert len(cleanup) == 2 and all(x['before'] is None and x['after'] is None and not x['signals'] for x in cleanup), 'private supervisor must reap both recorded identities before return; outer fallback cannot turn failure into PASS'
    post = [guard(x) for x in guards] + [guard(global_ht, 0o750)]
    for f in work.iterdir():
        if f.is_file():
            shutil.copyfile(f, out / (label + '-' + f.name))
    elapsed = time.monotonic() - started
    append({'type': 'invocation', 'label': label, 'mode': mode, 'arm': arm, 'returncode': result.returncode, 'controller_elapsed_seconds': elapsed, 'pre': pre, 'post': post, 'command': command})
    assert result.returncode == 0, (label, result.stdout, result.stderr)
    bound = json.loads((out / label / 'result.json').read_text())
    assert bound['returncode'] == 0 and bound['stop'] is None and not bound['final_observed_live_identities']
    r = [json.loads(l) for l in (work / 'worker.jsonl').read_text().splitlines()][-1]
    assert r['status'] == 'PASS' and r['leasesReleased']
    assert not list(home.rglob('leases/*.json'))
    if mode == 'worker':
        rows = r['rows']
        assert len(rows) == 6 and [x['sequence'] for x in rows] == list(range(1, 7))
        assert [x['job'] for x in rows] == ['agentdocs', 'topology'] * 3
        assert sum(x['testsRun'] for x in rows) == 90
        assert all(x['status'] == 'PASS' and x['cleanState'] and x['failures'] == 0 and x['errors'] == 0 and x['skips'] == 0 for x in rows)
        assert len({x['pid'] for x in rows}) == (6 if arm == 'fresh' else 1)
        assert len({x['leaseId'] for x in rows}) == (6 if arm == 'fresh' else 1)
    elif mode == 'cancel-control':
        assert r['cancelObserved'] and len(r['rows']) == 1
    elif mode == 'error-control':
        assert [x['status'] for x in r['rows']] == ['ERROR', 'PASS'] and all(x['cleanState'] for x in r['rows'])
    ready = json.loads((home / 'artifacts/v1/materializations' / fixture['artifact'].split(':')[1] / 'ready.json').read_text())
    materialized = Path(ready['path'])
    assert materialized == home / 'holotree' / r['materialization']
    wanted = {f['path'] for f in fixture['files']}
    actual = {str(x.relative_to(materialized)) for x in materialized.rglob('*') if x.is_file()}
    assert actual == wanted
    for f in fixture['files']:
        x = materialized / f['path']
        assert stat.S_ISREG(x.lstat().st_mode) and not x.is_symlink()
        assert x.stat().st_size == f['size'] and sha(x) == f['sha256']
    (out / (label + '-materialization-readback.json')).write_text(json.dumps({'status':'PASS','files':len(wanted),'mismatches':0,'materialization':r['materialization'],'artifact':fixture['artifact'],'path':str(materialized)},indent=2) + '\n')
    check_inputs()
    verify_binding()
    elapsed = time.monotonic() - started
    append({'type': 'accepted' , 'label': label, 'receipt': r, 'native_seconds': bound['seconds'], 'controller_seconds': elapsed})
    return r, bound['seconds'], elapsed


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument('--phase', choices=['controls', 'measure', 'resume-measure'], required=True)
    a = ap.parse_args()
    admission = json.loads((p / 'native-admission.json').read_text())
    assert a.phase in admission['allowed_phases']
    if a.phase == 'controls':
        out.mkdir(mode=0o700)
        run('error-control', 'error-control', '')
        run('cancel-control', 'cancel-control', '')
        run('smoke-fresh', 'worker', 'fresh')
        run('smoke-resident', 'worker', 'resident')
        append({'type': 'controls-complete', 'status': 'PASS', 'untimed_workload_test_cases':180, 'workload_smoke_arms':['fresh','resident']})
        print('isolation/error and cancellation/lease controls PASS')
        return
    prior = [json.loads(l) for l in (out / 'controller.jsonl').read_text().splitlines()]
    retained = {}
    if a.phase == 'resume-measure':
        assert prior[-1]['type'] == 'invocation' and prior[-1]['label'] == 'pair-2-resident' and prior[-1]['returncode'] != 0
        timed_accepted = {x['label']:x for x in prior if x['type']=='accepted' and x['label'].startswith('pair-')}
        assert set(timed_accepted) == {'pair-1-fresh','pair-1-resident'}
        for arm in ('fresh','resident'):
            x = timed_accepted['pair-1-'+arm]
            assert x['receipt']['status']=='PASS' and x['receipt']['leasesReleased']
            assert len(x['receipt']['rows'])==6 and sum(t['testsRun'] for t in x['receipt']['rows'])==90
            bound = json.loads((out / ('pair-1-'+arm) / 'result.json').read_text())
            assert bound['returncode']==0 and bound['stop'] is None and not bound['final_observed_live_identities']
            assert bound['launcher_sha256']==sha(old / 'run_bounded_v3.py')
            retained[1,arm]=(x['receipt'],x['native_seconds'],x['controller_seconds'])
        append({'type':'resume-admission','retained_pair':1,'retained_arms_not_replayed':True,'discarded_arm':'pair-2-resident','old_binding':sha(p/'build-3-binding-pre-runner-fix.json'),'new_binding':sha(p/'build-binding.json'),'same_worker_binary_driver_fixture':True,'observer_change':'v3 scratch lstat ENOENT race repaired in isolated v4; report per-pair source cohorts'})
        schedule = [(2, 'resident'), (2, 'fresh'), (3, 'fresh'), (3, 'resident')]
    else:
        assert prior[-1]['type'] == 'controls-complete' and prior[-1]['status'] == 'PASS'
        schedule = [(1, 'fresh'), (1, 'resident'), (2, 'resident'), (2, 'fresh'), (3, 'fresh'), (3, 'resident')]
    append({'type': 'schedule', 'arms': schedule, 'jobs': ['agentdocs', 'topology'] * 3, 'source': '7945f956b428359928908a9f42e36417b113dafd', 'workload_scope': 'actual agentdocs tests+validator and artifactFocused release-topology Python test subset; no full Go/Robot suite or release qualification', 'isolation': 'cooperative trusted-task CWD/env/sys.path/argv/tempdir/project-module restoration, separate private task dirs; not an OS sandbox', 'cache': 'OS cache uncontrolled/warm, stdlib imports reusable, project tests and suites constructed afresh each task'})
    rows = dict(retained)
    for pair, arm in schedule:
        label = ('resume-' if a.phase == 'resume-measure' else '') + f'pair-{pair}-{arm}'
        rows[pair, arm] = run(label, 'worker', arm)
    s = {'arms': {}, 'paired_fresh_minus_resident_batch_ms': [], 'paired_fresh_minus_resident_controller_ms': []}
    for arm in ('fresh', 'resident'):
        r = [rows[i, arm] for i in (1, 2, 3)]
        s['arms'][arm] = {'samples': 3, 'batch_ms': [x[0]['batchNs'] / 1e6 for x in r], 'median_batch_ms': statistics.median(x[0]['batchNs'] / 1e6 for x in r), 'median_setup_acquire_batch_ms': statistics.median((x[0]['providerSetupNs'] + x[0]['acquireNs'] + x[0]['batchNs']) / 1e6 for x in r), 'native_ms': [x[1] * 1e3 for x in r], 'median_native_ms': statistics.median(x[1] * 1e3 for x in r), 'controller_ms': [x[2] * 1e3 for x in r], 'median_controller_ms': statistics.median(x[2] * 1e3 for x in r), 'median_waited_child_cpu_ms': statistics.median((x[0].get('childCpuUserNs', 0) + x[0].get('childCpuSystemNs', 0)) / 1e6 for x in r)}
    for i in (1, 2, 3):
        s['paired_fresh_minus_resident_batch_ms'].append((rows[i, 'fresh'][0]['batchNs'] - rows[i, 'resident'][0]['batchNs']) / 1e6)
        s['paired_fresh_minus_resident_controller_ms'].append((rows[i, 'fresh'][2] - rows[i, 'resident'][2]) * 1e3)
    s['source_cohorts']={'pair1':'retainedv3fullouterPASSnoarmreplay','pairs2and3':'isolatedv4ENOENTracerepair; worker/driver/fixtureunchanged','pooled_outer_metrics':'descriptive only across observerversions; use per-pair/correctedcohort for claims'}
    s['per_pair']=[{'pair':i,'runner':'v3' if i==1 else 'v4','fresh_batch_ms':rows[i,'fresh'][0]['batchNs']/1e6,'resident_batch_ms':rows[i,'resident'][0]['batchNs']/1e6,'fresh_controller_ms':rows[i,'fresh'][2]*1e3,'resident_controller_ms':rows[i,'resident'][2]*1e3} for i in (1,2,3)]
    (out / 'summary.json').write_text(json.dumps(s, indent=2) + '\n')
    append({'type': 'complete', 'status': 'PASS', 'real_test_cases': 540, 'tasks': 36, 'root_guard': guard(root)})
    print(json.dumps(s))


if __name__ == '__main__':
    main()
