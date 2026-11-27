#!/usr/bin/env python3
"""Prepare pinned native UV and helper payloads for tagged Go builds."""
import argparse
import gzip
import hashlib
import os
from pathlib import Path
import platform
import subprocess
import tarfile
import tempfile
import urllib.request
import zipfile

VERSION = "0.9.26"
ARCHIVES = {
    "aarch64-apple-darwin": ("darwin", "arm64", "uv-aarch64-apple-darwin.tar.gz"),
    "aarch64-unknown-linux-gnu": ("linux", "arm64", "uv-aarch64-unknown-linux-gnu.tar.gz"),
    "x86_64-apple-darwin": ("darwin", "amd64", "uv-x86_64-apple-darwin.tar.gz"),
    "x86_64-pc-windows-msvc": ("windows", "amd64", "uv-x86_64-pc-windows-msvc.zip"),
    "x86_64-unknown-linux-gnu": ("linux", "amd64", "uv-x86_64-unknown-linux-gnu.tar.gz"),
}

def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--helper", required=True, type=Path, help="locked cargo build output for this native runner")
    args = ap.parse_args()
    system = platform.system().lower()
    goos = {"darwin": "darwin", "windows": "windows", "linux": "linux"}.get(system)
    goarch = {"x86_64": "amd64", "amd64": "amd64", "aarch64": "arm64", "arm64": "arm64"}.get(platform.machine().lower())
    target = next((t for t, (o, a, _) in ARCHIVES.items() if (o, a) == (goos, goarch)), None)
    if not target:
        raise SystemExit(f"unsupported native runner: {system}/{goarch}")
    helper = args.helper.resolve()
    if not helper.is_file(): raise SystemExit(f"helper executable missing: {helper}")
    pins = {
        "uv-aarch64-apple-darwin.tar.gz": "fcf0a9ea6599c6ae28a4c854ac6da76f2c889354d7c36ce136ef071f7ab9721f",
        "uv-aarch64-unknown-linux-gnu.tar.gz": "f71040c59798f79c44c08a7a1c1af7de95a8d334ea924b47b67ad6b9632be270",
        "uv-x86_64-apple-darwin.tar.gz": "171eb8c518313e157c5b4cec7b4f743bc6bab1bd23e09b646679a02d096a047f",
        "uv-x86_64-pc-windows-msvc.zip": "eb02fd95d8e0eed462b4a67ecdd320d865b38c560bffcda9a0b87ec944bdf036",
        "uv-x86_64-unknown-linux-gnu.tar.gz": "30ccbf0a66dc8727a02b0e245c583ee970bdafecf3a443c1686e1b30ec4939e8"
}
    archive = ARCHIVES[target][2]
    expected = pins[archive]
    base = os.environ.get("RCC_ENDPOINT_UV_RELEASES", "https://github.com/astral-sh/uv/releases/download").rstrip("/")
    url = f"{base}/{VERSION}/{archive}"
    with tempfile.TemporaryDirectory(prefix="rcc-provider-defaults-") as temp:
        downloaded = Path(temp) / archive
        urllib.request.urlretrieve(url, downloaded)
        if hashlib.sha256(downloaded.read_bytes()).hexdigest() != expected:
            raise SystemExit("UV archive SHA256 mismatch")
        if archive.endswith(".zip"):
            with zipfile.ZipFile(downloaded) as z:
                name = next(n for n in z.namelist() if n.endswith("/uv.exe") or n == "uv.exe")
                uv = z.read(name)
        else:
            with tarfile.open(downloaded, "r:gz") as t:
                member = next(m for m in t.getmembers() if m.name.endswith("/uv") and m.isfile())
                uv = t.extractfile(member).read()
    out = Path("blobs")
    assets = out / "assets"
    assets.mkdir(exist_ok=True)
    payloads = {"uv": uv, "rattler": helper.read_bytes()}
    rows = []
    for name, data in payloads.items():
        compressed = gzip.compress(data, mtime=0)
        asset = f"provider_default.{name}.{goos}_{goarch}.gz"
        (assets / asset).write_bytes(compressed)
        rows.append((name, asset, hashlib.sha256(data).hexdigest(), len(data), VERSION if name == "uv" else "0.1.0"))
    source = [f"//go:build rcc_provider_defaults\n\npackage blobs\n\nimport (\n\t\"embed\"\n\t\"fmt\"\n\t\"io\"\n)\n"]
    for name, asset, digest, size, version in rows:
        source.append(f"//go:embed assets/{asset}\nvar {name}Payload embed.FS\n")
    source.append("func openGeneratedProviderDefault(name string) (io.ReadCloser,string,string,error) {\n switch name {\n")
    for name, asset, digest, size, version in rows:
        source.append(f'case "{name}": b,e:={name}Payload.Open("assets/{asset}"); if e!=nil {{ return nil,"","",e }}; z,e:=openProviderGzip(b); return z,"{version}","{digest}",e\n')
    source.append('default: return nil,"","",fmt.Errorf("embedded provider %q is unavailable",name)\n }\n}\n')
    source.append("func generatedProviderDefaultInfo(name string)(string,string,int64,bool) { switch name {\n")
    for name, asset, digest, size, version in rows:
        source.append(f'case "{name}": return "{version}","{digest}",{size},true\n')
    source.append('default: return "","",0,false\n }\n}\n')
    (out / f"provider_payload_{goos}_{goarch}.go").write_text("".join(source))

if __name__ == "__main__": main()
