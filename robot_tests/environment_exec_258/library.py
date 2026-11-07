import json
import os
import shutil
import subprocess
import tempfile
import time
import zipfile
from pathlib import Path

from robot.api.deco import library
from robot.libraries.BuiltIn import BuiltIn


@library(scope="SUITE")
class EnvironmentExec258Library:
    def __init__(self):
        self.root = None
        self.provider = None
        self.provider_url = None
        self.binary = None
        self.artifact = None
        self.trust_root = None
        self.consumer_home = None
        self.host_bin = None
        self.calls = 0
        self.materialization_path = ""

    def prepare_environment_exec_258(self, binary):
        if os.name == "nt":
            raise AssertionError("this CLI acceptance fixture uses the native Linux shell")
        scratch = os.environ.get("RCC_ISSUE258_TMPDIR") or tempfile.gettempdir()
        self.root = Path(tempfile.mkdtemp(prefix="rcc-environment-exec-258-", dir=scratch))
        self.binary = str(Path(binary).resolve())
        self.producer_home = self.root / "producer-home"
        self.consumer_home = self.root / "consumer-home"
        self.host_bin = self.root / "host-bin"
        self.fixture = self.root / "fixture"
        self.provider_stdout = self.root / "provider.stdout"
        self.provider_stderr = self.root / "provider.stderr"
        for directory in (self.producer_home, self.consumer_home, self.host_bin, self.fixture):
            directory.mkdir(parents=True, exist_ok=True)

        package = self.fixture / "probe258"
        package.mkdir()
        (self.fixture / "robot.yaml").write_text(
            "condaConfigFile: conda.yaml\ntasks:\n  proof:\n    command:\n      - python\n      - -c\n      - print('fixture-ready')\n",
            encoding="utf-8",
        )
        (self.fixture / "conda.yaml").write_text(
            "channels:\n  - conda-forge\ndependencies:\n  - python=3.10.15\n  - pip\n  - pip:\n      - " + str(package) + "\n",
            encoding="utf-8",
        )
        (package / "setup.py").write_text(
            "from setuptools import setup\n"
            "setup(name='rcc-issue258-probe', version='0.0.3', "
            "scripts=['rcc-issue258-probe', 'rcc-issue258-unique'])\n",
            encoding="utf-8",
        )
        for script_name in ("rcc-issue258-probe", "rcc-issue258-unique"):
            (package / script_name).write_text(
                "#!/bin/sh\nprintf 'artifact-stdout\\n'\nprintf 'artifact-stderr\\n' >&2\n",
                encoding="utf-8",
            )
            (package / script_name).chmod(0o755)
        host_decoy = self.host_bin / "rcc-issue258-probe"
        host_decoy.write_text("#!/bin/sh\nprintf 'host-decoy\\n'\n", encoding="utf-8")
        host_decoy.chmod(0o755)
        self.host_decoy = str(host_decoy)
        self.trust_root = str(self.producer_home / "artifacts" / "v1" / "trust")

        archive = os.environ.get("RCC_258_ARCHIVE")
        if archive:
            with zipfile.ZipFile(archive) as exported:
                self.artifact = json.loads(exported.read("rcc-environment/manifest.json"))["artifactDigest"]
            self.provider_url = ""
            acquired = self._run(
                [self.binary, "env", "acquire", "--archive", str(Path(archive).resolve()), "--permissive-local", "--json"],
                self.consumer_home, offline=True, path=str(self.host_bin) + ":/usr/bin:/bin",
            )
            self._expect_success(acquired, "offline native archive acquisition")
            BuiltIn().set_suite_variable("${HOST_DECOY}", self.host_decoy)
            BuiltIn().set_suite_variable("${RCC_258_ARTIFACT}", self.artifact)
            return

        self.provider = subprocess.Popen(
            [self.binary, "cache", "serve", "--json"],
            env=self._environment(self.producer_home, offline=False, path=str(self.host_bin) + ":/usr/bin:/bin"),
            stdout=self.provider_stdout.open("w", encoding="utf-8"),
            stderr=self.provider_stderr.open("w", encoding="utf-8"),
            text=True,
        )
        provider_output = self._wait_for_json(self.provider_stdout, self.provider)
        self.provider_url = provider_output["url"]
        published = self._run(
            [self.binary, "env", "publish", "--robot", str(self.fixture / "robot.yaml"),
             "--provider", self.provider_url, "--json"],
            self.producer_home,
            offline=False,
            path=str(self.host_bin) + ":/usr/bin:/bin",
        )
        self._expect_success(published, "publish fixture artifact")
        self.artifact = json.loads(published["stdout"])["artifactDigest"]
        BuiltIn().set_suite_variable("${HOST_DECOY}", self.host_decoy)
        BuiltIn().set_suite_variable("${RCC_258_ARTIFACT}", self.artifact)

    def execute_258_inherited(self, *command):
        self.calls += 1
        receipt = self.root / f"receipt-{self.calls}.json"
        result = self._run(
            [self.binary, "env", "exec", "--artifact", self.artifact,
             "--provider", self.provider_url, "--trust-carrier", self.trust_root,
             "--trust-carrier-type", "filesystem", "--permissive-local",
             "--inherit-streams", "--receipt-file", str(receipt), "--", *command],
            self.consumer_home,
            offline=True,
            path=str(self.host_bin) + ":/usr/bin:/bin",
        )
        return self._result_with_receipt(result, receipt)

    def execute_258_json(self, *command):
        self.calls += 1
        result = self._run(
            [self.binary, "env", "exec", "--artifact", self.artifact,
             "--provider", self.provider_url, "--trust-carrier", self.trust_root,
             "--trust-carrier-type", "filesystem", "--permissive-local",
             "--json", "--", *command],
            self.consumer_home,
            offline=True,
            path=str(self.host_bin) + ":/usr/bin:/bin",
        )
        parsed = None
        if result["stdout"].strip():
            parsed = json.loads(result["stdout"])
        result["receipt"] = self._receipt_with_lease_state(parsed, self.consumer_home)
        return result

    def create_258_escaped_probe(self):
        if not self.materialization_path:
            raise AssertionError("the artifact has not been materialized")
        escaped = Path(self.materialization_path).parent / "rcc258-escaped-probe"
        escaped.write_text("#!/bin/sh\nprintf 'escaped-artifact\\n'\n", encoding="utf-8")
        escaped.chmod(0o755)

    def clean_environment_exec_258(self):
        export_error = None
        if os.environ.get("RCC_258_KEEP_ARCHIVE") == "1" and self.artifact and self.provider and self.provider.poll() is None:
            self.retained_archive = self.root.parent / f"{self.root.name}.rcca"
            exported = self._run(
                [self.binary, "env", "export", "--artifact", self.artifact,
                 "--provider", self.provider_url, "--output", str(self.retained_archive)],
                self.producer_home,
                offline=False,
                path=str(self.host_bin) + ":/usr/bin:/bin",
            )
            if exported["rc"] != 0:
                export_error = (
                    f"export retained .rcca failed (exit {exported['rc']})\n"
                    f"stdout={exported['stdout']}\nstderr={exported['stderr']}"
                )
            else:
                BuiltIn().log_to_console(
                    f"RCC issue #258 retained artifact: {self.retained_archive} "
                    f"({self.retained_archive.stat().st_size} bytes)"
                )
        if self.provider is not None:
            self.provider.terminate()
            try:
                self.provider.wait(timeout=5)
            except subprocess.TimeoutExpired:
                self.provider.kill()
                self.provider.wait(timeout=5)
        if self.root is not None:
            shutil.rmtree(self.root)
        if export_error:
            raise AssertionError(export_error)

    def _result_with_receipt(self, result, receipt_path):
        receipt = json.loads(receipt_path.read_text(encoding="utf-8")) if receipt_path.exists() else None
        result["receipt"] = self._receipt_with_lease_state(receipt, self.consumer_home)
        return result

    def _receipt_with_lease_state(self, receipt, home):
        if receipt is None:
            return {"status": "missing", "leaseReleased": False}
        self.materialization_path = receipt.get("path", "")
        lease_id = receipt.get("leaseId", "")
        lease_path = (
            Path(home) / "artifacts" / "v1" / "materializations"
            / self.artifact.split(":", 1)[1] / "leases" / (lease_id + ".json")
        ) if lease_id else None
        leases = list((Path(home) / "artifacts" / "v1" / "materializations"
                       / self.artifact.split(":", 1)[1] / "leases").glob("*.json"))
        receipt["leaseReleased"] = (lease_path is None or not lease_path.exists()) and not leases
        return receipt

    def _environment(self, home, offline, path):
        allowed = (
            "HOME", "USER", "LOGNAME", "LANG", "LC_ALL", "TMPDIR", "SSL_CERT_FILE",
            "SSL_CERT_DIR", "HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "NO_PROXY",
            "http_proxy", "https_proxy", "all_proxy", "no_proxy",
        )
        environment = {key: os.environ[key] for key in allowed if key in os.environ}
        environment.update({
            "PATH": path,
            "ROBOCORP_HOME": str(home),
            "RCC_HOLOTREE_MODE": "private",
            "PATHEXT": ".COM;.EXE;.BAT;.CMD",
        })
        if offline:
            environment.update({
                "CONDA_OFFLINE": "true", "MAMBA_OFFLINE": "true",
                "PIP_NO_INDEX": "1", "UV_NO_INDEX": "1", "RCC_NO_BUILD": "1",
                "HTTP_PROXY": "http://127.0.0.1:1", "HTTPS_PROXY": "http://127.0.0.1:1",
                "ALL_PROXY": "http://127.0.0.1:1", "http_proxy": "http://127.0.0.1:1",
                "https_proxy": "http://127.0.0.1:1", "all_proxy": "http://127.0.0.1:1",
                "NO_PROXY": "127.0.0.1,localhost", "no_proxy": "127.0.0.1,localhost",
            })
        return environment

    def _run(self, command, home, offline, path):
        completed = subprocess.run(
            command,
            env=self._environment(home, offline, path),
            capture_output=True,
            text=True,
            timeout=600,
            check=False,
        )
        return {"rc": completed.returncode, "stdout": completed.stdout, "stderr": completed.stderr,
                "stdoutText": completed.stdout.strip(), "stderrText": completed.stderr.strip(),
                "command": command}

    def _expect_success(self, result, description):
        if result["rc"] != 0:
            raise AssertionError(
                f"{description} failed (exit {result['rc']})\n"
                f"command={result['command']}\nstdout={result['stdout']}\nstderr={result['stderr']}"
            )

    def _wait_for_json(self, path, process):
        deadline = time.monotonic() + 30
        while time.monotonic() < deadline:
            if process.poll() is not None:
                raise AssertionError(f"provider exited {process.returncode}: {self.provider_stderr.read_text()}")
            try:
                content = path.read_text(encoding="utf-8")
                if content:
                    return json.loads(content)
            except (FileNotFoundError, json.JSONDecodeError):
                pass
            time.sleep(0.05)
        raise AssertionError(f"provider did not write startup JSON: {self.provider_stderr.read_text()}")


_library = EnvironmentExec258Library()


def prepare_environment_exec_258(binary):
    return _library.prepare_environment_exec_258(binary)


def execute_258_inherited(*command):
    return _library.execute_258_inherited(*command)


def execute_258_json(*command):
    return _library.execute_258_json(*command)


def create_258_escaped_probe():
    return _library.create_258_escaped_probe()


def clean_environment_exec_258():
    return _library.clean_environment_exec_258()
