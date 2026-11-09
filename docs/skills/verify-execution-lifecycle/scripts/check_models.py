#!/usr/bin/env python3
"""Run a subsystem's fixed, deliberately broken, and restored TLC models."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import subprocess

TLC_SHA = "936a262061c914694dfd669a543be24573c45d5aa0ff20a8b96b23d01e050e88"
TLC_TOOL = ("http:tla2tools[url=https://github.com/tlaplus/tlaplus/releases/download/"
            f"v1.7.4/tla2tools.jar,checksum=sha256:{TLC_SHA}]@1.7.4")


def mise(*args):
    return subprocess.check_output(["mise", *args], text=True).strip()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("model", type=Path)
    parser.add_argument("--out", required=True, type=Path)
    args = parser.parse_args()
    source = args.model.resolve()
    fixed = source.with_suffix(".cfg")
    broken = source.with_name(source.stem + "-broken.cfg")
    rules = re.findall(r"^INVARIANT\s+(\w+)\s*$", broken.read_text(), re.M)
    if len(rules) != 1:
        parser.error("broken config must name exactly one expected INVARIANT")
    jar = Path(os.environ.get("TLA2TOOLS") or Path(mise("where", TLC_TOOL)) / "tla2tools.jar")
    java = os.environ.get("JAVA") or mise("which", "java", "--tool", "java@temurin-25")
    if hashlib.sha256(jar.read_bytes()).hexdigest() != TLC_SHA:
        raise SystemExit("FORMAL BLOCKED: TLC 1.7.4 checksum differs")
    args.out.mkdir(parents=True, exist_ok=False)
    for path in (source, fixed, broken):
        shutil.copyfile(path, args.out / path.name)
    receipts = []
    for label, config, violation in (("fixed", fixed, None), ("broken", broken, rules[0]),
                                      ("restored", fixed, None)):
        timed_out = False
        try:
            result = subprocess.run(
                [java, "-Xmx256m", "-XX:+UseParallelGC", "-cp", str(jar), "tlc2.TLC",
                 "-workers", "1", "-config", config.name, source.name],
                cwd=args.out, text=True, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, timeout=60)
        except subprocess.TimeoutExpired as error:
            output = error.stdout or ""
            if isinstance(output, bytes):
                output = output.decode("utf-8", errors="replace")
            result = subprocess.CompletedProcess(error.cmd, 124, output + "\nTLC timed out after 60 seconds.\n")
            timed_out = True
        (args.out / (label + ".log")).write_text(result.stdout)
        passed = (result.returncode == 12 and f"Invariant {violation} is violated" in result.stdout
                  if violation else result.returncode == 0 and "No error has been found" in result.stdout)
        receipts.append(dict(case=label, exit=result.returncode, timed_out=timed_out, expected_violation=violation,
                             passed=passed))
        (args.out / "results.json").write_text(json.dumps(dict(
            model=source.name, sha256=hashlib.sha256(source.read_bytes()).hexdigest(),
            tlc_sha256=TLC_SHA, cases=receipts), indent=2) + "\n")
        print(f"{source.stem} {label}: {'PASS' if passed else 'FAIL'} (exit {result.returncode})", flush=True)
        if not passed:
            raise SystemExit(f"FORMAL FAILED: inspect {args.out / (label + '.log')}")
    shutil.rmtree(args.out / "states", ignore_errors=True)


if __name__ == "__main__":
    main()
