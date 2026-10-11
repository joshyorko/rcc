#!/usr/bin/env python3
"""Route the fixed global HTFS path into owned scratch; keep the home guard exact."""
import hashlib,json,os,subprocess
from pathlib import Path
own=Path(__file__).resolve().parent
source=Path('/workspace/rcc-frozen')
scratch=Path('/workspace/rcc-successor-lifecycle')
scratch.mkdir(mode=0o700,exist_ok=False)
global_path=scratch/'global-ht'
global_path.mkdir(mode=0o750)
raw=(source/'common/platform_linux.go').read_text()
assert raw.count('/opt/robocorp/ht')==1
replacement=raw.replace('/opt/robocorp/ht',str(global_path))
target=own/'platform_linux_isolated.go.txt'
target.write_text(replacement)
(own/'baseline-overlay.json').write_text(json.dumps({'Replace':{str(source/'common/platform_linux.go'):str(target)}},indent=2)+'\n')
for mode in ('baseline','prototype'):
    boundary=scratch/mode;boundary.mkdir(mode=0o700)
    (boundary/'rcc-home').mkdir(mode=0o700)
receipt={'source':'7945f956b428359928908a9f42e36417b113dafd','source_file':'common/platform_linux.go','original_sha256':hashlib.sha256(raw.encode()).hexdigest(),'replacement_sha256':hashlib.sha256(replacement.encode()).hexdigest(),'delta':'Only redirect fixed /opt/robocorp/ht to owned scratch; no home-mode or storage changes.', 'scratch':str(scratch),'guard':'Existing RCC_HOME, enclosing boundary and scratch root must remain 0700; no chmod repairs.'}
(own/'overlay-preparation.json').write_text(json.dumps(receipt,indent=2)+'\n')
