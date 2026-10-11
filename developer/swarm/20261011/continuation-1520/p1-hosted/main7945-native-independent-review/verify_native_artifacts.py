#!/usr/bin/env python3
"""Independent read-only verifier for preserved 7945 native workflow artifacts."""
from pathlib import Path, PurePosixPath
from zipfile import ZipFile
from xml.etree import ElementTree as ET
import hashlib, json, stat, subprocess

BASE=Path('/dev/shm/rcc-main7945-native-review-root')
PROOF=Path('/workspace/work/rcc-swarm/evidence/continuation-20261011/p1-hosted/root-main7945-native-bytes')
REPO=Path('/workspace/work/rcc-swarm/final-main-727')
GO=Path('/home/agent/.robocorp/holotree/c2fd1aa_5a1fac3_9fcd2534/go/bin/go')
BASE_SHA='727c1ff8b679cdbbe12836e53c87734a1edb0071'
HEAD_SHA='7945f956b428359928908a9f42e36417b113dafd'
VERSION='v18.19.6'
PLATFORMS={
 'linux_amd64':('main7945-linux-native.zip','rcc-linux64',('linux','amd64')),
 'darwin_amd64':('main7945-intel-native.zip','rcc-macos64',('darwin','amd64')),
 'darwin_arm64':('main7945-arm-native.zip','rcc-macosarm64',('darwin','arm64')),
 'windows_amd64':('main7945-windows-native.zip','rcc-windows64.exe',('windows','amd64')),
}
def sha(path):
 h=hashlib.sha256()
 with Path(path).open('rb') as f:
  for b in iter(lambda:f.read(1024*1024),b''): h.update(b)
 return h.hexdigest()
def go_tree(rev):
 raw=subprocess.check_output(['git','ls-tree','-r','-l',rev],cwd=REPO,text=True)
 d={}
 for line in raw.splitlines():
  meta,path=line.split('\t',1); mode,typ,oid,*size=meta.split()
  d[path]=(oid,int(size[0]) if size and size[0].isdigit() else None)
 return d
def project_inputs():
 a,b=go_tree(BASE_SHA),go_tree(HEAD_SHA)
 groups={
  'goSources':lambda p:p.endswith('.go'),
  'goModuleManifests':lambda p:p.rsplit('/',1)[-1] in ('go.mod','go.sum'),
  'embeddedBlobInputs':lambda p:p.startswith('blobs/assets/') or p.startswith('blobs/docs/'),
 }
 result={}
 for name,pred in groups.items():
  aa={p:x for p,x in a.items() if pred(p)}; bb={p:x for p,x in b.items() if pred(p)}
  changed=sorted(p for p in set(aa)|set(bb) if aa.get(p)!=bb.get(p))
  digest=lambda d:hashlib.sha256(''.join(f'{p}\\0{d[p][0]}\\n' for p in sorted(d)).encode()).hexdigest()
  result[name]={'baseCount':len(aa),'headCount':len(bb),'baseManifestSha256':digest(aa),'headManifestSha256':digest(bb),'changedPaths':changed,'equal':not changed and digest(aa)==digest(bb)}
 return result

def main():
 result={'source':HEAD_SHA,'version':VERSION,'inspectionTool':subprocess.check_output([str(GO),'version'],text=True).strip(),'platforms':{},'shippingInputComparison727vs7945':project_inputs()}
 all_ok=True
 for platform,(zipname,asset,target) in PLATFORMS.items():
  zpath=BASE/zipname; dest=BASE/platform
  proof=json.loads((PROOF/f'{platform}-byte-verification.json').read_text())
  zsha=sha(zpath); zsize=zpath.stat().st_size
  with ZipFile(zpath) as z:
   members=z.infolist(); names=[m.filename for m in members]; safe=True; seen=set(); entries=[]
   for m in members:
    pp=PurePosixPath(m.filename); mode=m.external_attr>>16
    if (not m.filename or pp.is_absolute() or any(x in ('','..','.') for x in pp.parts) or '\\\\' in m.filename or m.filename in seen or stat.S_ISLNK(mode)):
     safe=False
    seen.add(m.filename)
    if m.is_dir(): continue
    h=hashlib.sha256(); n=0
    with z.open(m) as f:
     for block in iter(lambda:f.read(1024*1024),b''): h.update(block); n+=len(block)
    p=dest/m.filename
    localok=p.is_file() and p.stat().st_size==n and sha(p)==h.hexdigest()
    entries.append({'path':m.filename,'size':n,'sha256':h.hexdigest(),'extractedBytesMatch':localok})
   expected_member_names={'log.html',f'native-binary-review/assets/{asset}','native-binary-review/native-binary-review.json','native-runtime-receipt.json','native-runtime-robot-evidence.json','native-runtime-robot-phases.log','output.xml','report.html'}
   if platform=='linux_amd64': expected_member_names.add('jat-class-consumer-receipt.json')
   extracted_names={p.relative_to(dest).as_posix() for p in dest.rglob('*') if p.is_file()}
   exact_members=set(names)==expected_member_names and len(names)==len(expected_member_names)
   extracted_members=extracted_names==expected_member_names
  metadata_path=dest/'native-binary-review'/'native-binary-review.json'
  metadata=json.loads(metadata_path.read_text()); binary=dest/'native-binary-review'/'assets'/asset
  receipt_path=dest/'native-runtime-receipt.json'; receipt=json.loads(receipt_path.read_text())
  recbin=receipt.get('binary',{})
  bsha=sha(binary); bsize=binary.stat().st_size
  binds=(metadata.get('sourceSha')==HEAD_SHA and metadata.get('version')==VERSION and metadata.get('platform')==platform and metadata.get('assetName')==asset and metadata.get('binarySha256')==bsha and metadata.get('binarySize')==bsize and receipt.get('commitSha')==HEAD_SHA and receipt.get('platform')==platform and recbin.get('sha256')==bsha and recbin.get('version')==VERSION and recbin.get('runtimeGOOS')==target[0] and recbin.get('runtimeGOARCH')==target[1] and recbin.get('expectedGOOS')==target[0] and recbin.get('expectedGOARCH')==target[1])
  goout=subprocess.check_output([str(GO),'version','-m',str(binary)],text=True,stderr=subprocess.STDOUT)
  compiler=goout.splitlines()[0].split(': ',1)[-1]; settings={}
  for line in goout.splitlines()[1:]:
   fields=line.strip().split(None,1)
   if len(fields)==2 and fields[0]=='build' and '=' in fields[1]: settings[fields[1].split('=',1)[0]]=fields[1].split('=',1)[1]
  buildok=(compiler=='go1.26.9' and settings.get('GOOS')==target[0] and settings.get('GOARCH')==target[1] and settings.get('vcs.revision')==HEAD_SHA and settings.get('vcs.modified')=='false')
  go9log=PROOF/f'{platform}-actual-go-metadata.txt'; go9text=go9log.read_text(); go9settings={}
  for line in go9text.splitlines()[1:]:
   fields=line.strip().split(None,1)
   if len(fields)==2 and fields[0]=='build' and '=' in fields[1]: go9settings[fields[1].split('=',1)[0]]=fields[1].split('=',1)[1]
  go9ok=go9text.splitlines()[0].endswith(': go1.26.9') and go9settings.get('GOOS')==target[0] and go9settings.get('GOARCH')==target[1] and go9settings.get('vcs.revision')==HEAD_SHA and go9settings.get('vcs.modified')=='false'
  buildok=buildok and go9ok
  meta_go=metadata.get('goBuildInfo',{})
  buildok=buildok and meta_go.get('goVersion')=='go1.26.9' and meta_go.get('goos')==target[0] and meta_go.get('goarch')==target[1] and meta_go.get('vcsRevision')==HEAD_SHA and meta_go.get('vcsModified') is False
  robot_path=dest/'output.xml'; xml=ET.parse(robot_path).getroot(); total=next(x for x in xml.findall('.//statistics/total/stat') if 'All Tests' in ''.join(x.itertext()))
  counts={k:int(total.get(k,'-1')) for k in ('pass','fail','skip')}
  robot=json.loads((dest/'native-runtime-robot-evidence.json').read_text())
  robotok=counts=={'pass':1,'fail':0,'skip':0} and robot.get('binarySha256')==bsha
  jat=None
  if platform=='linux_amd64':
   jp=dest/'jat-class-consumer-receipt.json'; j=json.loads(jp.read_text())
   jat=j.get('commitSha')==HEAD_SHA and j.get('binary',{}).get('sha256')==bsha and j.get('binary',{}).get('version')==VERSION and j.get('exactBinaryCLI',{}).get('cold',{}).get('nativeImport')=='sqlite3'
  apiok=(proof.get('source')==HEAD_SHA and proof.get('version')==VERSION and proof.get('platform')==platform and proof.get('zipBytes')==zsize and proof.get('zipSha256')==zsha and proof.get('artifactId'))
  checks={'zipAndArtifactMetadataMatch':bool(apiok),'safeZipMembers':safe,'exactMemberSet':exact_members and extracted_members,'allExtractedMemberBytesMatch':all(x['extractedBytesMatch'] for x in entries),'binaryReceiptMetadataMatch':binds,'GoBuildMetadataMatchesSourceTargetAndGo1269':buildok,'officialGo1269MetadataReceiptMatches':go9ok,'robotXMLPass1Fail0Skip0AndBinaryBound':robotok,'linuxJATBoundToSourceAndBinary':jat}
  passed=all(v for k,v in checks.items() if k!='linuxJATBoundToSourceAndBinary') and jat is not False
  all_ok &= passed
  result['platforms'][platform]={'artifactId':proof.get('artifactId'),'zipPath':str(zpath),'zipBytes':zsize,'zipSha256':zsha,'assetCount':len(entries),'memberSet':entries,'binaryPath':str(binary),'binaryBytes':bsize,'binarySha256':bsha,'source':HEAD_SHA,'version':VERSION,'target':{'goos':target[0],'goarch':target[1]},'nativeReceiptSha256':sha(receipt_path),'robotXmlSha256':sha(robot_path),'robotCounts':counts,'officialGo1269MetadataLogSha256':sha(go9log),'officialGo1269MetadataLogMatches':go9ok,'goTool':result['inspectionTool'],'goBuildInfo':{'embeddedCompiler':compiler,'GOOS':settings.get('GOOS'),'GOARCH':settings.get('GOARCH'),'vcs.revision':settings.get('vcs.revision'),'vcs.modified':settings.get('vcs.modified')},'checks':checks,'pass':passed}
 result['pass']=all_ok
 return result
if __name__=='__main__': print(json.dumps(main(),indent=2,sort_keys=True))
