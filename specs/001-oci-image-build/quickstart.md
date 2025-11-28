# Quickstart: Building OCI Images with RCC

This guide shows you how to turn your robot automation into a container image.

## Prerequisites

*   RCC 11.x+ installed
*   A robot project with `robot.yaml`

## 1. Build an Image (Native)

This creates a container image file (`robot-image.tar`) without needing Docker installed.

```bash
# In your robot directory
rcc oci build -r robot.yaml -t my-robot:v1
```

**Load into Docker/Podman:**
If you want to run it:

```bash
podman load -i image.tar
podman run my-robot:v1
```

## 2. Custom Base Image

Need a specific OS version?

```bash
rcc oci build --base-image ubuntu:22.04
```

## 3. Generate Dockerfile

If you prefer using your own build pipeline:

```bash
rcc oci dockerfile > Dockerfile
docker build -t my-robot .
```
