#!/usr/bin/env python3
"""Report Go functions unreachable in the current RCC build configuration."""

import argparse
import json
import os
import subprocess
import sys
from itertools import groupby
from pathlib import Path
from typing import NamedTuple


ANALYZER = "golang.org/x/tools/cmd/deadcode"
ANALYZER_VERSION = "v0.48.0"
REPOSITORY_ROOT = Path(__file__).resolve().parents[1]


class AnalysisError(RuntimeError):
    """Raised when the Go analyzer cannot produce a trustworthy report."""


class BuildConfig(NamedTuple):
    goos: str
    goarch: str


class Finding(NamedTuple):
    package: str
    file: str
    line: int
    column: int
    name: str


def create_parser():
    parser = argparse.ArgumentParser(
        description="Report Go functions unreachable on the current RCC target."
    )
    parser.add_argument(
        "--summary",
        action="store_true",
        help="Show configuration and totals without individual findings.",
    )
    parser.add_argument(
        "-v",
        "--verbose",
        action="store_true",
        help="Show analyzer commands and diagnostics.",
    )
    return parser


def _relative_path(filename, repository_root):
    normalized = filename.replace("\\", "/")
    source = Path(normalized)
    if source.is_absolute():
        try:
            source = source.relative_to(repository_root.resolve())
        except ValueError:
            pass
    return source.as_posix()


def parse_findings(payload, repository_root):
    try:
        packages = json.loads(payload)
        if not isinstance(packages, list):
            raise ValueError("top-level value is not an array")

        findings = []
        for package in packages:
            package_path = package["Path"]
            for function in package["Funcs"]:
                position = function["Position"]
                findings.append(
                    Finding(
                        package=package_path,
                        file=_relative_path(position["File"], repository_root),
                        line=int(position["Line"]),
                        column=int(position["Col"]),
                        name=function["Name"],
                    )
                )
    except (json.JSONDecodeError, KeyError, TypeError, ValueError) as error:
        raise AnalysisError(f"deadcode returned invalid JSON: {error}") from error

    return sorted(
        findings,
        key=lambda item: (
            item.package,
            item.file,
            item.line,
            item.column,
            item.name,
        ),
    )


def _plural(count, singular, plural):
    return singular if count == 1 else plural


def format_report(findings, config, summary=False):
    package_count = len({item.package for item in findings})
    lines = [
        "RCC DEAD CODE REPORT",
        f"Analyzer: {ANALYZER} {ANALYZER_VERSION}",
        f"Target: {config.goos}/{config.goarch} (CGO_ENABLED=0)",
        "Scope: ./... (tests included)",
    ]

    if findings:
        lines.append(
            "Result: "
            f"{len(findings)} {_plural(len(findings), 'unreachable function', 'unreachable functions')} "
            f"across {package_count} {_plural(package_count, 'package', 'packages')}"
        )
    else:
        lines.append("Result: no unreachable functions found")

    if findings and not summary:
        for package, package_findings in groupby(
            findings, key=lambda item: item.package
        ):
            lines.extend(("", package))
            for item in package_findings:
                lines.append(
                    f"  {item.file}:{item.line}:{item.column}  {item.name}"
                )

    lines.extend(
        (
            "",
            "Advisory: Review findings before deletion; results are specific to this target.",
        )
    )
    return "\n".join(lines) + "\n"


def _run(command, runner, repository_root, environment, verbose, stderr):
    if verbose:
        print(f"$ {' '.join(command)}", file=stderr)
    return runner(
        command,
        cwd=repository_root,
        env=environment,
        capture_output=True,
        text=True,
    )


def _build_config(runner, repository_root, environment, verbose, stderr):
    result = _run(
        ["go", "env", "-json", "GOHOSTOS", "GOHOSTARCH"],
        runner,
        repository_root,
        environment,
        verbose,
        stderr,
    )
    if result.returncode != 0:
        details = result.stderr.strip() or result.stdout.strip()
        raise AnalysisError(f"go env failed: {details or 'unknown error'}")
    try:
        values = json.loads(result.stdout)
        return BuildConfig(
            goos=values["GOHOSTOS"],
            goarch=values["GOHOSTARCH"],
        )
    except (json.JSONDecodeError, KeyError, TypeError) as error:
        raise AnalysisError(f"go env returned invalid JSON: {error}") from error


def run_cli(
    arguments=None,
    *,
    runner=subprocess.run,
    stdout=sys.stdout,
    stderr=sys.stderr,
    repo_root=REPOSITORY_ROOT,
):
    options = create_parser().parse_args(arguments)
    environment = os.environ.copy()
    environment["CGO_ENABLED"] = "0"

    try:
        config = _build_config(
            runner,
            repo_root,
            environment,
            options.verbose,
            stderr,
        )
        environment["GOOS"] = config.goos
        environment["GOARCH"] = config.goarch
        analyzer_environment = environment.copy()
        # x/tools v0.48.0 calls telemetry.Start before parsing arguments.
        # Its pinned child-process sentinel makes that call a no-op without
        # changing the user's global Go telemetry configuration.
        analyzer_environment["GO_TELEMETRY_CHILD"] = "2"
        result = _run(
            ["go", "tool", "deadcode", "-json", "-test", "./..."],
            runner,
            repo_root,
            analyzer_environment,
            options.verbose,
            stderr,
        )
        if result.returncode != 0:
            details = result.stderr.strip() or result.stdout.strip()
            raise AnalysisError(
                f"deadcode analysis failed: {details or 'unknown error'}"
            )
        if options.verbose and result.stderr:
            print(result.stderr.rstrip(), file=stderr)
        findings = parse_findings(result.stdout, repo_root)
    except FileNotFoundError:
        print(
            "ERROR: Go toolchain was not found. "
            "Run this through developer/toolkit.yaml.",
            file=stderr,
        )
        return 1
    except AnalysisError as error:
        print(f"ERROR: {error}", file=stderr)
        return 1

    stdout.write(format_report(findings, config, options.summary))
    return 0


def main():
    return run_cli()


if __name__ == "__main__":
    sys.exit(main())
