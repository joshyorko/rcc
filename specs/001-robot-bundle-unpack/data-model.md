# Data Model: Robot Bundle Unpack

**Feature**: Robot Bundle Unpack
**Date**: 2025-11-23

## Overview

This feature does not introduce new persistent data models or database schemas. It operates on existing file formats (Zip archives) and the filesystem.

## File Formats

### Robot Bundle (Input)
*   **Format**: ZIP archive.
*   **Structure**:
    *   `robot/`: Root directory for robot code.
    *   `robot/robot.yaml`: Configuration file (expected but not strictly validated by unpack command, it just extracts).
    *   Other files under `robot/`.

### Output Directory
*   **Structure**: Mirror of the `robot/` directory content from the bundle.
