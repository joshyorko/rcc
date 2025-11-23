# Feature Specification: Robot Bundle Unpack

**Feature Branch**: `001-robot-bundle-unpack`
**Created**: 2025-11-23
**Status**: Draft
**Input**: User description: "need to build robotBundleUnpack package cmd..."

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Unpack Robot Bundle (Priority: P1)

As a user, I want to extract the contents of a robot bundle to a specific directory so that I can inspect or modify the code.

**Why this priority**: This is the core functionality of the feature.

**Independent Test**:
1. Create a valid robot bundle (zip file with `robot/` directory).
2. Run `rcc robot unpack --bundle mybundle.zip --output ./extracted`.
3. Verify that `./extracted` contains the files from the bundle's `robot/` directory.

**Acceptance Scenarios**:

1. **Given** a valid robot bundle `bundle.zip` and a non-existent directory `output`, **When** I run `rcc robot unpack -b bundle.zip -o output`, **Then** the command succeeds and `output` contains the robot files.
2. **Given** a valid robot bundle `bundle.zip` and an existing directory `output`, **When** I run `rcc robot unpack -b bundle.zip -o output`, **Then** the command fails with an error message about the directory existing.
3. **Given** a valid robot bundle `bundle.zip` and an existing directory `output`, **When** I run `rcc robot unpack -b bundle.zip -o output --force`, **Then** the command succeeds and overwrites/updates files in `output`.
4. **Given** an invalid file path for bundle, **When** I run `rcc robot unpack -b missing.zip -o output`, **Then** the command fails with a file not found error.

---

## Clarifications

### Session 2025-11-23
- Q: How should the `--force` flag behave when the output directory exists? → A: **Merge/Overwrite** - Only overwrite files that are present in the bundle. Existing files in the target directory that are NOT in the bundle remain untouched.
- Q: How should symlinks within the bundle be handled during extraction? → A: **Treat as regular files (Content Copy)** - Extract as regular files containing the link target path to ensure cross-platform compatibility (Windows/Linux/macOS).
- Q: What should happen if the bundle does not contain a `robot/` directory? → A: **No (Fail Fast)** - The command must fail immediately with a specific error message, as it indicates an invalid or unexpected bundle structure.

## Functional Requirements

### CLI Command

*   **Command**: `rcc robot unpack`
*   **Short Description**: "Unpack a robot bundle into a directory."
*   **Long Description**: "Unpack a robot bundle into a directory. This command extracts the robot code from the bundle into the specified directory."

### Flags

| Flag | Short | Type | Required | Description |
| :--- | :--- | :--- | :--- | :--- |
| `--bundle` | `-b` | String | Yes | Path to the bundle file. |
| `--output` | `-o` | String | Yes | Output directory. |
| `--force` | `-f` | Bool | No | Overwrite existing directory. |

### Behavior

1.  **Validation**:
    *   Verify `--bundle` argument is provided and file exists.
    *   Verify `--output` argument is provided.
    *   Check if `--output` directory exists. If it does:
        *   If `--force` is NOT set, exit with error.
        *   If `--force` IS set, proceed with extraction (Merge/Overwrite behavior: existing files not in the bundle are preserved).
2.  **Extraction**:
    *   Open the zip file specified by `--bundle`.
    *   **Validation**: Check for existence of `robot/` directory. If missing, fail immediately.
    *   Locate files within the `robot/` directory inside the zip.
    *   Extract these files to the `--output` directory, stripping the `robot/` prefix.
    *   Preserve directory structure and file modes.
    *   **Symlinks**: Treat as regular files containing the link target path (do not create actual symlinks) to avoid cross-platform issues.
3.  **Output**:
    *   Report success or failure to stdout/stderr.
    *   Use standard `rcc` logging/output formatting.

## Success Criteria

*   **Functionality**: Users can successfully extract `robot/` content from bundles.
*   **Safety**: Users are prevented from accidentally overwriting existing directories without explicit intent (`--force`).
*   **Performance**: Unpacking happens reasonably fast (comparable to `unzip`).
*   **UX**: Error messages are clear (e.g., "Output directory exists", "Bundle not found").

## Assumptions

*   The bundle format is a standard ZIP file containing a `robot/` directory (standard RCC bundle format).
*   The `extractRobotTree` logic currently in `cmd/robotRunFromBundle.go` can be reused or refactored for this purpose.
*   The command will be part of the `rcc` binary and follow its existing CLI patterns (Cobra/Viper).
