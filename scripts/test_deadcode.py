import hashlib
import importlib.util
import io
import json
import os
import subprocess
import tempfile
import unittest
from contextlib import redirect_stderr
from pathlib import Path
from unittest.mock import patch


ROOT = Path(__file__).resolve().parents[1]
SCRIPT = Path(__file__).with_name("deadcode.py")
SPEC = importlib.util.spec_from_file_location("rcc_deadcode", SCRIPT)
deadcode = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(deadcode)

PACKAGE = "example.com/fixture"
LIVE = (PACKAGE, "Live")
TEST_ONLY = (PACKAGE, "UsedByTestOnly")
DEAD = (PACKAGE, "NeverUsed")
HELPER = (PACKAGE, "deadHelper")
SHARED = (PACKAGE, "Shared")


def analyzer_payload(*keys):
    packages = {}
    for package, name in keys:
        packages.setdefault(package, []).append(
            {
                "Name": name,
                "Position": {"File": "fixture.go", "Line": 9, "Col": 6},
                "Generated": False,
                "Marker": False,
            }
        )
    return json.dumps(
        [
            {"Name": path.rsplit("/", 1)[-1], "Path": path, "Funcs": functions}
            for path, functions in sorted(packages.items())
        ]
    )


def inventory_symbol(key, **overrides):
    package, name = key
    value = {
        "package": package,
        "name": name,
        "position": {"file": "fixture.go", "line": 9, "column": 6},
        "exported": False,
        "generated": False,
        "test_file": False,
        "marker": False,
        "linkname": False,
        "interface_obligations": [],
        "references": [],
        "calls": [],
    }
    value.update(overrides)
    return value


def snapshot(revision="abc123", tree="tree-1"):
    return {
        "revision": revision,
        "tree_sha256": tree,
        "state": "clean",
        "file_count": 1,
    }


def static_snapshot(*values):
    remaining = list(values or [snapshot()])

    def take_snapshot(_runner, _root):
        if len(remaining) > 1:
            return remaining.pop(0)
        return remaining[0]

    return take_snapshot


class FakeRunner:
    def __init__(self, analyzers=None, inventories=None, metadata=None, scopes=None):
        self.analyzers = analyzers or {}
        self.inventories = inventories or {}
        self.metadata = metadata or {}
        self.scopes = scopes or {}
        self.calls = []

    def __call__(self, command, **details):
        self.calls.append((command, details))
        environment = details["env"]
        target = f"{environment.get('GOOS')}/{environment.get('GOARCH')}"
        if command == ["go", "version"]:
            if "go-version" in self.metadata:
                return self.metadata["go-version"]
            return self._result(command, stdout="go version go1.26.9 linux/amd64\n")
        if command[:2] == ["go", "list"] and "-m" in command:
            if "analyzer-version" in self.metadata:
                return self.metadata["analyzer-version"]
            return self._result(command, stdout="v0.48.0\n")
        if command[:2] == ["go", "list"]:
            return self._result(command, stdout=self.scopes.get(target, PACKAGE + "\n"))
        if command[:2] == ["go", "env"]:
            return self._result(command, stdout='{"GOHOSTOS":"linux","GOHOSTARCH":"amd64"}')
        if command[0] == "/deadcode":
            mode = "test" if "-test" in command else "app"
            result = self.analyzers.get((target, mode))
            if isinstance(result, subprocess.CompletedProcess):
                return result
            return self._result(command, stdout=result or "[]")
        if command[0] == "/inventory":
            selected = {
                tuple(value.split("|", 1))
                for index, value in enumerate(command[:-1])
                if index and command[index - 1] == "-symbol"
            }
            symbols = [
                symbol
                for symbol in self.inventories.get(target, [])
                if (symbol["package"], symbol["name"]) in selected
            ]
            result = self.inventories.get((target, "result"))
            if isinstance(result, subprocess.CompletedProcess):
                return result
            return self._result(
                command,
                stdout=json.dumps({"schema_version": 1, "symbols": symbols}),
            )
        if command[:2] == ["go", "build"]:
            return self._result(command)
        raise AssertionError(f"unexpected command: {command}")

    @staticmethod
    def _result(command, returncode=0, stdout="", stderr=""):
        return subprocess.CompletedProcess(command, returncode, stdout, stderr)


class DeadcodeTests(unittest.TestCase):
    def run_report(self, runner, extra=None, snapshots=None):
        stdout = io.StringIO()
        stderr = io.StringIO()
        arguments = [
            "--format",
            "json",
            "--analyzer-binary",
            "/deadcode",
            "--inventory-binary",
            "/inventory",
        ]
        arguments.extend(extra or [])
        code = deadcode.run_cli(
            arguments,
            runner=runner,
            stdout=stdout,
            stderr=stderr,
            repo_root=ROOT,
            snapshotter=snapshots or static_snapshot(),
        )
        self.assertTrue(stdout.getvalue(), stderr.getvalue())
        return code, json.loads(stdout.getvalue()), stderr.getvalue()

    @staticmethod
    def symbol(report, key):
        return next(
            symbol
            for symbol in report["symbols"]
            if (symbol["package"], symbol["name"]) == key
        )

    def test_parse_findings_normalizes_paths_sorts_and_retains_flags(self):
        payload = json.dumps(
            [
                {
                    "Path": PACKAGE,
                    "Funcs": [
                        {
                            "Name": "z",
                            "Position": {"File": str(ROOT / "z.go"), "Line": 2, "Col": 3},
                            "Generated": True,
                            "Marker": True,
                        },
                        {
                            "Name": "a",
                            "Position": {"File": "a.go", "Line": 1, "Col": 2},
                        },
                    ],
                }
            ]
        )

        findings = deadcode.parse_findings(payload, ROOT)

        self.assertEqual(["a", "z"], [finding.name for finding in findings])
        self.assertEqual("a.go", findings[0].file)
        self.assertTrue(findings[1].generated)
        self.assertTrue(findings[1].marker)

    def test_null_analyzer_output_means_no_findings(self):
        self.assertEqual([], deadcode.parse_findings("null\n", ROOT))

    def test_application_and_test_reachability_are_classified_correctly(self):
        runner = FakeRunner(
            analyzers={
                ("linux/amd64", "app"): analyzer_payload(TEST_ONLY, DEAD),
                ("linux/amd64", "test"): analyzer_payload(DEAD),
            },
            inventories={"linux/amd64": [inventory_symbol(key) for key in (LIVE, TEST_ONLY, DEAD)]},
        )

        code, report, _ = self.run_report(
            runner,
            ["--target", "linux/amd64", "--symbol", f"{PACKAGE}:Live"],
        )

        self.assertEqual(0, code)
        self.assertEqual("test_only", self.symbol(report, TEST_ONLY)["targets"]["linux/amd64"]["state"])
        self.assertEqual("unreachable_both", self.symbol(report, DEAD)["targets"]["linux/amd64"]["state"])
        self.assertEqual("reachable_both", self.symbol(report, LIVE)["targets"]["linux/amd64"]["state"])

    def test_identical_analyzer_outputs_remain_unreachable_in_both_modes(self):
        runner = FakeRunner(
            analyzers={
                ("linux/amd64", "app"): analyzer_payload(DEAD),
                ("linux/amd64", "test"): analyzer_payload(DEAD),
            },
            inventories={"linux/amd64": [inventory_symbol(DEAD)]},
        )

        _, report, _ = self.run_report(runner, ["--target", "linux/amd64"])

        self.assertEqual(1, len(report["symbols"]))
        self.assertEqual("unreachable_both", report["symbols"][0]["targets"]["linux/amd64"]["state"])

    def test_default_target_matrix_is_explicit_and_ignores_inherited_target(self):
        runner = FakeRunner(
            inventories={
                target: [inventory_symbol(LIVE)]
                for target in ("linux/amd64", "darwin/amd64", "darwin/arm64", "windows/amd64")
            }
        )
        with patch.dict(os.environ, {"GOOS": "plan9", "GOARCH": "mips", "GOFLAGS": "-tags=hidden"}, clear=False):
            code, report, _ = self.run_report(runner, ["--symbol", f"{PACKAGE}:Live"])

        self.assertEqual(0, code)
        self.assertTrue(report["coverage"]["complete"])
        self.assertEqual(4, report["coverage"]["completed_targets"])
        analyzer_calls = [call for call in runner.calls if call[0][0] == "/deadcode"]
        inventory_calls = [call for call in runner.calls if call[0][0] == "/inventory"]
        scope_calls = [
            call
            for call in runner.calls
            if call[0][:2] == ["go", "list"] and "-m" not in call[0]
        ]
        self.assertEqual(8, len(analyzer_calls))
        self.assertEqual(4, len(inventory_calls))
        self.assertEqual(4, len(scope_calls))
        for _, details in analyzer_calls + inventory_calls:
            self.assertEqual("0", details["env"]["CGO_ENABLED"])
            self.assertEqual("", details["env"]["GOFLAGS"])
            self.assertNotEqual("plan9", details["env"]["GOOS"])
            self.assertEqual("2", details["env"]["GO_TELEMETRY_CHILD"])

    def test_selected_target_leaves_other_targets_explicitly_skipped(self):
        runner = FakeRunner(inventories={"linux/amd64": [inventory_symbol(DEAD)]})

        code, report, _ = self.run_report(
            runner,
            ["--target", "linux/amd64", "--symbol", f"{PACKAGE}:NeverUsed"],
        )

        self.assertEqual(0, code)
        self.assertEqual("skipped", report["targets"][1]["status"])
        self.assertFalse(report["coverage"]["complete"])
        self.assertFalse(report["symbols"][0]["aggregate"]["removal_evidence_complete"])
        self.assertEqual("unknown", report["symbols"][0]["targets"]["windows/amd64"]["state"])

    def test_failed_target_preserves_partial_evidence_and_blocks_removal(self):
        failure = subprocess.CompletedProcess([], 1, "", "windows package loading failed")
        runner = FakeRunner(
            analyzers={
                ("linux/amd64", "app"): analyzer_payload(DEAD),
                ("linux/amd64", "test"): analyzer_payload(DEAD),
                ("windows/amd64", "app"): failure,
                ("windows/amd64", "test"): analyzer_payload(DEAD),
            },
            inventories={
                "linux/amd64": [inventory_symbol(DEAD)],
                "windows/amd64": [inventory_symbol(DEAD)],
            },
        )

        code, report, _ = self.run_report(
            runner,
            ["--target", "linux/amd64", "--target", "windows/amd64"],
        )

        self.assertEqual(1, code)
        windows = next(target for target in report["targets"] if target["target"] == "windows/amd64")
        self.assertEqual("unknown", windows["status"])
        self.assertIn("windows package loading failed", "\n".join(windows["diagnostics"]))
        self.assertEqual("unreachable_both", self.symbol(report, DEAD)["targets"]["linux/amd64"]["state"])
        self.assertEqual("unknown", self.symbol(report, DEAD)["targets"]["windows/amd64"]["state"])
        self.assertFalse(self.symbol(report, DEAD)["aggregate"]["removal_evidence_complete"])
        self.assertEqual("not_performed", report["review"]["status"])

    def test_source_fingerprint_hashes_contents_untracked_files_and_deletions(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / "tracked.go").write_text("package one\n")
            (root / "untracked.go").write_text("package two\n")
            paths = ["tracked.go", "untracked.go"]
            first = deadcode.fingerprint_paths(root, paths)
            (root / "tracked.go").write_text("package changed\n")
            second = deadcode.fingerprint_paths(root, paths)
            (root / "untracked.go").write_text("package untrackedChanged\n")
            third = deadcode.fingerprint_paths(root, paths)
            (root / "tracked.go").unlink()
            fourth = deadcode.fingerprint_paths(root, paths)

        self.assertEqual(4, len({first, second, third, fourth}))
        self.assertNotEqual(hashlib.sha256(b"").hexdigest(), first)

    def test_source_change_during_analysis_makes_report_stale(self):
        runner = FakeRunner(inventories={"linux/amd64": [inventory_symbol(DEAD)]})
        snapshots = static_snapshot(snapshot(tree="before"), snapshot(tree="after"))

        code, report, _ = self.run_report(
            runner,
            ["--target", "linux/amd64"],
            snapshots=snapshots,
        )

        self.assertEqual(1, code)
        self.assertFalse(report["source"]["stable"])
        self.assertFalse(report["coverage"]["complete"])
        self.assertIn("Source changed", report["source"]["diagnostic"])

    def test_text_and_json_are_rendered_from_the_same_record(self):
        runner = FakeRunner(
            analyzers={
                ("linux/amd64", "app"): analyzer_payload(DEAD),
                ("linux/amd64", "test"): analyzer_payload(DEAD),
            },
            inventories={"linux/amd64": [inventory_symbol(DEAD)]},
        )

        _, report, _ = self.run_report(runner, ["--target", "linux/amd64"])
        text = deadcode.format_text(report)

        self.assertIn(report["source"]["tree_sha256"], text)
        self.assertIn(report["symbols"][0]["id"], text)
        self.assertIn("unreachable_both", text)
        self.assertIn("Review status: NOT PERFORMED", text)
        self.assertIn("native Windows and macOS execution NOT PERFORMED", text)
        summary = deadcode.format_text(report, summary=True)
        self.assertNotIn("Declaration:", summary)

    def test_selected_reachable_symbol_and_absent_platform_symbol_are_distinct(self):
        runner = FakeRunner(
            inventories={
                "linux/amd64": [],
                "windows/amd64": [inventory_symbol(LIVE)],
            }
        )

        code, report, _ = self.run_report(
            runner,
            [
                "--target",
                "linux/amd64",
                "--target",
                "windows/amd64",
                "--symbol",
                f"{PACKAGE}:Live",
            ],
        )

        self.assertEqual(0, code)
        symbol = self.symbol(report, LIVE)
        self.assertTrue(symbol["selected"])
        self.assertEqual("absent", symbol["targets"]["linux/amd64"]["state"])
        self.assertEqual("reachable_both", symbol["targets"]["windows/amd64"]["state"])
        self.assertEqual("reachable", symbol["aggregate"]["state"])
        self.assertFalse(symbol["aggregate"]["removal_evidence_complete"])

    def test_related_dead_functions_are_grouped_with_external_shared_callees(self):
        dead_facts = inventory_symbol(
            DEAD,
            calls=[
                {
                    "callee_package": PACKAGE,
                    "callee_name": "deadHelper",
                    "position": {"file": "fixture.go", "line": 12, "column": 2},
                    "kind": "call",
                    "test": False,
                    "callee_position": {"file": "fixture.go", "line": 20, "column": 6},
                },
                {
                    "callee_package": PACKAGE,
                    "callee_name": "Shared",
                    "position": {"file": "fixture.go", "line": 13, "column": 2},
                    "kind": "call",
                    "test": False,
                    "callee_position": {"file": "fixture.go", "line": 24, "column": 6},
                },
            ],
        )
        runner = FakeRunner(
            analyzers={
                ("linux/amd64", "app"): analyzer_payload(DEAD, HELPER),
                ("linux/amd64", "test"): analyzer_payload(DEAD, HELPER),
            },
            inventories={"linux/amd64": [dead_facts, inventory_symbol(HELPER)]},
        )

        _, report, _ = self.run_report(runner, ["--target", "linux/amd64"])

        self.assertEqual(1, len(report["opportunities"]))
        opportunity = report["opportunities"][0]
        self.assertEqual({DEAD[1], HELPER[1]}, {name.rsplit(":", 1)[1] for name in opportunity["symbols"]})
        self.assertEqual("deadHelper", opportunity["internal_references"][0]["callee"].rsplit(":", 1)[1])
        self.assertEqual([(PACKAGE, "Shared")], [(item["package"], item["name"]) for item in opportunity["external_callees"]])
        self.assertEqual("not_performed", opportunity["review_status"])

    def test_marker_method_is_unknown_not_assumed_reachable(self):
        runner = FakeRunner(inventories={"linux/amd64": [inventory_symbol(LIVE, marker=True)]})

        _, report, _ = self.run_report(
            runner,
            ["--target", "linux/amd64", "--symbol", f"{PACKAGE}:Live"],
        )

        self.assertEqual("unknown", self.symbol(report, LIVE)["targets"]["linux/amd64"]["state"])
        self.assertFalse(self.symbol(report, LIVE)["aggregate"]["removal_evidence_complete"])

    def test_focused_scope_and_custom_tags_never_authorize_repository_removal(self):
        for options in (["--package", "./pretty"], ["--tags", "custom"]):
            with self.subTest(options=options):
                analyzers = {}
                inventories = {}
                for target in ("linux/amd64", "darwin/amd64", "darwin/arm64", "windows/amd64"):
                    analyzers[(target, "app")] = analyzer_payload(DEAD)
                    analyzers[(target, "test")] = analyzer_payload(DEAD)
                    inventories[target] = [inventory_symbol(DEAD)]
                runner = FakeRunner(analyzers=analyzers, inventories=inventories)

                code, report, _ = self.run_report(runner, options)

                self.assertEqual(0, code)
                self.assertTrue(report["coverage"]["target_coverage_complete"])
                self.assertFalse(report["coverage"]["repository_scope_complete"])
                self.assertFalse(report["coverage"]["complete"])
                self.assertFalse(self.symbol(report, DEAD)["aggregate"]["removal_evidence_complete"])
                if "--package" in options:
                    analyzer_call = next(call for call in runner.calls if call[0][0] == "/deadcode")
                    self.assertIn("./cmd/rcc", analyzer_call[0])
                    self.assertIn("./cmd/rccremote", analyzer_call[0])
                    self.assertIn("./pretty", analyzer_call[0])

    def test_metadata_failure_or_version_mismatch_makes_coverage_unknown(self):
        cases = {
            "go-version": subprocess.CompletedProcess([], 1, "", "go unavailable"),
            "analyzer-version": subprocess.CompletedProcess([], 1, "", "module unavailable"),
            "mismatch": subprocess.CompletedProcess([], 0, "v0.47.0\n", ""),
        }
        for label, metadata_result in cases.items():
            with self.subTest(label=label):
                key = "mismatch" if label == "mismatch" else label
                metadata = {key: metadata_result} if key != "mismatch" else {"analyzer-version": metadata_result}
                runner = FakeRunner(
                    inventories={"linux/amd64": [inventory_symbol(DEAD)]},
                    metadata=metadata,
                )

                code, report, _ = self.run_report(runner, ["--target", "linux/amd64"])

                self.assertEqual(1, code)
                self.assertEqual("unknown", report["analyzer"]["status"])
                self.assertEqual("unknown", report["targets"][0]["status"])
                self.assertFalse(report["coverage"]["complete"])
                self.assertTrue(report["analyzer"]["diagnostics"])

    def test_exported_symbol_removal_requires_compatibility_review(self):
        analyzers = {}
        inventories = {}
        for target in ("linux/amd64", "darwin/amd64", "darwin/arm64", "windows/amd64"):
            analyzers[(target, "app")] = analyzer_payload(DEAD)
            analyzers[(target, "test")] = analyzer_payload(DEAD)
            inventories[target] = [inventory_symbol(DEAD, exported=True)]
        runner = FakeRunner(analyzers=analyzers, inventories=inventories)

        code, report, _ = self.run_report(runner)

        self.assertEqual(0, code)
        aggregate = self.symbol(report, DEAD)["aggregate"]
        self.assertTrue(aggregate["cross_target_unreachable"])
        self.assertFalse(aggregate["removal_evidence_complete"])
        self.assertIn("exported API", " ".join(aggregate["removal_blocked_reasons"]))

    def test_review_paths_and_template_are_repository_relative_and_consistent(self):
        runner = FakeRunner()
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / "developer").mkdir()
            previous = Path.cwd()
            try:
                os.chdir(root / "developer")
                code = deadcode.run_cli(
                    [
                        "--target",
                        "linux/amd64",
                        "--analyzer-binary",
                        "/deadcode",
                        "--inventory-binary",
                        "/inventory",
                        "--review",
                        "--output",
                        "build/deadcode/evidence.txt",
                    ],
                    runner=runner,
                    stdout=io.StringIO(),
                    stderr=io.StringIO(),
                    repo_root=root,
                    snapshotter=static_snapshot(),
                )
            finally:
                os.chdir(previous)
            evidence_path = root / "build" / "deadcode" / "evidence.json"
            text_path = root / "build" / "deadcode" / "evidence.txt"
            template_path = root / "build" / "deadcode" / "review-template.json"
            self.assertEqual(0, code)
            self.assertTrue(evidence_path.is_file())
            self.assertTrue(text_path.is_file())
            self.assertTrue(template_path.is_file())
            self.assertFalse((root / "developer" / "build").exists())
            evidence = json.loads(evidence_path.read_text())
            self.assertEqual(str(template_path), evidence["review_template"])
            self.assertIn(str(template_path), text_path.read_text())

    def test_toolkit_routes_review_arguments_directly_and_runs_real_fixtures(self):
        toolkit = (ROOT / "developer" / "toolkit.yaml").read_text()
        tasks = (ROOT / "tasks.py").read_text()

        self.assertIn("  deadcode:\n    shell: python ../scripts/deadcode.py\n", toolkit)
        self.assertIn("  deadcodeReview:\n    shell: python ../scripts/deadcode_review.py\n", toolkit)
        self.assertIn("  deadcodeTests:\n    shell: python call_invoke.py deadcodeTests\n", toolkit)
        self.assertIn("def deadcodeTests(c):", tasks)
        self.assertIn("scripts/test_deadcode.py", tasks)
        self.assertIn("scripts/test_deadcode_review.py", tasks)

    def test_removed_heuristic_flags_are_usage_errors(self):
        with redirect_stderr(io.StringIO()):
            with self.assertRaises(SystemExit) as raised:
                deadcode.create_parser().parse_args(["--max-refs", "1"])
        self.assertEqual(2, raised.exception.code)


class RealGoFixtureTests(unittest.TestCase):
    """Integration claims use actual Go loading and the official analyzer."""

    @classmethod
    def setUpClass(cls):
        cls.inventory_binary = ROOT / "build" / "deadcode" / "test-bin" / "deadcodeinventory"
        cls.analyzer_binary = ROOT / "build" / "deadcode" / "test-bin" / "deadcode"
        cls.inventory_binary.parent.mkdir(parents=True, exist_ok=True)
        environment = os.environ.copy()
        environment["CGO_ENABLED"] = "0"
        for binary, package in (
            (cls.inventory_binary, "./developer/deadcodeinventory"),
            (cls.analyzer_binary, "golang.org/x/tools/cmd/deadcode"),
        ):
            subprocess.run(
                ["go", "build", "-o", str(binary), package],
                cwd=ROOT,
                env=environment,
                check=True,
            )

    def run_fixture(self, package, *targets, symbol=None):
        stdout = io.StringIO()
        arguments = [
            "--format",
            "json",
            "--analyzer-binary",
            str(self.analyzer_binary),
            "--inventory-binary",
            str(self.inventory_binary),
            "--package",
            package,
        ]
        for target in targets:
            arguments.extend(("--target", target))
        if symbol:
            arguments.extend(("--symbol", symbol))
        code = deadcode.run_cli(
            arguments,
            runner=subprocess.run,
            stdout=stdout,
            stderr=io.StringIO(),
            repo_root=ROOT,
            snapshotter=static_snapshot(),
        )
        return code, json.loads(stdout.getvalue())

    def test_real_fixture_proves_application_test_and_grouping_facts(self):
        package = "github.com/joshyorko/rcc/scripts/testdata/deadcode/reachability"

        code, report = self.run_fixture(
            "./scripts/testdata/deadcode/reachability",
            "linux/amd64",
            symbol=f"{package}:Live",
        )

        self.assertEqual(0, code)
        by_name = {symbol["name"]: symbol for symbol in report["symbols"]}
        self.assertEqual("reachable_both", by_name["Live"]["targets"]["linux/amd64"]["state"])
        self.assertEqual("test_only", by_name["UsedByTestOnly"]["targets"]["linux/amd64"]["state"])
        self.assertEqual("unreachable_both", by_name["NeverUsed"]["targets"]["linux/amd64"]["state"])
        self.assertEqual("unreachable_both", by_name["deadHelper"]["targets"]["linux/amd64"]["state"])
        self.assertEqual(1, len(report["opportunities"]))
        outside = report["opportunities"][0]["external_callees"]
        self.assertIn((package, "Shared"), {(item["package"], item["name"]) for item in outside})
        test_references = by_name["UsedByTestOnly"]["targets"]["linux/amd64"]["facts"]["references"]
        self.assertTrue(any(reference["test"] for reference in test_references))

    def test_real_non_main_package_uses_application_entrypoints(self):
        package = "github.com/joshyorko/rcc/scripts/testdata/deadcode/library"

        code, report = self.run_fixture(
            "./scripts/testdata/deadcode/library",
            "linux/amd64",
        )

        self.assertEqual(0, code)
        self.assertEqual("complete", report["targets"][0]["status"])
        by_name = {symbol["name"]: symbol for symbol in report["symbols"]}
        self.assertEqual("test_only", by_name["Live"]["targets"]["linux/amd64"]["state"])
        self.assertEqual("unreachable_both", by_name["Unused"]["targets"]["linux/amd64"]["state"])
        linux = report["targets"][0]
        self.assertEqual([package], linux["package_scope"]["packages"])
        self.assertIn("./cmd/rcc", linux["analyzer_patterns"])

    def test_real_platform_fixture_distinguishes_absent_from_reachable(self):
        package = "github.com/joshyorko/rcc/scripts/testdata/deadcode/platform"

        code, report = self.run_fixture(
            "./scripts/testdata/deadcode/platform",
            "linux/amd64",
            "windows/amd64",
            symbol=f"{package}:PlatformOnly",
        )

        self.assertEqual(0, code)
        symbol = report["symbols"][0]
        self.assertEqual("absent", symbol["targets"]["linux/amd64"]["state"])
        self.assertEqual("reachable_both", symbol["targets"]["windows/amd64"]["state"])
        self.assertIn("platform_windows.go", symbol["targets"]["windows/amd64"]["facts"]["position"]["file"])
        self.assertFalse(symbol["aggregate"]["removal_evidence_complete"])


if __name__ == "__main__":
    unittest.main()
