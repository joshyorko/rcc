import json,hashlib,subprocess,gzip,tarfile,zipfile,datetime
from pathlib import Path
p=Path('/workspace/work/rcc-swarm/evidence/performance-admission/go-object-pack-native-preexecution-1600'); r=json.loads((p/'embed-7945-preparation/generated-inputs-receipt.json').read_text());s=Path(r['ram_source_copy']);m=json.loads((p/'source-manifest.json').read_text());sha=lambda b:hashlib.sha256(b).hexdigest()
for e in m['snapshot_files']:
 b=(s/e['path']).read_bytes();git=subprocess.check_output(['git','-C','/workspace/rcc','show','7945f956b428359928908a9f42e36417b113dafd:'+e['path']]);assert b==git and len(b)==e['bytes'] and sha(b)==e['sha256']
for name,e in r['generated_embed_inputs'].items():
 b=(s/name).read_bytes();assert len(b)==e['size'] and sha(b)==e['sha256']
a=Path(r['official_archive']['path']);assert sha(a.read_bytes())==r['official_archive']['expected_sha256'];
with tarfile.open(a,'r:bz2') as t:b=t.extractfile('bin/micromamba').read();assert len(b)==r['archive_member']['size'] and sha(b)==r['archive_member']['sha256']
assert gzip.decompress((s/'blobs/assets/micromamba.linux_amd64.gz').read_bytes())==b
for name in r['copied_tracked_assets_and_docs']:
 rel=name.removeprefix('blobs/')
 if rel.startswith('docs/'):original=rel
 else:original=rel
 assert (s/name).read_bytes()==(s/original).read_bytes()
for name in r['zip_members_sorted_fixed_1980_timestamp']:
 source=s/'templates'/Path(name).stem
 with zipfile.ZipFile(s/name) as z:
  assert set(z.namelist())=={str(x.relative_to(source)) for x in source.rglob('*') if x.is_file()}
  for q in z.namelist():assert z.read(q)==(source/q).read_bytes()
v=p/'embed-7945-preparation/harness-successor-v2.go';assert sha(v.read_bytes())=='79cab09b35028beac804dde357bd789a0654d9e487b6960b63e0230cc8eb5f37';l=json.loads((p/'embed-7945-preparation/offline-go-list-result.json').read_text());assert l['numeric_exit']==0 and l['dependency_package_count']==266 and l['stop_reason'] is None
out={'at':datetime.datetime.now(datetime.timezone.utc).isoformat(),'actualAll227SourceGitBytesEqual':True,'actualAll39GeneratedSHAverified':True,'officialArchiveSHA':r['official_archive']['sha256'],'actualMemberPayloadSHA':r['archive_member']['sha256'],'deterministicExperimentalEmbeds':'zip1980/gzipmtime0; generatedfromexact7945, not shippingbinarybit-for-bit claim','actualCopiedDocsAssetsTemplateContentExact':True,'harnessSuccessorSHA':sha(v.read_bytes()),'staticDesignRootReviewed':'same tmpfs device botharms; boundedHash/size eachtimedread; allpackSlicesuntimed; corrupted/missing/range/nonaliascontrols ownedcopy; perpassfsyncreceipts+phase markers; missingIO/RSSfailclosed; observerCPUoverhead/RSSpoint notpeak;3AB2BApartialbalance','goList':'266offlinepackages numeric0, NOTbuild/run proof','benchmarkAdmission':'PENDING supervisor/controltests/resourceguard'};Path(__file__).with_name('root-input-acceptance.json').write_text(json.dumps(out,indent=2)+'\n');print('Root actual227Git/39generated/officialarchive/payload/templates/offline266 verified0')
