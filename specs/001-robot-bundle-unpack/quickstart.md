# Quickstart: Robot Bundle Unpack

## Overview

The `rcc robot unpack` command allows you to extract the contents of a robot bundle (zip file) to a local directory. This is useful for inspecting the code inside a bundle or preparing it for modification.

## Usage

### Basic Unpack

Extract a bundle to a new directory:

```bash
rcc robot unpack --bundle my-robot.zip --output ./my-robot-code
```

### Overwrite Existing Directory

If the output directory already exists, use the `--force` flag to merge/overwrite:

```bash
rcc robot unpack --bundle my-robot.zip --output ./existing-dir --force
```

## Common Errors

*   **"Output directory exists"**: You are trying to unpack into an existing directory without `--force`.
*   **"Bundle file does not exist"**: The path provided to `--bundle` is incorrect.
*   **"no robot/ directory found in bundle"**: The zip file is not a valid RCC robot bundle.
