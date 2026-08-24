import argparse
import json
import re
import stat
from pathlib import Path

ASSETS = {
    "rcc-linux64", "rccremote-linux64", "rcc-windows64.exe", "rccremote-windows64.exe",
    "rcc-macos64", "rccremote-macos64", "rcc-macosarm64", "rccremote-macosarm64",
}
PUBLISHED_ASSETS = ASSETS | {"index.json"}
INDEX_ASSETS = {
    "windows": "rcc-windows64.exe",
    "linux": "rcc-linux64",
    "macos": "rcc-macos64",
    "macosarm64": "rcc-macosarm64",
}
TARGETS = {"linux64": ("rcc", "rccremote"), "windows64": ("rcc.exe", "rccremote.exe"), "macos64": ("rcc", "rccremote"), "macosarm64": ("rcc", "rccremote")}
RUNTIME_ROOT_ENV_VARS = {"ROBOCORP_HOME", "RCC_HOME", "GOCACHE", "GOMODCACHE", "TMPDIR", "TMP", "TEMP"}
CANDIDATE_SHA_EXPRESSION = "${{ github.event.pull_request.head.sha || github.sha }}"
RELEASE_IDENTITY_JOBS = ("build", "release-candidate", "robot", "release")


def resolve_candidate_sha(event_name, github_sha, pull_request_head_sha=None):
    candidate = pull_request_head_sha if event_name == "pull_request" else github_sha
    if not isinstance(candidate, str) or re.fullmatch(r"[0-9a-f]{40}", candidate) is None:
        raise ValueError(f"invalid candidate SHA for {event_name}: {candidate!r}")
    return candidate


def _workflow_job(workflow_text, name):
    match = re.search(rf"(?ms)^  {re.escape(name)}:\n(.*?)(?=^  [A-Za-z0-9_-]+:\n|\Z)", workflow_text)
    return match.group(1) if match else ""


def validate_release_identity_topology(workflow_text):
    errors = []
    for name in RELEASE_IDENTITY_JOBS:
        job = _workflow_job(workflow_text, name)
        if not job:
            errors.append(f"missing release identity job: {name}")
            continue
        if f"RCC_CANDIDATE_SHA: {CANDIDATE_SHA_EXPRESSION}" not in job:
            errors.append(f"{name} does not derive the intended candidate SHA")
        if f"ref: {CANDIDATE_SHA_EXPRESSION}" not in job:
            errors.append(f"{name} checkout is not pinned to the intended candidate SHA")
        for marker in ("Assert exact candidate checkout", "git rev-parse HEAD", '"$actual_sha" != "$RCC_CANDIDATE_SHA"'):
            if marker not in job:
                errors.append(f"{name} lacks candidate checkout assertion: {marker}")
    native = _workflow_job(workflow_text, "robot")
    if f"RCC_SOURCE_SHA: {CANDIDATE_SHA_EXPRESSION}" not in native:
        errors.append("robot native source SHA is not the intended candidate SHA")
    if '_validate_receipt_commit(data, os.environ["RCC_CANDIDATE_SHA"])' not in native:
        errors.append("robot native receipt is not validated against the intended candidate SHA")
    release = _workflow_job(workflow_text, "release")
    if f"target_commitish: {CANDIDATE_SHA_EXPRESSION}" not in release:
        errors.append("release target is not the intended candidate SHA")
    for marker in ('git rev-parse "${TAG}^{commit}"', '"$tag_sha" != "$RCC_CANDIDATE_SHA"'):
        if marker not in release:
            errors.append(f"release tag target assertion is missing: {marker}")
    return errors


def _runtime_root_is_beneath_checkout(value):
    value = value.strip().strip('"\'')
    workspace_roots = ("${{ github.workspace }}", "$GITHUB_WORKSPACE", "${GITHUB_WORKSPACE}")
    for workspace in workspace_roots:
        if value == workspace:
            return True
        if value.startswith(workspace + "/"):
            return not value.startswith(workspace + "/../")
    return not re.match(r"^(?:/|~|\$|[A-Za-z]:[\\/])", value)


def validate_release_runtime_roots(workflow_text):
    errors = []
    assignment = re.compile(r"^\s*(" + "|".join(sorted(RUNTIME_ROOT_ENV_VARS)) + r"):\s*(\S.*?)\s*$")
    for line in workflow_text.splitlines():
        match = assignment.match(line)
        if match and _runtime_root_is_beneath_checkout(match.group(2)):
            errors.append(f"{match.group(1)} runtime root must be outside the checkout: {match.group(2)}")
    return errors


def validate(root, workflow, index, asset_root=None):
    errors = []
    if asset_root:
        asset_root = Path(asset_root)
        children = list(asset_root.iterdir()) if asset_root.is_dir() and not asset_root.is_symlink() else []
        staged = {path.name for path in children}
        if staged != PUBLISHED_ASSETS:
            errors.append(f"downloaded assets mismatch: {sorted(staged)}")
        for path in children:
            if path.name in PUBLISHED_ASSETS and (path.is_symlink() or not stat.S_ISREG(path.lstat().st_mode)):
                errors.append(f"downloaded asset is not a regular file: {path.name}")
    else:
        for directory, binaries in TARGETS.items():
            for binary in binaries:
                if not (root / "build" / directory / binary).is_file():
                    errors.append(f"missing build binary: build/{directory}/{binary}")
    if workflow:
        text = Path(workflow).read_text()
        staged = set(re.findall(r"\bcp\s+\S+\s+rcc-builds/([^\s]+)", text))
        if staged != ASSETS:
            errors.append(f"workflow staged assets mismatch: {sorted(staged)}")
        errors.extend(validate_release_runtime_roots(text))
        if re.search(r"(?m)^  build:\s*$", text):
            errors.extend(validate_release_identity_topology(text))
    if index:
        data = json.loads(Path(index).read_text())
        tested = data.get("tested", [])
        if not tested:
            errors.append("index has no tested release entry")
        else:
            entry = tested[0]
            for key, expected in INDEX_ASSETS.items():
                if key not in entry:
                    errors.append(f"index entry missing required {key} URL")
                    continue
                value = entry[key]
                basename = value.rsplit("/", 1)[-1]
                if basename != expected:
                    errors.append(f"index {key} URL basename must be {expected}: {basename}")
    return errors


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("root", type=Path, nargs="?")
    parser.add_argument("--build-root", type=Path)
    parser.add_argument("--asset-root", type=Path)
    parser.add_argument("--workflow", type=Path)
    parser.add_argument("--index", type=Path)
    args = parser.parse_args()
    root = args.root or (args.build_root.parent if args.build_root else None)
    if root is None and args.asset_root is None:
        parser.error("a repository root, --build-root, or --asset-root is required")
    errors = validate(root, args.workflow, args.index, args.asset_root)
    for error in errors:
        print(error)
    return 1 if errors else 0


if __name__ == "__main__":
    raise SystemExit(main())
