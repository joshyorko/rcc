from pathlib import Path
import json,hashlib,gzip,stat,statistics,subprocess,collections,time
b=Path('/workspace/work/rcc-swarm/evidence/continuation-20261011'); d=b/'go-gzip-component-executed'; o=b/'go-gzip-component-preexecution-root'
p=json.loads((d/'plan.json').read_text());l=json.loads((d/'launch.json').read_text());r=json.loads((d/'results.json').read_text());a=json.loads((o/'root-admission.json').read_text())
def sha(raw):return hashlib.sha256(raw).hexdigest()
assert l['status']==r['status']=='PASS' and l['failure'] is None
for k in ('build_exit_code','metadata_exit_code','runtime_exit_code'):assert type(l[k]) is int and l[k]==0
assert not l['retry_performed'] and l['single_invocation']
for name,h in a['inputSha256'].items():assert sha((d/name).read_bytes())==h==l['input_hashes_before'][name]==l['input_hashes_after'][name]
assert sha((d/'rcc-gzip-go').read_bytes())==l['executable_sha256'] and (d/'rcc-gzip-go').stat().st_size==l['executable_size_bytes']
meta=subprocess.run(['/workspace/work/rcc-swarm/tools/go1.26.9/bin/go','version','-m',str(d/'rcc-gzip-go')],capture_output=True,text=True);assert meta.returncode==0
for required in ('go1.26.9','CGO_ENABLED=0','GOARCH=amd64','GOOS=linux'):assert required in meta.stdout
for k in ('index','manifest'):
 path=Path(p['source'][k+'_path']);raw=path.read_bytes();s=path.stat();v=r[k+'_before'];assert v==r[k+'_after'];assert len(raw)==v['size']==p['source'][k+'_bytes'];assert sha(raw)==v['sha256']==p['source'][k+'_sha256'];assert stat.S_IMODE(s.st_mode)==v['mode'] and s.st_mtime_ns==v['mtime_unix_nano']
idx=json.loads(Path(p['source']['index_path']).read_text());selected=sorted([x for x in idx['entries'] if x['logicalSize']>1048576],key=lambda x:x['storedDigest']);assert selected==p['source']['objects'];assert len(selected)==20
actual={}
for e in selected:
 h=e['storedDigest'].removeprefix('sha256:');path=Path(p['source']['provider_root'])/'objects/sha256'/h[:2]/h[2:4]/h;raw=path.read_bytes();s=path.stat();logical=gzip.decompress(raw);assert len(raw)==e['storedSize'] and sha(raw)==h;assert len(logical)==e['logicalSize'] and sha(logical)==e['legacyObjectId'];v=r['source_before'][e['storedDigest']];assert v==r['source_after'][e['storedDigest']];assert (len(raw),h,stat.S_IMODE(s.st_mode),s.st_mtime_ns)==(v['size'],v['sha256'],v['mode'],v['mtime_unix_nano']);f=r['canonical_frames'][e['storedDigest']];assert f['sha256']==h and f['size']==len(raw);actual[e['storedDigest']]=e
phases=['legacy-gzip-decode','gzip-bestspeed-encode','gzip-bestspeed-decode'];expected=[]
for rep in range(5):
 for pi in range(3):
  phase=phases[(rep+pi)%3];rot=(rep+pi)%20
  expected.extend((rep,phase,e['storedDigest']) for e in selected[rot:]+selected[:rot])
assert len(r['samples'])==300 and [(x['repetition'],x['phase'],x['storedDigest']) for x in r['samples']]==expected
for x in r['samples']:
 e=actual[x['storedDigest']];assert x['verified'] is True and x['logical_bytes']==e['logicalSize'] and x['legacyObjectId']==e['legacyObjectId'];assert type(x['wall_ns']) is int and x['wall_ns']>0 and type(x['process_cpu_ns']) is int and x['process_cpu_ns']>=0
 if x['phase']=='legacy-gzip-decode':assert x['input_bytes']==e['storedSize'] and x['output_bytes']==e['logicalSize']
 else:assert x['frame_sha256']==e['storedDigest'].removeprefix('sha256:') and x['frame_bytes']==e['storedSize'];assert x['input_bytes']==(e['logicalSize'] if x['phase'].endswith('encode') else e['storedSize']);assert x['output_bytes']==(e['storedSize'] if x['phase'].endswith('encode') else e['logicalSize'])
events=[json.loads(x) for x in (d/'phase-events.jsonl').read_text().splitlines()];assert len(events)==30;assert len(l['phase_events'])==30
for n in range(0,30,2):
 s,f=events[n:n+2];assert s['state']=='phase_start' and f['state']=='phase_done' and (s['phase'],s['repetition'])==(f['phase'],f['repetition']);assert 0<=f['unix_nano']-s['unix_nano']<=30000000000
assert l['elapsed_seconds']<120 and l['build_sampled_process_tree_rss_peak_bytes']<=536870912 and l['runtime_sampled_process_tree_rss_peak_bytes']<=536870912
assert min(l[k] for k in ('build_minimum_free_bytes','metadata_minimum_free_bytes','runtime_minimum_free_bytes'))>=2688548864
assert l['build_owned_ram_temp_peak_allocated_bytes']<=268435456 and l['final_evidence_allocated_bytes']<=1048576 and l['executable_size_bytes']<=8388608
summary={}
for ph in phases:
 totals=[sum(x['wall_ns'] for x in r['samples'] if x['phase']==ph and x['repetition']==rep)/1e9 for rep in range(5)]
 cpu=[sum(x['process_cpu_ns'] for x in r['samples'] if x['phase']==ph and x['repetition']==rep)/1e9 for rep in range(5)]
 summary[ph]={'five20objectCodecWallSeconds':totals,'medianCodecWallSeconds':statistics.median(totals),'medianProcessCPUSeconds':statistics.median(cpu),'medianWeightedLogicalMiBPerSecond':statistics.median([102457064/1048576/t for t in totals])}
proof={'status':'ROOT_ACTUAL_20_OBJECTS_300_ROWS_30_EVENTS_BINARY_SOURCE_AND_RESOURCE_VERIFIED','rawResultsSha256':sha((d/'results.json').read_bytes()),'actualStoredAndLogicalObjects':20,'canonicalFramesExactlyEqualActualOriginalStoredFrames':20,'timedRowsVerified':300,'phaseEventsVerified':30,'metadataExitCode':meta.returncode,'summary':summary,'scope':'Independent actual original bytes verify all canonical frame digests; Go frames were not regenerated. Memory-backed stdlib analogue, not direct RCC LiftFile/whole-pipeline or Actions and no comparative winner. CPU includes GC/observer; RSS sampled, not absolute peak.','outerToolExit':'Worker completion separately preserved; root did not execute benchmark','sourceUnchanged':True}
(o/'root-verification.json').write_text(json.dumps(proof,indent=2)+'\n');(o/'actual-executable-go-metadata.txt').write_text(meta.stdout);print(json.dumps(proof))
