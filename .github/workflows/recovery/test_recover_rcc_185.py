import copy
import hashlib
import io
import json
from pathlib import Path
import stat
import tempfile
import unittest
from unittest import mock
import zipfile

import recover_rcc_185 as recovery


class RecoveryTests(unittest.TestCase):
    def test_rejects_changed_source_or_failed_native_job(self):
        run = dict(id=recovery.SOURCE_RUN, head_sha=recovery.SOURCE_SHA, head_branch=recovery.TAG,
                   event="push", run_attempt=1, status="completed", conclusion="failure",
                   path=".github/workflows/rcc.yaml", repository={"full_name": recovery.REPO})
        jobs = [{"name": "Build RCC", "conclusion": "success"},
                {"name": "Release Candidate Verification", "conclusion": "failure"},
                {"name": "Create GitHub Release", "conclusion": "skipped"}]
        jobs += [{"name": f"Native Runtime ({p})", "conclusion": "success"} for p in recovery.PLATFORMS]
        recovery.verify_run(run, jobs)
        for field, value in (("head_sha", "a" * 40), ("event", "pull_request"), ("run_attempt", 2)):
            with self.subTest(field=field), self.assertRaises(RuntimeError):
                recovery.verify_run(dict(run, **{field: value}), jobs)
        bad_jobs = copy.deepcopy(jobs)
        bad_jobs[-1]["conclusion"] = "skipped"
        with self.assertRaises(RuntimeError):
            recovery.verify_run(run, bad_jobs)

    def test_zip_digest_and_path_guards(self):
        def zipped(name, mode=stat.S_IFREG):
            stream = io.BytesIO()
            with zipfile.ZipFile(stream, "w") as archive:
                info = zipfile.ZipInfo(name)
                info.external_attr = mode << 16
                archive.writestr(info, b"original-binary")
            return stream.getvalue()
        with tempfile.TemporaryDirectory() as directory:
            target = Path(directory) / "assets"
            valid = zipped("rcc-linux64")
            recovery.unpack_verified(valid, hashlib.sha256(valid).hexdigest(), target)
            self.assertEqual((target / "rcc-linux64").read_bytes(), b"original-binary")
            with self.assertRaises(RuntimeError):
                recovery.unpack_verified(valid, "0" * 64, target)
            for name, mode in (("../escape", stat.S_IFREG), ("/absolute", stat.S_IFREG),
                               ("C:relative", stat.S_IFREG), ("symlink", stat.S_IFLNK)):
                payload = zipped(name, mode)
                with self.subTest(name=name), self.assertRaises(RuntimeError):
                    recovery.unpack_verified(payload, hashlib.sha256(payload).hexdigest(), target)

    def test_native_receipts_must_bind_success_to_original_binary(self):
        phase = {"artifactDigest": "sha256:artifact", "cold": {"exitCode": 0, "leaseReleased": True},
                 "warm": {"exitCode": 0, "leaseReleased": True, "providerDeadWarmReuse": True},
                 "mismatch": {"rejected": True, "providerObjectGets": 0}}
        receipt = {"commitSha": recovery.SOURCE_SHA, "platform": "linux_amd64", "artifactDigest": "sha256:artifact",
                   "binary": {"sha256": "binary-hash", "version": recovery.TAG},
                   "exactBinaryCLI": copy.deepcopy(phase), "sourceAPI": copy.deepcopy(phase),
                   "robotExactBinary": {"binarySha256": "binary-hash", "execute": {"exitCode": 0, "leaseReleased": True},
                                        "warm": {"providerDeadWarmReuse": True}}}
        receipt["sourceAPI"]["warm"].update(exitCode=-1, leaseReleased=False)
        recovery.verify_native(receipt, "linux_amd64", "binary-hash")
        for mutate in (lambda r: r.update(commitSha="b" * 40),
                       lambda r: r["binary"].update(sha256="substituted"),
                       lambda r: r["sourceAPI"]["cold"].update(exitCode=1),
                       lambda r: r["exactBinaryCLI"]["warm"].update(leaseReleased=False),
                       lambda r: r["robotExactBinary"].update(binarySha256="substituted")):
            broken = copy.deepcopy(receipt)
            mutate(broken)
            with self.assertRaises(RuntimeError):
                recovery.verify_native(broken, "linux_amd64", "binary-hash")

    def test_index_uses_original_tag_and_exact_asset_names(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            recovery.save(root / "previous/index.json", {"tested": [{"version": "v18.19.3"}], "edge": []})
            assets = root / "RCC-Binaries"
            assets.mkdir()
            for name in recovery.topology.ASSETS:
                (assets / name).write_bytes(b"retained")
            recovery.stage_index(root)
            index = json.loads((assets / "index.json").read_text())
            self.assertEqual(index["tested"][0]["version"], "v18.19.5")
            self.assertEqual(index["tested"][0]["linux"], "https://github.com/joshyorko/rcc/releases/download/v18.19.5/rcc-linux64")
            self.assertEqual(index["tested"][1]["version"], "v18.19.3")
            (assets / "unexpected").write_bytes(b"extra")
            with self.assertRaises(RuntimeError):
                recovery.stage_index(root)

    def test_tag_change_prevents_any_publication(self):
        with mock.patch.object(recovery, "api", return_value={"object": {"type": "commit", "sha": "a" * 40}}), \
             mock.patch.object(recovery, "gh") as gh, self.assertRaises(RuntimeError):
            recovery.publish(Path("unused"))
        gh.assert_not_called()


if __name__ == "__main__":
    unittest.main()
