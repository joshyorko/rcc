# CLI Contract: rcc oci

This document defines the command-line interface for the OCI image builder features.

## 1. Build Image

**Command**: `rcc oci build`

**Description**: Builds an OCI-compliant container image containing the robot and its environment.

**Flags**:
*   `-r, --robot <file>`: Path to `robot.yaml`. Default: `robot.yaml` in current dir.
*   `-t, --tag <name>`: Image tag (can be specified multiple times). Default: `rcc-robot:<timestamp>`
*   `--base-image <image>`: Base image to use. Default: `debian:slim`.
*   `--save <path>`: Path to save the output tarball. Default: `image.tar`.
*   `--catalog <hash>`: (Optional) Use an existing Holotree catalog hash. If not provided, resolves environment from `conda.yaml`.
*   `--rcc-exec <path>`: (Optional) Path to the `rcc` linux binary to embed. Default: looks in standard build locations or downloads.
*   `--push`: (Future) Push to registry. (Not in V1).

**Output (Success)**:
```text
Resolving environment... OK
Pulling base image debian:slim... OK
Assembling layers:
  - Base Image: 25MB
  - RCC Binary: 15MB
  - Holotree Env: 150MB
  - Robot Code: 2MB
Saving to image.tar... OK
Build complete!
Digest: sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855
```

**Output (Error)**:
```text
Error: failed to resolve environment: verify conda.yaml
```

## 2. Generate Dockerfile

**Command**: `rcc oci dockerfile`

**Description**: Prints a valid Dockerfile to stdout that replicates the build process using standard tools.

**Flags**:
*   `-r, --robot <file>`: Path to `robot.yaml`.
*   `--base-image <image>`: Base image to use.

**Output**:
```dockerfile
FROM debian:slim
COPY rcc /bin/rcc
...
ENTRYPOINT ["/bin/rcc"]
```
