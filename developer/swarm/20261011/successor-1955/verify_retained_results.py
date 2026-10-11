#!/usr/bin/env python3
"""Independent coordinator readback, without rebuilding or replaying workloads."""
import hashlib,json,stat,subprocess
from pathlib import Path
p=Path(__file__).parent
def sha(f):return hashlib.sha256(f.read_bytes()).hexdigest()
counts={'materializations':0,'logical_files':0,'mismatches':0,'active_leases':0}
def check(home,fixture,r):
    record_root=home/'artifacts/v1/materializations'/r['artifact'].split(':')[1]
    ready=json.loads((record_root/'ready.json').read_bytes());assert ready['materializationId']==r['materialization']
    materialized=Path(ready['path'])
    for x in fixture['files']:
        f=materialized/x['path'];assert f.stat().st_size==x['size'] and sha(f)==x['sha256'],str(f);counts['logical_files']+=1
    leases=list((record_root/'leases').glob('*.json'));assert not leases;counts['materializations']+=1
    s=home.lstat();assert stat.S_ISDIR(s.st_mode) and stat.S_IMODE(s.st_mode)==0o700 and s.st_uid==1000 and s.st_gid==1000
    assert r['status']=='PASS' and r['leaseReleased']
for binding in (p/'pack-e2e/build-binding-4.json',p/'compression-e2e/results/build-binding.json'):
    b=json.loads(binding.read_bytes())
    for x in b['files']:
        f=Path(x['path']);assert f.stat().st_size==x['bytes'] and sha(f)==x['sha256'],str(f)
pack=p/'pack-e2e/results';fixture=json.loads((pack/'fixture.json').read_bytes());rows=[json.loads(x) for x in (pack/'measurements.jsonl').read_text().splitlines()];arms=[x for x in rows if x['type']=='arm'];assert len(arms)==6 and rows[-1]['status']=='PASS'
for r in arms:check(Path('/workspace/rcc-successor-experiments-2015/pack/workers')/f"pair-{r['pair']}-{r['arm']}"/'home',fixture,r)
neg=[json.loads(x) for x in (pack/'negative-controls.jsonl').read_text().splitlines()];assert len(neg)==13 and all(x['status']=='PASS' for x in neg) and neg[-1]['count']==12
comp=p/'compression-e2e/results';rows=[json.loads(x) for x in (comp/'controller.jsonl').read_text().splitlines()];arms=[x for x in rows if x.get('type')=='arm'];assert len(arms)==6 and rows[-1]['status']=='PASS'
fixtures={i:json.loads((comp/f'gzip-{i}-fixture.json').read_bytes()) for i in (1,6)};prepared={i:json.loads((comp/f'gzip-{i}-prepared.json').read_bytes()) for i in (1,6)}
assert fixtures[1]['files']==fixtures[6]['files'] and fixtures[1]['workload']==fixtures[6]['workload']
for i in (1,6):assert fixtures[i]['gzipLevel']==i and prepared[i]['gzipLevel']==i
assert prepared[1]['packBytes']==46058 and prepared[6]['packBytes']==38746 and prepared[1]['packSha256']!=prepared[6]['packSha256']
for x in arms:
    r=x['receipt'];check(Path('/workspace/rcc-successor-compression-2018')/f"pair-{x['pair']}-gzip-{x['gzip_level']}"/'home',fixtures[x['gzip_level']],r)
    assert r['warmLeaseReleased'] and r['warmOutput']==r['output'] and r.get('warmProviderCalls',0)==0 and r['warmPipelineNs']==r['warmNs']+r['warmExecuteNs']
for f in comp.glob('pair-*/result.json'):
    r=json.loads(f.read_bytes());assert r['returncode']==0 and r['stop'] is None and not r['final_observed_live_identities']
assert json.loads((p/'import-baseline-run/result.json').read_bytes())['returncode']==1
assert stat.S_IMODE(Path('/workspace/rcc-successor-lifecycle/baseline/rcc-home').stat().st_mode)==0o750
assert subprocess.run(['git','-C','/workspace/rcc-frozen','diff','--quiet']).returncode==0
result={'exit':0,'counts':counts,'pack_arms':6,'pack_controls':12,'compression_arms':6,'original_baseline_failure_preserved':True,'frozen_tracked_source_unchanged':True,'scope':'Readback only: exact bindings, all actual materialized files, ready records, lease directories, mode/owners, raw native outcomes; no benchmark replay.'}
(p/'coordinator-verification.json').write_text(json.dumps(result,indent=2)+'\n');print(json.dumps(result))
