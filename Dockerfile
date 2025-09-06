# Multi-stage Docker build for RCC (Robocorp Control Client)
# 
# Build stage: Compile RCC from source with all dependencies
# Runtime stage: Minimal image with just the RCC binary

FROM golang:1.20-alpine AS builder

# Install required tools for building
RUN apk add --no-cache \
    git \
    python3 \
    py3-pip \
    curl \
    gzip \
    bash

# Set working directory
WORKDIR /build

# Copy source code
COPY . .

# Install Python build dependencies (using --break-system-packages for Docker build)
RUN python3 -m pip install --break-system-packages invoke

# Prepare assets required for build (micromamba downloads and template zips)
RUN inv assets

# Build RCC binary for Linux
RUN inv linux64

# Runtime stage - minimal Alpine image
FROM alpine:latest

# Install runtime dependencies
RUN apk add --no-cache \
    ca-certificates \
    bash

# Create non-root user for security
RUN addgroup -S rcc && adduser -S rcc -G rcc

# Copy the RCC binary from builder
COPY --from=builder /build/build/linux64/rcc /usr/local/bin/rcc
COPY --from=builder /build/build/linux64/rccremote /usr/local/bin/rccremote

# Make binaries executable
RUN chmod +x /usr/local/bin/rcc /usr/local/bin/rccremote

# Switch to non-root user
USER rcc
WORKDIR /home/rcc

# Set default command
CMD ["rcc", "--help"]

# Metadata
LABEL maintainer="RCC Team"
LABEL description="RCC (Robocorp Control Client) - Create, manage, and distribute Python-based automation packages"
LABEL version="latest"