from __future__ import annotations

import contextlib
import importlib.util
import io
import tempfile
import unittest
from pathlib import Path
from unittest import mock

SCRIPT = Path(__file__).with_name("validate_agent_docs.py")
SPEC = importlib.util.spec_from_file_location("validate_agent_docs", SCRIPT)
assert SPEC and SPEC.loader
validate_agent_docs = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(validate_agent_docs)


class ValidateAgentDocsTests(unittest.TestCase):
    def write_valid_required_files(self, root: Path) -> None:
        skills_root = root / "docs" / "skills"
        skills_root.mkdir(parents=True, exist_ok=True)
        (skills_root / "README.md").write_text("# Skills\n", encoding="utf-8")
        receipt = "\n".join(
            (
                "Canonical guidance:",
                "Durable learning:",
                "Evidence:",
                "Stale guidance removed:",
                "Remaining uncertainty:",
            )
        )
        (root / "AGENTS.md").write_text(receipt, encoding="utf-8")
        (root / "docs" / "agent-boundaries.md").write_text(
            "# Boundaries\n", encoding="utf-8"
        )
        (root / ".github").mkdir()
        (root / ".github" / "pull_request_template.md").write_text(
            receipt, encoding="utf-8"
        )

    def test_current_guidance_is_valid(self) -> None:
        self.assertEqual([], validate_agent_docs.validate())

    def test_reports_unlisted_skill_and_broken_link(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            skill = root / "docs" / "skills" / "example" / "SKILL.md"
            skill.parent.mkdir(parents=True)
            skill.write_text(
                "---\nname: example\ndescription: Example skill.\n---\n"
                "# Example\n\n[missing](missing.md)\n",
                encoding="utf-8",
            )
            self.write_valid_required_files(root)

            with (
                mock.patch.object(validate_agent_docs, "ROOT", root),
                mock.patch.object(
                    validate_agent_docs, "SKILLS_ROOT", root / "docs" / "skills"
                ),
            ):
                errors = validate_agent_docs.validate()

        self.assertTrue(any("not listed" in error for error in errors))
        self.assertTrue(any("broken link" in error for error in errors))

    def test_reports_invalid_frontmatter_and_missing_receipt_fields(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            skill = root / "docs" / "skills" / "example" / "SKILL.md"
            self.write_valid_required_files(root)
            skill.parent.mkdir()
            skill.write_text("# Missing frontmatter\n", encoding="utf-8")
            (root / "docs" / "skills" / "README.md").write_text(
                "[example](example/SKILL.md)\n", encoding="utf-8"
            )
            (root / "AGENTS.md").write_text("No receipt.\n", encoding="utf-8")
            (root / ".github" / "pull_request_template.md").write_text(
                "No receipt.\n", encoding="utf-8"
            )

            with (
                mock.patch.object(validate_agent_docs, "ROOT", root),
                mock.patch.object(
                    validate_agent_docs, "SKILLS_ROOT", root / "docs" / "skills"
                ),
            ):
                errors = validate_agent_docs.validate()

        self.assertTrue(any("frontmatter" in error for error in errors))
        self.assertEqual(
            10, sum("documentation receipt missing" in error for error in errors)
        )

    def test_missing_required_files_are_stable_errors(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            skills_root = root / "docs" / "skills"
            skills_root.mkdir(parents=True)

            with (
                mock.patch.object(validate_agent_docs, "ROOT", root),
                mock.patch.object(validate_agent_docs, "SKILLS_ROOT", skills_root),
            ):
                first = validate_agent_docs.validate()
                second = validate_agent_docs.validate()
                stderr = io.StringIO()
                with (
                    mock.patch.object(
                        validate_agent_docs, "validate", return_value=first
                    ),
                    contextlib.redirect_stderr(stderr),
                ):
                    exit_code = validate_agent_docs.main()

        self.assertEqual(first, second)
        self.assertEqual(1, exit_code)
        self.assertEqual("\n".join(first) + "\n", stderr.getvalue())
        self.assertEqual(
            [
                "AGENTS.md: missing required file",
                "docs/skills/README.md: missing required file",
                "docs/agent-boundaries.md: missing required file",
                ".github/pull_request_template.md: missing required file",
            ],
            first,
        )

    def test_missing_skill_during_traversal_is_an_error(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            self.write_valid_required_files(root)
            skill = root / "docs" / "skills" / "example" / "SKILL.md"
            skill.parent.mkdir()
            skill.write_text(
                "---\nname: example\ndescription: Example skill.\n---\n",
                encoding="utf-8",
            )
            original_read_text = Path.read_text

            def disappearing_skill(path: Path, *args, **kwargs) -> str:
                if path == skill:
                    raise FileNotFoundError(path)
                return original_read_text(path, *args, **kwargs)

            with (
                mock.patch.object(validate_agent_docs, "ROOT", root),
                mock.patch.object(
                    validate_agent_docs, "SKILLS_ROOT", root / "docs" / "skills"
                ),
                mock.patch.object(Path, "read_text", disappearing_skill),
            ):
                errors = validate_agent_docs.validate()

        self.assertIn(
            "docs/skills/example/SKILL.md: missing skill file",
            errors,
        )


if __name__ == "__main__":
    unittest.main()
