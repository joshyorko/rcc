from pathlib import Path
import hashlib, json, re, subprocess, xml.etree.ElementTree as ET

BASE = Path(__file__).resolve().parent
RAM = Path('/dev/shm/rcc-p1-236-current-native-root')
GO = Path('/workspace/work/rcc-swarm/tools/go1.26.9/bin/go')
SOURCE = 'bdeff0bc40a15b93e3df3ee430ee4a021d0d70f8'
VERSION = 'v18.19.6'
ITEMS = {
    'linux_amd64': (11706526381, 'linux-amd64', 'rcc-linux64', 'linux', 'amd64', 13385953, '065bd53924727776f01f6a5b0d72fd3f6615adb51763801572f287e22274f935'),
    'darwin_arm64': (11706636128, 'macos-arm64', 'rcc-macosarm64', 'darwin', 'arm64', 12497966, '0bd7ae2f49bc99823c996fc7c90a2b92a6f4d47c35bef9867b7bb257ad95c213'),
    'darwin_amd64': (11706013157, 'macos-amd64', 'rcc-macos64', 'darwin', 'amd64', 13072743, '3ebb38d6d5888d332919bb325de2c545cd73d25aed79ebf136a8593057261533'),
    'windows_amd64': (11705647535, 'windows-amd64', 'rcc-windows64.exe', 'windows', 'amd64', 10850290, '11ed43eabaad48afb7e8e1ab158eb62aa724f61ca4b6a0c16d5a7c74044d5a54'),
}
sha = lambda data: hashlib.sha256(data).hexdigest()
api = json.loads((BASE / 'github-api-artifacts.json').read_text())
assert api['runId'] == 38148406741 and api['sourceSha'] == SOURCE
job_data = json.loads((BASE / 'api-run-jobs-and-artifacts.json').read_text())
assert all(job['conclusion'] == 'success' and job['run_id'] == 38148406741 for job in job_data['nativeJobs'])
assert len(job_data['nativeJobs']) == 4
report = {'schemaVersion': 1, 'runId': api['runId'], 'sourceSha': SOURCE, 'version': VERSION,
          'goBinary': str(GO), 'goVersion': subprocess.check_output([str(GO), 'version'], text=True).strip(),
          'nativeJobConclusions': {j['name']: j['conclusion'] for j in job_data['nativeJobs']},
          'platforms': {}, 'foreignBinariesExecutedLocally': False}
for platform, (artifact_id, file_platform, binary_name, goos, goarch, zip_size, zip_sha) in ITEMS.items():
    zpath = RAM / f'pr236-38148406741-{file_platform}-native-runtime-receipt.zip'
    assert zpath.is_file() and zpath.stat().st_size == zip_size and sha(zpath.read_bytes()) == zip_sha
    artifact = next(x for x in api['artifacts'] if x['artifactId'] == artifact_id)
    assert artifact['platform'] == platform and artifact['bytes'] == zip_size and artifact['digest'] == f'sha256:{zip_sha}'
    extracted = RAM / platform
    binary_path = extracted / 'native-binary-review' / 'assets' / binary_name
    binary_bytes = binary_path.read_bytes()
    binary_sha = sha(binary_bytes)
    receipt_path = extracted / 'native-runtime-receipt.json'
    receipt = json.loads(receipt_path.read_text())
    review = json.loads((extracted / 'native-binary-review' / 'native-binary-review.json').read_text())
    assert receipt['commitSha'] == SOURCE and receipt['platform'] == platform
    assert receipt['binary']['version'] == VERSION and receipt['binary']['sha256'] == binary_sha
    assert receipt['binary']['runtimeGOOS'] == goos and receipt['binary']['runtimeGOARCH'] == goarch
    assert review['sourceSha'] == SOURCE and review['platform'] == platform and review['version'] == VERSION
    assert review['binarySha256'] == binary_sha and review['nativeReceiptSha256'] == sha(receipt_path.read_bytes())
    meta = subprocess.run([str(GO), 'version', '-m', str(binary_path)], capture_output=True, text=True)
    assert meta.returncode == 0
    for token in ('go1.26.9', f'GOOS={goos}', f'GOARCH={goarch}', 'CGO_ENABLED=0',
                  f'vcs.revision={SOURCE}', 'vcs.modified=false'):
        assert token in meta.stdout, (platform, token)
    xml_root = ET.parse(extracted / 'output.xml').getroot()
    total = xml_root.find('./statistics/total/stat')
    counts = {key: int(total.get(key, '0')) for key in ('pass', 'fail', 'skip')}
    assert counts == {'pass': 1, 'fail': 0, 'skip': 0}
    cli = receipt['exactBinaryCLI']
    source_api = receipt['sourceAPI']
    for mode, evidence in (('exactBinaryCLI', cli), ('sourceAPI', source_api)):
        assert evidence['cold']['cacheHit'] == 'provider' and evidence['cold']['exitCode'] == 0
        assert evidence['cold']['leaseId'] and evidence['cold']['leaseReleased'] is True
        assert evidence['warm']['cacheHit'] == 'local-materialization'
        assert evidence['warm']['providerDeadWarmReuse'] is True
        assert evidence['cold']['materializationId'] == evidence['warm']['materializationId']
        assert evidence['mismatch']['rejected'] is True and evidence['mismatch']['providerObjectGets'] == 0
        assert evidence['cold']['nativeImport'] == 'sqlite3' and evidence['cold']['sqliteVersion']
    robot = receipt['robotExactBinary']
    assert robot['binarySha256'] == binary_sha and robot['binaryVersion'] == VERSION
    assert robot['cold']['cacheHit'] == 'provider' and robot['execute']['leaseReleased'] is True
    assert robot['warm']['providerDeadWarmReuse'] is True and robot['native']['import'] == 'sqlite3'
    assert review['goBuildInfo'] == {'goVersion': 'go1.26.9', 'goos': goos, 'goarch': goarch,
                                     'vcsRevision': SOURCE, 'vcsModified': False}
    platform_checks = cli['platformChecks']
    assert platform_checks
    entry = {'artifactId': artifact_id, 'artifactName': artifact['name'], 'zipBytes': zip_size,
             'githubZipDigest': f'sha256:{zip_sha}', 'actualZipPath': str(zpath),
             'actualBinaryPath': str(binary_path), 'binaryBytes': len(binary_bytes),
             'binarySha256': binary_sha, 'embeddedGoMetadataExitCode': meta.returncode,
             'goos': goos, 'goarch': goarch, 'nativeRuntimeReceiptSha256': sha(receipt_path.read_bytes()),
             'exactBinaryCLI': {'coldProviderHit': cli['cold']['cacheHit'], 'coldLeaseReleased': cli['cold']['leaseReleased'],
                                'warmHit': cli['warm']['cacheHit'], 'providerDeadWarmReuse': cli['warm']['providerDeadWarmReuse'],
                                'mismatchRejected': cli['mismatch']['rejected'], 'mismatchProviderObjectGets': cli['mismatch']['providerObjectGets'],
                                'nativeImport': cli['cold']['nativeImport'], 'sqliteVersion': cli['cold']['sqliteVersion']},
             'sourceAPI': {'coldProviderHit': source_api['cold']['cacheHit'], 'coldLeaseReleased': source_api['cold']['leaseReleased'],
                           'warmHit': source_api['warm']['cacheHit'], 'providerDeadWarmReuse': source_api['warm']['providerDeadWarmReuse'],
                           'mismatchRejected': source_api['mismatch']['rejected'], 'mismatchProviderObjectGets': source_api['mismatch']['providerObjectGets']},
             'robotCounts': counts, 'robotBinaryMatchesReceipt': robot['binarySha256'] == binary_sha,
             'platformChecks': platform_checks, 'linuxJATReceiptPresent': platform == 'linux_amd64' and (extracted / 'jat-class-consumer-receipt.json').is_file()}
    if platform == 'linux_amd64':
        assert entry['linuxJATReceiptPresent']
        jat = json.loads((extracted / 'jat-class-consumer-receipt.json').read_text())
        assert jat['commitSha'] == SOURCE and jat['binary']['sha256'] == binary_sha
        assert jat['binary']['version'] == VERSION and jat['exactBinaryCLI']['cold']['nativeImport'] == 'sqlite3'
        entry['linuxJAT'] = {'receiptSha256': sha((extracted / 'jat-class-consumer-receipt.json').read_bytes()),
                             'artifactDigest': jat['exactBinaryCLI']['artifactDigest'],
                             'objectCount': jat['exactBinaryCLI']['objectCount'],
                             'nativeImport': jat['exactBinaryCLI']['cold']['nativeImport'],
                             'coldLeaseReleased': jat['exactBinaryCLI']['cold']['leaseReleased'],
                             'warmProviderDeadReuse': jat['exactBinaryCLI']['warm']['providerDeadWarmReuse'],
                             'mismatchProviderObjectGets': jat['exactBinaryCLI']['mismatch']['providerObjectGets']}
    report['platforms'][platform] = entry
out=BASE/'final-independent-verification.json'
out.write_text(json.dumps(report, sort_keys=True, indent=2)+'\n')
print(json.dumps({'status':'PASS','sourceSha':SOURCE,'runId':api['runId'],'platformCount':len(report['platforms']),
                  'binarySha256':{k:v['binarySha256'] for k,v in report['platforms'].items()},
                  'receipt':str(out)},sort_keys=True))
