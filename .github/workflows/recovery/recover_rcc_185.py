"""One-shot v18.19.5 recovery: verify and publish existing run artifacts only."""
import argparse
import datetime
import hashlib
import io
import json
import os
from pathlib import Path, PurePosixPath
import shutil
import stat
import subprocess
import sys
import tempfile
import zipfile

REPO = "joshyorko/rcc"
TAG = "v18.19.5"
SOURCE_SHA = "d1aec7d0bb897a81274423c7a6bb747233f9c263"
SOURCE_RUN = 34149302701
FAILED_JOB = 101827991898
N1_SHA256 = "7e588c01751ca2ae15ba13ef67f2f4b7567697a5a8389737059a73936f509428"
INDEX_SHA256 = "843f0f2be9c45916549b23da716da86ee8f1c5d9b381dece26821dacf35d64b7"
ARTIFACTS = {
    "RCC-Binaries": (10028800836, "60c354516284c68bed2f877704e162dd730ccba2d280bd024cbb9690f3eb8e3f"),
    "native-runtime-binaries": (10028802297, "16bf53dd48de6671b20cc8c3c1b9b66727e71ff58098840a5e57bffa2dc830fc"),
    "linux-amd64-native-runtime-receipt": (10029036711, "e63081469fe3c1bc37b7b9f69fdd3dd47862fff8858d7f0a968ab0a53f959b9f"),
    "windows-amd64-native-runtime-receipt": (10029231172, "c0d62c042485b6d6241203b212ebaa203406c52716d78b72c167d4afb4d2ee67"),
    "macos-amd64-native-runtime-receipt": (10029150248, "56315c0bf86360e99859708f828eabb959dabcebb0b4475744e95e01d24547d9"),
    "macos-arm64-native-runtime-receipt": (10029041575, "b463e2587f1570695fb52c4f00ae66997ddae15c5d085ceb47d572e31901cdfa"),
    "release-candidate-evidence": (10029497964, "6e824cde3d9d3e4293106499dc3f28aeb7b29e77d7488d7c4df76155d8f9b697"),
}
PLATFORMS = {
    "linux-amd64": ("linux_amd64", "rcc-linux64"),
    "windows-amd64": ("windows_amd64", "rcc-windows64.exe"),
    "macos-amd64": ("darwin_amd64", "rcc-macos64"),
    "macos-arm64": ("darwin_arm64", "rcc-macosarm64"),
}
ROOT = Path(__file__).resolve().parents[3]
sys.path.insert(0, str(ROOT / "scripts"))
import validate_release_topology as topology


def require(condition, message):
    if not condition:
        raise RuntimeError(message)


def gh(*args):
    return subprocess.check_output(["gh", *map(str, args)])


def api(path):
    return json.loads(gh("api", f"repos/{REPO}/{path}"))


def sha(path):
    return hashlib.sha256(Path(path).read_bytes()).hexdigest()


def save(path, data):
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(data, indent=2) + "\n")


def verify_tag():
    ref = api(f"git/ref/tags/{TAG}")["object"]
    require(ref["type"] == "commit" and ref["sha"] == SOURCE_SHA, "release tag target changed")


def verify_run(run, jobs):
    require((run["id"], run["head_sha"], run["head_branch"], run["event"], run["run_attempt"], run["status"], run["conclusion"], run["path"]) ==
            (SOURCE_RUN, SOURCE_SHA, TAG, "push", 1, "completed", "failure", ".github/workflows/rcc.yaml"), "unexpected source run identity or outcome")
    require(run["repository"]["full_name"] == REPO, "wrong source repository")
    expected = {"Build RCC": "success", "Release Candidate Verification": "failure", "Create GitHub Release": "skipped"}
    expected.update({f"Native Runtime ({p})": "success" for p in PLATFORMS})
    require(len(jobs) == len(expected) and {j["name"]: j["conclusion"] for j in jobs} == expected,
            "source run does not have the approved successful build/native jobs")


def unpack_verified(payload, digest, target):
    require(hashlib.sha256(payload).hexdigest() == digest, "downloaded artifact digest mismatch")
    with zipfile.ZipFile(io.BytesIO(payload)) as archive:
        members = archive.infolist()
        require(sum(m.file_size for m in members) <= 1024 ** 3, "artifact exceeds extraction bound")
        names = set()
        for member in members:
            name = PurePosixPath(member.filename)
            kind = stat.S_IFMT(member.external_attr >> 16)
            require(member.filename not in names and not name.is_absolute() and ".." not in name.parts
                    and "\\" not in member.filename and ":" not in member.filename
                    and kind in (0, stat.S_IFREG, stat.S_IFDIR), "unsafe artifact member")
            names.add(member.filename)
        archive.extractall(target)


def verify_native(receipt, platform, binary_hash):
    require(receipt["commitSha"] == SOURCE_SHA and receipt["platform"] == platform, "native receipt source/platform mismatch")
    require(receipt["binary"]["sha256"] == binary_hash and receipt["binary"]["version"] == TAG, "native receipt binary mismatch")
    for label in ("exactBinaryCLI", "sourceAPI"):
        phase = receipt[label]
        require(phase["artifactDigest"] == receipt["artifactDigest"], "artifact receipt identity mismatch")
        # sourceAPI.warm is acquisition-only; execution is proved by exactBinaryCLI.
        for temperature in (("cold", "warm") if label == "exactBinaryCLI" else ("cold",)):
            require(phase[temperature]["exitCode"] == 0 and phase[temperature]["leaseReleased"] is True, "native execution/lease failure")
        require(phase["warm"]["providerDeadWarmReuse"] is True and phase["mismatch"]["rejected"] is True
                and phase["mismatch"]["providerObjectGets"] == 0, "native warm reuse or mismatch proof missing")
    robot = receipt["robotExactBinary"]
    require(robot["binarySha256"] == binary_hash and robot["execute"]["exitCode"] == 0
            and robot["execute"]["leaseReleased"] is True and robot["warm"]["providerDeadWarmReuse"] is True,
            "Robot evidence does not match the tested binary")


def prepare(root):
    verify_tag()
    verify_run(api(f"actions/runs/{SOURCE_RUN}"), api(f"actions/runs/{SOURCE_RUN}/jobs?per_page=100")["jobs"])
    log = gh("api", "--allow-escape-sequences", f"repos/{REPO}/actions/jobs/{FAILED_JOB}/logs")
    require(b"RuntimeError: released N-1 archive rollback was not the expected unsupported CLI error" in log,
            "source failure is not the approved rollback assertion")
    (root / "source-failed-job.log").write_bytes(log)
    for name, (artifact_id, digest) in ARTIFACTS.items():
        meta = api(f"actions/artifacts/{artifact_id}")
        require(meta["name"] == name and not meta["expired"] and meta["digest"] == "sha256:" + digest
                and meta["workflow_run"]["id"] == SOURCE_RUN and meta["workflow_run"]["head_sha"] == SOURCE_SHA,
                "source artifact metadata mismatch: " + name)
        payload = gh("api", f"repos/{REPO}/actions/artifacts/{artifact_id}/zip")
        unpack_verified(payload, digest, root / name)
    assets = root / "RCC-Binaries"
    require({p.name for p in assets.iterdir()} == topology.ASSETS, "unexpected RCC binary inventory")
    hashes = {p.name: sha(p) for p in assets.iterdir()}
    native = root / "native-runtime-binaries"
    sums = dict(line.split()[::-1] for line in (native / "SHA256SUMS").read_text().splitlines())
    require(set(sums) == {asset for _, asset in PLATFORMS.values()}, "native checksum inventory mismatch")
    for name, (platform, asset) in PLATFORMS.items():
        require(sha(native / asset) == sums[asset] == hashes[asset], "tested/release binary mismatch")
        receipt = json.loads((root / f"{name}-native-runtime-receipt/native-runtime-receipt.json").read_text())
        verify_native(receipt, platform, hashes[asset])
    # The Linux JAT result is retained from its successful native job.
    jat = json.loads((root / "linux-amd64-native-runtime-receipt/jat-class-consumer-receipt.json").read_text())
    require(jat["commitSha"] == SOURCE_SHA and jat["binary"]["sha256"] == hashes["rcc-linux64"], "JAT binary/source mismatch")
    gh("release", "download", "v18.19.3", "--repo", REPO, "--pattern", "rcc-linux64", "--pattern", "index.json", "--dir", root / "previous")
    require(sha(root / "previous/rcc-linux64") == N1_SHA256 and sha(root / "previous/index.json") == INDEX_SHA256,
            "pinned previous-release digest mismatch")
    for binary in (assets / "rcc-linux64", root / "previous/rcc-linux64"):
        binary.chmod(0o700)
    require(subprocess.check_output([str(assets / "rcc-linux64"), "version"], text=True).strip() == TAG, "candidate version mismatch")
    save(root / "provenance.json", {"tag": TAG, "sourceSha": SOURCE_SHA, "sourceRun": SOURCE_RUN,
                                  "recoverySha": subprocess.check_output(["git", "rev-parse", "HEAD"], text=True).strip(),
                                  "artifacts": ARTIFACTS, "assets": hashes})
    print("Source run, seven artifact digests, four native receipts and release binaries verified.", flush=True)


def rollback(root):
    sys.path.insert(0, str(ROOT))
    import tasks  # merged #222 validation, used with the original binaries
    proof = root / "rollback"
    proof.mkdir(exist_ok=True)
    producer = proof / "producer"
    consumer = Path(tempfile.mkdtemp(prefix="consumer-", dir=proof))
    project = proof / "project"
    project.mkdir(exist_ok=True)
    fixture = project / "robot.yaml"
    fixture.write_text("tasks:\n  proof:\n    command: [python, -c, \"print('n1-old-task-ok')\"]\ncondaConfigFile: conda.yaml\nartifactsDir: output\n")
    (project / "conda.yaml").write_text("channels:\n- conda-forge\ndependencies:\n- python=3.11.16\n")
    candidate, old = root / "RCC-Binaries/rcc-linux64", root / "previous/rcc-linux64"

    def execute(label, binary, args, home):
        env = {k: v for k, v in os.environ.items() if k not in {"GH_TOKEN", "GITHUB_TOKEN", "HOMEBREW_TOOLS_PAT"}}
        env["ROBOCORP_HOME"] = str(home)
        result = subprocess.run([str(binary), *map(str, args)], env=env, text=True, capture_output=True, timeout=600)
        save(proof / (label + ".json"), {"returncode": result.returncode, "stdout": result.stdout, "stderr": result.stderr})
        require(result.returncode == 0, f"{label} failed; see rollback/{label}.json")
        print(label + ": passed", flush=True)
        return result

    # Only a disposable Python environment is provisioned; RCC is never built.
    provider = "local"
    archive = proof / "fixture.rcca"
    if not archive.is_file():
        published = execute("fixture-publish", old, ["env", "publish", "--robot", fixture, "--provider", provider, "--json"], producer)
        digest = json.loads(published.stdout)["artifactDigest"]
        execute("fixture-export", old, ["env", "export", "--artifact", digest, "--provider", provider, "--output", archive], producer)
    with zipfile.ZipFile(archive) as carrier:
        digest = json.loads(carrier.read("rcc-environment/manifest.json"))["artifactDigest"]
    tasks._install_archive_legacy_closure(consumer, archive)
    execute("old-v12", old, ["holotree", "variables", project / "conda.yaml", "--robot", fixture, "--json"], consumer)
    legacy_before = tasks._legacy_closure_state(consumer, archive)
    archive_hash = sha(archive)
    state_root = consumer / "artifacts/v1"
    args = ["env", "acquire", "--archive", archive, "--trust-carrier", state_root / "trust",
            "--trust-carrier-type", "filesystem", "--permissive-local", "--json"]
    upgraded = json.loads(execute("candidate-import", candidate, args, consumer).stdout)
    # Inspect completes the candidate's normal reconciliation of provisional
    # materialization journals before taking the stable rollback baseline.
    inspected = json.loads(execute("candidate-inspect", candidate, ["env", "lifecycle", "inspect", "--artifact", digest, "--json"], consumer).stdout)
    require(inspected["ready"] is True and inspected["corrupt"] is False and inspected["digest"] == digest,
            "candidate is not ready for rollback")
    legacy_upgraded = tasks._legacy_closure_state(consumer, archive)
    tasks._validate_rebased_legacy_closure(legacy_before, legacy_upgraded, consumer)
    state_before = tasks._state_digests(state_root)
    save(proof / "state-before.json", state_before)
    name = digest.replace(":", "_")
    audits = ("content/.audit", f"verification/{name}.history.jsonl")
    audit_before = {p: (state_root / p).read_bytes() for p in audits}
    result = execute("released-import", old, args, consumer)
    require(tasks._validate_archive_rollback(result, upgraded) == "imported", "N-1 archive import not proven")
    state_after = tasks._state_digests(state_root)
    save(proof / "state-after.json", state_after)
    refreshed = {*audits, f"verification/{name}.json"}
    changed = [k for k in sorted(set(state_before) | set(state_after)) if k not in refreshed and state_before.get(k) != state_after.get(k)]
    require(not changed, "rollback changed immutable artifact state: " + repr(changed))
    require(all((state_root / p).read_bytes().startswith(original) for p, original in audit_before.items()), "rollback rewrote audit history")
    verification = json.loads((state_root / f"verification/{name}.json").read_text())
    require(verification.get("valid") is True and verification.get("artifactDigest") == digest
            and verification.get("policyMode") == "permissive-local", "rollback trust verification failed")
    legacy = execute("released-consumption", old, ["task", "testrun", "--robot", fixture, "--task", "proof", "--no-outputs"], consumer)
    require("n1-old-task-ok" in legacy.stdout + legacy.stderr, "N-1 task did not execute")
    require(tasks._legacy_closure_state(consumer, archive) == legacy_upgraded, "rollback changed legacy closure")
    require(tasks._state_digests(state_root) == state_after and sha(archive) == archive_hash, "rollback changed artifact/archive bytes")
    save(root / "rollback-proof.json", {"status": "passed", "sourceSha": SOURCE_SHA, "sourceRun": SOURCE_RUN,
                                      "candidateSha256": sha(candidate), "n1Sha256": sha(old), "archiveSha256": archive_hash,
                                      "artifactDigest": digest, "archiveRollback": "imported", "legacyConsumption": "passed"})
    print("Corrected N-1 rollback passed using original release binaries.", flush=True)


def stage_index(root):
    assets = root / "RCC-Binaries"
    data = json.loads((root / "previous/index.json").read_text())
    entry = {"version": TAG, "when": datetime.datetime.now(datetime.timezone.utc).strftime("%d.%m.%Y"),
             "changelog": f"https://github.com/{REPO}/blob/{SOURCE_SHA}/docs/changelog.md#v18195-date-07092026"}
    entry.update({key: f"https://github.com/{REPO}/releases/download/{TAG}/{asset}" for key, asset in topology.INDEX_ASSETS.items()})
    data["tested"] = [entry] + [e for e in data["tested"] if e["version"] != TAG][:19]
    save(assets / "index.json", data)
    errors = topology.validate(None, ROOT / ".github/workflows/rcc.yaml", assets / "index.json", assets)
    require(not errors, str(errors))


def publish(root):
    verify_tag()
    assets = root / "RCC-Binaries"
    provenance = json.loads((root / "provenance.json").read_text())
    proof = json.loads((root / "rollback-proof.json").read_text())
    require(provenance["sourceSha"] == SOURCE_SHA and provenance["sourceRun"] == SOURCE_RUN, "provenance changed")
    require(proof["status"] == "passed" and proof["sourceSha"] == SOURCE_SHA
            and proof["candidateSha256"] == provenance["assets"]["rcc-linux64"] and proof["n1Sha256"] == N1_SHA256,
            "rollback proof does not bind the existing binary")
    for name, digest in provenance["assets"].items():
        require(sha(assets / name) == digest, "release payload changed: " + name)
    stage_index(root)
    expected = {p.name: sha(p) for p in assets.iterdir()}
    existing = subprocess.run(["gh", "release", "view", TAG, "--repo", REPO, "--json", "isDraft"], capture_output=True, text=True)
    require(existing.returncode != 0 and "release not found" in existing.stderr.lower(), "release already exists or cannot be inspected; refusing overwrite")
    notes = root / "release-notes.md"
    notes.write_text(f"RCC {TAG}. Published from verified artifacts of [run {SOURCE_RUN}](https://github.com/{REPO}/actions/runs/{SOURCE_RUN}) at `{SOURCE_SHA}`.\n\nIncludes merged #219, #220 and #221. The corrected #222 rollback gate was verified with these original binaries.\n")
    gh("release", "create", TAG, "--repo", REPO, "--verify-tag", "--target", SOURCE_SHA,
       "--title", TAG, "--draft", "--notes-file", notes, *sorted(assets.iterdir()))
    download = root / "draft-readback"
    gh("release", "download", TAG, "--repo", REPO, "--dir", download)
    require({p.name: sha(p) for p in download.iterdir()} == expected, "uploaded draft assets differ from verified payload")
    verify_tag()
    gh("release", "edit", TAG, "--repo", REPO, "--draft=false", "--latest")
    public = api(f"releases/tags/{TAG}")
    require(not public["draft"] and not public["prerelease"] and
            {a["name"]: a["digest"] for a in public["assets"]} == {n: "sha256:" + h for n, h in expected.items()}, "public release asset verification failed")
    save(root / "publication.json", {"url": public["html_url"], "tag": TAG, "sourceSha": SOURCE_SHA, "sourceRun": SOURCE_RUN, "assets": expected})
    print(public["html_url"], flush=True)


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("phase", choices=("prepare", "rollback", "publish"))
    parser.add_argument("root", type=Path)
    args = parser.parse_args()
    args.root = args.root.resolve()
    args.root.mkdir(parents=True, exist_ok=True)
    globals()[args.phase](args.root)
