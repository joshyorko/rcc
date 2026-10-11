import os
import shutil
import subprocess
import sys
import tempfile
import unittest
import xml.etree.ElementTree as ET
from pathlib import Path


class DevelopmentProcessRobotTest(unittest.TestCase):
    """Run the production Robot check with the built RCC in isolated Git repos."""

    @classmethod
    def setUpClass(cls):
        cls.repo_root = Path(__file__).resolve().parents[1]
        binary_override = os.environ.get("RCC_ACCEPTANCE_BINARY")
        cls.binary = (
            Path(binary_override).expanduser().resolve()
            if binary_override
            else cls.repo_root / "build" / ("rcc.exe" if os.name == "nt" else "rcc")
        )
        if not cls.binary.is_file() or not os.access(cls.binary, os.X_OK):
            raise AssertionError(f"RCC_ACCEPTANCE_BINARY must name an executable file: {cls.binary}")
        cls.robot_dir = cls.repo_root / "robot_tests"
        cls.rcc_version = subprocess.run(
            [str(cls.binary), "--version"],
            check=True,
            capture_output=True,
            text=True,
        ).stdout.strip()
        if not cls.rcc_version or "\n" in cls.rcc_version or "\r" in cls.rcc_version:
            raise AssertionError(f"built RCC reported an invalid version: {cls.rcc_version!r}")

    def test_accepts_regular_changelog_commit(self):
        self._assert_robot_result("normal", should_pass=True)

    def test_accepts_merge_when_current_changelog_is_tracked_in_tree(self):
        self._assert_robot_result("merge", should_pass=True)

    def test_rejects_missing_tracked_changelog(self):
        self._assert_robot_result("missing", should_pass=False)

    def test_rejects_untracked_matching_changelog(self):
        self._assert_robot_result("untracked", should_pass=False)

    def test_rejects_wrong_version_heading(self):
        self._assert_robot_result("wrong-version", should_pass=False)

    def _assert_robot_result(self, layout, should_pass):
        with tempfile.TemporaryDirectory(prefix="rcc-development-process-") as temp:
            fixture = Path(temp)
            self._copy_test_inputs(fixture)
            self._make_git_fixture(fixture, layout)

            if layout == "merge":
                tracked = self._git(fixture, "show", "HEAD:docs/changelog.md")
                stat = self._git(fixture, "show", "--stat", "HEAD")
                self.assertIn(f"## {self.rcc_version}", tracked)
                self.assertNotIn("docs/changelog.md", stat)
            if layout == "untracked":
                self.assertTrue((fixture / "docs" / "changelog.md").is_file())
                self.assertEqual(self._git(fixture, "ls-files", "docs/changelog.md"), "")

            output_dir = fixture / "robot-output"
            result = subprocess.run(
                [
                    sys.executable,
                    "-m",
                    "robot",
                    "--outputdir",
                    str(output_dir),
                    "robot_tests/development_process.robot",
                ],
                cwd=fixture,
                capture_output=True,
                text=True,
            )

            output_xml = output_dir / "output.xml"
            self.assertTrue(output_xml.is_file(), f"Robot output missing: {result.stdout}\n{result.stderr}")
            tests = list(ET.parse(output_xml).getroot().iter("test"))
            self.assertEqual(len(tests), 1, result.stdout + result.stderr)
            status = tests[0].find("status")
            actual_passed = status is not None and status.get("status") == "PASS"
            self.assertEqual(
                actual_passed,
                should_pass,
                f"layout={layout}, exit={result.returncode}, XML={ET.tostring(tests[0], encoding='unicode')}, "
                f"stdout={result.stdout!r}, stderr={result.stderr!r}",
            )
            self.assertEqual((result.returncode == 0), should_pass, result.stdout + result.stderr)

    def _copy_test_inputs(self, fixture):
        (fixture / "build").mkdir()
        (fixture / "robot_tests").mkdir()
        fixture_binary_name = "rcc.exe" if os.name == "nt" else "rcc"
        shutil.copy2(self.binary, fixture / "build" / fixture_binary_name)
        for name in ("development_process.robot", "resources.robot", "supporting.py"):
            shutil.copy2(self.robot_dir / name, fixture / "robot_tests" / name)

    def _make_git_fixture(self, fixture, layout):
        self._git(fixture, "init", "--initial-branch=main")
        self._git(fixture, "config", "user.name", "RCC Acceptance Fixture")
        self._git(fixture, "config", "user.email", "fixture@example.invalid")
        (fixture / "tracked.txt").write_text("fixture\n", encoding="utf-8")
        self._git(fixture, "add", "tracked.txt")
        self._git(fixture, "commit", "-m", "initialize fixture")

        if layout == "missing":
            return
        if layout == "untracked":
            self._write_changelog(fixture, self.rcc_version)
            return
        if layout == "wrong-version":
            wrong_version = "v0.0.0"
            if wrong_version == self.rcc_version:
                wrong_version = "v0.0.1"
            self._write_changelog(fixture, wrong_version)
            self._git(fixture, "add", "docs/changelog.md")
            self._git(fixture, "commit", "-m", "add prior-version changelog")
            return
        if layout == "normal":
            self._write_changelog(fixture, self.rcc_version)
            self._git(fixture, "add", "docs/changelog.md")
            self._git(fixture, "commit", "-m", "add current changelog")
            return
        if layout == "merge":
            self._write_changelog(fixture, self.rcc_version)
            self._git(fixture, "add", "docs/changelog.md")
            self._git(fixture, "commit", "-m", "add current changelog")
            self._git(fixture, "checkout", "-b", "feature")
            (fixture / "feature.txt").write_text("feature change\n", encoding="utf-8")
            self._git(fixture, "add", "feature.txt")
            self._git(fixture, "commit", "-m", "change feature file")
            self._git(fixture, "checkout", "main")
            (fixture / "mainline.txt").write_text("mainline change\n", encoding="utf-8")
            self._git(fixture, "add", "mainline.txt")
            self._git(fixture, "commit", "-m", "change mainline file")
            self._git(fixture, "merge", "--no-ff", "feature", "-m", "merge feature")
            return
        raise ValueError(f"unsupported fixture layout: {layout}")

    @staticmethod
    def _write_changelog(fixture, version):
        changelog = fixture / "docs" / "changelog.md"
        changelog.parent.mkdir(parents=True, exist_ok=True)
        changelog.write_text(
            f"# rcc change log\n## Unreleased\n\n## {version} (date: 11.10.2026)\n",
            encoding="utf-8",
        )

    @staticmethod
    def _git(fixture, *args, check=True):
        result = subprocess.run(
            ["git", "-c", "commit.gpgsign=false", *args],
            cwd=fixture,
            check=check,
            capture_output=True,
            text=True,
        )
        return result.stdout


if __name__ == "__main__":
    unittest.main()
