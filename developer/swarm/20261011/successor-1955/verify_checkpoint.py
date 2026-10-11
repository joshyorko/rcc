#!/usr/bin/env python3
"""Verify immutable checkpoint bytes and preservation without replaying workloads."""
import concurrent.futures
import hashlib
import json
import subprocess
import time
import urllib.request
from pathlib import Path

ROOT = Path(__file__).resolve().parents[4]
OUT = Path(__file__).resolve().parent
SHA = '2f6570c99d5ba98f26ceb7b74d6da69fb84f99e8'
PREFIX = 'developer/swarm/20261011/continuation-1740/'

def git(*args):
    return subprocess.check_output(['git', '-C', str(ROOT), *args])

def tree(sha):
    rows = git('ls-tree', '-r', '-z', sha).split(b'\0')
    return dict(row.split(b'\t', 1)[::-1] for row in rows if row)

def main():
    started = time.monotonic()
    manifest_raw = git('show', SHA + ':' + PREFIX + 'manifest.json')
    manifest = json.loads(manifest_raw)
    parent = tree(manifest['parent'])
    child = tree(SHA)
    assert all(child.get(path) == value for path, value in parent.items())
    entries = manifest['entries'] + [{'path': PREFIX + 'manifest.json',
        'bytes': len(manifest_raw), 'sha256': hashlib.sha256(manifest_raw).hexdigest()}]

    def verify(entry):
        local = git('show', SHA + ':' + entry['path'])
        assert len(local) == entry['bytes']
        assert hashlib.sha256(local).hexdigest() == entry['sha256'], entry['path']
        request = urllib.request.Request(
            'https://raw.githubusercontent.com/joshyorko/rcc/' + SHA + '/' + entry['path'],
            headers={'Accept-Encoding': 'identity'})
        with urllib.request.urlopen(request, timeout=30) as response:
            assert response.status == 200
            remote = response.read(entry['bytes'] + 1)
        assert remote == local, entry['path']
        return {'path': entry['path'], 'bytes': len(remote), 'sha256': entry['sha256']}

    with concurrent.futures.ThreadPoolExecutor(max_workers=6) as executor:
        rows = list(executor.map(verify, entries))
    result = {'checkpoint': SHA, 'parent': manifest['parent'], 'exit': 0,
        'remote_raw_files': len(rows), 'remote_raw_bytes': sum(r['bytes'] for r in rows),
        'all_parent_entries_preserved': len(parent), 'seconds': time.monotonic() - started,
        'files': rows, 'scope': 'Byte verification only; no historical benchmark replay.'}
    (OUT / 'checkpoint-verification.json').write_text(json.dumps(result, indent=2) + '\n')
    print(json.dumps({k:v for k,v in result.items() if k != 'files'}))

if __name__ == '__main__':
    main()
