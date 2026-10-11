import json,hashlib,re,datetime
from pathlib import Path
root=Path('/workspace/work/rcc-swarm/evidence/hosted-native/final-main-727')
rows=[]
for platform in ('linux-amd64','macos-amd64','macos-arm64','windows-amd64'):
 p=root/(platform+'-native-job.log')
 raw=p.read_bytes(); text=raw.decode()
 passed=re.findall(r'--- PASS: (TestCopyPythonPrefix[^ ]*)',text)
 top=[x for x in passed if '/' not in x]; sub=[x for x in passed if '/' in x]
 assert len(top)==7 and len(set(top))==7 and len(sub)==4 and len(set(sub))==4,(platform,top,sub)
 assert not re.search(r'--- (FAIL|SKIP): TestCopyPythonPrefix',text)
 assert re.search(r'--- PASS: TestRealCurrentRCCAtoBVertical',text)
 rows.append({'platform':platform,'log':p.name,'sha256':hashlib.sha256(raw).hexdigest(),'uvTopLevelPassed':7,'uvSubcasesPassed':4,'nativeVerticalPassed':True})
out={'source':'727c1ff8b679cdbbe12836e53c87734a1edb0071','at':datetime.datetime.now(datetime.timezone.utc).isoformat(),'logs':rows,'allFourLogsVerified':True}
(root/'root-native-uv-log-verification.json').write_text(json.dumps(out,indent=2)+'\n')
print(json.dumps({'source':out['source'],'allFourLogsVerified':True,'uvTopLevelTotal':28,'uvNestedTotal':16}))
