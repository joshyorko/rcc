#!/usr/bin/env python3
"""Run only caller-owned subprocess groups, preserving exits and sampled resources."""
import argparse, json, os, resource, signal, subprocess, time
from pathlib import Path

p=argparse.ArgumentParser();p.add_argument('--out',type=Path,required=True);p.add_argument('--seconds',type=int,required=True);p.add_argument('command',nargs=argparse.REMAINDER);a=p.parse_args()
a.out.mkdir(parents=True,exist_ok=True)
assert a.command and not (a.out/'result.json').exists()
start=time.monotonic(); peak=0; reason=None; rows=[]
with (a.out/'stdout').open('xb') as out,(a.out/'stderr').open('xb') as err:
    child=subprocess.Popen(a.command, stdout=out, stderr=err, start_new_session=True)
    while child.poll() is None:
        total=0; count=0
        for path in Path('/proc').glob('[0-9]*/stat'):
            try:
                data=path.read_text(); fields=data[data.rfind(')')+2:].split()
                if int(fields[2])==child.pid:
                    total+=int(fields[21])*os.sysconf('SC_PAGE_SIZE');count+=1
            except (OSError,ValueError,IndexError): pass
        peak=max(peak,total)
        free=os.statvfs('/workspace').f_bavail*os.statvfs('/workspace').f_frsize
        elapsed=time.monotonic()-start
        if elapsed>a.seconds:reason='deadline'
        elif total>2*1024**3:reason='sampled-group-RSS-cap'
        elif free<2688548864:reason='filesystem-free-floor'
        rows.append({'elapsed':elapsed,'group_rss':total,'processes':count,'free':free})
        if reason:
            os.killpg(child.pid,signal.SIGTERM)
            try:child.wait(timeout=3)
            except subprocess.TimeoutExpired:os.killpg(child.pid,signal.SIGKILL)
            break
        time.sleep(.2)
    child.wait()
result={'command':a.command,'returncode':child.returncode,'resource_stop':reason,'seconds':time.monotonic()-start,'sampled_group_rss_max':peak,'rss_scope':'sum of live process-group RSS; shared pages may double count and sampling misses peaks; not escaped-descendant quota proof','samples':rows}
(a.out/'result.json').write_text(json.dumps(result,indent=2)+'\n');print(json.dumps({k:v for k,v in result.items() if k!='samples'}))
raise SystemExit(0 if child.returncode==0 and reason is None else 1)
