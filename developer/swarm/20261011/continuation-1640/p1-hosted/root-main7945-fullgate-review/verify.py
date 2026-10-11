import json,hashlib,stat,zipfile,subprocess,datetime
from pathlib import Path,PurePosixPath
out=Path(__file__).parent;z=Path('/dev/shm/rcc-p1-239-exporter-review/review-evidence.zip');dst=Path('/dev/shm/rcc-main7945-fullgate-root');dst.mkdir(exist_ok=False)
assert z.stat().st_size==2167771 and hashlib.sha256(z.read_bytes()).hexdigest()=='7c54e6bcc13f7a62c2c85ac62e02edbb33731215d858e9b8ea24168501c30125'
rows=[]
with zipfile.ZipFile(z) as f:
 a=f.infolist();assert len(a)==29 and len(set(x.filename for x in a))==29 and sum(x.file_size for x in a)==10199861
 for x in a:
  p=PurePosixPath(x.filename);assert not p.is_absolute() and '..' not in p.parts and '\\' not in x.filename and not x.is_dir() and stat.S_IFMT(x.external_attr>>16) in (0,stat.S_IFREG)
  b=f.read(x);q=dst.joinpath(*p.parts);q.parent.mkdir(parents=True,exist_ok=True);q.write_bytes(b);rows.append({'path':x.filename,'bytes':len(b),'sha256':hashlib.sha256(b).hexdigest()})
cmd=['python3','/workspace/work/rcc-swarm/two-phase-release/scripts/release_publication.py','verify-review-evidence','--root',str(dst),'--source-sha','7945f956b428359928908a9f42e36417b113dafd','--version','v18.19.6','--run-id','38147141244','--artifact-id','11705567118','--manifest-sha','26a47203ec1375e2c60d362c5aa55ba0aa65e334c1b24bc07b16f973a585f62c','--preparation-zip-digest','sha256:75919093aaeb1a49bfa7898246b4bd8d2e56ea065db837a391fb0ec83d78c7f5','--exporter-sha','3fe6e200c0acc06d5920544a3bd5c3c036096855','--exporter-run-id','38155612008']
r=subprocess.run(cmd,capture_output=True,text=True,timeout=30);(out/'validator-command.json').write_text(json.dumps({'cmd':cmd,'numericExit':r.returncode,'stdout':r.stdout,'stderr':r.stderr},indent=2)+'\n');assert r.returncode==0,r.stderr
(out/'actual-member-inventory.json').write_text(json.dumps({'at':datetime.datetime.now(datetime.timezone.utc).isoformat(),'source':'7945f956b428359928908a9f42e36417b113dafd','actualZIPsha':'7c54e6bcc13f7a62c2c85ac62e02edbb33731215d858e9b8ea24168501c30125','actualZIPbytes':z.stat().st_size,'retainedActualZIP':str(z),'actualExtractedRoot':str(dst),'all29ActualSHAverified':rows},indent=2)+'\n')
for n in ['release-candidate-exit.json','release-candidate-v1.json','self-host-v1.json','large-stream-receipt.json']:
 p=dst/'evidence/release-candidate'/n;d=json.loads(p.read_text());print(n, json.dumps(d)[:11500])
