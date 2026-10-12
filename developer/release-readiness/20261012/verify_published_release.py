"""Read back the approved RCC publication and hash every downloaded asset."""
import argparse
from contextlib import nullcontext
import hashlib
import json
import os
from pathlib import Path
import subprocess
import sys
import urllib.request


def api(path):
    return json.loads(subprocess.check_output(["gh", "api", path], text=True))


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--manifest", type=Path, required=True)
    parser.add_argument("--manifest-sha", required=True)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--repository-root", type=Path, required=True)
    parser.add_argument("--go", required=True)
    parser.add_argument("--resume", action="store_true", help="Rehash retained public downloads without downloading again")
    args = parser.parse_args()
    if sys.flags.optimize:
        raise RuntimeError("Run without Python optimization so checks remain active")
    assert hashlib.sha256(args.manifest.read_bytes()).hexdigest() == args.manifest_sha
    manifest = json.loads(args.manifest.read_bytes())
    version, source = manifest["version"], manifest["sourceSha"]
    repository = "repos/joshyorko/rcc"
    tag = api(f"{repository}/git/ref/tags/{version}")
    assert tag["object"]["type"] == "commit"
    assert tag["object"]["sha"] == source
    release = api(f"{repository}/releases/tags/{version}")
    assert release["draft"] is False and release["prerelease"] is False and release["published_at"]
    assert api(f"{repository}/releases/latest")["tag_name"] == version
    assets = {asset["name"]: asset for asset in release["assets"]}
    assert len(assets) == len(release["assets"]) == 9
    assert set(assets) == set(manifest["assets"])
    args.output.mkdir(parents=True, exist_ok=args.resume)
    (args.output / "release-api.json").write_text(json.dumps(release, indent=2) + "\n")
    actual = {}
    asset_root = args.output / "assets"
    asset_root.mkdir(exist_ok=args.resume)
    for name, expected in manifest["assets"].items():
        asset = assets[name]
        assert asset["state"] == "uploaded" and asset["size"] == expected["size"]
        if asset.get("digest") is not None:
            assert asset["digest"] == "sha256:" + expected["sha256"]
        url = f"https://github.com/joshyorko/rcc/releases/download/{version}/{name}"
        assert asset["browser_download_url"] == url
        digest, size = hashlib.sha256(), 0
        # Public download; do not send an API credential to redirected storage.
        incoming = (asset_root / name).open("rb") if args.resume else urllib.request.urlopen(url, timeout=60)
        outgoing = nullcontext(None) if args.resume else (asset_root / name).open("wb")
        with incoming as stream, outgoing as target:
            while block := stream.read(1024 * 1024):
                size += len(block)
                assert size <= expected["size"]
                digest.update(block)
                if target is not None:
                    target.write(block)
        actual[name] = {"sha256": digest.hexdigest(), "size": size}
        assert actual[name] == expected, name
    sys.path.insert(0, str(args.repository_root / "scripts"))
    from release_publication import validate_asset_build_info
    metadata = validate_asset_build_info(asset_root, source, args.go)
    linux = asset_root / "rcc-linux64"
    linux.chmod(0o700)
    environment = {**os.environ, "ROBOCORP_HOME": str(args.output / "runtime-home")}
    runtime = subprocess.check_output([str(linux), "--version"], env=environment, text=True).strip()
    assert version in runtime, runtime
    assert api(f"{repository}/git/ref/tags/{version}")["object"]["sha"] == source
    report = {"version": version, "sourceSha": source, "releaseId": release["id"],
              "publishedAt": release["published_at"], "manifestSha256": args.manifest_sha,
              "downloadedAssets": actual, "actualBinaryMetadata": metadata, "linuxRuntimeVersion": runtime}
    (args.output / "verification.json").write_text(json.dumps(report, indent=2) + "\n")
    print(json.dumps(report, indent=2))


if __name__ == "__main__":
    main()
