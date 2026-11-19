#!/usr/bin/env python3
"""Verify RCC onboarding docs, links, TOC cleanliness, and README Linux first-run."""

from __future__ import annotations

import argparse
import http.server
import os
import re
import shutil
import subprocess
import sys
import tempfile
import threading
import xml.etree.ElementTree as ET
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
if str(ROOT) not in sys.path:
    sys.path.insert(0, str(ROOT))

from scripts import toc as toc_module

ONBOARDING_DOCS = (
    "README.md",
    "docs/README.md",
    "docs/toc.md",
    "docs/quickstart.md",
    "docs/capability-inventory.md",
    "docs/providers.md",
    "docs/holotree.md",
    "docs/roadmap.md",
    "docs/cloud-commands.md",
    "docs/features.md",
    "docs/profile_configuration.md",
    "docs/environment-caching.md",
)

LINK_RE = re.compile(r"!?\[[^\]]*\]\((<[^>]+>|[^)\s]+)\)")
HEADING_RE = re.compile(r"^\s*#{1,6}\s+(.*?)\s*$")
ANCHOR_NAME_RE = re.compile(r'<a\s+name="([^"]+)"')
FENCE_RE = re.compile(r"^\s*```")


def extract_anchors(path: Path) -> set[str]:
    anchors: set[str] = set()
    counts: dict[str, int] = {}
    in_code = False
    for line in path.read_text(encoding="utf-8").splitlines():
        if FENCE_RE.match(line):
            in_code = not in_code
            continue
        if in_code:
            continue
        match = HEADING_RE.match(line)
        if match:
            base = toc_module.unify(match.group(1))
            seen = counts.get(base, 0)
            counts[base] = seen + 1
            anchors.add(base if seen == 0 else f"{base}-{seen}")
        for anchor in ANCHOR_NAME_RE.finditer(line):
            anchors.add(anchor.group(1))
    return anchors


def check_links() -> None:
    anchor_cache: dict[Path, set[str]] = {}
    errors: list[str] = []
    checked = 0
    for rel in ONBOARDING_DOCS:
        path = ROOT / rel
        in_code = False
        for lineno, line in enumerate(path.read_text(encoding="utf-8").splitlines(), 1):
            if FENCE_RE.match(line):
                in_code = not in_code
                continue
            if in_code:
                continue
            for match in LINK_RE.finditer(line):
                raw = match.group(1).strip()
                if raw.startswith("<") and raw.endswith(">"):
                    raw = raw[1:-1]
                if raw.startswith(("http://", "https://", "mailto:")):
                    continue
                checked += 1
                target_part, _, frag = raw.partition("#")
                if not target_part:
                    target = path
                elif target_part.startswith("/"):
                    target = (ROOT / target_part.lstrip("/")).resolve()
                else:
                    target = (path.parent / target_part).resolve()
                if not target.exists():
                    errors.append(f"{rel}:{lineno}: missing link target {raw!r}")
                    continue
                if frag and target.suffix == ".md":
                    anchors = anchor_cache.setdefault(target, extract_anchors(target))
                    if frag not in anchors:
                        errors.append(
                            f"{rel}:{lineno}: missing anchor #{frag} in {target.relative_to(ROOT)}"
                        )
    if errors:
        raise RuntimeError("Markdown link/anchor failures:\n" + "\n".join(errors))
    print(f"Verified {checked} local links/anchors across {len(ONBOARDING_DOCS)} docs.")


def check_svg() -> None:
    svg_path = ROOT / "docs" / "rcc-artifact-flow.svg"
    tree = ET.parse(svg_path)
    root = tree.getroot()
    if not root.tag.endswith("svg"):
        raise RuntimeError(f"{svg_path} root tag is not svg: {root.tag}")
    print(f"Verified well-formed SVG: {svg_path.relative_to(ROOT)}")


def check_toc_clean() -> None:
    toc_path = ROOT / "docs" / "toc.md"
    before = toc_path.read_bytes()
    previous = Path.cwd()
    try:
        os.chdir(ROOT)
        toc_module.process()
    finally:
        os.chdir(previous)
    after = toc_path.read_bytes()
    if before != after:
        toc_path.write_bytes(before)
        raise RuntimeError("docs/toc.md is out of date; run `python3 scripts/toc.py`")
    print("Verified docs/toc.md matches scripts/toc.py output.")


def extract_section_shell_block(markdown: str, heading_prefix: str) -> str:
    lines = markdown.splitlines()
    start = None
    for idx, line in enumerate(lines):
        if line.startswith(heading_prefix):
            start = idx + 1
            break
    if start is None:
        raise RuntimeError(f"Heading starting with {heading_prefix!r} not found in README.md")
    in_block = False
    collected: list[str] = []
    for line in lines[start:]:
        if not in_block:
            if line.strip() == "```sh":
                in_block = True
            elif line.startswith("## "):
                break
        else:
            if line.strip() == "```":
                return "\n".join(collected) + "\n"
            collected.append(line)
    raise RuntimeError(f"No ```sh block found under heading {heading_prefix!r}")


def _serve_corrupt_payload(payload: bytes) -> tuple[http.server.HTTPServer, str]:
    class CorruptHandler(http.server.BaseHTTPRequestHandler):
        def do_GET(self) -> None:  # noqa: N802
            self.send_response(200)
            self.send_header("Content-Length", str(len(payload)))
            self.end_headers()
            self.wfile.write(payload)

        def log_message(self, format: str, *args: object) -> None:  # noqa: A002
            return

    server = http.server.HTTPServer(("127.0.0.1", 0), CorruptHandler)
    port = server.server_address[1]
    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()
    return server, f"http://127.0.0.1:{port}/rcc-linux64"


def run_readme_linux_smoke() -> None:
    if sys.platform != "linux":
        raise RuntimeError(f"README Linux smoke requires Linux; found {sys.platform}")

    readme = (ROOT / "README.md").read_text(encoding="utf-8")
    install_block = extract_section_shell_block(readme, "### Linux x86_64")
    first_run_block = extract_section_shell_block(readme, "## First Run")

    url_match = re.search(
        r"https://github\.com/joshyorko/rcc/releases/download/(v[0-9.]+)/rcc-linux64",
        install_block,
    )
    if not url_match:
        raise RuntimeError("README Linux installer is missing pinned release URL")
    expected_version = url_match.group(1)

    with tempfile.TemporaryDirectory(prefix="rcc-docs-smoke-") as tmp_str:
        tmp = Path(tmp_str)
        fake_home = tmp / "home"
        robocorp_home = tmp / "rcc-home"
        work_dir = tmp / "work"
        fake_home.mkdir()
        robocorp_home.mkdir()
        work_dir.mkdir()

        env = os.environ.copy()
        env["HOME"] = str(fake_home)
        env["ROBOCORP_HOME"] = str(robocorp_home)
        env["RCC_HOLOTREE_MODE"] = "private"

        # 1. Negative checksum-mismatch check: existing binary must survive untouched.
        dest_bin = fake_home / ".local" / "bin"
        dest_bin.mkdir(parents=True)
        existing = dest_bin / "rcc"
        sentinel = "#!/bin/sh\necho SENTINEL-EXISTING\n"
        existing.write_text(sentinel, encoding="utf-8")
        existing.chmod(0o755)

        server, bad_url = _serve_corrupt_payload(b"incomplete-corrupt-download")
        try:
            corrupt_script = install_block.replace(url_match.group(0), bad_url)
            mismatch = subprocess.run(
                ["bash", "-c", corrupt_script],
                env=env,
                capture_output=True,
                text=True,
            )
        finally:
            server.shutdown()
            server.server_close()

        if mismatch.returncode == 0:
            raise RuntimeError("Corrupted download unexpectedly succeeded in README Linux installer")
        if existing.read_text(encoding="utf-8") != sentinel:
            raise RuntimeError("Corrupted download overwrote existing rcc binary")
        leftovers = list(dest_bin.glob(".rcc.tmp.*"))
        if leftovers:
            raise RuntimeError(f"Corrupted download left temporary files: {leftovers}")
        print("Verified README Linux installer rejects mismatched digest and preserves existing binary.")

        # 2. Positive staged install check against real published release asset.
        installed = subprocess.run(
            ["bash", "-c", install_block],
            env=env,
            capture_output=True,
            text=True,
        )
        if installed.returncode != 0:
            raise RuntimeError(
                f"README Linux installer failed (exit {installed.returncode}):\n"
                f"STDOUT:\n{installed.stdout}\nSTDERR:\n{installed.stderr}"
            )
        if expected_version not in installed.stdout:
            raise RuntimeError(
                f"Expected version {expected_version} in installer output, got:\n{installed.stdout}"
            )
        print(f"Verified README Linux installer installed and validated {expected_version}.")

        # 3. Execute README First Run block with installed binary (cold + warm run).
        run_env = env.copy()
        run_env["PATH"] = f"{dest_bin}:{env.get('PATH', '')}"
        resolved = shutil.which("rcc", path=run_env["PATH"])
        if resolved != str(existing):
            raise RuntimeError(f"Expected installed rcc at {existing}, resolved {resolved}")

        cold = subprocess.run(
            ["bash", "-euo", "pipefail", "-c", first_run_block],
            cwd=work_dir,
            env=run_env,
            capture_output=True,
            text=True,
        )
        if cold.returncode != 0:
            raise RuntimeError(
                f"README First Run cold execution failed (exit {cold.returncode}):\n"
                f"STDOUT:\n{cold.stdout[-2000:]}\nSTDERR:\n{cold.stderr[-2000:]}"
            )
        combined_cold = cold.stdout + "\n" + cold.stderr
        if "Hello from RCC! Python 3.12.11" not in combined_cold:
            raise RuntimeError(f"Missing expected greeting in cold run output:\n{combined_cold[-2000:]}")
        print("Verified README First Run cold execution succeeded.")

        warm = subprocess.run(
            [str(existing), "run"],
            cwd=work_dir / "rcc-hello",
            env=run_env,
            capture_output=True,
            text=True,
        )
        if warm.returncode != 0:
            raise RuntimeError(
                f"README First Run warm execution failed (exit {warm.returncode}):\n"
                f"STDOUT:\n{warm.stdout[-2000:]}\nSTDERR:\n{warm.stderr[-2000:]}"
            )
        combined_warm = warm.stdout + "\n" + warm.stderr
        if "Hello from RCC! Python 3.12.11" not in combined_warm:
            raise RuntimeError(f"Missing expected greeting in warm run output:\n{combined_warm[-2000:]}")
        print("Verified README First Run warm execution succeeded.")


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument(
        "--skip-runtime-smoke",
        action="store_true",
        help="Run offline structural, TOC, SVG, and link/anchor checks only.",
    )
    args = parser.parse_args()

    check_toc_clean()
    check_svg()
    check_links()
    if not args.skip_runtime_smoke:
        run_readme_linux_smoke()
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
