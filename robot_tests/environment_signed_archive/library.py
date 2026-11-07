import base64
import hashlib
import json
import os
import shutil
import subprocess
import sys
import tempfile
import time
import urllib.request
import zipfile
from datetime import datetime, timedelta, timezone
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from environment_artifacts.library import (
    environment_artifact_process_environment,
    new_environment_artifact_fixture,
    remove_environment_artifact_fixture,
    wait_for_json_file,
)


ARTIFACT_KEY_ID = "signed-archive-artifact-test"
METADATA_KEY_ID = "signed-archive-metadata-test"
# Public RFC 8032 test vectors; the matching private seeds are test-only.
ARTIFACT_SEED = "9d61b19deffd5a60ba844af492ec2cc44449c5697b326919703bac031cae7f60"
ARTIFACT_PUBLIC = "d75a980182b10ab7d54bfed3c964073a0ee172f3daa62325af021a68f707511a"
METADATA_SEED = "4ccd089b28ff96da9db6c346ec114e0f5b8a319f35aba624da8cf6ed4fb8a6fb"
METADATA_PUBLIC = "3d4017c3e843895a92b70aa74d1b7ebc9c982ccf2ec4968cc0cd55f12af4660c"


def _phase(name):
    path = Path(__file__).resolve().parents[2] / "developer" / "tmp" / "signed-archive-robot" / "phases.log"
    path.parent.mkdir(parents=True, exist_ok=True)
    with path.open("a", encoding="utf-8") as stream:
        stream.write(f"{time.strftime('%Y-%m-%dT%H:%M:%SZ', time.gmtime())} {name}\n")
        stream.flush()


def _run(binary, arguments, environment, expected=0):
    result = subprocess.run(
        [str(binary), *map(str, arguments)], env=dict(environment),
        capture_output=True, text=True, timeout=30 * 60, check=False,
    )
    if (expected is None and result.returncode == 0) or (expected is not None and result.returncode != expected):
        raise AssertionError(
            f"command exited {result.returncode}, expected {expected}: {arguments}\n"
            f"stdout={result.stdout}\nstderr={result.stderr}"
        )
    return result


def _write_private_key(path, seed):
    Path(path).write_bytes(bytes.fromhex(seed))
    Path(path).chmod(0o600)


def _trust_roots(path, key_id, public_hex):
    Path(path).write_text(json.dumps({key_id: base64.b64encode(bytes.fromhex(public_hex)).decode()}, separators=(",", ":")), encoding="utf-8")


def _metadata_roots(path, public_hex):
    now = datetime.now(timezone.utc)
    value = {
        "schemaVersion": 2,
        "scope": "signed-offline-archive-robot",
        "minimumSequence": 1,
        "keys": {
            METADATA_KEY_ID: {
                "publicKey": base64.b64encode(bytes.fromhex(public_hex)).decode().rstrip("="),
                "notBefore": (now - timedelta(hours=1)).isoformat().replace("+00:00", "Z"),
                "notAfter": (now + timedelta(hours=24)).isoformat().replace("+00:00", "Z"),
            }
        },
    }
    Path(path).write_text(json.dumps(value, separators=(",", ":")), encoding="utf-8")


def _portable_carrier_file(root, artifact, kind):
    digest = artifact.removeprefix("sha256:")
    return Path(root) / "sha256" / digest / f"{kind}.json"


def _install_metadata(binary, environment, artifact, carrier_root, sequence, metadata_seed, revocations):
    attachment = _portable_carrier_file(carrier_root, artifact, "provenance").parent
    revocations_file = attachment.parent.parent / f"revocations-{sequence}.json"
    revocations_file.write_text(json.dumps(revocations, separators=(",", ":")), encoding="utf-8")
    now = datetime.now(timezone.utc).replace(microsecond=0)
    issued = now.isoformat().replace("+00:00", "Z")
    expires = (now + timedelta(hours=4)).isoformat().replace("+00:00", "Z")
    signed = attachment.parent.parent / f"metadata-{sequence}.json"
    _run(binary, ["env", "trust", "sign-metadata",
                  "--artifact", artifact, "--scope", "signed-offline-archive-robot",
                  "--sequence", sequence, "--metadata-key-id", METADATA_KEY_ID,
                  "--metadata-signing-key", metadata_seed,
                  "--issued-at", issued, "--expires-at", expires,
                  "--provenance", _portable_carrier_file(carrier_root, artifact, "provenance"),
                  "--sbom", _portable_carrier_file(carrier_root, artifact, "sbom"),
                  "--revocations", revocations_file, "--output", signed], environment)
    target = _portable_carrier_file(carrier_root, artifact, "metadata")
    target.parent.mkdir(parents=True, exist_ok=True)
    shutil.copyfile(signed, target)
    return target


def _export(binary, environment, artifact, provider_url, carrier, path):
    result = _run(binary, ["env", "export", "--artifact", artifact, "--provider", provider_url,
                           "--trust-carrier", carrier, "--trust-carrier-type", "filesystem",
                           "--include-trust-attachments", "--output", path], environment)
    receipt = json.loads(result.stdout)
    if receipt.get("artifactDigest") != artifact or not receipt.get("trustAttachmentsIncluded"):
        raise AssertionError(f"archive export omitted trust attachments: {receipt}")
    if set(receipt.get("trustAttachmentKinds", [])) != {"provenance", "sbom", "signature", "revocations", "metadata"}:
        raise AssertionError(f"unexpected exported trust kinds: {receipt}")
    return receipt


def _external_carrier_from_archive(archive, artifact, root):
    destination = Path(root)
    artifact_directory = destination / "sha256" / artifact.removeprefix("sha256:")
    artifact_directory.mkdir(parents=True, exist_ok=True)
    with zipfile.ZipFile(archive) as source:
        for kind in ("provenance", "sbom", "signature", "revocations", "metadata"):
            name = f"rcc-environment/attestations/{kind}.json"
            try:
                data = source.read(name)
            except KeyError:
                continue
            (artifact_directory / f"{kind}.json").write_bytes(data)
    return destination


def _acquire_args(archive, artifact, roots, metadata_roots, metadata_state, *, consume=True, external=None):
    arguments = ["env", "acquire", "--archive", archive, "--artifact", artifact, "--trust-roots", roots,
                 "--metadata-trust-roots", metadata_roots, "--metadata-state", metadata_state,
                 "--strict-remote", "--json"]
    if consume:
        arguments.append("--consume-archive-trust")
    if external:
        arguments.extend(["--trust-carrier", external, "--trust-carrier-type", "filesystem"])
    return arguments


def _archive_trust_path(home, archive):
    identity = hashlib.sha256(Path(archive).read_bytes()).hexdigest()
    return Path(home) / "artifacts" / "v1" / "archive-trust" / identity


def _assert_denied_before_execution(binary, archive, environment, roots, metadata_roots,
                                    metadata_state, artifact, task_marker, expected_error):
    acquired = _run(binary, _acquire_args(archive, artifact, roots, metadata_roots, metadata_state), environment, expected=None)
    if acquired.stdout.strip() or expected_error not in acquired.stderr:
        raise AssertionError(f"denied acquire exposed output or wrong reason: {acquired}")
    consumer_trust = _archive_trust_path(environment["ROBOCORP_HOME"], archive)
    executed = _run(binary, ["env", "exec", "--artifact", artifact,
                             "--trust-carrier", consumer_trust, "--trust-carrier-type", "filesystem",
                             "--trust-roots", roots, "--metadata-trust-roots", metadata_roots,
                             "--metadata-state", metadata_state, "--strict-remote", "--json", "--",
                             "python", "-c", "from pathlib import Path; Path(r'" + str(task_marker) + "').write_text('executed')"],
                    environment, expected=None)
    if executed.stdout.strip() or expected_error not in executed.stderr or Path(task_marker).exists():
        raise AssertionError(f"denied execution produced output or ran its task: {executed}")


def exercise_signed_offline_archive(binary):
    _phase("fixture:start")
    reuse_root = os.environ.get("RCC_SIGNED_ARCHIVE_REUSE_FIXTURE")
    if reuse_root:
        prepared = Path(reuse_root).resolve()
        expected_parent = (Path(__file__).resolve().parents[2] / "tmp").resolve()
        if prepared.parent != expected_parent or not prepared.name.startswith("environment-artifacts-"):
            raise AssertionError(f"refusing to reuse unexpected fixture root {prepared}")
        # Reuse only immutable transport and separately provisioned public roots.
        # Prior consumers and every retained journal remain untouched.
        if os.environ.get("RCC_SIGNED_ARCHIVE_RESUME_VALIDATION") == "1":
            root = prepared  # Keep every existing journal; replay must still fail closed.
        else:
            root = Path(tempfile.mkdtemp(prefix="environment-artifacts-", dir=expected_parent))
            for name in ("signed-v1.rcca", "signed-v2.rcca", "revoked.rcca", "artifact-roots.json", "metadata-roots.json", "provider.stdout"):
                shutil.copyfile(prepared / name, root / name)
        resume_home = root / "home-resume"
        resume_home.mkdir(exist_ok=True)
        fixture = {
            "root": str(root), "aHome": str(root / "home-a"), "bHome": str(resume_home),
            "providerRoot": str(root / "home-a" / "artifacts" / "v1" / "provider"),
            "serverStdout": str(root / "provider.stdout"), "serverStderr": str(root / "provider.stderr"),
        }
    else:
        fixture = new_environment_artifact_fixture()
    root = Path(fixture["root"])
    provider_root = Path(fixture["providerRoot"])
    publisher_home = Path(fixture["aHome"])
    consumer_home = Path(fixture["bHome"])
    publisher_env = environment_artifact_process_environment(publisher_home, offline=False)
    consumer_env = environment_artifact_process_environment(consumer_home, offline=True)
    publisher_trust = publisher_home / "artifacts" / "v1" / "trust"
    provider_stdout = root / "provider.stdout"
    provider_stderr = root / "provider.stderr"
    provider_stdout_handle = None
    provider_stderr_handle = None
    server = None
    if not reuse_root:
        provider_stdout_handle = provider_stdout.open("w", encoding="utf-8")
        provider_stderr_handle = provider_stderr.open("w", encoding="utf-8")
        server = subprocess.Popen([str(binary), "cache", "serve", "--json", "--root", str(provider_root)],
                                  env=publisher_env, stdout=provider_stdout_handle, stderr=provider_stderr_handle)
    succeeded = False
    try:
        provider = wait_for_json_file(provider_stdout)
        provider_url = provider["url"]
        roots = root / "artifact-roots.json"
        metadata_roots = root / "metadata-roots.json"
        archive_v1 = root / "signed-v1.rcca"
        archive_v2 = root / "signed-v2.rcca"
        revoked_archive = root / "revoked.rcca"
        if reuse_root:
            with zipfile.ZipFile(archive_v1) as source:
                artifact = json.loads(source.read("rcc-environment/manifest.json"))["artifactDigest"]
            original_archive_hash = hashlib.sha256(archive_v1.read_bytes()).hexdigest()
            external_trust = _external_carrier_from_archive(archive_v1, artifact, root / "external-trust-v1")
            _phase("publisher:fixture-reused")
        else:
            _phase("provider:ready")
            _run(binary, ["provider", "add", "office", "--type", "http", "--url", provider_url, "--json"], publisher_env)
            _run(binary, ["provider", "add", "office", "--type", "http", "--url", provider_url, "--json"], consumer_env)
            artifact_key = root / "artifact-test.key"
            metadata_key = root / "metadata-test.key"
            _write_private_key(artifact_key, ARTIFACT_SEED)
            _write_private_key(metadata_key, METADATA_SEED)
            published = _run(binary, ["env", "publish", "--robot", Path(__file__).resolve().parents[1] / "environment_artifacts" / "robot.yaml",
                                      "--provider", "office", "--trust-carrier", publisher_trust,
                                      "--trust-carrier-type", "filesystem", "--signing-key", artifact_key,
                                      "--signing-key-id", ARTIFACT_KEY_ID, "--json"], publisher_env)
            artifact = json.loads(published.stdout)["artifactDigest"]
            _phase("publish:complete")
            _trust_roots(roots, ARTIFACT_KEY_ID, ARTIFACT_PUBLIC)
            _metadata_roots(metadata_roots, METADATA_PUBLIC)
            _install_metadata(binary, publisher_env, artifact, publisher_trust, 1, metadata_key, [])
            _export(binary, publisher_env, artifact, "office", publisher_trust, archive_v1)
            _phase("export:v1")
            original_archive_hash = hashlib.sha256(archive_v1.read_bytes()).hexdigest()
            external_trust = _external_carrier_from_archive(archive_v1, artifact, root / "external-trust-v1")

            # Prepare a later authenticated checkpoint and a signed revocation archive while reachable.
            _install_metadata(binary, publisher_env, artifact, publisher_trust, 2, metadata_key, [])
            _export(binary, publisher_env, artifact, "office", publisher_trust, archive_v2)
            _phase("export:v2")
            _install_metadata(binary, publisher_env, artifact, publisher_trust, 3, metadata_key,
                               [{"keyIDs": [ARTIFACT_KEY_ID], "updatedAt": datetime.now(timezone.utc).replace(microsecond=0).isoformat().replace("+00:00", "Z")}])
            _export(binary, publisher_env, artifact, "office", publisher_trust, revoked_archive)
            _phase("export:revoked")

        # Import and strict verification in the offline private consumer home.
        if server is not None:
            server.terminate()
            server.wait(timeout=10)
        _phase("provider:stopped")
        try:
            urllib.request.urlopen(provider_url + "/v1/capabilities", timeout=1)
        except OSError:
            pass
        else:
            raise AssertionError("provider remained reachable during offline archive acquisition")
        metadata_state = root / "deployment-metadata-state"
        acquired = _run(binary, _acquire_args(archive_v1, artifact, roots, metadata_roots, metadata_state), consumer_env)
        _phase("acquire:v1")
        acquisition_receipt = json.loads(acquired.stdout)
        if (acquisition_receipt.get("artifactDigest") != artifact
                or acquisition_receipt.get("trustAttachmentsConsumed") is not True
                or set(acquisition_receipt.get("trustAttachmentKinds", [])) !=
                {"provenance", "sbom", "signature", "revocations", "metadata"}):
            raise AssertionError(f"archive evidence was not consumed: {acquisition_receipt}")
        task_marker = root / "executed.txt"
        executed = _run(binary, ["env", "exec", "--artifact", artifact,
                                 "--trust-carrier", acquisition_receipt["trustCarrierPath"],
                                 "--trust-carrier-type", "filesystem", "--trust-roots", roots,
                                 "--metadata-trust-roots", metadata_roots, "--metadata-state", metadata_state,
                                 "--strict-remote", "--json", "--", "python", "-c",
                                 "from pathlib import Path; Path(r'" + str(task_marker) + "').write_text('executed')"], consumer_env)
        _phase("exec:v1")
        if not task_marker.is_file() or json.loads(executed.stdout).get("exitCode") != 0:
            raise AssertionError("offline acquired environment did not execute the task")

        # Same digest and warm home must not reuse evidence omitted from this archive's selected bytes.
        missing_metadata = root / "missing-metadata.rcca"
        with zipfile.ZipFile(archive_v1) as source, zipfile.ZipFile(missing_metadata, "w", compression=zipfile.ZIP_STORED) as target:
            for item in source.infolist():
                if item.filename != "rcc-environment/attestations/metadata.json":
                    target.writestr(item, source.read(item.filename))
        state_before_missing_metadata = hashlib.sha256(b"".join(sorted(p.read_bytes() for p in metadata_state.rglob("*") if p.is_file()))).hexdigest()
        _assert_denied_before_execution(binary, missing_metadata, consumer_env, roots, metadata_roots,
                                        metadata_state, artifact, root / "missing-metadata-must-not-execute.txt", "metadata-unsigned")
        state_after_missing_metadata = hashlib.sha256(b"".join(sorted(p.read_bytes() for p in metadata_state.rglob("*") if p.is_file()))).hexdigest()
        if state_before_missing_metadata != state_after_missing_metadata:
            raise AssertionError("missing archive metadata changed retained metadata state")

        # The old archive remains valid through a separately selected external carrier.
        external_home = root / "external-carrier-consumer"
        external_home.mkdir(exist_ok=True)
        external_env = environment_artifact_process_environment(external_home, offline=True)
        external_acquire = _run(binary, _acquire_args(archive_v1, artifact, roots, metadata_roots, root / "external-state",
                                                       consume=False, external=external_trust), external_env)
        if json.loads(external_acquire.stdout).get("artifactDigest") != artifact:
            raise AssertionError("external detached carrier alternative failed")

        # Tampered/missing signatures and wrong metadata authorities deny before task output.
        signature_name = f"rcc-environment/attestations/signature.json"
        tampered = root / "tampered.rcca"
        with zipfile.ZipFile(archive_v1) as source, zipfile.ZipFile(tampered, "w", compression=zipfile.ZIP_STORED) as target:
            for item in source.infolist():
                data = source.read(item.filename)
                if item.filename == signature_name:
                    signature = json.loads(data)
                    signature["signatures"][0]["signature"] = base64.b64encode(bytes(64)).decode().rstrip("=")
                    data = json.dumps(signature, separators=(",", ":")).encode()
                target.writestr(item, data)
        missing = root / "missing-signature.rcca"
        with zipfile.ZipFile(archive_v1) as source, zipfile.ZipFile(missing, "w", compression=zipfile.ZIP_STORED) as target:
            for item in source.infolist():
                if item.filename != signature_name:
                    target.writestr(item, source.read(item.filename))
        wrong_metadata_roots = root / "wrong-metadata-roots.json"
        _metadata_roots(wrong_metadata_roots, ARTIFACT_PUBLIC)
        for name, candidate, selected_roots, expected in (
            ("tampered", tampered, metadata_roots, "invalid"),
            ("missing", missing, metadata_roots, "unsigned"),
            ("wrong-authority", archive_v1, wrong_metadata_roots, "metadata-authority"),
        ):
            denial_home = root / f"denial-{name}"
            denial_home.mkdir(exist_ok=True)
            denial_env = environment_artifact_process_environment(denial_home, offline=True)
            marker = root / f"{name}-must-not-execute.txt"
            _assert_denied_before_execution(binary, candidate, denial_env, roots, selected_roots,
                                            root / f"state-{name}", artifact, marker, expected)

        # Advance retained state with sequence 2, then reject replay of sequence 1.
        _run(binary, _acquire_args(archive_v2, artifact, roots, metadata_roots, metadata_state), consumer_env)
        state_before_replay = hashlib.sha256(b"".join(sorted(p.read_bytes() for p in metadata_state.rglob("*") if p.is_file()))).hexdigest()
        _assert_denied_before_execution(binary, archive_v1, consumer_env, roots, metadata_roots,
                                        metadata_state, artifact, root / "rollback-must-not-execute.txt", "metadata-rollback")
        state_after_replay = hashlib.sha256(b"".join(sorted(p.read_bytes() for p in metadata_state.rglob("*") if p.is_file()))).hexdigest()
        if state_before_replay != state_after_replay:
            raise AssertionError("replayed archive rewound or changed retained metadata state")

        # A signed revocation snapshot is transported but denies acquisition and execution.
        revoked_home = root / "revoked-consumer"
        revoked_home.mkdir(exist_ok=True)
        revoked_env = environment_artifact_process_environment(revoked_home, offline=True)
        _assert_denied_before_execution(binary, revoked_archive, revoked_env, roots, metadata_roots,
                                        root / "revoked-state", artifact, root / "revoked-must-not-execute.txt", "revoked")

        evidence = {
            "artifactDigest": artifact,
            "archiveSha256": original_archive_hash,
            "binarySha256": hashlib.sha256(Path(binary).read_bytes()).hexdigest(),
            "executionMarker": str(task_marker),
            "externalCarrier": "passed",
            "negativeCases": ["tampered", "missing", "missing-metadata-warm-home", "wrong-authority", "metadata-rollback", "revoked"],
            "platform": os.name,
        }
        succeeded = True
        return evidence
    finally:
        if server is not None and server.poll() is None:
            server.terminate()
            server.wait(timeout=10)
        if provider_stdout_handle is not None:
            provider_stdout_handle.close()
        if provider_stderr_handle is not None:
            provider_stderr_handle.close()
        if succeeded and not reuse_root:
            remove_environment_artifact_fixture(fixture["root"])


def signed_offline_archive_vertical(binary):
    return exercise_signed_offline_archive(binary)
