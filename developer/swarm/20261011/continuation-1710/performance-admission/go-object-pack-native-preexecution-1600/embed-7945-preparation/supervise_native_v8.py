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
PACK_LAYOUT=pathlib.Path('/workspace/work/rcc-swarm/evidence/continuation-20261011/pack-layout-micro')
PACK_SOURCE=PACK_LAYOUT/'small-objects.concat'
OFFSET_SOURCE=PACK_LAYOUT/'offset-index.json'
INDEX_SOURCE=pathlib.Path('/workspace/work/rcc-swarm/evidence/performance-steer-20261011/runtime-prerequisites/attempt-10/attempt-10/provider-content/objects/sha256/e5/87/e587078b0b8d8bce38d4e665999b9a3a0d4d9fb1de1b87a8c3a4b223e71e50f6')
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
BUILD_SECONDS=90
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

def checked_path(path:pathlib.Path,anchor:pathlib.Path,kind:str='file',must_exist:bool=True,uid:int|None=None)->pathlib.Path:
 """Reject symlink traversal and paths outside an owned namespace."""
 uid=os.getuid() if uid is None else uid
 anchor=pathlib.Path(os.path.abspath(anchor));path=pathlib.Path(os.path.abspath(path))
 try: rel=path.relative_to(anchor)
 except ValueError: raise GuardFailure(f'path escapes owned namespace: {path}')
 # Verify every ancestor for symlinks/non-directories; only the declared
 # namespace anchor and descendants must be owned by this process UID.
 ancestors=list(reversed(anchor.parents))+[anchor]
 for ancestor in ancestors:
  if not ancestor.exists():raise GuardFailure(f'missing namespace ancestor: {ancestor}')
  ast=ancestor.lstat()
  if stat.S_ISLNK(ast.st_mode) or not stat.S_ISDIR(ast.st_mode):raise GuardFailure(f'unsafe namespace ancestor: {ancestor}')
 if anchor.exists():
  ast=anchor.lstat()
  if stat.S_ISLNK(ast.st_mode) or not stat.S_ISDIR(ast.st_mode) or ast.st_uid!=uid:
   raise GuardFailure(f'unsafe namespace anchor: {anchor}')
 current=anchor
 parts=rel.parts
 for i,part in enumerate(parts):
  current=current/part
  if not current.exists() and not current.is_symlink():
   if must_exist or i<len(parts)-1:raise GuardFailure(f'missing path component: {current}')
   continue
  st=current.lstat()
  if stat.S_ISLNK(st.st_mode):raise GuardFailure(f'symlink path component: {current}')
  if st.st_uid!=uid:raise GuardFailure(f'path component has wrong owner: {current}')
  final=i==len(parts)-1
  if not final and not stat.S_ISDIR(st.st_mode):raise GuardFailure(f'non-directory path component: {current}')
  if final and kind=='file' and not stat.S_ISREG(st.st_mode):raise GuardFailure(f'expected regular file: {current}')
  if final and kind=='dir' and not stat.S_ISDIR(st.st_mode):raise GuardFailure(f'expected directory: {current}')
 if not rel.parts:
  st=anchor.lstat()
  if kind=='dir' and not stat.S_ISDIR(st.st_mode):raise GuardFailure(f'expected directory: {anchor}')
 return path

def allocated_bytes(root:pathlib.Path)->int:
 if not root.exists():return 0
 total=0
 for p in (root,*root.rglob('*')):
  try:
   st=p.lstat()
   if stat.S_ISLNK(st.st_mode):raise GuardFailure(f'symlink in owned allocation tree: {p}')
   total+=st.st_blocks*512
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
  if numeric is None:
   try:numeric=proc.wait(timeout=2)
   except subprocess.TimeoutExpired:stop=stop or 'child did not exit after bounded termination grace'
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
  census_ok=True
  try:
   rows=snapshot(proc);remaining=group_members(rows,pgid)
  except Exception as e:
   rows={};remaining=[];census_ok=False;stop=stop or f'final process census failed: {e!r}'
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
  quiescent=census_ok and not remaining
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
 checked_path(source,PACK_LAYOUT,'file')
 checked_path(destination,RAM,'file',must_exist=False)
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

def validate_input_paths(index_path:pathlib.Path,offset_path:pathlib.Path,pack_path:pathlib.Path)->dict:
 paths=(pathlib.Path(os.path.abspath(index_path)),pathlib.Path(os.path.abspath(offset_path)),pathlib.Path(os.path.abspath(pack_path)))
 expected=(INDEX_SOURCE,OFFSET_SOURCE,PACK_SOURCE)
 if paths!=expected:raise GuardFailure(f'frozen benchmark input paths mismatch: {paths!r}')
 return {'index_path':str(paths[0]),'offset_index_path':str(paths[1]),'pack_path':str(paths[2])}

def verify_source_and_embeds(source:pathlib.Path)->dict:
 checked_path(source,RAM,'dir')
 manifest_path=checked_path(BASE/'source-manifest.json',BASE,'file');manifest_bytes=manifest_path.read_bytes()
 if hashlib.sha256(manifest_bytes).hexdigest()!=SOURCE_MANIFEST_SHA:raise GuardFailure('source manifest SHA mismatch')
 manifest=json.loads(manifest_bytes)
 if manifest['source_commit']!=SOURCE_COMMIT or manifest['source_tree']!=SOURCE_TREE:raise GuardFailure('source manifest commit/tree mismatch')
 tracked=[]
 for e in manifest['snapshot_files']:
  p=checked_path(source/e['path'],source,'file');n,h=sha_file(p)
  if n!=e['bytes'] or h!=e['sha256']:raise GuardFailure('tracked input mismatch: '+e['path'])
  tracked.append((e['path'],h,n))
 gh=hashlib.sha256()
 for rel,h,n in sorted(tracked):gh.update(rel.encode()+b'\0'+h.encode()+b'\0'+str(n).encode()+b'\n')
 if len(tracked)!=227 or sum(x[2] for x in tracked)!=1541402:raise GuardFailure('tracked source count/size mismatch')
 gen_path=checked_path(WORK/'generated-inputs-receipt.json',WORK,'file')
 gen_receipt=json.loads(gen_path.read_text())
 if gen_receipt['source_commit']!=SOURCE_COMMIT or gen_receipt['generated_embed_tree_sha256']!=GENERATED_TREE_SHA:raise GuardFailure('generated embed receipt identity mismatch')
 embeds=[]
 for rel,meta in gen_receipt['generated_embed_inputs'].items():
  n,h=sha_file(checked_path(source/rel,source,'file'))
  if n!=meta['size'] or h!=meta['sha256']:raise GuardFailure('generated embed mismatch: '+rel)
  embeds.append((rel,h,n))
 tree=hashlib.sha256()
 for rel,h,n in sorted(embeds):tree.update(rel.encode()+b'\0'+h.encode()+b'\0'+str(n).encode()+b'\n')
 # The generator's canonical tree recipe is separately pinned in its receipt.
 if len(embeds)!=39 or sum(x[2] for x in embeds)!=7474027:raise GuardFailure('generated embed count/size mismatch')
 if tree.hexdigest()!=GENERATED_TREE_SHA:raise GuardFailure('generated embed tree digest mismatch')
 return {'source_tracked_count':len(tracked),'source_tracked_bytes':sum(x[2] for x in tracked),'source_tracked_tree_sha256':gh.hexdigest(),'generated_count':len(embeds),'generated_bytes':sum(x[2] for x in embeds),'generated_tree_sha256':tree.hexdigest(),'source_manifest_sha256':hashlib.sha256(manifest_path.read_bytes()).hexdigest()}

def verify_module_cache(source:pathlib.Path,manifest_path:pathlib.Path,readonly_projection:bool=False,expected_tree:str|None=None,expected_count:int=1930,expected_total:int=55499478,source_anchor:pathlib.Path|None=None,manifest_anchor:pathlib.Path|None=None)->dict:
 checked_path(source,source_anchor or RAM,'dir')
 checked_path(manifest_path,manifest_anchor or WORK,'file')
 manifest=json.loads(manifest_path.read_text())
 frozen_tree=expected_tree or '57a5f811603b86df1c2cc2cdf64bdca47eaca0ced8cca6bb8382caa776729e43'
 if manifest.get('tree_sha256')!=frozen_tree:raise GuardFailure('offline module cache manifest identity mismatch')
 tree=hashlib.sha256(); actual_tree=hashlib.sha256(); total=0
 expected_files={item['path'] for item in manifest['files']}
 expected_dirs=set()
 for rel in expected_files:
  parts=pathlib.PurePosixPath(rel).parts
  for i in range(1,len(parts)):expected_dirs.add('/'.join(parts[:i]))
 actual_files=set();actual_dirs=set()
 for entry in source.rglob('*'):
  rel=entry.relative_to(source).as_posix();st=entry.lstat()
  if stat.S_ISLNK(st.st_mode) or st.st_uid!=os.getuid():raise GuardFailure(f'unsafe offline module cache entry: {rel}')
  if stat.S_ISDIR(st.st_mode):actual_dirs.add(rel)
  elif stat.S_ISREG(st.st_mode):actual_files.add(rel)
  else:raise GuardFailure(f'non-file module cache entry: {rel}')
 if actual_files!=expected_files or not actual_dirs.issubset(expected_dirs):raise GuardFailure('offline module cache contains missing or unexpected entries')
 for item in manifest['files']:
  p=checked_path(source/item['path'],source,'file');n,h=sha_file(p)
  mode=stat.S_IMODE(p.stat().st_mode)
  expected_mode=item['mode']&~0o222 if readonly_projection else item['mode']
  if (n,h,mode)!=(item['size'],item['sha256'],expected_mode):raise GuardFailure('offline module cache file/mode mismatch: '+item['path'])
  total+=n
  tree.update(item['path'].encode()+b'\0'+str(n).encode()+b'\0'+str(item['mode']).encode()+b'\0'+h.encode()+b'\n')
  actual_tree.update(item['path'].encode()+b'\0'+str(n).encode()+b'\0'+str(mode).encode()+b'\0'+h.encode()+b'\n')
 if len(manifest['files'])!=expected_count or total!=expected_total or tree.hexdigest()!=manifest['tree_sha256']:raise GuardFailure('offline module cache closure mismatch')
 return {'file_count':len(manifest['files']),'logical_bytes':total,'tree_sha256':tree.hexdigest(),'observed_modes_tree_sha256':actual_tree.hexdigest(),'readonly_projection':readonly_projection}

def validate_result_jsonl(path:pathlib.Path)->dict:
 checked_path(path,path.parent,'file')
 raw=path.read_bytes()
 if not raw.endswith(b'\n') or len(raw)>RESULT_CAP:raise GuardFailure('harness JSONL missing final newline or exceeds cap')
 lines=raw.splitlines()
 if len(lines)!=12:raise GuardFailure(f'harness JSONL event count {len(lines)} != 12')
 try:events=[json.loads(x) for x in lines]
 except Exception as e:raise GuardFailure(f'harness JSONL parse failed: {e!r}')
 schedule=SCHEDULE
 start=events[0]
 if start.get('event')!='run_start':raise GuardFailure('harness JSONL missing run_start')
 initial=start.get('result',{})
 expected={'status':'RUNNING','source_commit':SOURCE_COMMIT,'index_sha256':INDEX_SHA,'offset_index_sha256':OFFSET_SHA,'pack_sha256':PACK_SHA,'untimed_pack_slice_count':4649,'untimed_pack_slice_bytes':PACK_BYTES,'selected_count':4649,'selected_bytes':PACK_BYTES,'pack_file_opens_before_timing':1,'schedule':schedule}
 for key,value in expected.items():
  if initial.get(key)!=value:raise GuardFailure(f'harness run_start field {key} mismatch')
 pack_identity=initial.get('untimed_pack_ordered_sha256','')
 if not re.fullmatch(r'[0-9a-f]{64}',pack_identity):raise GuardFailure('invalid untimed pack identity')
 if len(initial.get('untimed_controls_passed',[]))<5:raise GuardFailure('too few harness untimed negative controls')
 reps={'filesystem':0,'pack':0}
 for pos,arm in enumerate(schedule,1):
  event=events[pos]
  if event.get('event')!='pass_complete' or event.get('position')!=pos:raise GuardFailure(f'harness missing pass_complete at {pos}')
  p=event.get('pass',{})
  reps[arm]+=1
  required={'arm':arm,'replicate':reps[arm],'objects':4649,'bytes':PACK_BYTES,'provider_getobject_calls':4649 if arm=='filesystem' else 0,'pack_file_opens_during_pass':0,'read_at_calls':4649 if arm=='pack' else 0}
  for key,value in required.items():
   if p.get(key)!=value:raise GuardFailure(f'harness pass {pos} field {key} mismatch')
  if p.get('ordered_result_sha256')!=pack_identity:raise GuardFailure(f'harness pass {pos} ordered digest differs from untimed pack identity')
  for key in ('wall_ns','cpu_user_ns','cpu_system_ns','proc_io_rchar_delta','proc_io_read_bytes_delta','proc_io_syscr_delta','rss_bytes_sampled_after_pass'):
   if not isinstance(p.get(key),int) or p[key]<0:raise GuardFailure(f'harness pass {pos} invalid {key}')
 complete=events[-1]
 if complete.get('event')!='run_complete':raise GuardFailure('harness JSONL missing final run_complete')
 final=complete.get('result',{})
 if final.get('status')!='PASS' or len(final.get('passes',[]))!=10:raise GuardFailure('harness final receipt status/pass count mismatch')
 for key,value in expected.items():
  if key!='status' and key in final and final.get(key)!=value:raise GuardFailure(f'harness final field {key} mismatch')
 if final.get('source_commit')!=SOURCE_COMMIT or final.get('untimed_pack_ordered_sha256')!=pack_identity:raise GuardFailure('harness final source or pack identity mismatch')
 if final.get('untimed_pack_slice_count')!=4649 or final.get('untimed_pack_slice_bytes')!=PACK_BYTES or final.get('pack_file_opens_before_timing')!=1 or final.get('schedule')!=schedule:raise GuardFailure('harness final pack controls/schedule mismatch')
 if len(final.get('untimed_controls_passed',[]))<5:raise GuardFailure('harness final receipt lacks negative-control proof')
 for i in range(10):
  if final['passes'][i]!=events[i+1].get('pass'):raise GuardFailure(f'harness final pass summary differs from event {i+1}')
 for key in ('source_closure_before_sha256','source_closure_after_sha256','index_sha256','index_after_sha256','offset_index_sha256','offset_index_after_sha256','pack_sha256','pack_after_sha256'):
  if not re.fullmatch(r'[0-9a-f]{64}',final.get(key,'')):raise GuardFailure(f'harness final receipt missing {key}')
 if final['source_closure_before_sha256']!=final['source_closure_after_sha256'] or final['index_sha256']!=final['index_after_sha256'] or final['offset_index_sha256']!=final['offset_index_after_sha256'] or final['pack_sha256']!=final['pack_after_sha256']:
  raise GuardFailure('harness final input closure hashes changed')
 return {'jsonl_sha256':hashlib.sha256(raw).hexdigest(),'jsonl_bytes':len(raw),'event_count':len(events),'pass_count':10,'selected_count':4649,'selected_bytes':PACK_BYTES}

def final_resources(budget:Budget)->dict:
 free=free_bytes(WORK);ram=allocated_bytes(RAM);workspace=allocated_bytes(WORK);rss=own_rss_bytes()
 if free<FLOOR:raise GuardFailure('final workspace free-space floor breached')
 if ram>RAM_CAP:raise GuardFailure('final owned RAM cap exceeded')
 if workspace>budget.cap:raise GuardFailure('final workspace allocation cap exceeded')
 if rss>RSS_CAP:raise GuardFailure('final supervisor RSS cap exceeded')
 return {'workspace_free_bytes':free,'owned_ram_allocated_bytes':ram,'workspace_allocated_bytes':workspace,'supervisor_rss_bytes_sampled':rss}

def readonly_copy_tree(source:pathlib.Path,dest:pathlib.Path)->None:
 checked_path(source,RAM,'dir')
 checked_path(dest,RAM,'dir',must_exist=False)
 if dest.exists():raise GuardFailure('refusing to overwrite read-only module cache copy')
 shutil.copytree(source,dest,symlinks=True)
 for p in [dest,*dest.rglob('*')]:
  try:os.chmod(p,stat.S_IMODE(p.lstat().st_mode)&~0o222,follow_symlinks=False)
  except (NotImplementedError,TypeError):pass

def verify_go_metadata(env:dict)->dict:
 checked_path(GO,GO.parent,'file')
 n,h=sha_file(GO)
 if h!=GO_SHA:raise GuardFailure('official Go binary SHA mismatch')
 v=subprocess.run([str(GO),'version'],capture_output=True,text=True,env=env,timeout=10)
 if v.returncode!=0 or v.stdout.strip()!='go version go1.26.9 linux/amd64':raise GuardFailure(f'unexpected Go version output/exit: {v.returncode} {v.stdout.strip()!r}')
 e=subprocess.run([str(GO),'env','-json','GOVERSION','GOOS','GOARCH','CGO_ENABLED','GOFLAGS','GOTOOLCHAIN'],capture_output=True,text=True,env=env,timeout=10)
 if e.returncode!=0:raise GuardFailure(f'go env metadata failed: {e.returncode}')
 meta=json.loads(e.stdout)
 want={'GOVERSION':'go1.26.9','GOOS':'linux','GOARCH':'amd64','CGO_ENABLED':'0','GOFLAGS':'','GOTOOLCHAIN':'local'}
 if meta!=want:raise GuardFailure(f'Go environment metadata mismatch: {meta!r}')
 return {'binary_size':n,'binary_sha256':h,'version_stdout':v.stdout.strip(),'environment':meta,'version_exit':v.returncode,'env_exit':e.returncode}

def private_runtime_environment(runtime:pathlib.Path,base_env:dict|None=None)->tuple[dict,dict]:
 checked_path(runtime,RAM,'dir')
 home=runtime/'rcc-home'
 checked_path(home,RAM,'dir',must_exist=False)
 if home.exists():raise GuardFailure('refusing existing private RCC runtime home')
 os.mkdir(home,0o700)
 checked_path(home,RAM,'dir')
 mode=stat.S_IMODE(home.lstat().st_mode)
 if mode!=0o700:raise GuardFailure(f'private RCC runtime home mode is {oct(mode)}, expected 0o700')
 env=dict(os.environ if base_env is None else base_env)
 removed=sorted(k for k in env if k.startswith('RCC_') or k.startswith('ROBOCORP_'))
 for key in removed:env.pop(key,None)
 env['ROBOCORP_HOME']=str(home)
 env['RCC_HOLOTREE_MODE']='private'
 if env.get('HOME')!=os.environ.get('HOME'):raise GuardFailure('runtime isolation must not repurpose HOME')
 return env,{'home':str(home),'mode':mode,'removed_inherited_variable_names':removed,'holotree_mode':'private'}

def check_inherited_file_size_limit(limits:tuple[int,int]|None=None,required:int|None=None)->dict:
 """Require inherited soft and hard RLIMIT_FSIZE to cover all planned writes.

 This check never changes the current process limits. RLIM_INFINITY is
 represented as null in the receipt; finite Linux values are bytes.
 """
 if limits is None:limits=resource.getrlimit(resource.RLIMIT_FSIZE)
 if required is None:required=max(PACK_BYTES,BINARY_CAP)
 if len(limits)!=2:raise GuardFailure('RLIMIT_FSIZE did not return soft/hard pair')
 def normalize(value):
  if value==resource.RLIM_INFINITY:return None
  if not isinstance(value,int) or value<0:raise GuardFailure(f'invalid RLIMIT_FSIZE value: {value!r}')
  return value
 soft,hard=map(normalize,limits)
 if soft is not None and soft<required:
  raise GuardFailure(f'inherited RLIMIT_FSIZE soft limit {soft} bytes is below required {required}')
 if hard is not None and hard<required:
  raise GuardFailure(f'inherited RLIMIT_FSIZE hard limit {hard} bytes is below required {required}')
 return {'soft_limit_bytes':soft,'hard_limit_bytes':hard,'required_bytes':required,'checked_without_changing_parent_limits':True}

def verify_binary_build_metadata(raw:str)->None:
 lines=[line.strip().replace('\t',' ') for line in raw.splitlines()]
 if not lines or not re.fullmatch(r'.+: go1\.26\.9',lines[0]):raise GuardFailure('built binary metadata version header is not go1.26.9')
 tokens=[line.split() for line in lines[1:]]
 def has(key:str,value:str)->bool:
  return any((parts==['build',key,value] or parts==['build',key+'='+value]) for parts in tokens)
 for key,value in (('GOOS','linux'),('GOARCH','amd64'),('CGO_ENABLED','0')):
  if not has(key,value):raise GuardFailure(f'built binary metadata missing {key}={value}')

def execute(admission:pathlib.Path)->int:
 started=time.monotonic();global_deadline=started+TOTAL_SECONDS
 results={'status':'FAIL','source_commit':SOURCE_COMMIT,'numeric_build_exit':None,'numeric_harness_exit':None,'numeric_outer_exit':1,'stages':[],'inherited_rlimit_fsize':None,'limits':{'workspace_bytes':WORKSPACE_CAP,'ram_bytes':RAM_CAP,'sampled_rss_bytes':RSS_CAP,'floor_bytes':FLOOR,'total_seconds':TOTAL_SECONDS,'build_seconds':BUILD_SECONDS,'pass_seconds':PASS_SECONDS}}
 run_id='run-native-02';outdir=WORK/run_id
 budget=Budget();
 try:
  checked_path(WORK,WORK,'dir')
  checked_path(RAM,RAM,'dir')
  if outdir.exists():raise GuardFailure('refusing to reuse prior run directory')
  budget.preflight(BINARY_CAP+RAW_CAP+RESULT_CAP+SUMMARY_CAP)
  # Create the owned run directory only after reserving its complete fixed
  # output budget, so every later preflight failure can still get a receipt.
  checked_path(outdir,WORK,'dir',must_exist=False)
  os.mkdir(outdir,0o700)
  checked_path(outdir,WORK,'dir')
  # Reject inherited wrappers such as `ulimit -f` before any staged file copy.
  results['inherited_rlimit_fsize']=check_inherited_file_size_limit()
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
  checked_path(source_root,source_root,'dir')
  input_path_binding=validate_input_paths(index_path,offset_path,pack_source)
  for p,expected,size in ((index_path,INDEX_SHA,1491800),(offset_path,OFFSET_SHA,525841),(pack_source,PACK_SHA,PACK_BYTES)):
   anchor=INDEX_SOURCE.parent if p==INDEX_SOURCE else PACK_LAYOUT
   checked_path(p,anchor,'file')
   n,h=sha_file(p)
   if n!=size or h!=expected:raise GuardFailure(f'frozen input mismatch: {p}')
  binary=outdir/'rcc-go-packprobe'
  runtime=RAM/run_id
  checked_path(runtime,RAM,'dir',must_exist=False)
  if runtime.exists():raise GuardFailure('refusing to reuse RAM runtime directory')
  runtime.mkdir(mode=0o700)
  checked_path(runtime,RAM,'dir')
  env,rcc_env_binding=private_runtime_environment(runtime)
  if allocated_bytes(RAM)+PACK_BYTES+PACK_BYTES+128*1024*1024>RAM_CAP:raise GuardFailure('RAM prewrite reservation for pack/provider/build exceeds cap')
  ram_pack=runtime/'pack.bin';pack_receipt=copy_verified_pack(pack_source,ram_pack)
  provider=runtime/'provider';out=outdir/'results.jsonl'
  checked_path(provider,RAM,'dir',must_exist=False)
  checked_path(out,WORK,'file',must_exist=False)
  # Preserve the offline list source and create a new exact source copy for the candidate harness.
  run_source=RAM/'source-run-native-02'
  checked_path(run_source,RAM,'dir',must_exist=False)
  if run_source.exists():raise GuardFailure('refusing to reuse exact source copy')
  shutil.copytree(source,run_source,copy_function=shutil.copy2)
  probe=run_source/'prep-probe';shutil.rmtree(probe)
  probe.mkdir()
  main_go=probe/'main.go'
  fd=os.open(main_go,os.O_CREAT|os.O_EXCL|os.O_WRONLY,0o600)
  try:
   with os.fdopen(fd,'wb') as f:f.write((WORK/'harness-successor-v2.go').read_bytes())
  except BaseException:
   try:os.unlink(main_go)
   except OSError:pass
   raise
  checked_path(main_go,run_source,'file')
  bind2=verify_source_and_embeds(run_source)
  for p in [run_source,*run_source.rglob('*')]:
   if p.is_dir():os.chmod(p,p.stat().st_mode|0o700)
  ownmod=RAM/'gomodcache-run-native-02';readonly_copy_tree(RAM/'gomodcache',ownmod)
  for k in ('GOFLAGS','GOWORK','GOENV','GOTOOLCHAIN','GOPROXY','GOSUMDB','GOTELEMETRY','CGO_ENABLED','GOOS','GOARCH','GOMODCACHE','GOCACHE','GOTMPDIR','TMPDIR'):env.pop(k,None)
  owncache=RAM/'gocache-run-native-02';owntmp=RAM/'gotmp-run-native-02';owntmp.mkdir(mode=0o700)
  env.update({'GOENV':'off','GOTOOLCHAIN':'local','GOPROXY':'off','GOSUMDB':'off','GOTELEMETRY':'off','CGO_ENABLED':'0','GOOS':'linux','GOARCH':'amd64','GOFLAGS':'','GOMODCACHE':str(ownmod),'GOCACHE':str(owncache),'GOTMPDIR':str(owntmp),'TMPDIR':str(owntmp)})
  go_metadata=verify_go_metadata(env)
  run_cache_before=verify_module_cache(ownmod,WORK/'offline-gomodcache-manifest.json',readonly_projection=True)
  source_cache_before=verify_module_cache(RAM/'gomodcache',WORK/'offline-gomodcache-manifest.json')
  if allocated_bytes(RAM)+PACK_BYTES>RAM_CAP:raise GuardFailure('owned RAM prebuild cap exceeded')
  build_cmd=[str(GO),'build','-p=1','-mod=readonly','-trimpath','-buildvcs=false','-o',str(binary),'./prep-probe']
  budget.reserve=BINARY_CAP+RAW_CAP+RESULT_CAP+SUMMARY_CAP
  build=run_stage('build',build_cmd,run_source,env,outdir/'build.stdout',outdir/'build.stderr',budget,RAM,global_deadline,BUILD_SECONDS,False,BINARY_CAP)
  results['stages'].append(build);results['numeric_build_exit']=build['numeric_exit']
  if build['numeric_exit']!=0 or build['stop_reason']:raise GuardFailure('build stage failed')
  if not binary.is_file() or binary.is_symlink():raise GuardFailure('build output missing or nonregular')
  bn,bh=sha_file(binary)
  if bn>BINARY_CAP:raise GuardFailure('binary size cap exceeded')
  checked_path(binary,WORK,'file')
  bm=subprocess.run([str(GO),'version','-m',str(binary)],capture_output=True,text=True,env=env,timeout=5)
  if bm.returncode!=0 or len(bm.stdout.encode())>16384:raise GuardFailure('built binary metadata command failed or exceeded output cap')
  verify_binary_build_metadata(bm.stdout)
  budget.reserve=RAW_CAP+RESULT_CAP+SUMMARY_CAP
  if provider.exists() or out.exists():raise GuardFailure('refusing existing provider or results output before harness')
  run_cmd=[str(binary),'-source-root',str(source_root),'-index',str(index_path),'-offset-index',str(offset_path),'-pack',str(ram_pack),'-provider-root',str(provider),'-out',str(out)]
  stage=run_stage('harness',run_cmd,run_source,env,outdir/'harness.stdout',outdir/'harness.stderr',budget,RAM,global_deadline,TOTAL_SECONDS,True,RESULT_CAP)
  results['stages'].append(stage);results['numeric_harness_exit']=stage['numeric_exit']
  if stage['numeric_exit']!=0 or stage['stop_reason']:raise GuardFailure('harness stage failed')
  checked_path(provider,RAM,'dir')
  checked_path(out,WORK,'file')
  checked_path(runtime/'rcc-home',RAM,'dir')
  if stat.S_IMODE((runtime/'rcc-home').lstat().st_mode)!=0o700:raise GuardFailure('private RCC runtime home permissions changed')
  if os.stat(ram_pack).st_dev!=os.stat(provider).st_dev:raise GuardFailure('pack and provider do not reside on same filesystem')
  jsonl_binding=validate_result_jsonl(out)
  run_cache_after=verify_module_cache(ownmod,WORK/'offline-gomodcache-manifest.json',readonly_projection=True)
  source_cache_after=verify_module_cache(RAM/'gomodcache',WORK/'offline-gomodcache-manifest.json')
  if run_cache_before!=run_cache_after or source_cache_before!=source_cache_after:raise GuardFailure('offline module cache changed during run')
  nidx,hidx=sha_file(index_path);noff,hoff=sha_file(offset_path);npack,hpack=sha_file(pack_source)
  if (nidx,hidx)!=(1491800,INDEX_SHA) or (noff,hoff)!=(525841,OFFSET_SHA) or (npack,hpack)!=(PACK_BYTES,PACK_SHA):raise GuardFailure('original fixture input changed during run')
  final_guard=final_resources(budget)
  results.update({'status':'PASS','binary_size':bn,'binary_sha256':bh,'binary_go_metadata_stdout':bm.stdout,'go_tool_metadata':go_metadata,'runtime_environment_binding':rcc_env_binding,'input_path_binding':input_path_binding,'offline_source_binding':binding,'run_source_binding':bind2,'offline_module_cache_binding':cache_binding,'run_module_cache_before':run_cache_before,'run_module_cache_after':run_cache_after,'source_module_cache_before':source_cache_before,'source_module_cache_after':source_cache_after,'pack_copy':pack_receipt,'harness_jsonl_binding':jsonl_binding,'final_resource_guard':final_guard,'elapsed_total_s':round(time.monotonic()-started,4),'numeric_outer_exit':0})
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
