#!/usr/bin/env python3
"""Read-only verifier for the exact-head RCC hosted release-candidate gate.

The verifier reads GitHub API JSON and artifact ZIPs; it never extracts or changes
them.  The workflow artifact must preserve the actual source/candidate binaries,
self-host generations, N-1 archive, JSON receipts, and Robot output.xml.
"""

from __future__ import annotations

import argparse
import hashlib
import json
import re
import sys
import zipfile
from pathlib import PurePosixPath
from typing import Any
import xml.etree.ElementTree as ET


GATES = {
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
}
INVENTORY = {
    "rcc-linux64", "rccremote-linux64", "rcc-windows64.exe", "rccremote-windows64.exe",
    "rcc-macos64", "rccremote-macos64", "rcc-macosarm64", "rccremote-macosarm64",
}
SCENARIOS = {
    "cli-claim": "published",
    "cli-heartbeat": "renewed",
    "cli-loopback-rejection": "rejected",
    "cli-prewarm": "ready-and-capacity-limited",
    "cli-release": "released",
    "cli-wait": "artifact-backed",
}
RECEIPT_NAMES = {
    "release-candidate-v1.json", "native-runtime-receipt.json",
    "jat-class-consumer-receipt.json", "self-host-v1.json",
    "large-stream-receipt.json", "coordination-blackbox-v1.json",
    "run-identity.json", "native-runtime-robot-evidence.json",
}


class CheckError(Exception):
    pass


def require(condition: bool, message: str) -> None:
    if not condition:
        raise CheckError(message)


def load_json_bytes(data: bytes, label: str) -> dict[str, Any]:
    try:
        result = json.loads(data)
    except (UnicodeDecodeError, json.JSONDecodeError) as exc:
        raise CheckError(f"{label}: invalid JSON: {exc}") from exc
    require(isinstance(result, dict), f"{label}: expected a JSON object")
    return result


def sha256(data: bytes) -> str:
    return hashlib.sha256(data).hexdigest()


def json_candidates(zf: zipfile.ZipFile) -> dict[str, list[str]]:
    names: dict[str, list[str]] = {}
    for info in zf.infolist():
        if info.is_dir():
            continue
        base = PurePosixPath(info.filename).name
        if base.endswith(".json"):
            names.setdefault(base, []).append(info.filename)
    return names


def read_named_json(zf: zipfile.ZipFile, index: dict[str, list[str]], name: str,
                    *, required: bool = True) -> tuple[dict[str, Any] | None, str | None]:
    matches = index.get(name, [])
    if not matches and not required:
        return None, None
    require(len(matches) == 1, f"artifact needs exactly one {name}; found {matches}")
    member = matches[0]
    return load_json_bytes(zf.read(member), member), member


def find_numeric_exit(zf: zipfile.ZipFile, index: dict[str, list[str]], expected_head: str) -> tuple[int, str]:
    """Require the workflow's durable numeric result, not just job conclusion."""
    preferred = (
        "release-candidate-exit.json", "release-candidate-result.json",
        "release-candidate-run.json", "gate-result.json", "gate-exit.json",
    )
    candidates: list[tuple[str, int]] = []
    for name in preferred:
        for member in index.get(name, []):
            record = load_json_bytes(zf.read(member), member)
            value = next((record[k] for k in ("numericExitCode", "gateExitCode", "exitCode", "returnCode", "returncode")
                          if k in record), None)
            require(type(value) is int, f"{member}: missing integer numeric exit code")
            require(record.get("sourceSha") == expected_head,
                    f"{member}: numeric gate result is not bound to source {expected_head}")
            if "teeExitCode" in record:
                require(type(record["teeExitCode"]) is int and record["teeExitCode"] == 0,
                        f"{member}: tee/log pipeline exit was not zero")
            candidates.append((member, value))
    # Also accept a deliberately simple text sidecar, but never infer success
    # from absent files or a receipt whose gates say 'passed'.
    for info in zf.infolist():
        if info.is_dir() or PurePosixPath(info.filename).name not in {"release-candidate.exit", "gate.exit"}:
            continue
        raw = zf.read(info.filename).decode("ascii", "strict").strip()
        require(re.fullmatch(r"-?[0-9]+", raw) is not None, f"{info.filename}: not an integer exit code")
        candidates.append((info.filename, int(raw)))
    require(len(candidates) == 1,
            f"expected exactly one durable numeric gate exit record; found {[x[0] for x in candidates]}")
    member, code = candidates[0]
    require(code == 0, f"release-candidate process recorded numeric exit {code} in {member}")
    return code, member


def verify_run(run: dict[str, Any], jobs: dict[str, Any], expected_head: str) -> dict[str, Any]:
    require(run.get("head_sha") == expected_head,
            f"workflow run head {run.get('head_sha')!r} != expected {expected_head}")
    require(run.get("status") == "completed", f"workflow run is not completed: {run.get('status')}")
    require(run.get("conclusion") == "success", f"workflow run conclusion: {run.get('conclusion')}")
    run_id = run.get("id")
    require(run_id is not None, "workflow run API object has no id")
    raw_jobs = jobs if isinstance(jobs, list) else jobs.get("jobs", [])
    require(isinstance(raw_jobs, list), "jobs JSON must be a list or contain jobs: []")
    candidates = [j for j in raw_jobs if isinstance(j, dict) and
                  (j.get("name") == "Release Candidate Verification" or
                   j.get("name") == "release-candidate")]
    require(len(candidates) == 1, f"expected one release-candidate job; found {[j.get('name') for j in candidates]}")
    job = candidates[0]
    require(job.get("conclusion") == "success", f"release-candidate job conclusion: {job.get('conclusion')}")
    if job.get("run_id") is not None:
        require(job["run_id"] == run_id, "release-candidate job belongs to another run")
    if job.get("head_sha") is not None:
        require(job["head_sha"] == expected_head, "release-candidate job head SHA differs")
    return {"runId": run_id, "runHead": run["head_sha"], "runConclusion": run["conclusion"],
            "jobId": job.get("id"), "jobName": job.get("name"), "jobConclusion": job["conclusion"]}


def verify_artifact_metadata(artifact_data: dict[str, Any], name: str, run_id: Any,
                             expected_head: str, zip_path: str) -> dict[str, Any]:
    rows = artifact_data if isinstance(artifact_data, list) else artifact_data.get("artifacts", [])
    require(isinstance(rows, list), "artifact metadata must be a list or contain artifacts: []")
    matches = [a for a in rows if isinstance(a, dict) and a.get("name") == name]
    require(len(matches) == 1, f"expected one {name} artifact, found {len(matches)}")
    artifact = matches[0]
    require(artifact.get("expired") is not True, f"artifact {name} is expired")
    workflow_run = artifact.get("workflow_run", {})
    if workflow_run:
        require(str(workflow_run.get("id")) == str(run_id), f"artifact {name} belongs to another workflow run")
        if workflow_run.get("head_sha") is not None:
            require(workflow_run["head_sha"] == expected_head,
                    f"artifact {name} belongs to source {workflow_run['head_sha']}, expected {expected_head}")
    digest = artifact.get("digest")
    require(isinstance(digest, str) and re.fullmatch(r"sha256:[0-9a-f]{64}", digest) is not None,
            f"artifact {name} lacks GitHub SHA-256 digest")
    with open(zip_path, "rb") as f:
        actual = hashlib.file_digest(f, "sha256").hexdigest()
    require(digest == f"sha256:{actual}", f"artifact {name} ZIP digest differs from GitHub API")
    size = artifact.get("size_in_bytes")
    if size is not None:
        require(size == __import__("os").path.getsize(zip_path), f"artifact {name} ZIP size differs from API")
    return {"name": name, "id": artifact.get("id"), "digest": digest,
            "actualZipSha256": actual, "sizeBytes": size}


def verify_receipt_head(receipt: dict[str, Any], expected_head: str, label: str) -> None:
    require(receipt.get("commitSha") == expected_head,
            f"{label} source commit {receipt.get('commitSha')!r} != {expected_head}")


def check_cli_lifecycle(receipt: dict[str, Any], key: str, label: str) -> None:
    lifecycle = receipt.get(key)
    require(isinstance(lifecycle, dict), f"{label}: missing {key}")
    cold, warm, mismatch = (lifecycle.get(x) for x in ("cold", "warm", "mismatch"))
    require(all(isinstance(x, dict) for x in (cold, warm, mismatch)), f"{label}: incomplete {key} lifecycle")
    require(cold.get("cacheHit") == "provider" and cold.get("exitCode") == 0 and
            cold.get("leaseReleased") is True and bool(cold.get("leaseId")),
            f"{label}: cold operation did not prove a provider hit and released lease")
    require(warm.get("cacheHit") == "local-materialization" and warm.get("exitCode") == 0 and
            warm.get("leaseReleased") is True and warm.get("providerDeadWarmReuse") is True,
            f"{label}: warm reuse after provider death is incomplete")
    digest = lifecycle.get("artifactDigest")
    require(isinstance(digest, str) and cold.get("artifactDigest") == digest and warm.get("artifactDigest") == digest,
            f"{label}: cold/warm lifecycle does not preserve one artifact digest")
    require(mismatch.get("rejected") is True and mismatch.get("providerObjectGets") == 0,
            f"{label}: synthetic CPU mismatch was not rejected before provider object GET")


def verify_native(receipt: dict[str, Any], expected_head: str, binary_sha: str,
                  *, jat: bool = False) -> dict[str, Any]:
    label = "JAT consumer" if jat else "native runtime"
    verify_receipt_head(receipt, expected_head, label)
    require(receipt.get("platform") == "linux_amd64", f"{label}: unexpected platform {receipt.get('platform')}")
    binary = receipt.get("binary", {})
    require(binary.get("sha256") == binary_sha, f"{label}: receipt binary SHA differs from the actual candidate binary")
    require(binary.get("version") == "v18.19.6", f"{label}: reported candidate version is {binary.get('version')!r}")
    require(binary.get("runtimeGOOS") == "linux" and binary.get("runtimeGOARCH") == "amd64",
            f"{label}: reported runtime is not Linux amd64")
    exact = receipt.get("exactBinaryCLI", {})
    require(exact.get("artifactDigest") == receipt.get("artifactDigest"),
            f"{label}: exact CLI archive digest differs from receipt")
    require(exact.get("objectCount", 0) > (1000 if jat else 0), f"{label}: object count is absent or too small")
    check_cli_lifecycle({"exactBinaryCLI": exact}, "exactBinaryCLI", label + " exact CLI")
    cold_cli = exact.get("cold", {})
    require(cold_cli.get("nativeImport") == "sqlite3" and cold_cli.get("nativeExtension") and
            cold_cli.get("sqliteVersion"), f"{label}: SQLite native import evidence is missing")
    source_api = receipt.get("sourceAPI", {})
    require(source_api.get("artifactDigest") == receipt.get("artifactDigest"),
            f"{label}: source API archive digest differs from receipt")
    cold, warm, mismatch = (source_api.get(x) for x in ("cold", "warm", "mismatch"))
    require(all(isinstance(x, dict) for x in (cold, warm, mismatch)), f"{label}: incomplete source API lifecycle")
    require(cold.get("cacheHit") == "provider" and cold.get("exitCode") == 0 and
            cold.get("leaseReleased") is True and bool(cold.get("leaseId")),
            f"{label}: source API cold operation did not prove a provider hit and released lease")
    require(warm.get("cacheHit") == "local-materialization" and warm.get("providerDeadWarmReuse") is True,
            f"{label}: source API warm reuse after provider death is incomplete")
    require(cold.get("artifactDigest") == source_api.get("artifactDigest") and
            warm.get("artifactDigest") == source_api.get("artifactDigest"),
            f"{label}: source API cold/warm lifecycle digest mismatch")
    require(mismatch.get("rejected") is True and mismatch.get("providerObjectGets") == 0,
            f"{label}: source API mismatch was not rejected before provider object GET")
    if jat:
        require(receipt.get("artifactDigest") == exact.get("artifactDigest"), f"{label}: archive digest differs")
    return {"platform": receipt["platform"], "binarySha256": binary_sha,
            "archiveDigest": receipt.get("artifactDigest"), "objectCount": exact["objectCount"],
            "coldProviderHit": True, "providerDeadWarmReuse": True, "mismatchZeroGets": True}


def verify_robot_xml(zf: zipfile.ZipFile, files_by_base: dict[str, list[str]], member: str,
                     expected: tuple[int, int, int], expected_skip: str) -> dict[str, int]:
    entries = files_by_base.get(member, [])
    require(len(entries) == 1, f"expected one Robot XML {member}; found {entries}")
    root = ET.fromstring(zf.read(entries[0]))
    stats = root.find("statistics")
    require(stats is not None, f"{entries[0]} has no Robot statistics")
    total = stats.find("total/stat")
    require(total is not None, f"{entries[0]} has no total Robot statistic")
    actual = (int(total.get("pass", "-1")), int(total.get("fail", "-1")), int(total.get("skip", "-1")))
    require(actual == expected, f"Robot counts {actual} != expected {expected}")
    skipped = []
    for test in root.iter("test"):
        status = test.find("status")
        if status is not None and status.get("status") == "SKIP":
            skipped.append(test.get("name"))
    require(skipped == [expected_skip], f"Robot skip set {skipped} != expected Windows-only skip {[expected_skip]}")
    return {"pass": actual[0], "fail": actual[1], "skip": actual[2], "total": sum(actual)}


def hash_zip_member(zf: zipfile.ZipFile, member: str) -> str:
    digest = hashlib.sha256()
    with zf.open(member) as stream:
        while True:
            block = stream.read(1024 * 1024)
            if not block:
                break
            digest.update(block)
    return digest.hexdigest()


def verify_bundle(args: argparse.Namespace, run_context: dict[str, Any]) -> dict[str, Any]:
    with zipfile.ZipFile(args.evidence_zip) as zf:
        infos = {i.filename: i for i in zf.infolist() if not i.is_dir()}
        index = json_candidates(zf)
        files_by_base: dict[str, list[str]] = {}
        for name in infos:
            files_by_base.setdefault(PurePosixPath(name).name, []).append(name)
        numeric_exit, exit_path = find_numeric_exit(zf, index, args.expected_head)
        logs = files_by_base.get("release-candidate.log", [])
        require(len(logs) == 1, f"expected exactly one raw release-candidate log; found {logs}")
        raw_log = zf.read(logs[0]).decode("utf-8", "replace")
        require("Release-candidate receipt:" in raw_log,
                "raw release-candidate log does not show receipt emission")
        release, _ = read_named_json(zf, index, "release-candidate-v1.json")
        assert release is not None
        verify_receipt_head(release, args.expected_head, "release candidate")
        require(release.get("schemaVersion") == 1, "unsupported release-candidate receipt schema")
        gates = release.get("gates", {})
        require(set(gates) == GATES, f"gate set mismatch; missing={sorted(GATES-set(gates))}, extra={sorted(set(gates)-GATES)}")
        require(all(v == "passed" for v in gates.values()), "one or more release-candidate gates did not pass")
        require(len(release.get("commands", [])) >= 12, "release receipt does not retain all gate commands")
        source = release.get("source", {})
        candidate_sha = source.get("sha256")
        require(isinstance(candidate_sha, str) and re.fullmatch(r"[0-9a-f]{64}", candidate_sha) is not None,
                "release receipt has no exact built candidate SHA-256")
        source_member = args.candidate_binary_member
        if source_member is None:
            matches = [name for name in infos if PurePosixPath(name).name == "rcc" and
                       ("build/" in name or name.startswith("build/"))]
            require(len(matches) == 1, f"specify --candidate-binary-member; found candidates {matches}")
            source_member = matches[0]
        require(source_member in infos, f"candidate binary member missing: {source_member}")
        source_info = infos[source_member]
        require(source_info.file_size > 0 and (source_info.external_attr >> 16) & 0o170000 != 0o120000,
                "candidate binary is empty or symlinked in evidence ZIP")
        actual_binary_sha = hash_zip_member(zf, source_member)
        require(actual_binary_sha == candidate_sha,
                f"actual candidate binary {source_member} SHA {actual_binary_sha} != receipt {candidate_sha}")

        native, native_path = read_named_json(zf, index, "native-runtime-receipt.json")
        jat, jat_path = read_named_json(zf, index, "jat-class-consumer-receipt.json")
        selfhost, selfhost_path = read_named_json(zf, index, "self-host-v1.json")
        stream, stream_path = read_named_json(zf, index, "large-stream-receipt.json")
        coordination, coord_path = read_named_json(zf, index, "coordination-blackbox-v1.json")
        staged_metadata, metadata_path = read_named_json(zf, index, "self-host-metadata-v1.json")
        assert native and jat and selfhost and stream and coordination
        native_result = verify_native(native, args.expected_head, actual_binary_sha)
        jat_result = verify_native(jat, args.expected_head, actual_binary_sha, jat=True)

        verify_receipt_head(selfhost, args.expected_head, "self-host")
        binaries = selfhost.get("binaries", {})
        for key in ("released", "generationA", "candidate", "generationB"):
            require(key in binaries, f"self-host receipt missing binary record {key}")
        candidate_selfhost_sha = binaries["candidate"].get("sha256")
        require(candidate_selfhost_sha and binaries["generationA"].get("candidateSha256") == candidate_selfhost_sha and
                binaries["generationB"].get("sha256") == candidate_selfhost_sha,
                "self-host promoted/generation B candidate binary identities differ")
        # The archive inputs/outputs and every self-host executable must be
        # individually retained in the uploaded ZIP, even when their receipt
        # paths point at runner-local locations.
        n1_candidates = [name for name in infos if name.endswith(".rcca")]
        require(len(n1_candidates) >= 1, "evidence ZIP contains no self-host N-1 archive")
        hash_sidecars = files_by_base.get("n1-archive-sha256.txt", [])
        require(len(hash_sidecars) == 1, f"expected one self-host N-1 archive hash sidecar; found {hash_sidecars}")
        fresh_archive_sha = zf.read(hash_sidecars[0]).decode("ascii", "strict").strip()
        require(re.fullmatch(r"[0-9a-f]{64}", fresh_archive_sha) is not None,
                "N-1 archive hash sidecar is malformed")
        archive_matches = [name for name in n1_candidates if hash_zip_member(zf, name) == fresh_archive_sha]
        require(len(archive_matches) == 1, f"fresh N-1 archive hash has no unique artifact member; matches={archive_matches}")
        require(binaries["released"].get("version") == args.expected_n1_version,
                "self-host released binary is not the pinned N-1 version")
        require(binaries["candidate"].get("version") == args.expected_candidate_version,
                "self-host candidate version differs from expected release candidate")
        by_hash: dict[str, list[str]] = {}
        for name in infos:
            if PurePosixPath(name).name in {"rcc", "rccremote"}:
                by_hash.setdefault(hash_zip_member(zf, name), []).append(name)
        must_hashes = {
            "generationA.sourceSha256": binaries["generationA"].get("sourceSha256"),
            "generationA.candidateSha256": binaries["generationA"].get("candidateSha256"),
            "candidate.sha256": binaries["candidate"].get("sha256"),
            "generationB.sha256": binaries["generationB"].get("sha256"),
        }
        for label, digest in must_hashes.items():
            require(digest in by_hash, f"self-host binary bytes for {label} ({digest}) were not uploaded")
        role_hash_paths = {
            "released source": ("/self-host/released/rcc", binaries["generationA"].get("sourceSha256")),
            "released source remote": ("/self-host/released/rccremote", None),
            "promoted candidate": ("/self-host/promoted/rcc", binaries["generationA"].get("candidateSha256")),
            "generation B candidate": ("/self-host/candidate/rcc", binaries["generationB"].get("sha256")),
            "generation B remote": ("/self-host/candidate/rccremote", None),
        }
        self_host_actual_binaries = {}
        for label, (suffix, digest) in role_hash_paths.items():
            matches = [name for name in infos if name.endswith(suffix)]
            require(len(matches) == 1, f"self-host artifact must retain exactly one {label} at *{suffix}; found {matches}")
            require(infos[matches[0]].file_size > 0, f"self-host {label} executable is empty")
            actual_sha = hash_zip_member(zf, matches[0])
            if digest is not None:
                require(actual_sha == digest, f"self-host {label} bytes differ from receipt")
            self_host_actual_binaries[label] = {"member": matches[0], "sha256": actual_sha,
                                                "sizeBytes": infos[matches[0]].file_size}
        require(staged_metadata == {"schemaVersion": 1},
                "staged self-host home-B public metadata does not contain exactly schemaVersion 1")
        rollback_members = files_by_base.get("n1-rollback-state.json", [])
        require(len(rollback_members) == 1, f"expected one N-1 before/after state receipt; found {rollback_members}")
        rollback_state = load_json_bytes(zf.read(rollback_members[0]), rollback_members[0])
        require(rollback_state.get("archiveSha256") == fresh_archive_sha,
                "self-host N-1 rollback receipt archive SHA differs from actual retained archive")
        legacy_before = rollback_state.get("legacyBefore", {})
        legacy_after = rollback_state.get("legacyAfterUpgrade", {})
        legacy_rollback = rollback_state.get("legacyAfterRollback", {})
        object_map = lambda state: {k: v for k, v in state.items() if k.startswith("object:")}
        require(object_map(legacy_before) == object_map(legacy_after) == object_map(legacy_rollback),
                "N-1 archive upgrade/rollback changed legacy object bytes")
        require(legacy_after == legacy_rollback,
                "released N-1 legacy consumption changed the rebased legacy closure")
        require(rollback_state.get("artifactAfterArchiveRollback") == rollback_state.get("artifactAfterRollback"),
                "N-1 rollback changed artifact state after archive consumption")
        archive_rollback = rollback_state.get("archiveRollback")
        require(archive_rollback == "imported",
                "pinned N-1 candidate could not import the exact candidate archive")
        candidate_upgrade_members = files_by_base.get("candidate-archive-upgrade.json", [])
        require(len(candidate_upgrade_members) == 1,
                f"expected one candidate archive import receipt; found {candidate_upgrade_members}")
        candidate_upgrade = load_json_bytes(zf.read(candidate_upgrade_members[0]), candidate_upgrade_members[0])
        require(candidate_upgrade.get("artifactDigest") == rollback_state.get("artifactDigest") and
                candidate_upgrade.get("materializationId") == rollback_state.get("materializationId"),
                "candidate archive receipt and N-1 rollback state identify different artifacts/materializations")
        artifact_before = rollback_state.get("artifactBefore", {})
        artifact_after = rollback_state.get("artifactAfterArchiveRollback", {})
        artifact_after_old_task = rollback_state.get("artifactAfterRollback", {})
        require(isinstance(artifact_before, dict) and isinstance(artifact_after, dict) and
                isinstance(artifact_after_old_task, dict),
                "N-1 rollback receipt lacks artifact-state snapshots")
        # This receipt's artifactBefore snapshot predates candidate archive
        # acquisition. The exact provisional-record delta is validated inside
        # the passing gate from a later, private snapshot; that later baseline
        # is not emitted. We can independently confirm the imported state did
        # not change while the old binary consumed the task.
        require(artifact_after == artifact_after_old_task,
                "N-1 legacy task consumption changed the post-import artifact state")
        legacy_catalog_hashes = {
            "before": legacy_before.get("catalog", {}).get("sha256"),
            "afterCandidate": legacy_after.get("catalog", {}).get("sha256"),
            "afterN1": legacy_rollback.get("catalog", {}).get("sha256"),
        }
        consumption_members = files_by_base.get("released-v12-rollback-consumption.json", [])
        require(len(consumption_members) == 1, f"missing released N-1 legacy-consumption proof: {consumption_members}")
        consumption = load_json_bytes(zf.read(consumption_members[0]), consumption_members[0])
        require(consumption.get("returncode") == 0 and "n1-old-task-ok" in consumption.get("stdout", ""),
                "released N-1 binary did not consume the imported legacy task successfully")
        evidence_names = {PurePosixPath(x).name for x in selfhost.get("evidencePaths", [])}
        required_evidence = {"released-v12.json", "candidate-v12.json", "released-v12-compatibility.json",
                             "released-v12-archive-closure.json", "candidate-archive-upgrade.json",
                             "released-archive-rollback-attempt.json", "released-v12-rollback-consumption.json",
                             "n1-rollback-state.json"}
        require(required_evidence <= evidence_names,
                f"self-host evidence path manifest lacks {sorted(required_evidence-evidence_names)}")

        verify_receipt_head(stream, args.expected_head, "large stream")
        require(stream.get("bytes") == 2 * 1024**3 and stream.get("bufferBytes") == 64 * 1024,
                "large stream did not prove 2 GiB input with 64 KiB transfer buffer")
        require(stream.get("requests") == 1 and stream.get("restartPolicy") == "full-restart" and
                stream.get("interruptionAcceptance") == "TestHTTPInterruptedDownloadFailsVerificationThenFullRestartSucceeds",
                "large stream retry/interruption receipt is incomplete")
        require(isinstance(stream.get("memoryBytes"), int) and stream["memoryBytes"] > 0,
                "large stream heap allocation receipt is absent")

        verify_receipt_head(coordination, args.expected_head, "coordination")
        require(coordination.get("binarySha256") == actual_binary_sha,
                "coordination receipt binary SHA differs from actual candidate binary")
        require(coordination.get("scenarios") == SCENARIOS, "coordination scenario set/result mismatch")
        robot_xml_member = args.robot_xml_member
        if robot_xml_member not in infos:
            matches = [name for name in infos if name.endswith("/output/output.xml")]
            require(len(matches) == 1,
                    f"full Robot XML member {robot_xml_member} missing and fallback is ambiguous: {matches}")
            robot_xml_member = matches[0]
        robot_counts = verify_robot_xml(zf, {"output.xml": [robot_xml_member]}, "output.xml",
                                        args.robot_counts, args.expected_robot_skip)
        return {
            "schemaVersion": 1,
            "sourceHead": args.expected_head,
            "run": run_context,
            "evidenceArtifact": args.evidence_artifact_metadata,
            "numericGateExit": {"code": numeric_exit, "member": exit_path},
            "rawLog": {"member": logs[0], "bytes": len(raw_log.encode("utf-8")),
                       "receiptEmissionPresent": True},
            "releaseCandidate": {"receipt": "release-candidate-v1.json", "all11GatesPassed": True,
                                 "candidateBinaryMember": source_member, "candidateBinarySha256": actual_binary_sha},
            "native": {"receipt": native_path, **native_result},
            "jat": {"receipt": jat_path, **jat_result},
            "selfHost": {"receipt": selfhost_path, "releasedN1Version": binaries["released"]["version"],
                         "candidateVersion": binaries["candidate"]["version"],
                         "candidateGenerationSha256": candidate_selfhost_sha,
                         "generationARuntimeHashes": must_hashes,
                         "actualUploadedBinaries": self_host_actual_binaries,
                         "n1ArchiveMember": archive_matches[0], "n1ArchiveSha256": fresh_archive_sha,
                         "archiveRollback": archive_rollback,
                         "legacyObjectCount": len(object_map(legacy_after)),
                         "legacyCatalogHashes": legacy_catalog_hashes,
                         "artifactEntryCounts": {"beforeArchiveImport": len(artifact_before),
                                                 "afterCandidateImportAndN1Rollback": len(artifact_after),
                                                 "afterOldTaskConsumption": len(artifact_after_old_task)},
                         "exactProvisionalRemovalLimit": "validated by releaseCandidate gate; post-acquire private snapshot is not emitted in this receipt",
                         "evidenceManifestCount": len(selfhost.get("evidencePaths", []))},
            "largeStream": {"receipt": stream_path, "bytes": stream["bytes"],
                            "bufferBytes": stream["bufferBytes"], "memoryBytes": stream["memoryBytes"],
                            "memoryMetric": "runtime.MemStats.Alloc heap bytes at test snapshot (not RSS)"},
            "coordination": {"receipt": coord_path, "scenarioCount": len(coordination["scenarios"])},
            "robot": {"outputXml": robot_xml_member, **robot_counts},
            "inventory": "verified separately with --binaries-zip when supplied",
            "publicMetadataReceipt": metadata_path,
        }


def verify_binary_inventory(path: str) -> dict[str, Any]:
    with zipfile.ZipFile(path) as zf:
        files = [i for i in zf.infolist() if not i.is_dir()]
        by_base: dict[str, list[str]] = {}
        for info in files:
            by_base.setdefault(PurePosixPath(info.filename).name, []).append(info.filename)
            mode = (info.external_attr >> 16) & 0o170000
            require(mode != 0o120000, f"binary inventory member is a symlink: {info.filename}")
        require(set(by_base) == INVENTORY and all(len(v) == 1 for v in by_base.values()),
                f"8-binary inventory mismatch; names={sorted(by_base)}")
        rows = []
        for filename in sorted(INVENTORY):
            member = by_base[filename][0]
            info = zf.getinfo(member)
            require(info.file_size > 0, f"empty inventory binary: {member}")
            rows.append({"name": filename, "member": member, "sizeBytes": info.file_size,
                         "sha256": hash_zip_member(zf, member)})
        return {"artifact": path, "count": len(rows), "binaries": rows}


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--expected-head", required=True, help="Exact 40-character source commit")
    parser.add_argument("--run-json", required=True, help="GitHub workflow-run API JSON")
    parser.add_argument("--jobs-json", required=True, help="GitHub workflow-jobs API JSON")
    parser.add_argument("--artifacts-json", required=True, help="GitHub workflow-artifacts API JSON")
    parser.add_argument("--evidence-zip", required=True, help="Downloaded release-candidate evidence ZIP")
    parser.add_argument("--evidence-artifact-metadata", default="release-candidate-evidence")
    parser.add_argument("--candidate-binary-member", help="ZIP member for build/rcc; auto-detected only when unique")
    parser.add_argument("--robot-xml-member", default="tmp/output/output.xml",
                        help="Full Robot suite XML member in evidence ZIP")
    parser.add_argument("--binaries-zip", help="Downloaded RCC-Binaries ZIP; verifies exact 8-file inventory")
    parser.add_argument("--expected-n1-version", default="v18.19.5")
    parser.add_argument("--expected-candidate-version", default="v18.19.6")
    parser.add_argument("--robot-counts", nargs=3, type=int, default=(171, 0, 1), metavar=("PASS", "FAIL", "SKIP"))
    parser.add_argument("--expected-robot-skip", default="Goal: Windows uv-native activation script uses cmd semantics")
    parser.add_argument("--output", required=True, help="Write verification JSON here")
    args = parser.parse_args()
    try:
        require(re.fullmatch(r"[0-9a-f]{40}", args.expected_head) is not None, "expected head must be lowercase 40-char SHA")
        run = json.loads(open(args.run_json, encoding="utf-8").read())
        jobs = json.loads(open(args.jobs_json, encoding="utf-8").read())
        artifacts = json.loads(open(args.artifacts_json, encoding="utf-8").read())
        run_context = verify_run(run, jobs, args.expected_head)
        evidence_meta = verify_artifact_metadata(artifacts, args.evidence_artifact_metadata,
                                                 run_context["runId"], args.expected_head, args.evidence_zip)
        result = verify_bundle(args, run_context)
        result["evidenceArtifact"] = evidence_meta
        if args.binaries_zip:
            binaries_name = "RCC-Binaries"
            binary_meta = verify_artifact_metadata(artifacts, binaries_name, run_context["runId"],
                                                   args.expected_head, args.binaries_zip)
            result["binaryArtifact"] = binary_meta
            result["inventory"] = verify_binary_inventory(args.binaries_zip)
        with open(args.output, "w", encoding="utf-8") as f:
            json.dump(result, f, indent=2, sort_keys=True)
            f.write("\n")
        print(json.dumps({"verified": True, "head": args.expected_head,
                          "runId": run_context["runId"], "output": args.output}, sort_keys=True))
        return 0
    except (CheckError, OSError, zipfile.BadZipFile, KeyError, TypeError, ValueError) as exc:
        print(f"hosted-full verification failed: {exc}", file=sys.stderr)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
