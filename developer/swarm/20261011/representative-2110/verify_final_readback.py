#!/usr/bin/env python3
"""Read retained receipts, hashes and resources; never execute a workload."""
from pathlib import Path
import ast
import hashlib
import json
import os
import shutil
import stat

p = Path(__file__).resolve().parent
root = Path('/workspace/rcc-representative-2110')
fixture_root = Path('/workspace/rcc-successor-compression-2018/gzip-1')
sha = lambda x: hashlib.sha256(x.read_bytes()).hexdigest()
fixture = json.loads((fixture_root / 'fixture.json').read_text())
inventory = json.loads((p / 'input-snapshot-inventory.json').read_text())
for item in inventory['files']:
    x = root / 'inputs' / item['path']
    assert x.is_file() and not x.is_symlink()
    assert x.stat().st_size == item['bytes'] and sha(x) == item['sha256']
    assert oct(stat.S_IMODE(x.stat().st_mode)) == item['mode']
assert {str(x.relative_to(root / 'inputs')) for x in (root / 'inputs').rglob('*') if x.is_file()} == {x['path'] for x in inventory['files']}
for item in json.loads((p / 'build-binding.json').read_text())['files']:
    assert sha(Path(item['path'])) == item['sha256'], item['path']
for item in json.loads((p / 'retained-provider-verification.json').read_text())['files']:
    assert sha(Path(item['path'])) == item['sha256'], item['path']

materializations = []
logs = []
guards = []
for work in sorted(root.iterdir()):
    if work.name == 'inputs':
        continue
    for x in [work, work / 'home', work / 'tasks']:
        s = x.lstat()
        assert stat.S_ISDIR(s.st_mode) and stat.S_IMODE(s.st_mode) == 0o700
        assert (s.st_uid, s.st_gid) == (os.geteuid(), os.getegid())
        guards.append({'path': str(x), 'mode': '0700', 'uid': s.st_uid, 'gid': s.st_gid})
    assert not list((work / 'home').rglob('leases/*.json'))
    for ready in (work / 'home').rglob('ready.json'):
        d = json.loads(ready.read_text())
        tree = Path(d['path'])
        assert tree.is_relative_to(work / 'home' / 'holotree')
        actual = {str(x.relative_to(tree)) for x in tree.rglob('*') if x.is_file()}
        assert actual == {x['path'] for x in fixture['files']}
        for item in fixture['files']:
            x = tree / item['path']
            assert stat.S_ISREG(x.lstat().st_mode) and not x.is_symlink()
            assert x.stat().st_size == item['size'] and sha(x) == item['sha256']
        materializations.append({'work': work.name, 'ready': str(ready), 'path': str(tree), 'files': len(actual), 'mismatches': 0})
    assert not list((work / 'tasks').glob('task-*')), str(work / 'tasks')
    for log in (work / 'tasks' / 'logs').glob('*.log'):
        target = p / 'task-logs' / work.name / log.name
        target.parent.mkdir(exist_ok=True, parents=True)
        if target.exists():
            assert target.read_bytes() == log.read_bytes()
        else:
            shutil.copyfile(log, target)
        logs.append({'path': str(target.relative_to(p)), 'bytes': log.stat().st_size, 'sha256': sha(log)})

alive = []
for proc in Path('/proc').iterdir():
    if proc.name.isdigit():
        try:
            argv = (proc / 'cmdline').read_bytes().split(b'\0')
            if argv and argv[0] in [b'/workspace/rcc-successor-tools/representative-worker', b'/workspace/rcc-successor-tools/representative-worker-group']:
                alive.append(int(proc.name))
        except (FileNotFoundError, ProcessLookupError):
            pass
assert not alive, alive

expected = {}
for job, module in [('agentdocs', 'test_validate_agent_docs'), ('topology', 'test_validate_release_topology')]:
    tree = ast.parse((root / 'inputs' / 'scripts' / (module + '.py')).read_text())
    expected[job] = sorted(cls.name + '.' + method.name for cls in tree.body if isinstance(cls, ast.ClassDef) for method in cls.body if isinstance(method, ast.FunctionDef) and method.name.startswith('test_'))
accepted = []
for line in (p / 'results-group-2' / 'controller.jsonl').read_text().splitlines():
    row = json.loads(line)
    if row['type'] != 'accepted' or not ('pair-' in row['label']):
        continue
    for task in row['receipt']['rows']:
        assert sorted('.'.join(x['id'].split('.')[-2:]) for x in task['testIds']) == expected[task['job']]
        assert all(x['status'] == 'PASS' for x in task['testIds'])
    accepted.append({'label': row['label'], 'cases': sum(x['testsRun'] for x in row['receipt']['rows']), 'tasks': len(row['receipt']['rows'])})
assert len(accepted) == 6 and sum(x['cases'] for x in accepted) == 540
failed_home = Path('/workspace/rcc-successor-lifecycle/baseline/rcc-home')
assert stat.S_IMODE(failed_home.stat().st_mode) == 0o750
result = {'status': 'PASS', 'scope': 'Read-only postexecution hashes/census and exact-byte log collection; no workload replay', 'input_files': len(inventory['files']), 'binding_files': 535, 'provider_files': 519, 'materializations': materializations, 'materialized_file_checks': sum(x['files'] for x in materializations), 'materialized_mismatches': 0, 'guards': guards, 'remaining_leases': 0, 'remaining_task_tempdirs': 0, 'live_worker_executables': alive, 'accepted': accepted, 'accepted_test_cases': 540, 'expected_test_ids': expected, 'task_logs': logs, 'historical_failed_home_mode': '0750 preserved'}
(p / 'final-integrity-readback.json').write_text(json.dumps(result, indent=2) + '\n')
print(json.dumps({k: v for k, v in result.items() if k not in ['guards', 'task_logs', 'materializations', 'expected_test_ids']}))
