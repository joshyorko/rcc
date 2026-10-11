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
    validate_review_evidence,
    stage_review_evidence,
    export_preparation_review,
    validate_exporter_source_checkout,
    validate_asset_build_info,
    validate_actual_binary_metadata,
    read_go_build_info,
    validate_source_checkout,
    download_preparation_artifact,
    verify_downstream_workflow,
    _ArtifactRedirectHandler,
)
import release_publication as publication


SOURCE = "a" * 40
VERSION = "v18.19.6"


class ReleasePublicationTests(unittest.TestCase):
    def fixture(self, root):
        assets = root / "assets"
        assets.mkdir(parents=True)
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

    def review_fixture(self, root):
        manifest_sha = self.fixture(root)
        release = root / "evidence" / "release-candidate"
        tmp = release / "tmp"
        (tmp / "output").mkdir(parents=True)
        (tmp / "output" / "output.xml").write_text(
            '<robot><statistics><total><stat pass="10" fail="0" skip="1">All Tests</stat></total></statistics></robot>'
        )
        (tmp / "release-candidate-v1.json").write_text(json.dumps({
            "schemaVersion": 1,
            "commitSha": SOURCE,
            "gates": {name: "passed" for name in publication.RELEASE_CANDIDATE_GATES},
        }))
        (tmp / "self-host-v1.json").write_text(json.dumps({
            "schemaVersion": 1,
            "commitSha": SOURCE,
            "homes": {"a": "/tmp/home-a", "b": "/tmp/home-b"},
            "commands": [{"step": "released-build"}],
            "binaries": {
                "released": {"version": "v18.19.5", "sha256": "d" * 64},
                "candidate": {"version": VERSION, "sha256": "a" * 64},
                "generationA": {"sourceSha256": "a" * 64, "candidateSha256": "a" * 64},
                "generationB": {"version": VERSION, "sha256": "b" * 64},
            },
        }))
        (tmp / "self-host-metadata-v1.json").write_text('{"schemaVersion":1}\n')
        (tmp / "coordination-blackbox-v1.json").write_text(json.dumps({
            "schemaVersion": 1,
            "commitSha": SOURCE,
            "binarySha256": "f" * 64,
            "scenarios": {name: "passed" for name in
                          ("cli-claim", "cli-wait", "cli-heartbeat", "cli-release", "cli-prewarm", "cli-loopback-rejection")},
        }))
        (tmp / "large-stream-receipt.json").write_text(json.dumps({
            "schemaVersion": 1, "commitSha": SOURCE, "bytes": 2 << 30,
            "bufferBytes": 64 * 1024, "requests": 1, "restartPolicy": "full-restart",
            "interruptionAcceptance": "TestHTTPInterruptedDownloadFailsVerificationThenFullRestartSucceeds",
        }))
        n1 = release / "tmp" / "rcc-self-host-fixture"
        n1.mkdir()
        archive_sha = "a" * 64
        (n1 / "n1-archive-sha256.txt").write_text(archive_sha + "\n")
        candidate_receipt = {"artifactDigest": "sha256:" + "f" * 64, "materializationId": "mat-1"}
        (n1 / "candidate-archive-upgrade.json").write_text(json.dumps(candidate_receipt))
        (n1 / "released-archive-rollback-attempt.json").write_text(json.dumps({
            "returncode": 0, "stdout": json.dumps(candidate_receipt), "stderr": "",
        }))
        (n1 / "released-v12-rollback-consumption.json").write_text(json.dumps({
            "returncode": 0, "stdout": "n1-old-task-ok\n", "stderr": "",
        }))
        state = {
            "archiveSha256": archive_sha,
            "artifactDigest": candidate_receipt["artifactDigest"],
            "materializationId": candidate_receipt["materializationId"],
            "archiveRollback": "imported",
            "legacyBefore": {"catalog": {"sha256": "b" * 64}},
            "legacyAfterUpgrade": {"catalog": {"sha256": "b" * 64}},
            "legacyAfterRollback": {"catalog": {"sha256": "b" * 64}},
            "artifactAfterArchiveRollback": {"entries": []},
            "artifactAfterRollback": {"entries": []},
        }
        (n1 / "n1-rollback-state.json").write_text(json.dumps(state))
        for platform in publication.NATIVE_RECEIPTS:
            (root / "evidence" / "native" / platform / "native-runtime-robot-evidence.json").write_text(
                json.dumps({"schemaVersion": 1, "sourceSha": SOURCE})
            )
        return manifest_sha

    def fake_go(self, root, overrides=None):
        executable = root / "fake-go"
        targets = ASSET_TARGETS
        overrides = overrides or {}
        cases = "\n".join(
            f"  {name}) goos={goos}; goarch={goarch} ;;"
            for name, (goos, goarch) in targets.items()
        )
        build_overrides = {
            "GOOS": overrides.get("GOOS"),
            "GOARCH": overrides.get("GOARCH"),
            "vcs.revision": overrides.get("vcs.revision", SOURCE),
            "vcs.modified": overrides.get("vcs.modified", "false"),
        }
        goos_line = ("printf '\\tbuild\\tGOOS=%s\\n' \"$goos\"\n" if build_overrides["GOOS"] is None
                     else f"printf '\\tbuild\\tGOOS=%s\\n' '{build_overrides['GOOS']}'\n")
        goarch_line = ("printf '\\tbuild\\tGOARCH=%s\\n' \"$goarch\"\n" if build_overrides["GOARCH"] is None
                       else f"printf '\\tbuild\\tGOARCH=%s\\n' '{build_overrides['GOARCH']}'\n")
        executable.write_text(
            "#!/bin/sh\n"
            "name=${3##*/}\n"
            "case \"$name\" in\n" + cases + "\n"
            "  *) goos=linux; goarch=amd64 ;;\n"
            "esac\n"
            "printf '%s: go1.26.9\\n' \"$3\"\n"
            + goos_line + goarch_line
            + f"printf '\\tbuild\\tvcs.revision=%s\\n' '{build_overrides['vcs.revision']}'\n"
            + f"printf '\\tbuild\\tvcs.modified=%s\\n' '{build_overrides['vcs.modified']}'\n",
            encoding="utf-8",
        )
        executable.chmod(0o755)
        return executable

    def test_review_asset_staging_binds_nine_small_exact_payloads(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            bundle = root / "bundle"
            digest = self.fixture(bundle)
            output = root / "review-assets"
            staged = publication.stage_review_assets(bundle, output, SOURCE, VERSION, 123, digest,
                                                      str(self.fake_go(root)))
            self.assertEqual(set(staged), set(ASSET_NAMES))
            self.assertTrue(publication.validate_review_assets(output, SOURCE, VERSION, 123, digest))
            for name in ASSET_NAMES:
                self.assertLessEqual(staged[name], publication.MAX_REVIEW_ARTIFACT_BYTES)
                self.assertEqual(
                    {path.relative_to(output / name).as_posix()
                     for path in (output / name).rglob("*") if path.is_file()},
                    {f"assets/{name}", "evidence/asset-manifest.json", "evidence/SHA256SUMS",
                     "evidence/provenance.json", "review-identity.json"},
                )

    def test_review_assets_reject_mismatched_identity_extra_layout_and_oversize(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            bundle = root / "bundle"
            digest = self.fixture(bundle)
            output = root / "review-assets"
            publication.stage_review_assets(bundle, output, SOURCE, VERSION, 123, digest,
                                             str(self.fake_go(root)))
            identity_path = output / "rcc-linux64" / "review-identity.json"
            identity = json.loads(identity_path.read_text())
            identity["preparationRunId"] = 124
            identity_path.write_text(json.dumps(identity))
            with self.assertRaisesRegex(ReleaseGateError, "identity mismatch"):
                publication.validate_review_assets(output, SOURCE, VERSION, 123, digest)
            identity["preparationRunId"] = 123
            identity_path.write_text(json.dumps(identity))
            (output / "rcc-linux64" / "unexpected").mkdir()
            with self.assertRaisesRegex(ReleaseGateError, "layout mismatch"):
                publication.validate_review_assets(output, SOURCE, VERSION, 123, digest)

        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            bundle = root / "bundle"
            digest = self.fixture(bundle)
            output = root / "review-assets"
            with patch.object(publication, "MAX_REVIEW_ARTIFACT_BYTES", 1):
                with self.assertRaisesRegex(ReleaseGateError, "exceeds 1 raw bytes"):
                    publication.stage_review_assets(bundle, output, SOURCE, VERSION, 123, digest,
                                                    str(self.fake_go(root)))
            self.assertFalse(output.exists())

    def test_review_evidence_export_contains_only_bound_small_receipts(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory) / "prepared"
            root.mkdir()
            manifest_sha = self.review_fixture(root)
            output = Path(directory) / "review"
            artifact = {"id": 456, "digest": "sha256:" + "b" * 64}
            with patch.object(publication, "validate_actual_binary_metadata", return_value={}):
                result = stage_review_evidence(root, output, SOURCE, VERSION, 123, manifest_sha,
                                               artifact, "c" * 40, 789)
            self.assertEqual(result["preparationArtifactId"], 456)
            self.assertEqual(result["preparationArtifactZipSha256"], artifact["digest"])
            self.assertEqual(result["fileCount"], len(publication.REVIEW_EVIDENCE_PATHS))
            self.assertLessEqual(result["rawBytes"], publication.MAX_REVIEW_ARTIFACT_BYTES)
            self.assertFalse((output / "assets").exists())
            self.assertTrue(validate_review_evidence(output, SOURCE, VERSION, 123, 456, 789, manifest_sha,
                                                     artifact["digest"], "c" * 40))
            identity = json.loads((output / "review-identity.json").read_text())
            self.assertEqual(identity["sourceSha"], SOURCE)
            self.assertEqual(identity["evidenceExporterWorkflowPath"], publication.REVIEW_EXPORT_WORKFLOW_PATH)
            self.assertIn("evidence/native/linux_amd64/jat-class-consumer-receipt.json",
                          {record["path"] for record in identity["files"]})
            for expected_source, expected_run, expected_artifact, expected_manifest in (
                ("d" * 40, 123, 456, manifest_sha),
                (SOURCE, 124, 456, manifest_sha),
                (SOURCE, 123, 457, manifest_sha),
                (SOURCE, 123, 456, "e" * 64),
            ):
                with self.subTest(source=expected_source, run=expected_run,
                                  artifact=expected_artifact, manifest=expected_manifest):
                    with self.assertRaises(ReleaseGateError):
                        validate_review_evidence(output, expected_source, VERSION, expected_run,
                                                 expected_artifact, 789, expected_manifest,
                                                 artifact["digest"], "c" * 40)
            with self.assertRaisesRegex(ReleaseGateError, "identity or file hash inventory"):
                validate_review_evidence(output, SOURCE, VERSION, 123, 456, 790, manifest_sha,
                                         artifact["digest"], "c" * 40)

            (output / "evidence" / "native" / "windows_amd64" / "output.xml").write_text("tampered")
            with self.assertRaisesRegex(ReleaseGateError, "identity or file hash inventory"):
                validate_review_evidence(output, SOURCE, VERSION, 123, 456, 789, manifest_sha,
                                         artifact["digest"], "c" * 40)

    def test_review_evidence_export_rejects_missing_symlinked_and_oversize_inputs(self):
        artifact = {"id": 456, "digest": "sha256:" + "b" * 64}
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory) / "missing"
            root.mkdir()
            manifest_sha = self.review_fixture(root)
            (root / "evidence" / "release-candidate" / "tmp" / "large-stream-receipt.json").unlink()
            with patch.object(publication, "validate_prepared_bundle"), \
                 patch.object(publication, "validate_actual_binary_metadata", return_value={}):
                with self.assertRaisesRegex(ReleaseGateError, "missing or ambiguous"):
                    stage_review_evidence(root, Path(directory) / "out", SOURCE, VERSION, 123,
                                          manifest_sha, artifact, "c" * 40, 789)

        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory) / "symlink"
            root.mkdir()
            manifest_sha = self.review_fixture(root)
            target = root / "evidence" / "release-candidate" / "tmp" / "rcc-self-host-fixture" / "n1-archive-sha256.txt"
            content = target.read_bytes()
            target.unlink()
            referent = target.with_name("archive-hash-source.txt")
            referent.write_bytes(content)
            target.symlink_to(referent.name)
            with patch.object(publication, "validate_prepared_bundle"), \
                 patch.object(publication, "validate_actual_binary_metadata", return_value={}):
                with self.assertRaisesRegex(ReleaseGateError, "unsafe"):
                    stage_review_evidence(root, Path(directory) / "out", SOURCE, VERSION, 123,
                                          manifest_sha, artifact, "c" * 40, 789)

        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory) / "oversize"
            root.mkdir()
            manifest_sha = self.review_fixture(root)
            (root / "evidence" / "release-candidate" / "tmp" / "large-stream-receipt.json").write_bytes(b"x" * 2048)
            output = Path(directory) / "out"
            with patch.object(publication, "validate_prepared_bundle"), \
                 patch.object(publication, "validate_actual_binary_metadata", return_value={}):
                with self.assertRaisesRegex(ReleaseGateError, "exceeds 128 raw bytes"):
                    stage_review_evidence(root, output, SOURCE, VERSION, 123, manifest_sha,
                                          artifact, "c" * 40, 789, max_bytes=128)
            self.assertFalse(output.exists())

    def test_evidence_export_checks_ancestor_and_full_artifact_digest(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            subprocess.run(["git", "init", "-q"], cwd=root, check=True)
            subprocess.run(["git", "config", "user.name", "Test"], cwd=root, check=True)
            subprocess.run(["git", "config", "user.email", "test@example.invalid"], cwd=root, check=True)
            (root / "source.txt").write_text("source\n")
            subprocess.run(["git", "add", "source.txt"], cwd=root, check=True)
            subprocess.run(["git", "commit", "-qm", "source"], cwd=root, check=True)
            source = subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=root, text=True).strip()
            (root / "exporter.txt").write_text("exporter\n")
            subprocess.run(["git", "add", "exporter.txt"], cwd=root, check=True)
            subprocess.run(["git", "commit", "-qm", "exporter"], cwd=root, check=True)
            exporter = subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=root, text=True).strip()
            self.assertTrue(validate_exporter_source_checkout(root, exporter, source))
            with self.assertRaisesRegex(ReleaseGateError, "differs from its workflow SHA"):
                validate_exporter_source_checkout(root, source, source)

        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            bundle = root / "prepared"
            bundle.mkdir()
            manifest_sha = self.review_fixture(bundle)
            artifact = {"id": 456, "name": "rcc-release-preparation", "digest": "sha256:" + "b" * 64}
            with patch.object(publication, "validate_exporter_source_checkout"), \
                 patch.object(publication, "validate_source_checkout"), \
                 patch.object(publication, "verify_preparation_api", return_value=artifact), \
                 patch.object(publication, "download_preparation_artifact", return_value="sha256:" + "c" * 64), \
                 patch.object(publication, "stage_review_evidence") as stage:
                with self.assertRaisesRegex(ReleaseGateError, "differs from the GitHub API"):
                    export_preparation_review("https://api.github.test", "token", "owner/repo", 123,
                                              SOURCE, VERSION, manifest_sha, root, "c" * 40,
                                              789, root, root / "bundle.zip", bundle, root / "review")
                stage.assert_not_called()

        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            bundle = root / "prepared"
            artifact = {"id": 456, "name": "rcc-release-preparation", "digest": "sha256:" + "b" * 64}
            with patch.object(publication, "validate_exporter_source_checkout") as validate_exporter, \
                 patch.object(publication, "validate_source_checkout") as validate_prepared_source, \
                 patch.object(publication, "verify_preparation_api", return_value=artifact) as verify_api, \
                 patch.object(publication, "download_preparation_artifact", return_value=artifact["digest"]) as download, \
                 patch.object(publication, "stage_review_evidence", return_value={"rawBytes": 120}) as stage:
                result = export_preparation_review(
                    "https://api.github.test", "test-token", "owner/repo", 123, SOURCE, VERSION,
                    "a" * 64, root, "c" * 40, 789, root / "source", root / "bundle.zip", bundle,
                    root / "review", go_binary="/toolchain/go",
                )
            validate_exporter.assert_called_once_with(root, "c" * 40, SOURCE)
            validate_prepared_source.assert_called_once_with(root / "source", SOURCE, VERSION)
            verify_api.assert_called_once_with("https://api.github.test", "test-token", "owner/repo", 123,
                                               SOURCE, ".github/workflows/rcc.yaml")
            download.assert_called_once_with("https://api.github.test", "test-token", "owner/repo", artifact,
                                             root / "bundle.zip", bundle)
            stage.assert_called_once_with(bundle, root / "review", SOURCE, VERSION, 123, "a" * 64,
                                          artifact, "c" * 40, 789, max_bytes=None, go_binary="/toolchain/go")
            self.assertEqual(result, {"rawBytes": 120})

    def test_review_export_workflow_is_manual_read_only_and_source_bound(self):
        workflow = (Path(__file__).parents[1] / ".github" / "workflows" /
                    "export-release-evidence.yml").read_text()
        self.assertIn("workflow_dispatch:", workflow)
        self.assertIn("if: github.ref == 'refs/heads/main'", workflow)
        self.assertIn("actions: read", workflow)
        self.assertIn("contents: read", workflow)
        self.assertNotIn("contents: write", workflow)
        self.assertIn("actions/checkout@93cb6efe18208431cddfb8368fd83d5badbf9bfd", workflow)
        self.assertIn("actions/upload-artifact@ea165f8d65b6e75b540449e92b4886f43607fa02", workflow)
        self.assertIn("export-preparation-review", workflow)
        self.assertIn("git merge-base --is-ancestor", workflow)
        self.assertIn("MAX_REVIEW_ARTIFACT_BYTES", (Path(__file__).parents[1] / "scripts" / "release_publication.py").read_text())
        for forbidden in ("gh release create", "gh api --method POST", "contents: write", "id-token: write"):
            self.assertNotIn(forbidden, workflow)

    def test_native_binary_review_payload_binds_executed_binary_and_rejects_tampering(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            binary = root / "rcc-linux64"
            binary.write_bytes(b"tiny-linux-binary")
            receipt = root / "native-runtime-receipt.json"
            receipt.write_text(json.dumps({
                "schemaVersion": 1,
                "commitSha": SOURCE,
                "platform": "linux_amd64",
                "binary": {
                    "sha256": hashlib.sha256(binary.read_bytes()).hexdigest(),
                    "version": VERSION, "runtimeGOOS": "linux", "runtimeGOARCH": "amd64",
                },
            }))
            go = self.fake_go(root)
            output = root / "native-review"
            metadata = publication.stage_native_review_binary(
                binary, receipt, output, "rcc-linux64", "linux_amd64", SOURCE, VERSION, str(go)
            )
            self.assertEqual(metadata["binarySha256"], hashlib.sha256(binary.read_bytes()).hexdigest())
            self.assertEqual(metadata["nativeReceiptSha256"], hashlib.sha256(receipt.read_bytes()).hexdigest())
            self.assertEqual(publication.check_review_payload_size(output), len(binary.read_bytes()) +
                             (output / "native-binary-review.json").stat().st_size)

            bad_receipt = dict(json.loads(receipt.read_text()))
            bad_receipt["binary"]["sha256"] = "0" * 64
            receipt.write_text(json.dumps(bad_receipt))
            with self.assertRaisesRegex(ReleaseGateError, "differs from the executed-binary receipt"):
                publication.stage_native_review_binary(
                    binary, receipt, root / "rejected", "rcc-linux64", "linux_amd64", SOURCE, VERSION, str(go)
                )

        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            binary = root / "rcc-linux64"
            binary.write_bytes(b"tiny-linux-binary")
            receipt = root / "native-runtime-receipt.json"
            receipt.write_text(json.dumps({"commitSha": SOURCE, "platform": "linux_amd64", "binary": {
                "sha256": hashlib.sha256(binary.read_bytes()).hexdigest(), "version": VERSION,
                "runtimeGOOS": "linux", "runtimeGOARCH": "amd64",
            }}))
            with patch.object(publication, "MAX_REVIEW_ARTIFACT_BYTES", 1):
                with self.assertRaisesRegex(ReleaseGateError, "exceeds 1 raw bytes"):
                    publication.stage_native_review_binary(
                        binary, receipt, root / "rejected", "rcc-linux64", "linux_amd64", SOURCE, VERSION,
                        str(self.fake_go(root)),
                    )
            self.assertFalse((root / "rejected").exists())

    def test_native_review_reports_each_go_metadata_mismatch(self):
        cases = (
            ("GOOS", "darwin"),
            ("GOARCH", "arm64"),
            ("vcs.revision", "b" * 40),
            ("vcs.modified", "true"),
        )
        for field, actual in cases:
            with self.subTest(field=field), tempfile.TemporaryDirectory() as directory:
                root = Path(directory)
                binary = root / "rcc-linux64"
                binary.write_bytes(b"tiny-linux-binary")
                receipt = root / "native-runtime-receipt.json"
                receipt.write_text(json.dumps({"commitSha": SOURCE, "platform": "linux_amd64", "binary": {
                    "sha256": hashlib.sha256(binary.read_bytes()).hexdigest(), "version": VERSION,
                    "runtimeGOOS": "linux", "runtimeGOARCH": "amd64",
                }}))
                with self.assertRaisesRegex(ReleaseGateError, field.replace(".", r"\.")):
                    publication.stage_native_review_binary(
                        binary, receipt, root / "rejected", "rcc-linux64", "linux_amd64", SOURCE, VERSION,
                        str(self.fake_go(root, {field: actual})),
                    )

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
        self.assertEqual(validate_preparation_run(run, jobs, artifact, SOURCE, 42, 123), artifact)
        with self.assertRaisesRegex(ReleaseGateError, "source SHA"):
            validate_preparation_run(run, jobs, artifact, "b" * 40, 42, 123)
        run["id"] = 124
        with self.assertRaisesRegex(ReleaseGateError, "run ID"):
            validate_preparation_run(run, jobs, artifact, SOURCE, 42, 123)
        run["id"] = 123
        run["workflow_id"] = 43
        with self.assertRaisesRegex(ReleaseGateError, "unexpected workflow"):
            validate_preparation_run(run, jobs, artifact, SOURCE, 42, 123)
        run["workflow_id"] = 42
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
