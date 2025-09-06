# Docker Support for RCC

This document describes how to build and use RCC (Robocorp Control Client) in Docker containers.

## Overview

RCC is now containerized with a multi-stage Docker build that:
- Builds RCC from source in a Go build environment
- Creates a minimal Alpine Linux runtime image (~35MB)
- Includes both `rcc` and `rccremote` binaries
- Runs as a non-root user for security

## Building the Docker Image

### Prerequisites
- Docker installed and running
- Python 3.x with invoke installed (for build automation)

### Build Commands

Using the invoke build system (recommended):

```bash
# Build Docker image with default tag 'rcc:latest'
inv docker-build

# Build with custom tag
inv docker-build --tag myregistry/rcc:v1.0.0
```

Using Docker directly:

```bash
# Basic build
docker build -t rcc:latest .

# Build with custom tag and build args
docker build -t myregistry/rcc:v1.0.0 .
```

### Build Process

The Docker build uses a multi-stage approach:

1. **Builder stage** (golang:1.20-alpine):
   - Installs build dependencies (git, python3, pip, curl, gzip, bash)
   - Installs invoke for build automation
   - Prepares RCC assets (micromamba downloads, templates)
   - Compiles RCC for Linux x64

2. **Runtime stage** (alpine:latest):
   - Minimal Alpine Linux base
   - Installs ca-certificates and bash
   - Creates non-root user 'rcc'
   - Copies RCC binaries from builder
   - Sets secure defaults

## Using the Docker Image

### Basic Usage

```bash
# Show help
docker run --rm rcc:latest

# Show version
docker run --rm rcc:latest rcc version

# View documentation
docker run --rm rcc:latest rcc man changelog
```

### Interactive Usage

```bash
# Run RCC interactively
docker run --rm -it rcc:latest bash

# Inside container
rcc --help
rcc version
```

### Working with Projects

When working with RCC projects, you'll need to mount volumes:

```bash
# Mount current directory to work with local robot projects
docker run --rm -it -v $(pwd):/workspace -w /workspace rcc:latest rcc run

# Mount home directory for persistent RCC configuration
docker run --rm -it \
  -v $(pwd):/workspace \
  -v ~/.robocorp:/home/rcc/.robocorp \
  -w /workspace \
  rcc:latest rcc run
```

### Network Access

For RCC operations that require network access (downloading packages, etc.):

```bash
# RCC with network access
docker run --rm -it --network host -v $(pwd):/workspace -w /workspace rcc:latest rcc pull github.com/robocorp/template-python-browser
```

### Production Usage

For production environments, consider:

```bash
# Run as specific user ID
docker run --rm --user $(id -u):$(id -g) -v $(pwd):/workspace -w /workspace rcc:latest rcc run

# Set resource limits
docker run --rm --memory=1g --cpus=2 -v $(pwd):/workspace -w /workspace rcc:latest rcc run
```

## Testing the Docker Image

Use the invoke task to test the built image:

```bash
# Test basic functionality
inv docker-test

# Test with custom tag
inv docker-test --tag myregistry/rcc:v1.0.0
```

Manual testing:

```bash
# Test version
docker run --rm rcc:latest rcc version

# Test help
docker run --rm rcc:latest rcc --help

# Test documentation access
docker run --rm rcc:latest rcc man changelog | head -10
```

## Publishing the Docker Image

### Using invoke (when registry is configured)

```bash
# Push to registry
inv docker-push --tag rcc:latest --registry myregistry.com
```

### Using Docker directly

```bash
# Tag for registry
docker tag rcc:latest myregistry.com/rcc:latest

# Push to registry
docker push myregistry.com/rcc:latest
```

## Docker Compose Example

Create a `docker-compose.yml` for development:

```yaml
version: '3.8'

services:
  rcc:
    image: rcc:latest
    volumes:
      - .:/workspace
      - ~/.robocorp:/home/rcc/.robocorp
    working_dir: /workspace
    command: ["rcc", "--help"]
    user: "1000:1000"  # Adjust to your user ID
```

Run with:

```bash
docker-compose run --rm rcc rcc version
docker-compose run --rm rcc rcc run
```

## Security Considerations

- The container runs as non-root user 'rcc' (UID varies)
- Use `--user` flag to run as specific user ID in production
- Mount only necessary directories
- Consider using read-only mounts for source code
- Use specific image tags (not 'latest') in production

## Troubleshooting

### Build Issues

If the build fails with asset-related errors:

```bash
# Ensure assets are prepared before building
inv assets
docker build -t rcc:latest .
```

### Runtime Issues

If RCC fails to access mounted directories:

```bash
# Check permissions and user mapping
docker run --rm -it rcc:latest ls -la /workspace

# Run as your user ID
docker run --rm --user $(id -u):$(id -g) -v $(pwd):/workspace rcc:latest rcc --help
```

### Network Issues

If RCC cannot download dependencies:

```bash
# Use host networking
docker run --rm --network host rcc:latest rcc pull <package>

# Check DNS resolution
docker run --rm rcc:latest nslookup downloads.robocorp.com
```

## Image Details

- **Base Image**: Alpine Linux (latest)
- **Size**: ~35MB
- **Architecture**: linux/amd64
- **Binaries**: 
  - `/usr/local/bin/rcc` (main CLI)
  - `/usr/local/bin/rccremote` (remote functionality)
- **User**: `rcc` (non-root)
- **Working Directory**: `/home/rcc`

## Integration with CI/CD

Example GitHub Actions usage:

```yaml
- name: Build RCC Docker image
  run: |
    cd rcc
    inv docker-build --tag rcc:${{ github.sha }}

- name: Test RCC Docker image
  run: |
    cd rcc
    inv docker-test --tag rcc:${{ github.sha }}

- name: Run RCC in container
  run: |
    docker run --rm -v ${{ github.workspace }}:/workspace -w /workspace rcc:${{ github.sha }} rcc run
```

For more information about RCC itself, see the main [README.md](README.md) and [documentation](docs/).