# Feature Specification: RCC OCI Image Builder

**Feature Branch**: `001-oci-image-build`
**Created**: 2025-11-27
**Status**: Draft
**Input**: User description: "Build OCI images containing Holotree environments, robot packages, and RCC runtime for self-contained container deployment"

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Build OCI Image from Robot (Priority: P1)

A developer wants to package their robot automation into a portable container image. They have a working robot with `robot.yaml` and `conda.yaml` that runs successfully via `rcc run`. They want to create an OCI image that bundles everything needed to run the robot without requiring RCC installation on the target system.

**Why this priority**: This is the core use case - converting a working robot into a deployable container. Without this, no other functionality matters.

**Independent Test**: Can be fully tested by running `rcc oci build -r robot.yaml` and verifying the resulting image can execute the robot in a fresh container runtime.

**Acceptance Scenarios**:

1. **Given** a directory with valid `robot.yaml` and `conda.yaml`, **When** user runs `rcc oci build -r robot.yaml`, **Then** an OCI image is created containing the robot code, resolved Holotree environment, and RCC binary
2. **Given** a robot with dependencies in `conda.yaml`, **When** user builds an OCI image, **Then** the image contains a pre-built Holotree catalog with all dependencies resolved
3. **Given** a successfully built image, **When** user runs `podman run <image>`, **Then** the robot executes using the embedded RCC and Holotree environment

---

### User Story 2 - Custom Image Tagging (Priority: P2)

A developer wants to tag their built images with meaningful names and versions for use in container registries and deployment pipelines.

**Why this priority**: Production deployments require proper image naming for registry management, versioning, and rollback capabilities.

**Independent Test**: Can be tested by building an image with custom tag and verifying it appears with that name in local image storage.

**Acceptance Scenarios**:

1. **Given** a robot directory, **When** user runs `rcc oci build -r robot.yaml -t myorg/myrobot:v1.0`, **Then** the image is tagged with `myorg/myrobot:v1.0`
2. **Given** no tag specified, **When** user builds an image, **Then** a sensible default tag is generated based on robot name and timestamp
3. **Given** a tag with registry prefix, **When** build completes, **Then** the image is ready for `push` commands to that registry

---

### User Story 3 - Specify Base Image (Priority: P2)

A developer needs to use a specific base image to meet organizational security requirements, compliance policies, or to include additional system dependencies.

**Why this priority**: Enterprise environments often mandate specific base images for security scanning, compliance, or organizational standards.

**Independent Test**: Can be tested by building with `--base-image` flag and inspecting the resulting image layers to confirm the correct base was used.

**Acceptance Scenarios**:

1. **Given** a robot directory, **When** user runs `rcc oci build --base-image registry.example.com/approved-base:latest`, **Then** the image uses the specified base
2. **Given** no base image specified, **When** user builds, **Then** a reasonable default Linux base image is used (e.g., minimal Debian or Alpine)
3. **Given** an invalid or inaccessible base image, **When** build is attempted, **Then** user receives clear error message explaining the issue

---

### User Story 4 - Build with Existing Holotree Catalog (Priority: P3)

A developer has already resolved their environment and wants to reuse an existing Holotree catalog rather than rebuilding from scratch during image creation.

**Why this priority**: Speeds up iterative development and ensures consistency with locally tested environments.

**Independent Test**: Can be tested by providing a catalog hash/path and verifying build time is significantly reduced compared to fresh resolution.

**Acceptance Scenarios**:

1. **Given** an existing Holotree catalog ID, **When** user runs `rcc oci build --catalog <id>`, **Then** the image is built using that pre-existing catalog
2. **Given** a catalog that doesn't exist, **When** build is attempted, **Then** user is informed and option to resolve fresh is offered
3. **Given** catalog and robot.yaml with matching environment, **When** build completes, **Then** image size is minimized by avoiding duplicate environment resolution

---

### User Story 5 - Generate Dockerfile Without Building (Priority: P3)

A developer wants to integrate RCC image building into an existing CI/CD pipeline that uses standard container build tools (Docker, Buildah, Kaniko).

**Why this priority**: Supports integration with existing build infrastructure without requiring RCC to perform the actual image build.

**Independent Test**: Can be tested by generating Dockerfile and successfully building it with standard `docker build` or `podman build`.

**Acceptance Scenarios**:

1. **Given** a robot directory, **When** user runs `rcc oci dockerfile -r robot.yaml`, **Then** a valid Dockerfile is generated that can be used with standard build tools
2. **Given** generated Dockerfile, **When** user runs `docker build .`, **Then** the build succeeds and produces equivalent image to `rcc oci build`
3. **Given** custom base image requirement, **When** dockerfile is generated with `--base-image` flag, **Then** the Dockerfile FROM clause uses that image

---

### Edge Cases

- What happens when the robot has platform-specific dependencies that conflict with the target container platform (e.g., Windows-only packages)?
- How does the system handle very large Holotree environments (multi-GB) during image creation?
- What happens if the build is interrupted mid-way through environment resolution?
- How does the system behave when building on a host platform different from the target image platform?
- What happens when the user doesn't have a container runtime installed?
- How does the system handle robots with private package dependencies requiring authentication?

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: System MUST create valid OCI-compliant container images from robot packages
- **FR-002**: System MUST embed a statically-linked RCC binary in the generated image
- **FR-003**: System MUST include pre-resolved Holotree catalog in the image so no runtime environment resolution is needed
- **FR-004**: System MUST copy robot source code to a well-defined location in the image
- **FR-005**: System MUST configure the container entrypoint to execute the robot via embedded RCC
- **FR-006**: System MUST support user-specified image tags following OCI naming conventions
- **FR-007**: System MUST support user-specified base images for the container
- **FR-008**: System MUST provide a default base image when none is specified
- **FR-009**: System MUST support generating standalone Dockerfile for external build tools
- **FR-010**: System MUST accept existing Holotree catalog references to avoid redundant environment resolution
- **FR-011**: System MUST validate robot.yaml and conda.yaml before attempting build
- **FR-012**: System MUST provide clear error messages when build prerequisites are missing
- **FR-013**: System MUST respect existing holotree shared/private mode configurations
- **FR-014**: System MUST ensure path invariants are maintained for holotree environment compatibility
- **FR-015**: System MUST detect and warn when cross-platform builds may produce incompatible environments

### Key Entities

- **Robot Package**: The user's automation code including `robot.yaml`, `conda.yaml`, and associated source files
- **Holotree Catalog**: The resolved Python environment with all dependencies, ready for execution
- **OCI Image**: The output container image following OCI specification, containing RCC, catalog, and robot
- **Base Image**: The foundation Linux image upon which the robot image is built
- **Image Tag**: The name:version identifier for the built image

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: Users can build a container image from a robot in under 5 minutes (excluding initial environment resolution time)
- **SC-002**: Built images execute robots identically to running `rcc run` on the host system
- **SC-003**: 100% of valid robot packages that run via `rcc run` can be successfully containerized
- **SC-004**: Generated images are portable across any OCI-compliant container runtime (Docker, Podman, containerd)
- **SC-005**: Users can integrate RCC OCI builds into CI/CD pipelines without requiring container runtime on build agents (via Dockerfile generation)
- **SC-006**: Built images start and execute robot tasks within 10 seconds of container launch (excluding robot task runtime)
- **SC-007**: Image size overhead from RCC and Holotree infrastructure is under 100MB beyond the environment dependencies themselves

## Assumptions

- Users have a functioning container runtime (Docker or Podman) installed when using direct build (not required for Dockerfile generation)
- Target deployment environments support OCI-compliant container images
- The RCC binary can be statically compiled for Linux x86_64 (primary target platform)
- Holotree environments resolved on Linux are used for Linux container images (no cross-platform environment resolution)
- Users understand basic container concepts (images, tags, registries)
- Robot packages follow standard RCC conventions with valid robot.yaml and conda.yaml files

## Scope Boundaries

### In Scope

- Building OCI images from robot packages
- Embedding RCC binary in images
- Including pre-resolved Holotree environments
- Custom base image specification
- Custom image tagging
- Dockerfile generation for external build tools
- Linux x86_64 container images

### Out of Scope

- Multi-architecture image builds (arm64, etc.) - may be added later
- Direct push to container registries (users can use standard tools)
- Windows container images
- Image signing and security scanning (users can use external tools)
- Kubernetes-specific configurations (deployments, services, etc.)
- Runtime configuration injection beyond environment variables
