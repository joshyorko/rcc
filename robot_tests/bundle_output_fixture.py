import argparse
import json
import os
import signal
import subprocess
import time
from pathlib import Path


def process_environment(home, offline=False):
    allowed = (
        "PATH", "HOME", "USER", "LOGNAME", "LANG", "LC_ALL", "TMPDIR", "TEMP", "TMP",
        "SYSTEMROOT", "WINDIR", "PATHEXT", "USERPROFILE", "SSL_CERT_FILE", "SSL_CERT_DIR",
        "HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "NO_PROXY", "http_proxy", "https_proxy",
        "all_proxy", "no_proxy",
    )
    environment = {key: os.environ[key] for key in allowed if key in os.environ}
    environment.update({"ROBOCORP_HOME": str(home), "RCC_HOLOTREE_MODE": "private"})
    if offline:
        environment.update({
            "CONDA_OFFLINE": "true", "MAMBA_OFFLINE": "true", "PIP_NO_INDEX": "1", "UV_NO_INDEX": "1",
            "RCC_NO_BUILD": "1", "HTTP_PROXY": "http://127.0.0.1:1", "HTTPS_PROXY": "http://127.0.0.1:1",
            "ALL_PROXY": "http://127.0.0.1:1", "NO_PROXY": "127.0.0.1,localhost",
            "http_proxy": "http://127.0.0.1:1", "https_proxy": "http://127.0.0.1:1",
            "all_proxy": "http://127.0.0.1:1", "no_proxy": "127.0.0.1,localhost",
        })
    return environment


def run(binary, name, arguments, home, cwd, root, offline=False, expected=None):
    result = subprocess.run(
        [str(binary), *map(str, arguments)], cwd=cwd, env=process_environment(home, offline),
        capture_output=True, text=True, timeout=1800, check=False,
    )
    evidence = root / "commands" / name
    evidence.mkdir(parents=True, exist_ok=True)
    (evidence / "argv.json").write_text(json.dumps({
        "argv": [str(binary), *map(str, arguments)], "cwd": str(cwd), "privateHome": str(home), "offline": offline,
    }, indent=2) + "\n", encoding="utf-8")
    (evidence / "stdout.txt").write_text(result.stdout, encoding="utf-8")
    (evidence / "stderr.txt").write_text(result.stderr, encoding="utf-8")
    (evidence / "exit.txt").write_text(f"{result.returncode}\n", encoding="ascii")
    if expected is not None and result.returncode != expected:
        raise AssertionError(f"{name} exit={result.returncode}, expected={expected}; see {evidence}")
    return result


def make_fixture(root):
    source = root / "project"
    source.mkdir(parents=True)
    cancel_marker = root / "cancel-ready.txt"
    (source / "conda.yaml").write_text("channels:\n  - conda-forge\ndependencies:\n  - python=3.11\n", encoding="utf-8")
    (source / "robot.yaml").write_text(
        "tasks:\n"
        "  success:\n    command: [python, task.py, success]\n"
        "  failure:\n    command: [python, task.py, failure]\n"
        "  cancel:\n    command: [python, task.py, cancel, " + json.dumps(str(cancel_marker)) + "]\n"
        "condaConfigFile: conda.yaml\nartifactsDir: output\n",
        encoding="utf-8",
    )
    (source / "task.py").write_text(
        "import os, sys, time\n"
        "from pathlib import Path\n"
        "mode = sys.argv[1]\n"
        "if mode == 'denied':\n"
        "    Path(sys.argv[2]).write_text('executed\\n', encoding='utf-8')\n"
        "    raise SystemExit(0)\n"
        "Path('output').mkdir(exist_ok=True)\n"
        "with open('output/result.txt', 'w', encoding='utf-8') as result:\n"
        "    result.write(mode + '-result\\n')\n"
        "    result.flush()\n"
        "print('child-' + mode + '-stdout', flush=True)\n"
        "print('child-' + mode + '-stderr', file=sys.stderr, flush=True)\n"
        "if mode == 'failure': raise SystemExit(23)\n"
        "if mode == 'cancel':\n"
        "    with open(sys.argv[2], 'w', encoding='utf-8') as ready: ready.write(str(os.getpid()) + '\\n'); ready.flush()\n"
        "    print('cancel-ready', flush=True)\n"
        "    time.sleep(120)\n",
        encoding="utf-8",
    )
    return source


def create_cancel_run(binary, bundle, name, home, cwd, root, extra_flags):
    cwd.mkdir(parents=True)
    ready = root / "cancel-ready.txt"
    ready.unlink(missing_ok=True)
    argv = [str(binary), "robot", "run-from-bundle", str(bundle), "--task", "cancel", "--controller", "citests", *extra_flags]
    process = subprocess.Popen(
        argv,
        cwd=cwd, env=process_environment(home, offline=True), stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True,
    )
    deadline = time.monotonic() + 120
    while time.monotonic() < deadline and not ready.exists() and process.poll() is None:
        time.sleep(0.05)
    if not ready.is_file():
        if process.poll() is None:
            process.kill()
        stdout, stderr = process.communicate(timeout=40)
        evidence = root / "commands" / name
        evidence.mkdir(parents=True, exist_ok=True)
        (evidence / "argv.json").write_text(json.dumps({
            "argv": argv, "cwd": str(cwd), "privateHome": str(home), "offline": True,
        }, indent=2) + "\n", encoding="utf-8")
        (evidence / "stdout.txt").write_text(stdout, encoding="utf-8")
        (evidence / "stderr.txt").write_text(stderr, encoding="utf-8")
        (evidence / "exit.txt").write_text(f"{process.returncode}\n", encoding="ascii")
        raise AssertionError(f"{name} did not reach cancel-ready: {process.returncode}; see {evidence}")
    os.kill(int(ready.read_text(encoding="ascii")), signal.SIGTERM)
    stdout, stderr = process.communicate(timeout=40)
    evidence = root / "commands" / name
    evidence.mkdir(parents=True, exist_ok=True)
    (evidence / "argv.json").write_text(json.dumps({
        "argv": argv, "cwd": str(cwd), "privateHome": str(home), "offline": True, "cancelSignal": "SIGTERM to child pid",
    }, indent=2) + "\n", encoding="utf-8")
    (evidence / "stdout.txt").write_text(stdout, encoding="utf-8")
    (evidence / "stderr.txt").write_text(stderr, encoding="utf-8")
    (evidence / "exit.txt").write_text(f"{process.returncode}\n", encoding="ascii")
    if process.returncode != 10:
        raise AssertionError(f"{name} outer exit={process.returncode}, expected legacy task failure 10 after SIGTERM")
    return stdout, stderr, process.returncode


def verify_run(root, mode, outcome, result, stdout, stderr, exit_code):
    cwd = root / "runs" / f"{mode}-{outcome}"
    stdout_log = cwd / "output" / "stdout.log"
    stderr_log = cwd / "output" / "stderr.log"
    artifact = cwd / "output" / "result.txt"
    if not stdout_log.is_file() or not stderr_log.is_file():
        raise AssertionError(f"{mode}-{outcome} did not preserve both task streams")
    if f"child-{outcome}-stdout\n" not in stdout_log.read_text(encoding="utf-8"):
        raise AssertionError(f"{mode}-{outcome} stdout log is incomplete")
    if f"child-{outcome}-stderr\n" not in stderr_log.read_text(encoding="utf-8"):
        raise AssertionError(f"{mode}-{outcome} stderr log is incomplete")
    if not artifact.is_file() or artifact.read_text(encoding="utf-8") != f"{outcome}-result\n":
        raise AssertionError(f"{mode}-{outcome} artifact was not handed back")
    if f"child-{outcome}-stdout\n" not in stdout or f"child-{outcome}-stderr\n" not in stderr:
        raise AssertionError(f"{mode}-{outcome} task streams were not visible to the caller")
    if exit_code != result:
        raise AssertionError(f"{mode}-{outcome} exit={exit_code}, expected={result}")


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--rcc", required=True, type=Path)
    parser.add_argument("--root", required=True, type=Path)
    args = parser.parse_args()
    binary, root = args.rcc.resolve(), args.root.resolve()
    root.mkdir(parents=True, exist_ok=True)
    source = make_fixture(root)
    producer_home = Path(os.environ.get("RCC_BUNDLE_OUTPUT_PRODUCER_HOME", root / "producer-home")).resolve()
    producer_home.mkdir(parents=True, exist_ok=True)
    producer_cwd = root / "producer"
    producer_cwd.mkdir()
    environment = process_environment(producer_home)
    server = subprocess.Popen(
        [str(binary), "cache", "serve", "--json"], cwd=producer_cwd, env=environment,
        stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True,
    )
    try:
        server_line = server.stdout.readline()
        if not server_line:
            raise AssertionError(f"provider failed to start: {server.stderr.read()}")
        server_info = json.loads(server_line)
        provider_url = server_info["url"]
        run(binary, "provider-add", ["provider", "add", "bundle-output", "--type", "http", "--url", provider_url, "--replace"], producer_home, producer_cwd, root, expected=0)
        published = run(binary, "publish", ["env", "publish", "--robot", source / "robot.yaml", "--provider", "bundle-output", "--json"], producer_home, producer_cwd, root, expected=0)
        digest = json.loads(published.stdout)["artifactDigest"]
        archive = root / "fixture.rcca"
        run(binary, "export", ["env", "export", "--artifact", digest, "--provider", "bundle-output", "--output", archive], producer_home, producer_cwd, root, expected=0)
        source_bundle, artifact_bundle = root / "source-only.py", root / "source-artifact.py"
        run(binary, "bundle-source", ["robot", "bundle", "--robot", source / "robot.yaml", "--output", source_bundle], producer_home, producer_cwd, root, expected=0)
        run(binary, "bundle-artifact", ["robot", "bundle", "--robot", source / "robot.yaml", "--artifact-archive", archive, "--output", artifact_bundle], producer_home, producer_cwd, root, expected=0)
        denied_source = root / "denied-project"
        denied_source.mkdir()
        (denied_source / "conda.yaml").write_bytes((source / "conda.yaml").read_bytes())
        (denied_source / "task.py").write_bytes((source / "task.py").read_bytes())
        denied_marker = root / "denied-task-ran.txt"
        (denied_source / "robot.yaml").write_text(
            "tasks:\n  denied:\n    command: [python, task.py, denied, " + json.dumps(str(denied_marker)) + "]\n"
            "condaConfigFile: conda.yaml\nartifactsDir: ../escape\n",
            encoding="utf-8",
        )
        denied_source_bundle = root / "denied-source.py"
        denied_artifact_bundle = root / "denied-artifact.py"
        run(binary, "bundle-denied-source", ["robot", "bundle", "--robot", denied_source / "robot.yaml", "--output", denied_source_bundle], producer_home, producer_cwd, root, expected=0)
        run(binary, "bundle-denied-artifact", ["robot", "bundle", "--robot", denied_source / "robot.yaml", "--artifact-archive", archive, "--output", denied_artifact_bundle], producer_home, producer_cwd, root, expected=0)
        bundle_help = run(binary, "run-from-bundle-help", ["robot", "run-from-bundle", "--help"], producer_home, producer_cwd, root, expected=0)
        artifact_trust_flag = ["--permissive-local"] if "--permissive-local" in bundle_help.stdout else []
    finally:
        server.terminate()
        server.wait(timeout=20)
        (root / "provider-server.stdout.txt").write_text(server.stdout.read(), encoding="utf-8")
        (root / "provider-server.stderr.txt").write_text(server.stderr.read(), encoding="utf-8")

    for mode, bundle in (("source-only", source_bundle), ("source-artifact", artifact_bundle)):
        for outcome, expected in (("success", 0), ("failure", 10)):
            home, cwd = root / "homes" / f"{mode}-{outcome}", root / "runs" / f"{mode}-{outcome}"
            home.mkdir(parents=True)
            cwd.mkdir(parents=True)
            extra_flags = artifact_trust_flag if mode == "source-artifact" else []
            result = run(binary, f"{mode}-{outcome}", ["robot", "run-from-bundle", bundle, "--task", outcome, "--controller", "citests", *extra_flags], home, cwd, root, offline=True, expected=expected)
            verify_run(root, mode, outcome, result.returncode, result.stdout, result.stderr, result.returncode)
            if outcome == "failure" and "exit status 23" not in result.stderr:
                raise AssertionError(f"{mode} failure did not report the child exit 23")

        home, cwd = root / "homes" / f"{mode}-cancel", root / "runs" / f"{mode}-cancel"
        home.mkdir(parents=True)
        extra_flags = artifact_trust_flag if mode == "source-artifact" else []
        stdout, stderr, exit_code = create_cancel_run(binary, bundle, f"{mode}-cancel", home, cwd, root, extra_flags)
        verify_run(root, mode, "cancel", exit_code, stdout, stderr, exit_code)

        denied_bundle = denied_source_bundle if mode == "source-only" else denied_artifact_bundle
        denied_home, denied_cwd = root / "homes" / f"{mode}-denied", root / "runs" / f"{mode}-denied"
        denied_home.mkdir()
        denied_cwd.mkdir(parents=True)
        extra_flags = artifact_trust_flag if mode == "source-artifact" else []
        denied_result = run(binary, f"{mode}-denied", ["robot", "run-from-bundle", denied_bundle, "--task", "denied", "--controller", "citests", *extra_flags], denied_home, denied_cwd, root, offline=True)
        if denied_result.returncode == 0 or "outside the bundle workarea" not in denied_result.stderr:
            raise AssertionError(f"{mode} accepted an artifact path escape")
        if denied_marker.exists() or (denied_cwd.parent / "escape").exists():
            raise AssertionError(f"{mode} executed a task or wrote escaped artifacts for a denied path")

    json_home, json_cwd = root / "homes" / "json-flag", root / "runs" / "json-flag"
    json_home.mkdir()
    json_cwd.mkdir(parents=True)
    json_result = run(binary, "json-flag", ["robot", "run-from-bundle", source_bundle, "--task", "success", "--json", "--controller", "citests"], json_home, json_cwd, root, offline=True)
    if json_result.returncode == 0 or "unknown flag: --json" not in json_result.stderr or (json_cwd / "output" / "result.txt").exists():
        raise AssertionError("run-from-bundle JSON flag contract changed or task executed")

    summary = {
        "binary": str(binary), "binarySha256": __import__("hashlib").sha256(binary.read_bytes()).hexdigest(),
        "artifactDigest": digest, "archiveSha256": __import__("hashlib").sha256(archive.read_bytes()).hexdigest(),
        "platform": {"system": os.uname().sysname, "machine": os.uname().machine},
        "cases": [f"{mode}-{outcome}" for mode in ("source-only", "source-artifact") for outcome in ("success", "failure", "cancel")],
        "failureContract": "both CLI paths preserve legacy outer exit 10 and report child exit 23",
        "cancellationContract": "only closed output files present at child reap are handed back; partial writes are not guaranteed",
        "jsonFlag": "unsupported by robot run-from-bundle",
    }
    (root / "summary.json").write_text(json.dumps(summary, indent=2) + "\n", encoding="utf-8")
    print(json.dumps(summary, sort_keys=True))


if __name__ == "__main__":
    main()
