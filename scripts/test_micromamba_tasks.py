import io
from contextlib import contextmanager
import gzip
import os
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


@contextmanager
def _working_directory(directory):
  """Restore the caller's cwd before temporary-directory cleanup, including on Windows."""
  previous = Path.cwd()
  try:
    os.chdir(directory)
    yield
  finally:
    os.chdir(previous)


class MicromambaDownloadTests(unittest.TestCase):
  def test_asset_generation_replaces_stale_binaries_and_reuses_verified_bytes(self):
    """Reject stale or corrupt executables while retaining verified, reproducible assets."""
    archive = _archive_bytes()
    binary_digest = hashlib.sha256(b"micromamba").hexdigest()
    arches = ("linux_amd64", "darwin_amd64", "darwin_arm64", "windows_amd64")

    def download(url, destination, **kwargs):
      """Supply the real archive layouts without a network dependency."""
      # The Windows archive has a different executable member.
      if "/win-64/" in url:
        stream = io.BytesIO()
        with tarfile.open(fileobj=stream, mode="w:bz2") as tar:
          member = tarfile.TarInfo("Library/bin/micromamba.exe")
          member.size = len(b"micromamba")
          tar.addfile(member, io.BytesIO(b"micromamba"))
        Path(destination).write_bytes(stream.getvalue())
      else:
        Path(destination).write_bytes(archive)

    with tempfile.TemporaryDirectory() as directory, _working_directory(directory):
      Path("assets").mkdir()
      Path("assets/micromamba_version.txt").write_text("v2.10.0\n")
      Path("blobs/assets").mkdir(parents=True)
      outputs = [Path(f"blobs/assets/micromamba.{arch}.gz") for arch in arches]
      for output in outputs:
        output.write_bytes(gzip.compress(b"stale 2.5.0"))
      with mock.patch.object(tasks, "MICROMAMBA_BINARY_SHA256", {
          platform: binary_digest for platform in ("linux64", "macos64", "macosarm64", "windows64")
      }, create=True), mock.patch.object(tasks, "_download_micromamba_archive", side_effect=download) as fetch:
        tasks.micromamba.body(None)
        self.assertEqual(fetch.call_count, 4)
        first = [output.read_bytes() for output in outputs]
        for content in first:
          self.assertEqual(gzip.decompress(content), b"micromamba")
        fetch.reset_mock()
        tasks.micromamba.body(None)
        fetch.assert_not_called()
        # Corruption must trigger regeneration, with deterministic gzip bytes.
        outputs[0].write_bytes(b"truncated gzip")
        tasks.micromamba.body(None)
        self.assertEqual(fetch.call_count, 1)
        self.assertEqual(outputs[0].read_bytes(), first[0])
        fetch.reset_mock()
        corrupt = bytearray(first[0])
        corrupt[10] = 7  # Invalid deflate block, after a valid gzip header.
        outputs[0].write_bytes(corrupt)
        tasks.micromamba.body(None)
        self.assertEqual(fetch.call_count, 1)
        self.assertEqual(outputs[0].read_bytes(), first[0])
        outputs[0].write_bytes(gzip.compress(b"old"))
        fetch.side_effect = RuntimeError("unavailable")
        with self.assertRaisesRegex(RuntimeError, "unavailable"):
          tasks.micromamba.body(None)
        self.assertEqual(gzip.decompress(outputs[0].read_bytes()), b"old")
        fetch.side_effect = download
        tasks.MICROMAMBA_BINARY_SHA256["linux64"] = "0" * 64
        with self.assertRaisesRegex(ValueError, "executable SHA-256 mismatch"):
          tasks.micromamba.body(None)
        self.assertEqual(gzip.decompress(outputs[0].read_bytes()), b"old")
        Path("assets/micromamba_version.txt").write_text("v2.10.1\n")
        with self.assertRaisesRegex(ValueError, "no integrity-bound micromamba fallback"):
          tasks.micromamba.body(None)

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

  def test_archive_validation_rejects_windows_drive_relative_paths(self):
    """Break caught: D:escape can resolve outside the extraction root on Windows."""
    drive_relative = tarfile.TarInfo("D:escape")
    drive_relative.size = 1
    payload = _archive_with_members([drive_relative])
    with tarfile.open(fileobj=io.BytesIO(payload), mode="r:bz2") as archive:
      with self.assertRaisesRegex(ValueError, r"drive-relative"):
        tasks._validate_micromamba_archive_members(archive, "bin/micromamba")

  def test_archive_validation_rejects_windows_drive_absolute_paths(self):
    """Break caught: drive-qualified absolute names must not reach extraction."""
    for name in ("C:/escape", "C:\\escape", "\\\\server\\share\\escape"):
      with self.subTest(member=name):
        member = tarfile.TarInfo(name)
        member.size = 1
        payload = _archive_with_members([member])
        with tarfile.open(fileobj=io.BytesIO(payload), mode="r:bz2") as archive:
          with self.assertRaisesRegex(ValueError, r"Unsafe archive"):
            tasks._validate_micromamba_archive_members(archive, "bin/micromamba")

  def test_archive_validation_accepts_legitimate_relative_regular_members(self):
    """Break caught: drive checks accidentally reject valid relative archive members."""
    directory = tarfile.TarInfo("bin")
    directory.type = tarfile.DIRTYPE
    executable = tarfile.TarInfo("bin/micromamba")
    executable.size = 1
    data = tarfile.TarInfo("bin/data")
    data.size = 1
    payload = _archive_with_members([directory, executable, data])
    with tarfile.open(fileobj=io.BytesIO(payload), mode="r:bz2") as archive:
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
          tasks.get_official_micromamba_url("v2.10.0", "linux64"),
          "https://mirror.test/root/linux-64/2.10.0",
      )

  def test_fallback_metadata_is_bound_to_exact_release_archives(self):
    """Break caught: a mutable or wrong-version fallback bypasses integrity binding."""
    expected = {
        "linux64": (
            "https://github.com/mamba-org/micromamba-releases/releases/download/2.10.0-0/"
            "micromamba-linux-64.tar.bz2",
            "9a97976244929bbae28fb1fae504e74a3e7e6d1932870e3a03321f01b7fbd07b",
        ),
        "macos64": (
            "https://github.com/mamba-org/micromamba-releases/releases/download/2.10.0-0/"
            "micromamba-osx-64.tar.bz2",
            "17b20c4271d65c030a5c86788f5ca86eb575187209f626edabdb8c6a875c6838",
        ),
        "macosarm64": (
            "https://github.com/mamba-org/micromamba-releases/releases/download/2.10.0-0/"
            "micromamba-osx-arm64.tar.bz2",
            "acef4eaab19ec2174ad5a449e5aad981ca326270db819502ff096a92fc31caa6",
        ),
        "windows64": (
            "https://github.com/mamba-org/micromamba-releases/releases/download/2.10.0-0/"
            "micromamba-win-64.tar.bz2",
            "6283f25f8142606d86e81a43c7103b5d4f1d5c2027e3f9d7326eff23b33930d8",
        ),
    }
    for platform, metadata in expected.items():
      self.assertEqual(tasks._get_official_micromamba_fallback("v2.10.0", platform), metadata)
    for version, platform in (("v2.10.1", "linux64"), ("v2.10.0", "unknown")):
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


class MicromambaAcceptanceBinaryTests(unittest.TestCase):
  """Keep acceptance bound to the candidate and preserve staged CI artifacts."""

  def test_local_acceptance_builds_current_candidate_even_if_missing_or_stale(self):
    """Standalone acceptance must replace stale bytes and build an absent candidate."""
    for stale in (False, True):
      with self.subTest(stale=stale), tempfile.TemporaryDirectory() as directory, _working_directory(directory):
        binary = Path("build/rcc.exe" if os.name == "nt" else "build/rcc")
        if stale:
          binary.parent.mkdir()
          binary.write_bytes(b"stale candidate")

        def run(argv, **kwargs):
          """Emulate only the build boundary while leaving observable candidate bytes."""
          if argv[:2] == ["go", "build"]:
            Path(argv[argv.index("-o") + 1]).write_bytes(b"current candidate")
          return subprocess.CompletedProcess(argv, 0)

        with mock.patch.dict(os.environ, {"RCC_REAL_BINARY": "", "RCC_REAL_BINARY_SHA256": ""}), mock.patch.object(tasks.subprocess, "run", side_effect=run):
          tasks.micromambaAcceptance.body(mock.Mock())
        self.assertTrue(binary.is_file(), "acceptance did not build the missing candidate")
        self.assertEqual(binary.read_bytes(), b"current candidate")

  def test_ci_acceptance_preserves_digest_verified_staged_candidate(self):
    """CI acceptance must execute its verified artifact without rebuilding it."""
    with tempfile.TemporaryDirectory() as directory, _working_directory(directory):
      binary = Path("build/rcc.exe" if os.name == "nt" else "build/rcc")
      binary.parent.mkdir()
      binary.write_bytes(b"staged CI candidate")

      def run(argv, **kwargs):
        """Refuse any build attempt while allowing the isolated test commands."""
        self.assertNotEqual(argv[:2], ["go", "build"], "must not overwrite the staged CI binary")
        return subprocess.CompletedProcess(argv, 0)

      with mock.patch.dict(os.environ, {"RCC_REAL_BINARY": str(binary.resolve()), "RCC_REAL_BINARY_SHA256": hashlib.sha256(binary.read_bytes()).hexdigest()}), mock.patch.object(tasks.subprocess, "run", side_effect=run):
        tasks.micromambaAcceptance.body(mock.Mock())
      self.assertEqual(binary.read_bytes(), b"staged CI candidate")

  def test_ci_acceptance_rejects_unbound_or_mismatched_candidate_without_overwrite(self):
    """Reject partial bindings, foreign paths and tampered bytes without modifying them."""
    with tempfile.TemporaryDirectory() as directory, _working_directory(directory):
      binary = Path("build/rcc.exe" if os.name == "nt" else "build/rcc")
      binary.parent.mkdir()
      binary.write_bytes(b"staged CI candidate")
      digest = hashlib.sha256(binary.read_bytes()).hexdigest()
      for staged, expected in ((str(binary.resolve()), "0" * 64), (str(binary.resolve()), ""), ("", digest), (str(Path("foreign-rcc").resolve()), digest)):
        with self.subTest(staged=staged, expected=expected), mock.patch.dict(os.environ, {"RCC_REAL_BINARY": staged, "RCC_REAL_BINARY_SHA256": expected}), mock.patch.object(tasks.subprocess, "run", return_value=subprocess.CompletedProcess([], 0)):
          with self.assertRaises(RuntimeError):
            tasks.micromambaAcceptance.body(mock.Mock())
          self.assertEqual(binary.read_bytes(), b"staged CI candidate")


if __name__ == "__main__":
  unittest.main()
