#!/usr/bin/env python3
"""
Validate robot.yaml and conda.yaml configuration files.

Usage:
    python validate_robot.py [path/to/robot.yaml]

Checks:
    - YAML syntax
    - Required fields
    - File references
    - Common issues
"""

import argparse
import os
import sys
from pathlib import Path
from typing import Any, Dict, List, Tuple

try:
    import yaml
except ImportError:
    print("PyYAML required. Install with: pip install pyyaml")
    sys.exit(1)


class ValidationResult:
    """Holds validation results."""

    def __init__(self):
        self.errors: List[str] = []
        self.warnings: List[str] = []
        self.info: List[str] = []

    def error(self, msg: str):
        self.errors.append(f"ERROR: {msg}")

    def warn(self, msg: str):
        self.warnings.append(f"WARNING: {msg}")

    def info_msg(self, msg: str):
        self.info.append(f"INFO: {msg}")

    @property
    def is_valid(self) -> bool:
        return len(self.errors) == 0

    def print_results(self):
        for msg in self.errors:
            print(f"  {msg}")
        for msg in self.warnings:
            print(f"  {msg}")
        for msg in self.info:
            print(f"  {msg}")


def load_yaml(path: Path) -> Tuple[Dict[str, Any], str]:
    """Load a YAML file, return (data, error_message)."""
    try:
        with open(path) as f:
            data = yaml.safe_load(f)
            return data or {}, ""
    except yaml.YAMLError as e:
        return {}, f"YAML syntax error: {e}"
    except FileNotFoundError:
        return {}, f"File not found: {path}"
    except Exception as e:
        return {}, f"Error reading file: {e}"


def validate_robot_yaml(path: Path, result: ValidationResult) -> Dict[str, Any]:
    """Validate robot.yaml structure."""
    data, error = load_yaml(path)

    if error:
        result.error(error)
        return {}

    robot_dir = path.parent

    # Check required fields
    if "tasks" not in data and "devTasks" not in data:
        result.error("No 'tasks' or 'devTasks' defined")

    # Validate tasks
    if "tasks" in data:
        if not isinstance(data["tasks"], dict):
            result.error("'tasks' must be a dictionary")
        else:
            for task_name, task_def in data["tasks"].items():
                if not isinstance(task_def, dict):
                    result.error(f"Task '{task_name}' must be a dictionary")
                    continue

                # Check task has valid definition
                valid_keys = {"shell", "command", "robotTaskName"}
                task_keys = set(task_def.keys())

                if not task_keys & valid_keys:
                    result.error(
                        f"Task '{task_name}' needs 'shell', 'command', or 'robotTaskName'"
                    )

    # Validate devTasks
    if "devTasks" in data:
        if not isinstance(data["devTasks"], dict):
            result.error("'devTasks' must be a dictionary")

    # Check conda config
    conda_file = None
    if "condaConfigFile" in data:
        conda_file = robot_dir / data["condaConfigFile"]
        if not conda_file.exists():
            result.error(f"condaConfigFile not found: {data['condaConfigFile']}")
        else:
            result.info_msg(f"Using conda config: {data['condaConfigFile']}")

    if "environmentConfigs" in data:
        found_config = False
        for config_name in data["environmentConfigs"]:
            config_path = robot_dir / config_name
            if config_path.exists():
                result.info_msg(f"Found environment config: {config_name}")
                if not conda_file:
                    conda_file = config_path
                found_config = True
                break
        if not found_config and not conda_file:
            result.warn("No environmentConfigs files found")

    if not conda_file and "condaConfigFile" not in data:
        result.error("No 'condaConfigFile' or 'environmentConfigs' defined")

    # Check artifacts directory
    if "artifactsDir" in data:
        artifacts_path = robot_dir / data["artifactsDir"]
        if not artifacts_path.exists():
            result.info_msg(f"artifactsDir will be created: {data['artifactsDir']}")

    # Check ignore files
    if "ignoreFiles" in data:
        for ignore_file in data["ignoreFiles"]:
            ignore_path = robot_dir / ignore_file
            if not ignore_path.exists():
                result.warn(f"ignoreFiles entry not found: {ignore_file}")

    # Check PATH entries
    if "PATH" in data:
        for path_entry in data["PATH"]:
            if path_entry != ".":
                entry_path = robot_dir / path_entry
                if not entry_path.exists():
                    result.warn(f"PATH entry not found: {path_entry}")

    # Check PYTHONPATH entries
    if "PYTHONPATH" in data:
        for path_entry in data["PYTHONPATH"]:
            if path_entry != ".":
                entry_path = robot_dir / path_entry
                if not entry_path.exists():
                    result.warn(f"PYTHONPATH entry not found: {path_entry}")

    # Check preRunScripts
    if "preRunScripts" in data:
        for script in data["preRunScripts"]:
            script_path = robot_dir / script
            if not script_path.exists():
                result.warn(f"preRunScript not found: {script}")

    return data


def validate_conda_yaml(path: Path, result: ValidationResult) -> Dict[str, Any]:
    """Validate conda.yaml structure."""
    data, error = load_yaml(path)

    if error:
        result.error(error)
        return {}

    # Check required fields
    if "channels" not in data:
        result.error("No 'channels' defined")
    elif not isinstance(data["channels"], list):
        result.error("'channels' must be a list")
    elif len(data["channels"]) == 0:
        result.error("'channels' is empty")
    else:
        if "conda-forge" not in data["channels"]:
            result.warn("Consider using 'conda-forge' channel for better compatibility")

    if "dependencies" not in data:
        result.error("No 'dependencies' defined")
    elif not isinstance(data["dependencies"], list):
        result.error("'dependencies' must be a list")
    else:
        has_python = False
        has_pip = False
        pip_deps = []

        for dep in data["dependencies"]:
            if isinstance(dep, str):
                if dep.startswith("python"):
                    has_python = True
                    # Check version format
                    if "=" in dep and "==" not in dep:
                        result.info_msg(f"Python version: {dep.split('=')[1]}")
                elif dep.startswith("pip"):
                    has_pip = True
            elif isinstance(dep, dict) and "pip" in dep:
                pip_deps = dep["pip"] or []

        if not has_python:
            result.warn("No Python version specified in dependencies")

        if pip_deps and not has_pip:
            result.warn("pip packages defined but pip not in dependencies")

        # Check pip package formats
        for pip_dep in pip_deps:
            if "==" not in pip_dep and ">=" not in pip_dep and "<=" not in pip_dep:
                result.warn(f"pip package without version pin: {pip_dep}")

    # Check rccPostInstall
    if "rccPostInstall" in data:
        if not isinstance(data["rccPostInstall"], list):
            result.error("'rccPostInstall' must be a list")
        else:
            result.info_msg(f"Post-install scripts: {len(data['rccPostInstall'])}")

    return data


def main():
    parser = argparse.ArgumentParser(
        description="Validate RCC robot configuration"
    )
    parser.add_argument(
        "path",
        nargs="?",
        default="robot.yaml",
        help="Path to robot.yaml (default: ./robot.yaml)"
    )
    parser.add_argument(
        "--quiet", "-q",
        action="store_true",
        help="Only show errors and warnings"
    )

    args = parser.parse_args()
    robot_path = Path(args.path)

    if not robot_path.exists():
        print(f"File not found: {robot_path}")
        return 1

    print(f"Validating: {robot_path}")
    print()

    # Validate robot.yaml
    print("=== robot.yaml ===")
    robot_result = ValidationResult()
    robot_data = validate_robot_yaml(robot_path, robot_result)

    if not args.quiet or not robot_result.is_valid:
        robot_result.print_results()

    if robot_result.is_valid:
        print("  Valid!")
    print()

    # Validate conda.yaml if found
    conda_path = None
    if "condaConfigFile" in robot_data:
        conda_path = robot_path.parent / robot_data["condaConfigFile"]
    elif "environmentConfigs" in robot_data:
        for config in robot_data["environmentConfigs"]:
            config_path = robot_path.parent / config
            if config_path.exists():
                conda_path = config_path
                break

    if conda_path and conda_path.exists():
        print(f"=== {conda_path.name} ===")
        conda_result = ValidationResult()
        validate_conda_yaml(conda_path, conda_result)

        if not args.quiet or not conda_result.is_valid:
            conda_result.print_results()

        if conda_result.is_valid:
            print("  Valid!")
        print()

        if not conda_result.is_valid:
            return 1

    return 0 if robot_result.is_valid else 1


if __name__ == "__main__":
    sys.exit(main())
