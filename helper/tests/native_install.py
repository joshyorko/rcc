"""Install a checksum-pinned local package with the native helper."""
import hashlib
import io
import os
import json
import platform
import subprocess
import sys
import tarfile
import tempfile
from pathlib import Path


def check(binary, rcc_binary, root):
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
    helper_hash = hashlib.sha256(binary.read_bytes()).hexdigest()
    rcc_hash = hashlib.sha256(rcc_binary.read_bytes()).hexdigest()
    print(json.dumps({"source": subprocess.check_output(["git", "rev-parse", "HEAD"], text=True).strip(), "rccSHA256": rcc_hash, "platform": target, "helperSHA256": helper_hash,
                      "installedSHA256": installed["sha256"]}))
    # RCC resolves "local" from ROBOCORP_HOME/channel.
    (channel.parent / "channeldata.json").write_text("{}")
    manifest = root / "conda.yaml"
    manifest.write_text("dependencies: rcc-provider-plan-v1\nrcc:\n  schema-version: 1\n  layers:\n    - provider: rattler\n      channels: [local]\n      packages: [receipt=1]\n")
    env = dict(os.environ, ROBOCORP_HOME=str(root))
    subprocess.run([rcc_binary, "holotree", "variables", manifest, "--json"],
                   cwd=root, env=env, check=True, stdout=subprocess.DEVNULL)
    spaces = [p for p in (root/"holotree").glob("*/share/rcc-receipt")
              if (p.parents[1]/"golden-ee.yaml").is_file()]
    assert spaces and all(p.read_bytes() == b"installed\n" for p in spaces)
    assert any("name: receipt" in (p.parents[1]/"golden-ee.yaml").read_text() for p in spaces)
    print(json.dumps({"materialized": True, "inventory": "receipt", "platform": target}))


if __name__ == "__main__":
    with tempfile.TemporaryDirectory() as tmp:
        check(Path(sys.argv[1]).resolve(strict=True), Path(sys.argv[2]).resolve(strict=True), Path(tmp))
