#!/usr/bin/env python3
"""Read-only independent validation of the retained one-shot probe artifacts."""
import hashlib, json, os, stat, sys
root = '/workspace/rcc-continuation/developer/swarm/20261011/successor-1955/capability'
r = json.load(open(os.path.join(root, 'probe-receipt.json')))
def sha(p):
    h = hashlib.sha256()
    with open(p, 'rb') as f:
        for b in iter(lambda: f.read(65536), b''): h.update(b)
    return h.hexdigest()
results=[]
for x in r['roots']:
    src=x['source']; before=x['source_before']; fallback=x['fallback']
    assert x['ficlone']['supported'] is False
    assert x['ficlone']['errno'] == 95 and x['ficlone']['errno_name'] == 'ENOTSUP'
    assert not os.path.lexists(x['clone_destination'])
    assert sha(src) == before['sha256']
    ss=os.stat(src, follow_symlinks=False)
    assert ss.st_ino == before['identity']['inode'] and ss.st_dev == before['identity']['device']
    cp=fallback['copy']; cs=os.stat(cp, follow_symlinks=False)
    assert sha(cp) == fallback['copy_after_mutation']['sha256']
    assert cs.st_ino == fallback['copy_after_mutation']['identity']['inode']
    assert cs.st_dev == fallback['copy_after_mutation']['identity']['device']
    assert cs.st_ino != ss.st_ino
    assert fallback['copy_before_mutation']['sha256'] == before['sha256']
    assert fallback['source_after_copy_mutation']['sha256'] == before['sha256']
    assert fallback['copy_after_mutation']['sha256'] != before['sha256']
    assert fallback['symlink_rejection']['rejected'] is True
    assert not os.path.lexists(fallback['symlink_path'] + '-copy.bin')
    results.append({'root':x['root'],'verified':True,'source_sha256':sha(src),'copy_sha256_after_mutation':sha(cp),'distinct_inode':True,'ficlone_errno':'ENOTSUP'})
print(json.dumps({'verification':'PASS','roots':results}, indent=2, sort_keys=True))
