import io
import hashlib
import subprocess
import sys
import tarfile
import tempfile
import unittest
from pathlib import Path
from unittest import mock


ROOT = Path(__file__).parents[1]
sys.path.insert(0, str(ROOT))
import tasks  # noqa: E402


def _archive_bytes():
  content = b"micromamba"
  stream = io.BytesIO()
  with tarfile.open(fileobj=stream, mode="w:bz2") as archive:
    member = tarfile.TarInfo("bin/micromamba")
    member.size = len(content)
    archive.addfile(member, io.BytesIO(content))
  return stream.getvalue()


def _archive_with_members(members):
  stream = io.BytesIO()
  with tarfile.open(fileobj=stream, mode="w:bz2") as archive:
    for member in members:
      if member.isreg() and member.size:
        archive.addfile(member, io.BytesIO(b"x" * member.size))
      else:
        archive.addfile(member)
  return stream.getvalue()


class MicromambaDownloadTests(unittest.TestCase):
  def test_download_rejects_oversized_payload_at_curl_and_local_boundaries(self):
    """Break caught: configured sources can force unbounded archive resource use."""
    calls = []

    def run(argv, **kwargs):
      calls.append(argv)
      destination = Path(argv[argv.index("-o") + 1])
      with destination.open("wb") as output:
        output.seek(tasks.MICROMAMBA_MAX_ARCHIVE_BYTES)
        output.write(b"x")
      return subprocess.CompletedProcess(argv, 0, stderr="")

    with tempfile.TemporaryDirectory() as directory:
      destination = Path(directory) / "micromamba.tar.bz2"
      with self.assertRaisesRegex(
          RuntimeError,
          r"https://primary\.test/micromamba.*size.*67108864",
      ):
        tasks._download_micromamba_archive(
            "https://primary.test/micromamba",
            destination,
            expected_sha256="0" * 64,
            run=run,
            sleep=lambda _: None,
        )

    self.assertIn("--max-filesize", calls[0])
    self.assertIn("67108864", calls[0])

  def test_archive_hashing_streams_without_reading_the_whole_file(self):
    """Break caught: archive hashing regresses to Path.read_bytes materialization."""
    archive = _archive_bytes()
    digest = hashlib.sha256(archive).hexdigest()

    def run(argv, **kwargs):
      Path(argv[argv.index("-o") + 1]).write_bytes(archive)
      return subprocess.CompletedProcess(argv, 0, stderr="")

    with tempfile.TemporaryDirectory() as directory, mock.patch.object(
        Path, "read_bytes", side_effect=AssertionError("whole-file read"),
    ):
      destination = Path(directory) / "micromamba.tar.bz2"
      tasks._download_micromamba_archive(
          "https://primary.test/micromamba",
          destination,
          expected_sha256=digest,
          run=run,
          sleep=lambda _: None,
      )

  def test_archive_validation_rejects_unsafe_paths_and_special_entries(self):
    """Break caught: Python 3.10 tar extraction accepts unsafe or special members."""
    special_members = []
    symlink = tarfile.TarInfo("bin/link")
    symlink.type = tarfile.SYMTYPE
    symlink.linkname = "micromamba"
    special_members.append(symlink)
    hardlink = tarfile.TarInfo("bin/hardlink")
    hardlink.type = tarfile.LNKTYPE
    hardlink.linkname = "bin/micromamba"
    special_members.append(hardlink)
    for entry_type in (tarfile.FIFOTYPE, tarfile.CHRTYPE, tarfile.BLKTYPE):
      special = tarfile.TarInfo("bin/special")
      special.type = entry_type
      special_members.append(special)

    traversal = tarfile.TarInfo("../escape")
    traversal.size = 1
    special_members.append(traversal)
    absolute = tarfile.TarInfo("/absolute")
    absolute.size = 1
    special_members.append(absolute)

    for unsafe in special_members:
      with self.subTest(member=unsafe.name, type=unsafe.type):
        payload = _archive_with_members([unsafe])
        with tarfile.open(fileobj=io.BytesIO(payload), mode="r:bz2") as archive:
          with self.assertRaisesRegex(ValueError, r"Unsafe archive"):
            tasks._validate_micromamba_archive_members(archive, "bin/micromamba")

  def test_archive_validation_requires_regular_expected_executable(self):
    """Break caught: extraction proceeds without the exact regular executable member."""
    missing = tarfile.TarInfo("bin/other")
    missing.size = 1
    payload = _archive_with_members([missing])
    with tarfile.open(fileobj=io.BytesIO(payload), mode="r:bz2") as archive:
      with self.assertRaisesRegex(ValueError, r"expected executable member"):
        tasks._validate_micromamba_archive_members(archive, "bin/micromamba")

    directory = tarfile.TarInfo("bin/micromamba")
    directory.type = tarfile.DIRTYPE
    payload = _archive_with_members([directory])
    with tarfile.open(fileobj=io.BytesIO(payload), mode="r:bz2") as archive:
      with self.assertRaisesRegex(ValueError, r"expected executable member"):
        tasks._validate_micromamba_archive_members(archive, "bin/micromamba")

  def test_micromamba_base_override_remains_the_primary_url(self):
    """Break caught: fallback hardening silently discards RCC_MICROMAMBA_BASE."""
    with mock.patch.dict("os.environ", {"RCC_MICROMAMBA_BASE": "https://mirror.test/root/"}):
      self.assertEqual(
          tasks.get_official_micromamba_url("v2.5.0", "linux64"),
          "https://mirror.test/root/linux-64/2.5.0",
      )

  def test_fallback_metadata_is_bound_to_exact_release_archives(self):
    """Break caught: a mutable or wrong-version fallback bypasses integrity binding."""
    expected = {
        "linux64": (
            "https://github.com/mamba-org/micromamba-releases/releases/download/2.5.0-2/"
            "micromamba-linux-64.tar.bz2",
            "cec496f2299f9ceb5f5e23fc2ccf081fffda0c4bc87a6fdab575bf1e04f103b6",
        ),
        "macos64": (
            "https://github.com/mamba-org/micromamba-releases/releases/download/2.5.0-2/"
            "micromamba-osx-64.tar.bz2",
            "2d6ab968ba08c003e43642f55afded1f0fc4247387f04e340cdc6a0129d9c550",
        ),
        "macosarm64": (
            "https://github.com/mamba-org/micromamba-releases/releases/download/2.5.0-2/"
            "micromamba-osx-arm64.tar.bz2",
            "74d1f250b1505cd4d78728bd8b0b7f0ffbaa660b18042f15f7b610c16102d201",
        ),
        "windows64": (
            "https://github.com/mamba-org/micromamba-releases/releases/download/2.5.0-2/"
            "micromamba-win-64.tar.bz2",
            "8a53345b37016e4303d3ecf243ff620380971dbe24d71ea849ec1d6957d15883",
        ),
    }
    for platform, metadata in expected.items():
      self.assertEqual(tasks._get_official_micromamba_fallback("v2.5.0", platform), metadata)
    for version, platform in (("v2.5.1", "linux64"), ("v2.5.0", "unknown")):
      with self.assertRaisesRegex(ValueError, "no integrity-bound micromamba fallback"):
        tasks._get_official_micromamba_fallback(version, platform)

  def test_primary_exhaustion_uses_checksum_validated_fallback_without_post_success_sleep(self):
    """Break caught: exhausted primary retries never try the trusted fallback."""
    archive = _archive_bytes()
    digest = hashlib.sha256(archive).hexdigest()
    calls = []
    sleeps = []

    def run(argv, **kwargs):
      url = argv[argv.index("--location") + 1]
      calls.append(url)
      if url == "https://primary.test/micromamba":
        return subprocess.CompletedProcess(argv, 22, stderr="curl: HTTP 503")
      Path(argv[argv.index("-o") + 1]).write_bytes(archive)
      return subprocess.CompletedProcess(argv, 0, stderr="")

    with tempfile.TemporaryDirectory() as directory:
      destination = Path(directory) / "micromamba.tar.bz2"
      tasks._download_micromamba_archive(
          "https://primary.test/micromamba",
          destination,
          expected_sha256=digest,
          fallback_url="https://fallback.test/micromamba.tar.bz2",
          run=run,
          sleep=sleeps.append,
      )
      self.assertEqual(hashlib.sha256(destination.read_bytes()).hexdigest(), digest)

    self.assertEqual(calls, ["https://primary.test/micromamba"] * 3 + [
        "https://fallback.test/micromamba.tar.bz2",
    ])
    self.assertEqual(sleeps, [10, 20])

  def test_all_sources_exhaustion_reports_primary_and_fallback(self):
    """Break caught: fallback failure hides which integrity-bound sources failed."""
    calls = []
    sleeps = []

    def run(argv, **kwargs):
      calls.append(argv[argv.index("--location") + 1])
      return subprocess.CompletedProcess(argv, 22, stderr="curl: HTTP 503")

    with tempfile.TemporaryDirectory() as directory:
      destination = Path(directory) / "micromamba.tar.bz2"
      with self.assertRaisesRegex(
          RuntimeError,
          r"primary\.test/micromamba.*fallback\.test/micromamba\.tar\.bz2",
      ):
        tasks._download_micromamba_archive(
            "https://primary.test/micromamba",
            destination,
            expected_sha256="0" * 64,
            fallback_url="https://fallback.test/micromamba.tar.bz2",
            run=run,
            sleep=sleeps.append,
        )

    self.assertEqual(len(calls), 6)
    self.assertEqual(sleeps, [10, 20, 10, 20])

  def test_checksum_mismatch_rejects_valid_archive_from_every_source(self):
    """Break caught: structurally valid but untrusted archive content is accepted."""
    archive = _archive_bytes()
    calls = []

    def run(argv, **kwargs):
      calls.append(argv[argv.index("--location") + 1])
      Path(argv[argv.index("-o") + 1]).write_bytes(archive)
      return subprocess.CompletedProcess(argv, 0, stderr="")

    with tempfile.TemporaryDirectory() as directory:
      destination = Path(directory) / "micromamba.tar.bz2"
      with self.assertRaisesRegex(RuntimeError, r"SHA-256 mismatch"):
        tasks._download_micromamba_archive(
            "https://primary.test/micromamba",
            destination,
            expected_sha256="0" * 64,
            fallback_url="https://fallback.test/micromamba.tar.bz2",
            run=run,
            sleep=lambda _: None,
        )

    self.assertEqual(len(calls), 6)

  def test_transient_http_failure_is_retried_until_valid_archive_arrives(self):
    """Break caught: one transient HTTP failure aborts instead of retrying."""
    archive = _archive_bytes()
    responses = [(22, b"HTTP 503"), (0, archive)]
    calls = []
    sleeps = []

    def run(argv, **kwargs):
      calls.append((argv, kwargs))
      returncode, payload = responses.pop(0)
      Path(argv[argv.index("-o") + 1]).write_bytes(payload)
      return subprocess.CompletedProcess(argv, returncode, stderr="HTTP failure")

    with tempfile.TemporaryDirectory() as directory:
      destination = Path(directory) / "micromamba.tar.bz2"
      tasks._download_micromamba_archive(
          "https://example.test/micromamba", destination,
          expected_sha256=hashlib.sha256(archive).hexdigest(), run=run, sleep=sleeps.append,
      )
      with tarfile.open(destination, "r:bz2") as archive:
        self.assertEqual(archive.extractfile("bin/micromamba").read(), b"micromamba")

    self.assertEqual(len(calls), 2)
    self.assertEqual(sleeps, [10])
    self.assertIn("--fail", calls[0][0])

  def test_exhausted_http_failures_report_url_and_attempt_count(self):
    """Break caught: HTTP errors are accepted as archives or fail unclearly."""
    calls = []
    sleeps = []

    def run(argv, **kwargs):
      calls.append((argv, kwargs))
      Path(argv[argv.index("-o") + 1]).write_bytes(b"HTTP 500")
      return subprocess.CompletedProcess(argv, 22, stderr="curl: HTTP 500")

    with tempfile.TemporaryDirectory() as directory:
      destination = Path(directory) / "micromamba.tar.bz2"
      with self.assertRaisesRegex(
          RuntimeError,
          r"failed after 3 attempts.*https://example\.test/micromamba.*curl: HTTP 500",
      ):
        tasks._download_micromamba_archive(
            "https://example.test/micromamba", destination,
            expected_sha256="0" * 64, run=run, sleep=sleeps.append,
        )

    self.assertEqual(len(calls), 3)
    self.assertEqual(sleeps, [10, 20])

  def test_malformed_success_payload_is_retried_then_reported_as_invalid_archive(self):
    """Break caught: an HTTP 200 text payload reaches extraction as a bzip2 error."""
    payload = b"not a bzip2 tar archive"
    calls = []
    sleeps = []

    def run(argv, **kwargs):
      calls.append((argv, kwargs))
      Path(argv[argv.index("-o") + 1]).write_bytes(payload)
      return subprocess.CompletedProcess(argv, 0, stderr="")

    with tempfile.TemporaryDirectory() as directory:
      destination = Path(directory) / "micromamba.tar.bz2"
      with self.assertRaisesRegex(
          RuntimeError,
          r"failed after 3 attempts.*not a valid bzip2 tar archive",
      ):
        tasks._download_micromamba_archive(
            "https://example.test/micromamba", destination,
            expected_sha256=hashlib.sha256(payload).hexdigest(), run=run, sleep=sleeps.append,
        )

    self.assertEqual(len(calls), 3)
    self.assertEqual(sleeps, [10, 20])


if __name__ == "__main__":
  unittest.main()
