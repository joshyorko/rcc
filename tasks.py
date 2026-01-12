import os
import shutil
import sys

from invoke import task

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

    with open("assets/micromamba_version.txt", "r", encoding="utf-8") as f:
        version = f.read().strip()
    print(f"Using micromamba version {version}")

    platforms = {
        "linux64": "linux_amd64",
        "macos64": "darwin_amd64",
        "macosarm64": "darwin_arm64",
        "windows64": "windows_amd64",
    }

    # Use a cross-platform temporary directory
    tmp_dir = tempfile.gettempdir()

    for platform, arch in platforms.items():
        output = f"blobs/assets/micromamba.{arch}"
        output_gz = output + ".gz"
        if os.path.exists(output_gz):
            print(f"Asset {output_gz} already exists, skipping")
            continue

        url = get_official_micromamba_url(version, platform)
        print(f"Downloading from official source: {url}")

        # Create extraction directory
        extract_dir = os.path.join(tmp_dir, "rcc_micromamba_extract")
        os.makedirs(extract_dir, exist_ok=True)

        # Download the archive
        archive_path = os.path.join(extract_dir, "micromamba.tar.bz2")
        c.run(f'curl -sL "{url}" -o "{archive_path}"')

        # Extract the binary from the archive
        # The archive contains Library/bin/micromamba.exe on Windows, bin/micromamba on Unix
        import tarfile
        with tarfile.open(archive_path, "r:bz2") as tar:
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
    """Update table of contents on docs/ directory"""
    c.run(f"{PYTHON} scripts/deadcode.py")
    print("Ran scripts/deadcode.py")


@task(pre=[toc])
def support(c):
    """Create necessary directories"""
    for dir in ["tmp", "build/linux64", "build/macos64", "build/macosarm64", "build/windows64"]:
        os.makedirs(dir, exist_ok=True)


@task(pre=[support, assets])
def test(c, cover=False):
    """Run tests"""
    os.environ["GOARCH"] = "amd64"
    if cover:
        c.run("go test -cover -coverprofile=tmp/cover.out ./...")
        c.run("go tool cover -func=tmp/cover.out")
    else:
        c.run("go test ./...")


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
