#!/usr/bin/env python3
"""Root-review-only supervisor for one admitted RCC Go component experiment.

This file does not run at import time. Its execute() command requires an explicit
root-created admission JSON and is intentionally not invoked in this preparation.
"""
from __future__ import annotations
import argparse
import hashlib
import json
import os
import pathlib
import queue
import re
import resource
import shutil
import signal
import stat
import subprocess
import sys
import threading
import time
from dataclasses import dataclass, field
from typing import Callable

WORK=pathlib.Path('/workspace/work/rcc-swarm/evidence/performance-admission/go-object-pack-native-preexecution-1600/embed-7945-preparation')
BASE=pathlib.Path('/workspace/work/rcc-swarm/evidence/performance-admission/go-object-pack-native-preexecution-1600')
RAM=pathlib.Path('/dev/shm/rcc-125-exact7945-embedprep-workers226')
GO=pathlib.Path('/workspace/work/rcc-swarm/tools/go1.26.9/bin/go')
SOURCE_COMMIT='7945f956b428359928908a9f42e36417b113dafd'
SOURCE_TREE='49baf943b9d64ef79af56ec8f34eebe0385477b1'
SOURCE_MANIFEST_SHA='c1bc6f5c00a96c9e1e3dc4bcf4bb39b832ef525bc2795f570af61765f17246e6'
GENERATED_TREE_SHA='07e3c5978f32cbf9889bb24461e8cb2beee0b3f5358c4a6674b2337bc235e9e8'
HARNESS_SHA='79cab09b35028beac804dde357bd789a0654d9e487b6960b63e0230cc8eb5f37'
GO_SHA='e90c4e12df236ea0c2e7536fd68da8a47fc0b585c19588bf6c1a70cfc296a0f9'
INDEX_SHA='e587078b0b8d8bce38d4e665999b9a3a0d4d9fb1de1b87a8c3a4b223e71e50f6'
OFFSET_SHA='e8dca8fa7d069883441bd8616e49234d338ab5707b3cc05d6b3a5693b2ba99e2'
PACK_SHA='44d2341ac7cecf8a407e881bd4514e1f4fb6dd59530a4d172049d5c7e40f39a9'
PACK_BYTES=4484936
FLOOR=2688548864
WORKSPACE_CAP=32*1024*1024
RAM_CAP=1024*1024*1024
RSS_CAP=2*1024*1024*1024
TOTAL_SECONDS=180
BUILD_SECONDS=30
PASS_SECONDS=30
BINARY_CAP=28*1024*1024
RAW_CAP=512*1024
RESULT_CAP=512*1024
SUMMARY_CAP=256*1024
SCHEDULE=['filesystem','pack','pack','filesystem','filesystem','pack','pack','filesystem','filesystem','pack']

class GuardFailure(RuntimeError): pass

def sha_file(path:pathlib.Path)->tuple[int,str]:
 h=hashlib.sha256(); n=0
 with path.open('rb') as f:
  while True:
   b=f.read(1024*1024)
   if not b:break
   n+=len(b);h.update(b)
 return n,h.hexdigest()

def allocated_bytes(root:pathlib.Path)->int:
 if not root.exists():return 0
 total=0
 for p in (root,*root.rglob('*')):
  try: total+=p.stat().st_blocks*512
  except FileNotFoundError: continue
  except OSError as e: raise GuardFailure(f'cannot stat owned file {p}: {e}')
 return total

def free_bytes(root:pathlib.Path)->int:
 s=os.statvfs(root);return s.f_bavail*s.f_frsize

def parse_ps(raw:str)->dict[int,dict]:
 rows={}
 for line in raw.splitlines():
  fields=line.split()
  if len(fields)!=5:raise GuardFailure(f'unreadable ps row: {line!r}')
  pid,ppid,pgid,rss,state=fields
  row={'pid':int(pid),'ppid':int(ppid),'pgid':int(pgid),'rss':int(rss)*1024,'state':state}
  rows[row['pid']]=row
 return rows

def ps_rows(_proc=None)->dict[int,dict]:
 r=subprocess.run(['ps','-eo','pid=,ppid=,pgid=,rss=,stat='],capture_output=True,text=True,check=True)
 return parse_ps(r.stdout)

def own_rss_bytes()->int:
 r=subprocess.run(['ps','-p',str(os.getpid()),'-o','rss='],capture_output=True,text=True,check=True)
 vals=r.stdout.split()
 if len(vals)!=1:raise GuardFailure('supervisor RSS unavailable')
 return int(vals[0])*1024

@dataclass
class Budget:
 workspace:pathlib.Path=WORK
 cap:int=WORKSPACE_CAP
 raw_cap:int=RAW_CAP
 raw_written:int=0
 reserve:int=0
 lock:threading.Lock=field(default_factory=threading.Lock)
 def preflight(self,reserve:int)->None:
  with self.lock:
   current=allocated_bytes(self.workspace)
   if current+reserve>self.cap:raise GuardFailure(f'workspace reservation exceeds cap: {current}+{reserve}>{self.cap}')
   self.reserve=reserve
 def write_raw(self,file,chunk:bytes)->None:
  with self.lock:
   if self.raw_written+len(chunk)>self.raw_cap:raise GuardFailure('combined stdout/stderr output cap exceeded before write')
   current=allocated_bytes(self.workspace)
   remaining_reserve=max(0,self.reserve-self.raw_written)
   if current+remaining_reserve+len(chunk)>self.cap:raise GuardFailure('workspace cap would be exceeded before raw write')
   file.write(chunk);self.raw_written+=len(chunk)
 def write_output(self,path:pathlib.Path,data:bytes,max_file:int,reserve_after:int=0)->None:
  with self.lock:
   if path.exists():raise GuardFailure(f'refusing to overwrite {path}')
   if len(data)>max_file:raise GuardFailure(f'output cap exceeded before write: {path}')
   current=allocated_bytes(self.workspace)
   if current+len(data)+reserve_after>self.cap:raise GuardFailure(f'workspace cap would be exceeded before write: {path}')
   fd=os.open(path,os.O_CREAT|os.O_EXCL|os.O_WRONLY,0o600)
   try:
    view=memoryview(data)
    while view:
     n=os.write(fd,view);view=view[n:]
    os.fsync(fd)
   finally:os.close(fd)

@dataclass
class Progress:
 lock:threading.Lock=field(default_factory=threading.Lock)
 active:dict|None=None
 start_count:int=0
 done_count:int=0
 events:list=field(default_factory=list)
 error:str|None=None
 def line(self,line:str,now:float)->None:
  stripped=line.strip()
  if not stripped.startswith(('PASS_START ','PASS_DONE ')):return
  with self.lock:
   a=re.fullmatch(r'PASS_START (\d+)/10 arm=(filesystem|pack) replicate=(\d+)',stripped)
   b=re.fullmatch(r'PASS_DONE (\d+)/10 arm=(filesystem|pack) replicate=(\d+) wall_ns=(\d+)',stripped)
   if a:
    pos,arm,rep= int(a.group(1)),a.group(2),int(a.group(3))
    if self.active is not None or pos!=self.start_count+1 or pos>10 or arm!=SCHEDULE[pos-1]:
     self.error='unexpected or overlapping PASS_START marker';return
    self.start_count=pos;self.active={'position':pos,'arm':arm,'replicate':rep,'started_at':now}
    self.events.append({'event':'PASS_START','position':pos,'arm':arm,'replicate':rep,'monotonic_s':now})
   elif b:
    pos,arm,rep,wall=int(b.group(1)),b.group(2),int(b.group(3)),int(b.group(4))
    if self.active is None or (pos,arm,rep)!=(self.active['position'],self.active['arm'],self.active['replicate']):
     self.error='PASS_DONE does not match active pass';return
    self.done_count+=1;self.events.append({'event':'PASS_DONE','position':pos,'arm':arm,'replicate':rep,'wall_ns':wall,'monotonic_s':now});self.active=None
   else:self.error='malformed PASS progress marker'

def _reader(pipe,path:pathlib.Path,budget:Budget,progress:Progress,is_stderr:bool,done:threading.Event)->None:
 try:
  fd=os.open(path,os.O_CREAT|os.O_EXCL|os.O_WRONLY,0o600)
  with os.fdopen(fd,'wb',buffering=0) as log:
   pending=b''
   while True:
    # BufferedReader.read(n) may wait for n bytes or EOF, hiding a short
    # PASS_START marker from the per-pass watchdog. read1 returns available
    # bytes promptly so progress deadlines remain live while the child runs.
    chunk=pipe.read1(8192)
    if not chunk:break
    try:budget.write_raw(log,chunk)
    except Exception as e:
     with progress.lock:progress.error=str(e)
     # Continue draining so the child cannot block while parent terminates it.
    if is_stderr:
     pending+=chunk
     if len(pending)>8192 and b'\n' not in pending:
      with progress.lock:progress.error='stderr line exceeds 8192 bytes'
      pending=b''
     while b'\n' in pending:
      raw,pending=pending.split(b'\n',1)
      try:line=raw.decode('utf-8',errors='strict')
      except UnicodeDecodeError:
       with progress.lock:progress.error='stderr is not UTF-8';continue
      progress.line(line,time.monotonic())
   if is_stderr and pending:
    try:progress.line(pending.decode('utf-8',errors='strict'),time.monotonic())
    except UnicodeDecodeError:
     with progress.lock:progress.error='stderr trailing bytes are not UTF-8'
 except Exception as e:
  with progress.lock:progress.error=f'output reader failed: {e!r}'
 finally:
  try:pipe.close()
  except Exception:pass
  done.set()

def kill_group(pgid:int,proc:subprocess.Popen,grace:float=2.0)->None:
 try:os.killpg(pgid,signal.SIGTERM)
 except ProcessLookupError:pass
 try:proc.wait(timeout=grace)
 except subprocess.TimeoutExpired:
  try:os.killpg(pgid,signal.SIGKILL)
  except ProcessLookupError:pass
  try:proc.wait(timeout=grace)
  except subprocess.TimeoutExpired:pass

def group_members(rows:dict[int,dict],pgid:int)->list[dict]:
 return [r for r in rows.values() if r['pgid']==pgid and not r['state'].startswith('Z')]

def run_stage(name:str,cmd:list[str],cwd:pathlib.Path,env:dict,stdout_path:pathlib.Path,stderr_path:pathlib.Path,budget:Budget,ram_root:pathlib.Path,global_deadline:float,phase_seconds:float,pass_watch:bool=False,rlimit_fsize:int|None=None,sample_interval:float=0.05,snapshot:Callable=ps_rows)->dict:
 if stdout_path.exists() or stderr_path.exists():raise GuardFailure('refusing to overwrite phase logs')
 phase_start=time.monotonic();phase_deadline=min(global_deadline,phase_start+phase_seconds)
 progress=Progress();samples=[];stop=None;proc=None;numeric=None;readers=[];events=[threading.Event(),threading.Event()]
 def child_setup():
  if rlimit_fsize is not None:resource.setrlimit(resource.RLIMIT_FSIZE,(rlimit_fsize,rlimit_fsize))
 try:
  proc=subprocess.Popen(cmd,cwd=cwd,env=env,stdin=subprocess.DEVNULL,stdout=subprocess.PIPE,stderr=subprocess.PIPE,start_new_session=True,preexec_fn=child_setup if rlimit_fsize is not None else None)
  pgid=proc.pid
  readers=[threading.Thread(target=_reader,args=(proc.stdout,stdout_path,budget,progress,False,events[0]),daemon=False),threading.Thread(target=_reader,args=(proc.stderr,stderr_path,budget,progress,True,events[1]),daemon=False)]
  for t in readers:t.start()
  while True:
   rc=proc.poll()
   if rc is not None:
    numeric=rc;break
   now=time.monotonic()
   if now>=global_deadline:stop='combined 180-second deadline exceeded'
   elif now>=phase_deadline:stop=f'{name} {phase_seconds}-second deadline exceeded'
   if stop is None:
    try:
     rows=snapshot(proc)
     if proc.pid not in rows:
      rc=proc.poll()
      if rc is not None:numeric=rc;break
      stop='known live child missing from ps snapshot'
     members=group_members(rows,pgid)
     rss=sum(r['rss'] for r in members)+own_rss_bytes()
     free=free_bytes(WORK);ram=allocated_bytes(ram_root);workspace=allocated_bytes(budget.workspace)
     samples.append({'elapsed_s':round(now-phase_start,4),'rss_bytes_sampled':rss,'owned_ram_allocated_bytes':ram,'workspace_allocated_bytes':workspace,'workspace_free_bytes':free,'pgid':pgid,'live_group_pids':[r['pid'] for r in members]})
     if free<FLOOR:stop='workspace free-space floor breached'
     elif ram>RAM_CAP:stop='owned RAM allocation cap exceeded'
     elif workspace>budget.cap:stop='workspace allocation cap exceeded'
     elif rss>RSS_CAP:stop='sampled process-group plus supervisor RSS cap exceeded'
    except Exception as e:stop=f'resource observation failed: {e!r}'
   with progress.lock:
    perr=progress.error;active=progress.active
    active_deadline=(active['started_at']+PASS_SECONDS) if pass_watch and active else None
   if perr:stop=perr
   elif active_deadline is not None and time.monotonic()>active_deadline:stop='timed pass exceeded 30 seconds after PASS_START'
   if stop:
    kill_group(pgid,proc);numeric=proc.poll();break
   time.sleep(sample_interval)
  if numeric is None:numeric=proc.wait()
  for t in readers:t.join(timeout=5)
  readers_joined=all(not t.is_alive() for t in readers)
  with progress.lock:reader_error=progress.error
  if reader_error:stop=stop or reader_error
  if pass_watch:
   with progress.lock:
    if progress.active is not None:
     if time.monotonic()-progress.active['started_at']>PASS_SECONDS:
      stop=stop or f'timed pass exceeded {PASS_SECONDS} seconds after PASS_START'
     else:
      stop=stop or 'runtime exited with an incomplete PASS marker pair'
    for i,event in enumerate(progress.events):
     if event['event']=='PASS_START' and i+1<len(progress.events):
      done=progress.events[i+1]
      if done['event']=='PASS_DONE' and done['position']==event['position'] and done['monotonic_s']-event['monotonic_s']>PASS_SECONDS:
       stop=stop or f'timed pass exceeded {PASS_SECONDS} seconds before PASS_DONE'
  if not readers_joined:
   kill_group(pgid,proc)
   for t in readers:t.join(timeout=2)
   readers_joined=all(not t.is_alive() for t in readers)
   stop=stop or 'output reader failed to join'
  # Final census catches surviving members of this run's process group.
  try:
   rows=snapshot(proc);remaining=group_members(rows,pgid)
  except Exception as e:
   rows={};remaining=[];stop=stop or f'final process census failed: {e!r}'
  if remaining:
   try:os.killpg(pgid,signal.SIGTERM)
   except ProcessLookupError:pass
   until=time.monotonic()+2
   while time.monotonic()<until:
    time.sleep(.05);rows=snapshot(proc);remaining=group_members(rows,pgid)
    if not remaining:break
   if remaining:
    try:os.killpg(pgid,signal.SIGKILL)
    except ProcessLookupError:pass
    rows=snapshot(proc);remaining=group_members(rows,pgid)
  quiescent=not remaining
  if not quiescent:stop=stop or 'owned process group not quiescent after kill'
  if pass_watch and numeric==0:
   with progress.lock:
    if progress.active is not None or progress.done_count!=10 or progress.start_count!=10:
     stop=stop or 'runtime exited 0 without ten complete PASS marker pairs'
  return {'phase':name,'command':cmd,'numeric_exit':numeric,'stop_reason':stop,'elapsed_s':round(time.monotonic()-phase_start,4),'samples':samples,'progress_events':progress.events,'reader_threads_joined':readers_joined,'process_group_quiescent':quiescent,'process_group':pgid,'max_sampled_rss_bytes':max((x['rss_bytes_sampled'] for x in samples),default=None),'max_owned_ram_allocated_bytes':max((x['owned_ram_allocated_bytes'] for x in samples),default=None),'min_workspace_free_bytes':min((x['workspace_free_bytes'] for x in samples),default=None),'stdout_path':str(stdout_path),'stderr_path':str(stderr_path)}
 except BaseException:
  if proc is not None:
   kill_group(proc.pid,proc)
  for t in readers:
   if t.is_alive():t.join(timeout=2)
  raise

def copy_verified_pack(source:pathlib.Path,destination:pathlib.Path)->dict:
 if destination.exists():raise GuardFailure('refusing to overwrite staged pack')
 st=source.lstat()
 if not stat.S_ISREG(st.st_mode) or source.is_symlink():raise GuardFailure('pack source is not regular')
 if st.st_size!=PACK_BYTES:raise GuardFailure('pack source size mismatch')
 h=hashlib.sha256();n=0
 fd=os.open(destination,os.O_CREAT|os.O_EXCL|os.O_WRONLY,0o600)
 try:
  with source.open('rb') as src:
   while True:
    b=src.read(65536)
    if not b:break
    n+=len(b)
    if n>PACK_BYTES:raise GuardFailure('pack source exceeded frozen size')
    h.update(b);view=memoryview(b)
    while view:view=view[os.write(fd,view):]
  os.fsync(fd)
 finally:os.close(fd)
 os.chmod(destination,0o444)
 after_n,after_h=sha_file(source)
 dest_n,dest_h=sha_file(destination)
 if n!=PACK_BYTES or h.hexdigest()!=PACK_SHA or (after_n,after_h)!=(PACK_BYTES,PACK_SHA) or (dest_n,dest_h)!=(PACK_BYTES,PACK_SHA):raise GuardFailure('pack input or RAM copy digest mismatch')
 return {'source_path':str(source),'source_size':after_n,'source_sha256':after_h,'ram_copy_path':str(destination),'ram_copy_size':dest_n,'ram_copy_sha256':dest_h}

def verify_source_and_embeds(source:pathlib.Path)->dict:
 manifest_path=BASE/'source-manifest.json';manifest_bytes=manifest_path.read_bytes()
 if hashlib.sha256(manifest_bytes).hexdigest()!=SOURCE_MANIFEST_SHA:raise GuardFailure('source manifest SHA mismatch')
 manifest=json.loads(manifest_bytes)
 if manifest['source_commit']!=SOURCE_COMMIT or manifest['source_tree']!=SOURCE_TREE:raise GuardFailure('source manifest commit/tree mismatch')
 tracked=[]
 for e in manifest['snapshot_files']:
  p=source/e['path'];n,h=sha_file(p)
  if n!=e['bytes'] or h!=e['sha256']:raise GuardFailure('tracked input mismatch: '+e['path'])
  tracked.append((e['path'],h,n))
 gh=hashlib.sha256()
 for rel,h,n in sorted(tracked):gh.update(rel.encode()+b'\0'+h.encode()+b'\0'+str(n).encode()+b'\n')
 if len(tracked)!=227 or sum(x[2] for x in tracked)!=1541402:raise GuardFailure('tracked source count/size mismatch')
 gen_receipt=json.loads((WORK/'generated-inputs-receipt.json').read_text())
 if gen_receipt['source_commit']!=SOURCE_COMMIT or gen_receipt['generated_embed_tree_sha256']!=GENERATED_TREE_SHA:raise GuardFailure('generated embed receipt identity mismatch')
 embeds=[]
 for rel,meta in gen_receipt['generated_embed_inputs'].items():
  n,h=sha_file(source/rel)
  if n!=meta['size'] or h!=meta['sha256']:raise GuardFailure('generated embed mismatch: '+rel)
  embeds.append((rel,h,n))
 tree=hashlib.sha256()
 for rel,h,n in sorted(embeds):tree.update(rel.encode()+b'\0'+h.encode()+b'\0'+str(n).encode()+b'\n')
 # The generator's canonical tree recipe is separately pinned in its receipt.
 if len(embeds)!=39 or sum(x[2] for x in embeds)!=7474027:raise GuardFailure('generated embed count/size mismatch')
 if tree.hexdigest()!=GENERATED_TREE_SHA:raise GuardFailure('generated embed tree digest mismatch')
 return {'source_tracked_count':len(tracked),'source_tracked_bytes':sum(x[2] for x in tracked),'source_tracked_tree_sha256':gh.hexdigest(),'generated_count':len(embeds),'generated_bytes':sum(x[2] for x in embeds),'generated_tree_sha256':tree.hexdigest(),'source_manifest_sha256':hashlib.sha256(manifest_path.read_bytes()).hexdigest()}

def verify_module_cache(source:pathlib.Path,manifest_path:pathlib.Path)->dict:
 manifest=json.loads(manifest_path.read_text())
 if manifest.get('tree_sha256')!='57a5f811603b86df1c2cc2cdf64bdca47eaca0ced8cca6bb8382caa776729e43':raise GuardFailure('offline module cache manifest identity mismatch')
 tree=hashlib.sha256(); total=0
 for item in manifest['files']:
  p=source/item['path'];n,h=sha_file(p)
  mode=stat.S_IMODE(p.stat().st_mode)
  if (n,h,mode)!=(item['size'],item['sha256'],item['mode']):raise GuardFailure('offline module cache file mismatch: '+item['path'])
  total+=n;tree.update(item['path'].encode()+b'\0'+str(n).encode()+b'\0'+str(mode).encode()+b'\0'+h.encode()+b'\n')
 if len(manifest['files'])!=1930 or total!=55499478 or tree.hexdigest()!=manifest['tree_sha256']:raise GuardFailure('offline module cache closure mismatch')
 return {'file_count':len(manifest['files']),'logical_bytes':total,'tree_sha256':tree.hexdigest()}

def readonly_copy_tree(source:pathlib.Path,dest:pathlib.Path)->None:
 if dest.exists():raise GuardFailure('refusing to overwrite read-only module cache copy')
 shutil.copytree(source,dest,symlinks=True)
 for p in [dest,*dest.rglob('*')]:
  try:os.chmod(p,stat.S_IMODE(p.lstat().st_mode)&~0o222,follow_symlinks=False)
  except (NotImplementedError,TypeError):pass

def execute(admission:pathlib.Path)->int:
 started=time.monotonic();global_deadline=started+TOTAL_SECONDS
 results={'status':'FAIL','source_commit':SOURCE_COMMIT,'numeric_build_exit':None,'numeric_harness_exit':None,'numeric_outer_exit':1,'stages':[],'limits':{'workspace_bytes':WORKSPACE_CAP,'ram_bytes':RAM_CAP,'sampled_rss_bytes':RSS_CAP,'floor_bytes':FLOOR,'total_seconds':TOTAL_SECONDS,'build_seconds':BUILD_SECONDS,'pass_seconds':PASS_SECONDS}}
 run_id='run-native-01';outdir=WORK/run_id
 budget=Budget();
 try:
  if outdir.exists():raise GuardFailure('refusing to reuse prior run directory')
  budget.preflight(BINARY_CAP+RAW_CAP+RESULT_CAP+SUMMARY_CAP)
  # Create the owned run directory only after reserving its complete fixed
  # output budget, so every later preflight failure can still get a receipt.
  os.mkdir(outdir,0o700)
  if free_bytes(WORK)<FLOOR:raise GuardFailure('preflight free-space floor failed')
  if not admission.is_file():raise GuardFailure('admission JSON missing')
  cfg=json.loads(admission.read_text())
  source=RAM/'source-accepted02';binding=verify_source_and_embeds(source)
  n,go_hash=sha_file(GO)
  if go_hash!=GO_SHA:raise GuardFailure('official Go binary SHA mismatch')
  if hashlib.sha256((WORK/'harness-successor-v2.go').read_bytes()).hexdigest()!=HARNESS_SHA:raise GuardFailure('harness source SHA mismatch')
  cache_binding=verify_module_cache(RAM/'gomodcache',WORK/'offline-gomodcache-manifest.json')
  source_root=pathlib.Path(cfg['source_root']);index_path=pathlib.Path(cfg['index_path']);offset_path=pathlib.Path(cfg['offset_index_path']);pack_source=pathlib.Path(cfg['pack_path'])
  if str(source_root)!='/workspace/work/rcc-swarm/evidence/performance-steer-20261011/runtime-prerequisites/attempt-10/attempt-10/provider-content':raise GuardFailure('source fixture path differs from frozen plan')
  for p,expected,size in ((index_path,INDEX_SHA,1491800),(offset_path,OFFSET_SHA,525841),(pack_source,PACK_SHA,PACK_BYTES)):
   n,h=sha_file(p)
   if n!=size or h!=expected:raise GuardFailure(f'frozen input mismatch: {p}')
  binary=outdir/'rcc-go-packprobe'
  runtime=RAM/run_id
  if runtime.exists():raise GuardFailure('refusing to reuse RAM runtime directory')
  runtime.mkdir(mode=0o700)
  if allocated_bytes(RAM)+PACK_BYTES+PACK_BYTES+128*1024*1024>RAM_CAP:raise GuardFailure('RAM prewrite reservation for pack/provider/build exceeds cap')
  ram_pack=runtime/'pack.bin';pack_receipt=copy_verified_pack(pack_source,ram_pack)
  provider=runtime/'provider';out=outdir/'results.jsonl'
  # Preserve the offline list source and create a new exact source copy for the candidate harness.
  run_source=RAM/'source-run-native-01'
  if run_source.exists():raise GuardFailure('refusing to reuse exact source copy')
  shutil.copytree(source,run_source,copy_function=shutil.copy2)
  probe=run_source/'prep-probe';shutil.rmtree(probe)
  probe.mkdir()
  shutil.copy2(WORK/'harness-successor-v2.go',probe/'main.go')
  bind2=verify_source_and_embeds(run_source)
  for p in [run_source,*run_source.rglob('*')]:
   if p.is_dir():os.chmod(p,p.stat().st_mode|0o700)
  ownmod=RAM/'gomodcache-run-native-01';readonly_copy_tree(RAM/'gomodcache',ownmod)
  env=os.environ.copy()
  for k in ('GOFLAGS','GOWORK','GOENV','GOTOOLCHAIN','GOPROXY','GOSUMDB','GOTELEMETRY','CGO_ENABLED','GOOS','GOARCH','GOMODCACHE','GOCACHE','GOTMPDIR','TMPDIR'):env.pop(k,None)
  owncache=RAM/'gocache';owntmp=RAM/'gotmp-run-native-01';owntmp.mkdir(mode=0o700)
  env.update({'GOENV':'off','GOTOOLCHAIN':'local','GOPROXY':'off','GOSUMDB':'off','GOTELEMETRY':'off','CGO_ENABLED':'0','GOOS':'linux','GOARCH':'amd64','GOFLAGS':'','GOMODCACHE':str(ownmod),'GOCACHE':str(owncache),'GOTMPDIR':str(owntmp),'TMPDIR':str(owntmp)})
  if allocated_bytes(RAM)+PACK_BYTES>RAM_CAP:raise GuardFailure('owned RAM prebuild cap exceeded')
  build_cmd=[str(GO),'build','-p=1','-mod=readonly','-trimpath','-buildvcs=false','-o',str(binary),'./prep-probe']
  budget.reserve=BINARY_CAP+RAW_CAP+RESULT_CAP+SUMMARY_CAP
  build=run_stage('build',build_cmd,run_source,env,outdir/'build.stdout',outdir/'build.stderr',budget,RAM,global_deadline,BUILD_SECONDS,False,BINARY_CAP)
  results['stages'].append(build);results['numeric_build_exit']=build['numeric_exit']
  if build['numeric_exit']!=0 or build['stop_reason']:raise GuardFailure('build stage failed')
  if not binary.is_file() or binary.is_symlink():raise GuardFailure('build output missing or nonregular')
  bn,bh=sha_file(binary)
  if bn>BINARY_CAP:raise GuardFailure('binary size cap exceeded')
  budget.reserve=RAW_CAP+RESULT_CAP+SUMMARY_CAP
  run_cmd=[str(binary),'-source-root',str(source_root),'-index',str(index_path),'-offset-index',str(offset_path),'-pack',str(ram_pack),'-provider-root',str(provider),'-out',str(out)]
  stage=run_stage('harness',run_cmd,run_source,env,outdir/'harness.stdout',outdir/'harness.stderr',budget,RAM,global_deadline,TOTAL_SECONDS,True,RESULT_CAP)
  results['stages'].append(stage);results['numeric_harness_exit']=stage['numeric_exit']
  if stage['numeric_exit']!=0 or stage['stop_reason']:raise GuardFailure('harness stage failed')
  nidx,hidx=sha_file(index_path);noff,hoff=sha_file(offset_path);npack,hpack=sha_file(pack_source)
  if (nidx,hidx)!=(1491800,INDEX_SHA) or (noff,hoff)!=(525841,OFFSET_SHA) or (npack,hpack)!=(PACK_BYTES,PACK_SHA):raise GuardFailure('original fixture input changed during run')
  results.update({'status':'PASS','binary_size':bn,'binary_sha256':bh,'offline_source_binding':binding,'run_source_binding':bind2,'offline_module_cache_binding':cache_binding,'pack_copy':pack_receipt,'elapsed_total_s':round(time.monotonic()-started,4),'numeric_outer_exit':0})
 except BaseException as e:
  results['error']=f'{type(e).__name__}: {e}'
  results['elapsed_total_s']=round(time.monotonic()-started,4)
  if results.get('stages'):
   for stage in results['stages']:
    if stage.get('stop_reason') or stage.get('numeric_exit')!=0:break
  results['numeric_outer_exit']=1
 try:
  resultpath=outdir/'supervisor-result.json'
  results['workspace_allocated_bytes_before_final_receipt']=allocated_bytes(WORK)
  results['owned_ram_allocated_bytes_at_receipt']=allocated_bytes(RAM)
  data=(json.dumps(results,sort_keys=True,indent=2)+'\n').encode()
  budget.reserve=0
  budget.write_output(resultpath,data,SUMMARY_CAP)
 except BaseException as e:
  print(f'failed to preserve final supervisor receipt: {e}',file=sys.stderr)
  results['numeric_outer_exit']=1
 print(json.dumps({'status':results.get('status'),'numeric_build_exit':results.get('numeric_build_exit'),'numeric_harness_exit':results.get('numeric_harness_exit'),'numeric_outer_exit':results.get('numeric_outer_exit'),'error':results.get('error')}))
 return int(results.get('numeric_outer_exit',1))

def main()->int:
 ap=argparse.ArgumentParser();ap.add_argument('--admission-json',type=pathlib.Path,required=True)
 args=ap.parse_args();return execute(args.admission_json)
if __name__=='__main__':raise SystemExit(main())
