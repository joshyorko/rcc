# Implementation Plan: OCI Image Builder

**Branch**: `001-oci-image-build` | **Date**: 2025-11-28 | **Spec**: [specs/001-oci-image-build/spec.md](specs/001-oci-image-build/spec.md)
**Input**: Feature specification from `/specs/001-oci-image-build/spec.md`

**Note**: This template is filled in by the `/speckit.plan` command. See `.specify/templates/commands/plan.md` for the execution workflow.

## Summary

Implement a native OCI image builder within RCC (`rcc oci build`) that allows users to package their robot automation, a resolved Holotree environment, and the RCC runtime into a single, self-contained container image. This process must not require an external container runtime (like Docker) to be present on the build machine. The feature also includes generating Dockerfiles (`rcc oci dockerfile`) for integration with standard build pipelines.

## Technical Context

<!--
  ACTION REQUIRED: Replace the content in this section with the technical details
  for the project. The structure here is presented in advisory capacity to guide
  the iteration process.
-->

**Language/Version**: Go 1.20
**Primary Dependencies**: `github.com/google/go-containerregistry` (for native OCI image construction)
**Storage**: N/A (Output is a file/tarball or registry push)
**Testing**: Go `testing` package (Unit), Robot Framework (Acceptance - requires container runtime on test host)
**Target Platform**: The *tool* runs on Linux, Windows, macOS. The *output image* is Linux x86_64.
**Linux Binary Strategy**: On Linux, use the current running binary. On Windows/macOS, attempt to download the matching version of `rcc` for Linux x86_64 from GitHub releases, or accept a specific path via `--rcc-exec`.
**Image Layout**:
*   Workdir: `/home/robot/app` (Robot code)
*   Binary: `/usr/local/bin/rcc`
*   Entrypoint: `["/usr/local/bin/rcc", "run", "--robot", "/home/robot/app/robot.yaml"]`
**Project Type**: CLI Subcommand (`rcc oci ...`)
**Performance Goals**: Build time < 5 mins; Minimal overhead (< 100MB)
**Constraints**: Must work without Docker installed. Must support "offline" building if assets are cached.
**Scale/Scope**: Handles single robot packages.

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

*   **I. Environment Isolation & Reproducibility**: ✅ **PASS**. The core goal is to package the reproducible Holotree environment into an immutable image.
*   **II. Cross-Platform Compatibility**: ✅ **PASS**. The `rcc oci` command must work on Windows, Mac, and Linux. While the *output* is a Linux image (standard for server-side automation), the *build process* is cross-platform.
*   **III. Build Patience & Stability**: ✅ **PASS**. Image building is a long-running process. We will use `common.Timeline` and `common.Log` to provide feedback and ensure timeouts are generous (e.g., 15+ mins for large envs).
*   **V. CLI-First**: ✅ **PASS**. Implemented as `rcc oci` subcommand.
*   **VI. Privacy & Telemetry**: ✅ **PASS**. No new telemetry added.

## Project Structure

### Documentation (this feature)

```text
specs/001-oci-image-build/
├── plan.md              # This file (/speckit.plan command output)
├── research.md          # Phase 0 output (/speckit.plan command)
├── data-model.md        # Phase 1 output (/speckit.plan command)
├── quickstart.md        # Phase 1 output (/speckit.plan command)
├── contracts/           # Phase 1 output (/speckit.plan command)
└── tasks.md             # Phase 2 output (/speckit.tasks command - NOT created by /speckit.plan)
```

### Source Code (repository root)
<!--
  ACTION REQUIRED: Replace the placeholder tree below with the concrete layout
  for this feature. Delete unused options and expand the chosen structure with
  real paths (e.g., apps/admin, packages/something). The delivered plan must
  not include Option labels.
-->

```text
cmd/
├── oci.go              # Entry point for 'rcc oci' parent command
├── ociBuild.go         # Implementation of 'rcc oci build'
└── ociDockerfile.go    # Implementation of 'rcc oci dockerfile'

common/
└── oci/
    ├── build.go        # Core logic for assembling OCI images
    ├── dockerfile.go   # Core logic for generating Dockerfiles
    └── image.go        # Image configuration and layer management
```

**Structure Decision**: We will add a new `oci` subcommand to `cmd/` following the existing pattern (e.g., `cmd/holotree.go`). The core logic will be encapsulated in a new package `common/oci` to separate CLI concerns from image building logic.

## Complexity Tracking

> **Fill ONLY if Constitution Check has violations that must be justified**

| Violation | Why Needed | Simpler Alternative Rejected Because |
|-----------|------------|-------------------------------------|
| N/A | | |
