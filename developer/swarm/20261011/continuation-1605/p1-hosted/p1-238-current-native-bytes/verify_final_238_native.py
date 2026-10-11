from pathlib import Path
import hashlib,json,subprocess,xml.etree.ElementTree as ET
BASE=Path(__file__).resolve().parent;RAM=Path('/dev/shm/rcc-p1-238-current-native-root')
GO=Path('/workspace/work/rcc-swarm/tools/go1.26.9/bin/go');PY='/home/agent/.robocorp/holotree/c2fd1aa_5a1fac3_9fcd2534/bin/python'
SOURCE='86cabfb8e4fac2e16048571b727bc60f2c800dba';VERSION='v18.19.6'
ITEMS={
 'linux_amd64':(11706494255,'linux-amd64','rcc-linux64','linux','amd64',13386549,'c7aa524bb45951fb3ea2ed8c075de1552e05f203c3d25634edc1ece06e81827f'),
 'darwin_arm64':(11706619102,'macos-arm64','rcc-macosarm64','darwin','arm64',12498346,'53717cd314456468b3cf22af6c758592d1d42fe7cbca12af851bcd931fa967c9'),
 'darwin_amd64':(11706776700,'macos-amd64','rcc-macos64','darwin','amd64',13072666,'edb18d8c3288d4f5c71be202bf491a2abf6292ccb83f074ae781cdee8cfbf598'),
 'windows_amd64':(11706786360,'windows-amd64','rcc-windows64.exe','windows','amd64',10850344,'9142e881cffd0e38fc8c375b0544e40d013e8aeb3091e2e1a7acab0c22b5af4f')}
sha=lambda b:hashlib.sha256(b).hexdigest();api=json.loads((BASE/'api-run-artifacts-jobs.json').read_text())
assert api['runId']==38150188705 and api['sourceSha']==SOURCE and len(api['jobs'])==4
assert all(j['conclusion']=='success' for j in api['jobs']) and all(a['sourceSha']==SOURCE and a['runId']==api['runId'] and not a['expired'] for a in api['artifacts'])
finaldir=BASE/'finalcheck';finaldir.mkdir(exist_ok=True)
orig=BASE/'verify_native_bundle_238.py';helper=finaldir/'verify_native_bundle_238_final.py'
helper.write_text(orig.read_text().replace('out=zpath.parent/a.platform;assert not out.exists();out.mkdir()', 'out=zpath.parent/(a.platform+"-finalcheck");assert not out.exists();out.mkdir()'))
runs=[]
for platform,(aid,fileplat,binary,goos,goarch,size,zsha) in ITEMS.items():
 z=RAM/f'pr238-38150188705-{fileplat}-native-runtime-receipt.zip';cmd=[PY,str(helper),platform,str(z),str(aid),zsha]
 r=subprocess.run(cmd,text=True,stdout=subprocess.PIPE,stderr=subprocess.STDOUT);log=finaldir/f'{platform}-verifier.log';log.write_text(r.stdout)
 runs.append({'platform':platform,'command':cmd,'exitCode':r.returncode,'log':str(log),'logSha256':sha(log.read_bytes())});assert r.returncode==0,(platform,r.stdout)
report={'schemaVersion':1,'runId':api['runId'],'sourceSha':SOURCE,'version':VERSION,'goBinary':str(GO),'goVersion':subprocess.check_output([str(GO),'version'],text=True).strip(),'nativeJobConclusions':{j['name']:j['conclusion'] for j in api['jobs']},'platforms':{},'foreignBinariesExecutedLocally':False}
for platform,(aid,fileplat,binary,goos,goarch,size,zsha) in ITEMS.items():
 z=RAM/f'pr238-38150188705-{fileplat}-native-runtime-receipt.zip';d=RAM/platform;b=d/'native-binary-review'/'assets'/binary;rp=d/'native-runtime-receipt.json';rec=json.loads(rp.read_text());review=json.loads((d/'native-binary-review'/'native-binary-review.json').read_text());bsha=sha(b.read_bytes())
 assert z.stat().st_size==size and sha(z.read_bytes())==zsha and rec['commitSha']==SOURCE and rec['platform']==platform
 assert rec['binary']['sha256']==bsha and rec['binary']['version']==VERSION and rec['binary']['runtimeGOOS']==goos and rec['binary']['runtimeGOARCH']==goarch
 assert review['sourceSha']==SOURCE and review['platform']==platform and review['version']==VERSION and review['binarySha256']==bsha and review['nativeReceiptSha256']==sha(rp.read_bytes())
 meta=subprocess.run([str(GO),'version','-m',str(b)],capture_output=True,text=True);assert meta.returncode==0
 for t in ('go1.26.9',f'GOOS={goos}',f'GOARCH={goarch}','CGO_ENABLED=0',f'vcs.revision={SOURCE}','vcs.modified=false'): assert t in meta.stdout,(platform,t)
 x=ET.parse(d/'output.xml').getroot().find('./statistics/total/stat');counts={k:int(x.get(k,'0')) for k in ('pass','fail','skip')};assert counts=={'pass':1,'fail':0,'skip':0}
 cli=rec['exactBinaryCLI'];source=rec['sourceAPI']
 for e in (cli,source):
  assert e['cold']['cacheHit']=='provider' and e['cold']['exitCode']==0 and e['cold']['leaseId'] and e['cold']['leaseReleased'] is True
  assert e['warm']['cacheHit']=='local-materialization' and e['warm']['providerDeadWarmReuse'] is True and e['cold']['materializationId']==e['warm']['materializationId']
  assert e['mismatch']['rejected'] is True and e['mismatch']['providerObjectGets']==0 and e['cold']['nativeImport']=='sqlite3' and e['cold']['sqliteVersion']
 robot=rec['robotExactBinary'];assert robot['binarySha256']==bsha and robot['binaryVersion']==VERSION and robot['cold']['cacheHit']=='provider' and robot['execute']['leaseReleased'] is True and robot['warm']['providerDeadWarmReuse'] is True and robot['native']['import']=='sqlite3'
 entry={'artifactId':aid,'artifactName':next(a['name'] for a in api['artifacts'] if a['artifactId']==aid),'zipBytes':size,'githubZipDigest':f'sha256:{zsha}','actualZipPath':str(z),'actualBinaryPath':str(b),'binaryBytes':b.stat().st_size,'binarySha256':bsha,'nativeRuntimeReceiptSha256':sha(rp.read_bytes()),'embeddedGoMetadataExitCode':meta.returncode,'goos':goos,'goarch':goarch,
 'exactBinaryCLI':{'coldProviderHit':cli['cold']['cacheHit'],'coldLeaseReleased':cli['cold']['leaseReleased'],'warmHit':cli['warm']['cacheHit'],'providerDeadWarmReuse':cli['warm']['providerDeadWarmReuse'],'mismatchRejected':cli['mismatch']['rejected'],'mismatchProviderObjectGets':cli['mismatch']['providerObjectGets'],'nativeImport':cli['cold']['nativeImport'],'sqliteVersion':cli['cold']['sqliteVersion']},
 'sourceAPI':{'coldProviderHit':source['cold']['cacheHit'],'coldLeaseReleased':source['cold']['leaseReleased'],'warmHit':source['warm']['cacheHit'],'providerDeadWarmReuse':source['warm']['providerDeadWarmReuse'],'mismatchRejected':source['mismatch']['rejected'],'mismatchProviderObjectGets':source['mismatch']['providerObjectGets']},'robotCounts':counts,'robotBinaryMatchesReceipt':robot['binarySha256']==bsha,'platformChecks':cli['platformChecks'],'linuxJATReceiptPresent':platform=='linux_amd64'}
 if platform=='linux_amd64':
  jp=d/'jat-class-consumer-receipt.json';jat=json.loads(jp.read_text());assert jat['commitSha']==SOURCE and jat['binary']['sha256']==bsha and jat['binary']['version']==VERSION and jat['exactBinaryCLI']['cold']['nativeImport']=='sqlite3'
  entry['linuxJAT']={'receiptSha256':sha(jp.read_bytes()),'artifactDigest':jat['exactBinaryCLI']['artifactDigest'],'objectCount':jat['exactBinaryCLI']['objectCount'],'nativeImport':jat['exactBinaryCLI']['cold']['nativeImport'],'coldLeaseReleased':jat['exactBinaryCLI']['cold']['leaseReleased'],'warmProviderDeadReuse':jat['exactBinaryCLI']['warm']['providerDeadWarmReuse'],'mismatchProviderObjectGets':jat['exactBinaryCLI']['mismatch']['providerObjectGets']}
 report['platforms'][platform]=entry
rp=BASE/'final-independent-verification.json';rp.write_text(json.dumps(report,sort_keys=True,indent=2)+'\n')
exitrec={'schemaVersion':1,'runId':api['runId'],'sourceSha':SOURCE,'verifierRuns':runs,'report':str(rp),'reportSha256':sha(rp.read_bytes()),'allVerifierExitsZero':all(r['exitCode']==0 for r in runs)}
(BASE/'final-verification-exit.json').write_text(json.dumps(exitrec,sort_keys=True,indent=2)+'\n')
print(json.dumps({'status':'PASS','runId':api['runId'],'sourceSha':SOURCE,'platformCount':len(report['platforms']),'verifierExitCodes':[r['exitCode'] for r in runs],'binarySHA256':{k:v['binarySha256'] for k,v in report['platforms'].items()},'receipt':str(rp)},sort_keys=True))
