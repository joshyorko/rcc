import os
import tempfile
import unittest
from pathlib import Path
from unittest import mock

from scripts import toc


class TocOrderingTests(unittest.TestCase):
    def test_shuffled_glob_order_has_stable_output_and_keeps_priorities(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            docs = root / "docs"
            docs.mkdir()
            titles = {
                "usecases.md": "Use cases",
                "features.md": "Features",
                "alpha.md": "Alpha",
                "zeta.md": "Zeta",
                "README.md": "Ignored README",
                "toc.md": "Ignored TOC",
                "changelog.md": "Ignored changelog",
                "BUILD.md": "Ignored build notes",
            }
            for name, title in titles.items():
                (docs / name).write_text(f"# {title}\n", encoding="utf-8")
            for priority in toc.PRIORITY_LIST:
                priority_path = docs / Path(priority).name
                if not priority_path.exists():
                    priority_path.write_text("", encoding="utf-8")

            first_glob_order = [
                "zeta.md", "features.md", "README.md", "alpha.md",
                "usecases.md", "changelog.md", "BUILD.md", "toc.md",
            ]
            second_glob_order = list(reversed(first_glob_order))

            def generate(names):
                with mock.patch.object(
                    toc.glob,
                    "glob",
                    return_value=[f"docs/{name}" for name in names],
                ):
                    previous = Path.cwd()
                    try:
                        os.chdir(root)
                        toc.process()
                        return (docs / "toc.md").read_bytes()
                    finally:
                        os.chdir(previous)

            first = generate(first_glob_order)
            second = generate(second_glob_order)
            self.assertEqual(first, second)
            output = first.decode("utf-8")
            self.assertLess(output.index("## 1 [Use cases]"), output.index("## 2 [Features]"))
            self.assertLess(output.index("## 3 [Alpha]"), output.index("## 4 [Zeta]"))
            self.assertIn("(usecases.md#use-cases)", output)
            self.assertNotIn("https://github.com/joshyorko/rcc/blob/main/", output)
            for ignored_title in ("Ignored README", "Ignored TOC", "Ignored changelog", "Ignored build notes"):
                self.assertNotIn(ignored_title, output)

    def test_github_compatible_slugs_and_repeated_headings(self):
        self.assertEqual(toc.unify("Tip: Python-only `uv`-native mode"), "tip-python-only-uv-native-mode")
        self.assertEqual(
            toc.unify("Terminal 1 — Start a loopback artifact provider"),
            "terminal-1--start-a-loopback-artifact-provider",
        )
        self.assertEqual(
            toc.unify("RCC Capability & Documentation Inventory"),
            "rcc-capability--documentation-inventory",
        )
        self.assertEqual(
            toc.unify("2. PROVEN ON MAIN (Post-`v18.19.11` Delta)"),
            "2-proven-on-main-post-v181911-delta",
        )
        self.assertEqual(toc.unify("Run it with `--` separator."), "run-it-with----separator")

        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            docs = root / "docs"
            docs.mkdir()
            (docs / "sample.md").write_text(
                "# Sample\n## Limitations\n## `uv`-native & Notes\n## Limitations\n",
                encoding="utf-8",
            )
            previous = Path.cwd()
            try:
                os.chdir(root)
                toc.process()
                output = (docs / "toc.md").read_text(encoding="utf-8")
            finally:
                os.chdir(previous)

            self.assertIn("(sample.md#limitations)", output)
            self.assertIn("(sample.md#uv-native--notes)", output)
            self.assertIn("(sample.md#limitations-1)", output)


if __name__ == "__main__":
    unittest.main()
