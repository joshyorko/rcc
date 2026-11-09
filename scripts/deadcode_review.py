#!/usr/bin/env python3
"""Validate and render snapshot-bound RCC simplification work briefs."""

import argparse
import hashlib
import json
import subprocess
import sys
from pathlib import Path

from deadcode import REPOSITORY_ROOT, SCHEMA_VERSION, AnalysisError, source_snapshot


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
    return all(left.get(field) == right.get(field) for field in ("revision", "tree_sha256"))


def _finding_map(evidence):
    return {symbol["id"]: symbol for symbol in evidence.get("symbols", [])}


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
        if not observation.get("text", "").strip():
            errors.append(f"observations[{index}].text is required")
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
        if not aggregate["removal_evidence_complete"]:
            blockers = "; ".join(aggregate["removal_blocked_reasons"]) or "incomplete reachability evidence"
            errors.append(f"remove is unsupported for {finding_id}: {blockers}")
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
        label = review.get("id") or f"reviews[{index}]"
        for field in REQUIRED_STRINGS:
            if not isinstance(review.get(field), str) or not review.get(field, "").strip():
                errors.append(f"{label}: {field} is required")
        for field in REQUIRED_LISTS:
            if field == "verification":
                if not isinstance(review.get(field), list):
                    errors.append(f"{label}: {field} must be an array")
                continue
            if not isinstance(review.get(field), list):
                errors.append(f"{label}: {field} must be an array")
        if label in seen_ids:
            errors.append(f"{label}: duplicate review id")
        seen_ids.add(label)
        if review.get("decision") not in DECISIONS:
            errors.append(f"{label}: unsupported decision {review.get('decision')!r}")
        finding_ids = review.get("finding_ids", [])
        if not isinstance(finding_ids, list):
            finding_ids = []
        if not finding_ids:
            errors.append(f"{label}: at least one finding id is required")
        unknown = [finding_id for finding_id in finding_ids if finding_id not in findings]
        if unknown:
            errors.append(f"{label}: unknown finding ids: {', '.join(unknown)}")
        if not isinstance(review.get("observations"), list):
            errors.append(f"{label}: observations must be an array")
        else:
            _validate_observations(review, findings, evidence, errors)
        if isinstance(review.get("verification"), list):
            _validate_verification(review, errors)
        if (
            review.get("decision") == "repair_or_finish"
            and not isinstance(review.get("requirement"), str)
        ):
            errors.append(f"{label}: repair_or_finish requires a requirement field")
        elif review.get("decision") == "repair_or_finish" and not review.get("requirement", "").strip():
            errors.append(f"{label}: repair_or_finish requires a justified requirement")
        if review.get("decision") == "remove" and not unknown:
            _validate_removal(review, findings, evidence, errors)

        brief = dict(review)
        brief["finding_ids"] = list(finding_ids)
        brief["source"] = {
            "revision": evidence.get("source", {}).get("revision"),
            "tree_sha256": evidence.get("source", {}).get("tree_sha256"),
        }
        brief["evidence_ids"] = list(finding_ids)
        unresolved = review.get("unresolved_questions", [])
        if not isinstance(unresolved, list):
            unresolved = []
            brief["unresolved_questions"] = []
        if review.get("decision") == "retain":
            implementation_status = "no_change"
        elif review.get("decision") == "investigate_further" or unresolved:
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
        current = snapshotter(runner, Path(options.repo).resolve())
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
