import importlib.util
import io
import json
import tempfile
import unittest
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
SCRIPT = Path(__file__).with_name("deadcode_review.py")
SPEC = importlib.util.spec_from_file_location("rcc_deadcode_review", SCRIPT)
reviewer = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(reviewer)

SOURCE = {"revision": "abc123", "tree_sha256": "tree-1", "state": "clean", "stable": True}
TARGETS = ("linux/amd64", "darwin/amd64", "darwin/arm64", "windows/amd64")


def target_evidence(state):
    return {
        "state": state,
        "presence": "present" if state != "absent" else "absent",
        "application": "unreachable" if state == "unreachable_both" else "reachable",
        "test_inclusive": "unreachable" if state == "unreachable_both" else "reachable",
        "command_ids": [],
        "diagnostics": [],
    }


def finding(identifier, package, name, states, cross_target=False, blockers=None):
    return {
        "id": identifier,
        "package": package,
        "name": name,
        "position": {"file": "fixture.go", "line": 1, "column": 1},
        "selected": True,
        "exported": False,
        "generated": False,
        "test_file": False,
        "marker": False,
        "linkname": False,
        "interface_obligations": [],
        "targets": {target: target_evidence(states.get(target, "unknown")) for target in TARGETS},
        "aggregate": {
            "state": "unreachable_both" if cross_target else "unknown",
            "cross_target_unreachable": cross_target,
            "removal_evidence_complete": cross_target,
            "removal_blocked_reasons": blockers or ([] if cross_target else ["incomplete evidence"]),
        },
    }


def evidence_document():
    symbols = [
        finding("F001", "github.com/joshyorko/rcc/pretty", "csif", {target: "unreachable_both" for target in TARGETS}, True),
        finding(
            "F002",
            "github.com/joshyorko/rcc/shell",
            "Task.StderrOnly",
            {target: "unreachable_both" for target in TARGETS[:-1]} | {"windows/amd64": "reachable_both"},
            blockers=["Windows caller must remain"],
        ),
        finding("F003", "github.com/joshyorko/rcc/operations", "loopExecutor", {target: "unreachable_both" for target in TARGETS}, True),
        finding("F004", "github.com/joshyorko/rcc/operations", "unzipper.Explode", {target: "unknown" for target in TARGETS}),
        finding("F005", "github.com/joshyorko/rcc/wizard", "memberValidation", {target: "unreachable_both" for target in TARGETS}, True),
        finding("F006", "github.com/joshyorko/rcc/buildcoord", "ArtifactVerifierFunc.VerifyArtifact", {target: "unreachable_both" for target in TARGETS}, True),
    ]
    return {
        "schema_version": 1,
        "kind": "rcc-deadcode-evidence",
        "source": SOURCE,
        "coverage": {"complete": True},
        "targets": [{"id": f"T:{target}", "target": target, "status": "complete"} for target in TARGETS],
        "commands": [{"id": "C001", "target": "windows/amd64", "mode": "application"}],
        "symbols": symbols,
        "review": {"status": "not_performed", "briefs": []},
    }


def review(identifier="review-1", finding_ids=None, decision="retain", **overrides):
    value = {
        "id": identifier,
        "title": "Bounded simplification opportunity",
        "finding_ids": finding_ids or ["F002"],
        "what_it_does": "Explains the selected code in ordinary language.",
        "observations": [
            {
                "text": "Windows reaches the method through conda/platform_windows.go:89.",
                "evidence_ids": ["F002@windows/amd64"],
            }
        ],
        "active_alternative": "Retain the existing implementation; no simpler replacement is proposed.",
        "shared_helpers": [],
        "decision": decision,
        "benefit": "Avoids a Windows regression.",
        "affected_callers": ["conda/platform_windows.go:89 calls shell.Task.StderrOnly"],
        "behavior_to_preserve": ["stderr-only process execution on Windows"],
        "unresolved_questions": [],
        "smallest_change": "No code change.",
        "verification": ["rcc run -r developer/toolkit.yaml --dev -t unitTests"],
        "requirement": "",
    }
    value.update(overrides)
    return value


def review_document(*reviews):
    return {
        "schema_version": 1,
        "source": {"revision": SOURCE["revision"], "tree_sha256": SOURCE["tree_sha256"]},
        "reviews": list(reviews),
    }


def current_snapshot(_runner, _root):
    return dict(SOURCE, file_count=1)


class DeadcodeReviewTests(unittest.TestCase):
    def run_review(self, evidence, document, snapshotter=current_snapshot, output_format="json"):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            evidence_path = root / "evidence.json"
            reviews_path = root / "reviews.json"
            evidence_path.write_text(json.dumps(evidence))
            reviews_path.write_text(json.dumps(document))
            stdout = io.StringIO()
            stderr = io.StringIO()
            code = reviewer.run_cli(
                [
                    "--evidence",
                    str(evidence_path),
                    "--reviews",
                    str(reviews_path),
                    "--format",
                    output_format,
                    "--repo",
                    str(ROOT),
                ],
                snapshotter=snapshotter,
                stdout=stdout,
                stderr=stderr,
            )
            self.assertTrue(stdout.getvalue(), stderr.getvalue())
            if output_format == "json":
                return code, json.loads(stdout.getvalue()), stderr.getvalue()
            return code, stdout.getvalue(), stderr.getvalue()

    def test_keep_decision_uses_windows_evidence_and_preserves_caller(self):
        code, report, _ = self.run_review(evidence_document(), review_document(review()))

        self.assertEqual(0, code)
        self.assertEqual("complete", report["review"]["status"])
        brief = report["review"]["briefs"][0]
        self.assertEqual("no_change", brief["implementation_status"])
        self.assertEqual(SOURCE["revision"], brief["source"]["revision"])
        self.assertEqual("F002@windows/amd64", brief["observations"][0]["evidence_ids"][0])

    def test_stale_source_requires_recheck_and_blocks_handoff(self):
        def stale(_runner, _root):
            return dict(SOURCE, tree_sha256="changed", file_count=1)

        proposal = review(decision="simplify_in_place")
        code, report, _ = self.run_review(evidence_document(), review_document(proposal), snapshotter=stale)

        self.assertEqual(1, code)
        self.assertEqual("stale", report["review"]["status"])
        self.assertFalse(report["source"]["fresh"])
        self.assertEqual("blocked", report["review"]["briefs"][0]["implementation_status"])
        self.assertIn("re-run deadcode", "\n".join(report["review"]["errors"]))

    def test_unknown_or_reachable_target_blocks_remove(self):
        proposal = review(decision="remove")

        code, report, _ = self.run_review(evidence_document(), review_document(proposal))

        self.assertEqual(1, code)
        self.assertEqual("invalid", report["review"]["status"])
        self.assertIn("remove is unsupported", "\n".join(report["review"]["errors"]))
        self.assertEqual("blocked", report["review"]["briefs"][0]["implementation_status"])

    def test_cross_target_unreachable_symbol_can_have_bounded_remove_proposal(self):
        proposal = review(
            finding_ids=["F001"],
            decision="remove",
            observations=[{"text": "No application or test references occur on any complete target.", "evidence_ids": ["F001"]}],
            active_alternative="Callers can use csi(fmt.Sprintf(...)) if one appears later.",
            benefit="Removes an unnecessary formatting wrapper, not formatting itself.",
            affected_callers=[],
            behavior_to_preserve=["Existing csi formatting and all pretty output"],
            smallest_change="Remove only csif in a separately authorized change.",
        )

        code, report, _ = self.run_review(evidence_document(), review_document(proposal))

        self.assertEqual(0, code)
        self.assertEqual("ready_for_separately_authorized_task", report["review"]["briefs"][0]["implementation_status"])

    def test_unresolved_question_blocks_implementation_without_erasing_review(self):
        proposal = review(
            decision="reuse_or_consolidate",
            unresolved_questions=["Does the alternative preserve bounds and defaults?"],
        )

        code, report, _ = self.run_review(evidence_document(), review_document(proposal))

        self.assertEqual(0, code)
        self.assertEqual("complete", report["review"]["status"])
        self.assertEqual("blocked", report["review"]["briefs"][0]["implementation_status"])

    def test_parallel_experiment_requires_requirement_and_names_ignored_result(self):
        proposal = review(
            identifier="parallel-extraction",
            finding_ids=["F003", "F004"],
            decision="investigate_further",
            what_it_does="Explode distributes archive entries to loopExecutor workers; Extract is the active sequential path.",
            observations=[
                {
                    "text": "operations/zipper.go:156 ignores the boolean returned by Execute before Explode returns nil.",
                    "evidence_ids": ["F003", "F004"],
                }
            ],
            active_alternative="Keep Extract and its error-returning WriteTarget.execute path.",
            shared_helpers=["operations/zipper.go:WriteTarget.execute", "operations/zipper.go:zipEntryTarget"],
            benefit="Avoids enabling an experiment with unspecified error and cleanup behavior.",
            affected_callers=["operations/zipper.go:Extract is the active alternative"],
            behavior_to_preserve=["flattening", "error propagation", "worker and goroutine cleanup"],
            unresolved_questions=["What requirement justifies parallel extraction and its error contract?"],
            smallest_change="Do not enable Explode; first specify and test the requirement.",
            verification=[
                "rcc run -r developer/toolkit.yaml --dev -t deadcode -- --target windows/amd64 --package ./operations",
                "rcc run -r developer/toolkit.yaml --dev -t unitTests",
            ],
        )

        code, text, _ = self.run_review(evidence_document(), review_document(proposal), output_format="text")

        self.assertEqual(0, code)
        self.assertIn("ignores the boolean returned by Execute", text)
        self.assertIn("Implementation handoff: blocked", text)
        self.assertIn("WriteTarget.execute", text)
        self.assertIn("What requirement justifies parallel extraction", text)

    def test_repair_or_finish_without_requirement_is_invalid(self):
        proposal = review(decision="repair_or_finish", requirement="")

        code, report, _ = self.run_review(evidence_document(), review_document(proposal))

        self.assertEqual(1, code)
        self.assertIn("justified requirement", "\n".join(report["review"]["errors"]))

    def test_wizard_reuse_brief_preserves_defaults_input_and_bounds(self):
        proposal = review(
            identifier="wizard-reuse",
            finding_ids=["F005"],
            decision="reuse_or_consolidate",
            what_it_does="memberValidation performs exact membership validation; choose builds numeric selection keys.",
            observations=[{"text": "wizard/create.go:choose compiles a numeric regular expression instead of reusing memberValidation.", "evidence_ids": ["F005"]}],
            active_alternative="Pass the existing numeric keys to memberValidation and remove only the dynamic selection regexp.",
            benefit="Reuses one exact-membership implementation without changing selection semantics.",
            affected_callers=["wizard/create.go:choose", "wizard/create.go:Create"],
            behavior_to_preserve=["default selection 1", "whitespace trimming", "EOF handling", "0 and N+1 rejection", "empty-candidate behavior"],
            smallest_change="Replace only the choose validator and add table-driven tests before refactoring.",
        )

        code, text, _ = self.run_review(evidence_document(), review_document(proposal), output_format="text")

        self.assertEqual(0, code)
        self.assertIn("default selection 1", text)
        self.assertIn("0 and N+1 rejection", text)
        self.assertIn("Implementation handoff: ready_for_separately_authorized_task", text)

    def test_test_only_adapter_consolidation_does_not_claim_production_use(self):
        proposal = review(
            identifier="test-adapter",
            finding_ids=["F006"],
            decision="reuse_or_consolidate",
            what_it_does="ArtifactVerifierFunc adapts a function to ArtifactVerifier; buildcoord_test.go duplicates that adapter privately.",
            observations=[{"text": "Test-inclusive reachability still reports the public method because tests use the private adapter; this does not establish production use.", "evidence_ids": ["F006"]}],
            active_alternative="Use buildcoord.ArtifactVerifierFunc in the two test call sites and delete only the duplicate test adapter.",
            benefit="Removes duplicate test machinery while exercising the public adapter.",
            affected_callers=["buildcoord/buildcoord_test.go:132", "buildcoord/buildcoord_test.go:211"],
            behavior_to_preserve=["verifier callback behavior", "exported API compatibility"],
            unresolved_questions=["Should the exported production adapter remain after test consolidation?"],
            smallest_change="Change only the test adapter in a later task; do not remove the exported production type.",
        )

        code, text, _ = self.run_review(evidence_document(), review_document(proposal), output_format="text")

        self.assertEqual(0, code)
        self.assertIn("does not establish production use", text)
        self.assertIn("Implementation handoff: blocked", text)
        self.assertIn("exported API compatibility", text)

    def test_unknown_evidence_and_uncontained_verification_are_rejected(self):
        proposal = review(
            observations=[{"text": "Unsupported observation.", "evidence_ids": ["F999"]}],
            verification=["go test ./..."],
        )

        code, report, _ = self.run_review(evidence_document(), review_document(proposal))

        self.assertEqual(1, code)
        errors = "\n".join(report["review"]["errors"])
        self.assertIn("unknown evidence id", errors)
        self.assertIn("not an exact contained toolkit command", errors)

    def test_text_and_json_have_consistent_review_fields(self):
        evidence = evidence_document()
        document = review_document(review())
        json_code, report, _ = self.run_review(evidence, document)
        text_code, text, _ = self.run_review(evidence, document, output_format="text")

        self.assertEqual((0, 0), (json_code, text_code))
        brief = report["review"]["briefs"][0]
        self.assertIn(brief["id"], text)
        self.assertIn(brief["decision"], text)
        self.assertIn(brief["implementation_status"], text)
        self.assertIn(report["source"]["tree_sha256"], text)
        self.assertIn(report["evidence_sha256"], text)


if __name__ == "__main__":
    unittest.main()
