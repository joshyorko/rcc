import json
import shutil
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).parent))
from validate_release_topology import validate


ASSETS = {
    "rcc-linux64", "rccremote-linux64", "rcc-windows64.exe", "rccremote-windows64.exe",
    "rcc-macos64", "rccremote-macos64", "rcc-macosarm64", "rccremote-macosarm64",
}
REPOSITORY_ROOT = Path(__file__).parents[1]


class ReleaseTopologyTests(unittest.TestCase):
    def fixture(self):
        root = Path(tempfile.mkdtemp())
        for directory in ("linux64", "windows64", "macos64", "macosarm64"):
            (root / "build" / directory).mkdir(parents=True)
        for name in ASSETS:
            target = {"linux": "linux64", "windows": "windows64", "macos64": "macos64", "macosarm64": "macosarm64"}
            if "windows" in name:
                directory = "windows64"
            elif "macosarm64" in name:
                directory = "macosarm64"
            elif "macos64" in name:
                directory = "macos64"
            else:
                directory = "linux64"
            source = root / "build" / directory / ("rccremote.exe" if "rccremote" in name and directory == "windows64" else "rcc.exe" if directory == "windows64" else "rccremote" if "rccremote" in name else "rcc")
            source.write_bytes(b"binary")
        workflow = root / "workflow.yml"
        workflow.write_text("\n".join(f"cp build/{d}/{s} rcc-builds/{n}" for d, s, n in [
            ("linux64", "rcc", "rcc-linux64"), ("linux64", "rccremote", "rccremote-linux64"),
            ("windows64", "rcc.exe", "rcc-windows64.exe"), ("windows64", "rccremote.exe", "rccremote-windows64.exe"),
            ("macos64", "rcc", "rcc-macos64"), ("macos64", "rccremote", "rccremote-macos64"),
            ("macosarm64", "rcc", "rcc-macosarm64"), ("macosarm64", "rccremote", "rccremote-macosarm64"),
        ]))
        index = root / "index.json"
        index.write_text(json.dumps({"tested": [{"windows": "https://x/rcc-windows64.exe", "linux": "https://x/rcc-linux64", "macos": "https://x/rcc-macos64", "macosarm64": "https://x/rcc-macosarm64"}]}))
        return root, workflow, index

    def test_valid_fixture(self):
        root, workflow, index = self.fixture()
        self.assertEqual(validate(root, workflow, index), [])

    def test_repository_workflow_stages_exact_current_assets(self):
        root, _, index = self.fixture()
        self.assertEqual(validate(root, REPOSITORY_ROOT / ".github/workflows/rcc.yaml", index), [])

    def test_repository_workflow_runs_all_release_platforms_natively(self):
        workflow = (REPOSITORY_ROOT / ".github/workflows/rcc.yaml").read_text()
        for runner in ("ubuntu-latest", "windows-latest", "macos-latest", "macos-13"):
            self.assertIn(f'"{runner}"', workflow)

    def test_repository_workflow_marks_rc_tags_as_prereleases(self):
        workflow = (REPOSITORY_ROOT / ".github/workflows/rcc.yaml").read_text()
        self.assertIn("prerelease: ${{ contains(github.ref_name, '-rc.') }}", workflow)

    def test_repository_release_waits_for_native_robot_matrix(self):
        workflow = (REPOSITORY_ROOT / ".github/workflows/rcc.yaml").read_text()
        release_job = workflow.split("\n  release:\n", 1)[1]
        self.assertRegex(release_job, r"needs:\s*\n(?:\s+- .+\n)*\s+- robot\n")

    def test_release_job_validates_downloaded_assets_without_build_tree(self):
        root, workflow, index = self.fixture()
        assets = root / "rcc-builds"
        assets.mkdir()
        for name in ASSETS:
            (assets / name).write_bytes(b"downloaded release asset")
        published_index = assets / "index.json"
        shutil.move(index, published_index)
        shutil.rmtree(root / "build")

        result = subprocess.run([
            sys.executable,
            str(REPOSITORY_ROOT / "scripts" / "validate_release_topology.py"),
            "--asset-root", str(assets),
            "--workflow", str(workflow),
            "--index", str(published_index),
        ], text=True, capture_output=True)
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)

    def test_release_job_rejects_unexpected_asset_directory(self):
        root, workflow, index = self.fixture()
        assets = root / "rcc-builds"
        assets.mkdir()
        for name in ASSETS:
            (assets / name).write_bytes(b"downloaded release asset")
        published_index = assets / "index.json"
        shutil.move(index, published_index)
        (assets / "unexpected-directory").mkdir()

        errors = validate(None, workflow, published_index, assets)
        self.assertTrue(any("downloaded assets mismatch" in error for error in errors), errors)

    def test_release_job_rejects_symlinked_asset(self):
        root, workflow, index = self.fixture()
        assets = root / "rcc-builds"
        assets.mkdir()
        for name in ASSETS:
            (assets / name).write_bytes(b"downloaded release asset")
        published_index = assets / "index.json"
        shutil.move(index, published_index)
        target = assets / "rcc-linux64-target"
        (assets / "rcc-linux64").rename(target)
        (assets / "rcc-linux64").symlink_to(target.name)

        errors = validate(None, workflow, published_index, assets)
        self.assertTrue(any("not a regular file" in error for error in errors), errors)

    def test_rejects_darwin_index_and_extra_asset(self):
        root, workflow, index = self.fixture()
        (root / "rcc-builds").mkdir()
        (root / "rcc-builds" / "unexpected").write_bytes(b"x")
        index.write_text(json.dumps({"tested": [{"macos": "https://x/rcc-darwin64"}]}))
        errors = validate(root, workflow, index)
        self.assertTrue(any("staged" in error or "basename" in error for error in errors))

    def test_requires_macosarm64_index_asset(self):
        root, workflow, index = self.fixture()
        index.write_text(json.dumps({"tested": [{"windows": "https://x/rcc-windows64.exe", "linux": "https://x/rcc-linux64", "macos": "https://x/rcc-macos64"}]}))
        self.assertTrue(any("macosarm64" in error for error in validate(root, workflow, index)))

    def test_rejects_index_urls_for_the_wrong_product_or_platform(self):
        root, workflow, index = self.fixture()
        index.write_text(json.dumps({"tested": [{
            "windows": "https://x/rccremote-windows64.exe",
            "linux": "https://x/rccremote-linux64",
            "macos": "https://x/rccremote-macosarm64",
            "macosarm64": "https://x/rccremote-macos64",
        }]}))
        errors = validate(root, workflow, index)
        self.assertEqual(len(errors), 4, errors)
        for key in ("windows", "linux", "macos", "macosarm64"):
            self.assertTrue(any(f"index {key} URL" in error for error in errors), errors)

    def test_rejects_extra_workflow_asset(self):
        root, workflow, index = self.fixture()
        workflow.write_text(workflow.read_text() + "\ncp build/linux64/rcc rcc-builds/extra-rcc\n")
        self.assertTrue(any("workflow staged assets mismatch" in error for error in validate(root, workflow, index)))

    def test_accepts_historical_entry_without_macosarm64(self):
        root, workflow, index = self.fixture()
        index.write_text(json.dumps({"tested": [
            {"windows": "https://x/rcc-windows64.exe", "linux": "https://x/rcc-linux64", "macos": "https://x/rcc-macos64", "macosarm64": "https://x/rcc-macosarm64"},
            {"windows": "https://x/rcc-windows64.exe", "linux": "https://x/rcc-linux64", "macos": "https://x/rcc-macos64"},
        ]}))
        self.assertEqual(validate(root, workflow, index), [])

    def test_accepts_historical_darwin_asset_name(self):
        root, workflow, index = self.fixture()
        index.write_text(json.dumps({"tested": [
            {"windows": "https://x/rcc-windows64.exe", "linux": "https://x/rcc-linux64", "macos": "https://x/rcc-macos64", "macosarm64": "https://x/rcc-macosarm64"},
            {"windows": "https://x/rcc-windows64.exe", "linux": "https://x/rcc-linux64", "macos": "https://x/rcc-darwin64"},
        ]}))
        self.assertEqual(validate(root, workflow, index), [])


if __name__ == "__main__":
    unittest.main()
