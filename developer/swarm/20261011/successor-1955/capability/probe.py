#!/usr/bin/env python3
"""One-shot, bounded host capability probe for RCC #125 successor lane."""
import errno
import fcntl
import hashlib
import json
import os
import platform
import shutil
import stat
import sys
import time

FICLONE = 0x40049409
SIZE = 64 * 1024
PATTERN = bytes((i * 37 + 11) % 256 for i in range(SIZE))
ROOTS = [
    ("workspace", "/workspace/rcc-continuation/developer/swarm/20261011/successor-1955/capability"),
    ("tmp", "/tmp/rcc-successor-capability-1955"),
]

def sha(path):
    h = hashlib.sha256()
    with open(path, "rb") as f:
        for block in iter(lambda: f.read(65536), b""):
            h.update(block)
    return h.hexdigest()

def identity(path):
    st = os.stat(path, follow_symlinks=False)
    return {"device": st.st_dev, "inode": st.st_ino, "mode_octal": oct(stat.S_IMODE(st.st_mode)), "size": st.st_size}

def safe_copy(source, destination):
    src_stat = os.lstat(source)
    if not stat.S_ISREG(src_stat.st_mode):
        raise OSError(errno.EINVAL, "source is not a regular file")
    sfd = os.open(source, os.O_RDONLY | getattr(os, "O_NOFOLLOW", 0))
    dfd = os.open(destination, os.O_WRONLY | os.O_CREAT | os.O_EXCL | getattr(os, "O_NOFOLLOW", 0), 0o600)
    try:
        while True:
            block = os.read(sfd, 65536)
            if not block:
                break
            view = memoryview(block)
            while view:
                n = os.write(dfd, view)
                view = view[n:]
        os.fsync(dfd)
    finally:
        os.close(sfd)
        os.close(dfd)

def main():
    started = time.time_ns()
    out = {"started_unix_ns": started, "kernel": platform.release(), "platform": platform.platform(),
           "python": sys.version, "ficlone_ioctl": hex(FICLONE), "payload_bytes": SIZE,
           "roots": [], "policy": "one invocation only; refuses pre-existing receipt or probe names"}
    for label, root in ROOTS:
        os.makedirs(root, mode=0o700, exist_ok=True)
        receipt = os.path.join(root, "probe-receipt.json")
        if os.path.exists(receipt):
            raise RuntimeError(f"refusing repeat probe; receipt already exists: {receipt}")
        prefix = f"{label}-1955"
        src = os.path.join(root, prefix + "-source.bin")
        dst = os.path.join(root, prefix + "-clone.bin")
        if any(os.path.lexists(p) for p in (src, dst)):
            raise RuntimeError(f"refusing repeat probe; probe pathname exists under {root}")
        with open(src, "xb") as f:
            f.write(PATTERN)
            f.flush(); os.fsync(f.fileno())
        os.chmod(src, 0o640)
        before = {"sha256": sha(src), "identity": identity(src)}
        os.close(os.open(dst, os.O_CREAT | os.O_EXCL | os.O_WRONLY, 0o600))
        item = {"root": root, "source": src, "source_before": before, "clone_destination": dst}
        try:
            with open(src, "rb") as sf, open(dst, "r+b") as df:
                fcntl.ioctl(df.fileno(), FICLONE, sf.fileno())
            item["ficlone"] = {"supported": True, "errno": None, "error": None}
            os.chmod(dst, 0o600)
            item["clone_before_mutation"] = {"sha256": sha(dst), "identity": identity(dst)}
            with open(dst, "r+b") as f:
                f.seek(0); f.write(b"R"); f.flush(); os.fsync(f.fileno())
            item["clone_after_mutation"] = {"sha256": sha(dst), "identity": identity(dst)}
            item["source_after_clone_mutation"] = {"sha256": sha(src), "identity": identity(src)}
            item["source_unchanged"] = item["source_after_clone_mutation"] == before
            item["clone_bytes_initially_equal"] = item["clone_before_mutation"]["sha256"] == before["sha256"]
            os.unlink(dst)
            item["clone_destination_unlinked"] = not os.path.lexists(dst)
            item["fallback"] = "not run: FICLONE succeeded"
        except OSError as e:
            item["ficlone"] = {"supported": False, "errno": e.errno, "errno_name": errno.errorcode.get(e.errno), "error": str(e)}
            # Preserve exact unsupported outcome and remove only the empty owned destination.
            try:
                os.unlink(dst)
            except FileNotFoundError:
                pass
            copy_path = os.path.join(root, prefix + "-copy.bin")
            symlink_path = os.path.join(root, prefix + "-symlink")
            os.symlink(src, symlink_path)
            try:
                safe_copy(symlink_path, os.path.join(root, prefix + "-symlink-copy.bin"))
                symlink_result = {"rejected": False, "error": None}
            except OSError as se:
                symlink_result = {"rejected": True, "errno": se.errno, "errno_name": errno.errorcode.get(se.errno), "error": str(se)}
            safe_copy(src, copy_path)
            os.chmod(copy_path, 0o600)
            cp_before = {"sha256": sha(copy_path), "identity": identity(copy_path)}
            with open(copy_path, "r+b") as f:
                f.seek(0); f.write(b"R"); f.flush(); os.fsync(f.fileno())
            cp_after = {"sha256": sha(copy_path), "identity": identity(copy_path)}
            src_after = {"sha256": sha(src), "identity": identity(src)}
            item["fallback"] = {"copy": copy_path, "copy_before_mutation": cp_before,
                                 "copy_after_mutation": cp_after, "source_after_copy_mutation": src_after,
                                 "source_unchanged": src_after == before,
                                 "copy_bytes_initially_equal": cp_before["sha256"] == before["sha256"],
                                 "different_inode": cp_before["identity"]["inode"] != before["identity"]["inode"],
                                 "symlink_rejection": symlink_result, "symlink_path": symlink_path}
        out["roots"].append(item)
    out["finished_unix_ns"] = time.time_ns()
    for item in out["roots"]:
        item["all_checks_pass"] = (
            item["source_unchanged"] and item["clone_bytes_initially_equal"] if item["ficlone"]["supported"] else
            item["fallback"]["source_unchanged"] and item["fallback"]["copy_bytes_initially_equal"] and item["fallback"]["different_inode"] and item["fallback"]["symlink_rejection"]["rejected"])
    with open(os.path.join(ROOTS[0][1], "probe-receipt.json"), "x") as f:
        json.dump(out, f, indent=2, sort_keys=True); f.write("\n")
    print(json.dumps(out, indent=2, sort_keys=True))
    if not all(x["all_checks_pass"] for x in out["roots"]):
        return 2
    return 0

if __name__ == "__main__":
    sys.exit(main())
