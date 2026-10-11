from pathlib import Path
import os, json, hashlib, subprocess, datetime

repo = Path('/workspace/rcc')
base = Path('/workspace/work/rcc-swarm/evidence/continuation-20261011')
own = base / 'checkpoint-1740'
parent = '4f370746ef02869a0f0a34e2571693ead90db432'
prefix = 'developer/swarm/20261011/continuation-1740'
selected = {}

def add(path, dest):
    assert path.is_file() and not path.is_symlink(), path
    assert path.stat().st_size < 16 * 1024**2, path
    assert dest not in selected, dest
    selected[dest] = path

for rel in ['p1-hosted/p1-240-root-native-review', 'p1-hosted/p1-240-current-native-bytes', 'issue182-coverage-acceptance', 'coordination-1716', 'live-1735', 'actions-compatibility-refresh-1737']:
    for path in sorted((base / rel).rglob('*')):
        if path.is_file() and not path.is_symlink() and '__pycache__' not in path.parts:
            add(path, prefix + '/' + str(path.relative_to(base)))
for rel in ['execution-ledger.json', 'dependency-graph.json', 'checkpoint-1710/remote-verification.json']:
    add(base / rel, prefix + '/' + rel)
perf = Path('/workspace/work/rcc-swarm/evidence/performance-admission/object-allocation-census-1720')
for path in sorted(perf.rglob('*')):
    if path.is_file() and not path.is_symlink() and '__pycache__' not in path.parts:
        add(path, prefix + '/performance-admission/object-allocation-census-1720/' + str(path.relative_to(perf)))
for rel in ['restart.md', 'release-gates.md', 'source-attribution.json', 'shipping-versus-main.diff-files.txt', 'build_checkpoint.py', 'verify_remote.py']:
    add(own / rel, prefix + '/' + rel)
manifest = {'parent': parent, 'timestamp': datetime.datetime.now(datetime.timezone.utc).isoformat(), 'all2724PriorEntriesPreserved': True, 'original119Preserved': True, 'entries': [{'path': dest, 'bytes': path.stat().st_size, 'sha256': hashlib.sha256(path.read_bytes()).hexdigest()} for dest, path in selected.items()]}
(own / 'manifest.json').write_text(json.dumps(manifest, indent=2) + '\n')
add(own / 'manifest.json', prefix + '/manifest.json')
env = dict(os.environ, GIT_INDEX_FILE=str(own / 'git-index'), GIT_AUTHOR_NAME='Josh Yorko', GIT_AUTHOR_EMAIL='54248591+joshyorko@users.noreply.github.com', GIT_COMMITTER_NAME='Josh Yorko', GIT_COMMITTER_EMAIL='54248591+joshyorko@users.noreply.github.com')
subprocess.run(['git', 'read-tree', parent], cwd=repo, env=env, check=True)
for dest, path in selected.items():
    blob = subprocess.check_output(['git', 'hash-object', '--no-filters', '-w', str(path)], cwd=repo, text=True).strip()
    subprocess.run(['git', 'update-index', '--add', '--cacheinfo', f'100644,{blob},{dest}'], cwd=repo, env=env, check=True)
tree = subprocess.check_output(['git', 'write-tree'], cwd=repo, env=env, text=True).strip()
commit = subprocess.check_output(['git', 'commit-tree', tree, '-p', parent], cwd=repo, env=env, input='docs(swarm): preserve journal integration and native allocation evidence\n\nSigned-off-by: Josh Yorko <54248591+joshyorko@users.noreply.github.com>\n', text=True).strip()
old = subprocess.check_output(['git', 'ls-tree', '-r', parent], cwd=repo, text=True).splitlines()
new = subprocess.check_output(['git', 'ls-tree', '-r', commit], cwd=repo, text=True).splitlines()
by = {line.split('\t', 1)[1]: line for line in new}
assert len(old) == 2724 and all(by[line.split('\t', 1)[1]] == line for line in old)
for dest, path in selected.items():
    assert subprocess.check_output(['git', 'show', commit + ':' + dest], cwd=repo) == path.read_bytes(), dest
result = {'sha': commit, 'tree': tree, 'parent': parent, 'prefix': prefix, 'files': len(selected), 'bytes': sum(path.stat().st_size for path in selected.values()), 'prior_entries_unchanged': len(old), 'total_entries': len(new), 'original119Preserved': True, 'allLocalActualBytesVerified': True}
(own / 'local-commit.json').write_text(json.dumps(result, indent=2) + '\n')
print(json.dumps(result))
