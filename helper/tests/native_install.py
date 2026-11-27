"""Install a checksum-pinned local package with the native helper."""
import hashlib
import io
import json
import platform
import subprocess
import sys
import tarfile
import tempfile
from pathlib import Path


def check(binary, root):
    target = {
        ("Linux", "x86_64"): "linux-64", ("Linux", "aarch64"): "linux-aarch64",
        ("Darwin", "arm64"): "osx-arm64", ("Darwin", "x86_64"): "osx-64",
        ("Windows", "AMD64"): "win-64",
    }[platform.system(), platform.machine()]
    channel = root / "channel" / target
    channel.mkdir(parents=True)
    record = dict(name="receipt", version="1", build="0", build_number=0,
                  depends=[], subdir=target)
    archive = channel / "receipt-1-0.tar.bz2"
    marker = "share/rcc-receipt"
    with tarfile.open(archive, "w:bz2") as tar:
        for name, body in [(marker, b"installed\n"), ("info/files", marker.encode()+b"\n"),
                           ("info/index.json", json.dumps(record).encode())]:
            item = tarfile.TarInfo(name)
            item.size = len(body)
            tar.addfile(item, io.BytesIO(body))
    record.update(size=archive.stat().st_size, sha256=hashlib.sha256(archive.read_bytes()).hexdigest())
    (channel / "repodata.json").write_text(json.dumps({"info": {"subdir": target},
        "packages": {archive.name: record}, "repodata_version": 1}))
    request = dict(protocol=1, prefix=str(root/"prefix"), cache=str(root/"cache"),
                   platform=target, channels=[str(channel.parent)], packages=["receipt=1"])
    subprocess.run([binary], input=json.dumps(request), text=True, check=True)
    assert (root/"prefix"/marker).read_bytes() == b"installed\n"
    installed = json.loads((root/"prefix/conda-meta/receipt-1-0.json").read_text())
    assert installed["sha256"] == record["sha256"]
    print(json.dumps({"platform": target, "helperSHA256": hashlib.sha256(binary.read_bytes()).hexdigest(),
                      "installedSHA256": installed["sha256"]}))


if __name__ == "__main__":
    with tempfile.TemporaryDirectory() as tmp:
        check(Path(sys.argv[1]).resolve(strict=True), Path(tmp))
