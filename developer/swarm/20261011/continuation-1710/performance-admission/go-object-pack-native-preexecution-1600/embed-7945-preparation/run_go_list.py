#!/usr/bin/env python3
"""One bounded offline Go package-graph check; no compile/build/test."""
from __future__ import annotations
import hashlib, json, os, pathlib, re, shutil, signal, subprocess, sys, time

ROOT=pathlib.Path('/dev/shm/rcc-125-exact7945-embedprep-workers226')
SRC=ROOT/'source-accepted02'
SHARED_MOD=pathlib.Path('/home/agent/go/pkg/mod')
OWN_MOD=ROOT/'gomodcache'
OWN_CACHE=ROOT/'gocache'
OWN_TMP=ROOT/'gotmp'
WORKSPACE=pathlib.Path('/workspace/work/rcc-swarm/evidence/performance-admission/go-object-pack-native-preexecution-1600/embed-7945-preparation')
MANIFEST=pathlib.Path('/workspace/work/rcc-swarm/evidence/performance-admission/go-object-pack-native-preexecution-1600/source-manifest.json')
GO=pathlib.Path('/workspace/work/rcc-swarm/tools/go1.26.9/bin/go')
FLOOR=2688548864
RAM_CAP=256*1024*1024
RSS_CAP=1024*1024*1024
TIME_CAP=45

def digest(path):
 h=hashlib.sha256(); n=0
 with path.open('rb') as f:
  while True:
   b=f.read(1024*1024)
   if not b:break
   n+=len(b); h.update(b)
 return n,h.hexdigest()
def enc(s):return re.sub(r'[A-Z]',lambda m:'!'+m.group().lower(),s)
def alloc_tree(path):
 if not path.exists():return 0
 total=0
 for p in [path,*path.rglob('*')]:
  try: total += p.stat().st_blocks*512
  except OSError: raise RuntimeError(f'cannot stat owned path {p}')
 return total
def free_bytes(path):
 st=os.statvfs(path); return st.f_bavail*st.f_frsize
def source_check():
 d=json.loads(MANIFEST.read_text()); vals=[]
 for e in d['snapshot_files']:
  p=SRC/e['path']; n,h=digest(p)
  if h!=e['sha256'] or n!=e['bytes']:raise RuntimeError('source mismatch '+e['path'])
  vals.append((e['path'],h,n))
 h=hashlib.sha256()
 for rel,x,n in sorted(vals):h.update(rel.encode()+b'\0'+x.encode()+b'\0'+str(n).encode()+b'\n')
 return h.hexdigest(),len(vals),sum(x[2] for x in vals)
def stage_modules():
 if OWN_MOD.exists():raise RuntimeError('refusing preexisting own module cache')
 OWN_MOD.mkdir()
 OWN_CACHE.mkdir(); OWN_TMP.mkdir()
 lines=(SRC/'go.mod').read_text().splitlines();inside=False; req=[]
 for line in lines:
  t=line.strip()
  if t=='require (':inside=True;continue
  if inside and t==')':inside=False;continue
  if inside:
   m=re.match(r'(\S+)\s+(v\S+)',t)
   if m:req.append(m.groups())
  elif t.startswith('require '):
   m=re.match(r'require\s+(\S+)\s+(v\S+)',t)
   if m:req.append(m.groups())
 present=[]; missing=[]
 for mod,ver in req:
  e=enc(mod); v=enc(ver); src=SHARED_MOD/(e+'@'+v)
  if src.is_dir():
   dst=OWN_MOD/(e+'@'+v); shutil.copytree(src,dst,symlinks=True)
   present.append({'module':mod,'version':ver,'source_bytes':sum(p.stat().st_size for p in src.rglob('*') if p.is_file())})
  else: missing.append({'module':mod,'version':ver})
  dsrc=SHARED_MOD/'cache'/'download'/e/'@v'
  if dsrc.is_dir():
   ddst=OWN_MOD/'cache'/'download'/e/'@v'; ddst.mkdir(parents=True,exist_ok=True)
   for ext in ('.mod','.info','.ziphash'):
    p=dsrc/(v+ext)
    if p.is_file():shutil.copy2(p,ddst/p.name)
 if alloc_tree(OWN_MOD)+alloc_tree(OWN_CACHE)+alloc_tree(OWN_TMP)>RAM_CAP:raise RuntimeError('owned cache/temp preflight exceeds 256 MiB')
 return present,missing

def child_tree(rootpid):
 raw=subprocess.run(['ps','-eo','pid=,ppid=,rss=,stat='],capture_output=True,text=True,check=True).stdout
 rows={}
 for line in raw.splitlines():
  parts=line.split()
  if len(parts)!=4:raise RuntimeError('unreadable ps row')
  pid,ppid,rss,st=parts;rows[int(pid)]=(int(ppid),int(rss)*1024,st)
 if rootpid not in rows:raise RuntimeError('known Go process missing from ps snapshot')
 selected={rootpid};changed=True
 while changed:
  changed=False
  for pid,(ppid,_,_) in rows.items():
   if ppid in selected and pid not in selected:selected.add(pid);changed=True
 return [(p,rows[p][1],rows[p][2]) for p in selected]

def run():
 if free_bytes(WORKSPACE)<FLOOR:raise RuntimeError('workspace free-space floor preflight failed')
 go_size,go_sha=digest(GO)
 present,missing=stage_modules()
 probe=SRC/'prep-probe';probe.mkdir()
 shutil.copy2(pathlib.Path('/workspace/work/rcc-swarm/evidence/performance-admission/go-object-pack-native-preexecution-1600/harness.go'),probe/'main.go')
 source_before=source_check()
 env=os.environ.copy()
 for k in ('GOFLAGS','GOWORK','GOENV','GOTOOLCHAIN','GOPROXY','GOSUMDB','GOTELEMETRY','CGO_ENABLED','GOOS','GOARCH','GOMODCACHE','GOCACHE','GOTMPDIR','TMPDIR'):
  env.pop(k,None)
 env.update({'GOENV':'off','GOTOOLCHAIN':'local','GOPROXY':'off','GOSUMDB':'off','GOTELEMETRY':'off','CGO_ENABLED':'0','GOOS':'linux','GOARCH':'amd64','GOFLAGS':'','GOMODCACHE':str(OWN_MOD),'GOCACHE':str(OWN_CACHE),'GOTMPDIR':str(OWN_TMP),'TMPDIR':str(OWN_TMP)})
 cmd=[str(GO),'list','-deps','-mod=readonly','./prep-probe']
 stdout=ROOT/'go-list.stdout';stderr=ROOT/'go-list.stderr'
 if stdout.exists() or stderr.exists():raise RuntimeError('refusing to overwrite go-list outputs')
 started=time.monotonic();samples=[];numeric=None;stop=None
 with stdout.open('xb') as out,stderr.open('xb') as err:
  proc=subprocess.Popen(cmd,cwd=SRC,env=env,stdout=out,stderr=err,start_new_session=True)
  while True:
   rc=proc.poll();now=time.monotonic(); elapsed=now-started
   try:
    if free_bytes(WORKSPACE)<FLOOR:stop='workspace free-space floor breached'
    rows=child_tree(proc.pid) if rc is None else []
    rss=sum(x[1] for x in rows)
    psraw=subprocess.run(['ps','-p',str(os.getpid()),'-o','rss=,stat='],capture_output=True,text=True,check=True).stdout.split()
    if len(psraw)!=2: raise RuntimeError('supervisor RSS unavailable')
    rss+=int(psraw[0])*1024
    ram=alloc_tree(OWN_MOD)+alloc_tree(OWN_CACHE)+alloc_tree(OWN_TMP)
    samples.append({'elapsed_s':round(elapsed,4),'rss_bytes_sampled':rss,'owned_cache_tmp_allocated_bytes':ram,'workspace_free_bytes':free_bytes(WORKSPACE),'live_pids':[x[0] for x in rows]})
    if ram>RAM_CAP:stop='owned cache/temp cap exceeded'
    elif rss>RSS_CAP:stop='sampled process-tree RSS cap exceeded'
    elif elapsed>TIME_CAP:stop='45 second deadline exceeded'
   except Exception as e:stop='resource observation failed: '+repr(e)
   if stop and rc is None:
    os.killpg(proc.pid,signal.SIGTERM)
    try:proc.wait(timeout=2)
    except subprocess.TimeoutExpired:os.killpg(proc.pid,signal.SIGKILL);proc.wait()
    rc=proc.returncode
   if rc is not None:
    numeric=rc;break
   time.sleep(.1)
 source_after=source_check()
 packages=stdout.read_text().splitlines()
 result={
  'status':'PASS' if numeric==0 and stop is None and source_before==source_after else 'FAIL',
  'command':cmd,'cwd':str(SRC),'numeric_exit':numeric,'stop_reason':stop,
  'go_binary':str(GO),'go_binary_size':go_size,'go_binary_sha256':go_sha,
  'env':{k:env[k] for k in ('GOENV','GOTOOLCHAIN','GOPROXY','GOSUMDB','GOTELEMETRY','CGO_ENABLED','GOOS','GOARCH','GOFLAGS','GOMODCACHE','GOCACHE','GOTMPDIR','TMPDIR')},
  'module_cache_present_required':present,'module_cache_missing_required':missing,
  'source_tracked_before':source_before,'source_tracked_after':source_after,
  'dependency_package_count':len(packages),'dependency_packages':packages,
  'workspace_free_floor_bytes':FLOOR,'rss_cap_bytes':RSS_CAP,'cache_tmp_cap_bytes':RAM_CAP,'deadline_s':TIME_CAP,
  'sample_count':len(samples),'samples':samples,
  'stdout_path':str(stdout),'stdout_bytes':stdout.stat().st_size,'stdout_sha256':digest(stdout)[1],
  'stderr_path':str(stderr),'stderr_bytes':stderr.stat().st_size,'stderr_sha256':digest(stderr)[1],
 }
 (ROOT/'go-list-result.json').write_text(json.dumps(result,sort_keys=True,separators=(',',':'))+'\n')
 print(json.dumps({k:v for k,v in result.items() if k not in ('module_cache_present_required','dependency_packages','samples')},sort_keys=True))
 return numeric or (1 if stop or source_before!=source_after else 0)
if __name__=='__main__':sys.exit(run())
