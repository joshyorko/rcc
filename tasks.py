import os
import json
import hashlib
import shutil
import shlex
import subprocess
import sys
import tempfile
import zipfile
from pathlib import Path

from invoke import task


def _binary_inventory_targets():
    return [
        ("linux", "amd64", "linux64", ""),
        ("darwin", "amd64", "macos64", ""),
        ("darwin", "arm64", "macosarm64", ""),
        ("windows", "amd64", "windows64", ".exe"),
    ]


def _require_linux(task_name):
    if sys.platform != "linux":
        raise RuntimeError(f"Linux-only task unsupported on {sys.platform}: {task_name}")


def _state_digests(root):
    """Return exact relative-file digests for one self-host state boundary."""
    root = Path(root)
    if not root.exists():
        return {}
    state = {}
    for path in sorted(root.rglob("*")):
        if path.is_file() and not path.is_symlink():
            state[str(path.relative_to(root))] = hashlib.sha256(path.read_bytes()).hexdigest()
    return state


def _legacy_closure_state(home, archive):
    """Hash only the v12 catalog and exact legacy objects named by an archive."""
    with zipfile.ZipFile(archive) as carrier:
        manifest = json.loads(carrier.read("rcc-environment/manifest.json"))
        object_index = json.loads(carrier.read("rcc-environment/object-index.json"))
    hololib = Path(home) / "hololib"
    paths = {
        "catalog": hololib / "catalog" / manifest["catalogs"][0]["legacyName"],
    }
    for entry in object_index["entries"]:
        legacy_id = entry["legacyObjectId"]
        paths[f"object:{legacy_id}"] = hololib / "library" / legacy_id[:2] / legacy_id[2:4] / legacy_id[4:6] / legacy_id
    state = {}
    for label, path in sorted(paths.items()):
        if not path.is_file() or path.is_symlink():
            raise RuntimeError(f"legacy v12 closure member is missing: {path}")
        content = path.read_bytes()
        state[label] = {"path": str(path), "size": len(content), "sha256": hashlib.sha256(content).hexdigest()}
    return state


def _archive_legacy_blueprint(archive):
    with zipfile.ZipFile(archive) as carrier:
        manifest = json.loads(carrier.read("rcc-environment/manifest.json"))
        digest = manifest["legacyBlueprint"]["digest"].split(":", 1)[1]
        return carrier.read(f"rcc-environment/legacy-blueprints/{digest}")


def _install_archive_legacy_closure(home, archive):
    home = Path(home)
    with zipfile.ZipFile(archive) as carrier:
        manifest = json.loads(carrier.read("rcc-environment/manifest.json"))
        object_index = json.loads(carrier.read("rcc-environment/object-index.json"))
        catalog = manifest["catalogs"][0]
        catalog_digest = catalog["digest"].split(":", 1)[1]
        catalog_bytes = carrier.read(f"rcc-environment/catalogs/{catalog_digest}")
        if hashlib.sha256(catalog_bytes).hexdigest() != catalog_digest:
            raise RuntimeError("archive catalog digest mismatch")
        catalog_path = home / "hololib" / "catalog" / catalog["legacyName"]
        catalog_path.parent.mkdir(parents=True, exist_ok=True)
        catalog_path.write_bytes(catalog_bytes)
        for entry in object_index["entries"]:
            stored_digest = entry["storedDigest"].split(":", 1)[1]
            content = carrier.read(f"rcc-environment/objects/{stored_digest}")
            if len(content) != entry["storedSize"] or hashlib.sha256(content).hexdigest() != stored_digest:
                raise RuntimeError(f"archive object verification failed: {stored_digest}")
            legacy_id = entry["legacyObjectId"]
            path = home / "hololib" / "library" / legacy_id[:2] / legacy_id[2:4] / legacy_id[4:6] / legacy_id
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_bytes(content)


def _write_self_host_receipt(root, *, released_binary, candidate_binary, home_a, home_b, commands,
                             binary_metadata=None, evidence_paths=None):
    if not binary_metadata or not evidence_paths:
        raise ValueError("self-host receipt requires binary metadata and evidence paths")
    required_generations = {"released", "generationA", "candidate", "generationB"}
    missing_generations = sorted(required_generations - binary_metadata.keys())
    if missing_generations:
        raise ValueError(f"self-host receipt missing binary metadata: {missing_generations}")
    missing = [str(path) for path in evidence_paths if not Path(path).exists()]
    if missing:
        raise ValueError(f"self-host evidence paths do not exist: {missing}")
    root = Path(root)
    root.mkdir(parents=True, exist_ok=True)
    receipt = root / "self-host-v1.json"
    receipt.write_text(json.dumps({
        "schemaVersion": 1,
        "releasedBinary": str(released_binary),
        "candidateBinary": str(candidate_binary),
        "homes": {"a": str(home_a), "b": str(home_b)},
        "commands": commands,
        "binaries": binary_metadata,
        "evidencePaths": [str(path) for path in evidence_paths],
    }, indent=2, sort_keys=True) + "\n")
    return receipt


def _self_host_command_plan(released, candidate, fixture):
    conda = Path(fixture).with_name("conda.yaml")
    return [
        {"step": "released-build", "argv": [released, "run", "-r", "developer/toolkit.yaml", "--dev", "-t", "selfHostBuild"]},
        {"step": "candidate-probe", "argv": [candidate, "run", "-r", "developer/toolkit.yaml", "--dev", "-t", "selfHostProbe"]},
        {"step": "candidate-build", "argv": [candidate, "run", "-r", "developer/toolkit.yaml", "--dev", "-t", "selfHostBuild"]},
        {"step": "released-probe", "argv": [released, "run", "-r", "developer/toolkit.yaml", "--dev", "-t", "selfHostProbe"]},
        {"step": "released-v12", "argv": [released, "holotree", "variables", str(conda), "--robot", str(fixture), "--json"]},
        {"step": "candidate-v12", "argv": [candidate, "holotree", "variables", str(conda), "--robot", str(fixture), "--json"]},
    ]


def _binary_metadata(path):
    path = Path(path).resolve()
    digest = hashlib.sha256(path.read_bytes()).hexdigest()
    output = subprocess.check_output([str(path), "--version"], text=True, stderr=subprocess.STDOUT).strip()
    return {"path": str(path), "sha256": digest, "version": output}


def _promote_self_host_generation(source, candidate):
    source, candidate = Path(source).resolve(), Path(candidate).resolve()
    if not source.is_file():
        raise RuntimeError(f"generation-A RCC output is missing: {source}")
    candidate.parent.mkdir(parents=True, exist_ok=True)
    shutil.copy2(source, candidate)
    source_digest = hashlib.sha256(source.read_bytes()).hexdigest()
    candidate_digest = hashlib.sha256(candidate.read_bytes()).hexdigest()
    if source_digest != candidate_digest:
        raise RuntimeError("candidate RCC differs from released generation-A output")
    return {"source": str(source), "candidate": str(candidate),
            "sourceSha256": source_digest, "candidateSha256": candidate_digest}


def _write_release_candidate_receipt(root, *, source, commands, gates=None):
    """Write a stable, machine-readable receipt for one release-candidate run."""
    root = Path(root)
    root.mkdir(parents=True, exist_ok=True)
    source = Path(source).resolve()
    source_record = {"path": str(source)}
    if source.is_file():
        source_record["sha256"] = hashlib.sha256(source.read_bytes()).hexdigest()
    commit = subprocess.check_output(["git", "rev-parse", "HEAD"], text=True).strip()
    if len(commit) != 40 or any(char not in "0123456789abcdef" for char in commit):
        raise RuntimeError(f"git returned a non-exact commit SHA: {commit!r}")
    receipt = root / "release-candidate-v1.json"
    receipt.write_text(json.dumps({
        "schemaVersion": 1,
        "commitSha": commit,
        "source": source_record,
        "commands": list(commands),
        "gates": dict(gates or {}),
    }, indent=2, sort_keys=True) + "\n")
    return receipt


def _run_checked(argv, env=None):
    subprocess.run([str(part) for part in argv], check=True, env=env)


def _invoke_command(task_name):
    executable = subprocess.list2cmdline([sys.executable]) if sys.platform == "win32" else shlex.quote(sys.executable)
    return f"{executable} -m invoke {task_name}"


def _contained_go_env():
    return {"CGO_ENABLED": "0", "GOARCH": "amd64"}


def _contained_race_env():
    if shutil.which("gcc") is None:
        raise RuntimeError("artifactRace requires gcc available on PATH")
    return {"CGO_ENABLED": "1", "GOARCH": "amd64", "CC": "gcc"}


def _new_self_host_homes():
    root = Path(os.environ.get("RCC_SELF_HOST_ROOT", Path.cwd().resolve().parent)).resolve()
    root.mkdir(parents=True, exist_ok=True)
    parent = Path(tempfile.mkdtemp(dir=str(root), prefix=".rcc-self-host-homes-"))
    home_a, home_b = parent / "home-a", parent / "home-b"
    home_a.mkdir()
    home_b.mkdir()
    return home_a, home_b


def _artifact_race_packages():
    return ["./environmentartifact", "./artifactprovider", "./environmentlifecycle", "./htfs", "./cmd"]


def _known_go_vet_findings():
    return [
        "htfs/directory.go:335:17: method ReadFrom(source io.Reader) error should have signature ReadFrom(io.Reader) (int64, error)",
        "htfs/relocator_test.go:18:3: result of fmt.Sprintf call not used",
        "htfs/relocator_test.go:21:3: result of fmt.Sprintf call not used",
        "htfs/relocator_test.go:24:3: result of fmt.Sprintf call not used",
        "htfs/relocator_test.go:27:3: result of fmt.Sprintf call not used",
        "operations/tlscheck.go:247:11: the cancel function returned by context.WithTimeout should be called, not discarded, to avoid a context leak",
        "operations/pull.go:67:2: unreachable code",
    ]


def _validate_go_vet_result(returncode, output):
    findings = [line.strip() for line in output.splitlines() if line.strip()]
    if returncode == 0 and not findings:
        return
    if findings != _known_go_vet_findings():
        raise RuntimeError(f"unexpected go vet findings: {findings}")

# Determine OS-specific commands
if sys.platform == "win32":
    PYTHON = "python"
    LS = "dir"
    WHICH = "where"
else:
    PYTHON = "python3"
    LS = "ls -l"
    WHICH = "which -a"


@task
def what(c):
    """Show latest HEAD with stats"""
    c.run("go version")
    c.run("git --no-pager log -2 --stat HEAD")


@task
def tooling(c):
    """Display tooling information"""
    print(f"PATH is {os.environ['PATH']}")
    print(f"GOPATH is {os.environ.get('GOPATH', 'Not set')}")
    print(f"GOROOT is {os.environ.get('GOROOT', 'Not set')}")
    print("git info:")
    c.run(f"{WHICH} git || echo NA")


@task
def noassets(c):
    """Remove asset files"""
    import glob

    patterns = [
        "blobs/assets/micromamba.*",
        "blobs/assets/*.zip",
        "blobs/assets/*.yaml",
        "blobs/assets/*.py",
        "blobs/assets/man/*.txt",
        "blobs/docs/*.md",
    ]
    for pattern in patterns:
        for file_path in glob.glob(pattern, recursive=True):
            try:
                os.remove(file_path)
                print(f"Removed: {file_path}")
            except OSError as e:
                print(f"Error removing {file_path}: {e}")


def get_official_micromamba_url(version, platform):
    """
    Get the official micromamba download URL from micro.mamba.pm (conda-forge).

    This uses the official mamba-org distribution instead of Robocorp's mirror.
    The binaries are identical (verified by SHA256), but using the official source
    eliminates dependency on Robocorp's CDN for this open-source fork.
    """
    # Map our platform names to conda-forge platform names
    platform_map = {
        "linux64": "linux-64",
        "macos64": "osx-64",
        "macosarm64": "osx-arm64",
        "windows64": "win-64",
    }
    conda_platform = platform_map.get(platform)
    if not conda_platform:
        raise ValueError(f"Unknown platform: {platform}")

    # Strip 'v' prefix if present - official API uses bare version numbers (e.g., "1.5.8" not "v1.5.8")
    if version.startswith("v"):
        version = version[1:]

    # Allow override via environment variable for custom mirrors
    base = os.environ.get("RCC_MICROMAMBA_BASE", "https://micro.mamba.pm/api/micromamba")
    base = base.rstrip("/")
    return f"{base}/{conda_platform}/{version}"


@task
def micromamba(c):
    """Download micromamba files from official conda-forge source"""
    import gzip
    import tempfile
    import tarfile

    with open("assets/micromamba_version.txt", "r", encoding="utf-8") as f:
        version = f.read().strip()
    print(f"Using micromamba version {version}")

    platforms = {
        "linux64": "linux_amd64",
        "macos64": "darwin_amd64",
        "macosarm64": "darwin_arm64",
        "windows64": "windows_amd64",
    }

    def _validate_archive_members(tar):
        for member in tar.getmembers():
            member_path = member.name
            if os.path.isabs(member_path):
                raise ValueError(f"Unsafe absolute path in archive: {member_path}")
            normalized = os.path.normpath(member_path)
            if normalized.startswith("..") or os.path.isabs(normalized):
                raise ValueError(f"Unsafe path traversal in archive: {member_path}")
            if member.issym() or member.islnk():
                raise ValueError(f"Unsafe link entry in archive: {member_path}")

    for platform, arch in platforms.items():
        output = f"blobs/assets/micromamba.{arch}"
        output_gz = output + ".gz"
        if os.path.exists(output_gz):
            print(f"Asset {output_gz} already exists, skipping")
            continue

        url = get_official_micromamba_url(version, platform)
        print(f"Downloading from official source: {url}")

        # Create unique extraction directory
        extract_dir = tempfile.mkdtemp(prefix="rcc_micromamba_extract_")

        # Download the archive
        archive_path = os.path.join(extract_dir, "micromamba.tar.bz2")
        c.run(f'curl -sL "{url}" -o "{archive_path}"')

        # Extract the binary from the archive
        # The archive contains Library/bin/micromamba.exe on Windows, bin/micromamba on Unix
        with tarfile.open(archive_path, "r:bz2") as tar:
            _validate_archive_members(tar)
            tar.extractall(path=extract_dir)

        # Find and move the binary
        if platform == "windows64":
            # Windows archive has binary at Library/bin/micromamba.exe
            src_binary = os.path.join(extract_dir, "Library", "bin", "micromamba.exe")
        else:
            # Unix archives have binary at bin/micromamba
            src_binary = os.path.join(extract_dir, "bin", "micromamba")

        shutil.move(src_binary, output)

        # Compress using Python's gzip module (cross-platform)
        print(f"Compressing {output}")
        with open(output, "rb") as f_in:
            with gzip.open(output_gz, "wb", compresslevel=9) as f_out:
                shutil.copyfileobj(f_in, f_out)
        os.remove(output)

        # Cleanup
        shutil.rmtree(extract_dir, ignore_errors=True)


@task(pre=[micromamba])
def assets(c):
    """Prepare asset files"""
    import glob
    from zipfile import ZIP_DEFLATED, ZipFile

    # Process template directories
    for directory in glob.glob("templates/*/"):
        basename = os.path.basename(os.path.dirname(directory))
        assetname = os.path.abspath(f"blobs/assets/{basename}.zip")

        if os.path.exists(assetname):
            print(f"Asset {assetname} already exists, skipping")
            continue

        print(f"Directory {directory} => {assetname}")

        with ZipFile(assetname, "w", ZIP_DEFLATED) as zipf:
            for root, _, files in os.walk(directory):
                for file in files:
                    file_path = os.path.join(root, file)
                    arcname = os.path.relpath(file_path, directory)
                    zipf.write(file_path, arcname)

    # Copy asset files
    asset_patterns = ["assets/*.txt", "assets/*.yaml", "assets/*.py"]
    for pattern in asset_patterns:
        for file in glob.glob(pattern):
            print(f"Copying {file} to blobs/assets/")
            shutil.copy(file, "blobs/assets/")

    # Copy man pages
    os.makedirs("blobs/assets/man", exist_ok=True)
    for file in glob.glob("assets/man/*.txt"):
        print(f"Copying {file} to blobs/assets/man/")
        shutil.copy(file, "blobs/assets/man/")

    # Copy docs
    os.makedirs("blobs/docs", exist_ok=True)
    for file in glob.glob("docs/*.md"):
        print(f"Copying {file} to blobs/docs/")
        shutil.copy(file, "blobs/docs/")


@task(pre=[noassets])
def clean(c):
    """Remove build directory"""
    shutil.rmtree("build", ignore_errors=True)
    print("Removed build directory")


@task
def toc(c):
    """Update table of contents on docs/ directory"""
    c.run(f"{PYTHON} scripts/toc.py")
    print("Ran scripts/toc.py")

@task
def deadcode(c):
    """Report Go functions unreachable in this build configuration"""
    c.run(f"{PYTHON} scripts/deadcode.py")


@task
def format(c):
    """Format tracked Go files"""
    output = subprocess.check_output(["git", "ls-files", "-z", "--", "*.go"])
    files = [path for path in output.decode().split("\0") if path]
    subprocess.run(["gofmt", "-w", *files], check=True)


@task
def lint(c):
    """Lint changed Go code"""
    c.run("golangci-lint run --new", env={"CGO_ENABLED": "0"})


@task
def lintAll(c):
    """Report all Go lint findings"""
    c.run("golangci-lint run", env={"CGO_ENABLED": "0"})


@task
def lintFix(c):
    """Apply supported lint fixes to changed Go code"""
    c.run("golangci-lint run --new --fix", env={"CGO_ENABLED": "0"})


@task
def agentdocs(c):
    """Run tests and structural validation for repository-local agent guidance"""
    c.run(f"{PYTHON} -m unittest scripts/test_validate_agent_docs.py")
    c.run(f"{PYTHON} scripts/validate_agent_docs.py")


@task(pre=[toc])
def support(c):
    """Create necessary directories"""
    for dir in ["tmp", "build/linux64", "build/macos64", "build/macosarm64", "build/windows64"]:
        os.makedirs(dir, exist_ok=True)


@task(pre=[support, assets])
def test(c, cover=False):
    """Run tests"""
    os.environ["GOARCH"] = "amd64"
    os.environ["CGO_ENABLED"] = "0"
    if cover:
        c.run("go test -cover -coverprofile=tmp/cover.out ./...")
        c.run("go tool cover -func=tmp/cover.out")
    else:
        c.run("go test ./...")


@task
def artifactFocused(c):
    """Run the focused Environment Artifacts v1 package tests."""
    c.run("go test -count=1 ./artifacttrust ./environmentartifact ./artifactprovider ./environmentlifecycle ./htfs ./cmd/...", env=_contained_go_env())
    c.run(f"{PYTHON} -m unittest scripts/test_environment_artifact_tasks.py scripts/test_validate_release_topology.py")


@task
def artifactRace(c):
    """Run focused package tests with the race detector on Linux."""
    _require_linux("artifactRace")
    c.run(f"go test -race -count=1 {' '.join(_artifact_race_packages())}", env=_contained_race_env())


@task
def goVet(c):
    """Run go vet and reject any finding outside the documented legacy baseline."""
    env = os.environ.copy()
    env.update(_contained_go_env())
    result = subprocess.run(["go", "vet", "./..."], env=env, text=True, capture_output=True)
    output = result.stdout + result.stderr
    if output:
        print(output, end="", file=sys.stderr)
    _validate_go_vet_result(result.returncode, output)
    if result.returncode:
        print("go vet reported only the documented unchanged baseline")


@task
def artifactVertical(c):
    """Run the real RCC A/B vertical lifecycle test."""
    _require_linux("artifactVertical")
    local(c, do_test=False)
    env = _contained_go_env()
    env.update({
        "RCC_REAL_ARTIFACT_TEST": "1",
        "RCC_REAL_BINARY": str(Path("build/rcc").resolve()),
        "RCC_NATIVE_PLATFORM": "linux-amd64",
        "RCC_REAL_RECEIPT_FILE": str((Path("tmp") / "native-runtime-receipt.json").resolve()),
    })
    if os.environ.get("RCC_N1_ARCHIVE"):
        env["RCC_N1_ARCHIVE_OUTPUT"] = str(Path(os.environ["RCC_N1_ARCHIVE"]).resolve())
    c.run("go test -count=1 ./environmentlifecycle -run '^TestRealCurrentRCCAtoBVertical$'", env=env)


@task
def artifactRobot(c):
    """Build and run only the Environment Artifacts robot acceptance suite."""
    _require_linux("artifactRobot")
    local(c, do_test=False)
    os.makedirs("tmp/environment-artifacts-robot", exist_ok=True)
    c.run(f"{PYTHON} -m robot -L DEBUG -d tmp/environment-artifacts-robot robot_tests/environment_artifacts.robot")


@task
def binaryInventory(c):
    """Build the release matrix and validate its exact binary topology."""
    build(c)
    expected = []
    for _, _, directory, extension in _binary_inventory_targets():
        expected.append(Path("build") / directory / f"rcc{extension}")
        expected.append(Path("build") / directory / f"rccremote{extension}")
    actual = sorted(path for path in Path("build").glob("*/rcc*") if path.is_file())
    if sorted(expected) != actual:
        raise RuntimeError(f"binary inventory mismatch: expected {sorted(expected)}, found {actual}")
    c.run(f"{PYTHON} scripts/validate_release_topology.py --build-root build --workflow .github/workflows/rcc.yaml")


@task
def selfHostBuild(c):
    """Build RCC and RCC remote in an isolated self-host generation."""
    generation = os.environ.get("RCC_SELF_HOST_GENERATION", "current")
    output = Path("build") / "self-host" / generation
    output.mkdir(parents=True, exist_ok=True)
    c.run("go test ./...", env=_contained_go_env())
    c.run(f"go build -o {output / 'rcc'} ./cmd/rcc", env=_contained_go_env())
    c.run(f"go build -o {output / 'rccremote'} ./cmd/rccremote", env=_contained_go_env())


@task
def selfHostProbe(c):
    """Prove the candidate toolkit can resolve and compile without host mutation."""
    c.run("go test ./environmentartifact ./artifactprovider ./environmentlifecycle ./htfs ./cmd/...", env=_contained_go_env())


@task
def selfHost(c):
    """Exercise released-to-candidate and candidate-to-released self-hosting."""
    _require_linux("selfHost")
    released = os.environ.get("RCC_N1_BINARY") or os.environ.get("RCC_SELF_HOST_RELEASED_BINARY") or shutil.which("rcc")
    if not released:
        raise RuntimeError("RCC_SELF_HOST_RELEASED_BINARY or PATH rcc is required")
    released = str(Path(released).resolve())
    released_info = _binary_metadata(released)
    expected_version = os.environ.get("RCC_SELF_HOST_EXPECTED_VERSION", "v18.18.1")
    if expected_version not in released_info["version"]:
        raise RuntimeError(f"released RCC must be {expected_version}: {released_info['version']}")
    root = (Path("tmp") / f"rcc-self-host-{os.getpid()}").resolve()
    root.mkdir(parents=True, exist_ok=False)
    home_a, home_b = _new_self_host_homes()
    candidate = Path("build/self-host/candidate/rcc").resolve()
    commands = []
    fixture = root / "robot.yaml"
    fixture.write_text(
        "tasks:\n  proof:\n    command: [python, -c, \"print('n1-old-task-ok')\"]\n"
        "condaConfigFile: conda.yaml\nartifactsDir: output\n"
    )
    n1_archive = os.environ.get("RCC_N1_ARCHIVE")
    conda = root / "conda.yaml"
    if n1_archive:
        conda.write_bytes(_archive_legacy_blueprint(Path(n1_archive).resolve()))
    else:
        conda.write_text("channels:\n  - conda-forge\ndependencies:\n  - python=3.10\n")

    def invoke(binary, home, step, generation=None):
        env = os.environ.copy(); env["ROBOCORP_HOME"] = str(home)
        if generation: env["RCC_SELF_HOST_GENERATION"] = generation
        argv = [binary, "run", "-r", "developer/toolkit.yaml", "--dev", "-t", step]
        _run_checked(argv, env)
        commands.append({"step": step, "argv": argv, "env": {"ROBOCORP_HOME": str(home)}})

    invoke(released, home_a, "selfHostBuild", "released")
    generation_a = Path("build/self-host/released/rcc")
    promotion = _promote_self_host_generation(generation_a, Path("build/rcc"))
    candidate = Path(promotion["candidate"])
    candidate_info = _binary_metadata(candidate)
    invoke(str(candidate), home_a, "selfHostProbe")
    invoke(str(candidate), home_b, "selfHostBuild", "candidate")
    generation_b = Path("build/self-host/candidate/rcc").resolve()
    generation_b_remote = Path("build/self-host/candidate/rccremote").resolve()
    generation_b_info = _binary_metadata(generation_b)
    invoke(str(generation_b), home_b, "selfHostProbe")
    (home_b / "artifacts" / "v1").mkdir(parents=True, exist_ok=True)
    (home_b / "artifacts" / "v1" / "metadata.json").write_text('{"schemaVersion":1}\n')
    invoke(released, home_b, "selfHostProbe")
    v12_evidence = []
    artifact_evidence = []
    def v12(binary, home, label):
        env = os.environ.copy(); env["ROBOCORP_HOME"] = str(home)
        argv = [binary, "holotree", "variables", str(conda), "--robot", str(fixture), "--json"]
        completed = subprocess.run([str(part) for part in argv], check=True, env=env,
                                   capture_output=True, text=True)
        variables = json.loads(completed.stdout)
        keys = {entry.get("key") for entry in variables if isinstance(entry, dict)}
        if not {"RCC_INSTALLATION_ID", "RCC_HOLOTREE_SPACE_ROOT"}.issubset(keys):
            raise RuntimeError(f"{label} did not return the required v12 identity variables")
        evidence_path = root / f"{label}.json"
        evidence_path.write_text(json.dumps(variables, indent=2, sort_keys=True) + "\n")
        v12_evidence.append(evidence_path)
        commands.append({"step": label, "argv": argv, "env": {"ROBOCORP_HOME": str(home)},
                         "evidencePath": str(evidence_path)})
    v12(released, home_a, "released-v12")
    v12(str(generation_b), home_a, "candidate-v12")
    v12(released, home_b, "released-v12-compatibility")
    if os.environ.get("RCC_N1_BINARY") and not n1_archive:
        raise RuntimeError("RCC_N1_ARCHIVE is required when RCC_N1_BINARY is supplied")
    if n1_archive:
        archive = Path(n1_archive).resolve()
        if not archive.is_file():
            raise RuntimeError(f"RCC_N1_ARCHIVE is not a file: {archive}")
        _install_archive_legacy_closure(home_a, archive)
        v12(released, home_a, "released-v12-archive-closure")
        legacy_before = _legacy_closure_state(home_a, archive)
        artifact_before = _state_digests(home_a / "artifacts" / "v1")
        archive_digest = hashlib.sha256(archive.read_bytes()).hexdigest()
        archive_hash_path = root / "n1-archive-sha256.txt"
        archive_hash_path.write_text(archive_digest + "\n")
        artifact_evidence.append(archive_hash_path)
        # N-1 acceptance is deliberately one home: the released binary first
        # creates the v12 closure, the candidate imports the archive, and the
        # old binary then consumes that same home through its legacy command.
        candidate_env = os.environ.copy(); candidate_env["ROBOCORP_HOME"] = str(home_a)
        trust_root = home_a / "artifacts" / "v1" / "trust"
        candidate_command = [str(generation_b), "env", "acquire", "--archive", str(archive),
                             "--trust-carrier", str(trust_root), "--trust-carrier-type", "filesystem",
                             "--permissive-local", "--json"]
        candidate_result = subprocess.run(
            candidate_command, check=True, env=candidate_env, capture_output=True, text=True
        )
        candidate_receipt = json.loads(candidate_result.stdout)
        artifact_digest = candidate_receipt.get("artifactDigest")
        if not artifact_digest or not candidate_receipt.get("materializationId"):
            raise RuntimeError(f"candidate archive receipt lacks artifact identity: {candidate_receipt}")
        candidate_path = root / "candidate-archive-upgrade.json"
        candidate_path.write_text(candidate_result.stdout); artifact_evidence.append(candidate_path)
        commands.append({"step": "candidate-archive-upgrade", "argv": candidate_command,
                         "env": {"ROBOCORP_HOME": str(home_a)}, "evidencePath": str(candidate_path)})
        legacy_after_upgrade = _legacy_closure_state(home_a, archive)
        artifact_after_upgrade = _state_digests(home_a / "artifacts" / "v1")
        if legacy_after_upgrade != legacy_before:
            raise RuntimeError("candidate archive upgrade changed the legacy v12 catalog/object closure")
        old_command = [released, "env", "acquire", "--archive", str(archive), "--json"]
        old_attempt = subprocess.run(old_command, env=candidate_env, capture_output=True, text=True)
        old_attempt_path = root / "released-archive-rollback-attempt.json"
        old_attempt_path.write_text(json.dumps({
            "argv": old_command,
            "returncode": old_attempt.returncode,
            "stdout": old_attempt.stdout,
            "stderr": old_attempt.stderr,
        }, indent=2, sort_keys=True) + "\n")
        artifact_evidence.append(old_attempt_path)
        commands.append({"step": "released-archive-rollback-attempt", "argv": old_command,
                         "env": {"ROBOCORP_HOME": str(home_a)}, "evidencePath": str(old_attempt_path)})
        old_error = (old_attempt.stdout + "\n" + old_attempt.stderr).lower()
        unsupported_archive_cli = old_attempt.returncode != 0 and (
            ("unknown flag" in old_error and "archive" in old_error)
            or ("unknown command" in old_error and ('"env"' in old_error or "archive" in old_error))
        )
        if not unsupported_archive_cli:
            raise RuntimeError("released N-1 archive rollback was not the expected unsupported CLI error")
        legacy_command = [released, "task", "testrun", "--robot", str(fixture), "--task", "proof", "--no-outputs"]
        legacy = subprocess.run(legacy_command, check=True, env=candidate_env, capture_output=True, text=True)
        legacy_output = legacy.stdout + "\n" + legacy.stderr
        if "n1-old-task-ok" not in legacy_output:
            raise RuntimeError(f"released N-1 task did not execute the imported v12 closure: {legacy_output[-2000:]}")
        legacy_receipt_path = root / "released-v12-rollback-consumption.json"
        legacy_receipt_path.write_text(json.dumps({"argv": legacy_command, "returncode": legacy.returncode,
                                                   "stdout": legacy.stdout, "stderr": legacy.stderr},
                                                  indent=2, sort_keys=True) + "\n")
        artifact_evidence.append(legacy_receipt_path)
        commands.append({"step": "released-v12-rollback-consumption", "argv": legacy_command,
                         "env": {"ROBOCORP_HOME": str(home_a)}, "evidencePath": str(legacy_receipt_path)})
        legacy_after_rollback = _legacy_closure_state(home_a, archive)
        artifact_after_rollback = _state_digests(home_a / "artifacts" / "v1")
        if legacy_after_rollback != legacy_before:
            raise RuntimeError("N-1 legacy v12 consumption changed catalog/object bytes")
        if artifact_after_rollback != artifact_after_upgrade:
            raise RuntimeError("N-1 artifact state was not stable after rollback consumption")
        rollback_receipt = root / "n1-rollback-state.json"
        rollback_receipt.write_text(json.dumps({"archiveSha256": archive_digest, "artifactDigest": artifact_digest,
                                                "materializationId": candidate_receipt["materializationId"],
                                                "legacyBefore": legacy_before,
                                                "legacyAfterUpgrade": legacy_after_upgrade,
                                                "legacyAfterRollback": legacy_after_rollback,
                                                "artifactBefore": artifact_before,
                                                "artifactAfterRollback": artifact_after_rollback},
                                               indent=2, sort_keys=True) + "\n")
        artifact_evidence.append(rollback_receipt)
    evidence = [fixture, generation_a, candidate, generation_b, generation_b_remote,
                home_b / "artifacts" / "v1" / "metadata.json", *v12_evidence, *artifact_evidence]
    receipt = _write_self_host_receipt("tmp", released_binary=released, candidate_binary=candidate,
                                       home_a=home_a, home_b=home_b, commands=commands,
                                       binary_metadata={"released": released_info, "candidate": candidate_info,
                                                         "generationA": promotion,
                                                         "generationB": generation_b_info},
                                       evidence_paths=evidence)
    print(f"Self-host receipt: {receipt}")


@task
def largeStream(c):
    """Run the mandatory multi-gigabyte bounded HTTP streaming acceptance gate."""
    _require_linux("largeStream")
    receipt = Path(os.environ.get("RCC_LARGE_STREAM_RECEIPT", "tmp/large-stream-receipt.json")).resolve()
    receipt.parent.mkdir(parents=True, exist_ok=True)
    env = _contained_go_env()
    env.update({"RCC_REAL_LARGE_STREAM": "1", "RCC_LARGE_STREAM_RECEIPT": str(receipt),
                "RCC_SOURCE_SHA": subprocess.check_output(["git", "rev-parse", "HEAD"], text=True).strip()})
    c.run("go test -count=1 ./artifactprovider -run '^TestHTTPMultiGiByteStreamingAcceptance$' -timeout 30m", env=env)
    if not receipt.is_file():
        raise RuntimeError(f"largeStream did not produce receipt: {receipt}")


@task
def releaseCandidate(c):
    """Run the complete Environment Artifacts v1 release-candidate gate."""
    _require_linux("releaseCandidate")
    task_names = (
        "artifactFocused",
        "artifactRace",
        "artifactVertical",
        "artifactRobot",
        "binaryInventory",
        "largeStream",
        "robot",
        "selfHost",
        "goVet",
        "coordinationAcceptance",
    )
    commands = []
    gates = {}
    for task_name in task_names:
        command = _invoke_command(task_name)
        commands.append(command)
        c.run(command)
        gates[task_name] = "passed"
    receipt = _write_release_candidate_receipt(
        "tmp", source=Path("build/rcc"),
        commands=commands, gates=gates)
    print(f"Release-candidate receipt: {receipt}")


@task
def coordinationAcceptance(c):
    """Run the exact-binary coordination CLI gate and black-box scenarios."""
    _require_linux("coordinationAcceptance")
    binary = Path("build/rcc").resolve()
    if not binary.is_file():
        raise RuntimeError(f"exact RCC binary is missing: {binary}")
    c.run(f"{shlex.quote(str(binary))} env coordinate --help")
    receipt = Path("tmp/coordination-blackbox-v1.json").resolve()
    env = os.environ.copy()
    env["RCC_COORDINATION_RECEIPT"] = str(receipt)
    c.run("GOARCH=amd64 CGO_ENABLED=0 go test ./buildcoord -run '^TestBlackBoxCoordinationContract$' -count=1", env=env)
    payload = json.loads(receipt.read_text())
    required = {"claim", "heartbeat", "verified-publish", "waiter-reuse", "stale-takeover",
                "nondeterminism", "release", "provider-failure", "n-worker-prewarm",
                "staging-capacity-generation"}
    if set(payload.get("scenarios", {})) != required:
        raise RuntimeError("coordination receipt is missing required scenario outcomes")
    payload["commitSha"] = subprocess.check_output(["git", "rev-parse", "HEAD"], text=True).strip()
    payload["binarySha256"] = hashlib.sha256(binary.read_bytes()).hexdigest()
    receipt.write_text(json.dumps(payload, indent=2, sort_keys=True) + "\n")
    print(f"Coordination black-box receipt: {receipt}")


def version() -> str:
    import re

    with open("common/version.go", "r") as file:
        content = file.read()
        match = re.search(r"Version\s*=\s*`v([^`]+)`", content)
        if match:
            return match.group(1)
        else:
            raise ValueError("Version not found in common/version.go")


@task
def version_txt(c):
    """Create version.txt file"""
    support(c)
    target = "build/version.txt"
    v = version()
    with open(target, "w") as f:
        f.write(f"v{v}")
    print(f"Created {target} with version {v}")


@task(pre=[support, version_txt, assets])
def build(c, platform="all"):
    """Build executables"""
    from pathlib import Path

    os.environ["CGO_ENABLED"] = "0"

    # Define platform-arch combinations
    build_targets = [
        ("linux", "amd64", "linux64"),
        ("darwin", "amd64", "macos64"),
        ("darwin", "arm64", "macosarm64"),
        ("windows", "amd64", "windows64"),
    ]

    if platform == "all":
        targets = build_targets
    elif platform == "linux":
        targets = [t for t in build_targets if t[0] == "linux"]
    elif platform == "darwin":
        targets = [t for t in build_targets if t[0] == "darwin"]
    elif platform == "windows":
        targets = [t for t in build_targets if t[0] == "windows"]
    else:
        raise ValueError(f"Invalid platform: {platform}")

    for goos, goarch, output_dir in targets:
        os.environ["GOOS"] = goos
        os.environ["GOARCH"] = goarch
        output = f"build/{output_dir}/"

        c.run(f"go build -ldflags -s -o {output} ./cmd/...")

        ext = ".exe" if goos == "windows" else ""
        f = f"{output}rcc{ext}"
        assert Path(f).exists(), f"File {f} does not exist"
        print(f"Built: {f} ({goos}/{goarch})")


@task
def windows64(c):
    """Build windows64 executable"""
    build(c, platform="windows")


@task
def linux64(c):
    """Build linux64 executable"""
    build(c, platform="linux")


@task
def macos64(c):
    """Build macos64 executable"""
    build(c, platform="darwin")


@task
def robotsetup(c):
    """Setup build environment"""
    if not os.path.exists("robot_requirements.txt"):
        raise RuntimeError(
            f"robot_requirements.txt not found. Current directory: {os.path.abspath(os.getcwd())}"
        )
    c.run(f"{PYTHON} -m pip install --upgrade -r robot_requirements.txt")
    c.run(f"{PYTHON} -m pip freeze")


@task
def local(c, do_test=True):
    """Build local, operating system specific rcc"""
    os.environ["CGO_ENABLED"] = "0"
    tooling(c)
    if do_test:
        test(c)
    c.run("go build -o build/ ./cmd/...")


@task(pre=[robotsetup, assets, local])
def robot(c):
    """Run robot tests on local application"""
    print("Running robot tests...")
    c.run(f"{PYTHON} -m robot -L DEBUG -d tmp/output robot_tests")


@task(pre=[robotsetup, assets, local])
def unpackTest(c):
    """Run unpack robot tests"""
    print("Running unpack robot tests...")
    c.run(f"{PYTHON} -m robot -L DEBUG -d tmp/output robot_tests/robot_bundle.robot")
