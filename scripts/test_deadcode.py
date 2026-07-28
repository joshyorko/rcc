import importlib.util
import io
import json
import subprocess
import unittest
from contextlib import redirect_stderr
from pathlib import Path
from unittest.mock import patch


SCRIPT = Path(__file__).with_name("deadcode.py")
SPEC = importlib.util.spec_from_file_location("rcc_deadcode", SCRIPT)
deadcode = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(deadcode)


ANALYZER_OUTPUT = json.dumps(
    [
        {
            "Name": "wizard",
            "Path": "github.com/joshyorko/rcc/wizard",
            "Funcs": [
                {
                    "Name": "memberValidation",
                    "Position": {
                        "File": "/repo/wizard/common.go",
                        "Line": 27,
                        "Col": 6,
                    },
                    "Generated": False,
                    "Marker": False,
                }
            ],
        },
        {
            "Name": "pretty",
            "Path": "github.com/joshyorko/rcc/pretty",
            "Funcs": [
                {
                    "Name": "csif",
                    "Position": {
                        "File": "pretty/internal.go",
                        "Line": 9,
                        "Col": 6,
                    },
                    "Generated": False,
                    "Marker": False,
                }
            ],
        },
    ]
)


class FakeRunner:
    def __init__(self, analyzer=None, env=None):
        self.analyzer = analyzer or subprocess.CompletedProcess(
            args=[], returncode=0, stdout=ANALYZER_OUTPUT, stderr=""
        )
        self.env = env or subprocess.CompletedProcess(
            args=[],
            returncode=0,
            stdout=(
                '{"GOOS":"linux","GOARCH":"amd64",'
                '"GOHOSTOS":"linux","GOHOSTARCH":"amd64"}'
            ),
            stderr="",
        )
        self.calls = []

    def __call__(self, command, **details):
        self.calls.append((command, details))
        if command[:2] == ["go", "env"]:
            return self.env
        return self.analyzer


class DeadcodeTests(unittest.TestCase):
    def test_parse_findings_normalizes_paths_and_sorts_results(self):
        findings = deadcode.parse_findings(ANALYZER_OUTPUT, Path("/repo"))

        self.assertEqual(
            [
                (
                    "github.com/joshyorko/rcc/pretty",
                    "pretty/internal.go",
                    9,
                    6,
                    "csif",
                ),
                (
                    "github.com/joshyorko/rcc/wizard",
                    "wizard/common.go",
                    27,
                    6,
                    "memberValidation",
                ),
            ],
            [
                (
                    item.package,
                    item.file,
                    item.line,
                    item.column,
                    item.name,
                )
                for item in findings
            ],
        )

    def test_detailed_report_is_deterministic_and_grouped_by_package(self):
        findings = deadcode.parse_findings(ANALYZER_OUTPUT, Path("/repo"))
        config = deadcode.BuildConfig(goos="linux", goarch="amd64")

        report = deadcode.format_report(findings, config, summary=False)

        self.assertEqual(
            """RCC DEAD CODE REPORT
Analyzer: golang.org/x/tools/cmd/deadcode v0.48.0
Target: linux/amd64 (CGO_ENABLED=0)
Scope: ./... (tests included)
Result: 2 unreachable functions across 2 packages

github.com/joshyorko/rcc/pretty
  pretty/internal.go:9:6  csif

github.com/joshyorko/rcc/wizard
  wizard/common.go:27:6  memberValidation

Advisory: Review findings before deletion; results are specific to this target.
""",
            report,
        )
        self.assertEqual(report, deadcode.format_report(findings, config, summary=False))

    def test_summary_report_omits_individual_findings(self):
        findings = deadcode.parse_findings(ANALYZER_OUTPUT, Path("/repo"))
        config = deadcode.BuildConfig(goos="linux", goarch="amd64")

        report = deadcode.format_report(findings, config, summary=True)

        self.assertIn("Result: 2 unreachable functions across 2 packages", report)
        self.assertNotIn("memberValidation", report)
        self.assertNotIn("pretty/internal.go", report)

    def test_zero_findings_has_an_explicit_result(self):
        config = deadcode.BuildConfig(goos="linux", goarch="amd64")

        report = deadcode.format_report([], config, summary=False)

        self.assertIn("Result: no unreachable functions found", report)

    def test_cli_uses_repo_root_build_configuration_and_disables_telemetry(self):
        runner = FakeRunner()
        stdout = io.StringIO()
        stderr = io.StringIO()

        code = deadcode.run_cli(
            [],
            runner=runner,
            stdout=stdout,
            stderr=stderr,
            repo_root=Path("/repo"),
        )

        self.assertEqual(0, code)
        self.assertEqual("", stderr.getvalue())
        self.assertEqual(
            ["go", "env", "-json", "GOHOSTOS", "GOHOSTARCH"],
            runner.calls[0][0],
        )
        self.assertEqual(
            ["go", "tool", "deadcode", "-json", "-test", "./..."],
            runner.calls[1][0],
        )
        self.assertEqual(Path("/repo"), runner.calls[1][1]["cwd"])
        self.assertEqual("0", runner.calls[1][1]["env"]["CGO_ENABLED"])
        self.assertEqual("2", runner.calls[1][1]["env"]["GO_TELEMETRY_CHILD"])
        self.assertIn("Target: linux/amd64", stdout.getvalue())

    def test_cli_ignores_inherited_cross_target_overrides(self):
        runner = FakeRunner()
        stdout = io.StringIO()

        with patch.dict(
            "os.environ",
            {"GOOS": "darwin", "GOARCH": "arm64"},
            clear=False,
        ):
            code = deadcode.run_cli(
                [],
                runner=runner,
                stdout=stdout,
                stderr=io.StringIO(),
                repo_root=Path("/repo"),
            )

        self.assertEqual(0, code)
        self.assertEqual("linux", runner.calls[1][1]["env"]["GOOS"])
        self.assertEqual("amd64", runner.calls[1][1]["env"]["GOARCH"])
        self.assertIn("Target: linux/amd64", stdout.getvalue())

    def test_analyzer_failure_is_an_analysis_error(self):
        runner = FakeRunner(
            analyzer=subprocess.CompletedProcess(
                args=[], returncode=1, stdout="", stderr="package loading failed"
            )
        )
        stderr = io.StringIO()

        code = deadcode.run_cli(
            [],
            runner=runner,
            stdout=io.StringIO(),
            stderr=stderr,
            repo_root=Path("/repo"),
        )

        self.assertEqual(1, code)
        self.assertIn("package loading failed", stderr.getvalue())

    def test_malformed_analyzer_json_is_an_analysis_error(self):
        runner = FakeRunner(
            analyzer=subprocess.CompletedProcess(
                args=[], returncode=0, stdout="{", stderr=""
            )
        )
        stderr = io.StringIO()

        code = deadcode.run_cli(
            [],
            runner=runner,
            stdout=io.StringIO(),
            stderr=stderr,
            repo_root=Path("/repo"),
        )

        self.assertEqual(1, code)
        self.assertIn("invalid JSON", stderr.getvalue())

    def test_missing_go_has_actionable_error(self):
        def missing_go(command, **details):
            raise FileNotFoundError("go")

        stderr = io.StringIO()

        code = deadcode.run_cli(
            [],
            runner=missing_go,
            stdout=io.StringIO(),
            stderr=stderr,
            repo_root=Path("/repo"),
        )

        self.assertEqual(1, code)
        self.assertIn("Go toolchain was not found", stderr.getvalue())
        self.assertIn("developer/toolkit.yaml", stderr.getvalue())

    def test_removed_heuristic_flags_are_usage_errors(self):
        with redirect_stderr(io.StringIO()):
            with self.assertRaises(SystemExit) as raised:
                deadcode.create_parser().parse_args(["--max-refs", "1"])

        self.assertEqual(2, raised.exception.code)


if __name__ == "__main__":
    unittest.main()
