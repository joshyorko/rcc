import os
import json
import hashlib
import shutil
import subprocess
import sys
import tempfile
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


def _run_checked(argv, env=None):
    subprocess.run([str(part) for part in argv], check=True, env=env)


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
    c.run("go test -count=1 ./environmentartifact ./artifactprovider ./environmentlifecycle ./htfs ./cmd/...", env=_contained_go_env())
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
    })
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
    released = os.environ.get("RCC_SELF_HOST_RELEASED_BINARY") or shutil.which("rcc")
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
    fixture.write_text("tasks:\n  proof:\n    command: [python, -V]\ncondaConfigFile: conda.yaml\n")
    conda = root / "conda.yaml"
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
    def v12(binary, home, label):
        env = os.environ.copy(); env["ROBOCORP_HOME"] = str(home)
        argv = [binary, "holotree", "variables", str(conda), "--robot", str(fixture), "--json"]
        _run_checked(argv, env)
        commands.append({"step": label, "argv": argv, "env": {"ROBOCORP_HOME": str(home)}})
    v12(released, home_a, "released-v12")
    v12(str(generation_b), home_a, "candidate-v12")
    v12(released, home_b, "released-v12-compatibility")
    evidence = [fixture, generation_a, candidate, generation_b, generation_b_remote,
                home_a / "holotree", home_b / "holotree",
                home_b / "artifacts" / "v1" / "metadata.json"]
    receipt = _write_self_host_receipt("tmp", released_binary=released, candidate_binary=candidate,
                                       home_a=home_a, home_b=home_b, commands=commands,
                                       binary_metadata={"released": released_info, "candidate": candidate_info,
                                                         "generationA": promotion,
                                                         "generationB": generation_b_info},
                                       evidence_paths=evidence)
    print(f"Self-host receipt: {receipt}")


@task
def releaseCandidate(c):
    """Run the complete Environment Artifacts v1 release-candidate gate."""
    _require_linux("releaseCandidate")
    for task_name in (
        "artifactFocused",
        "artifactRace",
        "artifactVertical",
        "artifactRobot",
        "binaryInventory",
    ):
        c.run(f"invoke {task_name}")
    c.run("invoke robot")
    c.run("invoke selfHost")
    c.run("invoke goVet")


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
