"""Fail closed when release compiler selectors disagree with go.mod."""
import argparse
import re
from pathlib import Path

# Conda's developer/setup.yaml is deliberately a bootstrap, not provenance.
COMPILER_PINS = {
    "go.mod": (r"(?m)^go (\d+\.\d+\.\d+)$", 1),
    ".dagger/main.go": (r'defaultGoVersion\s*=\s*"(\d+\.\d+\.\d+)"', 1),
    ".github/workflows/rcc.yaml": (r'go-version:\s*"(\d+\.\d+\.\d+)"', 3),
    ".github/workflows/publish-release.yml": (r'go-version:\s*"(\d+\.\d+\.\d+)"', 1),
    ".github/workflows/export-release-evidence.yml": (r'go-version:\s*"(\d+\.\d+\.\d+)"', 1),
    "scripts/release_publication.py": (r'EXPECTED_GO_VERSION = "go(\d+\.\d+\.\d+)"', 1),
}


def validate(root):
    errors = []
    observed = {}
    for name, (pattern, count) in COMPILER_PINS.items():
        try:
            values = re.findall(pattern, (root / name).read_text(encoding="utf-8"))
        except OSError as exc:
            errors.append(f"{name}: {exc}")
            continue
        if len(values) != count:
            errors.append(f"{name}: expected {count} explicit compiler pins, found {len(values)}")
        observed[name] = values
    expected = observed.get("go.mod", [])
    if len(expected) == 1:
        for name, values in observed.items():
            if any(value != expected[0] for value in values):
                errors.append(f"{name}: compiler pins {values} differ from go.mod {expected[0]}; update the complete Go toolchain group")
    return errors


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--root", type=Path, default=Path(__file__).resolve().parents[1])
    args = parser.parse_args()
    errors = validate(args.root)
    if errors:
        raise SystemExit("\n".join(errors))
    print("Release compiler pins agree with go.mod")


if __name__ == "__main__":
    main()
