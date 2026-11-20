#!/usr/bin/env python3
"""Validate and render snapshot-bound RCC simplification work briefs."""

import argparse
import hashlib
import json
import subprocess
import sys
from pathlib import Path

from deadcode import (
    REPOSITORY_ROOT,
    SCHEMA_VERSION,
    AnalysisError,
    repository_path,
    source_snapshot,
)


DECISIONS = {
    "retain",
    "remove",
    "reuse_or_consolidate",
    "simplify_in_place",
    "repair_or_finish",
    "investigate_further",
}
REQUIRED_STRINGS = (
    "id",
    "title",
    "what_it_does",
    "active_alternative",
    "decision",
    "benefit",
    "smallest_change",
)
REQUIRED_LISTS = (
    "finding_ids",
    "affected_callers",
    "behavior_to_preserve",
    "unresolved_questions",
    "verification",
    "shared_helpers",
)


def create_parser():
    parser = argparse.ArgumentParser(
        description=(
            "Validate human-authored simplification reviews against deadcode "
            "evidence and the current source snapshot, then render text and JSON."
        )
    )
    parser.add_argument("--evidence", required=True, help="Evidence JSON produced by scripts/deadcode.py.")
    parser.add_argument("--reviews", required=True, help="Human/agent-authored review JSON in the existing session.")
    parser.add_argument("--repo", default=str(REPOSITORY_ROOT), help="Repository root for the source freshness check.")
    parser.add_argument("--format", choices=("text", "json"), default="text", help="Primary output format.")
    parser.add_argument("--output", help="Write primary output to this path instead of stdout.")
    parser.add_argument("--json", dest="json_output", help="Also write the complete reviewed report as JSON.")
    return parser


def _load_json(path):
    try:
        value = json.loads(Path(path).read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError) as error:
        raise AnalysisError(f"cannot load {path}: {error}") from error
    if not isinstance(value, dict):
        raise AnalysisError(f"{path}: top-level JSON value must be an object")
    return value


def _same_snapshot(left, right):
    if not isinstance(left, dict) or not isinstance(right, dict):
        return False
    return all(left.get(field) == right.get(field) for field in ("revision", "tree_sha256"))


def _finding_map(evidence):
    symbols = evidence.get("symbols", [])
    if not isinstance(symbols, list):
        return {}
    return {
        symbol["id"]: symbol
        for symbol in symbols
        if isinstance(symbol, dict) and isinstance(symbol.get("id"), str)
    }


def _valid_evidence_id(value, findings, evidence):
    if value in findings:
        return True
    if "@" in value:
        finding_id, target = value.split("@", 1)
        return finding_id in findings and target in findings[finding_id].get("targets", {})
    if value.startswith("T:"):
        return any(target.get("id") == value for target in evidence.get("targets", []))
    return any(command.get("id") == value for command in evidence.get("commands", []))


def _validate_observations(review, findings, evidence, errors):
    observations = review.get("observations")
    if not isinstance(observations, list) or not observations:
        errors.append("observations must be a non-empty array")
        return
    for index, observation in enumerate(observations):
        if not isinstance(observation, dict):
            errors.append(f"observations[{index}] must be an object")
            continue
        text = observation.get("text")
        if not isinstance(text, str) or not text.strip():
            errors.append(f"observations[{index}].text must be a non-empty string")
        evidence_ids = observation.get("evidence_ids")
        if not isinstance(evidence_ids, list) or not evidence_ids:
            errors.append(f"observations[{index}].evidence_ids must be non-empty")
            continue
        for evidence_id in evidence_ids:
            if not isinstance(evidence_id, str) or not _valid_evidence_id(evidence_id, findings, evidence):
                errors.append(f"observations[{index}] has unknown evidence id {evidence_id!r}")


def _validate_verification(review, errors):
    commands = review.get("verification", [])
    if not commands:
        errors.append("verification must name exact developer-toolkit commands")
    for command in commands:
        if not isinstance(command, str) or not command.startswith(
            "rcc run -r developer/toolkit.yaml --dev -t "
        ):
            errors.append(f"verification is not an exact contained toolkit command: {command!r}")


def _validate_removal(review, findings, evidence, errors):
    for finding_id in review["finding_ids"]:
        symbol = findings[finding_id]
        aggregate = symbol["aggregate"]
        if not aggregate["removal_evidence_complete"] or symbol.get("exported"):
            blockers = "; ".join(aggregate["removal_blocked_reasons"]) or "incomplete reachability evidence"
            if symbol.get("exported") and "exported" not in blockers:
                blockers = (blockers + "; " if blockers else "") + "exported API compatibility is unresolved"
            errors.append(f"remove is unsupported for {finding_id}: {blockers}")
    scope = evidence.get("scope", {})
    if scope.get("packages") != ["./..."] or scope.get("build_tags"):
        errors.append("remove requires the full ./... package scope with no custom build tags")
    if not evidence["source"].get("stable") or not evidence["coverage"].get("complete"):
        errors.append("remove requires a stable source snapshot and complete supported-target coverage")


def validate_reviews(evidence, review_document):
    errors = []
    findings = _finding_map(evidence)
    if evidence.get("schema_version") != SCHEMA_VERSION or evidence.get("kind") != "rcc-deadcode-evidence":
        errors.append("unsupported evidence schema")
    if review_document.get("schema_version") != SCHEMA_VERSION:
        errors.append("unsupported review schema")
    if not _same_snapshot(evidence.get("source", {}), review_document.get("source", {})):
        errors.append("review document is not attached to the evidence source snapshot")
    reviews = review_document.get("reviews")
    if not isinstance(reviews, list) or not reviews:
        errors.append("reviews must be a non-empty array")
        reviews = []

    briefs = []
    seen_ids = set()
    for index, review in enumerate(reviews):
        if not isinstance(review, dict):
            errors.append(f"reviews[{index}] must be an object")
            continue
        brief = dict(review)
        raw_id = review.get("id")
        label = raw_id if isinstance(raw_id, str) and raw_id.strip() else f"reviews[{index}]"
        brief["id"] = label
        for field in REQUIRED_STRINGS:
            value = review.get(field)
            if not isinstance(value, str) or not value.strip():
                errors.append(f"{label}: {field} must be a non-empty string")
                if field != "id":
                    brief[field] = ""
        for field in REQUIRED_LISTS:
            value = review.get(field)
            if not isinstance(value, list):
                errors.append(f"{label}: {field} must be an array")
                brief[field] = []
                continue
            invalid = [item for item in value if not isinstance(item, str)]
            if invalid:
                errors.append(f"{label}: {field} must contain only strings")
            brief[field] = [item for item in value if isinstance(item, str)]
        if label in seen_ids:
            errors.append(f"{label}: duplicate review id")
        seen_ids.add(label)

        decision = review.get("decision")
        if not isinstance(decision, str) or decision not in DECISIONS:
            errors.append(f"{label}: unsupported decision {decision!r}")
            decision = ""
            brief["decision"] = ""
        finding_ids = brief["finding_ids"]
        if not finding_ids:
            errors.append(f"{label}: at least one finding id is required")
        unknown = [finding_id for finding_id in finding_ids if finding_id not in findings]
        if unknown:
            errors.append(f"{label}: unknown finding ids: {', '.join(unknown)}")

        observations = review.get("observations")
        if not isinstance(observations, list):
            errors.append(f"{label}: observations must be an array")
            observations = []
            brief["observations"] = []
        else:
            _validate_observations(review, findings, evidence, errors)
            normalized_observations = []
            for observation in observations:
                if not isinstance(observation, dict):
                    continue
                normalized = dict(observation)
                if not isinstance(normalized.get("text"), str):
                    normalized["text"] = ""
                evidence_ids = normalized.get("evidence_ids")
                if not isinstance(evidence_ids, list):
                    evidence_ids = []
                normalized["evidence_ids"] = [item for item in evidence_ids if isinstance(item, str)]
                normalized_observations.append(normalized)
            brief["observations"] = normalized_observations
        _validate_verification(brief, errors)
        requirement = review.get("requirement")
        if decision == "repair_or_finish" and not isinstance(requirement, str):
            errors.append(f"{label}: repair_or_finish requires a requirement field")
            brief["requirement"] = ""
        elif decision == "repair_or_finish" and not requirement.strip():
            errors.append(f"{label}: repair_or_finish requires a justified requirement")
        if decision == "remove" and finding_ids and not unknown:
            _validate_removal(brief, findings, evidence, errors)

        evidence_source = evidence.get("source", {})
        if not isinstance(evidence_source, dict):
            evidence_source = {}
        brief["source"] = {
            "revision": evidence_source.get("revision"),
            "tree_sha256": evidence_source.get("tree_sha256"),
        }
        brief["evidence_ids"] = list(finding_ids)
        unresolved = brief["unresolved_questions"]
        if decision == "retain":
            implementation_status = "no_change"
        elif decision == "investigate_further" or unresolved:
            implementation_status = "blocked"
        else:
            implementation_status = "ready_for_separately_authorized_task"
        brief["implementation_status"] = implementation_status
        briefs.append(brief)
    return briefs, errors


def render_text(report):
    lines = [
        "# RCC simplification review",
        "",
        f"Review status: {report['review']['status'].upper()}",
        f"Source revision: {report['source']['revision']}",
        f"Working-tree fingerprint: {report['source']['tree_sha256']}",
        f"Evidence SHA-256: {report['evidence_sha256']}",
        "Analysis boundary: static cross-target evidence; native Windows/macOS execution NOT PERFORMED.",
        "Authority: recommendations are handoffs for separately authorized work; no code was changed by this review.",
        "",
    ]
    for brief in report["review"]["briefs"]:
        lines.extend(
            (
                f"## {brief['id']}: {brief['title']}",
                "",
                f"Decision: {brief['decision']}",
                f"Implementation handoff: {brief['implementation_status']}",
                f"Findings: {', '.join(brief['finding_ids'])}",
                "",
                "### What the code does",
                brief["what_it_does"],
                "",
                "### Observed facts",
            )
        )
        for observation in brief["observations"]:
            lines.append(f"- {observation['text']} Evidence: {', '.join(observation['evidence_ids'])}.")
        lines.extend(
            (
                "",
                "### Recommendation",
                f"- Simpler alternative: {brief['active_alternative']}",
                f"- Expected benefit: {brief['benefit']}",
                f"- Smallest proposed change: {brief['smallest_change']}",
                f"- Shared helpers to review/preserve: {', '.join(brief['shared_helpers']) or 'None identified'}",
                "",
                "### Contract and verification",
                "- Affected callers: " + ("; ".join(brief["affected_callers"]) or "None found in the loaded packages"),
                "- Behavior to preserve: " + "; ".join(brief["behavior_to_preserve"]),
                "- Unresolved questions: " + ("; ".join(brief["unresolved_questions"]) or "None"),
                "- Exact verification:",
            )
        )
        for command in brief["verification"]:
            lines.append(f"  - `{command}`")
        if brief["decision"] == "repair_or_finish":
            lines.append(f"- Requirement justifying completion: {brief['requirement']}")
        lines.append("")
    if report["review"]["errors"]:
        lines.append("## Validation errors")
        lines.extend(f"- {error}" for error in report["review"]["errors"])
    return "\n".join(lines) + "\n"


def _write(path, content):
    destination = Path(path)
    destination.parent.mkdir(parents=True, exist_ok=True)
    destination.write_text(content, encoding="utf-8")


def run_cli(
    arguments=None,
    *,
    runner=subprocess.run,
    snapshotter=source_snapshot,
    stdout=sys.stdout,
    stderr=sys.stderr,
):
    options = create_parser().parse_args(arguments)
    repo_root = Path(options.repo).resolve()
    for field in ("evidence", "reviews", "output", "json_output"):
        value = getattr(options, field)
        if value:
            setattr(options, field, str(repository_path(repo_root, value)))
    try:
        evidence_path = Path(options.evidence)
        raw_evidence = evidence_path.read_bytes()
        evidence = json.loads(raw_evidence)
        if not isinstance(evidence, dict):
            raise AnalysisError("evidence top-level JSON value must be an object")
        review_document = _load_json(options.reviews)
    except (OSError, json.JSONDecodeError, AnalysisError) as error:
        print(f"ERROR: {error}", file=stderr)
        return 2

    briefs, errors = validate_reviews(evidence, review_document)
    try:
        current = snapshotter(runner, repo_root)
        fresh = _same_snapshot(evidence.get("source", {}), current)
        if not fresh:
            errors.append("source snapshot is stale; re-run deadcode and recheck every recommendation")
    except AnalysisError as error:
        fresh = False
        errors.append(f"source freshness could not be established: {error}")
    if not evidence.get("source", {}).get("stable"):
        fresh = False
        errors.append("evidence source snapshot was unstable during analysis")

    status = "complete" if not errors else ("stale" if not fresh else "invalid")
    for brief in briefs:
        if errors and brief["implementation_status"] != "no_change":
            brief["implementation_status"] = "blocked"
    report = {
        "schema_version": SCHEMA_VERSION,
        "kind": "rcc-simplification-review",
        "source": {
            "revision": evidence.get("source", {}).get("revision"),
            "tree_sha256": evidence.get("source", {}).get("tree_sha256"),
            "fresh": fresh,
        },
        "evidence_sha256": hashlib.sha256(raw_evidence).hexdigest(),
        "analysis_boundary": "static_cross_target_analysis",
        "native_execution": "not_performed",
        "review": {"status": status, "briefs": briefs, "errors": errors},
    }
    rendered_json = json.dumps(report, indent=2, sort_keys=True) + "\n"
    primary = rendered_json if options.format == "json" else render_text(report)
    if options.output:
        _write(options.output, primary)
    else:
        stdout.write(primary)
    if options.json_output:
        _write(options.json_output, rendered_json)
    return 0 if not errors else 1


def main():
    return run_cli()


if __name__ == "__main__":
    sys.exit(main())
