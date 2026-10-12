"""Verify retained API metadata and actual canonical candidate bytes."""
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


def load(path):
    return json.loads(path.read_bytes())


def extract(archive, output):
    assert not output.exists(), "use a fresh extraction directory"
    with zipfile.ZipFile(archive) as bundle:
        seen = set()
        for entry in bundle.infolist():
            name = PurePosixPath(entry.filename)
            assert not name.is_absolute() and ".." not in name.parts
            assert (entry.external_attr >> 16) & 0o170000 != 0o120000
            assert entry.filename not in seen
            seen.add(entry.filename)
        bundle.extractall(output)


def verify(args):
    sys.path.insert(0, str(args.repository / "scripts"))
    import release_publication as checks
    metadata = checks.validate_preparation_run(
        load(args.run), load(args.jobs)["jobs"], load(args.artifacts)["artifacts"],
        args.source, 186842508, args.run_id)
    assert metadata["digest"] == "sha256:" + sha(args.archive)
    assert metadata["workflow_run"]["head_sha"] == args.source
    assert metadata["size_in_bytes"] == args.archive.stat().st_size
    extract(args.archive, args.output)
    manifest_sha = sha(args.output / "evidence/asset-manifest.json")
    manifest = checks.validate_prepared_bundle(
        args.output, args.source, "v18.19.6", manifest_sha, args.run_id)
    actual_build_info = checks.validate_actual_binary_metadata(args.output, args.source, "go")
    rc_root = args.output / "evidence/release-candidate"
    task = load(next(rc_root.rglob("release-candidate-v1.json")))
    assert task["commitSha"] == args.source
    assert set(task["gates"]) == set(checks.RELEASE_CANDIDATE_GATES)
    assert all(value == "passed" for value in task["gates"].values())
    robot = ElementTree.parse(rc_root / "tmp/output/output.xml").getroot().find("./statistics/total/stat")
    robot_counts = {key: int(robot.attrib.get(key, "0")) for key in ("pass", "fail", "skip")}
    assert robot_counts["fail"] == 0 and robot_counts["pass"] > 0
    native = {}
    for platform in checks.NATIVE_RECEIPTS:
        root = args.output / "evidence/native" / platform
        stat = ElementTree.parse(root / "output.xml").getroot().find("./statistics/total/stat")
        native[platform] = {key: int(stat.attrib.get(key, "0")) for key in ("pass", "fail", "skip")}
    n1_archive = next(rc_root.rglob("rcc-n1-environment.rcca"))
    n1_state = load(next(rc_root.rglob("n1-rollback-state.json")))
    assert n1_state["archiveSha256"] == sha(n1_archive)
    old = load(args.frozen / "evidence/asset-manifest.json")
    comparison = {name: {"oldSha256": old["assets"][name]["sha256"],
                         "newSha256": record["sha256"],
                         "oldSize": old["assets"][name]["size"], "newSize": record["size"],
                         "changed": old["assets"][name]["sha256"] != record["sha256"]}
                  for name, record in manifest["assets"].items()}
    return {"publication": "HOLD", "sourceSha": args.source, "version": "v18.19.6",
            "preparationRunId": args.run_id, "artifactId": metadata["id"],
            "artifactZipSha256": sha(args.archive), "assetManifestSha256": manifest_sha,
            "apiRequiredJobs": "all seven success", "assets": manifest["assets"],
            "actualBinaryBuildInfo": actual_build_info, "releaseCandidateGates": task["gates"],
            "releaseRobot": robot_counts, "nativeRobot": native,
            "linuxJat": "validated by canonical bundle validator",
            "n1ArchiveSha256": sha(n1_archive), "n1RollbackState": n1_state,
            "comparisonWithFrozen7945": comparison}


if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    for name in ("archive", "run", "jobs", "artifacts", "output", "frozen", "repository"):
        parser.add_argument("--" + name, type=Path, required=True)
    parser.add_argument("--source", required=True)
    parser.add_argument("--run-id", type=int, required=True)
    print(json.dumps(verify(parser.parse_args()), indent=2, sort_keys=True))
