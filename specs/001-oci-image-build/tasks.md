# Tasks: RCC OCI Image Builder

**Feature**: OCI Image Builder
**Status**: Approved
**Spec**: specs/001-oci-image-build/spec.md
**Plan**: specs/001-oci-image-build/plan.md

## Dependencies

1. **Phase 1 (Setup)**: Must be done first.
2. **Phase 2 (Foundational)**: Depends on Phase 1. Blocks all User Stories.
3. **Phase 3 (US1)**: Depends on Phase 2. Core value.
4. **Phase 4 (US2)**: Depends on Phase 3.
5. **Phase 5 (US3)**: Depends on Phase 3.
6. **Phase 6 (US4)**: Depends on Phase 3.
7. **Phase 7 (US5)**: Can be done in parallel with US2-US4, depends on Phase 2.
8. **Phase 8 (Polish)**: Depends on all others.

## Phase 1: Setup
*Goal: Initialize project structure and dependencies.*

- [ ] T001 Add `github.com/google/go-containerregistry` dependency to `go.mod`
- [ ] T002 Create `cmd/oci.go` with `ociCmd` struct and help text
- [ ] T003 Register `ociCmd` in `cmd/oci.go` `init()` function (add to `rootCmd`)
- [ ] T004 Create `common/oci` directory

## Phase 2: Foundational
*Goal: Core data structures and skeletons.*

- [ ] T005 Create `common/oci/image.go` with `OCIBuildPlan` and `BuildManifest` structs
- [ ] T006 Implement helper to resolve Linux RCC binary in `common/oci/image.go`: Check `--rcc-exec` flag; if on Linux use self; if on Windows/Mac download matching version from GitHub to cache.
- [ ] T007 Implement `common/oci/build.go` skeleton with `BuildImage(plan OCIBuildPlan) error`
- [ ] T008 Create `cmd/ociBuild.go` with `ociBuildCmd` skeleton
- [ ] T009 Register `ociBuildCmd` as sub-command of `ociCmd` in `cmd/ociBuild.go`

## Phase 3: User Story 1 - Build OCI Image from Robot
*Goal: Build a basic valid OCI image from a robot.*

- [ ] T010 [US1] Implement robot.yaml/conda.yaml validation in `cmd/ociBuild.go`
- [ ] T010a [US1] [FR-012] Implement comprehensive prerequisite checks with clear error messages (missing files, permissions, disk space, network availability, Linux binary availability on non-Linux hosts)
- [ ] T011 [US1] Implement base image pulling in `common/oci/build.go` using `go-containerregistry`
- [ ] T012 [US1] Implement layer creation for RCC binary in `common/oci/build.go`
- [ ] T013 [US1] Implement layer creation for Robot code (copy `robot.yaml` dir to `/home/robot/app`) in `common/oci/build.go`
- [ ] T014 [US1] Implement layer creation for Holotree environment (copy resolved env) in `common/oci/build.go`
- [ ] T014a [US1] [FR-013] Respect holotree shared/private mode when copying environment in `common/oci/build.go`
- [ ] T014b [US1] [FR-014] Ensure path invariants are maintained for holotree compatibility (use canonical paths in image)
- [ ] T015 [US1] Implement image assembly in `common/oci/build.go`: Entrypoint `["/usr/local/bin/rcc", "run", "--robot", "/home/robot/app/robot.yaml"]`
- [ ] T015a [US1] [FR-016] Add progress output messages during build phases (validation, pulling, layering, assembly, saving)
- [ ] T015b [US1] [FR-018] Pass through CONDA_TOKEN, PIP_INDEX_URL, and other auth env vars during environment resolution
- [ ] T015c [US1] [FR-019] [FR-020] Implement platform compatibility check during environment resolution with warning/error output for incompatible dependencies
- [ ] T016 [US1] Implement saving image to tarball in `common/oci/build.go`
- [ ] T017 [US1] Wire up `cmd/ociBuild.go` to call `BuildImage` with basic plan
- [ ] T018 [US1] Add integration test for building a simple robot image in `cmd/oci_test.go`

## Phase 4: User Story 2 - Custom Image Tagging
*Goal: Support tagging images.*

- [ ] T019 [P] [US2] Update `OCIBuildPlan` to support multiple tags in `common/oci/image.go`
- [ ] T020 [P] [US2] Update `cmd/ociBuild.go` to parse `--tag` flag
- [ ] T021 [US2] Implement tagging logic when saving image in `common/oci/build.go`

## Phase 5: User Story 3 - Specify Base Image
*Goal: Support custom base images.*

- [ ] T022 [P] [US3] Update `cmd/ociBuild.go` to parse `--base-image` flag
- [ ] T023 [US3] Pass base image from plan to `BuildImage` function in `common/oci/build.go`

## Phase 6: User Story 4 - Build with Existing Holotree Catalog
*Goal: Optimize build by using existing catalog.*

- [ ] T024 [P] [US4] Update `cmd/ociBuild.go` to parse `--catalog` flag
- [ ] T025 [US4] Implement logic to use existing catalog in `common/oci/build.go` instead of resolving

## Phase 7: User Story 5 - Generate Dockerfile Without Building
*Goal: Generate Dockerfile for external builds.*

- [ ] T026 [P] [US5] Create `common/oci/dockerfile.go` with `GenerateDockerfile` function
- [ ] T027 [P] [US5] Create `cmd/ociDockerfile.go` command
- [ ] T028 [US5] Implement Dockerfile generation template matching `OCIBuildPlan` in `common/oci/dockerfile.go`

## Phase 8: Polish & Cross-Cutting Concerns
*Goal: Cleanup, error handling, and documentation.*

- [ ] T029 Clean up any temporary files in `common/oci`
- [ ] T030 Add documentation for `rcc oci` command in `docs/`
- [ ] T031 [FR-015] Add cross-platform build detection and warning when host platform differs from target (Linux x86_64)
- [ ] T032 [FR-017] Implement SIGINT/SIGTERM handler for best-effort cleanup of partial artifacts during build

## Implementation Strategy
1. **MVP**: Complete Phases 1, 2, and 3. This delivers the core value of building an image.
2. **Enhancements**: Phases 4, 5, and 6 can be done iteratively.
3. **Integration**: Phase 7 can be done separately as it's a parallel feature.
