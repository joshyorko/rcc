# RCC Deployment Patterns

Enterprise-ready deployment options for RCC automations.

## RCC Remote Server

Share holotree environments across machines without rebuilding.

### Quick Setup

```bash
# Clone the deployment repository
git clone https://github.com/joshyorko/rccremote-docker.git
cd rccremote-docker

# Local development
make quick-dev
export RCC_REMOTE_ORIGIN=https://localhost:8443
rcc holotree catalogs

# Cloudflare Tunnel (public access)
make quick-cf HOSTNAME=rccremote.yourdomain.com
export RCC_REMOTE_ORIGIN=https://rccremote.yourdomain.com

# Production with signed certs
make certs-signed SERVER_NAME=your-domain.com
make prod-up SERVER_NAME=your-domain.com
```

### Kubernetes Deployment

```bash
make quick-k8s
```

### Adding Robots to RCC Remote

**Method 1: Robot Directories**
```bash
data/robots/
├── my-robot/
│   ├── robot.yaml
│   └── conda.yaml
make dev-restart
```

**Method 2: Pre-built ZIP Catalogs**
```bash
# Export from build machine
rcc holotree export -r robot.yaml -z my-robot.zip

# Import to server
cp my-robot.zip data/hololib_zip/
make dev-restart
```

### Client Configuration

```bash
# Set remote origin
export RCC_REMOTE_ORIGIN=https://rccremote.yourdomain.com

# Pull environment from remote
rcc holotree pull

# List available catalogs
rcc holotree catalogs
```

## Docker Deployment

### Base Dockerfile

```dockerfile
# Stage 1: Download binaries
FROM alpine:latest AS downloader
RUN apk add --no-cache wget ca-certificates
WORKDIR /tmp
RUN wget https://github.com/joshyorko/rcc/releases/download/v18.8.0/rcc-linux64 -O rcc && \
    chmod +x rcc

# Stage 2: Runtime
FROM ubuntu:22.04
ENV LANG=C.UTF-8 DEBIAN_FRONTEND=noninteractive

RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates bash && \
    rm -rf /var/lib/apt/lists/*

COPY --from=downloader /tmp/rcc /usr/local/bin/rcc
COPY robot.yaml conda.yaml /robot/
COPY *.py /robot/

WORKDIR /robot

# Pre-build environment during image build
RUN rcc ht vars -r robot.yaml

ENTRYPOINT ["rcc", "run", "-r", "robot.yaml"]
```

### Multi-Robot Dockerfile

```dockerfile
FROM ubuntu:22.04 AS base
ENV LANG=C.UTF-8 DEBIAN_FRONTEND=noninteractive

RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates curl bash && \
    rm -rf /var/lib/apt/lists/*

# Install RCC
RUN curl -o /usr/local/bin/rcc \
    https://github.com/joshyorko/rcc/releases/latest/download/rcc-linux64 && \
    chmod +x /usr/local/bin/rcc

# Copy robots
COPY robots/ /robots/

# Pre-build all environments
RUN for robot in /robots/*/robot.yaml; do \
    rcc ht vars -r "$robot" || true; \
    done

WORKDIR /robots
ENTRYPOINT ["/bin/bash"]
```

### Docker Compose

```yaml
version: '3.8'

services:
  robot-processor:
    build:
      context: .
      dockerfile: Dockerfile
    volumes:
      - ./input:/robot/input:ro
      - ./output:/robot/output
    environment:
      - TESTING=false
      - LOG_LEVEL=INFO
    restart: unless-stopped

  robot-scheduler:
    build:
      context: .
      dockerfile: Dockerfile
    command: ["--task", "Scheduler"]
    volumes:
      - ./config:/robot/config:ro
    depends_on:
      - robot-processor
```

## Holotree Management

### Export Environment

```bash
# Export current environment as catalog
rcc holotree export -r robot.yaml -z environment.zip

# Export specific catalog by hash
rcc ht export -c abc123def456 -o env-catalog.zip
```

### Import Environment

```bash
# Import on another machine
rcc holotree import environment.zip

# Verify import
rcc holotree list
```

### Shared Holotree (Multi-User)

```bash
# Enable shared holotree (requires admin/sudo)
# Windows:
rcc holotree shared --enable

# macOS/Linux:
sudo rcc holotree shared --enable

# Initialize user for shared access
rcc holotree init

# Shared locations:
# - Windows: C:\ProgramData\robocorp
# - macOS: /Users/Shared/robocorp
# - Linux: /opt/robocorp
```

## CI/CD Integration

### GitHub Actions

```yaml
name: Build and Deploy Robot

on:
  push:
    branches: [main]

jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - name: Install RCC
        run: |
          curl -o rcc https://github.com/joshyorko/rcc/releases/latest/download/rcc-linux64
          chmod +x rcc && sudo mv rcc /usr/local/bin/

      - name: Cache holotree
        uses: actions/cache@v4
        with:
          path: ~/.robocorp
          key: holotree-${{ hashFiles('conda.yaml') }}

      - name: Build environment
        run: rcc ht vars -r robot.yaml

      - name: Run tests
        run: rcc run --task Test

      - name: Export catalog
        run: rcc ht export -r robot.yaml -z robot-catalog.zip

      - name: Upload artifact
        uses: actions/upload-artifact@v4
        with:
          name: robot-catalog
          path: robot-catalog.zip

  deploy:
    needs: build
    runs-on: ubuntu-latest
    steps:
      - name: Download artifact
        uses: actions/download-artifact@v4
        with:
          name: robot-catalog

      - name: Deploy to RCC Remote
        run: |
          scp robot-catalog.zip ${{ secrets.RCC_SERVER }}:/data/hololib_zip/
```

### GitLab CI

```yaml
stages:
  - build
  - test
  - deploy

variables:
  RCC_VERSION: "18.8.0"

.rcc-setup: &rcc-setup
  before_script:
    - curl -o /usr/local/bin/rcc https://github.com/joshyorko/rcc/releases/download/v${RCC_VERSION}/rcc-linux64
    - chmod +x /usr/local/bin/rcc

build:
  stage: build
  <<: *rcc-setup
  script:
    - rcc ht vars -r robot.yaml
  cache:
    key: holotree-$CI_COMMIT_REF_SLUG
    paths:
      - ~/.robocorp/

test:
  stage: test
  <<: *rcc-setup
  script:
    - rcc run --task Test

deploy:
  stage: deploy
  <<: *rcc-setup
  script:
    - rcc ht export -r robot.yaml -z robot.zip
    - curl -X POST -F "file=@robot.zip" $RCC_REMOTE_UPLOAD_URL
  only:
    - main
```

## Self-Contained Bundles

Create portable executables:

```bash
# Create bundle (includes code + environment reference)
rcc robot bundle --robot robot.yaml --output my-robot.py

# Run bundle on any machine with RCC
rcc robot run-from-bundle my-robot.py --task Main

# Direct execution (shows usage)
chmod +x my-robot.py
./my-robot.py
```

## Environment Variables for Deployment

```bash
# Core settings
export ROBOCORP_HOME=/opt/robocorp
export RCC_VERBOSITY=silent

# Custom endpoints (air-gapped)
export RCC_ENDPOINT_PYPI="https://pypi.internal.com/simple/"
export RCC_ENDPOINT_CONDA="https://conda.internal.com/"

# Remote server
export RCC_REMOTE_ORIGIN="https://rccremote.company.com"

# Disable telemetry
export RCC_NO_TRACKING=1
```

## Production Checklist

1. **Freeze Dependencies**: Use `environmentConfigs` with freeze files
2. **Pre-build Environments**: Run `rcc ht vars` in CI before deployment
3. **Export Catalogs**: Share pre-built environments via RCC Remote
4. **Monitor Disk Space**: Holotree can grow large
5. **Clean Up**: Periodically run `rcc holotree delete --all` for old envs
6. **Health Checks**: Use `rcc configure diagnostics` in monitoring
7. **Logging**: Set `RCC_VERBOSITY=debug` for troubleshooting
