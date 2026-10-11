import json,shutil,hashlib,os,stat,datetime
from pathlib import Path
out=Path(__file__).parent; base=out.parent; guard=2688548864
extra=Path('/dev/shm/rcc-preserved-root-artifacts/completed-root-duplicates-1625');extra.mkdir(exist_ok=False)
paths=list(Path('/workspace/work/rcc-swarm/evidence/resume-acceptance/release-artifacts/current-release').iterdir())+[Path('/workspace/work/rcc-swarm/evidence/hosted-native/final-main-727')/n for n in ['RCC-Binaries.zip','native-runtime-binaries.zip']]
def sha(p):
 h=hashlib.sha256()
 with p.open('rb') as f:
  for c in iter(lambda:f.read(1048576),b''):h.update(c)
 return h.hexdigest()
rows=[]
for p in paths:
 assert p.is_file() and not p.is_symlink();s=p.stat();q=extra/(p.parent.name+'__'+p.name)
 r={'original':str(p),'preserved':str(q),'sha256':sha(p),'bytes':s.st_size,'mode':stat.S_IMODE(s.st_mode),'mtime_ns':s.st_mtime_ns,'sourceAttribution':'v18.19.5 actual released assets' if p.parent.name=='current-release' else '727c1ff8b679cdbbe12836e53c87734a1edb0071 actual historical hosted bundles'}
 shutil.copy2(p,q); assert sha(q)==r['sha256'] and q.stat().st_mtime_ns==s.st_mtime_ns and stat.S_IMODE(q.stat().st_mode)==r['mode']; rows.append(r)
(out/'completed-root-duplicates-intact-relocation.json').write_text(json.dumps({'at':datetime.datetime.now(datetime.timezone.utc).isoformat(),'intactPreservationNotDiscard':True,'noRamExecution':True,'originalSmallReceiptsUnchanged':True,'rootCompletedDuplicateAssetsOnly':True,'records':rows},indent=2)+'\n')
for r in rows:Path(r['original']).unlink()
rest=[]
for name,key in [('accepted324-temporary-staging.json','records'),('accepted324-additional-staging.json','files')]:
 for r in json.loads((base/'main7945-consumer-resource-admission'/name).read_text())[key]:
  p=Path(r['original']);q=Path(r.get('retainedActualBytes',r.get('preserved')));n=r.get('bytes',r.get('sizeBytes'));mt=r.get('mtime_ns',r.get('mtimeNs'))
  assert not p.exists() and sha(q)==r['sha256'] and q.stat().st_size==n
  assert shutil.disk_usage('/workspace').free-n-4194304>guard,(str(p),shutil.disk_usage('/workspace').free,n)
  shutil.copy2(q,p);assert sha(p)==r['sha256'] and p.stat().st_size==n and p.stat().st_mtime_ns==mt and stat.S_IMODE(p.stat().st_mode)==r['mode']
  rest.append({'original':str(p),'sha256':sha(p),'bytes':n,'mode':r['mode'],'mtime_ns':mt,'restoredVerified':True})
(out/'all-temporary324-restoration.json').write_text(json.dumps({'at':datetime.datetime.now(datetime.timezone.utc).isoformat(),'allListedHistorical324OriginalPathsRestored':True,'files':rest,'count':len(rest),'logicalBytesRestored':sum(r['bytes'] for r in rest),'retainedRAMCopiesIntact':True,'topology':'Original staging lacked inode topology; each original path restored with independently verified SHA,size,mode,mtime. No invented hardlink attribution.','guard':guard,'workspaceFreeAfter':shutil.disk_usage('/workspace').free},indent=2)+'\n')
print((out/'all-temporary324-restoration.json').read_text()[-1100:])
