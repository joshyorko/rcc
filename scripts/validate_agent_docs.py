#!/usr/bin/env python3
"""Structurally validate RCC's repository-local agent guidance."""

from __future__ import annotations

import re
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
SKILLS_ROOT = ROOT / "docs" / "skills"
SKILL_MAX_LINES = 250
LINK = re.compile(r"\[[^\]]*\]\(([^)]+)\)")


def frontmatter(text: str) -> dict[str, str]:
    if not text.startswith("---\n"):
        raise ValueError("missing YAML frontmatter opener")
    end = text.find("\n---\n", 4)
    if end == -1:
        raise ValueError("missing YAML frontmatter closer")

    values: dict[str, str] = {}
    for line in text[4:end].splitlines():
        key, separator, value = line.partition(":")
        if separator:
            values[key.strip()] = value.strip().strip("\"'")
    return values


def required_files() -> list[Path]:
    return [
        ROOT / "AGENTS.md",
        SKILLS_ROOT / "README.md",
        ROOT / "docs" / "agent-boundaries.md",
        ROOT / ".github" / "pull_request_template.md",
    ]


def read_text(path: Path, errors: list[str], kind: str) -> str | None:
    try:
        return path.read_text(encoding="utf-8")
    except FileNotFoundError:
        errors.append(f"{path.relative_to(ROOT)}: missing {kind}")
        return None


def validate() -> list[str]:
    errors: list[str] = []
    index = SKILLS_ROOT / "README.md"
    texts = {
        path: text
        for path in required_files()
        if (text := read_text(path, errors, "required file")) is not None
    }
    index_text = texts.get(index)
    skills = sorted(SKILLS_ROOT.glob("*/SKILL.md"))

    for skill in skills:
        relative = skill.relative_to(ROOT)
        skill_text = read_text(skill, errors, "skill file")
        if skill_text is None:
            continue
        texts[skill] = skill_text
        try:
            metadata = frontmatter(skill_text)
        except ValueError as error:
            errors.append(f"{relative}: {error}")
            continue
        for field in ("name", "description"):
            if not metadata.get(field):
                errors.append(f"{relative}: frontmatter missing {field!r}")
        if metadata.get("name") != skill.parent.name:
            errors.append(
                f"{relative}: skill name {metadata.get('name')!r} "
                f"does not match directory {skill.parent.name!r}"
            )
        if len(skill_text.splitlines()) > SKILL_MAX_LINES:
            errors.append(f"{relative}: exceeds {SKILL_MAX_LINES} lines")
        expected_link = f"{skill.parent.name}/SKILL.md"
        if index_text is not None and expected_link not in index_text:
            errors.append(f"{relative}: not listed in docs/skills/README.md")

    for markdown, text in sorted(texts.items()):
        for match in LINK.finditer(text):
            target = match.group(1).split("#", 1)[0]
            if not target or target.startswith(("http://", "https://", "mailto:")):
                continue
            resolved = (
                ROOT / target.lstrip("/")
                if target.startswith("/")
                else markdown.parent / target
            )
            if not resolved.exists():
                errors.append(
                    f"{markdown.relative_to(ROOT)}: broken link to {match.group(1)}"
                )

    required_receipt_fields = (
        "Canonical guidance:",
        "Durable learning:",
        "Evidence:",
        "Stale guidance removed:",
        "Remaining uncertainty:",
    )
    receipt_files = (
        ROOT / "AGENTS.md",
        ROOT / ".github" / "pull_request_template.md",
    )
    for receipt_file in receipt_files:
        receipt_text = texts.get(receipt_file)
        if receipt_text is None:
            continue
        for field in required_receipt_fields:
            if field not in receipt_text:
                errors.append(
                    f"{receipt_file.relative_to(ROOT)}: documentation receipt "
                    f"missing {field!r}"
                )

    return errors


def main() -> int:
    errors = validate()
    if errors:
        for error in errors:
            print(error, file=sys.stderr)
        return 1
    count = len(list(SKILLS_ROOT.glob("*/SKILL.md")))
    print(f"Agent guidance passes structural validation ({count} skills).")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
