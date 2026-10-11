from pathlib import Path
import hashlib, json, subprocess, xml.etree.ElementTree as ET

BASE = Path(__file__).resolve().parent
RAM = Path('/dev/shm/rcc-p1-235-current-native-root')
GO = Path('/workspace/work/rcc-swarm/tools/go1.26.9/bin/go')
SOURCE = 'fa464a23ce5bc8c2b903bfc62d53b71205379812'
VERSION = 'v18.19.6'
ITEMS = {
    'linux_amd64': (11705982458, 'linux-amd64', 'rcc-linux64', 'linux', 'amd64', 13385941, '6ba70fa24f3118f7776fa2be6ad6ad88d5f6aaca57ce5b2591237c9a80f485ac'),
    'darwin_arm64': (11706037477, 'macos-arm64', 'rcc-macosarm64', 'darwin', 'arm64', 12498073, '07efb6630460fe61a94bc5cc708e5c3b0077f7d5f887137fbb754179185cb7b2'),
    'darwin_amd64': (11705997666, 'macos-amd64', 'rcc-macos64', 'darwin', 'amd64', 13072873, 'cfb290cf3a2434574bd190d3c00da0115c871e11bcfc0c5598b095beb20f84ac'),
    'windows_amd64': (11706528113, 'windows-amd64', 'rcc-windows64.exe', 'windows', 'amd64', 10849798, '2a49f4b51c314e6b96950e58d386f8043e730eedfec83508c496cc8d354c01ce'),
}
sha = lambda data: hashlib.sha256(data).hexdigest()
api = json.loads((BASE / 'api-run-artifacts-jobs.json').read_text())
assert api['runId'] == 38148812198 and api['sourceSha'] == SOURCE
assert len(api['jobs']) == 4 and all(j['conclusion'] == 'success' for j in api['jobs'])
assert all(x['runId'] == api['runId'] and x['sourceSha'] == SOURCE and not x['expired'] for x in api['artifacts'])
source_helper = BASE / 'verify_native_bundle_235.py'
final_helper = BASE / 'finalcheck' / 'verify_native_bundle_235_final.py'
final_helper.parent.mkdir(exist_ok=True)
helper_text = source_helper.read_text().replace('out=zpath.parent/a.platform;assert not out.exists();out.mkdir()',
                                                'out=zpath.parent/(a.platform+"-finalcheck");assert not out.exists();out.mkdir()')
final_helper.write_text(helper_text)
python = '/home/agent/.robocorp/holotree/c2fd1aa_5a1fac3_9fcd2534/bin/python'
runs=[]
for platform,(artifact_id,file_platform,binary_name,goos,goarch,zip_size,zip_sha) in ITEMS.items():
    zip_path=RAM/f'pr235-38148812198-{file_platform}-native-runtime-receipt.zip'
    cmd=[python,str(final_helper),platform,str(zip_path),str(artifact_id),zip_sha]
    result=subprocess.run(cmd,text=True,stdout=subprocess.PIPE,stderr=subprocess.STDOUT)
    log=BASE/'finalcheck'/f'{platform}-verifier.log';log.write_text(result.stdout)
    runs.append({'platform':platform,'command':cmd,'exitCode':result.returncode,'log':str(log),
                 'logSha256':sha(log.read_bytes())})
    assert result.returncode==0,(platform,result.stdout)
report={'schemaVersion':1,'runId':api['runId'],'sourceSha':SOURCE,'version':VERSION,
        'goBinary':str(GO),'goVersion':subprocess.check_output([str(GO),'version'],text=True).strip(),
        'nativeJobConclusions':{j['name']:j['conclusion'] for j in api['jobs']},'platforms':{},
        'foreignBinariesExecutedLocally':False}
for platform,(artifact_id,file_platform,binary_name,goos,goarch,zip_size,zip_sha) in ITEMS.items():
    zip_path=RAM/f'pr235-38148812198-{file_platform}-native-runtime-receipt.zip'
    extracted=RAM/platform
    binary=extracted/'native-binary-review'/'assets'/binary_name
    receipt_path=extracted/'native-runtime-receipt.json'
    receipt=json.loads(receipt_path.read_text())
    review=json.loads((extracted/'native-binary-review'/'native-binary-review.json').read_text())
    data=binary.read_bytes();binary_sha=sha(data)
    assert zip_path.stat().st_size==zip_size and sha(zip_path.read_bytes())==zip_sha
    assert receipt['commitSha']==SOURCE and receipt['platform']==platform
    assert receipt['binary']['version']==VERSION and receipt['binary']['sha256']==binary_sha
    assert receipt['binary']['runtimeGOOS']==goos and receipt['binary']['runtimeGOARCH']==goarch
    assert review['sourceSha']==SOURCE and review['platform']==platform and review['version']==VERSION
    assert review['binarySha256']==binary_sha and review['nativeReceiptSha256']==sha(receipt_path.read_bytes())
    meta=subprocess.run([str(GO),'version','-m',str(binary)],capture_output=True,text=True)
    assert meta.returncode==0
    for token in ('go1.26.9',f'GOOS={goos}',f'GOARCH={goarch}','CGO_ENABLED=0',f'vcs.revision={SOURCE}','vcs.modified=false'):
        assert token in meta.stdout,(platform,token)
    xml=ET.parse(extracted/'output.xml').getroot().find('./statistics/total/stat')
    counts={key:int(xml.get(key,'0')) for key in ('pass','fail','skip')}
    assert counts=={'pass':1,'fail':0,'skip':0}
    cli=receipt['exactBinaryCLI'];source_api=receipt['sourceAPI']
    for proof in (cli,source_api):
        assert proof['cold']['cacheHit']=='provider' and proof['cold']['exitCode']==0
        assert proof['cold']['leaseId'] and proof['cold']['leaseReleased'] is True
        assert proof['warm']['cacheHit']=='local-materialization' and proof['warm']['providerDeadWarmReuse'] is True
        assert proof['cold']['materializationId']==proof['warm']['materializationId']
        assert proof['mismatch']['rejected'] is True and proof['mismatch']['providerObjectGets']==0
        assert proof['cold']['nativeImport']=='sqlite3' and proof['cold']['sqliteVersion']
    robot=receipt['robotExactBinary']
    assert robot['binarySha256']==binary_sha and robot['binaryVersion']==VERSION
    assert robot['cold']['cacheHit']=='provider' and robot['execute']['leaseReleased'] is True
    assert robot['warm']['providerDeadWarmReuse'] is True and robot['native']['import']=='sqlite3'
    entry={'artifactId':artifact_id,'artifactName':next(a['name'] for a in api['artifacts'] if a['artifactId']==artifact_id),
      'zipBytes':zip_size,'githubZipDigest':f'sha256:{zip_sha}','actualZipPath':str(zip_path),
      'actualBinaryPath':str(binary),'binaryBytes':len(data),'binarySha256':binary_sha,
      'nativeRuntimeReceiptSha256':sha(receipt_path.read_bytes()),'embeddedGoMetadataExitCode':meta.returncode,
      'goos':goos,'goarch':goarch,
      'exactBinaryCLI':{'coldProviderHit':cli['cold']['cacheHit'],'coldLeaseReleased':cli['cold']['leaseReleased'],
       'warmHit':cli['warm']['cacheHit'],'providerDeadWarmReuse':cli['warm']['providerDeadWarmReuse'],
       'mismatchRejected':cli['mismatch']['rejected'],'mismatchProviderObjectGets':cli['mismatch']['providerObjectGets'],
       'nativeImport':cli['cold']['nativeImport'],'sqliteVersion':cli['cold']['sqliteVersion']},
      'sourceAPI':{'coldProviderHit':source_api['cold']['cacheHit'],'coldLeaseReleased':source_api['cold']['leaseReleased'],
       'warmHit':source_api['warm']['cacheHit'],'providerDeadWarmReuse':source_api['warm']['providerDeadWarmReuse'],
       'mismatchRejected':source_api['mismatch']['rejected'],'mismatchProviderObjectGets':source_api['mismatch']['providerObjectGets']},
      'robotCounts':counts,'robotBinaryMatchesReceipt':robot['binarySha256']==binary_sha,
      'platformChecks':cli.get('platformChecks'),'linuxJATReceiptPresent':platform=='linux_amd64'}
    if platform=='linux_amd64':
        jat_path=extracted/'jat-class-consumer-receipt.json';jat=json.loads(jat_path.read_text())
        assert jat['commitSha']==SOURCE and jat['binary']['sha256']==binary_sha and jat['binary']['version']==VERSION
        entry['linuxJAT']={'receiptSha256':sha(jat_path.read_bytes()),'artifactDigest':jat['exactBinaryCLI']['artifactDigest'],
          'objectCount':jat['exactBinaryCLI']['objectCount'],'nativeImport':jat['exactBinaryCLI']['cold']['nativeImport'],
          'coldLeaseReleased':jat['exactBinaryCLI']['cold']['leaseReleased'],
          'warmProviderDeadReuse':jat['exactBinaryCLI']['warm']['providerDeadWarmReuse'],
          'mismatchProviderObjectGets':jat['exactBinaryCLI']['mismatch']['providerObjectGets']}
    report['platforms'][platform]=entry
report_path=BASE/'final-independent-verification.json'
report_path.write_text(json.dumps(report,sort_keys=True,indent=2)+'\n')
exit_receipt={'schemaVersion':1,'runId':api['runId'],'sourceSha':SOURCE,'verifierRuns':runs,
              'report':str(report_path),'reportSha256':sha(report_path.read_bytes()),'allVerifierExitsZero':all(x['exitCode']==0 for x in runs)}
(BASE/'final-verification-exit.json').write_text(json.dumps(exit_receipt,sort_keys=True,indent=2)+'\n')
print(json.dumps({'status':'PASS','runId':api['runId'],'sourceSha':SOURCE,'platforms':len(report['platforms']),
                  'binarySHA256':{k:v['binarySha256'] for k,v in report['platforms'].items()},
                  'verifierExitCodes':[r['exitCode'] for r in runs],'report':str(report_path)},sort_keys=True))
