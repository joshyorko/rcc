# Research: OCI Image Building in Go

## Decision: Use `github.com/google/go-containerregistry`

We will use the `github.com/google/go-containerregistry` library (specifically `pkg/v1`, `pkg/v1/mutate`, `pkg/v1/remote`, and `pkg/v1/tarball`) to implement the native OCI image building functionality.

### Rationale

1.  **Native & Daemonless**: The library is designed to manipulate OCI images purely in Go without requiring a container runtime daemon (like Docker or Podman) or external binaries. This directly satisfies **FR-001** and **Assumption 1**.
2.  **Layer Composition**: It provides a clean API (`mutate.AppendLayers`) to construct images by layering filesystems. This is perfect for our use case:
    *   Base Layer: From upstream (e.g., `debian:slim`).
    *   Layer 1: The `rcc` binary.
    *   Layer 2: The resolved Holotree environment (Catalog).
    *   Layer 3: The Robot source code.
3.  **Standard Compliance**: It produces strictly compliant OCI images that work with all runtimes.
4.  **Ecosystem Adoption**: It is the underlying engine for tools like `ko`, `jib`, and `kaniko`, ensuring maturity and stability.

### Alternatives Considered

*   **`github.com/openshift/imagebuilder`**:
    *   *Pros*: Can parse and execute Dockerfiles.
    *   *Cons*: Designed more for "building from source/Dockerfile" semantics. We primarily want to "assemble" an image from already-resolved artifacts on disk. `go-containerregistry` gives us finer control over this assembly process without the overhead of Dockerfile parsing logic for the native build path.
*   **`os/exec` calling `docker build` or `buildah`**:
    *   *Pros*: Trivial implementation.
    *   *Cons*: Violates **FR-001** (No external dependency). Requires user to have tools installed. Breaks the "self-contained" value prop of RCC.

## Implementation Strategy

### 1. Base Image Retrieval
We will use `remote.Image()` to fetch the base image configuration and layers from a registry.
*   *Challenge*: Authentication. We need to support public images (Debian) easily. `go-containerregistry` handles standard auth flows (docker config, etc.).

### 2. Layer Creation
We need to convert local directories (Holotree catalog, Robot path) into `v1.Layer` objects.
*   The library provides helpers to tar up a directory and compute the hash.
*   *Optimization*: Holotree catalogs are immutable. We can compute the layer hash once and potentially cache it? (Maybe V2). For now, fast tar-stream creation is sufficient.

### 3. Image Assembly
```go
// Pseudo-code
base, _ := remote.Image(ref)
rccLayer, _ := layerFromBin(rccPath)
holoLayer, _ := layerFromDir(catalogPath)
robotLayer, _ := layerFromDir(robotPath)

newImage, _ := mutate.AppendLayers(base, rccLayer, holoLayer, robotLayer)

// Set Entrypoint
cfg, _ := newImage.ConfigFile()
cfg.Config.Entrypoint = []string{"/usr/local/bin/rcc", "run", ...}
newImage, _ = mutate.Config(newImage, cfg.Config)
```

### 4. Output
We will support:
*   Saving to a tarball (`tarball.Write`) which can be loaded via `docker load` or `podman load`.
*   (Future/Optional) Pushing directly to registry (`remote.Write`). The spec focuses on "building", but "pushing" is a natural extension. For V1, we stick to local artifacts.

## Unknowns Resolved
*   **Q**: How to handle the `rcc` binary?
    *   **A**: We will assume the `rcc` binary used to *run* the build command is the one to embed, OR we require a specific `rcc` build artifact.
    *   *Refinement*: Since `rcc` is running the build, it can copy *itself* (`os.Executable()`) into the image, or (better for cross-compilation) look for a `linux/amd64` binary in `build/`. Since the target is always Linux, if we are running on Windows, we *cannot* copy `os.Executable()`. We must ensure `rcc` has access to a Linux binary of itself.
    *   *Resolution*: The `rcc oci build` command should probably require a path to the linux `rcc` binary if running on non-Linux, OR we just bundle it if available. Alternatively, we download the matching version from GitHub Releases if missing.
    *   *Decision*: For V1, we will verify `rcc` is linux-compatible or require a flag `--rcc-binary` if cross-building. If running on Linux, use self.

