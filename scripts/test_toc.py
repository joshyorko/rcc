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
                        return (docs / "README.md").read_bytes()
                    finally:
                        os.chdir(previous)

            first = generate(first_glob_order)
            second = generate(second_glob_order)
            self.assertEqual(first, second)
            output = first.decode("utf-8")
            self.assertLess(output.index("## 1 [Use cases]"), output.index("## 2 [Features]"))
            self.assertLess(output.index("## 3 [Alpha]"), output.index("## 4 [Zeta]"))
            for ignored_title in ("Ignored README", "Ignored TOC", "Ignored changelog", "Ignored build notes"):
                self.assertNotIn(ignored_title, output)


if __name__ == "__main__":
    unittest.main()
