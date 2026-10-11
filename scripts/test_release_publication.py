import hashlib
import json
import subprocess
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch
from io import BytesIO
import zipfile
import sys

sys.path.insert(0, str(Path(__file__).parent))

from release_publication import (
    ASSET_NAMES,
    ASSET_TARGETS,
    EXPECTED_GO_VERSION,
    REQUIRED_NATIVE_JOBS,
    ReleaseGateError,
    validate_environment_protection,
    validate_preparation_run,
    validate_prepared_bundle,
    validate_asset_build_info,
    validate_actual_binary_metadata,
    read_go_build_info,
    validate_source_checkout,
    download_preparation_artifact,
    verify_downstream_workflow,
    _ArtifactRedirectHandler,
)


SOURCE = "a" * 40
VERSION = "v18.19.6"


class ReleasePublicationTests(unittest.TestCase):
    def fixture(self, root):
        assets = root / "assets"
        assets.mkdir()
        asset_hashes = {}
        for name in ASSET_NAMES:
            if name == "index.json":
                base = f"https://github.com/joshyorko/rcc/releases/download/{VERSION}"
                content = (json.dumps({"tested": [{
                    "version": VERSION,
                    "windows": f"{base}/rcc-windows64.exe",
                    "linux": f"{base}/rcc-linux64",
                    "macos": f"{base}/rcc-macos64",
                    "macosarm64": f"{base}/rcc-macosarm64",
                }]}, separators=(",", ":")) + "\n").encode()
            else:
                content = f"asset:{name}".encode()
            (assets / name).write_bytes(content)
            asset_hashes[name] = {"sha256": hashlib.sha256(content).hexdigest(), "size": len(content)}

        evidence = root / "evidence"
        (evidence / "release-candidate").mkdir(parents=True)
        (evidence / "native").mkdir()
        manifest = {
            "schemaVersion": 1,
            "sourceSha": SOURCE,
            "version": VERSION,
            "preparationRunId": 123,
            "assets": asset_hashes,
        }
        manifest_path = evidence / "asset-manifest.json"
        manifest_path.write_text(json.dumps(manifest, sort_keys=True, separators=(",", ":")) + "\n")
        manifest_digest = hashlib.sha256(manifest_path.read_bytes()).hexdigest()
        (evidence / "SHA256SUMS").write_text("".join(f"{item['sha256']}  assets/{name}\n" for name, item in sorted(asset_hashes.items())))
        (evidence / "provenance.json").write_text(json.dumps({
            "schemaVersion": 1,
            "sourceSha": SOURCE,
            "version": VERSION,
            "preparationRunId": 123,
            "assetManifestSha256": manifest_digest,
            "nativePlatforms": sorted(("linux_amd64", "darwin_amd64", "darwin_arm64", "windows_amd64")),
            "binaryBuildInfo": {
                name: {
                    "goVersion": EXPECTED_GO_VERSION,
                    "goos": target[0],
                    "goarch": target[1],
                    "vcsRevision": SOURCE,
                    "vcsModified": False,
                }
                for name, target in ASSET_TARGETS.items()
            },
        }))
        (evidence / "release-candidate" / "release-candidate-exit.json").write_text(json.dumps({
            "sourceSha": SOURCE,
            "gateExitCode": 0,
            "teeExitCode": 0,
        }))

        def lifecycle(digest):
            compatibility = {
                "status": "compatible",
                "worker": {
                    "python": {"artifactProvided": True},
                    "os": {"nativeArchitecture": "amd64"},
                    "cpu": {"architecture": "amd64"},
                    "filesystem": {"maxPath": 4096},
                    "relocationVersions": ["test-relocation"],
                },
            }
            common = {
                "artifactDigest": digest,
                "materializationId": "materialization-1",
                "path": "/home/holotree/materialization-1",
                "compatibility": compatibility,
            }
            return {
                "artifactDigest": digest,
                "cold": {
                    **common, "cacheHit": "provider", "exitCode": 0, "leaseId": "lease-1",
                    "leaseReleased": True, "providerDeadWarmReuse": False,
                    "nativeImport": "sqlite3", "nativeExtension": "/python/_sqlite3.so", "sqliteVersion": "3.54.0",
                },
                "warm": {
                    **common, "cacheHit": "local-materialization", "providerDeadWarmReuse": True,
                },
                "mismatch": {"rejected": True, "providerObjectGets": 0},
            }

        platform_checks = {
            "linux": {"runnerOS", "machine", "caseCollision", "executableSymlink", "filesystemType", "libc", "glibcMuslMismatch"},
            "darwin": {"runnerOS", "machine", "caseCollision", "executableSymlink", "rosetta", "machoRPath", "machoDependencies", "quarantineXattr", "codeSigning", "translationAllowed", "nativeOnlyOnRosetta"},
            "windows": {"runnerOS", "machine", "caseCollision", "executableSymlink", "longPath", "junction", "fileLock", "pathNormalization"},
        }
        platform_assets = {
            "linux_amd64": "rcc-linux64",
            "darwin_amd64": "rcc-macos64",
            "darwin_arm64": "rcc-macosarm64",
            "windows_amd64": "rcc-windows64.exe",
        }
        for platform, asset in platform_assets.items():
            directory = evidence / "native" / platform
            directory.mkdir(parents=True)
            digest = f"sha256:{hashlib.sha256(platform.encode()).hexdigest()}"
            goos, goarch = ASSET_TARGETS[asset]
            native = {
                "schemaVersion": 1,
                "commitSha": SOURCE,
                "platform": platform,
                "artifactDigest": digest,
                "binary": {
                    "sha256": asset_hashes[asset]["sha256"], "version": VERSION,
                    "runtimeGOOS": goos, "runtimeGOARCH": goarch,
                    "expectedGOOS": goos, "expectedGOARCH": goarch,
                },
                "exactBinaryCLI": {
                    **lifecycle(digest),
                    "objectCount": 1000,
                    "platformChecks": {
                        key: ("case-sensitive-filesystem" if key == "caseCollision" else
                              "skipped:native-runner" if key == "nativeOnlyOnRosetta" else "measured")
                        for key in platform_checks[goos]
                    },
                },
                "sourceAPI": lifecycle(digest),
                "robotExactBinary": {
                    "binarySha256": asset_hashes[asset]["sha256"], "binaryVersion": VERSION,
                    "artifactDigest": "sha256:" + "d" * 64,
                    "cold": {"artifactDigest": "sha256:" + "d" * 64, "cacheHit": "provider", "materializationId": "robot-materialization", "path": "/robot/materialization"},
                    "execute": {"artifactDigest": "sha256:" + "d" * 64, "cacheHit": "local-materialization", "materializationId": "robot-materialization",
                                "path": "/robot/materialization", "leaseId": "robot-lease", "leaseReleased": True, "exitCode": 0},
                    "warm": {"artifactDigest": "sha256:" + "d" * 64, "cacheHit": "local-materialization", "materializationId": "robot-materialization",
                             "path": "/robot/materialization", "providerDeadWarmReuse": True},
                    "native": {"import": "sqlite3", "extension": "/python/_sqlite3.so", "sqliteVersion": "3.54.0"},
                },
            }
            (directory / "native-runtime-receipt.json").write_text(json.dumps({
                **native,
            }))
            (directory / "output.xml").write_text("""<robot><statistics><total><stat pass="1" fail="0" skip="0">All Tests</stat></total></statistics></robot>""")
            if platform == "linux_amd64":
                jat = {key: value for key, value in native.items() if key != "robotExactBinary"}
                (directory / "jat-class-consumer-receipt.json").write_text(json.dumps(jat))
        return manifest_digest

    def test_accepts_complete_bundle_and_exact_prep_run(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            digest = self.fixture(root)
            validate_prepared_bundle(root, SOURCE, VERSION, digest, 123)

        run = {
            "event": "workflow_dispatch",
            "head_branch": "main",
            "head_sha": SOURCE,
            "conclusion": "success",
            "workflow_id": 42,
            "id": 123,
        }
        jobs = [{"name": name, "conclusion": "success"} for name in REQUIRED_NATIVE_JOBS]
        jobs += [
            {"name": "Build RCC", "conclusion": "success"},
            {"name": "Release Candidate Verification", "conclusion": "success"},
            {"name": "Assemble Release Preparation", "conclusion": "success"},
        ]
        artifact = {"name": "rcc-release-preparation", "expired": False, "digest": "sha256:" + "c" * 64}
        validate_preparation_run(run, jobs, artifact, SOURCE, 42, 123)

    def test_rejects_tampered_or_missing_asset(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            digest = self.fixture(root)
            (root / "assets" / "rcc-linux64").write_bytes(b"tampered")
            with self.assertRaisesRegex(ReleaseGateError, "size mismatch|checksum mismatch"):
                validate_prepared_bundle(root, SOURCE, VERSION, digest, 123)

    def test_rejects_boolean_or_float_zero_exit_receipts(self):
        for field, value in (("gateExitCode", True), ("teeExitCode", 0.0)):
            with self.subTest(field=field, value=value), tempfile.TemporaryDirectory() as directory:
                root = Path(directory)
                digest = self.fixture(root)
                receipt_path = root / "evidence" / "release-candidate" / "release-candidate-exit.json"
                receipt = json.loads(receipt_path.read_text())
                receipt[field] = value
                receipt_path.write_text(json.dumps(receipt))
                with self.assertRaisesRegex(ReleaseGateError, "numeric zero exit status"):
                    validate_prepared_bundle(root, SOURCE, VERSION, digest, 123)

    def test_bundle_validation_independently_rejects_wrong_index_version_and_release_path(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            digest = self.fixture(root)
            index = root / "assets" / "index.json"
            data = json.loads(index.read_text())
            data["tested"][0]["version"] = "v0.0.0"
            for key in ("windows", "linux", "macos", "macosarm64"):
                data["tested"][0][key] = data["tested"][0][key].replace(
                    "https://github.com/joshyorko/rcc/releases/download/v18.19.6",
                    "https://attacker.invalid/releases/download/v0.0.0",
                )
            index.write_text(json.dumps(data, separators=(",", ":")) + "\n")

            manifest_path = root / "evidence" / "asset-manifest.json"
            manifest = json.loads(manifest_path.read_text())
            content = index.read_bytes()
            manifest["assets"]["index.json"] = {"sha256": hashlib.sha256(content).hexdigest(), "size": len(content)}
            manifest_bytes = (json.dumps(manifest, sort_keys=True, separators=(",", ":")) + "\n").encode()
            manifest_path.write_bytes(manifest_bytes)
            sums = root / "evidence" / "SHA256SUMS"
            lines = sums.read_text().splitlines()
            lines = [line for line in lines if not line.endswith("  assets/index.json")]
            lines.append(f"{hashlib.sha256(content).hexdigest()}  assets/index.json")
            sums.write_text("\n".join(sorted(lines)) + "\n")

            with self.assertRaisesRegex(ReleaseGateError, "release index semantics are invalid"):
                validate_prepared_bundle(root, SOURCE, VERSION, hashlib.sha256(manifest_bytes).hexdigest(), 123)

    def test_checks_compiler_revision_and_target_for_all_eight_binaries(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            assets = root / "assets"
            assets.mkdir()
            for name in ASSET_NAMES:
                (assets / name).write_bytes(b"placeholder")
            go = root / "go"
            go.write_text("""#!/usr/bin/env python3
import pathlib, sys
name = pathlib.Path(sys.argv[-1]).name
targets = {
    "rcc-linux64": ("linux", "amd64"), "rccremote-linux64": ("linux", "amd64"),
    "rcc-windows64.exe": ("windows", "amd64"), "rccremote-windows64.exe": ("windows", "amd64"),
    "rcc-macos64": ("darwin", "amd64"), "rccremote-macos64": ("darwin", "amd64"),
    "rcc-macosarm64": ("darwin", "arm64"), "rccremote-macosarm64": ("darwin", "arm64"),
}
goos, goarch = targets[name]
print(f"{name}: go1.26.9")
print(f"\\tbuild\\tGOOS={goos}")
print(f"\\tbuild\\tGOARCH={goarch}")
print(f"\\tbuild\\tvcs.revision={'a' * 40}")
print("\\tbuild\\tvcs.modified=false")
""")
            go.chmod(0o755)
            metadata = validate_asset_build_info(assets, SOURCE, str(go))
            self.assertEqual(set(metadata), set(ASSET_TARGETS))
            go.write_text(go.read_text().replace("go1.26.9", "go1.26.90"))
            with self.assertRaisesRegex(ReleaseGateError, "not exactly go1.26.9"):
                read_go_build_info(assets / "rcc-linux64", str(go))
            go.write_text(go.read_text().replace("go1.26.90", "go1.26.9"))
            go.write_text(go.read_text().replace("{'a' * 40}", "'b' * 40"))
            with self.assertRaisesRegex(ReleaseGateError, "source revision mismatch"):
                validate_asset_build_info(assets, SOURCE, str(go))

        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            digest = self.fixture(root)
            (root / "assets" / "rcc-linux64").unlink()
            with self.assertRaisesRegex(ReleaseGateError, "asset set"):
                validate_prepared_bundle(root, SOURCE, VERSION, digest, 123)

    def test_rejects_source_or_version_mismatch(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            digest = self.fixture(root)
            with self.assertRaisesRegex(ReleaseGateError, "source SHA"):
                validate_prepared_bundle(root, "b" * 40, VERSION, digest)
            with self.assertRaisesRegex(ReleaseGateError, "version"):
                validate_prepared_bundle(root, SOURCE, "v18.19.7", digest)

    def test_rejects_failed_preparation_or_missing_native_job(self):
        run = {
            "event": "workflow_dispatch",
            "head_branch": "main",
            "head_sha": SOURCE,
            "conclusion": "failure",
            "workflow_id": 42,
            "id": 123,
        }
        jobs = [{"name": name, "conclusion": "success"} for name in REQUIRED_NATIVE_JOBS]
        jobs += [{"name": "Build RCC", "conclusion": "success"}, {"name": "Release Candidate Verification", "conclusion": "success"}, {"name": "Assemble Release Preparation", "conclusion": "success"}]
        artifact = {"name": "rcc-release-preparation", "expired": False, "digest": "sha256:" + "c" * 64}
        with self.assertRaisesRegex(ReleaseGateError, "successful"):
            validate_preparation_run(run, jobs, artifact, SOURCE, 42, 123)

        run["conclusion"] = "success"
        jobs = [job for job in jobs if job["name"] != REQUIRED_NATIVE_JOBS[0]]
        with self.assertRaisesRegex(ReleaseGateError, "native job or preparation job"):
            validate_preparation_run(run, jobs, artifact, SOURCE, 42, 123)

    def test_rejects_missing_or_unprotected_environment(self):
        with self.assertRaisesRegex(ReleaseGateError, "unreadable"):
            validate_environment_protection(None)
        with self.assertRaisesRegex(ReleaseGateError, "required reviewers"):
            validate_environment_protection({"can_admins_bypass": False, "protection_rules": []})
        with self.assertRaisesRegex(ReleaseGateError, "self-review"):
            validate_environment_protection({
                "can_admins_bypass": False,
                "protection_rules": [{
                    "type": "required_reviewers",
                    "reviewers": [{"type": "User", "reviewer": {"login": "release-reviewer"}}],
                    "prevent_self_review": False,
                }],
            })

    def test_download_checks_github_zip_digest_and_rejects_unsafe_members(self):
        def archive_bytes(member_name="assets/index.json"):
            buffer = BytesIO()
            with zipfile.ZipFile(buffer, "w") as archive:
                archive.writestr(member_name, b"{}");
            return buffer.getvalue()

        class Response:
            def __init__(self, payload):
                self.payload = BytesIO(payload)
                self.headers = {}

            def __enter__(self):
                return self

            def __exit__(self, *_args):
                return False

            def read(self, size=-1):
                return self.payload.read(size)

        class Opener:
            def __init__(self, payload):
                self.payload = payload

            def open(self, _request, timeout):
                self.asserted_timeout = timeout
                return Response(self.payload)

        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            payload = archive_bytes()
            digest = "sha256:" + hashlib.sha256(payload).hexdigest()
            artifact = {"id": 7, "digest": digest}
            with patch("release_publication.urllib.request.build_opener", return_value=Opener(payload)):
                actual = download_preparation_artifact("https://api.github.test", "test", "owner/repo", artifact,
                                                       root / "bundle.zip", root / "bundle")
            self.assertEqual(actual, digest)
            self.assertEqual((root / "bundle" / "assets" / "index.json").read_bytes(), b"{}")

            with patch("release_publication.urllib.request.build_opener", return_value=Opener(payload)):
                with self.assertRaisesRegex(ReleaseGateError, "digest differs"):
                    download_preparation_artifact("https://api.github.test", "test", "owner/repo",
                                                   {"id": 7, "digest": "sha256:" + "0" * 64},
                                                   root / "tampered.zip", root / "tampered")

            unsafe = archive_bytes("../escape")
            with patch("release_publication.urllib.request.build_opener", return_value=Opener(unsafe)):
                with self.assertRaisesRegex(ReleaseGateError, "unsafe path"):
                    download_preparation_artifact("https://api.github.test", "test", "owner/repo",
                                                   {"id": 7, "digest": "sha256:" + hashlib.sha256(unsafe).hexdigest()},
                                                   root / "unsafe.zip", root / "unsafe")
            self.assertFalse((root / "unsafe").exists())

    def test_artifact_redirect_drops_bearer_on_cross_origin_and_requires_https(self):
        handler = _ArtifactRedirectHandler()
        request = __import__("urllib.request", fromlist=["Request"]).Request("https://api.github.test/file")
        request.add_header("Authorization", "Bearer never-forward-this")
        redirected = handler.redirect_request(request, None, 302, "Found", {}, "https://objects.test/file")
        self.assertNotIn("Authorization", redirected.headers)
        same_origin = handler.redirect_request(request, None, 302, "Found", {}, "https://api.github.test/other")
        self.assertEqual(same_origin.get_header("Authorization"), "Bearer never-forward-this")
        with self.assertRaisesRegex(ReleaseGateError, "HTTPS"):
            handler.redirect_request(request, None, 302, "Found", {}, "http://objects.test/file")

    def test_prepared_bundle_checks_actual_go_metadata_against_provenance(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            self.fixture(root)
            go = root / "go"
            go.write_text("""#!/usr/bin/env python3
import pathlib, sys
targets = {
  "rcc-linux64": ("linux", "amd64"), "rccremote-linux64": ("linux", "amd64"),
  "rcc-windows64.exe": ("windows", "amd64"), "rccremote-windows64.exe": ("windows", "amd64"),
  "rcc-macos64": ("darwin", "amd64"), "rccremote-macos64": ("darwin", "amd64"),
  "rcc-macosarm64": ("darwin", "arm64"), "rccremote-macosarm64": ("darwin", "arm64"),
}
name = pathlib.Path(sys.argv[-1]).name
os, arch = targets[name]
print(f"{name}: go1.26.9")
print(f"\\tbuild\\tGOOS={os}")
print(f"\\tbuild\\tGOARCH={arch}")
print(f"\\tbuild\\tvcs.revision={'a' * 40}")
print("\\tbuild\\tvcs.modified=false")
""")
            go.chmod(0o755)
            actual = validate_actual_binary_metadata(root, SOURCE, str(go))
            self.assertEqual(set(actual), set(ASSET_TARGETS))
            go.write_text(go.read_text().replace("{'a' * 40}", "'b' * 40"))
            with self.assertRaisesRegex(ReleaseGateError, "source revision mismatch"):
                validate_actual_binary_metadata(root, SOURCE, str(go))

    def test_homebrew_lookup_uses_owner_of_current_repository(self):
        with patch("release_publication._api_get", return_value={"state": "active"}) as get:
            verify_downstream_workflow("https://api.github.test", "token", "joshyorko")
        self.assertEqual(get.call_args.args[0],
                         "https://api.github.test/repos/joshyorko/homebrew-tools/actions/workflows/tap-auto-update.yml")
        with self.assertRaisesRegex(ReleaseGateError, "bypass"):
            validate_environment_protection({
                "can_admins_bypass": True,
                "protection_rules": [{
                    "type": "required_reviewers",
                    "reviewers": [{"type": "User", "reviewer": {"login": "release-reviewer"}}],
                    "prevent_self_review": True,
                }],
            })

    def test_rejects_legacy_auto_tag_workflow_and_accepts_corrected_source(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            version = root / "common" / "version.go"
            version.parent.mkdir()
            version.write_text("package common\nconst Version = `v18.19.6`\n")
            changelog = root / "docs" / "changelog.md"
            changelog.parent.mkdir()
            changelog.write_text("## Unreleased\n\n## v18.19.6 (date: 10.10.2026)\n")
            workflow = root / ".github" / "workflows" / "rcc.yaml"
            workflow.parent.mkdir(parents=True)
            workflow.write_text("""name: Rcc
on:
  push:
    branches: [main]
  workflow_dispatch:
    inputs: {}
permissions:
  contents: read
jobs:
  build:
    steps: []
""")
            tag_workflow = root / ".github" / "workflows" / "create-release-tag.yml"
            tag_workflow.write_text("""name: Deprecated Release Tag Entry
on:
  workflow_dispatch:
permissions:
  contents: read
jobs:
  hold:
    steps: []
""")
            recovery_workflow = root / ".github" / "workflows" / "recover-rcc-185.yml"
            recovery_workflow.write_text("""name: Retired RCC v18.19.5 Recovery Hold
on:
  workflow_dispatch:
permissions:
  contents: read
jobs:
  retired:
    steps:
      - run: echo retired; exit 1
""")
            publisher = root / ".github" / "workflows" / "publish-release.yml"
            publisher.write_text("""name: Publish RCC Release
on:
  workflow_dispatch:
    inputs: {}
permissions:
  contents: write
jobs:
  publish:
    environment:
      name: rcc-production
    steps:
      - run: verify-environment verify-run validate-bundle git merge-base --is-ancestor gh release create
""")
            subprocess.run(["git", "init", "-q"], cwd=root, check=True)
            subprocess.run(["git", "config", "user.name", "Test"], cwd=root, check=True)
            subprocess.run(["git", "config", "user.email", "test@example.invalid"], cwd=root, check=True)
            subprocess.run(["git", "add", "common/version.go", "docs/changelog.md", ".github/workflows"], cwd=root, check=True)
            subprocess.run(["git", "commit", "-qm", "fixture"], cwd=root, check=True)
            source_sha = subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=root, text=True).strip()
            self.assertTrue(validate_source_checkout(root, source_sha, VERSION))
            recovery_workflow.write_text(recovery_workflow.read_text().replace("contents: read", "contents: write"))
            with self.assertRaisesRegex(ReleaseGateError, "historical recovery workflow"):
                validate_source_checkout(root, source_sha, VERSION)
            recovery_workflow.write_text(recovery_workflow.read_text().replace("contents: write", "contents: read"))
            workflow.write_text(workflow.read_text().replace("branches: [main]", 'branches: [main]\n    tags: ["v*"]'))
            subprocess.run(["git", "add", ".github/workflows/rcc.yaml"], cwd=root, check=True)
            subprocess.run(["git", "commit", "-qm", "legacy tag trigger"], cwd=root, check=True)
            source_sha = subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=root, text=True).strip()
            with self.assertRaisesRegex(ReleaseGateError, "automatically from tag pushes"):
                validate_source_checkout(root, source_sha, VERSION)
            workflow.write_text("name: Rcc\non: workflow_dispatch\njobs:\n  release:\n    steps: []\n")
            subprocess.run(["git", "add", ".github/workflows/rcc.yaml"], cwd=root, check=True)
            subprocess.run(["git", "commit", "-qm", "legacy release"], cwd=root, check=True)
            source_sha = subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=root, text=True).strip()
            with self.assertRaisesRegex(ReleaseGateError, "automatic release job"):
                validate_source_checkout(root, source_sha, VERSION)


if __name__ == "__main__":
    unittest.main()
