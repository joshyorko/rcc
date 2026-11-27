"""Qualify mixed native and Python package providers with the native RCC build."""
import hashlib
import json
import os
import platform
import subprocess
import sys
import tempfile
from pathlib import Path
import zipfile

sys.path.insert(0, str(Path(__file__).resolve().parents[2] / "scripts"))
from profile_package_providers import prepare_profile_channel


def sha256(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def main(helper, rcc_binary, cache):
    target = {("Linux", "x86_64"): "linux-64", ("Linux", "aarch64"): "linux-aarch64",
              ("Darwin", "arm64"): "osx-arm64", ("Darwin", "x86_64"): "osx-64",
              ("Windows", "AMD64"): "win-64"}[platform.system(), platform.machine()]
    source = subprocess.check_output(["git", "rev-parse", "HEAD"], text=True).strip()
    for conda, pypi in (("rattler", "pip"), ("rattler", "uv"), ("micromamba", "uv")):
        with tempfile.TemporaryDirectory(prefix="rcc-native-mixed-") as tmp:
            root = Path(tmp)
            home = root / "home"
            wheels = home / "wheels"
            wheels.mkdir(parents=True)
            _, records = prepare_profile_channel(home, cache, target)
            if not any(records[target][key] for key in ("packages", "packages.conda")):
                raise RuntimeError(f"no verified archives for {target}")
            dist = "provider_receipt-1.0.dist-info"
            wheel_path = wheels / "provider_receipt-1.0-py3-none-any.whl"
            with zipfile.ZipFile(wheel_path, "w") as wheel:
                files = {"provider_receipt.py": "value=306\n",
                         dist + "/METADATA": "Metadata-Version: 2.1\nName: provider-receipt\nVersion: 1.0\n",
                         dist + "/WHEEL": "Wheel-Version: 1.0\nGenerator: rcc-test\nRoot-Is-Purelib: true\nTag: py3-none-any\n"}
                files[dist + "/RECORD"] = "".join(name + ",,\n" for name in [*files, dist + "/RECORD"])
                for name, body in files.items():
                    wheel.writestr(name, body)
            manifest = root / "package.yaml"
            packages = "python=3.12.15, pip=25.0.1, zlib=1.3.2"
            manifest.write_text(
                "dependencies: rcc-provider-plan-v1\nrcc:\n  schema-version: 1\n  layers:\n"
                f"    - provider: {conda}\n      channels: [local]\n      packages: [{packages}]\n"
                f"    - provider: {pypi}\n      packages: [provider-receipt==1.0]\n")
            env = dict(os.environ, ROBOCORP_HOME=str(home), PIP_NO_INDEX="1",
                       RCC_ENDPOINT_PYPI=wheels.as_uri()+"/", UV_NO_INDEX="1",
                       PIP_FIND_LINKS=str(wheels), UV_FIND_LINKS=str(wheels))
            proc = subprocess.run([str(rcc_binary), "holotree", "variables", str(manifest), "--json",
                                   "--no-retry-build"], env=env, text=True, capture_output=True, timeout=600)
            if proc.returncode:
                raise RuntimeError(f"{conda}->{pypi} failed: {proc.stdout}\n{proc.stderr}")
            activation = {entry["key"]: entry["value"] for entry in json.loads(proc.stdout)}
            prefix = Path(activation["CONDA_PREFIX"]).resolve()
            python = Path(activation["PYTHON_EXE"]).resolve(strict=True)
            if not prefix.is_relative_to(home / "holotree") or not python.is_relative_to(prefix):
                raise RuntimeError("interpreter escaped the materialized environment")
            probe = """
import json, platform, sys, zlib
from importlib.metadata import version
import provider_receipt
assert provider_receipt.value == 306
print(json.dumps(dict(python=sys.executable, pythonVersion=platform.python_version(),
                      pipVersion=version('pip'), zlibVersion=zlib.ZLIB_RUNTIME_VERSION)))
"""
            check = subprocess.run([str(python), "-I", "-c", probe],
                                   env={**env, **activation}, text=True, capture_output=True, check=True)
            runtime = json.loads(check.stdout)
            assert (runtime["pythonVersion"], runtime["pipVersion"], runtime["zlibVersion"]) == ("3.12.15", "25.0.1", "1.3.2"), runtime
            if "provider-receipt" not in (prefix / "golden-ee.yaml").read_text():
                raise RuntimeError(f"{conda}->{pypi}: wheel absent from environment inventory")
            print(json.dumps({"sourceSHA": source, "platform": target, "conda": conda, "pypi": pypi,
                              "helperSHA256": sha256(helper), "rccSHA256": sha256(rcc_binary),
                              "wheelSHA256": sha256(wheel_path), **runtime,
                              "inventory": True}))


if __name__ == "__main__":
    helper, rcc = [Path(arg).resolve(strict=True) for arg in sys.argv[1:3]]
    if len(sys.argv) == 4:
        main(helper, rcc, Path(sys.argv[3]).resolve(strict=True))
    else:
        with tempfile.TemporaryDirectory(prefix="rcc-seed-") as tmp:
            home = Path(tmp)
            seed = home / "conda.yaml"
            seed.write_text("channels: [conda-forge]\ndependencies: [python=3.12.15, pip=25.0.1, zlib=1.3.2]\n")
            subprocess.run([str(rcc), "holotree", "variables", str(seed), "--json", "--no-retry-build"],
                           check=True, env=dict(os.environ, ROBOCORP_HOME=tmp), stdout=subprocess.DEVNULL)
            main(helper, rcc, home / "pkgs")
