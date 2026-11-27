#!/usr/bin/env python3
"""Exercise micromamba's sharded repodata path against a loopback channel."""

import argparse
import hashlib
import http.server
import json
import io
import os
import shutil
import tarfile
import subprocess
import tempfile
import threading
import time
import uuid
from pathlib import Path


PACKAGE = "rcc-shard-fixture"
VERSION = "1.0"
BUILD = "0"
ARCHIVE = f"{PACKAGE}-{VERSION}-{BUILD}.tar.bz2"
PAYLOAD = b"deterministic local package fixture\n"


def msgpack(value):
    """Encode the maps, strings, integers, and binary values used by this fixture."""
    if isinstance(value, dict):
        return _length(0x80, 0xDE, 0xDF, len(value)) + b"".join(msgpack(k) + msgpack(v) for k, v in value.items())
    if isinstance(value, (list, tuple)):
        size = len(value)
        prefix = bytes([0x90 | size]) if size < 16 else b"\xdc" + size.to_bytes(2, "big")
        return prefix + b"".join(msgpack(item) for item in value)
    if isinstance(value, str):
        raw = value.encode()
        if len(raw) < 32:
            return bytes([0xA0 | len(raw)]) + raw
        return b"\xd9" + bytes([len(raw)]) + raw
    if isinstance(value, int):
        if 0 <= value < 128:
            return bytes([value])
        if 0 <= value < 256:
            return b"\xcc" + value.to_bytes(1, "big")
        if 0 <= value < 65536:
            return b"\xcd" + value.to_bytes(2, "big")
        return b"\xce" + value.to_bytes(4, "big")
    if isinstance(value, bytes):
        return b"\xc4" + bytes([len(value)]) + value
    raise TypeError(type(value))


def _length(fix, u16, u32, size):
    if size < 16:
        return bytes([fix | size])
    if size < 65536:
        return bytes([u16]) + size.to_bytes(2, "big")
    return bytes([u32]) + size.to_bytes(4, "big")


def build_files():
    payload = io.BytesIO()
    with tarfile.open(fileobj=payload, mode="w:bz2") as archive:
        metadata = json.dumps({"name": PACKAGE, "version": VERSION, "build": BUILD,
                               "build_number": 0, "depends": [], "subdir": "noarch"}).encode()
        member = tarfile.TarInfo("info/index.json")
        member.size = len(metadata)
        archive.addfile(member, io.BytesIO(metadata))
        member = tarfile.TarInfo("share/rcc-shard-fixture.txt")
        member.size = len(PAYLOAD)
        archive.addfile(member, io.BytesIO(PAYLOAD))
        for name, data in (("info/files", b"share/rcc-shard-fixture.txt\n"),
                           ("info/paths.json", json.dumps({"paths_version": 1, "paths": [
                               {"_path": "share/rcc-shard-fixture.txt", "path_type":"hardlink","sha256":hashlib.sha256(PAYLOAD).hexdigest(),"size_in_bytes":len(PAYLOAD)}
                           ]}).encode())):
            member = tarfile.TarInfo(name)
            member.size = len(data)
            archive.addfile(member, io.BytesIO(data))
    package = payload.getvalue()
    record = {"name": PACKAGE, "version": VERSION, "build": BUILD, "build_number": 0,
              "depends": [], "subdir": "noarch", "size": len(package),
              "sha256": hashlib.sha256(package).digest()}
    shard = msgpack({"packages": {ARCHIVE: record}, "packages.conda": {}})
    compressed_shard = subprocess.run([shutil.which("zstd"), "-q", "-c"], input=shard,
                                      capture_output=True, check=True).stdout
    digest = hashlib.sha256(compressed_shard).hexdigest()
    index = msgpack({"info": {"subdir": "noarch", "base_url": "./", "shards_base_url": "shards/"},
                     "shards": {PACKAGE: bytes.fromhex(digest)}})
    return compressed_shard, digest, index, package


class Channel(http.server.BaseHTTPRequestHandler):
    requests = []
    files = {}

    def do_GET(self):
        path = self.path.split("?", 1)[0].lstrip("/")
        type(self).requests.append({"path": self.path, "time": time.time()})
        body = type(self).files.get(path)
        if body is None:
            self.send_error(404)
            return
        self.send_response(200)
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def do_HEAD(self):
        path = self.path.split("?", 1)[0].lstrip("/")
        type(self).requests.append({"path": self.path, "method": "HEAD", "time": time.time()})
        body = type(self).files.get(path)
        self.send_response(200 if body is not None else 404)
        if body is not None:
            self.send_header("Content-Length", str(len(body)))
        self.end_headers()

    def log_message(self, *_args):
        pass


def run(binary, base, mode, rc_path, root):
    Channel.requests = []
    channel = base
    if mode == "flat-fallback":
        channel += "/flat"
    args = [str(binary), "create", "-y", "-p", str(root / mode), PACKAGE,
            "-c", channel, "--override-channels", "--no-rc", "--no-env"]
    if mode in ("custom-rc-default","disabled"):
        args.remove("--no-rc")
        args.extend(["--rc-file", str(rc_path)])
    env = {**os.environ, "MAMBA_ROOT_PREFIX": str(root / "mamba-root"), "MAMBA_NO_BANNER": "1"}
    args.extend(["--root-prefix",str(root / "mamba-root")])
    root.mkdir(parents=True,exist_ok=True)
    env.update(HOME=str(root),XDG_CACHE_HOME=str(root/"cache"),XDG_DATA_HOME=str(root/"data"),USERPROFILE=str(root))
    env["CONDA_PKGS_DIRS"] = str(root / "pkgs")
    started = time.perf_counter()
    proc = subprocess.run(args, env=env, capture_output=True, text=True, timeout=120)
    print(mode, proc.returncode, [r["path"] for r in Channel.requests],flush=True)
    return {"mode": mode, "argv": args, "exit_code": proc.returncode,
            "elapsed_seconds": time.perf_counter() - started,
            "requests": list(Channel.requests), "stdout": proc.stdout[-2000:],
            "stderr": proc.stderr[-3000:]}


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--binary", type=Path, default=Path.home() / ".robocorp/micromamba/v2.10.0/micromamba")
    parser.add_argument("--receipt", type=Path, default=Path("/tmp/sharded-channel-receipt.json"))
    args = parser.parse_args()
    binary = args.binary.resolve(strict=True)
    shard, digest, index, package = build_files()
    zstd = shutil.which("zstd")
    if not zstd:
        parser.error("zstd command is required")
    compressed = subprocess.run([zstd, "-q", "-c"], input=index, capture_output=True, check=True).stdout
    flat = json.dumps({"info": {"subdir": "noarch"},
                       "packages": {ARCHIVE: {"name": PACKAGE, "version": VERSION,
                       "build": BUILD, "build_number": 0, "depends": [], "subdir": "noarch",
                       "size": len(package), "sha256": hashlib.sha256(package).hexdigest()}}, "packages.conda": {},
                       "repodata_version": 1}).encode()
    server = http.server.ThreadingHTTPServer(("127.0.0.1", 0), Channel)
    threading.Thread(target=server.serve_forever, daemon=True).start()
    results = []
    try:
        base = f"http://127.0.0.1:{server.server_port}"
        with tempfile.TemporaryDirectory(prefix="rcc-sharded-channel-") as raw:
            root = Path(raw)
            for mode in ["default", "custom-rc-default", "disabled", "flat-fallback"]:
                channel = base if mode!="flat-fallback" else base+"/flat"
                rc = root / (mode+".yaml")
                rc.write_text("channels: ["+channel+"]\n"+("use_sharded_repodata: false\n" if mode=="disabled" else ""))
                empty = {"info":{"subdir":"linux-64"},"packages":{},"packages.conda":{},"repodata_version":1}
                files={"noarch/repodata.json":flat,"linux-64/repodata.json":json.dumps(empty).encode(),f"noarch/{ARCHIVE}":package}
                if mode!="flat-fallback":
                    files["noarch/repodata_shards.msgpack.zst"]=compressed
                    files[f"noarch/shards/{digest}.msgpack.zst"]=shard
                    empty_index=msgpack({"info":{"subdir":"linux-64","base_url":"./","shards_base_url":"shards/"},"shards":{}})
                    files["linux-64/repodata_shards.msgpack.zst"]=subprocess.run([zstd,"-qc"],input=empty_index,capture_output=True,check=True).stdout
                Channel.files={("flat/" if mode=="flat-fallback" else "")+key: value for key,value in files.items()}
                results.append(run(binary,base,mode,rc,root/mode))
                row = results[-1]
                reqs = row["requests"]
                paths = {r["path"] for r in reqs}
                assert row["exit_code"] == 0, (row["stdout"],row["stderr"],paths)
                assert (root / mode / mode / "share/rcc-shard-fixture.txt").read_bytes() == PAYLOAD
                if mode == "disabled":
                    assert not any("/shards/" in p for p in paths), paths
                    assert "/noarch/repodata.json" in paths, paths
                elif mode == "flat-fallback":
                    assert any(p.endswith("/repodata.json") for p in paths), paths
                    assert not any("/shards/" in p for p in paths), paths
                else:
                    assert f"/noarch/shards/{digest}.msgpack.zst" in paths, paths
                    assert not any(p.endswith("/noarch/repodata.json") for p in paths), paths
    finally:
        server.shutdown()
        server.server_close()
    report = {"fixture": "zstd-msgpack-content-addressed-sharded-channel", "package": PACKAGE,
              "binary": str(binary), "binary_sha256": hashlib.sha256(binary.read_bytes()).hexdigest(),
              "shard_sha256": digest, "modes": results}
    args.receipt.parent.mkdir(parents=True, exist_ok=True)
    args.receipt.write_text(json.dumps(report, indent=2) + "\n")
    print(json.dumps(report, indent=2))
    if any(r["exit_code"] for r in results):
        raise SystemExit(1)


if __name__ == "__main__":
    main()
