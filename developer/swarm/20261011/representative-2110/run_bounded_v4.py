#!/usr/bin/env python3
"""Bound owned workloads across process groups and retain sampled identity evidence."""
import argparse,hashlib,json,os,signal,subprocess,time
from pathlib import Path
p=argparse.ArgumentParser();p.add_argument('--out',type=Path,required=True);p.add_argument('--seconds',type=int,required=True);p.add_argument('--scratch-root',type=Path);p.add_argument('command',nargs=argparse.REMAINDER);a=p.parse_args()
a.out.mkdir(parents=True,exist_ok=False);assert a.command
def census():
    rows={}
    for path in Path('/proc').glob('[0-9]*/stat'):
        try:
            s=path.read_text();v=s[s.rfind(')')+2:].split();pid=int(path.parent.name)
            rows[pid]={'pid':pid,'ppid':int(v[1]),'pgid':int(v[2]),'sid':int(v[3]),'start':v[19],'rss':int(v[21])*os.sysconf('SC_PAGE_SIZE'),'state':v[0]}
        except (OSError,ValueError,IndexError):continue
    return rows
def scratch_logical_bytes(root):
    total=0;vanished=0
    for x in root.rglob('*'):
        try:
            if x.is_file() and not x.is_symlink():total+=x.lstat().st_size
        except FileNotFoundError:
            # Legitimate concurrent task-temp cleanup: no longer allocated.
            # Other I/O errors fail the sampled run through the outer handler.
            vanished+=1
    return total,vanished
def signal_owned_identity(pid,start,sig):
    try:fd=os.pidfd_open(pid)
    except ProcessLookupError:return
    try:
        row=census().get(pid)
        if row and row['start']==start and row['state']!='Z':
            try:signal.pidfd_send_signal(fd,sig)
            except ProcessLookupError:pass
    finally:os.close(fd)
started=time.monotonic();known={};samples=[];reason=None;peak=0
with (a.out/'stdout').open('xb') as out,(a.out/'stderr').open('xb') as err:
    child=subprocess.Popen(a.command,stdout=out,stderr=err,start_new_session=True)
    try:
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
            elif out.tell()+err.tell()>8*1024**2:reason='observed-output-cap'
            if a.scratch_root and a.scratch_root.exists():
                scratch,vanished=scratch_logical_bytes(a.scratch_root)
                samples[-1]['scratch_logical_bytes']=scratch
                samples[-1]['scratch_vanished_entries']=vanished
                if scratch>128*1024**2:reason='sampled-scratch-logical-cap'
            if reason:break
            time.sleep(.1)
    except Exception as exc:
        reason="sampler-error:"+type(exc).__name__+":"+str(exc)
    except KeyboardInterrupt:
        reason="sampler-interrupt"
    if reason:
        if child.poll() is None:
            child.terminate()
        for pid,start in known.items():
            row=census().get(pid)
            if row and row['start']==start:
                signal_owned_identity(pid,start,signal.SIGTERM)
        try:child.wait(timeout=3)
        except subprocess.TimeoutExpired:
            child.kill()
            for pid,start in known.items():
                row=census().get(pid)
                if row and row['start']==start:
                    signal_owned_identity(pid,start,signal.SIGKILL)
    child.wait()
    final=census()
    # All cooperative descendants retain the launcher's private session even
    # when RCC gives its worker a separate process group. Census it after wait
    # so a between-poll child is not silently omitted from the final check.
    for pid,row in final.items():
        if row['sid']==child.pid:known[pid]=row['start']
    alive=[r for pid,r in final.items() if known.get(pid)==r['start'] and r['state']!='Z']
    if alive:
        reason=reason or 'observed-owned-descendants-remain'
        for r in alive:
            signal_owned_identity(r['pid'],r['start'],signal.SIGTERM)
        settle_deadline=time.monotonic()+2
        while alive and time.monotonic()<settle_deadline:
            time.sleep(.05)
            final=census();alive=[r for pid,r in final.items() if known.get(pid)==r['start'] and r['state']!='Z']
        for r in alive:
            signal_owned_identity(r['pid'],r['start'],signal.SIGKILL)
        final=census();alive=[r for pid,r in final.items() if known.get(pid)==r['start'] and r['state']!='Z']
    final_output_bytes=(a.out/'stdout').stat().st_size+(a.out/'stderr').stat().st_size
    final_scratch=None;final_scratch_vanished=0
    if final_output_bytes>8*1024**2:reason=reason or 'final-observed-output-cap'
    try:
        if a.scratch_root and a.scratch_root.exists():
            final_scratch,final_scratch_vanished=scratch_logical_bytes(a.scratch_root)
            if final_scratch>128*1024**2:reason=reason or 'final-sampled-scratch-logical-cap'
    except Exception as exc:reason=reason or 'final-sampler-error:'+type(exc).__name__+':'+str(exc)
result={'final_output_bytes':final_output_bytes,'final_scratch_logical_bytes':final_scratch,'final_scratch_vanished_entries':final_scratch_vanished,'command':a.command,'returncode':child.returncode,'stop':reason,'seconds':time.monotonic()-started,'sampled_tree_rss_max':peak,'known_identities':known,'final_observed_live_identities':alive,'scope':'Sampled descendant/process identity monitoring across process groups plus final private-session census; misses unobserved descendants that create another session; no hard memory quota or absolute peak claim. Output and optional owned scratch limits are sampled; no absolute disk quota. Stops at 8MiB output / 128MiB scratch logical bytes / 2GiB observed RSS / 2.5GiB free floor. Output closed after wait.','launcher_sha256':hashlib.sha256(Path(__file__).read_bytes()).hexdigest(),'samples':samples}
(a.out/'result.json').write_text(json.dumps(result,indent=2)+'\n');print(json.dumps({k:v for k,v in result.items() if k not in ('samples','known_identities')}));raise SystemExit(0 if child.returncode==0 and reason is None else 1)
