#!/usr/bin/env python3
"""
RCC Environment Health Check utility.

Usage:
    python env_check.py [--verbose]

Checks:
    - RCC installation and version
    - Python environment in holotree
    - Network connectivity
    - Disk space
    - Environment variables

This complements `rcc configure diagnostics` with Python-specific checks.
"""

import argparse
import os
import shutil
import subprocess
import sys
from pathlib import Path
from typing import Dict, List, Tuple


class HealthCheck:
    """Environment health check results."""

    def __init__(self):
        self.checks: List[Tuple[str, bool, str]] = []

    def add(self, name: str, passed: bool, message: str):
        self.checks.append((name, passed, message))

    def print_results(self):
        print("\n=== RCC Environment Health Check ===\n")
        passed = 0
        failed = 0

        for name, status, message in self.checks:
            icon = "✓" if status else "✗"
            status_str = "PASS" if status else "FAIL"
            print(f"[{icon}] {name}: {status_str}")
            if message:
                print(f"    {message}")
            if status:
                passed += 1
            else:
                failed += 1

        print(f"\n=== Summary: {passed} passed, {failed} failed ===")
        return failed == 0


def check_rcc_installed() -> Tuple[bool, str]:
    """Check if rcc is installed and get version."""
    try:
        result = subprocess.run(
            ["rcc", "version"],
            capture_output=True,
            text=True,
            timeout=10
        )
        if result.returncode == 0:
            version = result.stdout.strip().split("\n")[0]
            return True, f"Version: {version}"
        return False, "rcc found but version check failed"
    except FileNotFoundError:
        return False, "rcc not found in PATH"
    except subprocess.TimeoutExpired:
        return False, "rcc version check timed out"
    except Exception as e:
        return False, f"Error: {e}"


def check_robocorp_home() -> Tuple[bool, str]:
    """Check ROBOCORP_HOME directory."""
    home = os.environ.get("ROBOCORP_HOME")
    if home:
        path = Path(home)
        if path.exists():
            return True, f"Custom: {home}"
        return False, f"Set to {home} but directory doesn't exist"

    # Check default locations
    default_paths = [
        Path.home() / ".robocorp",
        Path("/opt/robocorp"),
        Path("C:/ProgramData/robocorp"),
    ]

    for default in default_paths:
        if default.exists():
            return True, f"Default: {default}"

    return True, "Not set (will use default on first run)"


def check_holotree_spaces() -> Tuple[bool, str]:
    """Check holotree environments."""
    try:
        result = subprocess.run(
            ["rcc", "holotree", "list"],
            capture_output=True,
            text=True,
            timeout=30
        )
        if result.returncode == 0:
            lines = [l for l in result.stdout.strip().split("\n") if l.strip()]
            # Skip header lines
            spaces = len([l for l in lines if not l.startswith("Identity")])
            return True, f"{spaces} environment(s) found"
        return True, "No environments yet"
    except Exception as e:
        return False, f"Error listing holotrees: {e}"


def check_disk_space() -> Tuple[bool, str]:
    """Check available disk space."""
    try:
        home = os.environ.get("ROBOCORP_HOME", str(Path.home() / ".robocorp"))
        path = Path(home)
        if not path.exists():
            path = Path.home()

        usage = shutil.disk_usage(path)
        free_gb = usage.free / (1024**3)
        total_gb = usage.total / (1024**3)
        used_pct = (usage.used / usage.total) * 100

        if free_gb < 1:
            return False, f"Low disk space: {free_gb:.1f}GB free ({used_pct:.0f}% used)"
        elif free_gb < 5:
            return True, f"Warning: {free_gb:.1f}GB free of {total_gb:.1f}GB ({used_pct:.0f}% used)"
        return True, f"{free_gb:.1f}GB free of {total_gb:.1f}GB"
    except Exception as e:
        return False, f"Error checking disk: {e}"


def check_network() -> Tuple[bool, str]:
    """Check network connectivity to conda-forge."""
    try:
        import urllib.request
        urllib.request.urlopen("https://conda.anaconda.org/conda-forge/", timeout=10)
        return True, "conda-forge accessible"
    except Exception:
        pass

    # Try pypi as fallback
    try:
        import urllib.request
        urllib.request.urlopen("https://pypi.org/simple/", timeout=10)
        return True, "PyPI accessible (conda-forge unreachable)"
    except Exception as e:
        return False, f"Network issues: {e}"


def check_robot_yaml() -> Tuple[bool, str]:
    """Check for robot.yaml in current directory."""
    robot_yaml = Path("robot.yaml")
    if robot_yaml.exists():
        return True, "Found in current directory"
    return True, "Not found (not in a robot directory)"


def check_conda_yaml() -> Tuple[bool, str]:
    """Check for conda.yaml in current directory."""
    conda_yaml = Path("conda.yaml")
    if conda_yaml.exists():
        return True, "Found in current directory"

    # Check robot.yaml for reference
    robot_yaml = Path("robot.yaml")
    if robot_yaml.exists():
        try:
            import yaml
            with open(robot_yaml) as f:
                data = yaml.safe_load(f)
                if "condaConfigFile" in data:
                    conda_file = Path(data["condaConfigFile"])
                    if conda_file.exists():
                        return True, f"Found: {conda_file}"
                    return False, f"Referenced {conda_file} not found"
        except ImportError:
            pass
        except Exception:
            pass

    return True, "Not found (not in a robot directory)"


def check_python_version() -> Tuple[bool, str]:
    """Check Python version."""
    version = sys.version_info
    version_str = f"{version.major}.{version.minor}.{version.micro}"

    if version.major < 3:
        return False, f"Python 2.x not supported: {version_str}"
    if version.minor < 8:
        return False, f"Python 3.8+ recommended: {version_str}"

    return True, f"Python {version_str}"


def check_env_variables() -> Tuple[bool, str]:
    """Check RCC-related environment variables."""
    rcc_vars = {
        "ROBOCORP_HOME": os.environ.get("ROBOCORP_HOME"),
        "RCC_VERBOSE_ENVIRONMENT_BUILDING": os.environ.get("RCC_VERBOSE_ENVIRONMENT_BUILDING"),
        "RCC_NO_BUILD": os.environ.get("RCC_NO_BUILD"),
        "RCC_ENDPOINT_PYPI": os.environ.get("RCC_ENDPOINT_PYPI"),
        "RCC_ENDPOINT_CONDA": os.environ.get("RCC_ENDPOINT_CONDA"),
    }

    set_vars = {k: v for k, v in rcc_vars.items() if v is not None}

    if not set_vars:
        return True, "Using defaults"

    details = ", ".join(f"{k}={v}" for k, v in set_vars.items())
    return True, f"Custom: {details}"


def run_rcc_diagnostics(verbose: bool = False) -> Tuple[bool, str]:
    """Run rcc configure diagnostics."""
    try:
        args = ["rcc", "configure", "diagnostics", "--quick"]
        if not verbose:
            args.append("--json")

        result = subprocess.run(
            args,
            capture_output=True,
            text=True,
            timeout=60
        )

        if result.returncode == 0:
            if verbose:
                return True, f"\n{result.stdout}"
            return True, "All checks passed"
        return False, f"Some checks failed: {result.stderr or result.stdout}"
    except subprocess.TimeoutExpired:
        return False, "Diagnostics timed out"
    except Exception as e:
        return False, f"Error: {e}"


def main():
    parser = argparse.ArgumentParser(
        description="RCC environment health check"
    )
    parser.add_argument(
        "--verbose", "-v",
        action="store_true",
        help="Show detailed output"
    )
    parser.add_argument(
        "--skip-network",
        action="store_true",
        help="Skip network checks"
    )

    args = parser.parse_args()
    health = HealthCheck()

    # Run checks
    passed, msg = check_rcc_installed()
    health.add("RCC Installation", passed, msg)

    passed, msg = check_python_version()
    health.add("Python Version", passed, msg)

    passed, msg = check_robocorp_home()
    health.add("ROBOCORP_HOME", passed, msg)

    passed, msg = check_disk_space()
    health.add("Disk Space", passed, msg)

    if not args.skip_network:
        passed, msg = check_network()
        health.add("Network Access", passed, msg)

    passed, msg = check_holotree_spaces()
    health.add("Holotree Environments", passed, msg)

    passed, msg = check_robot_yaml()
    health.add("robot.yaml", passed, msg)

    passed, msg = check_conda_yaml()
    health.add("conda.yaml", passed, msg)

    passed, msg = check_env_variables()
    health.add("Environment Variables", passed, msg)

    if args.verbose:
        passed, msg = run_rcc_diagnostics(verbose=True)
        health.add("RCC Diagnostics", passed, msg)

    # Print results
    all_passed = health.print_results()

    if not all_passed:
        print("\nTip: Run 'rcc configure diagnostics' for detailed system check")

    return 0 if all_passed else 1


if __name__ == "__main__":
    sys.exit(main())
