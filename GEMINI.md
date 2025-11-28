# Project Overview

**RCC (Repeatable, Contained Code)** is a CLI tool designed to create, manage, and distribute self-contained Python-based automation packages. It ensures reproducibility by isolating environments and freezing dependencies.

*   **Language:** Go (1.20+)
*   **Core Purpose:** Managing Python environments (using `micromamba`), executing automation tasks, and distributing `robot.yaml` based projects.
*   **Key Technologies:**
    *   **CLI Framework:** [Cobra](https://github.com/spf13/cobra)
    *   **Configuration:** [Viper](https://github.com/spf13/viper)
    *   **Environment Management:** Embedded `micromamba`
    *   **Build Automation:** Python `invoke`

## Architecture

*   **`cmd/`**: Contains the entry points for all CLI commands. Each file generally corresponds to a subcommand (e.g., `cmd/run.go` -> `rcc run`). `cmd/root.go` defines the main entry point and global flags.
*   **`common/`**: Shared utilities and core logic used across different commands.
*   **`robot_tests/`**: Acceptance tests written in Robot Framework.
*   **`tasks.py`**: The `invoke` task definitions for building, testing, and maintaining the project.
*   **`assets/`**: Contains static assets and configuration templates.

# Building and Running

The project uses `invoke` (Python) to manage build tasks. Ensure you have Python and `invoke` installed.

## Prerequisites

*   Go 1.20+
*   Python 3.x
*   `pip install invoke`

## Key Commands

*   **Build All Platforms:**
    ```bash
    inv build
    ```
    Builds binaries for Linux, macOS, and Windows in the `build/` directory.

*   **Build Local Version:**
    ```bash
    inv local
    ```
    Builds the binary for your current OS.

*   **Run Unit Tests:**
    ```bash
    inv test
    ```
    Runs standard Go tests (`go test ./...`).

*   **Run Acceptance Tests:**
    ```bash
    inv robot
    ```
    Runs Robot Framework tests located in `robot_tests/`. **Note:** These are primarily designed for Linux.

*   **List All Tasks:**
    ```bash
    inv -l
    ```

# Development Conventions

*   **Command Structure:** New CLI commands should be added to `cmd/` using the Cobra pattern. Register them in the appropriate parent command (usually in `init()` functions).
*   **Testing:**
    *   Unit tests go alongside the code (e.g., `foo_test.go`).
    *   Integration/Acceptance tests go in `robot_tests/`.
*   **Assets:** If you modify `assets/`, run `inv assets` to update the embedded resources.
*   **Versioning:** Version information is stored in `common/version.go`.
*   **Code Style:** Follow standard Go conventions (`gofmt`, `go vet`).

## Active Technologies
- Go 1.20 + `github.com/google/go-containerregistry` (for native OCI image construction) (001-oci-image-build)
- N/A (Output is a file/tarball or registry push) (001-oci-image-build)

## Recent Changes
- 001-oci-image-build: Added Go 1.20 + `github.com/google/go-containerregistry` (for native OCI image construction)
