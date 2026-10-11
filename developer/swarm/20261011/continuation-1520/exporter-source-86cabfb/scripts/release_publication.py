#!/usr/bin/env python3
"""Fail-closed preparation and publication checks for RCC release artifacts."""

import argparse
import hashlib
import json
import os
import re
import shutil
import stat
import subprocess
import sys
import tempfile
import urllib.error
import urllib.parse
import urllib.request
import zipfile
from pathlib import Path
from xml.etree import ElementTree

from validate_release_topology import validate as validate_release_topology


ASSET_NAMES = (
    "rcc-linux64",
    "rccremote-linux64",
    "rcc-windows64.exe",
    "rccremote-windows64.exe",
    "rcc-macos64",
    "rccremote-macos64",
    "rcc-macosarm64",
    "rccremote-macosarm64",
    "index.json",
)
NATIVE_RECEIPTS = {
    "linux_amd64": "rcc-linux64",
    "darwin_amd64": "rcc-macos64",
    "darwin_arm64": "rcc-macosarm64",
    "windows_amd64": "rcc-windows64.exe",
}
NATIVE_JOB_NAMES = {
    "linux_amd64": "Native Runtime (linux-amd64)",
    "darwin_amd64": "Native Runtime (macos-amd64)",
    "darwin_arm64": "Native Runtime (macos-arm64)",
    "windows_amd64": "Native Runtime (windows-amd64)",
}
REQUIRED_NATIVE_JOBS = tuple(NATIVE_JOB_NAMES.values())
REQUIRED_JOBS = (
    "Build RCC",
    "Release Candidate Verification",
    *REQUIRED_NATIVE_JOBS,
    "Assemble Release Preparation",
)
ASSET_TARGETS = {
    "rcc-linux64": ("linux", "amd64"),
    "rccremote-linux64": ("linux", "amd64"),
    "rcc-windows64.exe": ("windows", "amd64"),
    "rccremote-windows64.exe": ("windows", "amd64"),
    "rcc-macos64": ("darwin", "amd64"),
    "rccremote-macos64": ("darwin", "amd64"),
    "rcc-macosarm64": ("darwin", "arm64"),
    "rccremote-macosarm64": ("darwin", "arm64"),
}
EXPECTED_GO_VERSION = "go1.26.9"
MAX_REVIEW_ARTIFACT_BYTES = 24 * 1024 * 1024
REVIEW_EXPORT_WORKFLOW_PATH = ".github/workflows/export-release-evidence.yml"
SHA_RE = re.compile(r"^[0-9a-f]{40}$")
VERSION_RE = re.compile(r"^v[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z.-]+)?$")
RELEASE_CANDIDATE_GATES = (
    "artifactFocused",
    "artifactRace",
    "artifactVertical",
    "artifactConsumerVertical",
    "artifactRobot",
    "binaryInventory",
    "largeStream",
    "robot",
    "selfHost",
    "goVet",
    "coordinationAcceptance",
)
REVIEW_RELEASE_CANDIDATE_FILES = (
    "release-candidate-exit.json",
    "release-candidate-v1.json",
    "self-host-v1.json",
    "self-host-metadata-v1.json",
    "coordination-blackbox-v1.json",
    "large-stream-receipt.json",
)
REVIEW_N1_FILES = (
    "n1-archive-sha256.txt",
    "candidate-archive-upgrade.json",
    "released-archive-rollback-attempt.json",
    "released-v12-rollback-consumption.json",
    "n1-rollback-state.json",
)
REVIEW_NATIVE_FILES = (
    "native-runtime-receipt.json",
    "native-runtime-robot-evidence.json",
    "output.xml",
)
REVIEW_EVIDENCE_PATHS = tuple(sorted((
    "evidence/asset-manifest.json",
    "evidence/SHA256SUMS",
    "evidence/provenance.json",
    *(f"evidence/release-candidate/{name}" for name in REVIEW_RELEASE_CANDIDATE_FILES),
    "evidence/release-candidate/robot/output.xml",
    *(f"evidence/release-candidate/n1/{name}" for name in REVIEW_N1_FILES),
    *(f"evidence/native/{platform}/{name}" for platform in NATIVE_RECEIPTS for name in REVIEW_NATIVE_FILES),
    "evidence/native/linux_amd64/jat-class-consumer-receipt.json",
)))


class ReleaseGateError(ValueError):
    """A release precondition was absent or did not match the approved input."""


class _ArtifactRedirectHandler(urllib.request.HTTPRedirectHandler):
    """Do not forward GitHub API credentials to the artifact storage origin."""

    def redirect_request(self, request, fp, code, message, headers, newurl):
        old = urllib.parse.urlsplit(request.full_url)
        new = urllib.parse.urlsplit(newurl)
        if old.scheme != "https" or new.scheme != "https":
            raise ReleaseGateError("preparation artifact redirect must remain HTTPS")
        redirected = super().redirect_request(request, fp, code, message, headers, newurl)
        if redirected is not None and (old.scheme, old.netloc) != (new.scheme, new.netloc):
            for header_set in (redirected.headers, redirected.unredirected_hdrs):
                for key in list(header_set):
                    if key.casefold() == "authorization":
                        del header_set[key]
        return redirected


def _read_json(path):
    try:
        value = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError) as error:
        raise ReleaseGateError(f"cannot read JSON evidence {path}: {error}") from error
    if not isinstance(value, dict):
        raise ReleaseGateError(f"JSON evidence is not an object: {path}")
    return value


def _require(condition, message):
    if not condition:
        raise ReleaseGateError(message)


def _sha256(path):
    digest = hashlib.sha256()
    with path.open("rb") as stream:
        for block in iter(lambda: stream.read(1024 * 1024), b""):
            digest.update(block)
    return digest.hexdigest()


def read_go_build_info(path, go_binary="go"):
    path = Path(path)
    try:
        output = subprocess.check_output([go_binary, "version", "-m", str(path)], text=True, stderr=subprocess.STDOUT)
    except (OSError, subprocess.CalledProcessError) as error:
        raise ReleaseGateError(f"cannot inspect Go build metadata for {path}: {error}") from error
    lines = output.splitlines()
    version_tokens = lines[0].split() if lines else []
    _require(EXPECTED_GO_VERSION in version_tokens,
             f"binary Go compiler version is not exactly {EXPECTED_GO_VERSION}: {path.name}")
    settings = {}
    for line in lines[1:]:
        fields = line.strip().split(None, 1)
        if len(fields) == 2 and fields[0] == "build":
            key, separator, value = fields[1].partition("=")
            if separator:
                settings[key] = value
    return settings


def _is_zero_exit_code(value):
    return type(value) is int and value == 0


def validate_asset_build_info(assets_dir, source_sha, go_binary="go"):
    assets_dir = Path(assets_dir)
    records = {}
    for name in ASSET_TARGETS:
        path = assets_dir / name
        _require(path.is_file() and not path.is_symlink(), f"release asset is missing or unsafe: {name}")
        _require(name in ASSET_TARGETS, f"cannot infer build target for asset: {name}")
        goos, goarch = ASSET_TARGETS[name]
        info = read_go_build_info(path, go_binary)
        _require(info.get("GOOS") == goos and info.get("GOARCH") == goarch, f"binary target metadata mismatch: {name}")
        _require(info.get("vcs.revision") == source_sha, f"binary source revision mismatch: {name}")
        _require(info.get("vcs.modified") == "false", f"binary has uncommitted build state: {name}")
        records[name] = {
            "goVersion": EXPECTED_GO_VERSION,
            "goos": goos,
            "goarch": goarch,
            "vcsRevision": source_sha,
            "vcsModified": False,
        }
    return records


def validate_inputs(source_sha, version):
    _require(isinstance(source_sha, str) and SHA_RE.fullmatch(source_sha) is not None, "invalid source SHA")
    _require(isinstance(version, str) and VERSION_RE.fullmatch(version) is not None, "invalid release version")


def validate_environment_protection(environment):
    _require(isinstance(environment, dict), "release environment configuration is unreadable")
    _require(environment.get("can_admins_bypass") is False, "release environment permits admin bypass or bypass policy is unreadable")
    rules = environment.get("protection_rules")
    _require(isinstance(rules, list), "release environment protection rules are unreadable")
    reviewer_rules = [rule for rule in rules if isinstance(rule, dict) and rule.get("type") == "required_reviewers"]
    _require(len(reviewer_rules) == 1, "release environment must have exactly one required reviewers rule")
    rule = reviewer_rules[0]
    reviewers = rule.get("reviewers")
    _require(isinstance(reviewers, list) and bool(reviewers), "release environment has no required reviewers")
    _require(rule.get("prevent_self_review") is True, "release environment does not prevent self-review")
    return True


def validate_source_checkout(source_root, expected_source_sha, expected_version, workflow_relative_path=".github/workflows/rcc.yaml"):
    validate_inputs(expected_source_sha, expected_version)
    source_root = Path(source_root)
    try:
        actual_source_sha = subprocess.check_output(
            ["git", "rev-parse", "HEAD"], cwd=source_root, text=True, stderr=subprocess.PIPE
        ).strip()
    except (OSError, subprocess.CalledProcessError) as error:
        raise ReleaseGateError(f"source checkout identity is unreadable: {error}") from error
    _require(actual_source_sha == expected_source_sha, "source checkout SHA differs from the approved source SHA")
    version_source = source_root / "common" / "version.go"
    _require(version_source.is_file() and not version_source.is_symlink(), "source version file is missing or unsafe")
    version_text = version_source.read_text(encoding="utf-8")
    match = re.search(r"(?m)^\s*(?:const\s+)?Version\s*=\s*`([^`]+)`", version_text)
    _require(match is not None and match.group(1) == expected_version, "source version differs from the approved version")
    changelog_path = source_root / "docs" / "changelog.md"
    _require(changelog_path.is_file() and not changelog_path.is_symlink(), "source changelog is missing or unsafe")
    changelog = changelog_path.read_text(encoding="utf-8")
    _require(re.search(rf"(?m)^## {re.escape(expected_version)}(?:\s|$)", changelog) is not None,
             "source changelog has no heading for the approved version")
    workflow_path = source_root / workflow_relative_path
    _require(workflow_path.is_file() and not workflow_path.is_symlink(), "source RCC workflow is missing or unsafe")
    workflow_text = workflow_path.read_text(encoding="utf-8")
    trigger_section = workflow_text.split("\njobs:", 1)[0]
    job_section = workflow_text.split("\njobs:", 1)[1] if "\njobs:" in workflow_text else ""
    _require(not re.search(r"(?m)^\s+tags:", trigger_section),
             "source RCC workflow still starts automatically from tag pushes")
    _require(not re.search(r"(?m)^  release:\s*$", job_section),
             "source RCC workflow still has an automatic release job")
    forbidden = (
        "softprops/action-gh-release",
        "gh release create",
        "gh release edit",
        "github.rest.repos.createRelease",
        "refs/tags/",
    )
    for marker in forbidden:
        _require(marker not in job_section, f"source RCC workflow contains an automatic release operation: {marker}")
    tag_path = source_root / ".github" / "workflows" / "create-release-tag.yml"
    _require(tag_path.is_file() and not tag_path.is_symlink(), "source legacy tag workflow is missing or unsafe")
    tag_text = tag_path.read_text(encoding="utf-8")
    _require("workflow_dispatch:" in tag_text and "contents: read" in tag_text,
             "source legacy tag workflow is not a read-only manual hold")
    for marker in ("push:", "contents: write", "git push", "gh release", "refs/tags/"):
        _require(marker not in tag_text, f"source legacy tag workflow can mutate release state: {marker}")
    recovery_path = source_root / ".github" / "workflows" / "recover-rcc-185.yml"
    _require(recovery_path.is_file() and not recovery_path.is_symlink(),
             "source historical recovery workflow is missing or unsafe")
    recovery_text = recovery_path.read_text(encoding="utf-8")
    _require("workflow_dispatch:" in recovery_text and "contents: read" in recovery_text,
             "source historical recovery workflow is not a read-only manual hold")
    for marker in (
        "push:", "pull_request:", "contents: write", "actions: write", "git push", "gh api", "gh release",
        "github.rest", "createRelease", "uploadReleaseAsset", "repository_dispatch", "workflow run",
        "refs/tags/", "HOMEBREW_TOOLS_PAT", "GITHUB_TOKEN", "GH_TOKEN", "github.token", "recover_rcc_185.py",
    ):
        _require(marker not in recovery_text,
                 f"source historical recovery workflow can still mutate or publish release state: {marker}")
    _require("exit 1" in recovery_text and "retired" in recovery_text.lower(),
             "source historical recovery workflow does not explicitly refuse publication")
    publisher_path = source_root / ".github" / "workflows" / "publish-release.yml"
    _require(publisher_path.is_file() and not publisher_path.is_symlink(), "source manual publisher workflow is missing or unsafe")
    publisher_text = publisher_path.read_text(encoding="utf-8")
    for marker in (
        "workflow_dispatch:", "name: rcc-production", "verify-environment", "verify-run",
        "validate-bundle", "git merge-base --is-ancestor", "gh release create",
    ):
        _require(marker in publisher_text, f"source manual publisher is missing required protection: {marker}")
    publisher_triggers = publisher_text.split("permissions:", 1)[0]
    _require(not re.search(r"(?m)^\s+tags:", publisher_triggers),
             "source manual publisher can start automatically from tags")
    return True


def validate_preparation_run(run, jobs, artifacts, expected_source_sha, expected_workflow_id, expected_run_id):
    validate_inputs(expected_source_sha, "v0.0.0")
    _require(isinstance(run, dict), "preparation run metadata is unreadable")
    _require(run.get("event") == "workflow_dispatch", "preparation must be a manual workflow_dispatch run")
    _require(run.get("head_branch") == "main", "preparation run must be dispatched from main")
    _require(run.get("head_sha") == expected_source_sha, "preparation source SHA differs from the approved source SHA")
    _require(run.get("workflow_id") == expected_workflow_id, "preparation run belongs to an unexpected workflow")
    _require(run.get("id") == expected_run_id, "preparation run ID differs from the requested run")
    _require(run.get("conclusion") == "success", "preparation run did not complete successfully")
    _require(isinstance(jobs, list), "preparation job results are unreadable")
    by_name = {job.get("name"): job for job in jobs if isinstance(job, dict)}
    for name in REQUIRED_JOBS:
        job = by_name.get(name)
        _require(job is not None, f"required native job or preparation job is missing: {name}")
        _require(job.get("conclusion") == "success", f"required preparation job did not succeed: {name}")
    if isinstance(artifacts, dict):
        artifacts = [artifacts]
    _require(isinstance(artifacts, list), "preparation artifacts are unreadable")
    matches = [artifact for artifact in artifacts if isinstance(artifact, dict) and artifact.get("name") == "rcc-release-preparation"]
    _require(len(matches) == 1, "preparation must contain exactly one release artifact")
    _require(matches[0].get("expired") is False, "release preparation artifact is expired or its expiry is unreadable")
    _require(isinstance(matches[0].get("digest"), str) and matches[0]["digest"].startswith("sha256:"),
             "release preparation artifact ZIP digest is missing")
    return matches[0]


def validate_prepared_bundle(root, expected_source_sha, expected_version, expected_manifest_sha, expected_run_id=None):
    validate_inputs(expected_source_sha, expected_version)
    _require(isinstance(expected_manifest_sha, str) and re.fullmatch(r"[0-9a-f]{64}", expected_manifest_sha) is not None,
             "invalid expected asset manifest SHA-256")
    root = Path(root)
    _require(root.is_dir() and not root.is_symlink(), "release artifact root is missing or unsafe")
    for path in [root, *root.rglob("*")]:
        _require(not path.is_symlink(), f"release artifact contains a symlink: {path.relative_to(root) if path != root else '.'}")
    _require({path.name for path in root.iterdir()} == {"assets", "evidence"},
             "release artifact has unexpected top-level paths")
    assets_dir = root / "assets"
    evidence_dir = root / "evidence"
    manifest_path = evidence_dir / "asset-manifest.json"
    _require(assets_dir.is_dir() and not assets_dir.is_symlink(), "release assets directory is missing or unsafe")
    _require(evidence_dir.is_dir() and not evidence_dir.is_symlink(), "release evidence directory is missing or unsafe")
    manifest_bytes = manifest_path.read_bytes() if manifest_path.is_file() and not manifest_path.is_symlink() else b""
    actual_manifest_sha = hashlib.sha256(manifest_bytes).hexdigest()
    _require(actual_manifest_sha == expected_manifest_sha, "asset manifest SHA-256 differs from the approved value")
    manifest = _read_json(manifest_path)
    _require(manifest.get("schemaVersion") == 1, "unsupported asset manifest schema")
    _require(manifest.get("sourceSha") == expected_source_sha, "manifest source SHA differs from the approved source SHA")
    _require(manifest.get("version") == expected_version, "manifest version differs from the approved version")
    _require(type(manifest.get("preparationRunId")) is int and manifest["preparationRunId"] > 0,
             "manifest preparation run ID is invalid")
    if expected_run_id is not None:
        _require(manifest["preparationRunId"] == expected_run_id, "manifest preparation run ID differs from the requested run")

    children = list(assets_dir.iterdir())
    actual_names = {path.name for path in children}
    _require(actual_names == set(ASSET_NAMES), f"release asset set mismatch: {sorted(actual_names)}")
    assets = manifest.get("assets")
    _require(isinstance(assets, dict) and set(assets) == set(ASSET_NAMES), "manifest asset set mismatch")
    for name in ASSET_NAMES:
        path = assets_dir / name
        _require(not path.is_symlink() and path.is_file(), f"release asset is missing or unsafe: {name}")
        recorded = assets[name]
        _require(isinstance(recorded, dict), f"manifest record is malformed: {name}")
        actual_size = path.stat().st_size
        actual_sha = _sha256(path)
        _require(recorded.get("size") == actual_size, f"release asset size mismatch: {name}")
        _require(recorded.get("sha256") == actual_sha, f"release asset checksum mismatch: {name}")

    index_errors = validate_release_topology(
        None, None, assets_dir / "index.json", asset_root=assets_dir, expected_version=expected_version
    )
    _require(not index_errors, "release index semantics are invalid: " + "; ".join(index_errors))

    sums_path = evidence_dir / "SHA256SUMS"
    _require(sums_path.is_file() and not sums_path.is_symlink(), "asset checksum file is missing or unsafe")
    expected_sums = {f"assets/{name}": assets[name]["sha256"] for name in ASSET_NAMES}
    parsed_sums = {}
    for line in sums_path.read_text(encoding="utf-8").splitlines():
        parts = line.split(maxsplit=1)
        _require(len(parts) == 2 and re.fullmatch(r"[0-9a-f]{64}", parts[0]) is not None,
                 "asset checksum file contains a malformed line")
        _require(parts[1] not in parsed_sums, "asset checksum file has a duplicate path")
        parsed_sums[parts[1]] = parts[0]
    _require(parsed_sums == expected_sums, "asset checksum file differs from the manifest")

    provenance = _read_json(evidence_dir / "provenance.json")
    _require(provenance.get("schemaVersion") == 1, "unsupported release provenance schema")
    _require(provenance.get("sourceSha") == expected_source_sha, "provenance source SHA mismatch")
    _require(provenance.get("version") == expected_version, "provenance version mismatch")
    _require(provenance.get("preparationRunId") == manifest["preparationRunId"], "provenance preparation run ID mismatch")
    _require(provenance.get("assetManifestSha256") == expected_manifest_sha, "provenance manifest hash mismatch")
    _require(set(provenance.get("nativePlatforms", [])) == set(NATIVE_RECEIPTS),
             "provenance native platform set mismatch")
    build_info = provenance.get("binaryBuildInfo")
    _require(isinstance(build_info, dict) and set(build_info) == set(ASSET_TARGETS), "binary compiler provenance asset set mismatch")
    for name, (goos, goarch) in ASSET_TARGETS.items():
        info = build_info[name]
        _require(isinstance(info, dict), f"binary compiler provenance is malformed: {name}")
        _require(info.get("goVersion") == EXPECTED_GO_VERSION, f"binary compiler version provenance mismatch: {name}")
        _require(info.get("goos") == goos and info.get("goarch") == goarch, f"binary architecture provenance mismatch: {name}")
        _require(info.get("vcsRevision") == expected_source_sha and info.get("vcsModified") is False,
                 f"binary source provenance mismatch: {name}")

    exit_receipts = list((evidence_dir / "release-candidate").rglob("release-candidate-exit.json"))
    _require(len(exit_receipts) == 1, "release-candidate gate receipt is missing or ambiguous")
    gate = _read_json(exit_receipts[0])
    _require(gate.get("sourceSha") == expected_source_sha, "release-candidate source SHA mismatch")
    _require(_is_zero_exit_code(gate.get("gateExitCode")) and _is_zero_exit_code(gate.get("teeExitCode")),
             "release-candidate gate or tee did not have numeric zero exit status")

    native_root = evidence_dir / "native"
    receipts = list(native_root.rglob("native-runtime-receipt.json"))
    _require(len(receipts) == len(NATIVE_RECEIPTS), "native runtime receipt count is not exactly four")
    seen_platforms = set()
    for receipt_path in receipts:
        receipt = _read_json(receipt_path)
        platform = receipt.get("platform")
        _require(platform in NATIVE_RECEIPTS and platform not in seen_platforms, "native receipt platform is missing, duplicate, or unexpected")
        seen_platforms.add(platform)
        asset_name = NATIVE_RECEIPTS[platform]
        _validate_native_receipt(receipt, platform, asset_name, assets[asset_name], expected_source_sha, expected_version)
        xml_reports = list(receipt_path.parent.rglob("output.xml"))
        _require(len(xml_reports) == 1, f"native Robot XML report is missing or ambiguous: {platform}")
        _validate_robot_xml(xml_reports[0], platform)
    jat_receipts = list(native_root.rglob("jat-class-consumer-receipt.json"))
    _require(len(jat_receipts) == 1, "Linux JAT consumer receipt is missing or ambiguous")
    _require(jat_receipts[0].is_file() and not jat_receipts[0].is_symlink(), "Linux JAT consumer receipt is unsafe")
    jat = _read_json(jat_receipts[0])
    _validate_native_receipt(jat, "linux_amd64", NATIVE_RECEIPTS["linux_amd64"],
                             assets[NATIVE_RECEIPTS["linux_amd64"]], expected_source_sha, expected_version,
                             require_robot=False)
    _require(jat.get("exactBinaryCLI", {}).get("cold", {}).get("nativeImport") == "sqlite3",
             "Linux JAT receipt does not prove the native SQLite consumer import")
    return manifest


def check_review_payload_size(root, max_bytes=None):
    root = Path(root)
    max_bytes = MAX_REVIEW_ARTIFACT_BYTES if max_bytes is None else max_bytes
    _require(root.is_dir() and not root.is_symlink(), f"review artifact payload root is missing or unsafe: {root}")
    total = 0
    for path in root.rglob("*"):
        mode = path.lstat().st_mode
        _require(not stat.S_ISLNK(mode), f"review artifact payload contains a symlink: {path.relative_to(root)}")
        _require(stat.S_ISDIR(mode) or stat.S_ISREG(mode),
                 f"review artifact payload contains a nonregular entry: {path.relative_to(root)}")
        if stat.S_ISREG(mode):
            total += path.stat().st_size
    _require(type(max_bytes) is int and max_bytes > 0, "review artifact size limit is invalid")
    _require(total <= max_bytes,
             f"review artifact payload exceeds {max_bytes} raw bytes: {total}")
    return total


def _parse_asset_checksums(path, expected_assets):
    parsed = {}
    try:
        lines = path.read_text(encoding="utf-8").splitlines()
    except OSError as error:
        raise ReleaseGateError(f"cannot read asset checksum file {path}: {error}") from error
    for line in lines:
        parts = line.split(maxsplit=1)
        _require(len(parts) == 2 and re.fullmatch(r"[0-9a-f]{64}", parts[0]) is not None,
                 "asset checksum file contains a malformed line")
        _require(parts[1] not in parsed, "asset checksum file has a duplicate path")
        parsed[parts[1]] = parts[0]
    _require(set(parsed) == {f"assets/{name}" for name in expected_assets},
             "asset checksum file has a missing or unexpected path")
    return parsed


def validate_review_assets(root, expected_source_sha, expected_version, expected_run_id, expected_manifest_sha):
    validate_inputs(expected_source_sha, expected_version)
    _require(type(expected_run_id) is int and expected_run_id > 0, "invalid review artifact preparation run ID")
    _require(isinstance(expected_manifest_sha, str) and re.fullmatch(r"[0-9a-f]{64}", expected_manifest_sha) is not None,
             "invalid review artifact manifest SHA-256")
    root = Path(root)
    _require(root.is_dir() and not root.is_symlink(), "review artifact directory is missing or unsafe")
    asset_dirs = list(root.iterdir())
    _require({path.name for path in asset_dirs} == set(ASSET_NAMES),
             f"review artifact asset directory set mismatch: {sorted(path.name for path in asset_dirs)}")

    expected_manifest_bytes = None
    for name in ASSET_NAMES:
        artifact_dir = root / name
        _require(artifact_dir.is_dir() and not artifact_dir.is_symlink(), f"review artifact is missing or unsafe: {name}")
        check_review_payload_size(artifact_dir)
        expected_files = {
            f"assets/{name}",
            "evidence/asset-manifest.json",
            "evidence/SHA256SUMS",
            "evidence/provenance.json",
            "review-identity.json",
        }
        actual_files = {
            str(path.relative_to(artifact_dir)).replace(os.sep, "/")
            for path in artifact_dir.rglob("*") if path.is_file()
        }
        actual_directories = {
            str(path.relative_to(artifact_dir)).replace(os.sep, "/")
            for path in artifact_dir.rglob("*") if path.is_dir()
        }
        _require(actual_files == expected_files,
                 f"review artifact file set mismatch for {name}: {sorted(actual_files)}")
        _require(actual_directories == {"assets", "evidence"},
                 f"review artifact directory layout mismatch for {name}: {sorted(actual_directories)}")
        manifest_path = artifact_dir / "evidence" / "asset-manifest.json"
        manifest_bytes = manifest_path.read_bytes()
        if expected_manifest_bytes is None:
            expected_manifest_bytes = manifest_bytes
        _require(manifest_bytes == expected_manifest_bytes,
                 f"review artifact manifest differs across assets: {name}")
        _require(hashlib.sha256(manifest_bytes).hexdigest() == expected_manifest_sha,
                 f"review artifact manifest SHA-256 mismatch: {name}")
        manifest = _read_json(manifest_path)
        _require(manifest.get("schemaVersion") == 1 and manifest.get("sourceSha") == expected_source_sha
                 and manifest.get("version") == expected_version
                 and manifest.get("preparationRunId") == expected_run_id,
                 f"review artifact manifest identity mismatch: {name}")
        records = manifest.get("assets")
        _require(isinstance(records, dict) and set(records) == set(ASSET_NAMES),
                 "review artifact manifest asset set mismatch")
        sums = _parse_asset_checksums(artifact_dir / "evidence" / "SHA256SUMS", ASSET_NAMES)
        _require(sums == {f"assets/{key}": value.get("sha256") for key, value in records.items()},
                 f"review artifact checksum file differs from manifest: {name}")
        provenance = _read_json(artifact_dir / "evidence" / "provenance.json")
        _require(provenance.get("schemaVersion") == 1
                 and provenance.get("sourceSha") == expected_source_sha
                 and provenance.get("version") == expected_version
                 and provenance.get("preparationRunId") == expected_run_id
                 and provenance.get("assetManifestSha256") == expected_manifest_sha,
                 f"review artifact provenance identity mismatch: {name}")
        identity = _read_json(artifact_dir / "review-identity.json")
        record = records[name]
        asset_path = artifact_dir / "assets" / name
        _require(asset_path.is_file() and not asset_path.is_symlink(), f"review asset is missing or unsafe: {name}")
        actual_sha = _sha256(asset_path)
        actual_size = asset_path.stat().st_size
        _require(record.get("sha256") == actual_sha and record.get("size") == actual_size,
                 f"review asset bytes differ from manifest: {name}")
        _require(sums[f"assets/{name}"] == actual_sha, f"review asset checksum mismatch: {name}")
        expected_identity = {
            "schemaVersion": 1,
            "sourceSha": expected_source_sha,
            "version": expected_version,
            "preparationRunId": expected_run_id,
            "assetManifestSha256": expected_manifest_sha,
            "assetName": name,
            "assetSha256": actual_sha,
            "assetSize": actual_size,
        }
        _require(identity == expected_identity, f"review artifact identity mismatch: {name}")
    return True


def validate_exporter_source_checkout(root, exporter_sha, preparation_source_sha):
    """Require the exporter to run from main history containing the exact prepared source."""
    validate_inputs(preparation_source_sha, "v0.0.0")
    _require(isinstance(exporter_sha, str) and SHA_RE.fullmatch(exporter_sha) is not None,
             "invalid evidence exporter source SHA")
    try:
        actual = subprocess.check_output(
            ["git", "rev-parse", "HEAD"], cwd=root, text=True, stderr=subprocess.PIPE
        ).strip()
        ancestor = subprocess.run(
            ["git", "merge-base", "--is-ancestor", preparation_source_sha, exporter_sha],
            cwd=root, text=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
        )
    except (OSError, subprocess.CalledProcessError) as error:
        raise ReleaseGateError(f"cannot validate evidence exporter source history: {error}") from error
    _require(actual == exporter_sha, "evidence exporter checkout differs from its workflow SHA")
    _require(ancestor.returncode == 0,
             "prepared source is not an ancestor of the evidence exporter main commit")
    return True


def _unique_evidence_file(root, filename, description):
    matches = [path for path in Path(root).rglob(filename) if path.name == filename]
    _require(len(matches) == 1, f"{description} is missing or ambiguous: {filename}")
    path = matches[0]
    _require(path.is_file() and not path.is_symlink(), f"{description} is unsafe: {filename}")
    return path


def _review_evidence_sources(root):
    root = Path(root)
    evidence = root / "evidence"
    sources = {
        "evidence/asset-manifest.json": evidence / "asset-manifest.json",
        "evidence/SHA256SUMS": evidence / "SHA256SUMS",
        "evidence/provenance.json": evidence / "provenance.json",
    }
    release = evidence / "release-candidate"
    for name in REVIEW_RELEASE_CANDIDATE_FILES:
        sources[f"evidence/release-candidate/{name}"] = _unique_evidence_file(
            release, name, "release-candidate evidence file"
        )
    robot_xml = release / "tmp" / "output" / "output.xml"
    _require(robot_xml.is_file() and not robot_xml.is_symlink(),
             "release-candidate Robot XML is missing or unsafe")
    sources["evidence/release-candidate/robot/output.xml"] = robot_xml
    for name in REVIEW_N1_FILES:
        sources[f"evidence/release-candidate/n1/{name}"] = _unique_evidence_file(
            release, name, "N-1 archive evidence file"
        )
    native_root = evidence / "native"
    for platform in NATIVE_RECEIPTS:
        platform_root = native_root / platform
        _require(platform_root.is_dir() and not platform_root.is_symlink(),
                 f"native platform evidence is missing or unsafe: {platform}")
        for name in REVIEW_NATIVE_FILES:
            path = platform_root / name
            _require(path.is_file() and not path.is_symlink(),
                     f"native review evidence is missing or unsafe: {platform}/{name}")
            sources[f"evidence/native/{platform}/{name}"] = path
    jat = native_root / "linux_amd64" / "jat-class-consumer-receipt.json"
    _require(jat.is_file() and not jat.is_symlink(), "Linux JAT consumer receipt is missing or unsafe")
    sources["evidence/native/linux_amd64/jat-class-consumer-receipt.json"] = jat
    _require(set(sources) == set(REVIEW_EVIDENCE_PATHS), "internal review evidence allowlist is inconsistent")
    for relative, path in sources.items():
        _require(path.is_file() and not path.is_symlink(), f"review evidence is missing or unsafe: {relative}")
    return sources


def _review_file_inventory(root):
    root = Path(root)
    records = []
    for relative in REVIEW_EVIDENCE_PATHS:
        path = root / relative
        _require(path.is_file() and not path.is_symlink(), f"review evidence is missing or unsafe: {relative}")
        records.append({"path": relative, "sha256": _sha256(path), "size": path.stat().st_size})
    return records


def validate_review_evidence(root, expected_source_sha, expected_version, expected_run_id,
                             expected_artifact_id, expected_exporter_run_id, expected_manifest_sha,
                             expected_full_artifact_digest,
                             expected_exporter_sha, max_bytes=None):
    """Validate a bounded, evidence-only sidecar against its canonical identity."""
    validate_inputs(expected_source_sha, expected_version)
    _require(type(expected_run_id) is int and expected_run_id > 0, "invalid review preparation run ID")
    _require(type(expected_artifact_id) is int and expected_artifact_id > 0,
             "invalid review preparation artifact ID")
    _require(type(expected_exporter_run_id) is int and expected_exporter_run_id > 0,
             "invalid evidence exporter workflow run ID")
    _require(isinstance(expected_manifest_sha, str)
             and re.fullmatch(r"[0-9a-f]{64}", expected_manifest_sha) is not None,
             "invalid review asset manifest SHA-256")
    _require(isinstance(expected_full_artifact_digest, str)
             and re.fullmatch(r"sha256:[0-9a-f]{64}", expected_full_artifact_digest) is not None,
             "invalid full preparation artifact ZIP digest")
    _require(isinstance(expected_exporter_sha, str) and SHA_RE.fullmatch(expected_exporter_sha) is not None,
             "invalid evidence exporter source SHA")
    root = Path(root)
    _require(root.is_dir() and not root.is_symlink(), "review evidence root is missing or unsafe")
    actual = set()
    directories = set()
    for path in root.rglob("*"):
        relative = str(path.relative_to(root)).replace(os.sep, "/")
        mode = path.lstat().st_mode
        _require(not stat.S_ISLNK(mode), f"review evidence contains a symlink: {relative}")
        if stat.S_ISDIR(mode):
            directories.add(relative)
        elif stat.S_ISREG(mode):
            actual.add(relative)
        else:
            raise ReleaseGateError(f"review evidence contains a nonregular entry: {relative}")
    expected_files = {*REVIEW_EVIDENCE_PATHS, "review-identity.json"}
    _require(actual == expected_files,
             f"review evidence file set mismatch: missing={sorted(expected_files - actual)} extra={sorted(actual - expected_files)}")
    expected_directories = {str(Path(name).parent).replace(os.sep, "/")
                            for name in REVIEW_EVIDENCE_PATHS if Path(name).parent != Path(".")}
    expected_directories.update({"evidence", "evidence/release-candidate", "evidence/release-candidate/robot",
                                 "evidence/release-candidate/n1", "evidence/native"})
    expected_directories.update(f"evidence/native/{platform}" for platform in NATIVE_RECEIPTS)
    _require(directories == expected_directories,
             f"review evidence directory layout mismatch: {sorted(directories)}")
    check_review_payload_size(root, max_bytes=max_bytes)
    identity = _read_json(root / "review-identity.json")
    expected_identity = {
        "schemaVersion": 1,
        "preparationRunId": expected_run_id,
        "preparationArtifactId": expected_artifact_id,
        "preparationWorkflowPath": ".github/workflows/rcc.yaml",
        "preparationArtifactZipSha256": expected_full_artifact_digest,
        "sourceSha": expected_source_sha,
        "version": expected_version,
        "assetManifestSha256": expected_manifest_sha,
        "evidenceExporterWorkflowPath": REVIEW_EXPORT_WORKFLOW_PATH,
        "evidenceExporterRunId": expected_exporter_run_id,
        "evidenceExporterSourceSha": expected_exporter_sha,
        "files": _review_file_inventory(root),
    }
    _require(type(expected_identity["preparationArtifactId"]) is int
             and expected_identity["preparationArtifactId"] > 0,
             "review identity has invalid preparation artifact ID")
    _require(identity == expected_identity, "review evidence identity or file hash inventory mismatch")

    manifest_path = root / "evidence" / "asset-manifest.json"
    _require(_sha256(manifest_path) == expected_manifest_sha, "review asset manifest SHA differs from requested value")
    manifest = _read_json(manifest_path)
    _require(manifest.get("schemaVersion") == 1 and manifest.get("sourceSha") == expected_source_sha
             and manifest.get("version") == expected_version
             and manifest.get("preparationRunId") == expected_run_id,
             "review asset manifest identity mismatch")
    assets = manifest.get("assets")
    _require(isinstance(assets, dict) and set(assets) == set(ASSET_NAMES),
             "review asset manifest asset set mismatch")
    provenance = _read_json(root / "evidence" / "provenance.json")
    _require(provenance.get("schemaVersion") == 1 and provenance.get("sourceSha") == expected_source_sha
             and provenance.get("version") == expected_version
             and provenance.get("preparationRunId") == expected_run_id
             and provenance.get("assetManifestSha256") == expected_manifest_sha,
             "review provenance identity mismatch")
    checksums = _parse_asset_checksums(root / "evidence" / "SHA256SUMS", ASSET_NAMES)
    _require(checksums == {f"assets/{name}": record.get("sha256") for name, record in assets.items()},
             "review checksum file differs from the manifest")

    gate_exit = _read_json(root / "evidence/release-candidate/release-candidate-exit.json")
    _require(gate_exit.get("sourceSha") == expected_source_sha
             and _is_zero_exit_code(gate_exit.get("gateExitCode"))
             and _is_zero_exit_code(gate_exit.get("teeExitCode")),
             "review release-candidate gate did not pass with numeric zero exit status")
    gate_receipt = _read_json(root / "evidence/release-candidate/release-candidate-v1.json")
    _require(gate_receipt.get("schemaVersion") == 1 and gate_receipt.get("commitSha") == expected_source_sha,
             "review release-candidate receipt source identity mismatch")
    gates = gate_receipt.get("gates")
    _require(isinstance(gates, dict) and set(gates) == set(RELEASE_CANDIDATE_GATES)
             and all(gates[name] == "passed" for name in RELEASE_CANDIDATE_GATES),
             "review release-candidate receipt is missing a passing required gate")
    _validate_robot_xml(root / "evidence/release-candidate/robot/output.xml", "release-candidate")
    release_evidence = root / "evidence/release-candidate"
    self_host = _read_json(release_evidence / "self-host-v1.json")
    _require(self_host.get("schemaVersion") == 1 and self_host.get("commitSha") == expected_source_sha,
             "review self-host receipt source identity mismatch")
    _require(isinstance(self_host.get("homes"), dict) and set(self_host["homes"]) == {"a", "b"}
             and isinstance(self_host.get("commands"), list) and isinstance(self_host.get("binaries"), dict)
             and {"released", "generationA", "candidate", "generationB"}.issubset(self_host["binaries"]),
             "review self-host lifecycle receipt is incomplete")
    binaries = self_host["binaries"]
    released, candidate_binary, generation_a, generation_b = (
        binaries[name] for name in ("released", "candidate", "generationA", "generationB")
    )
    _require(isinstance(released, dict) and isinstance(released.get("version"), str)
             and isinstance(released.get("sha256"), str)
             and isinstance(candidate_binary, dict) and candidate_binary.get("version") == expected_version
             and isinstance(candidate_binary.get("sha256"), str)
             and isinstance(generation_a, dict) and generation_a.get("sourceSha256") == candidate_binary.get("sha256")
             and generation_a.get("candidateSha256") == candidate_binary.get("sha256")
             and isinstance(generation_b, dict) and generation_b.get("version") == expected_version
             and isinstance(generation_b.get("sha256"), str),
             "review self-host binary generations are not bound to the candidate version and bytes")
    _require(_read_json(release_evidence / "self-host-metadata-v1.json") == {"schemaVersion": 1},
             "review self-host home-b metadata fixture is invalid")
    coordination = _read_json(release_evidence / "coordination-blackbox-v1.json")
    required_scenarios = {"cli-claim", "cli-wait", "cli-heartbeat", "cli-release", "cli-prewarm",
                          "cli-loopback-rejection"}
    _require(coordination.get("schemaVersion") == 1 and coordination.get("commitSha") == expected_source_sha
             and isinstance(coordination.get("binarySha256"), str)
             and re.fullmatch(r"[0-9a-f]{64}", coordination["binarySha256"]) is not None
             and isinstance(coordination.get("scenarios"), dict)
             and set(coordination["scenarios"]) == required_scenarios,
             "review coordination receipt is incomplete or source-mismatched")
    stream = _read_json(release_evidence / "large-stream-receipt.json")
    _require(stream.get("schemaVersion") == 1 and stream.get("commitSha") == expected_source_sha
             and type(stream.get("bytes")) is int and stream["bytes"] >= (2 << 30)
             and stream.get("bufferBytes") == 64 * 1024
             and type(stream.get("requests")) is int and stream["requests"] == 1
             and stream.get("restartPolicy") == "full-restart"
             and stream.get("interruptionAcceptance") ==
             "TestHTTPInterruptedDownloadFailsVerificationThenFullRestartSucceeds",
             "review large-stream receipt is incomplete or source-mismatched")
    archive_hash_text = (release_evidence / "n1" / "n1-archive-sha256.txt").read_text(encoding="ascii").strip()
    _require(re.fullmatch(r"[0-9a-f]{64}", archive_hash_text) is not None,
             "review N-1 archive SHA-256 receipt is malformed")
    candidate = _read_json(release_evidence / "n1" / "candidate-archive-upgrade.json")
    rollback_attempt = _read_json(release_evidence / "n1" / "released-archive-rollback-attempt.json")
    legacy_consumption = _read_json(release_evidence / "n1" / "released-v12-rollback-consumption.json")
    rollback_state = _read_json(release_evidence / "n1" / "n1-rollback-state.json")
    _require(isinstance(candidate.get("artifactDigest"), str) and candidate.get("materializationId"),
             "review candidate archive acquisition receipt lacks artifact identity")
    _require(rollback_state.get("archiveSha256") == archive_hash_text
             and rollback_state.get("artifactDigest") == candidate["artifactDigest"]
             and rollback_state.get("materializationId") == candidate["materializationId"],
             "review N-1 archive receipt identity differs across upgrade and rollback")
    _require(rollback_state.get("archiveRollback") in {"imported", "unsupported"},
             "review N-1 archive rollback result is unsupported")
    _require(type(legacy_consumption.get("returncode")) is int and legacy_consumption["returncode"] == 0
             and "n1-old-task-ok" in (legacy_consumption.get("stdout", "") + legacy_consumption.get("stderr", "")),
             "review N-1 released binary did not consume the legacy task")
    _require(rollback_state.get("legacyBefore") == rollback_state.get("legacyAfterUpgrade")
             == rollback_state.get("legacyAfterRollback"),
             "review N-1 rollback changed the legacy v12 closure")
    _require(rollback_state.get("artifactAfterArchiveRollback") == rollback_state.get("artifactAfterRollback"),
             "review N-1 rollback state changed after legacy consumption")
    if rollback_state["archiveRollback"] == "imported":
        try:
            old_result = json.loads(rollback_attempt.get("stdout", ""))
        except (TypeError, json.JSONDecodeError) as error:
            raise ReleaseGateError("review N-1 released-binary rollback output is not valid JSON") from error
        _require(rollback_attempt.get("returncode") == 0
                 and old_result.get("artifactDigest") == candidate["artifactDigest"]
                 and old_result.get("materializationId") == candidate["materializationId"],
                 "review N-1 released-binary rollback did not consume the candidate artifact")
    else:
        error_output = (rollback_attempt.get("stdout", "") + "\n" + rollback_attempt.get("stderr", "")).lower()
        _require(type(rollback_attempt.get("returncode")) is int and rollback_attempt["returncode"] != 0
                 and (("unknown flag" in error_output and "archive" in error_output)
                      or ("unknown command" in error_output and ("archive" in error_output or '"env"' in error_output))),
                 "review N-1 unsupported rollback does not match the accepted old-CLI failure")
    for platform, asset_name in NATIVE_RECEIPTS.items():
        platform_root = root / "evidence/native" / platform
        receipt_path = platform_root / "native-runtime-receipt.json"
        receipt = _read_json(receipt_path)
        _validate_native_receipt(receipt, platform, asset_name, assets[asset_name], expected_source_sha, expected_version)
        _validate_robot_xml(platform_root / "output.xml", platform)
    jat_path = root / "evidence/native/linux_amd64/jat-class-consumer-receipt.json"
    jat = _read_json(jat_path)
    _validate_native_receipt(jat, "linux_amd64", NATIVE_RECEIPTS["linux_amd64"],
                             assets[NATIVE_RECEIPTS["linux_amd64"]], expected_source_sha,
                             expected_version, require_robot=False)
    _require(jat.get("exactBinaryCLI", {}).get("cold", {}).get("nativeImport") == "sqlite3",
             "review Linux JAT receipt does not prove the native SQLite import")
    return True


def stage_review_evidence(root, output, source_sha, version, run_id, manifest_sha,
                          full_artifact, exporter_sha, exporter_run_id, max_bytes=None, go_binary="go"):
    """Copy only bounded source-bound receipts from the already verified full artifact."""
    validate_prepared_bundle(root, source_sha, version, manifest_sha, run_id)
    validate_actual_binary_metadata(root, source_sha, go_binary)
    _require(isinstance(full_artifact, dict), "full preparation artifact metadata is unreadable")
    artifact_id = full_artifact.get("id")
    artifact_digest = full_artifact.get("digest")
    _require(type(artifact_id) is int and artifact_id > 0, "full preparation artifact ID is invalid")
    _require(isinstance(artifact_digest, str) and re.fullmatch(r"sha256:[0-9a-f]{64}", artifact_digest) is not None,
             "full preparation artifact ZIP digest is invalid")
    sources = _review_evidence_sources(root)
    limit = MAX_REVIEW_ARTIFACT_BYTES if max_bytes is None else max_bytes
    _require(type(limit) is int and limit > 0, "review artifact size limit is invalid")
    source_size = sum(path.stat().st_size for path in sources.values())
    _require(source_size <= limit,
             f"review artifact payload exceeds {limit} raw bytes: {source_size}")
    output = Path(output)
    _require(not output.exists(), f"review evidence output already exists: {output}")
    output.parent.mkdir(parents=True, exist_ok=True)
    temporary = Path(tempfile.mkdtemp(prefix=".rcc-review-evidence-", dir=output.parent))
    try:
        for relative, source in sources.items():
            destination = temporary / relative
            destination.parent.mkdir(parents=True, exist_ok=True)
            shutil.copyfile(source, destination)
        manifest_sha = _sha256(temporary / "evidence/asset-manifest.json")
        identity = {
            "schemaVersion": 1,
            "preparationRunId": run_id,
            "preparationArtifactId": artifact_id,
            "preparationWorkflowPath": ".github/workflows/rcc.yaml",
            "preparationArtifactZipSha256": artifact_digest,
            "sourceSha": source_sha,
            "version": version,
            "assetManifestSha256": manifest_sha,
            "evidenceExporterWorkflowPath": REVIEW_EXPORT_WORKFLOW_PATH,
            "evidenceExporterRunId": exporter_run_id,
            "evidenceExporterSourceSha": exporter_sha,
            "files": _review_file_inventory(temporary),
        }
        (temporary / "review-identity.json").write_text(
            json.dumps(identity, sort_keys=True, indent=2) + "\n", encoding="utf-8"
        )
        total_size = check_review_payload_size(temporary, max_bytes=max_bytes)
        validate_review_evidence(temporary, source_sha, version, run_id, artifact_id,
                                 exporter_run_id, manifest_sha,
                                 artifact_digest, exporter_sha, max_bytes=max_bytes)
        os.replace(temporary, output)
        return {"fileCount": len(sources), "rawBytes": total_size, "assetManifestSha256": manifest_sha,
                "preparationArtifactId": artifact_id, "preparationArtifactZipSha256": artifact_digest,
                "sourceSha": source_sha, "version": version, "preparationRunId": run_id,
                "evidenceExporterRunId": exporter_run_id}
    finally:
        if temporary.exists():
            shutil.rmtree(temporary)


def export_preparation_review(api_url, token, repository, run_id, source_sha, version, manifest_sha,
                              exporter_root, exporter_sha, exporter_run_id, source_root, zip_path, bundle_root,
                              output_root, max_bytes=None, go_binary="go"):
    """Download and validate an existing successful preparation, then export small evidence only."""
    validate_exporter_source_checkout(exporter_root, exporter_sha, source_sha)
    validate_source_checkout(source_root, source_sha, version)
    artifact = verify_preparation_api(api_url, token, repository, run_id, source_sha, ".github/workflows/rcc.yaml")
    _require(artifact.get("name") == "rcc-release-preparation", "preparation artifact has an unexpected name")
    actual_digest = download_preparation_artifact(api_url, token, repository, artifact, zip_path, bundle_root)
    _require(actual_digest == artifact.get("digest"), "downloaded full preparation digest differs from the GitHub API value")
    return stage_review_evidence(bundle_root, output_root, source_sha, version, run_id, manifest_sha,
                                 artifact, exporter_sha, exporter_run_id, max_bytes=max_bytes, go_binary=go_binary)


def stage_review_assets(root, output, expected_source_sha, expected_version, expected_run_id,
                        expected_manifest_sha, go_binary="go"):
    root = Path(root)
    output = Path(output)
    validate_prepared_bundle(root, expected_source_sha, expected_version, expected_manifest_sha, expected_run_id)
    validate_actual_binary_metadata(root, expected_source_sha, go_binary)
    _require(not output.exists(), f"review artifact output already exists: {output}")
    output.parent.mkdir(parents=True, exist_ok=True)
    temporary = Path(tempfile.mkdtemp(prefix=".rcc-review-assets-", dir=output.parent))
    try:
        manifest_bytes = (root / "evidence" / "asset-manifest.json").read_bytes()
        sums_bytes = (root / "evidence" / "SHA256SUMS").read_bytes()
        provenance_bytes = (root / "evidence" / "provenance.json").read_bytes()
        manifest = _read_json(root / "evidence" / "asset-manifest.json")
        for name in ASSET_NAMES:
            artifact_dir = temporary / name
            asset_dir = artifact_dir / "assets"
            evidence_dir = artifact_dir / "evidence"
            asset_dir.mkdir(parents=True)
            evidence_dir.mkdir()
            source = root / "assets" / name
            destination = asset_dir / name
            shutil.copyfile(source, destination)
            (evidence_dir / "asset-manifest.json").write_bytes(manifest_bytes)
            (evidence_dir / "SHA256SUMS").write_bytes(sums_bytes)
            (evidence_dir / "provenance.json").write_bytes(provenance_bytes)
            record = manifest["assets"][name]
            identity = {
                "schemaVersion": 1,
                "sourceSha": expected_source_sha,
                "version": expected_version,
                "preparationRunId": expected_run_id,
                "assetManifestSha256": expected_manifest_sha,
                "assetName": name,
                "assetSha256": record["sha256"],
                "assetSize": record["size"],
            }
            (artifact_dir / "review-identity.json").write_text(
                json.dumps(identity, sort_keys=True, separators=(",", ":")) + "\n", encoding="utf-8"
            )
            check_review_payload_size(artifact_dir)
        validate_review_assets(temporary, expected_source_sha, expected_version, expected_run_id, expected_manifest_sha)
        os.replace(temporary, output)
        return {name: check_review_payload_size(output / name) for name in ASSET_NAMES}
    finally:
        if temporary.exists():
            shutil.rmtree(temporary)


def stage_native_review_binary(binary_path, receipt_path, output, asset_name, platform,
                               source_sha, version, go_binary="go"):
    validate_inputs(source_sha, version)
    _require(asset_name in ASSET_TARGETS and NATIVE_RECEIPTS.get(platform) == asset_name,
             "native review asset/platform mapping is invalid")
    binary_path = Path(binary_path)
    receipt_path = Path(receipt_path)
    output = Path(output)
    _require(binary_path.is_file() and not binary_path.is_symlink(), "native review binary is missing or unsafe")
    _require(receipt_path.is_file() and not receipt_path.is_symlink(), "native runtime receipt is missing or unsafe")
    receipt = _read_json(receipt_path)
    goos, goarch = ASSET_TARGETS[asset_name]
    receipt_binary = receipt.get("binary")
    _require(receipt.get("commitSha") == source_sha and receipt.get("platform") == platform,
             "native review receipt source/platform identity mismatch")
    _require(isinstance(receipt_binary, dict)
             and receipt_binary.get("sha256") == _sha256(binary_path)
             and receipt_binary.get("version") == version
             and receipt_binary.get("runtimeGOOS") == goos
             and receipt_binary.get("runtimeGOARCH") == goarch,
             "native review binary differs from the executed-binary receipt")
    build_info = read_go_build_info(binary_path, go_binary)
    expected_build_info = {
        "GOOS": goos,
        "GOARCH": goarch,
        "vcs.revision": source_sha,
        "vcs.modified": "false",
    }
    metadata_mismatches = {
        key: {"expected": expected, "actual": build_info.get(key)}
        for key, expected in expected_build_info.items()
        if build_info.get(key) != expected
    }
    _require(not metadata_mismatches,
             "native review binary Go metadata mismatch: " +
             json.dumps(metadata_mismatches, sort_keys=True, separators=(",", ":")))
    _require(not output.exists(), f"native review binary output already exists: {output}")
    output.parent.mkdir(parents=True, exist_ok=True)
    temporary = Path(tempfile.mkdtemp(prefix=".rcc-native-review-", dir=output.parent))
    try:
        staged_asset_dir = temporary / "assets"
        staged_asset_dir.mkdir()
        staged_binary = staged_asset_dir / asset_name
        shutil.copyfile(binary_path, staged_binary)
        metadata = {
            "schemaVersion": 1,
            "sourceSha": source_sha,
            "platform": platform,
            "assetName": asset_name,
            "version": version,
            "binarySha256": _sha256(staged_binary),
            "binarySize": staged_binary.stat().st_size,
            "nativeReceiptSha256": _sha256(receipt_path),
            "goBuildInfo": {
                "goVersion": EXPECTED_GO_VERSION,
                "goos": goos,
                "goarch": goarch,
                "vcsRevision": source_sha,
                "vcsModified": False,
            },
        }
        (temporary / "native-binary-review.json").write_text(
            json.dumps(metadata, sort_keys=True, indent=2) + "\n", encoding="utf-8"
        )
        check_review_payload_size(temporary)
        os.replace(temporary, output)
        return metadata
    finally:
        if temporary.exists():
            shutil.rmtree(temporary)


def _validate_native_receipt(receipt, platform, asset_name, asset_record, source_sha, version, require_robot=True):
    _require(receipt.get("schemaVersion") == 1, f"native receipt schema is unsupported: {platform}")
    _require(receipt.get("commitSha") == source_sha, f"native receipt source SHA mismatch: {platform}")
    binary = receipt.get("binary")
    goos, goarch = ASSET_TARGETS[asset_name]
    _require(isinstance(binary, dict) and binary.get("version") == version,
             f"native binary version mismatch: {platform}")
    _require(binary.get("sha256") == asset_record["sha256"], f"native binary checksum differs from release asset: {platform}")
    _require(binary.get("runtimeGOOS") == goos and binary.get("runtimeGOARCH") == goarch and
             binary.get("expectedGOOS") == goos and binary.get("expectedGOARCH") == goarch,
             f"native binary platform mismatch: {platform}")
    artifact_digest = receipt.get("artifactDigest")
    _require(isinstance(artifact_digest, str) and artifact_digest.startswith("sha256:"),
             f"native artifact digest is missing: {platform}")
    _require(receipt.get("exactBinaryCLI", {}).get("artifactDigest") == artifact_digest,
             f"native CLI artifact digest differs from receipt: {platform}")
    _require(receipt.get("exactBinaryCLI", {}).get("objectCount", 0) > 0,
             f"native CLI proof has no provider objects: {platform}")
    _validate_consumer_lifecycle(receipt.get("exactBinaryCLI"), artifact_digest, platform)
    _validate_consumer_lifecycle(receipt.get("sourceAPI"), artifact_digest, platform)
    if not require_robot:
        return
    robot = receipt.get("robotExactBinary")
    robot_digest = robot.get("artifactDigest") if isinstance(robot, dict) else None
    _require(isinstance(robot, dict) and robot.get("binarySha256") == binary["sha256"] and
             robot.get("binaryVersion") == version and isinstance(robot_digest, str) and robot_digest.startswith("sha256:"),
             f"native Robot proof binary or artifact identity mismatch: {platform}")
    _require(all(robot.get(phase, {}).get("artifactDigest") == robot_digest for phase in ("cold", "execute", "warm")),
             f"native Robot artifact identity changed between phases: {platform}")
    _require(robot.get("cold", {}).get("cacheHit") == "provider" and
             robot.get("execute", {}).get("cacheHit") == "local-materialization" and
             robot.get("warm", {}).get("cacheHit") == "local-materialization",
             f"native Robot provider/cached lifecycle is incomplete: {platform}")
    _require(robot.get("cold", {}).get("materializationId") == robot.get("execute", {}).get("materializationId") ==
             robot.get("warm", {}).get("materializationId") and
             robot.get("cold", {}).get("path") == robot.get("execute", {}).get("path") ==
             robot.get("warm", {}).get("path") and bool(robot.get("cold", {}).get("path")),
             f"native Robot materialization changed: {platform}")
    _require(robot.get("execute", {}).get("leaseId") and robot.get("execute", {}).get("leaseReleased") is True and
             robot.get("execute", {}).get("exitCode") == 0, f"native Robot execution/lease proof failed: {platform}")
    _require(robot.get("warm", {}).get("providerDeadWarmReuse") is True,
             f"native Robot provider-dead warm proof failed: {platform}")
    _require(robot.get("native", {}).get("import") == "sqlite3" and
             robot.get("native", {}).get("extension") and robot.get("native", {}).get("sqliteVersion"),
             f"native Robot SQLite proof is incomplete: {platform}")
    checks = receipt.get("exactBinaryCLI", {}).get("platformChecks")
    _require(isinstance(checks, dict) and bool(checks), f"native platform probes are missing: {platform}")
    expected_checks = {
        "linux": {"runnerOS", "machine", "caseCollision", "executableSymlink", "filesystemType", "libc", "glibcMuslMismatch"},
        "darwin": {"runnerOS", "machine", "caseCollision", "executableSymlink", "rosetta", "machoRPath", "machoDependencies", "quarantineXattr", "codeSigning", "translationAllowed", "nativeOnlyOnRosetta"},
        "windows": {"runnerOS", "machine", "caseCollision", "executableSymlink", "longPath", "junction", "fileLock", "pathNormalization"},
    }[goos]
    _require(set(checks) == expected_checks, f"native platform probe key set is invalid: {platform}")
    _require(checks.get("caseCollision") in {"case-sensitive-filesystem", "case-insensitive-filesystem"},
             f"native case-sensitivity probe is invalid: {platform}")
    for key, value in checks.items():
        allowed_rosetta_skip = goos == "darwin" and key == "nativeOnlyOnRosetta" and value == "skipped:native-runner"
        _require(isinstance(value, str) and bool(value) and
                 (allowed_rosetta_skip or not value.startswith(("skipped:", "unavailable:", "failed"))),
                 f"native platform probe is not a measured result ({key}): {platform}")
    if platform.startswith("darwin_"):
        _require(checks.get("nativeOnlyOnRosetta") in {"true", "false", "skipped:native-runner"},
                 f"native Rosetta probe is not an explicit result: {platform}")


def _validate_consumer_lifecycle(evidence, artifact_digest, label):
    _require(isinstance(evidence, dict) and evidence.get("artifactDigest") == artifact_digest,
             f"consumer lifecycle artifact mismatch: {label}")
    cold, warm, mismatch = evidence.get("cold"), evidence.get("warm"), evidence.get("mismatch")
    _require(isinstance(cold, dict) and isinstance(warm, dict) and isinstance(mismatch, dict),
             f"consumer lifecycle receipt is incomplete: {label}")
    _require(cold.get("artifactDigest") == artifact_digest and warm.get("artifactDigest") == artifact_digest and
             cold.get("materializationId") == warm.get("materializationId") and cold.get("path") == warm.get("path"),
             f"consumer lifecycle changed artifact materialization: {label}")
    _require(cold.get("cacheHit") == "provider" and warm.get("cacheHit") == "local-materialization" and
             cold.get("exitCode") == 0 and cold.get("leaseId") and cold.get("leaseReleased") is True,
             f"consumer provider/lease proof failed: {label}")
    _require(warm.get("providerDeadWarmReuse") is True, f"consumer provider-dead reuse proof failed: {label}")
    _require(mismatch.get("rejected") is True and mismatch.get("providerObjectGets") == 0,
             f"consumer CPU mismatch fetched provider objects or was accepted: {label}")
    _require(cold.get("nativeImport") == "sqlite3" and cold.get("nativeExtension") and cold.get("sqliteVersion"),
             f"consumer native Python SQLite proof is incomplete: {label}")
    compatibility = cold.get("compatibility")
    worker = compatibility.get("worker") if isinstance(compatibility, dict) else None
    _require(isinstance(compatibility, dict) and compatibility.get("status") == "compatible" and isinstance(worker, dict),
             f"consumer compatibility receipt is missing: {label}")
    _require(worker.get("python", {}).get("artifactProvided") is True and
             worker.get("os", {}).get("nativeArchitecture") and worker.get("cpu", {}).get("architecture") and
             worker.get("filesystem", {}).get("maxPath", 0) > 0 and worker.get("relocationVersions"),
             f"consumer worker capability receipt is incomplete: {label}")


def _validate_robot_xml(path, platform):
    try:
        root = ElementTree.parse(path).getroot()
    except (OSError, ElementTree.ParseError) as error:
        raise ReleaseGateError(f"native Robot XML is unreadable: {platform}: {error}") from error
    total = root.find("./statistics/total/stat")
    _require(total is not None and total.attrib.get("fail") == "0" and
             int(total.attrib.get("pass", "0")) >= 1,
             f"native Robot suite did not pass: {platform}")


def validate_actual_binary_metadata(root, source_sha, go_binary="go"):
    root = Path(root)
    actual = validate_asset_build_info(root / "assets", source_sha, go_binary)
    provenance = _read_json(root / "evidence" / "provenance.json")
    _require(actual == provenance.get("binaryBuildInfo"),
             "actual binary Go metadata differs from recorded compiler provenance")
    return actual


def assemble_prepared_bundle(asset_root, release_candidate_root, native_root, output_root,
                             source_sha, version, preparation_run_id, go_binary="go"):
    validate_inputs(source_sha, version)
    _require(isinstance(preparation_run_id, int) and preparation_run_id > 0, "invalid preparation run ID")
    asset_root = Path(asset_root)
    release_candidate_root = Path(release_candidate_root)
    native_root = Path(native_root)
    output_root = Path(output_root)
    _require(not output_root.exists(), "output directory already exists")
    _require(asset_root.is_dir() and not asset_root.is_symlink(), "build asset directory is missing or unsafe")
    _require({path.name for path in asset_root.iterdir()} == set(ASSET_NAMES), "build asset directory must contain exactly nine assets")
    _require(release_candidate_root.is_dir() and not release_candidate_root.is_symlink(), "release-candidate evidence is missing or unsafe")
    _require(native_root.is_dir() and not native_root.is_symlink(), "native receipt artifacts are missing or unsafe")

    output_assets = output_root / "assets"
    evidence = output_root / "evidence"
    output_assets.mkdir(parents=True)
    (evidence / "release-candidate").mkdir(parents=True)
    (evidence / "native").mkdir()
    for name in ASSET_NAMES:
        source = asset_root / name
        _require(source.is_file() and not source.is_symlink(), f"build asset is missing or unsafe: {name}")
        shutil.copyfile(source, output_assets / name)
    shutil.copytree(release_candidate_root, evidence / "release-candidate", dirs_exist_ok=True, symlinks=True)
    for platform in NATIVE_RECEIPTS:
        matches = list(native_root.rglob("native-runtime-receipt.json"))
        matching = []
        for receipt_path in matches:
            try:
                receipt = _read_json(receipt_path)
            except ReleaseGateError:
                continue
            if receipt.get("platform") == platform:
                matching.append(receipt_path.parent)
        _require(len(matching) == 1, f"expected one native receipt artifact for {platform}")
        source_dir = matching[0]
        _require(source_dir.is_dir() and not source_dir.is_symlink(), f"native evidence directory is unsafe: {platform}")
        shutil.copytree(source_dir, evidence / "native" / platform, symlinks=True)

    # Verify before hashing: this rejects symlinked or mismatched release evidence.
    exit_receipts = list((evidence / "release-candidate").rglob("release-candidate-exit.json"))
    _require(len(exit_receipts) == 1, "release-candidate exit receipt is missing or ambiguous")
    gate = _read_json(exit_receipts[0])
    _require(gate.get("sourceSha") == source_sha and _is_zero_exit_code(gate.get("gateExitCode"))
             and _is_zero_exit_code(gate.get("teeExitCode")),
             "release-candidate gate did not pass with numeric zero exit status for the requested source")

    records = {}
    for name in ASSET_NAMES:
        path = output_assets / name
        records[name] = {"sha256": _sha256(path), "size": path.stat().st_size}
    build_info = validate_asset_build_info(output_assets, source_sha, go_binary)
    manifest = {
        "schemaVersion": 1,
        "sourceSha": source_sha,
        "version": version,
        "preparationRunId": preparation_run_id,
        "assets": records,
    }
    manifest_path = evidence / "asset-manifest.json"
    manifest_bytes = (json.dumps(manifest, sort_keys=True, separators=(",", ":")) + "\n").encode()
    manifest_path.write_bytes(manifest_bytes)
    manifest_sha = hashlib.sha256(manifest_bytes).hexdigest()
    (evidence / "SHA256SUMS").write_text(
        "".join(f"{records[name]['sha256']}  assets/{name}\n" for name in sorted(ASSET_NAMES)),
        encoding="utf-8",
    )
    provenance = {
        "schemaVersion": 1,
        "sourceSha": source_sha,
        "version": version,
        "preparationRunId": preparation_run_id,
        "assetManifestSha256": manifest_sha,
        "releaseCandidateExitCode": 0,
        "nativePlatforms": sorted(NATIVE_RECEIPTS),
        "binaryBuildInfo": build_info,
    }
    (evidence / "provenance.json").write_text(json.dumps(provenance, sort_keys=True, indent=2) + "\n", encoding="utf-8")
    validate_prepared_bundle(output_root, source_sha, version, manifest_sha, preparation_run_id)
    return manifest_sha


def _api_get(api_url, token):
    request = urllib.request.Request(api_url, headers={
        "Accept": "application/vnd.github+json",
        "Authorization": f"Bearer {token}",
        "X-GitHub-Api-Version": "2022-11-28",
    })
    try:
        with urllib.request.urlopen(request, timeout=20) as response:
            payload = json.loads(response.read())
    except (urllib.error.URLError, TimeoutError, json.JSONDecodeError) as error:
        raise ReleaseGateError(f"required GitHub API data is unreadable: {error}") from error
    if not isinstance(payload, dict):
        raise ReleaseGateError("required GitHub API response is not an object")
    return payload


def verify_release_absent(api_url, token, repository, version):
    validate_inputs("a" * 40, version)
    _require(bool(token), "GitHub release preflight token is unavailable")
    repo_api = f"{api_url.rstrip('/')}/repos/{repository}"
    for path, label in ((f"git/ref/tags/{version}", "tag"), (f"releases/tags/{version}", "release")):
        request = urllib.request.Request(f"{repo_api}/{path}", headers={
            "Accept": "application/vnd.github+json",
            "Authorization": f"Bearer {token}",
            "X-GitHub-Api-Version": "2022-11-28",
        })
        try:
            with urllib.request.urlopen(request, timeout=20):
                raise ReleaseGateError(f"version {label} already exists; automatic replacement is prohibited")
        except urllib.error.HTTPError as error:
            if error.code != 404:
                raise ReleaseGateError(f"version {label} state is unreadable (HTTP {error.code})") from error
        except (urllib.error.URLError, TimeoutError) as error:
            raise ReleaseGateError(f"version {label} state is unreadable: {error}") from error
    return True


def verify_downstream_workflow(api_url, token, owner):
    _require(bool(token), "Homebrew dispatch token is unavailable")
    _require(isinstance(owner, str) and re.fullmatch(r"[A-Za-z0-9-]+", owner) is not None,
             "Homebrew repository owner is unavailable or invalid")
    workflow = _api_get(
        f"{api_url.rstrip('/')}/repos/{owner}/homebrew-tools/actions/workflows/tap-auto-update.yml",
        token,
    )
    _require(isinstance(workflow, dict) and workflow.get("state") == "active",
             "Homebrew update workflow is missing or inactive")
    return True


def verify_preparation_api(api_url, token, repository, run_id, source_sha, expected_workflow_path):
    validate_inputs(source_sha, "v0.0.0")
    _require(bool(token), "GitHub API token is unavailable")
    repo_api = f"{api_url.rstrip('/')}/repos/{repository}"
    run = _api_get(f"{repo_api}/actions/runs/{run_id}", token)
    workflow_id = run.get("workflow_id")
    _require(isinstance(workflow_id, int), "preparation workflow identity is unreadable")
    workflow = _api_get(f"{repo_api}/actions/workflows/{workflow_id}", token)
    _require(workflow.get("path") == expected_workflow_path, "preparation run belongs to an unexpected workflow path")
    jobs = _api_get(f"{repo_api}/actions/runs/{run_id}/jobs?per_page=100", token).get("jobs")
    artifacts = _api_get(f"{repo_api}/actions/runs/{run_id}/artifacts?per_page=100", token).get("artifacts")
    artifact = validate_preparation_run(run, jobs, artifacts, source_sha, workflow_id, run_id)
    return artifact


def download_preparation_artifact(api_url, token, repository, artifact, zip_path, output_root):
    """Fetch the API ZIP, verify GitHub's digest, and safely extract its files."""
    _require(isinstance(artifact, dict), "preparation artifact metadata is unreadable")
    artifact_id = artifact.get("id")
    _require(isinstance(artifact_id, int) and artifact_id > 0, "preparation artifact ID is invalid")
    expected_digest = artifact.get("digest")
    _require(isinstance(expected_digest, str) and re.fullmatch(r"sha256:[0-9a-f]{64}", expected_digest) is not None,
             "preparation artifact ZIP digest is missing or malformed")
    _require(not Path(output_root).exists(), "artifact extraction directory already exists")
    zip_path = Path(zip_path)
    zip_path.parent.mkdir(parents=True, exist_ok=True)
    request = urllib.request.Request(
        f"{api_url.rstrip('/')}/repos/{repository}/actions/artifacts/{artifact_id}/zip",
        headers={
            "Accept": "application/vnd.github+json",
            "Authorization": f"Bearer {token}",
            "X-GitHub-Api-Version": "2022-11-28",
        },
    )
    digest = hashlib.sha256()
    downloaded = 0
    try:
        opener = urllib.request.build_opener(_ArtifactRedirectHandler)
        with opener.open(request, timeout=60) as response, zip_path.open("wb") as output:
            content_length = response.headers.get("Content-Length")
            if content_length and content_length.isdecimal():
                _require(int(content_length) <= 2 * 1024 * 1024 * 1024,
                         "preparation artifact ZIP exceeds the accepted download size limit")
            while True:
                block = response.read(1024 * 1024)
                if not block:
                    break
                downloaded += len(block)
                _require(downloaded <= 2 * 1024 * 1024 * 1024,
                         "preparation artifact ZIP exceeds the accepted download size limit")
                digest.update(block)
                output.write(block)
    except ReleaseGateError:
        zip_path.unlink(missing_ok=True)
        raise
    except (OSError, urllib.error.URLError, TimeoutError) as error:
        zip_path.unlink(missing_ok=True)
        raise ReleaseGateError(f"cannot download preparation artifact ZIP: {error}") from error
    actual_digest = f"sha256:{digest.hexdigest()}"
    if actual_digest != expected_digest:
        zip_path.unlink(missing_ok=True)
        raise ReleaseGateError(
            f"downloaded preparation artifact ZIP digest differs from GitHub metadata: {actual_digest} != {expected_digest}"
        )

    output_root = Path(output_root)
    output_root.mkdir(parents=True)
    seen = set()
    total_size = 0
    try:
        with zipfile.ZipFile(zip_path) as archive:
            for member in archive.infolist():
                name = member.filename
                _require(bool(name) and "\\" not in name, f"unsafe path in preparation artifact ZIP: {name!r}")
                raw_parts = name.rstrip("/").split("/")
                path = Path(*raw_parts)
                _require(not path.is_absolute() and all(part not in ("", ".", "..") for part in raw_parts),
                         f"unsafe path in preparation artifact ZIP: {name!r}")
                _require(path.parts and path.parts[0] in {"assets", "evidence"},
                         f"unexpected root path in preparation artifact ZIP: {name!r}")
                _require(name not in seen, f"duplicate path in preparation artifact ZIP: {name}")
                seen.add(name)
                mode = member.external_attr >> 16
                _require(not stat.S_ISLNK(mode), f"symlink in preparation artifact ZIP: {name}")
                if member.is_dir():
                    (output_root / path).mkdir(parents=True, exist_ok=True)
                    continue
                _require(stat.S_ISREG(mode) or stat.S_IFMT(mode) == 0,
                         f"non-regular file in preparation artifact ZIP: {name}")
                destination = output_root / path
                destination.parent.mkdir(parents=True, exist_ok=True)
                with archive.open(member) as source, destination.open("xb") as output:
                    member_size = 0
                    while True:
                        block = source.read(1024 * 1024)
                        if not block:
                            break
                        member_size += len(block)
                        total_size += len(block)
                        _require(member_size <= 1024 * 1024 * 1024 and total_size <= 2 * 1024 * 1024 * 1024,
                                 "preparation artifact ZIP expands beyond the accepted size limit")
                        output.write(block)
                    _require(member_size == member.file_size, f"preparation artifact ZIP member size mismatch: {name}")
    except ReleaseGateError:
        shutil.rmtree(output_root, ignore_errors=True)
        raise
    except (OSError, zipfile.BadZipFile, RuntimeError) as error:
        shutil.rmtree(output_root, ignore_errors=True)
        raise ReleaseGateError(f"cannot safely extract preparation artifact ZIP: {error}") from error
    return actual_digest


def verify_environment_api(api_url, token, repository, environment_name):
    _require(bool(token), "release policy audit token is unavailable")
    encoded = urllib.parse.quote(environment_name, safe="")
    environment = _api_get(f"{api_url.rstrip('/')}/repos/{repository}/environments/{encoded}", token)
    return validate_environment_protection(environment)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    subparsers = parser.add_subparsers(dest="command", required=True)
    bundle = subparsers.add_parser("validate-bundle")
    bundle.add_argument("--root", type=Path, required=True)
    bundle.add_argument("--source-sha", required=True)
    bundle.add_argument("--version", required=True)
    bundle.add_argument("--manifest-sha", required=True)
    bundle.add_argument("--run-id", type=int, required=True)
    review = subparsers.add_parser("stage-review-assets")
    review.add_argument("--root", type=Path, required=True)
    review.add_argument("--output", type=Path, required=True)
    review.add_argument("--source-sha", required=True)
    review.add_argument("--version", required=True)
    review.add_argument("--run-id", type=int, required=True)
    review.add_argument("--manifest-sha", required=True)
    review.add_argument("--go", default="go")
    native_review = subparsers.add_parser("stage-native-review-binary")
    native_review.add_argument("--binary", type=Path, required=True)
    native_review.add_argument("--receipt", type=Path, required=True)
    native_review.add_argument("--output", type=Path, required=True)
    native_review.add_argument("--asset-name", required=True)
    native_review.add_argument("--platform", required=True)
    native_review.add_argument("--source-sha", required=True)
    native_review.add_argument("--version", required=True)
    native_review.add_argument("--go", default="go")
    payload = subparsers.add_parser("check-review-payload")
    payload.add_argument("--root", type=Path, required=True)
    assemble = subparsers.add_parser("assemble")
    assemble.add_argument("--assets", type=Path, required=True)
    assemble.add_argument("--release-candidate", type=Path, required=True)
    assemble.add_argument("--native", type=Path, required=True)
    assemble.add_argument("--output", type=Path, required=True)
    assemble.add_argument("--source-sha", required=True)
    assemble.add_argument("--version", required=True)
    assemble.add_argument("--run-id", type=int, required=True)
    assemble.add_argument("--go", default="go")
    run = subparsers.add_parser("verify-run")
    run.add_argument("--run-id", type=int, required=True)
    run.add_argument("--source-sha", required=True)
    run.add_argument("--workflow-path", default=".github/workflows/rcc.yaml")
    run.add_argument("--artifact-name", default="rcc-release-preparation")
    fetch = subparsers.add_parser("fetch-artifact")
    fetch.add_argument("--run-id", type=int, required=True)
    fetch.add_argument("--source-sha", required=True)
    fetch.add_argument("--workflow-path", default=".github/workflows/rcc.yaml")
    fetch.add_argument("--artifact-name", default="rcc-release-preparation")
    fetch.add_argument("--zip-path", type=Path, required=True)
    fetch.add_argument("--output", type=Path, required=True)
    export_review = subparsers.add_parser("export-preparation-review")
    export_review.add_argument("--run-id", type=int, required=True)
    export_review.add_argument("--source-sha", required=True)
    export_review.add_argument("--version", required=True)
    export_review.add_argument("--manifest-sha", required=True)
    export_review.add_argument("--exporter-root", type=Path, required=True)
    export_review.add_argument("--exporter-sha", required=True)
    export_review.add_argument("--exporter-run-id", type=int, required=True)
    export_review.add_argument("--source-root", type=Path, required=True)
    export_review.add_argument("--zip-path", type=Path, required=True)
    export_review.add_argument("--bundle-root", type=Path, required=True)
    export_review.add_argument("--output", type=Path, required=True)
    export_review.add_argument("--go", default="go")
    verify_review = subparsers.add_parser("verify-review-evidence")
    verify_review.add_argument("--root", type=Path, required=True)
    verify_review.add_argument("--source-sha", required=True)
    verify_review.add_argument("--version", required=True)
    verify_review.add_argument("--run-id", type=int, required=True)
    verify_review.add_argument("--artifact-id", type=int, required=True)
    verify_review.add_argument("--manifest-sha", required=True)
    verify_review.add_argument("--preparation-zip-digest", required=True)
    verify_review.add_argument("--exporter-sha", required=True)
    verify_review.add_argument("--exporter-run-id", type=int, required=True)
    environment = subparsers.add_parser("verify-environment")
    environment.add_argument("--name", default="rcc-production")
    source = subparsers.add_parser("validate-source")
    source.add_argument("--root", type=Path, required=True)
    source.add_argument("--source-sha", required=True)
    source.add_argument("--version", required=True)
    source.add_argument("--workflow", default=".github/workflows/rcc.yaml")
    absent = subparsers.add_parser("verify-release-absent")
    absent.add_argument("--version", required=True)
    downstream = subparsers.add_parser("verify-downstream")
    args = parser.parse_args()
    try:
        if args.command == "validate-bundle":
            validate_prepared_bundle(args.root, args.source_sha, args.version, args.manifest_sha, args.run_id)
            validate_actual_binary_metadata(args.root, args.source_sha, "go")
            print("prepared release artifact validated")
        elif args.command == "stage-review-assets":
            sizes = stage_review_assets(args.root, args.output, args.source_sha, args.version,
                                        args.run_id, args.manifest_sha, args.go)
            print("review-only release asset payloads validated: " + json.dumps(sizes, sort_keys=True))
        elif args.command == "stage-native-review-binary":
            metadata = stage_native_review_binary(args.binary, args.receipt, args.output, args.asset_name,
                                                  args.platform, args.source_sha, args.version, args.go)
            print("native binary review payload validated: " + json.dumps(metadata, sort_keys=True))
        elif args.command == "check-review-payload":
            size = check_review_payload_size(args.root)
            print(f"review artifact payload size is within limit: {size} raw bytes")
        elif args.command == "assemble":
            digest = assemble_prepared_bundle(args.assets, args.release_candidate, args.native, args.output,
                                              args.source_sha, args.version, args.run_id, args.go)
            print(f"assetManifestSha256={digest}")
        elif args.command == "verify-run":
            repository = os.environ.get("GITHUB_REPOSITORY", "")
            api_url = os.environ.get("GITHUB_API_URL", "https://api.github.com")
            token = os.environ.get("GH_TOKEN", "")
            _require(bool(repository), "GITHUB_REPOSITORY is unavailable")
            artifact = verify_preparation_api(api_url, token, repository, args.run_id, args.source_sha, args.workflow_path)
            _require(artifact.get("name") == args.artifact_name, "preparation artifact has an unexpected name")
            print(f"preparation run and artifact validated: id={artifact.get('id')}")
        elif args.command == "fetch-artifact":
            repository = os.environ.get("GITHUB_REPOSITORY", "")
            api_url = os.environ.get("GITHUB_API_URL", "https://api.github.com")
            token = os.environ.get("GH_TOKEN", "")
            _require(bool(repository), "GITHUB_REPOSITORY is unavailable")
            artifact = verify_preparation_api(api_url, token, repository, args.run_id, args.source_sha, args.workflow_path)
            _require(artifact.get("name") == args.artifact_name, "preparation artifact has an unexpected name")
            digest = download_preparation_artifact(api_url, token, repository, artifact, args.zip_path, args.output)
            print(f"preparation artifact ZIP verified and extracted: id={artifact.get('id')} digest={digest}")
        elif args.command == "export-preparation-review":
            repository = os.environ.get("GITHUB_REPOSITORY", "")
            api_url = os.environ.get("GITHUB_API_URL", "https://api.github.com")
            token = os.environ.get("GH_TOKEN", "")
            _require(bool(repository), "GITHUB_REPOSITORY is unavailable")
            result = export_preparation_review(
                api_url, token, repository, args.run_id, args.source_sha, args.version, args.manifest_sha,
                args.exporter_root, args.exporter_sha, args.exporter_run_id, args.source_root, args.zip_path,
                args.bundle_root, args.output, go_binary=args.go,
            )
            print("bounded review evidence exported: " + json.dumps(result, sort_keys=True))
        elif args.command == "verify-review-evidence":
            validate_review_evidence(
                args.root, args.source_sha, args.version, args.run_id, args.artifact_id,
                args.exporter_run_id, args.manifest_sha, args.preparation_zip_digest, args.exporter_sha,
            )
            print("bounded review evidence validated")
        elif args.command == "verify-environment":
            repository = os.environ.get("GITHUB_REPOSITORY", "")
            api_url = os.environ.get("GITHUB_API_URL", "https://api.github.com")
            token = os.environ.get("RCC_RELEASE_POLICY_AUDIT_TOKEN", "")
            _require(bool(repository), "GITHUB_REPOSITORY is unavailable")
            verify_environment_api(api_url, token, repository, args.name)
            print(f"environment protection validated: {args.name}")
        elif args.command == "validate-source":
            validate_source_checkout(args.root, args.source_sha, args.version, args.workflow)
            print("exact release source and no-auto-publish workflow validated")
        elif args.command == "verify-release-absent":
            repository = os.environ.get("GITHUB_REPOSITORY", "")
            api_url = os.environ.get("GITHUB_API_URL", "https://api.github.com")
            token = os.environ.get("GH_TOKEN", "")
            _require(bool(repository), "GITHUB_REPOSITORY is unavailable")
            verify_release_absent(api_url, token, repository, args.version)
            print("tag and release are absent")
        elif args.command == "verify-downstream":
            api_url = os.environ.get("GITHUB_API_URL", "https://api.github.com")
            token = os.environ.get("HOMEBREW_TOOLS_PAT", "")
            repository = os.environ.get("GITHUB_REPOSITORY", "")
            _require("/" in repository, "GITHUB_REPOSITORY is unavailable")
            verify_downstream_workflow(api_url, token, repository.split("/", 1)[0])
            print("Homebrew update workflow is readable and active")
    except ReleaseGateError as error:
        print(f"release preflight failed: {error}", file=sys.stderr)
        return 1
    except (AttributeError, KeyError, OSError, TypeError, ValueError) as error:
        print(f"release preflight failed on malformed evidence: {error}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
