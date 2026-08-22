# Environment Artifact v1 compatibility

Environment Artifacts are platform-specific. A worker must match the
manifest's operating system, architecture, RCC catalog platform, and v12
gzip/SHA-256 reader contract before it downloads or materializes content.

The released RCC platform matrix is:

| RCC platform | OS | Architecture | A→B runtime proof |
| --- | --- | --- | --- |
| `linux_amd64` | Linux | amd64 | supported |
| `darwin_amd64` | macOS | amd64 | supported |
| `darwin_arm64` | macOS | arm64 | supported |
| `windows_amd64` | Windows | amd64 | supported |

`GOOS/GOARCH` equality is not, by itself, an artifact compatibility check.
The manifest also binds the catalog reader, storage encoding, relocation
features, and builder compatibility key into its immutable identity. An
unsupported platform or a platform mismatch is rejected before provider
objects are fetched, with the worker and artifact platform named in the
diagnostic.
