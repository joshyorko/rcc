import os, stat, hashlib, json, shutil, datetime
from pathlib import Path
base=Path('/workspace/work/rcc-swarm/evidence/continuation-20261011/p1-hosted')
src=base/'main7945-strict-consumer/attempt-03'; dst=Path('/dev/shm/rcc-main7945-strict-accepted-attempt03-root')
out=base/'strict-preservation-1620'
assert json.loads((base/'root-main7945-strict-consumer-review/verification.json').read_text())['status']=='ROOT_INDEPENDENT_ACCEPTANCE_PASS'
assert src.is_dir() and not dst.exists()
def digest(p):
 h=hashlib.sha256()
 with p.open('rb') as f:
  for c in iter(lambda:f.read(1048576),b''): h.update(c)
 return h.hexdigest()
rows=[]; links={}
for p in [src]+sorted(src.rglob('*')):
 s=p.lstat(); r={'path':str(p.relative_to(src)),'mode':stat.S_IMODE(s.st_mode),'mtime_ns':s.st_mtime_ns}
 if stat.S_ISLNK(s.st_mode):r.update(type='symlink',target=os.readlink(p))
 elif stat.S_ISDIR(s.st_mode):r.update(type='directory')
 elif stat.S_ISREG(s.st_mode):
  r.update(type='file',bytes=s.st_size,sha256=digest(p)); key=(s.st_dev,s.st_ino)
  if key in links:r['hardlinkTo']=links[key]
  else:links[key]=r['path']
 else:raise ValueError('unexpectedtype '+str(p))
 rows.append(r)
assert shutil.disk_usage('/dev/shm').free>sum(r.get('bytes',0) for r in rows)+64*1024**2
dst.mkdir(mode=0o700)
for r in rows:
 p=src/r['path']; q=dst/r['path']
 if r['type']=='directory':q.mkdir(exist_ok=True)
 elif r['type']=='symlink':q.symlink_to(r['target'])
 elif 'hardlinkTo' in r:os.link(dst/r['hardlinkTo'],q)
 else:shutil.copy2(p,q)
for r in reversed(rows):
 q=dst/r['path']
 if r['type']!='symlink':os.chmod(q,r['mode'])
 os.utime(q,ns=(r['mtime_ns'],r['mtime_ns']),follow_symlinks=False)
for r in rows:
 q=dst/r['path']; s=q.lstat();assert stat.S_IMODE(s.st_mode)==r['mode'] and s.st_mtime_ns==r['mtime_ns']
 if r['type']=='file':
  assert s.st_size==r['bytes'] and digest(q)==r['sha256']
  if 'hardlinkTo' in r:assert s.st_ino==(dst/r['hardlinkTo']).stat().st_ino
 elif r['type']=='symlink':assert os.readlink(q)==r['target']
(out/'intact-state-manifest.json').write_text(json.dumps({'source':str(src),'retained':str(dst),'sourceRevision':'7945f956b428359928908a9f42e36417b113dafd','files':rows},indent=2)+'\n')
# Preserve bounded public command receipts before removing only our completed owned scratch.
shutil.copytree(src/'raw',out/'actual-raw')
assert sum(p.stat().st_size for p in (out/'actual-raw').rglob('*') if p.is_file())<4*1024**2
assert not Path('/proc/41126').exists()
shutil.rmtree(src)
(out/'preservation-result.json').write_text(json.dumps({'at':datetime.datetime.now(datetime.timezone.utc).isoformat(),'verifiedAllEntries':len(rows),'actualFileCount':sum(r['type']=='file' for r in rows),'logicalBytes':sum(r.get('bytes',0) for r in rows),'hardlinkAliasesPreserved':sum('hardlinkTo' in r for r in rows),'retainedIntactState':str(dst),'ownWorkspaceScratchRemovedAfterVerification':True,'noRamExecution':True,'workspaceFree':shutil.disk_usage('/workspace').free},indent=2)+'\n')
print((out/'preservation-result.json').read_text())
