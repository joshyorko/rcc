#!/usr/bin/env python3
"""Verify every immutable remote evidence byte, plus parent Git preservation."""
import argparse,concurrent.futures,hashlib,json,subprocess,time,urllib.request
from pathlib import Path
p=argparse.ArgumentParser();p.add_argument('--sha',required=True);p.add_argument('--manifest',required=True);p.add_argument('--out',type=Path,required=True);a=p.parse_args()
repo=Path(__file__).resolve().parents[4]
def git(*args):return subprocess.check_output(['git','-C',str(repo),*args])
def tree(commit):
    result={}
    for row in git('ls-tree','-r','-z',commit).split(b'\0'):
        if row:
            meta,path=row.split(b'\t',1);result[path]=meta
    return result
started=time.monotonic();raw=git('show',a.sha+':'+a.manifest);m=json.loads(raw);old=tree(m['parent']);new=tree(a.sha)
assert all(new.get(path)==value for path,value in old.items())
entries=m['entries']+[{'path':a.manifest,'bytes':len(raw),'sha256':hashlib.sha256(raw).hexdigest()}]
def verify(x):
    body=git('show',a.sha+':'+x['path']);assert len(body)==x['bytes'] and hashlib.sha256(body).hexdigest()==x['sha256'],x['path']
    with urllib.request.urlopen(urllib.request.Request('https://raw.githubusercontent.com/joshyorko/rcc/'+a.sha+'/'+x['path'],headers={'Accept-Encoding':'identity'}),timeout=30) as r:
        assert r.status==200;remote=r.read(x['bytes']+1)
    assert body==remote,x['path'];return x
with concurrent.futures.ThreadPoolExecutor(max_workers=6) as e:rows=list(e.map(verify,entries))
result={'sha':a.sha,'parent':m['parent'],'exit':0,'all_parent_entries_preserved':len(old),'remote_raw_files':len(rows),'remote_raw_bytes':sum(x['bytes'] for x in rows),'seconds':time.monotonic()-started,'files':rows,'scope':'Read-only exact remote/local Git bytes including symlink Git payloads; no workload replay.'}
a.out.write_text(json.dumps(result,indent=2)+'\n');print(json.dumps({k:v for k,v in result.items() if k!='files'}))
