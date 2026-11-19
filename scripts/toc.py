#!/bin/env python3

import glob
import re
from os.path import basename

MARKDOWN_LINK_PATTERN = re.compile(r"\[([^\]]+)\]\([^)]+\)")
STRIP_PUNCT_PATTERN = re.compile(r"[^\w -]+")
HEADING_PATTERN = re.compile(r"^\s*(#{1,3})\s+(.*?)\s*$")
CODE_PATTERN = re.compile(r"^\s*[`]{3}")

DOT = "."
NEWLINE = "\n"

IGNORE_LIST = ("changelog.md", "toc.md", "BUILD.md", "README.md")

PRIORITY_LIST = (
    "docs/quickstart.md",
    "docs/capability-inventory.md",
    "docs/providers.md",
    "docs/usecases.md",
    "docs/features.md",
    "docs/holotree.md",
    "docs/recipes.md",
    "docs/profile_configuration.md",
    "docs/environment-caching.md",
    "docs/maintenance.md",
    "docs/venv.md",
    "docs/cloud-commands.md",
    "docs/troubleshooting.md",
    "docs/vocabulary.md",
    "docs/history.md",
    "docs/roadmap.md",
)


def unify(value):
    """Return a GitHub-compatible heading fragment for a Markdown heading."""
    text = MARKDOWN_LINK_PATTERN.sub(r"\1", str(value))
    text = text.replace("`", "").strip().lower()
    text = STRIP_PUNCT_PATTERN.sub("", text)
    return text.replace(" ", "-")


class Toc:
    def __init__(self, title, baseurl=""):
        self.title = title
        self.baseurl = baseurl
        self.levels = [0]
        self.toc = [f"# {title}"]
        self.slug_counts = {}

    def leveling(self, level):
        levelup = True
        while len(self.levels) > level:
            self.levels.pop()
        while len(self.levels) < level:
            self.levels.append(1)
            levelup = False
        if levelup:
            self.levels[-1] += 1

    def add(self, filename, level, title):
        self.leveling(level)
        numbering = DOT.join(map(str, self.levels))
        prefix = "#" * level
        target = f"{self.baseurl}{basename(filename)}" if not self.baseurl else f"{self.baseurl}{filename}"
        base_ref = unify(title)
        key = (target, base_ref)
        seen = self.slug_counts.get(key, 0)
        self.slug_counts[key] = seen + 1
        ref = base_ref if seen == 0 else f"{base_ref}-{seen}"
        self.toc.append(
            f"#{prefix} {numbering} [{title}]({target}#{ref})"
        )

    def write(self, filename):
        with open(filename, "w+", encoding="utf-8") as sink:
            sink.write(NEWLINE.join(self.toc))


def headings(filename):
    inside = False
    with open(filename, encoding="utf-8") as source:
        for line in source:
            if CODE_PATTERN.match(line):
                inside = not inside
            if inside:
                continue
            if found := HEADING_PATTERN.match(line):
                level, title = found.groups()
                yield filename, len(level), title


def process():
    toc = Toc("Table of contents: rcc documentation")
    flatnames = list(map(basename, glob.glob("docs/*.md")))
    for filename in PRIORITY_LIST:
        flatname = basename(filename)
        if flatname in flatnames:
            flatnames.remove(flatname)
            for filename, level, title in headings(filename):
                toc.add(filename, level, title)
    for flatname in sorted(flatnames):
        if flatname in IGNORE_LIST:
            continue
        for filename, level, title in headings(f"docs/{flatname}"):
            toc.add(filename, level, title)
    toc.write("docs/toc.md")


if __name__ == "__main__":
    process()
