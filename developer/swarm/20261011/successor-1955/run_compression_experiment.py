#!/usr/bin/env python3
"""New counterbalanced filesystem-provider gzip comparison and warm execution."""
import hashlib, json, os, shutil, stat, subprocess, time, zipfile
from pathlib import Path

p=Path(__file__).parent
out=p/'compression-e2e/results'
root=Path('/workspace/rcc-successor-compression-2018')
source=p/'compression-e2e/source/main.go'
binary=Path('/workspace/rcc-successor-tools/compression-e2e')
python='/opt/codex/runtimes/codex-primary-runtime/dependencies/python/bin/python3'
global_ht=Path('/workspace/rcc-successor-lifecycle/global-ht')
expected='2d1ae24dd6aa2a4c7347b7aa30fb27256c990cd3ff2cae8d8f46b303e19d2dc0'
assert hashlib.sha256(source.read_bytes()).hexdigest()==expected
out.mkdir(mode=0o700);root.mkdir(mode=0o700);home=root/'coordinator-home';home.mkdir(mode=0o700)
binding={'base_source':'7945f956b428359928908a9f42e36417b113dafd','classification':'private diagnostic with overlays; gzip wire format retained; not shipping binary','build_result':'compression-e2e/build-1/result.json','files':[{'path':str(f),'bytes':f.stat().st_size,'sha256':hashlib.sha256(f.read_bytes()).hexdigest()} for f in [source,binary,p/'prototype-overlay.json',p/'functions_private_linux.go.txt',p/'platform_linux_isolated.go.txt',p/'run_bounded_v3.py']]}
def guard(path,mode=0o700):
    s=path.lstat();assert stat.S_ISDIR(s.st_mode) and stat.S_IMODE(s.st_mode)==mode,(str(path),oct(s.st_mode));assert (s.st_uid,s.st_gid)==(os.geteuid(),os.getegid()),str(path)
    return {'path':str(path),'mode':oct(mode),'uid':s.st_uid,'gid':s.st_gid}
guard(out)
(out/'build-binding.json').write_text(json.dumps(binding,indent=2)+'\n')
def append(v):
    with (out/'controller.jsonl').open('a') as f:f.write(json.dumps(v)+'\n');f.flush();os.fsync(f.fileno())
def closure_hash(fixture):
    h=hashlib.sha256()
    for prefix in ('filesystem-provider','producer-home','packed-sidecar','fixture.json','prepared.json'):
        item=fixture/prefix;items=sorted(item.rglob('*')) if item.is_dir() else [item]
        for f in items:
            st=f.lstat();h.update((str(f.relative_to(fixture))+'\0'+oct(stat.S_IMODE(st.st_mode))+'\0').encode())
            if stat.S_ISREG(st.st_mode):h.update(f.read_bytes())
            elif not stat.S_ISDIR(st.st_mode):raise AssertionError(f)
    return h.hexdigest()
def run(label,args,worker_home,guards):
    pre=[guard(x) for x in guards]+[guard(global_ht,0o750)]
    env=dict(os.environ,ROBOCORP_HOME=str(worker_home),RCC_HOLOTREE_MODE='private',GOMAXPROCS='2',GOMEMLIMIT='1GiB',GOTELEMETRY='off')
    cmd=['python3',str(p/'run_bounded_v3.py'),'--out',str(out/label),'--seconds','30','--scratch-root',str(root),str(binary)]+args
    started=time.monotonic();r=subprocess.run(cmd,env=env,capture_output=True,text=True)
    (out/(label+'.launcher.stdout')).write_text(r.stdout);(out/(label+'.launcher.stderr')).write_text(r.stderr)
    # All guards are inspected even when the native process fails. No repairs.
    post=[guard(x) for x in guards]+[guard(global_ht,0o750)]
    append({'label':label,'returncode':r.returncode,'controller_elapsed_seconds':time.monotonic()-started,'pre':pre,'post':post})
    assert r.returncode==0,(label,r.stdout,r.stderr)

prepared={};hashes={};fixtures={}
for level in (1,6):
    fixture=root/f'gzip-{level}';fixture.mkdir(mode=0o700);fixtures[level]=fixture
    run(f'prepare-{level}',['-mode=prepare','-root',str(fixture),'-python',python,'-gzip-level',str(level)],home,[root,home,fixture])
    prepared[level]=json.loads((fixture/'prepared.json').read_text());hashes[level]=closure_hash(fixture)
    guard(fixture/'producer-home')
    for name in ('prepared.json','fixture.json'):shutil.copyfile(fixture/name,out/f'gzip-{level}-{name}')
    with zipfile.ZipFile(out/f'gzip-{level}-closure.zip','x',zipfile.ZIP_DEFLATED) as z:
        for prefix in ('filesystem-provider','producer-home','packed-sidecar'):
            for f in sorted((fixture/prefix).rglob('*')):
                if f.is_file() and not f.is_symlink():z.write(f,str(f.relative_to(fixture)))
a=json.loads((fixtures[1]/'fixture.json').read_text());b=json.loads((fixtures[6]/'fixture.json').read_text())
assert a['files']==b['files'] and a['workload']==b['workload'] and a['expectedOutput']==b['expectedOutput']
schedule=[(1,1),(1,6),(2,6),(2,1),(3,1),(3,6)]
append({'type':'schedule','arms':schedule,'same_logical_files':True,'cold_consumer_CAS':'absent pre-acquire','os_cache':'warm/uncontrolled; no drop_caches','profile_scope':'Go process profile; children CPU from wait4/rusage; warm is local materialization reuse plus fresh Python process, no persistent worker'})
for pair,level in schedule:
    label=f'pair-{pair}-gzip-{level}';worker=root/label;worker.mkdir(mode=0o700);consumer=worker/'home';consumer.mkdir(mode=0o700)
    q=prepared[level];fixture=fixtures[level]
    args=['-mode=worker','-root',str(fixture),'-python',python,'-arm','filesystem','-pair',str(pair),'-artifact-root',q['providerRoot'],'-pack-root',q['packRoot'],'-artifact',q['artifact'],'-index-sha',q['packIndexSha256'],'-pack-sha',q['packSha256'],'-receipt',str(worker/'worker.jsonl'),'-profile',str(worker/'cpu.pprof')]
    run(label,args,consumer,[root,home,fixture,fixture/'producer-home',worker,consumer])
    r=json.loads((worker/'worker.jsonl').read_text());assert r['status']=='PASS' and r['leaseReleased'] and r['warmLeaseReleased'] and r.get('warmProviderCalls',0)==0
    assert closure_hash(fixture)==hashes[level]
    append({'type':'arm','pair':pair,'gzip_level':level,'receipt':r,'source_unchanged':True})
    for name in ('worker.jsonl','cpu.pprof'):shutil.copyfile(worker/name,out/f'{label}-{name}')
    go='/workspace/rcc-successor-tools/mod/golang.org/toolchain@v0.0.1-go1.26.9.linux-amd64/bin/go'
    prof=subprocess.run([go,'tool','pprof','-top','-nodecount=15',str(binary),str(worker/'cpu.pprof')],capture_output=True,text=True,env=dict(os.environ,GOTOOLCHAIN='local',GOPATH='/workspace/rcc-successor-tools',GOMODCACHE='/workspace/rcc-successor-tools/mod',GOCACHE='/workspace/rcc-successor-tools/cache'))
    (out/f'{label}-cpu-top.txt').write_text(prof.stdout+prof.stderr);assert prof.returncode==0
assert hashlib.sha256(source.read_bytes()).hexdigest()==expected
append({'type':'complete','status':'PASS','fixtures_sha256':hashes,'final_root':guard(root),'final_evidence_root':guard(out),'global':guard(global_ht,0o750)})
print(json.dumps({'status':'PASS','arms':6,'logical_files':len(a['files']),'levels':[1,6]}))
