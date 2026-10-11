import sys,json,hashlib,zipfile,datetime
from pathlib import Path
import xml.etree.ElementTree as ET

heads={727:'727c1ff8b679cdbbe12836e53c87734a1edb0071'}
mapping={'linux-amd64':('linux_amd64','rcc-linux64'),'windows-amd64':('windows_amd64','rcc-windows64.exe'),'macos-amd64':('darwin_amd64','rcc-macos64'),'macos-arm64':('darwin_arm64','rcc-macosarm64')}
for number in map(int,sys.argv[1:]):
 root=Path('/workspace/work/rcc-swarm/evidence/hosted-native')/'final-main-727'
 artifacts={a['name']:a for a in json.loads((root/'root-live-artifacts.json').read_text())['artifacts']}
 def archive(name):
  candidates=list(root.rglob(name+'.zip'))
  assert candidates, name
  p=candidates[0]; raw=p.read_bytes(); a=artifacts[name]
  assert a['workflow_run']['head_sha']==heads[number]
  assert len(raw)==a['size_in_bytes']
  assert 'sha256:'+hashlib.sha256(raw).hexdigest()==a['digest']
  return zipfile.ZipFile(p)
 bins=archive('native-runtime-binaries')
 out={'context':'final-main','source':heads[number],'at':datetime.datetime.now(datetime.timezone.utc).isoformat(),'platforms':[]}
 for native,(platform,filename) in mapping.items():
  if native+'-native-runtime-receipt' not in artifacts: continue
  z=archive(native+'-native-runtime-receipt')
  receipt=json.loads(z.read('native-runtime-receipt.json'))
  assert receipt['commitSha']==heads[number] and receipt['platform']==platform
  names=[n for n in bins.namelist() if Path(n).name==filename]
  assert len(names)==1
  digest=hashlib.sha256(bins.read(names[0])).hexdigest()
  assert receipt['binary']['sha256']==digest
  cli=receipt['exactBinaryCLI']; cold=cli['cold']; warm=cli['warm']; bad=cli['mismatch']
  assert cold['cacheHit']=='provider' and cold['exitCode']==0 and cold['leaseReleased'] is True
  assert cold['nativeImport']=='sqlite3' and cold['sqliteVersion']=='3.54.0'
  assert warm['cacheHit']=='local-materialization' and warm['exitCode']==0 and warm['leaseReleased'] is True and warm['providerDeadWarmReuse'] is True
  assert bad['rejected'] is True and bad['providerObjectGets']==0
  robot=json.loads(z.read('native-runtime-robot-evidence.json'))
  assert robot['binarySha256']==digest and robot['cold']['cacheHit']=='provider'
  assert robot['execute']['exitCode']==0 and robot['execute']['leaseReleased'] is True and robot['warm']['providerDeadWarmReuse'] is True
  xml=ET.fromstring(z.read('output.xml'))
  tests=list(xml.iter('test'))
  assert len(tests)==1 and tests[0].find('status').get('status')=='PASS'
  row={'platform':platform,'actualBinarySha256':digest,'artifact':artifacts[native+'-native-runtime-receipt']['id'],'zipDigestVerified':True,'nativeColdWarmLeasesSQLite':True,'cpuMismatchZeroGets':True,'robotExactBinaryPass':1,'signing':cli['platformChecks'].get('codeSigning','not-attested')}
  if platform=='linux_amd64':
   jat=json.loads(z.read('jat-class-consumer-receipt.json'))
   assert jat['commitSha']==heads[number] and jat['binary']['sha256']==digest
   jc=jat['exactBinaryCLI']; assert jc['objectCount']>0
   assert jc['cold']['cacheHit']=='provider' and jc['cold']['exitCode']==0 and jc['cold']['leaseReleased'] is True
   assert jc['warm']['cacheHit']=='local-materialization' and jc['warm']['exitCode']==0 and jc['warm']['leaseReleased'] is True and jc['warm']['providerDeadWarmReuse'] is True
   assert jc['mismatch']['rejected'] is True and jc['mismatch']['providerObjectGets']==0
   row['jatPass']=True
  out['platforms'].append(row)
 out['allFourPlatformsVerified']=len(out['platforms'])==4
 (root/'root-native-progress-verification.json').write_text(json.dumps(out,indent=2)+'\n')
 print(json.dumps({'context':'final-main','rootVerified':True,'actualBinaries':len(out['platforms']),'nativeRobotPass':len(out['platforms']),'allFourVerified':out['allFourPlatformsVerified'],'linuxJat':any(row.get('jatPass') is True for row in out['platforms'])}))
