#!/usr/bin/env python3
"""Prepare exact-source embeds using existing independently bound payload bytes."""
import gzip, hashlib, json, shutil, sys, importlib.util
from pathlib import Path

root = Path('/workspace/rcc-frozen')
own = Path(__file__).resolve().parent
source_payload = Path('/workspace/rcc/blobs/assets/micromamba.linux_amd64.gz')
raw = source_payload.read_bytes()
binary = gzip.decompress(raw)
assert len(binary) == 17546920
assert hashlib.sha256(binary).hexdigest() == 'c04571cfb0750e5432d530a3068b8fcd232ebed3133358e056e59a90b9852b00'
target = root / 'blobs/assets/micromamba.linux_amd64.gz'
if target.exists():
    assert target.read_bytes() == raw
else:
    target.write_bytes(raw)
sys.path.insert(0, str(root))
spec = importlib.util.spec_from_file_location('rcc_exact_tasks', root / 'tasks.py')
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)
# Invoke the canonical asset function; pre-download of all platforms is unnecessary
# for this Linux-only import diagnostic. The Linux payload was verified above.
module.assets.body(None)
files = []
for p in sorted((root/'blobs').rglob('*')):
    if p.is_file():
        b=p.read_bytes(); files.append({'path':str(p.relative_to(root)), 'bytes':len(b), 'sha256':hashlib.sha256(b).hexdigest()})
(own/'embed-preparation.json').write_text(json.dumps({'source':'7945f956b428359928908a9f42e36417b113dafd', 'payload_source':str(source_payload), 'linux_payload_sha256':hashlib.sha256(binary).hexdigest(), 'payload_archive_sha256':hashlib.sha256(raw).hexdigest(), 'files':files, 'scope':'Linux diagnostic embeds; not shipping artifact equivalence.'}, indent=2)+'\n')
