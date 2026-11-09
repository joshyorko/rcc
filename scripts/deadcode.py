#!/usr/bin/env python3
"""Collect reproducible, cross-target Go reachability evidence for RCC."""

import argparse
import hashlib
import json
import os
import stat
import subprocess
import sys
from pathlib import Path
from typing import NamedTuple


ANALYZER = "golang.org/x/tools/cmd/deadcode"
ANALYZER_VERSION = "v0.48.0"
REPOSITORY_ROOT = Path(__file__).resolve().parents[1]
SCHEMA_VERSION = 1
SUPPORTED_TARGETS = (
    ("linux", "amd64"),
    ("darwin", "amd64"),
    ("darwin", "arm64"),
    ("windows", "amd64"),
)
COMMAND_ENVIRONMENT = ("CGO_ENABLED", "GOFLAGS", "GOOS", "GOARCH", "GO_TELEMETRY_CHILD")


class AnalysisError(RuntimeError):
    """Raised when evidence cannot be collected or trusted."""


class Finding(NamedTuple):
    package: str
    file: str
    line: int
    column: int
    name: str
    generated: bool
    marker: bool

    @property
    def key(self):
        return (self.package, self.name)

    @property
    def position(self):
        return {"file": self.file, "line": self.line, "column": self.column}


def create_parser():
    parser = argparse.ArgumentParser(
        description=(
            "Compare application and test-inclusive Go reachability on RCC's "
            "supported build targets. This collects evidence; it does not review "
            "or change code."
        )
    )
    parser.add_argument(
        "--target",
        action="append",
        metavar="GOOS/GOARCH",
        help="Analyze this supported target (repeatable; default: all four release targets).",
    )
    parser.add_argument(
        "--package",
        action="append",
        metavar="PATTERN",
        help="Go package pattern to analyze (repeatable; default: ./...).",
    )
    parser.add_argument(
        "--symbol",
        action="append",
        metavar="IMPORT_PATH:NAME",
        help=(
            "Include a function or method even when reachable, for example "
            "github.com/joshyorko/rcc/pretty:csi (repeatable)."
        ),
    )
    parser.add_argument("--tags", default="", help="Comma-separated Go build tags (default: none).")
    parser.add_argument("--summary", action="store_true", help="Omit individual symbol details from text output.")
    parser.add_argument(
        "--format",
        choices=("text", "json"),
        default="text",
        help="Primary output format (default: text).",
    )
    parser.add_argument("--output", metavar="PATH", help="Write the primary output to this file instead of stdout.")
    parser.add_argument("--json", dest="json_output", metavar="PATH", help="Also write the complete structured evidence report.")
    parser.add_argument(
        "--review",
        action="store_true",
        help="Write evidence and an unreviewed work-brief template under build/deadcode/. Review remains not performed.",
    )
    parser.add_argument("--review-template", metavar="PATH", help="Write an unreviewed work-brief template to this path.")
    parser.add_argument("--analyzer-binary", metavar="PATH", help=argparse.SUPPRESS)
    parser.add_argument("--inventory-binary", metavar="PATH", help=argparse.SUPPRESS)
    parser.add_argument("-v", "--verbose", action="store_true", help="Print commands and diagnostics to stderr.")
    return parser


def _relative_path(filename, repository_root):
    normalized = str(filename).replace("\\", "/")
    source = Path(normalized)
    if source.is_absolute():
        try:
            source = source.relative_to(Path(repository_root).resolve())
        except ValueError:
            pass
    return source.as_posix()


def parse_findings(payload, repository_root):
    try:
        packages = json.loads(payload)
        # The official analyzer emits JSON null when there are no findings.
        if packages is None:
            packages = []
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
                        generated=bool(function.get("Generated", False)),
                        marker=bool(function.get("Marker", False)),
                    )
                )
    except (json.JSONDecodeError, KeyError, TypeError, ValueError) as error:
        raise AnalysisError(f"deadcode returned invalid JSON: {error}") from error
    return sorted(findings, key=lambda item: (item.package, item.name, item.file, item.line, item.column))


def parse_inventory(payload):
    try:
        inventory = json.loads(payload)
        if not isinstance(inventory, dict) or inventory.get("schema_version") != SCHEMA_VERSION:
            raise ValueError("unsupported inventory schema")
        symbols = inventory["symbols"]
        if not isinstance(symbols, list):
            raise ValueError("symbols is not an array")
        for symbol in symbols:
            if not isinstance(symbol["package"], str) or not isinstance(symbol["name"], str):
                raise ValueError("symbol package and name must be strings")
    except (json.JSONDecodeError, KeyError, TypeError, ValueError) as error:
        raise AnalysisError(f"deadcode inventory returned invalid JSON: {error}") from error
    return symbols


def _run_git(runner, repo_root, arguments):
    result = runner(arguments, cwd=repo_root, capture_output=True, text=True)
    if result.returncode != 0:
        details = result.stderr.strip() or result.stdout.strip() or "unknown error"
        raise AnalysisError(f"{' '.join(arguments[:2])} failed: {details}")
    return result.stdout


def fingerprint_paths(repository_root, paths):
    """Hash path, type, executable bit, and content for every source path."""
    root = Path(repository_root)
    digest = hashlib.sha256()
    for relative in sorted(set(paths)):
        path = root / relative
        encoded = str(relative).replace("\\", "/").encode("utf-8", "surrogateescape")
        digest.update(len(encoded).to_bytes(8, "big"))
        digest.update(encoded)
        try:
            info = path.lstat()
        except FileNotFoundError:
            digest.update(b"\0D\0")
            continue
        if stat.S_ISLNK(info.st_mode):
            target = os.readlink(path).encode("utf-8", "surrogateescape")
            digest.update(b"\0L\0" + target + b"\0")
            continue
        if not stat.S_ISREG(info.st_mode):
            digest.update(b"\0O\0" + str(info.st_mode).encode() + b"\0")
            continue
        content = hashlib.sha256()
        with path.open("rb") as source:
            for chunk in iter(lambda: source.read(1024 * 1024), b""):
                content.update(chunk)
        executable = "1" if info.st_mode & 0o111 else "0"
        digest.update(b"\0F" + executable.encode() + b"\0")
        digest.update(content.hexdigest().encode("ascii") + b"\0")
    return digest.hexdigest()


def source_snapshot(runner, repo_root):
    revision = _run_git(runner, repo_root, ["git", "rev-parse", "HEAD"]).strip()
    status = _run_git(
        runner,
        repo_root,
        ["git", "status", "--porcelain=v1", "--untracked-files=all"],
    )
    listing = _run_git(
        runner,
        repo_root,
        ["git", "ls-files", "-z", "--cached", "--others", "--exclude-standard"],
    )
    paths = [path for path in listing.split("\0") if path]
    return {
        "revision": revision,
        "tree_sha256": fingerprint_paths(repo_root, paths),
        "state": "modified" if status else "clean",
        "file_count": len(set(paths)),
    }


def _parse_target(value):
    try:
        goos, goarch = value.split("/", 1)
    except ValueError as error:
        raise AnalysisError(f"invalid target {value!r}: want GOOS/GOARCH") from error
    target = (goos, goarch)
    if target not in SUPPORTED_TARGETS:
        supported = ", ".join(target_name(item) for item in SUPPORTED_TARGETS)
        raise AnalysisError(f"unsupported target {value!r}; choose from: {supported}")
    return target


def _parse_symbol(value):
    package, separator, name = value.rpartition(":")
    if not separator or not package or not name:
        raise AnalysisError(f"invalid symbol {value!r}: want full/import/path:FuncOrType.Method")
    return package, name


def target_name(target):
    return "/".join(target)


def symbol_name(key):
    return f"{key[0]}:{key[1]}"


def _command(
    command,
    *,
    runner,
    repo_root,
    environment,
    commands,
    target="host",
    mode="metadata",
    verbose=False,
    stderr=sys.stderr,
):
    record = {
        "id": f"C{len(commands) + 1:03d}",
        "target": target,
        "mode": mode,
        "argv": [str(part) for part in command],
        "environment": {name: environment.get(name, "") for name in COMMAND_ENVIRONMENT if name in environment},
    }
    if verbose:
        print(f"$ {' '.join(str(part) for part in command)}", file=stderr)
    try:
        result = runner(
            [str(part) for part in command],
            cwd=repo_root,
            env=environment,
            capture_output=True,
            text=True,
        )
        stdout = result.stdout or ""
        diagnostic = result.stderr or ""
        returncode = result.returncode
    except FileNotFoundError as error:
        stdout = ""
        diagnostic = str(error)
        returncode = 127
    record["exit_code"] = returncode
    record["status"] = "complete" if returncode == 0 else "failed"
    if diagnostic.strip():
        record["diagnostics"] = diagnostic.strip()
    commands.append(record)
    if verbose and diagnostic.strip():
        print(diagnostic.rstrip(), file=stderr)
    return returncode, stdout, diagnostic, record


def _base_environment():
    environment = os.environ.copy()
    environment.pop("GOOS", None)
    environment.pop("GOARCH", None)
    environment["CGO_ENABLED"] = "0"
    # Do not inherit package selection, tags, or other loading behavior silently.
    environment["GOFLAGS"] = ""
    # x/tools v0.48.0 calls telemetry.Start before parsing arguments.
    environment["GO_TELEMETRY_CHILD"] = "2"
    return environment


def _tool_metadata(runner, repo_root, environment, commands, verbose, stderr):
    _, version_output, _, _ = _command(
        ["go", "version"],
        runner=runner,
        repo_root=repo_root,
        environment=environment,
        commands=commands,
        mode="go-version",
        verbose=verbose,
        stderr=stderr,
    )
    _, analyzer_output, _, _ = _command(
        ["go", "list", "-m", "-f={{.Version}}", "golang.org/x/tools"],
        runner=runner,
        repo_root=repo_root,
        environment=environment,
        commands=commands,
        mode="analyzer-version",
        verbose=verbose,
        stderr=stderr,
    )
    resolved = analyzer_output.strip() or "unknown"
    return {
        "name": ANALYZER,
        "declared_version": ANALYZER_VERSION,
        "resolved_version": resolved,
        "version_matches_pin": resolved == ANALYZER_VERSION,
        "go_version": version_output.strip() or "unknown",
    }


def _tool_binaries(
    options,
    runner,
    repo_root,
    environment,
    commands,
    verbose,
    stderr,
):
    analyzer_binary = Path(options.analyzer_binary).resolve() if options.analyzer_binary else None
    inventory_binary = Path(options.inventory_binary).resolve() if options.inventory_binary else None
    if analyzer_binary and inventory_binary:
        return analyzer_binary, inventory_binary, None
    code, output, diagnostics, _ = _command(
        ["go", "env", "-json", "GOHOSTOS", "GOHOSTARCH"],
        runner=runner,
        repo_root=repo_root,
        environment=environment,
        commands=commands,
        mode="host-target",
        verbose=verbose,
        stderr=stderr,
    )
    if code != 0:
        return None, None, f"go env failed: {diagnostics.strip() or output.strip() or 'unknown error'}"
    try:
        values = json.loads(output)
        host = (values["GOHOSTOS"], values["GOHOSTARCH"])
    except (json.JSONDecodeError, KeyError, TypeError) as error:
        return None, None, f"go env returned invalid JSON: {error}"
    build_environment = environment.copy()
    build_environment["GOOS"], build_environment["GOARCH"] = host
    suffix = ".exe" if host[0] == "windows" else ""
    specifications = (
        ("analyzer", analyzer_binary, "deadcode", "golang.org/x/tools/cmd/deadcode"),
        ("inventory", inventory_binary, "deadcodeinventory", "./developer/deadcodeinventory"),
    )
    binaries = {}
    for label, configured, executable, package in specifications:
        if configured:
            binaries[label] = configured
            continue
        binary = repo_root / "build" / "deadcode" / "bin" / (executable + suffix)
        binary.parent.mkdir(parents=True, exist_ok=True)
        code, output, diagnostics, _ = _command(
            ["go", "build", "-o", str(binary), package],
            runner=runner,
            repo_root=repo_root,
            environment=build_environment,
            commands=commands,
            mode=f"{label}-build",
            verbose=verbose,
            stderr=stderr,
        )
        if code != 0:
            return None, None, f"{label} build failed: {diagnostics.strip() or output.strip() or 'unknown error'}"
        binaries[label] = binary
    return binaries["analyzer"], binaries["inventory"], None


def _analyzer_command(binary, packages, tags, test):
    command = [str(binary), "-json", "-generated"]
    if tags:
        command.append(f"-tags={tags}")
    if test:
        command.append("-test")
    command.extend(packages)
    return command


def _new_target(target, selected):
    name = target_name(target)
    return {
        "id": f"T:{name}",
        "target": name,
        "status": "pending" if selected else "skipped",
        "selected": selected,
        "application": {"status": "skipped" if not selected else "pending", "dead_symbols": []},
        "test_inclusive": {"status": "skipped" if not selected else "pending", "dead_symbols": []},
        "inventory": {"status": "skipped" if not selected else "pending"},
        "command_ids": [],
        "diagnostics": [] if selected else ["Not selected; this target is unknown, not evidence of unused code."],
        "native_execution": "not_performed",
    }


def _mark_mode_failure(target, mode, message):
    target[mode]["status"] = "failed"
    target["diagnostics"].append(message)


def _aggregate_symbol(symbol, report):
    states = {name: evidence["state"] for name, evidence in symbol["targets"].items()}
    reachable_states = {"reachable_both", "test_only", "test_code_reachable"}
    if any(state in reachable_states for state in states.values()):
        aggregate = "reachable" if "reachable_both" in states.values() else "test_only"
    elif any(state in {"unknown", "inconsistent"} for state in states.values()):
        aggregate = "unknown"
    elif any(state in {"test_code_unreachable"} for state in states.values()):
        aggregate = "test_code_unreachable"
    elif states and all(state in {"unreachable_both", "absent"} for state in states.values()):
        aggregate = "unreachable_both"
    else:
        aggregate = "unknown"

    blockers = []
    if not report["coverage"]["complete"]:
        blockers.append("source or supported-target coverage is incomplete")
    present_states = [state for state in states.values() if state != "absent"]
    cross_target = (
        report["coverage"]["complete"]
        and bool(present_states)
        and all(state == "unreachable_both" for state in present_states)
    )
    if not cross_target:
        blockers.append("not unreachable in application and tests on every target where present")
    if symbol["generated"]:
        blockers.append("generated source requires generator review")
    if symbol["linkname"]:
        blockers.append("go:linkname can hide dispatch from the analyzer")
    if symbol["marker"]:
        blockers.append("marker/interface method obligations require preservation")
    if symbol["interface_obligations"]:
        blockers.append("interface obligations require an explicit compatibility review")
    removal_evidence_complete = cross_target and not (
        symbol["generated"]
        or symbol["marker"]
        or symbol["linkname"]
        or symbol["interface_obligations"]
    )
    symbol["aggregate"] = {
        "state": aggregate,
        "cross_target_unreachable": cross_target,
        "removal_evidence_complete": removal_evidence_complete,
        "removal_blocked_reasons": blockers,
    }


def _build_opportunities(symbols):
    by_key = {(symbol["package"], symbol["name"]): symbol for symbol in symbols}
    merged = {}
    for target in SUPPORTED_TARGETS:
        name = target_name(target)
        nodes = {
            (symbol["package"], symbol["name"])
            for symbol in symbols
            if symbol["targets"][name]["state"] == "unreachable_both"
        }
        if not nodes:
            continue
        adjacency = {node: set() for node in nodes}
        edges = []
        external = []
        for key in sorted(nodes):
            facts = by_key[key]["targets"][name].get("facts", {})
            for call in facts.get("calls", []):
                callee = (call["callee_package"], call["callee_name"])
                if callee in nodes:
                    adjacency[key].add(callee)
                    adjacency[callee].add(key)
                    edges.append(
                        {
                            "caller": symbol_name(key),
                            "callee": symbol_name(callee),
                            "kind": call["kind"],
                            "position": call["position"],
                        }
                    )
                else:
                    external.append(
                        {
                            "caller": symbol_name(key),
                            "package": callee[0],
                            "name": callee[1],
                            "kind": call["kind"],
                        }
                    )
        unseen = set(nodes)
        while unseen:
            first = min(unseen)
            component = {first}
            stack = [first]
            unseen.remove(first)
            while stack:
                current = stack.pop()
                for neighbor in adjacency[current]:
                    if neighbor in unseen:
                        unseen.remove(neighbor)
                        component.add(neighbor)
                        stack.append(neighbor)
            if len(component) < 2:
                continue
            group_key = frozenset(component)
            group = merged.setdefault(
                group_key,
                {
                    "finding_ids": sorted(by_key[key]["id"] for key in component),
                    "symbols": sorted(symbol_name(key) for key in component),
                    "targets": [],
                    "internal_references": [],
                    "external_callees": [],
                    "active_alternative": None,
                    "shared_helpers": [],
                    "review_status": "not_performed",
                },
            )
            group["targets"].append(name)
            member_names = {symbol_name(key) for key in component}
            group["internal_references"].extend(
                edge for edge in edges if edge["caller"] in member_names
            )
            group["external_callees"].extend(
                callee for callee in external if callee["caller"] in member_names
            )
    opportunities = []
    for group in merged.values():
        for field in ("internal_references", "external_callees"):
            unique = {json.dumps(item, sort_keys=True): item for item in group[field]}
            group[field] = [unique[key] for key in sorted(unique)]
        opportunities.append(group)
    opportunities.sort(key=lambda item: item["symbols"])
    for index, opportunity in enumerate(opportunities, 1):
        opportunity["id"] = f"O{index:03d}"
    return opportunities


def _review_template(report, selected):
    selected_ids = [symbol["id"] for symbol in report["symbols"] if symbol["id"] in selected]
    reviews = []
    for finding_id in selected_ids:
        reviews.append(
            {
                "id": f"review-{finding_id.lower()}",
                "title": "REPLACE WITH A HUMAN-READABLE OPPORTUNITY",
                "finding_ids": [finding_id],
                "what_it_does": "",
                "observations": [{"text": "", "evidence_ids": [finding_id]}],
                "active_alternative": "",
                "shared_helpers": [],
                "decision": "investigate_further",
                "benefit": "",
                "affected_callers": [],
                "behavior_to_preserve": [],
                "unresolved_questions": ["Complete the source review before making a recommendation."],
                "smallest_change": "",
                "verification": [],
                "requirement": "",
            }
        )
    return {
        "schema_version": SCHEMA_VERSION,
        "source": {"revision": report["source"]["revision"], "tree_sha256": report["source"]["tree_sha256"]},
        "candidates": [
            {
                "id": symbol["id"],
                "symbol": symbol_name((symbol["package"], symbol["name"])),
                "aggregate_state": symbol["aggregate"]["state"],
            }
            for symbol in report["symbols"]
        ],
        "reviews": reviews,
    }


def format_text(report, summary=False):
    source = report["source"]
    coverage = report["coverage"]
    analyzer = report["analyzer"]
    lines = [
        "RCC DEAD CODE EVIDENCE",
        f"Schema: {report['schema_version']}",
        "Review status: NOT PERFORMED",
        f"Source revision: {source['revision']}",
        f"Working tree: {source['state']}; sha256={source['tree_sha256']}; stable={str(source['stable']).lower()}",
        f"Source files fingerprinted: {source['file_count']}",
        f"Analyzer: {analyzer['name']} {analyzer['resolved_version']} (declared {analyzer['declared_version']})",
        f"Go toolchain: {analyzer['go_version']}",
        f"Package scope: {', '.join(report['scope']['packages'])}",
        f"Build tags: {report['scope']['build_tags'] or '(none)'}; CGO_ENABLED=0; GOFLAGS=(empty)",
        "Analysis: static cross-target analysis only; native Windows and macOS execution NOT PERFORMED.",
        (
            f"Coverage: {coverage['completed_targets']}/{coverage['supported_target_count']} supported targets complete; "
            f"cross-target removal evidence={'complete' if coverage['complete'] else 'INCOMPLETE'}"
        ),
        "",
        "Targets:",
    ]
    for target in report["targets"]:
        commands = ", ".join(target["command_ids"]) or "none"
        lines.append(
            f"  {target['id']}: {target['status']} "
            f"(application={target['application']['status']}, tests={target['test_inclusive']['status']}, "
            f"inventory={target['inventory']['status']}; commands={commands})"
        )
        for diagnostic in target["diagnostics"]:
            lines.append(f"    UNKNOWN: {diagnostic}")
    lines.extend(
        (
            "",
            (
                f"Symbols: {len(report['symbols'])} evidence records; "
                f"{len(report['opportunities'])} grouped abandoned-path opportunities."
            ),
            "Exact argv, target environment, references, calls, and diagnostics are retained in JSON.",
        )
    )
    if not summary:
        for symbol in report["symbols"]:
            flags = []
            for field in ("selected", "exported", "generated", "test_file", "marker", "linkname"):
                if symbol[field]:
                    flags.append(field.replace("_", "-"))
            lines.extend(
                (
                    "",
                    f"{symbol['id']} {symbol_name((symbol['package'], symbol['name']))}",
                    f"  Declaration: {symbol['position']['file']}:{symbol['position']['line']}:{symbol['position']['column']}",
                    f"  Facts: {', '.join(flags) if flags else 'unexported, ordinary source'}; "
                    f"aggregate={symbol['aggregate']['state']}; "
                    f"cross-target-unreachable={str(symbol['aggregate']['cross_target_unreachable']).lower()}",
                )
            )
            if symbol["interface_obligations"]:
                lines.append("  Interface obligations: " + ", ".join(symbol["interface_obligations"]))
            for target in SUPPORTED_TARGETS:
                name = target_name(target)
                evidence = symbol["targets"][name]
                lines.append(
                    f"    {name}: {evidence['state']} (presence={evidence['presence']}, "
                    f"application={evidence['application']}, tests={evidence['test_inclusive']})"
                )
            for reason in symbol["aggregate"]["removal_blocked_reasons"]:
                lines.append(f"  Removal blocker: {reason}")
        for opportunity in report["opportunities"]:
            lines.extend(
                (
                    "",
                    f"{opportunity['id']} Grouped opportunity (review NOT PERFORMED): "
                    + ", ".join(opportunity["symbols"]),
                    "  Targets: " + ", ".join(opportunity["targets"]),
                    "  Active alternative: requires source review; not inferred by the analyzer.",
                )
            )
            for reference in opportunity["internal_references"]:
                lines.append(f"  Internal: {reference['caller']} --{reference['kind']}--> {reference['callee']}")
            for callee in opportunity["external_callees"]:
                lines.append(
                    "  Outside-group callee; review shared-helper preservation: "
                    + symbol_name((callee["package"], callee["name"]))
                )
    lines.extend(
        (
            "",
            "Advisory: Reachability is not worthlessness. No deletion is authorized by this report.",
            "Review handoff: use docs/skills/deadcode-review/SKILL.md and scripts/deadcode_review.py.",
        )
    )
    if report.get("review_template"):
        lines.append(f"Unreviewed template: {report['review_template']}")
    return "\n".join(lines) + "\n"


def _write_output(path, content):
    destination = Path(path)
    destination.parent.mkdir(parents=True, exist_ok=True)
    destination.write_text(content, encoding="utf-8")


def run_cli(
    arguments=None,
    *,
    runner=subprocess.run,
    stdout=sys.stdout,
    stderr=sys.stderr,
    repo_root=REPOSITORY_ROOT,
    snapshotter=source_snapshot,
):
    repo_root = Path(repo_root).resolve()
    try:
        options = create_parser().parse_args(arguments)
        selected_targets = (
            tuple(_parse_target(value) for value in options.target)
            if options.target
            else SUPPORTED_TARGETS
        )
        if len(set(selected_targets)) != len(selected_targets):
            raise AnalysisError("duplicate --target values are not allowed")
        packages = options.package or ["./..."]
        selected_symbols = {_parse_symbol(value) for value in (options.symbol or [])}
        source_before = snapshotter(runner, repo_root)
    except AnalysisError as error:
        print(f"ERROR: {error}", file=stderr)
        return 2

    if options.review and not options.review_template:
        options.review_template = str(repo_root / "build" / "deadcode" / "review-template.json")
    if options.review and not options.json_output:
        options.json_output = str(repo_root / "build" / "deadcode" / "evidence.json")

    environment = _base_environment()
    commands = []
    analyzer = _tool_metadata(runner, repo_root, environment, commands, options.verbose, stderr)
    analyzer_binary, inventory_binary, tool_build_error = _tool_binaries(
        options,
        runner,
        repo_root,
        environment,
        commands,
        options.verbose,
        stderr,
    )

    targets = {target: _new_target(target, target in selected_targets) for target in SUPPORTED_TARGETS}
    candidates = {key: {"selected": key in selected_symbols, "analyzer": []} for key in selected_symbols}
    target_results = {}

    for target in selected_targets:
        name = target_name(target)
        target_environment = environment.copy()
        target_environment["GOOS"], target_environment["GOARCH"] = target
        target_record = targets[target]
        target_results[target] = {}
        for mode, test in (("application", False), ("test_inclusive", True)):
            if tool_build_error and not analyzer_binary:
                _mark_mode_failure(target_record, mode, tool_build_error)
                target_results[target][mode] = set()
                continue
            command = _analyzer_command(analyzer_binary, packages, options.tags, test)
            code, output, diagnostics, command_record = _command(
                command,
                runner=runner,
                repo_root=repo_root,
                environment=target_environment,
                commands=commands,
                target=name,
                mode=mode,
                verbose=options.verbose,
                stderr=stderr,
            )
            target_record["command_ids"].append(command_record["id"])
            if code != 0:
                _mark_mode_failure(
                    target_record,
                    mode,
                    f"{mode} analysis failed; target is unknown: "
                    + (diagnostics.strip() or output.strip() or "unknown error"),
                )
                target_results[target][mode] = set()
                continue
            try:
                findings = parse_findings(output, repo_root)
            except AnalysisError as error:
                command_record["status"] = "failed"
                _mark_mode_failure(target_record, mode, str(error))
                target_results[target][mode] = set()
                continue
            target_record[mode]["status"] = "complete"
            keys = set()
            for finding in findings:
                keys.add(finding.key)
                candidate = candidates.setdefault(finding.key, {"selected": False, "analyzer": []})
                candidate["analyzer"].append((target, mode, finding))
            target_record[mode]["dead_symbols"] = sorted(symbol_name(key) for key in keys)
            target_results[target][mode] = keys

    for target in selected_targets:
        name = target_name(target)
        target_record = targets[target]
        if tool_build_error and not inventory_binary:
            _mark_mode_failure(target_record, "inventory", tool_build_error)
            target_results[target]["inventory"] = {}
            continue
        command = [str(inventory_binary), "-root", str(repo_root), "-tags", options.tags]
        for key in sorted(candidates):
            command.extend(("-symbol", f"{key[0]}|{key[1]}"))
        command.append("--")
        command.extend(packages)
        target_environment = environment.copy()
        target_environment["GOOS"], target_environment["GOARCH"] = target
        code, output, diagnostics, command_record = _command(
            command,
            runner=runner,
            repo_root=repo_root,
            environment=target_environment,
            commands=commands,
            target=name,
            mode="inventory",
            verbose=options.verbose,
            stderr=stderr,
        )
        target_record["command_ids"].append(command_record["id"])
        if code != 0:
            _mark_mode_failure(
                target_record,
                "inventory",
                "Go-aware inventory failed; target is unknown: "
                + (diagnostics.strip() or output.strip() or "unknown error"),
            )
            target_results[target]["inventory"] = {}
            continue
        try:
            inventory_symbols = parse_inventory(output)
        except AnalysisError as error:
            command_record["status"] = "failed"
            _mark_mode_failure(target_record, "inventory", str(error))
            target_results[target]["inventory"] = {}
            continue
        mapped = {}
        for item in inventory_symbols:
            mapped.setdefault((item["package"], item["name"]), []).append(item)
        duplicates = sorted(symbol_name(key) for key, values in mapped.items() if len(values) > 1)
        if duplicates:
            _mark_mode_failure(
                target_record,
                "inventory",
                "ambiguous symbol declarations: " + ", ".join(duplicates),
            )
        target_record["inventory"]["status"] = "complete" if not duplicates else "failed"
        target_results[target]["inventory"] = mapped

    for target in selected_targets:
        record = targets[target]
        modes = (record["application"], record["test_inclusive"], record["inventory"])
        record["status"] = "complete" if all(mode["status"] == "complete" for mode in modes) else "unknown"
        if record["status"] == "unknown":
            record["diagnostics"].append("Incomplete target evidence blocks removal recommendations.")

    try:
        source_after = snapshotter(runner, repo_root)
        stable = all(
            source_before[field] == source_after[field]
            for field in ("revision", "tree_sha256")
        )
    except AnalysisError as error:
        source_after = dict(source_before)
        stable = False
        source_after["stability_diagnostic"] = str(error)
    source = dict(source_after)
    source["stable"] = stable
    source["repository_root"] = str(repo_root)
    if not stable:
        source["diagnostic"] = "Source changed during analysis; re-run before reviewing or removing code."

    completed = sum(targets[target]["status"] == "complete" for target in SUPPORTED_TARGETS)
    coverage = {
        "complete": stable and completed == len(SUPPORTED_TARGETS),
        "completed_targets": completed,
        "supported_target_count": len(SUPPORTED_TARGETS),
        "selected_targets": [target_name(target) for target in selected_targets],
        "static_analysis_only": True,
        "native_execution": "not_performed",
    }
    report = {
        "schema_version": SCHEMA_VERSION,
        "kind": "rcc-deadcode-evidence",
        "source": source,
        "analyzer": analyzer,
        "scope": {
            "packages": packages,
            "build_tags": options.tags,
            "cgo_enabled": "0",
            "goflags": "",
            "supported_targets": [target_name(target) for target in SUPPORTED_TARGETS],
        },
        "commands": commands,
        "targets": [targets[target] for target in SUPPORTED_TARGETS],
        "coverage": coverage,
        "symbols": [],
        "opportunities": [],
        "review": {"status": "not_performed", "briefs": []},
    }

    symbols = []
    for key in sorted(candidates):
        candidate = candidates[key]
        analyzer_positions = sorted(
            (finding for _, _, finding in candidate["analyzer"]),
            key=lambda finding: (finding.file, finding.line, finding.column),
        )
        position = analyzer_positions[0].position if analyzer_positions else {"file": "", "line": 0, "column": 0}
        symbol = {
            "package": key[0],
            "name": key[1],
            "position": position,
            "selected": candidate["selected"],
            "exported": False,
            "generated": any(finding.generated for _, _, finding in candidate["analyzer"]),
            "test_file": False,
            "marker": any(finding.marker for _, _, finding in candidate["analyzer"]),
            "linkname": False,
            "interface_obligations": set(),
            "targets": {},
        }
        for target in SUPPORTED_TARGETS:
            name = target_name(target)
            target_record = targets[target]
            evidence = {
                "state": "unknown",
                "presence": "unknown",
                "application": "unknown",
                "test_inclusive": "unknown",
                "command_ids": target_record["command_ids"],
                "diagnostics": [],
            }
            if not target_record["selected"]:
                evidence["diagnostics"].append("Target skipped; reachability is unknown.")
                symbol["targets"][name] = evidence
                continue
            results = target_results[target]
            inventory_matches = results.get("inventory", {}).get(key, [])
            facts = inventory_matches[0] if len(inventory_matches) == 1 else None
            app_dead = key in results.get("application", set())
            test_dead = key in results.get("test_inclusive", set())
            if target_record["status"] != "complete":
                evidence["diagnostics"].extend(target_record["diagnostics"])
            elif facts is None:
                if app_dead or test_dead:
                    evidence["diagnostics"].append("Analyzer finding has no matching Go type/source declaration.")
                else:
                    evidence.update(
                        state="absent",
                        presence="absent",
                        application="not_in_target",
                        test_inclusive="not_in_target",
                    )
            else:
                evidence["facts"] = facts
                evidence["presence"] = "present"
                symbol["exported"] = symbol["exported"] or facts["exported"]
                symbol["generated"] = symbol["generated"] or facts["generated"]
                symbol["test_file"] = symbol["test_file"] or facts["test_file"]
                symbol["marker"] = symbol["marker"] or facts["marker"]
                symbol["linkname"] = symbol["linkname"] or facts["linkname"]
                symbol["interface_obligations"].update(facts["interface_obligations"])
                if not symbol["position"]["file"]:
                    symbol["position"] = facts["position"]
                if facts["marker"] or facts["linkname"]:
                    evidence["diagnostics"].append(
                        "Marker/interface or go:linkname semantics make plain reachability classification unsafe."
                    )
                elif facts["test_file"]:
                    evidence["application"] = "not_applicable_test_source"
                    evidence["test_inclusive"] = "unreachable" if test_dead else "reachable"
                    evidence["state"] = "test_code_unreachable" if test_dead else "test_code_reachable"
                else:
                    evidence["application"] = "unreachable" if app_dead else "reachable"
                    evidence["test_inclusive"] = "unreachable" if test_dead else "reachable"
                    if app_dead and test_dead:
                        evidence["state"] = "unreachable_both"
                    elif app_dead:
                        evidence["state"] = "test_only"
                    elif test_dead:
                        evidence["state"] = "inconsistent"
                        evidence["diagnostics"].append(
                            "Application-reachable but test-unreachable is inconsistent; treat as unknown."
                        )
                    else:
                        evidence["state"] = "reachable_both"
            symbol["targets"][name] = evidence
        symbol["interface_obligations"] = sorted(symbol["interface_obligations"])
        symbols.append(symbol)

    selected_ids = set()
    for index, symbol in enumerate(symbols, 1):
        symbol["id"] = f"F{index:03d}"
        if symbol["selected"]:
            selected_ids.add(symbol["id"])
        _aggregate_symbol(symbol, report)
    report["symbols"] = symbols
    report["opportunities"] = _build_opportunities(symbols)

    selected_missing = [
        symbol_name(key)
        for key in sorted(selected_symbols)
        if not any(
            symbol["package"] == key[0]
            and symbol["name"] == key[1]
            and any(evidence["presence"] == "present" for evidence in symbol["targets"].values())
            for symbol in symbols
        )
    ]
    if selected_missing:
        report["coverage"]["selected_symbols_absent"] = selected_missing

    report_json = json.dumps(report, indent=2, sort_keys=True) + "\n"
    primary = report_json if options.format == "json" else format_text(report, options.summary)
    if options.output:
        _write_output(options.output, primary)
    else:
        stdout.write(primary)
    if options.json_output:
        _write_output(options.json_output, report_json)
    if options.review_template:
        _write_output(
            options.review_template,
            json.dumps(_review_template(report, selected_ids), indent=2, sort_keys=True) + "\n",
        )
        report["review_template"] = options.review_template
        if options.format == "text" and not options.output:
            stdout.write(f"Unreviewed template: {options.review_template}\n")

    failed = any(targets[target]["status"] != "complete" for target in selected_targets)
    return 1 if failed or not stable or selected_missing else 0


def main():
    try:
        return run_cli()
    except AnalysisError as error:
        print(f"ERROR: {error}", file=sys.stderr)
        return 1


if __name__ == "__main__":
    sys.exit(main())
