# Phase 0: Research

**Feature**: Robot Bundle Unpack
**Date**: 2025-11-23

## Existing Implementation Analysis

### `cmd/robotRunFromBundle.go`
*   Contains `extractRobotTree(zr *zip.Reader, dest string) error`.
*   Logic:
    *   Iterates through zip files.
    *   Checks for `robot/` prefix.
    *   Strips `robot/` prefix.
    *   Performs Zip Slip check (`strings.HasPrefix(rel, "..")`).
    *   Extracts files.
*   **Status**: This function is currently unexported and local to `robotRunFromBundle.go`. It is robust and handles security checks.

### `cmd/robot.go`
*   Defines `robotCmd` which is the parent command for `robot` subcommands.
*   `init()` registers `robotCmd` to `rootCmd`.

## Strategy

1.  **Refactoring**: Move `extractRobotTree` to a shared location. Since both `run-from-bundle` and `unpack` are in the `cmd` package, we can move it to a new file `cmd/robot_bundle_utils.go` (or similar) and make it available to other files in the same package. It doesn't strictly need to be exported (Capitalized) if it stays in `package cmd`, but exporting it `ExtractRobotTree` might be cleaner if we ever move it out. For now, keeping it unexported `extractRobotTree` but in a shared file is sufficient for package-level access.
2.  **New Command**: Create `cmd/robotUnpack.go`.
    *   Define `unpackCmd`.
    *   Flags: `--bundle`, `--output`, `--force`.
    *   Logic:
        *   Validate flags.
        *   Check output directory existence.
        *   Open zip.
        *   Call `extractRobotTree`.

## Risks & Mitigations

*   **Risk**: Zip Slip vulnerability if we rewrite extraction logic.
    *   **Mitigation**: Reuse the existing, tested `extractRobotTree` logic.
*   **Risk**: Windows path issues.
    *   **Mitigation**: Existing logic uses `filepath.FromSlash` (implicitly via `filepath.Join` and `filepath.Clean`) and `filepath.ToSlash` for zip entries. We must ensure this is preserved.

## Conclusion

The implementation is straightforward. The primary task is refactoring the extraction logic to be reusable and then wrapping it in a new Cobra command.
