import io
import subprocess
import sys
import tarfile
import tempfile
import unittest
from pathlib import Path


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


class MicromambaDownloadTests(unittest.TestCase):
  def test_transient_http_failure_is_retried_until_valid_archive_arrives(self):
    """Break caught: one transient HTTP failure aborts instead of retrying."""
    responses = [(22, b"HTTP 503"), (0, _archive_bytes())]
    calls = []

    def run(argv, **kwargs):
      calls.append((argv, kwargs))
      returncode, payload = responses.pop(0)
      Path(argv[argv.index("-o") + 1]).write_bytes(payload)
      return subprocess.CompletedProcess(argv, returncode, stderr="HTTP failure")

    with tempfile.TemporaryDirectory() as directory:
      destination = Path(directory) / "micromamba.tar.bz2"
      tasks._download_micromamba_archive(
          "https://example.test/micromamba", destination, run=run, sleep=lambda _: None,
      )
      with tarfile.open(destination, "r:bz2") as archive:
        self.assertEqual(archive.extractfile("bin/micromamba").read(), b"micromamba")

    self.assertEqual(len(calls), 2)
    self.assertIn("--fail", calls[0][0])

  def test_exhausted_http_failures_report_url_and_attempt_count(self):
    """Break caught: HTTP errors are accepted as archives or fail unclearly."""
    calls = []

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
            "https://example.test/micromamba", destination, run=run, sleep=lambda _: None,
        )

    self.assertEqual(len(calls), 3)

  def test_malformed_success_payload_is_retried_then_reported_as_invalid_archive(self):
    """Break caught: an HTTP 200 text payload reaches extraction as a bzip2 error."""
    calls = []

    def run(argv, **kwargs):
      calls.append((argv, kwargs))
      Path(argv[argv.index("-o") + 1]).write_bytes(b"not a bzip2 tar archive")
      return subprocess.CompletedProcess(argv, 0, stderr="")

    with tempfile.TemporaryDirectory() as directory:
      destination = Path(directory) / "micromamba.tar.bz2"
      with self.assertRaisesRegex(
          RuntimeError,
          r"failed after 3 attempts.*not a valid bzip2 tar archive",
      ):
        tasks._download_micromamba_archive(
            "https://example.test/micromamba", destination, run=run, sleep=lambda _: None,
        )

    self.assertEqual(len(calls), 3)


if __name__ == "__main__":
  unittest.main()
