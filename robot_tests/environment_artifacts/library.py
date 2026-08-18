import json
import os
import shutil
import tempfile
import time
import urllib.request
from pathlib import Path


def new_environment_artifact_fixture():
    temporary_root = Path(__file__).resolve().parents[2] / "tmp"
    temporary_root.mkdir(parents=True, exist_ok=True)
    root = Path(tempfile.mkdtemp(prefix="environment-artifacts-", dir=temporary_root))
    values = {
        "root": root,
        "aHome": root / "home-a",
        "bHome": root / "home-b",
        "providerRoot": root / "provider",
        "serverStdout": root / "server.stdout",
        "serverStderr": root / "server.stderr",
        "proofFile": root / "python-proof.json",
    }
    values["aHome"].mkdir()
    values["bHome"].mkdir()
    return {key: str(value.resolve()) for key, value in values.items()}


def remove_environment_artifact_fixture(root):
    path = Path(root).resolve()
    expected_parent = (Path(__file__).resolve().parents[2] / "tmp").resolve()
    if path.parent != expected_parent or not path.name.startswith("environment-artifacts-"):
        raise AssertionError(f"refusing to remove unexpected fixture root {path}")
    shutil.rmtree(path, ignore_errors=True)


def environment_artifact_process_environment(home, offline=False):
    allowed = (
        "PATH",
        "HOME",
        "USER",
        "LOGNAME",
        "LANG",
        "LC_ALL",
        "LC_CTYPE",
        "TMPDIR",
        "TEMP",
        "TMP",
        "SYSTEMROOT",
        "WINDIR",
        "PATHEXT",
        "SSL_CERT_FILE",
        "SSL_CERT_DIR",
    )
    environment = {key: os.environ[key] for key in allowed if key in os.environ}
    environment["ROBOCORP_HOME"] = str(Path(home).resolve())
    for key in ("CONDA_OFFLINE", "MAMBA_OFFLINE", "PIP_NO_INDEX", "UV_NO_INDEX"):
        environment.pop(key, None)
    if offline:
        environment.update(
            {
                "CONDA_OFFLINE": "true",
                "MAMBA_OFFLINE": "true",
                "PIP_NO_INDEX": "1",
                "UV_NO_INDEX": "1",
                "HTTP_PROXY": "http://127.0.0.1:1",
                "HTTPS_PROXY": "http://127.0.0.1:1",
                "ALL_PROXY": "http://127.0.0.1:1",
                "NO_PROXY": "127.0.0.1,localhost",
                "no_proxy": "127.0.0.1,localhost",
                "RCC_NO_BUILD": "1",
            }
        )
    return environment


def wait_for_json_file(filename, timeout=30):
    deadline = time.monotonic() + float(timeout)
    path = Path(filename)
    last_error = None
    while time.monotonic() < deadline:
        try:
            content = path.read_text(encoding="utf-8")
            if content:
                parsed = json.loads(content)
                assert isinstance(parsed, dict)
                return parsed
        except (FileNotFoundError, json.JSONDecodeError) as error:
            last_error = error
        time.sleep(0.05)
    raise AssertionError(f"no JSON startup result in {path}: {last_error}")


def read_json_file(filename):
    parsed = json.loads(Path(filename).read_text(encoding="utf-8"))
    assert isinstance(parsed, dict)
    return parsed


def environment_artifact_home_should_be_empty(directory):
    path = Path(directory)
    assert path.is_dir(), f"missing directory {path}"
    assert not any(path.iterdir()), f"directory is not empty: {path}"


def provider_should_contain_manifest(root, artifact_digest):
    algorithm, hex_digest = artifact_digest.split(":", 1)
    path = Path(root) / "manifests" / algorithm / hex_digest[:2] / hex_digest[2:4] / hex_digest
    assert path.is_file(), f"provider manifest is missing: {path}"


def provider_should_be_unreachable(url):
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
    try:
        opener.open(url + "/v1/capabilities", timeout=1)
    except OSError:
        return
    raise AssertionError(f"provider remains reachable at {url}")


def package_manager_caches_should_be_empty(home):
    home_path = Path(home)
    for name in ("pkgs", "pipcache", "uvcache", "wheels"):
        path = home_path / name
        files = [entry for entry in path.rglob("*") if entry.is_file()] if path.exists() else []
        assert not files, f"package-manager cache {path} contains {files[:5]}"
