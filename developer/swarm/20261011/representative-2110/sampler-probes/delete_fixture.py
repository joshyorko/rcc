import sys,time,json
from pathlib import Path
p=Path(sys.argv[1]);deadline=time.monotonic()+1.5;rounds=0
while time.monotonic()<deadline:
 for i in range(128): (p/f'tmp-{i:03d}').write_bytes(b'x'*128)
 for i in range(128): (p/f'tmp-{i:03d}').unlink()
 rounds+=1
print(json.dumps({'rounds':rounds,'temporary_files_written_deleted':rounds*128}))
