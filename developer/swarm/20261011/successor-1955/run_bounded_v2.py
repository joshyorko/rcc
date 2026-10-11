#!/usr/bin/env python3
"""Bound owned workloads across process groups and retain sampled identity evidence."""
import argparse,hashlib,json,os,signal,subprocess,time
from pathlib import Path
p=argparse.ArgumentParser();p.add_argument('--out',type=Path,required=True);p.add_argument('--seconds',type=int,required=True);p.add_argument('command',nargs=argparse.REMAINDER);a=p.parse_args()
a.out.mkdir(parents=True,exist_ok=False);assert a.command
def census():
    rows={}
    for path in Path('/proc').glob('[0-9]*/stat'):
        try:
            s=path.read_text();v=s[s.rfind(')')+2:].split();pid=int(path.parent.name)
            rows[pid]={'pid':pid,'ppid':int(v[1]),'pgid':int(v[2]),'start':v[19],'rss':int(v[21])*os.sysconf('SC_PAGE_SIZE'),'state':v[0]}
        except (OSError,ValueError,IndexError):continue
    return rows
started=time.monotonic();known={};samples=[];reason=None;peak=0
with (a.out/'stdout').open('xb') as out,(a.out/'stderr').open('xb') as err:
    child=subprocess.Popen(a.command,stdout=out,stderr=err,start_new_session=True)
    while child.poll() is None:
        rows=census()
        if child.pid not in rows:
            if child.poll() is not None:break
            reason='owned-root-stat-unavailable';break
        active={child.pid}
        for pid,start in known.items():
            if pid in rows and rows[pid]['start']==start:active.add(pid)
        changed=True
        while changed:
            descendants={pid for pid,r in rows.items() if r['ppid'] in active}
            changed=bool(descendants-active);active.update(descendants)
        for pid in active:known[pid]=rows[pid]['start']
        total=sum(rows[pid]['rss'] for pid in active);peak=max(peak,total)
        free=os.statvfs('/workspace').f_bavail*os.statvfs('/workspace').f_frsize
        elapsed=time.monotonic()-started
        samples.append({'elapsed':elapsed,'rss':total,'free':free,'identities':[rows[pid] for pid in sorted(active)]})
        if elapsed>a.seconds:reason='deadline'
        elif total>2*1024**3:reason='sampled-tree-RSS-cap'
        elif free<2688548864:reason='filesystem-free-floor'
        if reason:break
        time.sleep(.1)
    if reason:
        if child.poll() is None:
            child.terminate()
        for pid,start in known.items():
            row=census().get(pid)
            if row and row['start']==start:
                try:os.kill(pid,signal.SIGTERM)
                except ProcessLookupError:pass
        try:child.wait(timeout=3)
        except subprocess.TimeoutExpired:
            child.kill()
            for pid,start in known.items():
                row=census().get(pid)
                if row and row['start']==start:
                    try:os.kill(pid,signal.SIGKILL)
                    except ProcessLookupError:pass
    child.wait()
    final=census();alive=[r for pid,r in final.items() if known.get(pid)==r['start'] and r['state']!='Z']
    if alive:
        reason=reason or 'observed-owned-descendants-remain'
        for r in alive:
            try:os.kill(r['pid'],signal.SIGTERM)
            except ProcessLookupError:pass
result={'command':a.command,'returncode':child.returncode,'stop':reason,'seconds':time.monotonic()-started,'sampled_tree_rss_max':peak,'known_identities':known,'final_observed_live_identities':alive,'scope':'Sampled descendant/process identity monitoring across process groups, misses sub100ms/unobserved escaped children; no hard memory quota or absolute peak claim. Stdout/stderr are bounded by owned workload output plan and closed after wait.','launcher_sha256':hashlib.sha256(Path(__file__).read_bytes()).hexdigest(),'samples':samples}
(a.out/'result.json').write_text(json.dumps(result,indent=2)+'\n');print(json.dumps({k:v for k,v in result.items() if k not in ('samples','known_identities')}));raise SystemExit(0 if child.returncode==0 and reason is None else 1)
