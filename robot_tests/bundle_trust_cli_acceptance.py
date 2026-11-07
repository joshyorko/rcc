#!/usr/bin/env python3
"""Run real run-from-bundle trust cases from a JSON fixture manifest.

The fixture producer must supply source+artifact bundles and a task that writes
the configured marker and escaped-artifact paths. This runner keeps each RCC
home private and retains every command result for review.
"""

import argparse
import json
import os
import threading
import subprocess
import tempfile
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from urllib.parse import unquote, urlparse

REQUIRED_CASES = {
    "signed-allow", "unsigned-strict-deny", "revoked-deny", "tampered-deny",
    "unknown-signer-deny", "missing-evidence-deny", "invalid-evidence-deny",
    "permissive-legacy-allow", "lease-refresh-revocation-deny",
}


def load_cases(path):
    value = json.loads(Path(path).read_text(encoding="utf-8"))
    if not isinstance(value, list) or not value:
        raise ValueError("fixture manifest must be a non-empty JSON array")
    for case in value:
        required = {"name", "bundle", "task", "expect", "marker", "escaped_artifact"}
        if not isinstance(case, dict) or not required.issubset(case):
            raise ValueError(f"fixture case must include {sorted(required)}")
        if case["expect"] not in {"allow", "deny"}:
            raise ValueError(f"invalid expectation for {case['name']}")
        if not isinstance(case.get("trust_args", []), list):
            raise ValueError(f"trust_args must be an array for {case['name']}")
        if not Path(case["escaped_artifact"]).is_absolute():
            raise ValueError(f"escaped_artifact must be absolute for {case['name']}")
    names = [case["name"] for case in value]
    if len(set(names)) != len(names):
        raise ValueError("fixture case names must be unique")
    missing = REQUIRED_CASES.difference(names)
    if missing:
        raise ValueError(f"fixture manifest is missing required cases: {sorted(missing)}")
    expectations = {case["name"]: case["expect"] for case in value}
    if any(expectations[name] != "deny" for name in REQUIRED_CASES if name.endswith("-deny")):
        raise ValueError("all strict negative and lease-refresh cases must expect denial")
    if expectations["signed-allow"] != "allow" or expectations["permissive-legacy-allow"] != "allow":
        raise ValueError("signed and explicit permissive migration cases must expect allow")
    refresh = next(case for case in value if case["name"] == "lease-refresh-revocation-deny")
    if not refresh.get("carrier_refresh"):
        raise ValueError("lease-refresh-revocation-deny requires carrier_refresh before/after directories")
    return value


def process_environment(home):
    allowed = {
        "PATH", "HOME", "USER", "LOGNAME", "LANG", "LC_ALL", "LC_CTYPE",
        "TMPDIR", "TEMP", "TMP", "SYSTEMROOT", "WINDIR", "PATHEXT",
        "USERPROFILE", "HOMEDRIVE", "HOMEPATH", "USERNAME", "COMSPEC",
        "SSL_CERT_FILE", "SSL_CERT_DIR",
    }
    environment = {key: os.environ[key] for key in allowed if key in os.environ}
    environment["ROBOCORP_HOME"] = str(home)
    environment["RCC_HOLOTREE_MODE"] = "private"
    environment.update({
        "CONDA_OFFLINE": "true", "MAMBA_OFFLINE": "true",
        "PIP_NO_INDEX": "1", "UV_NO_INDEX": "1",
    })
    return environment


def start_refresh_carrier(configuration):
    before = Path(configuration["before"]).resolve()
    after = Path(configuration["after"]).resolve()
    if not before.is_dir() or not after.is_dir():
        raise ValueError("carrier refresh before/after must be directories")

    class Handler(BaseHTTPRequestHandler):
        switched = False
        current = before

        def do_GET(self):
            relative = Path(unquote(urlparse(self.path).path).lstrip("/"))
            target = (self.current / relative).resolve()
            if target != self.current and self.current not in target.parents:
                self.send_error(400)
                return
            try:
                content = target.read_bytes()
            except OSError:
                self.send_error(404)
                return
            self.send_response(200)
            self.send_header("Content-Length", str(len(content)))
            self.end_headers()
            self.wfile.write(content)
            if not self.switched and relative.as_posix().endswith("/revocations.json"):
                self.current = after
                self.switched = True

        def log_message(self, _format, *_arguments):
            return

    server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()
    return server, thread, f"http://127.0.0.1:{server.server_port}"


def run_case(binary, case, output_root):
    bundle = Path(case["bundle"]).resolve()
    if not bundle.is_file():
        raise ValueError(f"bundle does not exist: {bundle}")
    name = case["name"]
    if not name or Path(name).name != name:
        raise ValueError(f"invalid case name: {name!r}")
    case_output = output_root / name
    case_output.mkdir(parents=True, exist_ok=False)
    with tempfile.TemporaryDirectory(prefix=f"rcc-bundle-trust-{name}-") as temporary:
        root = Path(temporary)
        cwd = root / "work"
        home = root / "home"
        cwd.mkdir()
        home.mkdir()
        marker_relative = Path(case["marker"])
        if marker_relative.is_absolute():
            raise ValueError(f"marker must be relative to the task working directory: {case['name']}")
        marker = (cwd / marker_relative).resolve()
        if marker != cwd and cwd not in marker.parents:
            raise ValueError(f"marker escapes the task working directory: {case['name']}")
        escaped_artifact = Path(case["escaped_artifact"]).resolve()
        if escaped_artifact == root or root in escaped_artifact.parents:
            raise ValueError(f"escaped_artifact must be outside the private case root: {escaped_artifact}")
        marker.parent.mkdir(parents=True, exist_ok=True)
        if marker.exists() or escaped_artifact.exists():
            raise ValueError(f"case outputs already exist: {marker}, {escaped_artifact}")
        refresh_server = None
        command_trust_args = list(case.get("trust_args", []))
        if case.get("carrier_refresh"):
            if "--trust-carrier" in command_trust_args or "--trust-carrier-type" in command_trust_args:
                raise ValueError(f"{name}: carrier_refresh owns --trust-carrier flags")
            refresh_server, refresh_thread, carrier_url = start_refresh_carrier(case["carrier_refresh"])
            command_trust_args.extend(["--trust-carrier", carrier_url, "--trust-carrier-type", "http"])
        command = [
            str(binary), "robot", "run-from-bundle", str(bundle), "--task", str(case["task"]),
            *command_trust_args,
        ]
        try:
            result = subprocess.run(
                command, cwd=cwd, env=process_environment(home), text=True,
                capture_output=True, timeout=case.get("timeout_seconds", 600), check=False,
            )
        finally:
            if refresh_server is not None:
                refresh_server.shutdown()
                refresh_server.server_close()
                refresh_thread.join(timeout=5)
        (case_output / "command.json").write_text(json.dumps(command, indent=2) + "\n", encoding="utf-8")
        (case_output / "stdout.txt").write_text(result.stdout, encoding="utf-8")
        (case_output / "stderr.txt").write_text(result.stderr, encoding="utf-8")
        (case_output / "exit.txt").write_text(f"{result.returncode}\n", encoding="utf-8")
        if case["expect"] == "deny":
            if result.returncode == 0:
                raise AssertionError(f"{name}: denied case exited successfully")
            if marker.exists():
                raise AssertionError(f"{name}: denied task executed and created marker {marker}")
            if escaped_artifact.exists():
                raise AssertionError(f"{name}: denied task emitted escaped artifact {escaped_artifact}")
        else:
            if result.returncode != 0:
                raise AssertionError(f"{name}: allowed case exited {result.returncode}; see {case_output}")
            if not marker.is_file():
                raise AssertionError(f"{name}: allowed task did not create marker {marker}")
        return {"name": name, "exit": result.returncode, "expected": case["expect"], "evidence": str(case_output)}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--rcc", required=True, type=Path)
    parser.add_argument("--cases", required=True, type=Path)
    parser.add_argument("--output", required=True, type=Path)
    arguments = parser.parse_args()
    binary = arguments.rcc.resolve()
    if not binary.is_file() or not os.access(binary, os.X_OK):
        parser.error(f"--rcc is not an executable file: {binary}")
    output = arguments.output.resolve()
    output.mkdir(parents=True, exist_ok=False)
    results = [run_case(binary, case, output) for case in load_cases(arguments.cases)]
    print(json.dumps(results, indent=2))


if __name__ == "__main__":
    main()
