import argparse
import json
import re
from pathlib import Path

ASSETS = {
    "rcc-linux64", "rccremote-linux64", "rcc-windows64.exe", "rccremote-windows64.exe",
    "rcc-macos64", "rccremote-macos64", "rcc-macosarm64", "rccremote-macosarm64",
}
TARGETS = {"linux64": ("rcc", "rccremote"), "windows64": ("rcc.exe", "rccremote.exe"), "macos64": ("rcc", "rccremote"), "macosarm64": ("rcc", "rccremote")}


def validate(root, workflow, index):
    errors = []
    for directory, binaries in TARGETS.items():
        for binary in binaries:
            if not (root / "build" / directory / binary).is_file():
                errors.append(f"missing build binary: build/{directory}/{binary}")
    if workflow:
        text = Path(workflow).read_text()
        staged = set(re.findall(r"\bcp\s+\S+\s+rcc-builds/([^\s]+)", text))
        if staged != ASSETS:
            errors.append(f"workflow staged assets mismatch: {sorted(staged)}")
    if index:
        data = json.loads(Path(index).read_text())
        url_keys = {"windows", "linux", "macos", "macosarm64"}
        tested = data.get("tested", [])
        if not tested:
            errors.append("index has no tested release entry")
        else:
            entry = tested[0]
            for key in url_keys:
                if key not in entry:
                    errors.append(f"index entry missing required {key} URL")
                    continue
                value = entry[key]
                basename = value.rsplit("/", 1)[-1]
                if basename not in ASSETS:
                    errors.append(f"index {key} URL basename is not a staged RCC asset: {basename}")
    return errors


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("root", type=Path, nargs="?")
    parser.add_argument("--build-root", type=Path)
    parser.add_argument("--workflow", type=Path)
    parser.add_argument("--index", type=Path)
    args = parser.parse_args()
    root = args.root or (args.build_root.parent if args.build_root else None)
    if root is None:
        parser.error("a repository root positional argument or --build-root is required")
    errors = validate(root, args.workflow, args.index)
    for error in errors:
        print(error)
    return 1 if errors else 0


if __name__ == "__main__":
    raise SystemExit(main())
