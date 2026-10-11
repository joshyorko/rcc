import argparse,json,hashlib,zipfile,stat,sys,subprocess,xml.etree.ElementTree as ET
from pathlib import Path,PurePosixPath
p=argparse.ArgumentParser();p.add_argument('platform');p.add_argument('zip');p.add_argument('artifact_id',type=int);p.add_argument('expected_zip_sha');a=p.parse_args();root=Path(__file__).resolve().parent;source='4592139422f02fb7bc4fbc3f8e0b612a42551044';version='v18.19.6';target={'linux_amd64':('linux','amd64','rcc-linux64'),'darwin_arm64':('darwin','arm64','rcc-macosarm64'),'darwin_amd64':('darwin','amd64','rcc-macos64'),'windows_amd64':('windows','amd64','rcc-windows64.exe')}[a.platform];zpath=Path(a.zip);sha=lambda b:hashlib.sha256(b).hexdigest();zh=sha(zpath.read_bytes());assert zh==a.expected_zip_sha
out=zpath.parent/a.platform;assert not out.exists();out.mkdir()
with zipfile.ZipFile(zpath) as z:
 members=z.infolist();assert len(members)<32 and sum(x.file_size for x in members)<32*1024*1024;assert len({x.filename for x in members})==len(members)
 for x in members:
  q=PurePosixPath(x.filename);assert not q.is_absolute() and '..' not in q.parts and '\\' not in x.filename and not x.is_dir();kind=stat.S_IFMT(x.external_attr>>16);assert kind in (0,stat.S_IFREG)
  dest=out.joinpath(*q.parts);dest.parent.mkdir(parents=True,exist_ok=True);dest.write_bytes(z.read(x))
binary=out/'native-binary-review/assets'/target[2];raw=binary.read_bytes();bh=sha(raw);receipt=json.loads((out/'native-runtime-receipt.json').read_text());metadata=json.loads((out/'native-binary-review/native-binary-review.json').read_text());assert metadata['sourceSha']==source and metadata['version']==version and metadata['binarySha256']==bh and metadata['binarySize']==len(raw) and metadata['platform']==a.platform
assert metadata['nativeReceiptSha256']==sha((out/'native-runtime-receipt.json').read_bytes())
meta=subprocess.run(['/workspace/work/rcc-swarm/tools/go1.26.9/bin/go','version','-m',str(binary)],capture_output=True,text=True);assert meta.returncode==0
for s in ('go1.26.9','GOOS='+target[0],'GOARCH='+target[1],'CGO_ENABLED=0','vcs.revision='+source,'vcs.modified=false'):assert s in meta.stdout
sys.path.insert(0,'/workspace/work/rcc-swarm/two-phase-release/scripts');import release_publication as gate
gate._validate_native_receipt(receipt,a.platform,target[2],{'sha256':bh,'size':len(raw)},source,version)
x=ET.parse(out/'output.xml');counts=x.find('statistics/total/stat');assert counts is not None and (int(counts.get('pass','0')),int(counts.get('fail','0')),int(counts.get('skip','0')))==(1,0,0)
jat=None
if a.platform=='linux_amd64':
 jat=json.loads((out/'jat-class-consumer-receipt.json').read_text());gate._validate_native_receipt(jat,a.platform,target[2],{'sha256':bh,'size':len(raw)},source,version,require_robot=False)
for f in out.rglob('*'):
 if f.is_file() and f.suffix in ('.json','.xml','.log'):(root/(a.platform+'-'+f.name)).write_bytes(f.read_bytes())
(root/(a.platform+'-actual-go-metadata.txt')).write_text(meta.stdout)
r={'status':'ROOT_ACTUAL_NATIVE_ZIP_BINARY_METADATA_RECEIPT_ROBOT_VERIFIED','source':source,'version':version,'platform':a.platform,'artifactId':a.artifact_id,'zipBytes':zpath.stat().st_size,'zipSha256':zh,'actualZipPath':str(zpath),'actualBinaryPath':str(binary),'binaryBytes':len(raw),'binarySha256':bh,'metadataExitCode':meta.returncode,'robot':{'passed':1,'failed':0,'skipped':0},'linuxJatActualReceiptValidated':jat is not None,'scope':'Exact PR239 source executed-native binary and receipts; not frozen shipping7945; foreign binaries not executed here; no signing or Rosetta claim beyond actualreceipt','memberInventory':[{'path':x.filename,'bytes':x.file_size,'sha256':sha((out/x.filename).read_bytes())} for x in members]};(root/(a.platform+'-byte-verification.json')).write_text(json.dumps(r,indent=2)+'\n');print(json.dumps(r))
