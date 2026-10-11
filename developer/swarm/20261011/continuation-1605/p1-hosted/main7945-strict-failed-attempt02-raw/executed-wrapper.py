#!/usr/bin/env python3
import os, sys, time, json, hashlib, shutil, subprocess, signal, threading, urllib.request, pathlib, stat, platform, glob

BASE='/workspace/work/rcc-swarm/evidence/continuation-20261011/p1-hosted/main7945-strict-admission-plan'
OWNED_BASE='/workspace/work/rcc-swarm/evidence/continuation-20261011/p1-hosted/main7945-strict-consumer'
OUT=OWNED_BASE+'/attempt-02'
BIN_SRC='/dev/shm/rcc-main7945-release-review-root/assets-review/rcc-linux64/assets/rcc-linux64'
BIN=OUT+'/rcc-linux64'
PROVIDER='/workspace/work/rcc-swarm/evidence/provider-runtime-1791665359/provider'
CARRIER_SRC='/workspace/work/rcc-swarm/evidence/provider-runtime-1791665359/trust-signed'
ROOTS_SRC='/workspace/work/rcc-swarm/evidence/provider-runtime-1791665359/trust-roots.json'
HELPER='/workspace/work/rcc-swarm/evidence/candidate-09796/strict-trust/gocache/f7/f77bb2bfc95e2f794c72ae07492ff454da999b914d9a28eaee1e1707594635b6-d/refresh-revocations'
SOURCE='/workspace/work/rcc-swarm/build-provenance'
ART='sha256:0fd0a3dfd8348e06a1d68e21ce9c5bf8e6830d40a2b2c1e14737e8b27570e26e'
HEX=ART.split(':')[1]
GUARD=2688548864
SCRATCH_CAP=469762048
RAW_CAP=4194304
RSS_CAP=536870912
PHASE_CAP=120
TOTAL_CAP=600
INTERVAL=.25
RAW=OUT+'/raw'

if os.path.exists(OUT): raise SystemExit('owned run root already exists; no overwrite/retry')

def free_bytes():
 s=os.statvfs('/workspace'); return s.f_bavail*s.f_frsize

def sha(p):
 h=hashlib.sha256()
 with open(p,'rb') as f:
  for b in iter(lambda:f.read(1<<20),b''): h.update(b)
 return h.hexdigest()

def raw_used():
 total=0
 for root,ds,fs in os.walk(OWNED_BASE,followlinks=False):
  for n in fs:
   p=os.path.join(root,n)
   if os.sep+'raw'+os.sep not in p: continue
   try: total+=os.lstat(p).st_size
   except FileNotFoundError: pass
 return total

def allocated_tree():
 total=0
 for root,ds,fs in os.walk(OWNED_BASE,followlinks=False):
  if os.path.basename(root)=='raw':
   ds[:]=[]; continue
  if root==OWNED_BASE or root==RAW or root.startswith(RAW+'/'):
   if root==OWNED_BASE: pass
   else: ds[:]=[]; continue
  if os.sep+'raw'+os.sep in root:
   ds[:]=[]; continue
  kept=[]
  for d in ds:
   p=os.path.join(root,d)
   try:
    st=os.lstat(p); total+=st.st_blocks*512
    if not stat.S_ISLNK(st.st_mode): kept.append(d)
   except FileNotFoundError: pass
  ds[:]=kept
  for n in fs:
   p=os.path.join(root,n)
   if p.endswith('/rcc-linux64'): continue
   try: total+=os.lstat(p).st_blocks*512
   except FileNotFoundError: pass
 return total

def process_rss():
 groups=set(active_groups)
 total=0; pids=[]
 for ent in os.scandir('/proc'):
  if not ent.name.isdigit(): continue
  try:
   pid=int(ent.name)
   with open(f'/proc/{pid}/stat','r') as f: s=f.read()
   tail=s[s.rfind(')')+2:].split()
   pgrp=int(tail[2])
   if pid==os.getpid() or pgrp in groups:
    with open(f'/proc/{pid}/statm','r') as f: pages=int(f.read().split()[1])
    rss=pages*os.sysconf('SC_PAGE_SIZE'); total+=rss; pids.append((pid,rss,pgrp))
  except (FileNotFoundError,ProcessLookupError,PermissionError,ValueError,IndexError): pass
 return total,pids

active_groups=set()
start=time.monotonic(); last_sample=0; last_phase_start=None
samples=[]; monitor_error=[]; stop_monitor=False

def sample(force=False):
 global last_sample
 now=time.monotonic()
 if not force and now-last_sample<INTERVAL: return
 last_sample=now
 free=free_bytes(); scratch=allocated_tree(); raw=raw_used(); rss,pids=process_rss()
 row={'elapsed':round(now-start,3),'freeBytes':free,'scratchAllocatedBytes':scratch,'rawBytes':raw,'ownedAggregateRssBytes':rss,'ownedProcesses':pids}
 samples.append(row)
 if len(samples)%4==0:
  append_raw('resource-samples.jsonl',(json.dumps(row,separators=(',',':'))+'\n').encode())
 if free<GUARD: raise RuntimeError(f'free-space guard breached: {free} < {GUARD}')
 if scratch>SCRATCH_CAP: raise RuntimeError(f'scratch cap breached: {scratch} > {SCRATCH_CAP}')
 if raw>RAW_CAP: raise RuntimeError(f'raw-output cap breached: {raw} > {RAW_CAP}')
 if rss>RSS_CAP: raise RuntimeError(f'aggregate RSS cap breached: {rss} > {RSS_CAP}')
 if now-start>TOTAL_CAP: raise RuntimeError(f'total time cap breached: {now-start:.3f}s')
 if last_phase_start is not None and now-last_phase_start>PHASE_CAP: raise RuntimeError(f'phase time cap breached: {now-last_phase_start:.3f}s')

def append_raw(name,data):
 p=os.path.join(RAW,name)
 current=raw_used()
 if current+len(data)>RAW_CAP: raise RuntimeError(f'prewrite raw cap: {current}+{len(data)} > {RAW_CAP}')
 with open(p,'ab') as f: f.write(data)

def write_json(name,obj):
 data=(json.dumps(obj,sort_keys=True,separators=(',',':'))+'\n').encode()
 append_raw(name,data)

def tree_manifest(root, outfile):
 h=hashlib.sha256(); count=0; nfiles=0; total=0
 with open(outfile,'wb') as out:
  for cur,ds,fs in os.walk(root,topdown=True,followlinks=False):
   ds.sort(); fs.sort()
   entries=[]
   for n in ds:
    p=os.path.join(cur,n); st=os.lstat(p)
    if stat.S_ISLNK(st.st_mode): entries.append((p,'L',st))
    else: entries.append((p,'D',st))
   for n in fs:
    p=os.path.join(cur,n); st=os.lstat(p); entries.append((p,'F' if stat.S_ISREG(st.st_mode) else 'L',st))
   for p,k,st in entries:
    rel=os.path.relpath(p,root).replace(os.sep,'/')
    if k=='F':
     digest=sha(p); size=st.st_size; total+=size; nfiles+=1
    elif k=='L': digest=hashlib.sha256(os.readlink(p).encode()).hexdigest(); size=len(os.readlink(p));
    else: digest='-'; size=0
    row=f'{k}\t{rel}\t{size}\t{digest}\t{stat.S_IMODE(st.st_mode):o}\n'.encode()
    h.update(row); out.write(row); count+=1
    if count%64==0: sample()
    if raw_used()>RAW_CAP: raise RuntimeError('raw cap exceeded while writing tree manifest')
 return {'treeSha256':h.hexdigest(),'entries':count,'regularFiles':nfiles,'regularBytes':total,'manifestFile':os.path.basename(outfile)}

def begin_phase(name):
 global last_phase_start
 last_phase_start=time.monotonic(); sample(True)
 event={'event':'phase-start','phase':name,'elapsed':round(last_phase_start-start,3)}
 append_raw('events.jsonl',(json.dumps(event,separators=(',',':'))+'\n').encode())

def end_phase(name,extra=None):
 global last_phase_start
 d=round(time.monotonic()-last_phase_start,3); last_phase_start=None; sample(True)
 event={'event':'phase-end','phase':name,'durationSeconds':d,'elapsed':round(time.monotonic()-start,3)}
 if extra: event.update(extra)
 append_raw('events.jsonl',(json.dumps(event,separators=(',',':'))+'\n').encode())
 if d>PHASE_CAP: raise RuntimeError(f'phase {name} exceeded {PHASE_CAP}s')

def run_cmd(label,argv,env=None,expected=0,timeout=120):
 begin_phase(label)
 outp=RAW+'/'+label+'.stdout'; errp=RAW+'/'+label+'.stderr'
 t0=time.monotonic()
 with open(outp,'wb') as o, open(errp,'wb') as e:
  p=subprocess.Popen(argv,stdout=o,stderr=e,env=env,start_new_session=True)
  active_groups.add(p.pid)
  breached=None
  try:
   while p.poll() is None:
    try: sample()
    except Exception as ex:
     breached=str(ex); os.killpg(p.pid,signal.SIGTERM); break
    if time.monotonic()-t0>timeout:
     breached=f'phase command timeout {timeout}s'; os.killpg(p.pid,signal.SIGTERM); break
    time.sleep(.05)
   if p.poll() is None:
    try: code=p.wait(timeout=2)
    except subprocess.TimeoutExpired:
     os.killpg(p.pid,signal.SIGKILL); code=p.wait()
   else: code=p.returncode
  finally:
   active_groups.discard(p.pid)
  time.sleep(.05); sample(True)
 if breached:
  write_json(label+'.result.json',{'argv':argv,'numericExit':code,'durationSeconds':round(time.monotonic()-t0,3),'breach':breached})
  raise RuntimeError(breached)
 result={'argv':argv,'numericExit':code,'durationSeconds':round(time.monotonic()-t0,3),'stdoutBytes':os.path.getsize(outp),'stderrBytes':os.path.getsize(errp)}
 write_json(label+'.result.json',result)
 end_phase(label,{'numericExit':code})
 if code!=expected: raise RuntimeError(f'{label} exited {code}, expected {expected}; one-run admission prevents retry')
 return result

def start_server(label,argv,env,timeout=20):
 begin_phase(label)
 outp=RAW+'/'+label+'.stdout'; errp=RAW+'/'+label+'.stderr'
 o=open(outp,'wb'); e=open(errp,'wb')
 p=subprocess.Popen(argv,stdout=o,stderr=e,env=env,start_new_session=True)
 active_groups.add(p.pid)
 url=None; deadline=time.monotonic()+timeout
 while time.monotonic()<deadline:
  sample()
  if p.poll() is not None: break
  try:
   with open(outp,'r',errors='replace') as f: txt=f.read()
   for line in txt.splitlines():
    try:
     v=json.loads(line)
     if v.get('url','').startswith('http://127.0.0.1:'): url=v['url']; break
    except json.JSONDecodeError: pass
  except FileNotFoundError: pass
  if url: break
  time.sleep(.05)
 if not url:
  # stop only the process started in this owned group; preserve logs/status
  if p.poll() is None: os.killpg(p.pid,signal.SIGTERM)
  try: code=p.wait(timeout=2)
  except subprocess.TimeoutExpired: os.killpg(p.pid,signal.SIGKILL); code=p.wait()
  active_groups.discard(p.pid); o.close(); e.close()
  write_json(label+'.result.json',{'argv':argv,'numericExit':code,'url':None,'failure':'server failed to announce loopback URL'})
  raise RuntimeError(f'{label}: server did not announce URL (exit {code})')
 end_phase(label,{'url':url,'processPid':p.pid})
 return {'process':p,'stdout':o,'stderr':e,'url':url,'label':label,'argv':argv}

def stop_server(srv):
 p=srv['process']; t=time.monotonic()
 if p.poll() is None:
  os.killpg(p.pid,signal.SIGINT)
  try: code=p.wait(timeout=5)
  except subprocess.TimeoutExpired:
   os.killpg(p.pid,signal.SIGTERM)
   try: code=p.wait(timeout=2)
   except subprocess.TimeoutExpired: os.killpg(p.pid,signal.SIGKILL); code=p.wait()
 else: code=p.returncode
 active_groups.discard(p.pid); srv['stdout'].close(); srv['stderr'].close()
 write_json(srv['label']+'.stop.json',{'numericExit':code,'durationSeconds':round(time.monotonic()-t,3),'signal':'SIGINT'})
 end_phase(srv['label']+'-stop',{'numericExit':code})
 if code!=0: raise RuntimeError(f"{srv['label']} stopped with {code}")
 return code

def copytree_monitored(src,dst):
 shutil.copytree(src,dst,symlinks=True,copy_function=shutil.copy2)
 sample(True)

def ensure_port_closed(url):
 hostport=url.split('//',1)[1]; host,port=hostport.split(':'); import socket
 s=socket.socket(); s.settimeout(.4)
 try:
  s.connect((host,int(port))); s.close(); return False
 except OSError: return True

os.makedirs(OUT,mode=0o700); os.makedirs(RAW,mode=0o700)
try:
 # Strict admitted preflight, new path only.
 begin_phase('preflight')
 free=free_bytes()
 admission=json.load(open(BASE+'/root-admission-1552.json'))
 max_res=admission['maximumReserved']
 if free < GUARD+max_res: raise RuntimeError(f'preflight capacity {free} below guard+reservation {GUARD+max_res}')
 if sha(BIN_SRC)!='5a6ae55f33e041e376ca4538c592e7c214b9da30cc6c1cf32cd62a0f65077e21' or os.stat(BIN_SRC).st_size!=22392994: raise RuntimeError('exact shipping source binary hash/size mismatch')
 if subprocess.check_output(['git','-C',SOURCE,'rev-parse','7945f956b428359928908a9f42e36417b113dafd^{tree}'],text=True).strip()!='49baf943b9d64ef79af56ec8f34eebe0385477b1': raise RuntimeError('immutable 7945 source object tree mismatch')
 shutil.copy2(BIN_SRC,BIN); os.chmod(BIN,0o700)
 if sha(BIN)!=sha(BIN_SRC): raise RuntimeError('workspace binary copy hash mismatch')
 end_phase('preflight',{'freeBytes':free,'reservedBytes':max_res,'binarySha256':sha(BIN),'binaryBytes':os.stat(BIN).st_size})
 # Capture identity and fixture baselines compactly.
 begin_phase('identity-and-fixture-baselines')
 source_meta={'commit':'7945f956b428359928908a9f42e36417b113dafd','tree':subprocess.check_output(['git','-C',SOURCE,'rev-parse','7945f956b428359928908a9f42e36417b113dafd^{tree}'],text=True).strip(),'sourceIdentityMethod':'read-only git object tree lookup by immutable commit SHA; no checkout or worktree operation','repositoryPath':SOURCE}
 write_json('source.json',source_meta)
 write_json('binary.json',{'path':BIN,'sha256':sha(BIN),'bytes':os.stat(BIN).st_size,'sourceSha256':sha(BIN_SRC),'sourceBytes':os.stat(BIN_SRC).st_size})
 original_hashes={p:sha(p) for p in [ROOTS_SRC]+glob.glob(CARRIER_SRC+'/**',recursive=True) if os.path.isfile(p)}
 write_json('trust-inputs-before.json',original_hashes)
 if original_hashes[ROOTS_SRC]!='aae748c9408f911d7a94f9010e68b12249aa95c80b954fffc3082b5537404d64': raise RuntimeError('padded root hash mismatch')
 provider_baseline=tree_manifest(PROVIDER,RAW+'/provider-before.tsv')
 write_json('provider-before.json',provider_baseline)
 end_phase('identity-and-fixture-baselines',{'providerFiles':provider_baseline['regularFiles'],'providerTreeSha256':provider_baseline['treeSha256']})
 # Copy only signed fixture carrier/public roots into new owned state.
 begin_phase('copy-trust-fixture')
 copytree_monitored(CARRIER_SRC,OUT+'/trust-carrier')
 shutil.copy2(ROOTS_SRC,OUT+'/trust-roots.json')
 for rel,h in original_hashes.items():
  dest=(OUT+'/trust-roots.json' if rel==ROOTS_SRC else OUT+'/trust-carrier/'+os.path.relpath(rel,CARRIER_SRC))
  if sha(dest)!=h: raise RuntimeError('owned trust fixture copy mismatch '+os.path.basename(dest))
 end_phase('copy-trust-fixture')
 # Create a new unsigned empty fresh revocation state only via owned loopback carrier.
 begin_phase('refresh-unsigned-empty-revocation-fixture')
 os.makedirs(OUT+'/revocation-import-root',mode=0o700)
 envbase=os.environ.copy(); envbase.update({'RCC_HOLOTREE_MODE':'private'})
 importer=start_server('revocation-import-server',[BIN,'cache','serve','--root',OUT+'/revocation-import-root','--listen','127.0.0.1:0','--json'],{**envbase,'ROBOCORP_HOME':OUT+'/importer-home'})
 helper_result=run_cmd('revocation-helper',[HELPER,importer['url'],ART,RAW+'/fresh-revocations.json'],env=envbase,timeout=30)
 stop_server(importer)
 if not ensure_port_closed(importer['url']): raise RuntimeError('owned importer port remains open after shutdown')
 rev=json.load(open(RAW+'/revocation-helper.stdout'))
 fresh_data=open(RAW+'/fresh-revocations.json','rb').read()
 if rev.get('revocationCount')!=0 or rev.get('httpImportWrite') is not True or rev.get('httpReadbackMatches') is not True: raise RuntimeError('fresh empty unsigned revocation helper receipt invalid')
 artifact_dir=OUT+'/trust-carrier/sha256/'+HEX
 os.makedirs(artifact_dir,exist_ok=True)
 shutil.copy2(RAW+'/fresh-revocations.json',artifact_dir+'/revocations.json')
 if sha(artifact_dir+'/revocations.json')!=hashlib.sha256(fresh_data).hexdigest(): raise RuntimeError('fresh snapshot carrier copy mismatch')
 write_json('fresh-revocation-status.json',{'sha256':hashlib.sha256(fresh_data).hexdigest(),'bytes':len(fresh_data),'fetchedAt':rev['fetchedAt'],'source':rev['source'],'revocationCount':0,'signed':False,'authorityClaim':False,'helperSourceSha256':'a8f3726ebd44ab5112a266cb29c2b4248e4c849f43ce3d07d7f35083cd665834','helperBinarySha256':'f77bb2bfc95e2f794c72ae07492ff454da999b914d9a28eaee1e1707594635b6'})
 # Version and offline strict verification.
 run_cmd('rcc-version',[BIN,'--version'],env={**envbase,'ROBOCORP_HOME':OUT+'/version-home'},timeout=20)
 verify_args=[BIN,'env','trust','verify','--artifact',ART,'--platform','linux_amd64','--builder','rcc-holotree-v12','--provenance',artifact_dir+'/provenance.json','--sbom',artifact_dir+'/sbom.json','--signatures',artifact_dir+'/signature.json','--revocations',artifact_dir+'/revocations.json','--trust-roots',OUT+'/trust-roots.json','--strict-remote','--json']
 run_cmd('offline-strict-trust-verify',verify_args,env={**envbase,'ROBOCORP_HOME':OUT+'/offline-home'},timeout=45)
 # Start original fixture provider GET-only; strict no-mutation via before/after tree.
 provider_srv=start_server('fixture-provider-server',[BIN,'cache','serve','--root',PROVIDER,'--listen','127.0.0.1:0','--json'],{**envbase,'ROBOCORP_HOME':OUT+'/server-home'})
 consumer_env={**envbase,'ROBOCORP_HOME':OUT+'/consumer-home','RCC_NO_BUILD':'1','CONDA_OFFLINE':'true','MAMBA_OFFLINE':'true','PIP_NO_INDEX':'1','UV_NO_INDEX':'1','HTTP_PROXY':'http://127.0.0.1:1','HTTPS_PROXY':'http://127.0.0.1:1','ALL_PROXY':'http://127.0.0.1:1','http_proxy':'http://127.0.0.1:1','https_proxy':'http://127.0.0.1:1','all_proxy':'http://127.0.0.1:1','NO_PROXY':'127.0.0.1,localhost','no_proxy':'127.0.0.1,localhost'}
 run_cmd('strict-cold-acquire',[BIN,'env','acquire','--artifact',ART,'--provider',provider_srv['url'],'--trust-carrier',OUT+'/trust-carrier','--trust-carrier-type','filesystem','--trust-roots',OUT+'/trust-roots.json','--strict-remote','--json'],env=consumer_env,timeout=120)
 stop_server(provider_srv)
 if not ensure_port_closed(provider_srv['url']): raise RuntimeError('fixture provider port remains open after shutdown')
 # Providerless local native execution with same acquired consumer home.
 exec_args=[BIN,'env','exec','--artifact',ART,'--trust-carrier',OUT+'/trust-carrier','--trust-carrier-type','filesystem','--trust-roots',OUT+'/trust-roots.json','--strict-remote','--json','--','python','-c','import sqlite3; print("sqlite3", sqlite3.sqlite_version)']
 run_cmd('providerless-native-sqlite-exec',exec_args,env=consumer_env,timeout=120)
 # Readiness, references, manifest/index and all consumer content hashes.
 begin_phase('independent-content-integrity')
 home=OUT+'/consumer-home'; artroot=home+'/artifacts/v1'
 candidates=glob.glob(artroot+'/content/manifests/sha256/*/*/'+HEX)
 if len(candidates)!=1: raise RuntimeError(f'expected one consumer manifest, found {len(candidates)}')
 manifest_path=candidates[0]; manifest_bytes=open(manifest_path,'rb').read(); manifest=json.loads(manifest_bytes)
 if manifest.get('artifactDigest')!=ART: raise RuntimeError('manifest artifact digest mismatch')
 index_desc=manifest['objectIndex']; idx_hex=index_desc['digest'].split(':',1)[1]
 idx_candidates=glob.glob(artroot+'/content/objects/sha256/*/*/'+idx_hex)
 if len(idx_candidates)!=1: raise RuntimeError(f'expected object-index content object, found {len(idx_candidates)}')
 index_path=idx_candidates[0]; index_bytes=open(index_path,'rb').read()
 if len(index_bytes)!=index_desc['size'] or hashlib.sha256(index_bytes).hexdigest()!=idx_hex: raise RuntimeError('object index descriptor digest/size mismatch')
 index=json.loads(index_bytes); entries=index.get('entries',[])
 if len(entries)!=5045: raise RuntimeError(f'object index entry count {len(entries)} != 5045')
 indexed_ok=0; indexed_rows=[]
 for ix,e in enumerate(entries):
  dig=e['storedDigest'].split(':',1)[1]; path=artroot+'/content/objects/sha256/'+dig[:2]+'/'+dig[2:4]+'/'+dig
  if not os.path.isfile(path): raise RuntimeError('indexed object missing '+dig)
  size=os.path.getsize(path); actual=sha(path)
  if size!=e['storedSize'] or actual!=dig: raise RuntimeError('indexed object mismatch '+dig)
  indexed_ok+=1; indexed_rows.append(f'{dig}\t{size}\t{actual}\n')
  if ix%64==0: sample()
 append_raw('indexed-object-hashes.tsv',''.join(indexed_rows).encode())
 # All CAS files under content objects, including metadata objects, individually hash to their name.
 cas_root=artroot+'/content/objects/sha256'; cas_rows=[]; cas_count=0; cas_bytes=0
 for cur,ds,fs in os.walk(cas_root):
  ds.sort(); fs.sort()
  for n in fs:
   p=os.path.join(cur,n); dig=n
   actual=sha(p); size=os.path.getsize(p)
   if actual!=dig: raise RuntimeError('CAS object path digest mismatch '+dig)
   cas_count+=1; cas_bytes+=size; cas_rows.append(f'{dig}\t{size}\t{actual}\n')
   if cas_count%64==0: sample()
 append_raw('consumer-cas-hashes.tsv',''.join(cas_rows).encode())
 if cas_count!=5049: raise RuntimeError(f'consumer CAS count {cas_count} != 5049')
 materializations=glob.glob(artroot+'/materializations/'+HEX)
 if len(materializations)!=1: raise RuntimeError(f'expected one materialization, found {len(materializations)}')
 mat=materializations[0]
 ready=json.load(open(mat+'/ready.json'))
 refs=json.load(open(mat+'/references.json'))
 if ready.get('artifactDigest')!=ART or ready.get('state')!='ready': raise RuntimeError('ready record not bound/ready')
 if refs.get('manifest')!=ART or refs.get('state')!='live' or len(refs.get('protected',[]))!=5049: raise RuntimeError('live reference root mismatch')
 active_leases=[]
 lease_dir=mat+'/leases'
 if os.path.exists(lease_dir):
  for cur,ds,fs in os.walk(lease_dir): active_leases.extend(os.path.join(cur,n) for n in fs if n.endswith('.json'))
 if active_leases: raise RuntimeError('lease records remain after execution: '+str(len(active_leases)))
 verification_files=glob.glob(artroot+'/verification/sha256_'+HEX+'.json')
 if len(verification_files)!=1: raise RuntimeError('strict trust verification record missing')
 verify=json.load(open(verification_files[0]))
 write_json('integrity-summary.json',{'manifestPath':os.path.relpath(manifest_path,home),'manifestSha256':hashlib.sha256(manifest_bytes).hexdigest(),'manifestArtifactDigest':manifest.get('artifactDigest'),'objectIndexPath':os.path.relpath(index_path,home),'objectIndexSha256':hashlib.sha256(index_bytes).hexdigest(),'objectIndexDigest':index_desc['digest'],'indexedObjectCount':len(entries),'indexedObjectsValidated':indexed_ok,'indexedObjectHashList':'indexed-object-hashes.tsv','consumerCASCount':cas_count,'consumerCASTotalBytes':cas_bytes,'consumerCASHashList':'consumer-cas-hashes.tsv','ready':ready,'referenceState':refs.get('state'),'protectedReferenceCount':len(refs.get('protected',[])),'activeLeaseRecordCount':len(active_leases),'trustVerification':verify})
 end_phase('independent-content-integrity',{'indexedObjects':indexed_ok,'consumerCASObjects':cas_count})
 # Immutable fixture/binary verification after execution.
 begin_phase('final-immutability')
 provider_after=tree_manifest(PROVIDER,RAW+'/provider-after.tsv')
 write_json('provider-after.json',provider_after)
 if provider_after['treeSha256']!=provider_baseline['treeSha256'] or provider_after['entries']!=provider_baseline['entries']: raise RuntimeError('original provider fixture changed during GET-only serving')
 final_trust={p:sha(p) for p in [ROOTS_SRC]+glob.glob(CARRIER_SRC+'/**',recursive=True) if os.path.isfile(p)}
 if final_trust!=original_hashes: raise RuntimeError('original trust root/carrier changed')
 if sha(BIN)!=sha(BIN_SRC): raise RuntimeError('candidate binary changed')
 write_json('final-immutability.json',{'providerBefore':provider_baseline,'providerAfter':provider_after,'providerUnchanged':True,'originalTrustInputsUnchanged':True,'paddedRootSha256':final_trust[ROOTS_SRC],'candidateBinarySha256':sha(BIN),'candidateBinaryUnchanged':True,'sourceHead':source_meta['commit'],'sourceTree':source_meta['tree']})
 end_phase('final-immutability')
 # Final receipt with command exits captured from all *.result.json entries.
 results=[]
 for p in glob.glob(RAW+'/*.result.json'):
  d=json.load(open(p)); results.append({'receipt':os.path.basename(p),'numericExit':d['numericExit']})
 write_json('acceptance-receipt.json',{'status':'PASS','sourceCommit':source_meta['commit'],'sourceTree':source_meta['tree'],'binarySha256':sha(BIN),'binaryBytes':os.stat(BIN).st_size,'artifactDigest':ART,'paddedTrustRootSha256':final_trust[ROOTS_SRC],'freshRevocation':json.load(open(RAW+'/fresh-revocation-status.json')),'commands':results,'indexedObjectsVerified':indexed_ok,'casObjectsVerified':cas_count,'liveProtectedReferences':len(refs['protected']),'readyState':ready.get('state'),'activeLeaseRecordsAfterExecution':len(active_leases),'providerUnchanged':True,'originalTrustCarrierAndRootUnchanged':True,'unsignedRevocationFixtureOnly':True,'platformScope':'Linux amd64 only; no other native platform acceptance implied.'})
 print(json.dumps({'status':'PASS','output':OUT,'commands':len(results),'indexed':indexed_ok,'cas':cas_count,'elapsed':round(time.monotonic()-start,3),'freeBytes':free_bytes(),'scratchAllocatedBytes':allocated_tree(),'rawBytes':raw_used(),'rssBytes':process_rss()[0]},indent=2))
except BaseException as ex:
 try:
  write_json('run-status.json',{'status':'FAILED_OR_STOPPED','error':str(ex),'elapsedSeconds':round(time.monotonic()-start,3),'freeBytes':free_bytes(),'scratchAllocatedBytes':allocated_tree(),'rawBytes':raw_used(),'ownedAggregateRssBytes':process_rss()[0]})
 except Exception: pass
 # Do not clean the owned run directory. Stop any tracked process groups, then prove quiescence.
 stop_results=[]
 for pg in list(active_groups):
  try: os.killpg(pg,signal.SIGTERM)
  except ProcessLookupError: pass
  time.sleep(.2)
  try: os.killpg(pg,0); alive=True
  except ProcessLookupError: alive=False
  if alive:
   try: os.killpg(pg,signal.SIGKILL)
   except ProcessLookupError: pass
  stop_results.append({'processGroup':pg,'aliveAfterTerm':alive})
  active_groups.discard(pg)
 try: write_json('failure-quiescence.json',{'ownedProcessGroups':stop_results,'elapsedSeconds':round(time.monotonic()-start,3)})
 except Exception: pass
 print(f'RUN_STOPPED: {type(ex).__name__}: {ex}',file=sys.stderr)
 raise
