#!/usr/bin/env python3
"""Private Linux-only prototype: protect the exact configured existing 0700 home."""
import hashlib,json,subprocess
from pathlib import Path
own=Path(__file__).resolve().parent
source=Path('/workspace/rcc-frozen')
raw=(source/'pathlib/functions.go').read_text()
before='\t_, err = shared.MakeSharedDir(filepath.Dir(fullpath))\n\tfail.On(err != nil, "%v", err)'
assert raw.count(before)==1
after='''\tparent := filepath.Dir(fullpath)
\tif correct == 0750 && !common.SharedHolotree && filepath.Clean(parent) == filepath.Clean(common.Product.Home()) {
\t\tinfo, parentErr := os.Lstat(parent)
\t\tif parentErr == nil {
\t\t\tfail.On(!info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0700, "Private prototype home is not an existing 0700 directory")
\t\t\towner, ok := info.Sys().(*syscall.Stat_t)
\t\t\tfail.On(!ok || int(owner.Uid) != os.Getuid() || int(owner.Gid) != os.Getgid(), "Private prototype home has foreign ownership")
\t\t} else {
\t\t\tfail.On(!os.IsNotExist(parentErr), "Private prototype home cannot be inspected: %v", parentErr)
\t\t\t_, err = shared.MakeSharedDir(parent)
\t\t\tfail.On(err != nil, "%v", err)
\t\t}
\t} else {
\t\t_, err = shared.MakeSharedDir(parent)
\t\tfail.On(err != nil, "%v", err)
\t}'''
replacement=raw.replace(before,after).replace('import (','import (\n\t"syscall"',1)
target=own/'functions_private_linux.go.txt';target.write_text(replacement)
subprocess.run(['/workspace/rcc-successor-tools/mod/golang.org/toolchain@v0.0.1-go1.26.9.linux-amd64/bin/gofmt','-w',str(target)],check=True)
overlay=json.loads((own/'baseline-overlay.json').read_text());overlay['Replace'][str(source/'pathlib/functions.go')]=str(target)
(own/'prototype-overlay.json').write_text(json.dumps(overlay,indent=2)+'\n')
diff=subprocess.run(['diff','-u',str(source/'pathlib/functions.go'),str(target)],capture_output=True,text=True)
assert diff.returncode==1;(own/'prototype.patch').write_text(diff.stdout)
(own/'prototype-preparation.json').write_text(json.dumps({'original_sha256':hashlib.sha256(raw.encode()).hexdigest(),'prototype_sha256':hashlib.sha256(target.read_bytes()).hexdigest(),'platform':'linux/amd64 only','source':'7945f956b428359928908a9f42e36417b113dafd','scope':'Isolated overlay; protect exact existing current-UID/primary-GID 0700 private HOME in parent recursion only. Direct leaves/shared behavior retained. Not race-hardened or production accepted.'},indent=2)+'\n')
