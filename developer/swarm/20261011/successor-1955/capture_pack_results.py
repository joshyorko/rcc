#!/usr/bin/env python3
import hashlib, json, shutil, stat, subprocess, zipfile
from pathlib import Path

p = Path(__file__).parent
root = Path('/workspace/rcc-successor-experiments-2015/pack')
out = p/'pack-e2e/results'
out.mkdir()
for name in ('prepared.json','fixture.json','measurements.jsonl','negative-controls.jsonl'):
    shutil.copyfile(root/name,out/name)
entries=[]
for f in sorted(root.rglob('*')):
    s=f.lstat(); row={'path':str(f.relative_to(root)), 'mode':oct(stat.S_IMODE(s.st_mode)), 'uid':s.st_uid,'gid':s.st_gid,'bytes':s.st_size,'type':'other'}
    if stat.S_ISREG(s.st_mode):
        row.update(type='file',sha256=hashlib.sha256(f.read_bytes()).hexdigest())
    elif stat.S_ISDIR(s.st_mode): row['type']='directory'
    elif stat.S_ISLNK(s.st_mode):row.update(type='symlink',target=str(f.readlink()))
    entries.append(row)
(out/'scratch-inventory.json').write_text(json.dumps(entries,indent=2)+'\n')
with zipfile.ZipFile(out/'fixture-provider-closure.zip','x',zipfile.ZIP_DEFLATED) as z:
    for prefix in ('filesystem-provider','producer-home','packed-sidecar'):
        for f in sorted((root/prefix).rglob('*')):
            if f.is_file() and not f.is_symlink():z.write(f,str(f.relative_to(root)))
for f in sorted((root/'workers').rglob('*')):
    if f.is_file() and f.name in ('cpu.pprof','worker.jsonl','worker.log'):
        target=out/'workers'/f.relative_to(root/'workers');target.parent.mkdir(parents=True,exist_ok=True);shutil.copyfile(f,target)
go='/workspace/rcc-successor-tools/mod/golang.org/toolchain@v0.0.1-go1.26.9.linux-amd64/bin/go'
for f in sorted((out/'workers').glob('*/cpu.pprof')):
    r=subprocess.run([go,'tool','pprof','-top','-nodecount=15','/workspace/rcc-successor-tools/pack-e2e',str(f)],capture_output=True,text=True)
    (f.parent/'cpu-top.txt').write_text(r.stdout+r.stderr)
    assert r.returncode==0
print(json.dumps({'retained':len(entries),'logical_regular_bytes':sum(x['bytes'] for x in entries if x['type']=='file'),'closure_zip_bytes':(out/'fixture-provider-closure.zip').stat().st_size}))
