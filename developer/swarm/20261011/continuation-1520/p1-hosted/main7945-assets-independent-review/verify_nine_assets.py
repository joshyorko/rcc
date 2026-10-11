#!/usr/bin/env python3
"""Independent, read-only review verifier for preserved #7945 review asset ZIPs."""
from pathlib import Path, PurePosixPath
import hashlib,json,os,subprocess,stat,zipfile
ROOT=Path('/dev/shm/rcc-main7945-release-review-root')
NATIVE=Path('/dev/shm/rcc-main7945-native-review-root')
EVIDENCE=Path('/workspace/work/rcc-swarm/evidence/continuation-20261011/p1-hosted/root-main7945-assets')
API=Path('/workspace/work/rcc-swarm/evidence/continuation-20261011/p1-hosted/main7945-preparation/root-final-artifacts.json')
GO=Path('/workspace/work/rcc-swarm/tools/go1.26.9/bin/go')
GO_SHA='e90c4e12df236ea0c2e7536fd68da8a47fc0b585c19588bf6c1a70cfc296a0f9'
SOURCE='7945f956b428359928908a9f42e36417b113dafd'; VERSION='v18.19.6'; RUN=38147141244
ASSETS=('rcc-linux64','rccremote-linux64','rcc-windows64.exe','rccremote-windows64.exe','rcc-macos64','rccremote-macos64','rcc-macosarm64','rccremote-macosarm64','index.json')
TARGETS={'rcc-linux64':('linux','amd64'),'rccremote-linux64':('linux','amd64'),'rcc-windows64.exe':('windows','amd64'),'rccremote-windows64.exe':('windows','amd64'),'rcc-macos64':('darwin','amd64'),'rccremote-macos64':('darwin','amd64'),'rcc-macosarm64':('darwin','arm64'),'rccremote-macosarm64':('darwin','arm64')}
NATIVE={'rcc-linux64':'linux_amd64','rcc-macos64':'darwin_amd64','rcc-macosarm64':'darwin_arm64','rcc-windows64.exe':'windows_amd64'}
def sha_bytes(b):return hashlib.sha256(b).hexdigest()
def sha_file(p):
 h=hashlib.sha256()
 with Path(p).open('rb') as f:
  for b in iter(lambda:f.read(1024*1024),b''):h.update(b)
 return h.hexdigest()
def build_info(binary):
 out=subprocess.run([str(GO),'version','-m',str(binary)],capture_output=True,text=True,env={**os.environ,'GOTOOLCHAIN':'local'},check=False)
 settings={}; lines=out.stdout.splitlines(); compiler=lines[0].split(': ',1)[-1] if lines else None
 for line in lines[1:]:
  f=line.strip().split(None,1)
  if len(f)==2 and f[0]=='build' and '=' in f[1]:settings[f[1].split('=',1)[0]]=f[1].split('=',1)[1]
 return out,compiler,settings
def main():
 api=json.loads(API.read_text())['artifacts']
 expected_api={f'{VERSION}-{SOURCE}-review-{a}':a for a in ASSETS}
 api_items=[x for x in api if x.get('name') in expected_api]
 api_names={x['name'] for x in api_items}
 result={'source':SOURCE,'version':VERSION,'workflowRunId':RUN,'manifestSha256Expected':'26a47203ec1375e2c60d362c5aa55ba0aa65e334c1b24bc07b16f973a585f62c','officialGoTool':{'path':str(GO),'version':subprocess.check_output([str(GO),'version'],text=True).strip(),'sha256':sha_file(GO),'expectedSha256':GO_SHA},'artifactInventory':{'expectedCount':9,'matchingCount':len(api_items),'exactArtifactNames':api_names==set(expected_api),'sameRunAndHead':all(x.get('workflow_run',{}).get('id')==RUN and x.get('workflow_run',{}).get('head_sha')==SOURCE and x.get('expired') is False for x in api_items)},'assets':{},'allPass':False}
 go_log=[]
 common_manifest=None; all_ok=(result['officialGoTool']['sha256']==GO_SHA and result['officialGoTool']['version']=='go version go1.26.9 linux/amd64' and result['artifactInventory']['matchingCount']==9 and result['artifactInventory']['exactArtifactNames'] and result['artifactInventory']['sameRunAndHead'])
 for asset in ASSETS:
  artname=f'{VERSION}-{SOURCE}-review-{asset}'; art=next((x for x in api_items if x.get('name')==artname),None)
  zpath=ROOT/f'{asset}.zip'; dest=ROOT/'assets-review'/asset
  rawsha=sha_file(zpath); rawbytes=zpath.stat().st_size
  expected_paths={'assets/'+asset,'evidence/asset-manifest.json','evidence/SHA256SUMS','evidence/provenance.json','review-identity.json'}
  members=[]; safe=True; exact=False; extractedmatch=False
  with zipfile.ZipFile(zpath) as z:
   infos=z.infolist(); paths=[m.filename for m in infos]
   exact=len(infos)==5 and set(paths)==expected_paths and len(set(paths))==5 and sum(x.file_size for x in infos)<=24*1024*1024
   for m in infos:
    p=PurePosixPath(m.filename); mode=m.external_attr>>16
    if (not m.filename or p.is_absolute() or any(s in ('','.','..') for s in p.parts) or '\\\\' in m.filename or stat.S_ISLNK(mode) or stat.S_IFMT(mode) not in (0,stat.S_IFREG) or m.is_dir()):safe=False
    h=hashlib.sha256(); n=0
    with z.open(m) as f:
     for b in iter(lambda:f.read(1024*1024),b''):h.update(b);n+=len(b)
    target=dest/m.filename; same=target.is_file() and target.stat().st_size==n and sha_file(target)==h.hexdigest()
    members.append({'path':m.filename,'size':n,'sha256':h.hexdigest(),'extractedBytesMatch':same})
  extracted_paths={p.relative_to(dest).as_posix() for p in dest.rglob('*') if p.is_file()}
  extractedmatch=(extracted_paths==expected_paths and all(m['extractedBytesMatch'] for m in members))
  asset_bytes=(dest/'assets'/asset).read_bytes(); assetsha=sha_bytes(asset_bytes)
  mp=dest/'evidence/asset-manifest.json'; manifest_bytes=mp.read_bytes(); msha=sha_bytes(manifest_bytes); manifest=json.loads(manifest_bytes)
  sums_path=dest/'evidence/SHA256SUMS'; sums_text=sums_path.read_text(); sums={}
  for line in sums_text.splitlines():
   f=line.split(maxsplit=1)
   if len(f)==2 and f[1] not in sums:sums[f[1]]=f[0]
   else:sums['__malformed__']=f[0] if f else ''
  records=manifest.get('assets',{})
  manifest_ok=(manifest.get('schemaVersion')==1 and manifest.get('sourceSha')==SOURCE and manifest.get('version')==VERSION and type(manifest.get('preparationRunId')) is int and manifest.get('preparationRunId')==RUN and set(records)==set(ASSETS) and records.get(asset)=={'sha256':assetsha,'size':len(asset_bytes)} and msha=='26a47203ec1375e2c60d362c5aa55ba0aa65e334c1b24bc07b16f973a585f62c')
  if common_manifest is None:common_manifest=manifest_bytes
  shared_manifest=(manifest_bytes==common_manifest)
  sums_ok=(set(sums)=={f'assets/{n}' for n in ASSETS} and sums=={f'assets/{n}':records[n]['sha256'] for n in ASSETS})
  provenance=json.loads((dest/'evidence/provenance.json').read_text()); ident=json.loads((dest/'review-identity.json').read_text())
  build_record=provenance.get('binaryBuildInfo',{}).get(asset) if asset!='index.json' else None
  if asset!='index.json':
   target_for_asset=TARGETS[asset]
   provenance_build_ok=(isinstance(build_record,dict) and build_record.get('goVersion')=='go1.26.9' and build_record.get('goos')==target_for_asset[0] and build_record.get('goarch')==target_for_asset[1] and build_record.get('vcsRevision')==SOURCE and build_record.get('vcsModified') is False)
  else:
   provenance_build_ok=(set(provenance.get('binaryBuildInfo',{}))==set(TARGETS) and all(isinstance(v,dict) and v.get('goVersion')=='go1.26.9' and v.get('goos')==TARGETS[k][0] and v.get('goarch')==TARGETS[k][1] and v.get('vcsRevision')==SOURCE and v.get('vcsModified') is False for k,v in provenance.get('binaryBuildInfo',{}).items()))
  provenance_ok=(provenance.get('sourceSha')==SOURCE and provenance.get('version')==VERSION and provenance.get('preparationRunId')==RUN and provenance.get('assetManifestSha256')==msha and type(provenance.get('releaseCandidateExitCode')) is int and provenance.get('releaseCandidateExitCode')==0 and set(provenance.get('nativePlatforms',[]))=={'linux_amd64','darwin_amd64','darwin_arm64','windows_amd64'})
  expected_identity={'schemaVersion':1,'sourceSha':SOURCE,'version':VERSION,'preparationRunId':RUN,'assetManifestSha256':msha,'assetName':asset,'assetSha256':assetsha,'assetSize':len(asset_bytes)}
  identity_ok=ident==expected_identity
  api_ok=bool(art and type(art.get('id')) is int and art.get('size_in_bytes')==rawbytes and art.get('digest')=='sha256:'+rawsha and art.get('workflow_run',{}).get('id')==RUN and art.get('workflow_run',{}).get('head_sha')==SOURCE and not art.get('expired',True))
  go_result=None; go_ok=(asset=='index.json')
  if asset!='index.json':
   binary_path=dest/'assets'/asset
   go_result,compiler,settings=build_info(binary_path); target=TARGETS[asset]
   go_log.extend([f'$ {GO} version -m {binary_path}',go_result.stdout.rstrip(),f'exit={go_result.returncode}'])
   go_ok=(go_result.returncode==0 and compiler=='go1.26.9' and settings.get('GOOS')==target[0] and settings.get('GOARCH')==target[1] and settings.get('vcs.revision')==SOURCE and settings.get('vcs.modified')=='false' and settings.get('CGO_ENABLED')=='0')
  native_match=None
  if asset in NATIVE:
   platform=NATIVE[asset]; native_path=NATIVE_ROOT=Path('/dev/shm/rcc-main7945-native-review-root')/platform/'native-binary-review'/'assets'/asset
   native_match=sha_file(native_path)==assetsha and native_path.stat().st_size==len(asset_bytes)
  checks={'apiArtifactIdentityAndZipDigest':api_ok,'zipFiveExactMembers':exact,'safeRegularMembers':safe,'allExtractedMemberBytesMatch':extractedmatch,'manifestIdentityAndAssetBytes':manifest_ok,'sameCanonicalManifestAcrossAllNine':shared_manifest,'checksumSetAndBytes':sums_ok,'provenanceBinding':provenance_ok,'provenanceBuildInfoForAsset':provenance_build_ok,'reviewIdentityBinding':identity_ok,'officialGo1269Metadata':go_ok,'matchesNativeExecutedBinary':native_match}
  passed=all(v for v in checks.values() if v is not None)
  all_ok &= passed
  result['assets'][asset]={'artifactId':art.get('id') if art else None,'artifactName':artname,'zipBytes':rawbytes,'zipSha256':'sha256:'+rawsha,'assetBytes':len(asset_bytes),'assetSha256':assetsha,'memberCount':len(members),'members':members,'manifestSha256':msha,'goMetadata':({'exitCode':go_result.returncode,'embeddedCompiler':compiler,'GOOS':settings.get('GOOS'),'GOARCH':settings.get('GOARCH'),'vcs.revision':settings.get('vcs.revision'),'vcs.modified':settings.get('vcs.modified'),'CGO_ENABLED':settings.get('CGO_ENABLED')} if asset!='index.json' else None),'nativeExecutedBinaryMatch':native_match,'checks':checks,'pass':passed}
 index=json.loads((ROOT/'assets-review'/'index.json'/'assets'/'index.json').read_text())
 entries=[x for x in index.get('tested',[]) if isinstance(x,dict) and x.get('version')==VERSION]
 entry=entries[0] if len(entries)==1 else {}
 urlbase=f'https://github.com/joshyorko/rcc/releases/download/{VERSION}'
 index_expected={'windows':f'{urlbase}/rcc-windows64.exe','linux':f'{urlbase}/rcc-linux64','macos':f'{urlbase}/rcc-macos64','macosarm64':f'{urlbase}/rcc-macosarm64','changelog':f'https://github.com/joshyorko/rcc/blob/main/docs/changelog.md#v18196'}
 result['index']={'matchingVersionEntryCount':len(entries),'testedEntry':entry,'requiredMappings':index_expected,'mappingsPass':len(entries)==1 and all(entry.get(k)==v for k,v in index_expected.items())}
 all_ok &= result['index']['mappingsPass'] and all(x['pass'] for x in result['assets'].values())
 result['allPass']=bool(all_ok)
 (Path(__file__).resolve().parent/'official-go9-metadata.log').write_text('\n'.join(go_log)+'\n')
 return result
if __name__=='__main__':print(json.dumps(main(),indent=2,sort_keys=True))
