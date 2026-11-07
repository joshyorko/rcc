#!/usr/bin/env python3
"""Check the bounded admission model with pinned TLC, including its mutation."""
import hashlib
import os
from pathlib import Path
import shutil
import subprocess
import tempfile

TLC_SHA = "936a262061c914694dfd669a543be24573c45d5aa0ff20a8b96b23d01e050e88"
TLC_TOOL = (
    "http:tla2tools[url=https://github.com/tlaplus/tlaplus/releases/download/"
    f"v1.7.4/tla2tools.jar,checksum=sha256:{TLC_SHA}]@1.7.4"
)


def mise(*args):
    return subprocess.check_output(["mise", *args], text=True).strip()


def main():
    source = Path(__file__).resolve().parent
    jar = Path(os.environ.get("TLA2TOOLS") or Path(mise("where", TLC_TOOL)) / "tla2tools.jar")
    java = os.environ.get("JAVA") or mise("which", "java", "--tool", "java@temurin-25")
    if hashlib.sha256(jar.read_bytes()).hexdigest() != TLC_SHA:
        raise SystemExit("FORMAL BLOCKED: TLC 1.7.4 checksum differs")
    # Run outside the checkout; TLC's state directory is task-owned and removed.
    with tempfile.TemporaryDirectory(prefix="codex-rcc-formal-", dir=os.environ.get("TMPDIR", "/tmp")) as tmp:
        target = Path(tmp)
        for name in ("ExecutionLifecycle.tla", "ExecutionLifecycle.cfg", "ExecutionLifecycle-broken.cfg"):
            shutil.copyfile(source / name, target / name)
        for label, config, broken in (
            ("fixed", "ExecutionLifecycle.cfg", False),
            ("broken", "ExecutionLifecycle-broken.cfg", True),
            ("restored", "ExecutionLifecycle.cfg", False),
        ):
            result = subprocess.run(
                [java, "-Xmx256m", "-XX:+UseParallelGC", "-cp", str(jar), "tlc2.TLC",
                 "-workers", "1", "-config", config, "ExecutionLifecycle.tla"],
                cwd=target, text=True, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, timeout=60,
            )
            print(f"=== {label}: exit {result.returncode} ===\n{result.stdout}", flush=True)
            expected = (
                result.returncode == 12 and "Invariant NoTrustDowngrade is violated" in result.stdout
                if broken else result.returncode == 0 and "No error has been found" in result.stdout
            )
            if not expected:
                raise SystemExit(f"FORMAL FAILED: unexpected {label} result")


if __name__ == "__main__":
    main()
