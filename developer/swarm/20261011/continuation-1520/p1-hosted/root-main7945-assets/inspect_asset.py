import argparse,json,hashlib,zipfile,stat,sys,shutil,subprocess
from pathlib import Path,PurePosixPath
p=argparse.ArgumentParser();p.add_argument('asset');p.add_argument('zip');a=p.parse_args();out=Path(__file__).resolve().parent;ram=Path('/dev/shm/rcc-main7945-release-review-root');source='7945f956b428359928908a9f42e36417b113dafd';version='v18.19.6';run=38147141244;go='/workspace/work/rcc-swarm/tools/go1.26.9/bin/go';sys.path.insert(0,'/workspace/work/rcc-swarm/two-phase-release/scripts');import release_publication as gate
assert a.asset in gate.ASSET_NAMES
artifacts=json.loads((out.parent/'main7945-preparation/root-final-artifacts.json').read_text())['artifacts'];art=next(x for x in artifacts if x['name']==f'{version}-{source}-review-{a.asset}');assert art['workflow_run']['head_sha']==source and not art['expired']
s=Path(a.zip);sha=lambda x:hashlib.sha256(x).hexdigest();assert s.stat().st_size==art['size_in_bytes'] and 'sha256:'+sha(s.read_bytes())==art['digest'];d=ram/(a.asset+'.zip')
if s!=d:
 assert not d.exists();shutil.move(s,d)
dest=ram/'assets-review'/a.asset;assert not dest.exists();dest.mkdir(parents=True)
expected={'assets/'+a.asset,'evidence/asset-manifest.json','evidence/SHA256SUMS','evidence/provenance.json','review-identity.json'}
with zipfile.ZipFile(d) as z:
 infos=z.infolist();assert len(infos)==5 and {x.filename for x in infos}==expected;assert sum(x.file_size for x in infos)<=24*1024*1024
 for x in infos:
  q=PurePosixPath(x.filename);assert not q.is_absolute() and '..' not in q.parts and '\\' not in x.filename;assert stat.S_IFMT(x.external_attr>>16) in (0,stat.S_IFREG);v=dest.joinpath(*q.parts);v.parent.mkdir(parents=True,exist_ok=True);v.write_bytes(z.read(x))
manifest=json.loads((dest/'evidence/asset-manifest.json').read_text());identity=json.loads((dest/'review-identity.json').read_text());provenance=json.loads((dest/'evidence/provenance.json').read_text());mh=sha((dest/'evidence/asset-manifest.json').read_bytes());raw=(dest/'assets'/a.asset).read_bytes();bh=sha(raw)
assert manifest['sourceSha']==source and manifest['version']==version and type(manifest['preparationRunId']) is int and manifest['preparationRunId']==run and set(manifest['assets'])==set(gate.ASSET_NAMES)
assert identity['sourceSha']==source and identity['version']==version and identity['preparationRunId']==run and identity['assetManifestSha256']==mh and identity['assetName']==a.asset and identity['assetSha256']==bh and identity['assetSize']==len(raw)
assert manifest['assets'][a.asset]=={'sha256':bh,'size':len(raw)} and provenance['sourceSha']==source and provenance['preparationRunId']==run and provenance['assetManifestSha256']==mh and type(provenance['releaseCandidateExitCode']) is int and provenance['releaseCandidateExitCode']==0
metadata=None
if a.asset!='index.json':
 metadata=gate.read_go_build_info(dest/'assets'/a.asset,go);target=gate.ASSET_TARGETS[a.asset];assert metadata['GOOS']==target[0] and metadata['GOARCH']==target[1] and metadata['vcs.revision']==source and metadata['vcs.modified']=='false' and metadata['CGO_ENABLED']=='0'
 if a.asset in gate.NATIVE_RECEIPTS.values():
  platform=next(k for k,v in gate.NATIVE_RECEIPTS.items() if v==a.asset);r=json.loads((out.parent/'root-main7945-native-bytes'/(platform+'-byte-verification.json')).read_text());assert r['binarySha256']==bh and r['binaryBytes']==len(raw)
 (out/(a.asset+'-actual-go-metadata.json')).write_text(json.dumps(metadata,indent=2)+'\n')
for name in ['review-identity.json','evidence/asset-manifest.json','evidence/SHA256SUMS','evidence/provenance.json']:(out/(a.asset+'-'+Path(name).name)).write_bytes((dest/name).read_bytes())
if a.asset=='index.json':(out/'actual-index.json').write_bytes(raw)
r={'status':'ACTUAL_DISTRIBUTION_ASSET_BYTES_ZIP_SOURCE_METADATA_MANIFEST_VERIFIED','asset':a.asset,'source':source,'version':version,'run':run,'artifactId':art['id'],'zipBytes':art['size_in_bytes'],'zipSha256':art['digest'],'binaryBytes':len(raw),'assetSha256':bh,'assetManifestSha256':mh,'retainedActualZip':str(d),'retainedActualBytes':str(dest/'assets'/a.asset),'goBuildInfo':metadata,'nativeExecutedBinaryMatched':a.asset in gate.NATIVE_RECEIPTS.values(),'memberCount':len(infos)};(out/(a.asset+'-byte-verification.json')).write_text(json.dumps(r,indent=2)+'\n');print(json.dumps({k:v for k,v in r.items() if k!='goBuildInfo'}))
