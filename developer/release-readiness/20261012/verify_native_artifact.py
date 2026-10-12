"""Verify a downloaded source-bound native receipt and its executed binary."""
import argparse
import hashlib
import json
from pathlib import Path, PurePosixPath
import sys
import zipfile
from xml.etree import ElementTree


def sha(path):
    digest = hashlib.sha256()
    with path.open("rb") as stream:
        for block in iter(lambda: stream.read(1024 * 1024), b""):
            digest.update(block)
    return digest.hexdigest()


def verify(archive, artifact, source, repository):
    sys.path.insert(0, str(repository / "scripts"))
    import release_publication as checks
    assert artifact["digest"] == "sha256:" + sha(archive)
    assert artifact["expired"] is False
    assert artifact["workflow_run"]["head_sha"] == source
    output = archive.with_suffix("")
    output.mkdir(exist_ok=True)
    with zipfile.ZipFile(archive) as bundle:
        for entry in bundle.infolist():
            name = PurePosixPath(entry.filename)
            assert not name.is_absolute() and ".." not in name.parts
            assert (entry.external_attr >> 16) & 0o170000 != 0o120000
        bundle.extractall(output)
    receipt_path = output / "native-runtime-receipt.json"
    receipt = json.loads(receipt_path.read_bytes())
    identity = json.loads((output / "native-binary-review/native-binary-review.json").read_bytes())
    platform = receipt["platform"]
    name = checks.NATIVE_RECEIPTS[platform]
    binary = output / "native-binary-review/assets" / name
    record = {"sha256": sha(binary), "size": binary.stat().st_size}
    assert identity["sourceSha"] == source
    assert identity["platform"] == platform and identity["assetName"] == name
    assert identity["binarySha256"] == record["sha256"]
    assert identity["binarySize"] == record["size"]
    assert identity["nativeReceiptSha256"] == sha(receipt_path)
    checks._validate_native_receipt(receipt, platform, name, record, source, "v18.19.6")
    checks._validate_robot_xml(output / "output.xml", platform)
    robot_total = ElementTree.parse(output / "output.xml").getroot().find("./statistics/total/stat")
    robot = {key: int(robot_total.attrib.get(key, "0")) for key in ("pass", "fail", "skip")}
    metadata = checks.read_go_build_info(binary)
    goos, goarch = checks.ASSET_TARGETS[name]
    assert metadata["GOOS"] == goos and metadata["GOARCH"] == goarch
    assert metadata["vcs.revision"] == source and metadata["vcs.modified"] == "false"
    assert identity["goBuildInfo"] == {
        "goVersion": "go1.26.9", "goos": goos, "goarch": goarch,
        "vcsRevision": source, "vcsModified": False,
    }
    if platform == "linux_amd64":
        jat = json.loads((output / "jat-class-consumer-receipt.json").read_bytes())
        checks._validate_native_receipt(jat, platform, name, record, source, "v18.19.6", require_robot=False)
    return {"artifactId": artifact["id"], "workflowRunId": artifact["workflow_run"]["id"],
            "sourceSha": source, "platform": platform, "zipSha256": sha(archive),
            "binarySha256": record["sha256"], "binarySize": record["size"],
            "goVersion": "go1.26.9", "nativeLifecycle": "validated", "robot": robot,
            "jat": "validated" if platform == "linux_amd64" else "not applicable"}


if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("archive", type=Path)
    parser.add_argument("artifact", type=Path)
    parser.add_argument("source")
    parser.add_argument("--repository", type=Path, default=Path("/workspace/rcc"))
    args = parser.parse_args()
    print(json.dumps(verify(args.archive, json.loads(args.artifact.read_bytes()), args.source, args.repository), indent=2))
