#!/usr/bin/env python3
"""Generate exact-source RCC 7945 embed inputs in a caller-owned RAM copy."""
from __future__ import annotations
import gzip
import hashlib
import json
import os
from pathlib import Path, PurePosixPath
import shutil
import stat
import tarfile
from zipfile import ZIP_DEFLATED, ZipFile, ZipInfo

EXPECTED_ARCHIVE = "cec496f2299f9ceb5f5e23fc2ccf081fffda0c4bc87a6fdab575bf1e04f103b6"
EXPECTED_COMMIT = "7945f956b428359928908a9f42e36417b113dafd"
EXPECTED_MEMBER = "bin/micromamba"

def sha(data: bytes) -> str:
    return hashlib.sha256(data).hexdigest()

def hash_file(path: Path) -> tuple[int, str]:
    h=hashlib.sha256(); n=0
    with path.open("rb") as f:
        while True:
            chunk=f.read(1024*1024)
            if not chunk: break
            n+=len(chunk); h.update(chunk)
    return n,h.hexdigest()

def main() -> None:
    source=Path("/workspace/work/rcc-swarm/evidence/performance-admission/go-object-pack-native-preexecution-1600/source-7945")
    archive=Path("/dev/shm/rcc-125-exact7945-embedprep-workers226/micromamba-linux-64.tar.bz2")
    work=Path("/dev/shm/rcc-125-exact7945-embedprep-workers226/source-accepted01")
    receipt=Path("/dev/shm/rcc-125-exact7945-embedprep-workers226/receipt-accepted01.json")
    manifest_path=Path("/workspace/work/rcc-swarm/evidence/performance-admission/go-object-pack-native-preexecution-1600/source-manifest.json")
    manifest=json.loads(manifest_path.read_text())
    if manifest["source_commit"] != EXPECTED_COMMIT or Path(manifest["snapshot_root"]) != source:
        raise RuntimeError("source manifest identity mismatch")
    if work.exists() or receipt.exists():
        raise RuntimeError("refusing to overwrite owned output")
    archive_size,archive_sha=hash_file(archive)
    if archive_sha != EXPECTED_ARCHIVE or archive_size > 12*1024*1024:
        raise RuntimeError(f"archive gate failed: {archive_size} {archive_sha}")
    tracked={}
    for entry in manifest["snapshot_files"]:
        rel,expected=entry["path"],entry["sha256"]
        src=source/rel
        if not src.is_file() or src.is_symlink(): raise RuntimeError(f"bad tracked input {rel}")
        n,digest=hash_file(src)
        if digest != expected: raise RuntimeError(f"tracked input mismatch: {rel}")
        tracked[rel]={"size":n,"sha256":digest}
    shutil.copytree(source,work,copy_function=shutil.copy2)
    for d in (work, *[p for p in work.rglob("*") if p.is_dir()]):
        d.chmod(d.stat().st_mode | 0o700)
    with tarfile.open(archive,"r:bz2") as tar:
        members=tar.getmembers()
        found=[]
        for m in members:
            name=m.name.replace("\\","/")
            norm=PurePosixPath(name)
            if norm.is_absolute() or ".." in norm.parts or not (m.isdir() or m.isreg()):
                raise RuntimeError(f"unsafe tar member: {m.name!r}")
            if name == EXPECTED_MEMBER: found.append(m)
        if len(found)!=1 or not found[0].isreg(): raise RuntimeError("expected one regular bin/micromamba")
        stream=tar.extractfile(found[0])
        if stream is None: raise RuntimeError("cannot read expected member")
        binary=stream.read(32*1024*1024+1)
        if len(binary)>32*1024*1024: raise RuntimeError("micromamba binary exceeds 32 MiB bound")
    binary_sha=sha(binary)
    asset_root=work/"blobs/assets"
    docs_root=work/"blobs/docs"
    asset_root.mkdir(parents=True,exist_ok=True)
    docs_root.mkdir(parents=True,exist_ok=True)
    generated={}
    def record(path: Path, data: bytes):
        path.parent.mkdir(parents=True,exist_ok=True)
        with path.open("xb") as f: f.write(data)
        rel=path.relative_to(work).as_posix()
        generated[rel]={"size":len(data),"sha256":sha(data)}
    # Match the exact source task's template selection and relative member paths.
    template_zips=[]
    for directory in sorted(p for p in (work/"templates").glob("*/") if p.is_dir()):
        name=directory.name+".zip"
        out=asset_root/name
        with ZipFile(out,"x",ZIP_DEFLATED) as z:
            for f in sorted(p for p in directory.rglob("*") if p.is_file()):
                rel=f.relative_to(directory).as_posix()
                info=ZipInfo(rel,date_time=(1980,1,1,0,0,0))
                info.compress_type=ZIP_DEFLATED
                info.external_attr=(stat.S_IFREG|0o644)<<16
                z.writestr(info,f.read())
        size,digest=hash_file(out)
        generated[out.relative_to(work).as_posix()]={"size":size,"sha256":digest}
        template_zips.append(out.relative_to(work).as_posix())
    copied=[]
    for pattern in ("*.txt","*.yaml","*.py"):
        for f in sorted((work/"assets").glob(pattern)):
            dest=asset_root/f.name; data=f.read_bytes(); record(dest,data);copied.append(dest.relative_to(work).as_posix())
    man_root=asset_root/"man"; man_root.mkdir(parents=True,exist_ok=True)
    for f in sorted((work/"assets/man").glob("*.txt")):
        dest=man_root/f.name;record(dest,f.read_bytes());copied.append(dest.relative_to(work).as_posix())
    for f in sorted((work/"docs").glob("*.md")):
        dest=docs_root/f.name;record(dest,f.read_bytes());copied.append(dest.relative_to(work).as_posix())
    gzpath=asset_root/"micromamba.linux_amd64.gz"
    with gzpath.open("xb") as raw:
        with gzip.GzipFile(filename="",mode="wb",compresslevel=9,fileobj=raw,mtime=0) as gz:
            gz.write(binary)
    size,digest=hash_file(gzpath)
    if gzip.decompress(gzpath.read_bytes()) != binary: raise RuntimeError("gzip payload check failed")
    generated[gzpath.relative_to(work).as_posix()]={"size":size,"sha256":digest}
    generated_tree=hashlib.sha256()
    for rel,info in sorted(generated.items()):
        generated_tree.update(rel.encode()+b"\0"+info["sha256"].encode()+b"\0"+str(info["size"]).encode()+b"\n")
    rec={
      "status":"PREPARATION_ONLY",
      "source_commit":EXPECTED_COMMIT,
      "source_tree":manifest["source_tree"],
      "source_manifest_sha256":sha(manifest_path.read_bytes()),
      "tracked_file_count":len(tracked),
      "tracked_bytes":sum(v["size"] for v in tracked.values()),
      "tracked_inputs":tracked,
      "official_archive":{"path":str(archive),"size":archive_size,"sha256":archive_sha,"expected_sha256":EXPECTED_ARCHIVE},
      "archive_member":{"path":EXPECTED_MEMBER,"size":len(binary),"sha256":binary_sha},
      "generated_embed_inputs":generated,
      "generated_embed_file_count":len(generated),
      "generated_embed_bytes":sum(v["size"] for v in generated.values()),
      "generated_embed_tree_sha256":generated_tree.hexdigest(),
      "zip_members_sorted_fixed_1980_timestamp":template_zips,
      "copied_tracked_assets_and_docs":copied,
      "linux_gzip_mtime_unix":0,
      "ram_source_copy":str(work),
    }
    receipt.write_text(json.dumps(rec,sort_keys=True,separators=(",",":"))+"\n")
    print(json.dumps({k:v for k,v in rec.items() if k not in ("tracked_inputs","generated_embed_inputs","copied_tracked_assets_and_docs")},sort_keys=True))
if __name__=="__main__": main()
