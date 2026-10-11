import os,sys,json,time,resource,hashlib,subprocess,threading,signal
from pathlib import Path
p=Path(__file__).parent
w=Path('/workspace/work/rcc-swarm/evidence/performance-admission/go-object-pack-native-preexecution-1600/embed-7945-preparation')
cfg=json.loads((p/'root-admission-native-v8.json').read_text()); controller=w/'supervise_native_v8.py'
assert hashlib.sha256(controller.read_bytes()).hexdigest()==cfg['supervisor_sha256']
limits=resource.getrlimit(resource.RLIMIT_FSIZE)
assert all(v==resource.RLIM_INFINITY or v>=28*1024**2 for v in limits),limits
started=time.monotonic(); error=[]; buffers={};lock=threading.Lock()
proc=subprocess.Popen([sys.executable,str(controller),'--admission-json',str(p/'root-admission-native-v8.json')],stdout=subprocess.PIPE,stderr=subprocess.PIPE,start_new_session=True,env=dict(os.environ,PYTHONDONTWRITEBYTECODE='1',TMPDIR='/dev/shm'))
def read(name,stream):
 data=bytearray()
 try:
  while True:
   chunk=stream.read1(256)
   if not chunk:break
   if len(data)+len(chunk)>2048:raise RuntimeError(name+' outer capture exceeded prewrite cap')
   data.extend(chunk)
 except BaseException as e:
  with lock:error.append(str(e))
  try:os.killpg(proc.pid,signal.SIGTERM)
  except ProcessLookupError:pass
 finally:buffers[name]=bytes(data);stream.close()
threads=[threading.Thread(target=read,args=(n,s)) for n,s in [('stdout',proc.stdout),('stderr',proc.stderr)]]
for t in threads:t.start()
try:code=proc.wait(timeout=195)
except subprocess.TimeoutExpired:
 error.append('outer195secondtimeout');os.killpg(proc.pid,signal.SIGTERM)
 try:code=proc.wait(timeout=3)
 except subprocess.TimeoutExpired:os.killpg(proc.pid,signal.SIGKILL);code=proc.wait(timeout=3)
for t in threads:t.join(timeout=3)
joined=all(not t.is_alive() for t in threads)
for n in ['stdout','stderr']:(p/('actual-v8-outer.'+n)).write_bytes(buffers.get(n,b''))
r={'utc':time.strftime('%Y-%m-%dT%H:%M:%SZ',time.gmtime()),'numeric_outer_exit':code,'elapsed_s':time.monotonic()-started,'parent_rlimit_fsize':limits,'reader_threads_joined':joined,'errors':error,'supervisor_sha256':cfg['supervisor_sha256'],'admission_sha256':hashlib.sha256((p/'root-admission-native-v8.json').read_bytes()).hexdigest(),'outer_raw':{n:{'bytes':len(buffers.get(n,b'')),'sha256':hashlib.sha256(buffers.get(n,b'')).hexdigest()} for n in ['stdout','stderr']}}
(p/'actual-v8-outer-result.json').write_text(json.dumps(r,indent=2)+'\n');print(json.dumps(r));print(buffers.get('stdout',b'').decode());sys.exit(code if code>=0 else 1)
