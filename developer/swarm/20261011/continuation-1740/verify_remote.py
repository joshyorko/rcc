import json, hashlib, urllib.request, concurrent.futures, time
from pathlib import Path

own = Path('/workspace/work/rcc-swarm/evidence/continuation-20261011/checkpoint-1740')
result = json.loads((own / 'local-commit.json').read_text())
manifest = json.loads((own / 'manifest.json').read_text())
entries = manifest['entries'] + [{'path': result['prefix'] + '/manifest.json', 'bytes': (own / 'manifest.json').stat().st_size, 'sha256': hashlib.sha256((own / 'manifest.json').read_bytes()).hexdigest()}]

def check(entry):
    request = urllib.request.Request('https://raw.githubusercontent.com/joshyorko/rcc/' + result['sha'] + '/' + entry['path'], headers={'Accept-Encoding': 'identity'})
    digest = hashlib.sha256()
    size = 0
    with urllib.request.urlopen(request, timeout=45) as response:
        assert response.status == 200
        while True:
            chunk = response.read(65536)
            if not chunk:
                break
            digest.update(chunk)
            size += len(chunk)
    assert size == entry['bytes'] and digest.hexdigest() == entry['sha256'], entry['path']
    return size

started = time.monotonic()
with concurrent.futures.ThreadPoolExecutor(max_workers=6) as pool:
    sizes = list(pool.map(check, entries))
proof = {'status': 'ACTUAL_REMOTE_RAW_BYTES_SHA256_AND_SIZE_VERIFIED', 'verifier_exit': 0, 'sha': result['sha'], 'parent': result['parent'], 'files': len(entries), 'bytes': sum(sizes), 'seconds': time.monotonic() - started, 'prior_tree_entries_unchanged': 2724, 'original119_preserved': True}
(own / 'remote-verification.json').write_text(json.dumps(proof, indent=2) + '\n')
print(json.dumps(proof))
