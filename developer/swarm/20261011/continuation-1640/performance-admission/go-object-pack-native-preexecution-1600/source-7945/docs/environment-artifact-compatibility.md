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

Workers that are already opted into a machine-wide shared Holotree can select
the private, `ROBOCORP_HOME`-scoped lifecycle explicitly with
`RCC_HOLOTREE_MODE=private`. The default remains the existing user marker, so
ordinary v12 shared-Holotree behavior is unchanged when the variable is absent.

## Linux compatibility semantics

Linux Environment Artifact requirements do not copy the producer's `uname -r`
into an operating-system minimum. Linux has no single distribution product
version that can describe the user-space ABI across supported distributions,
so `os.minimumVersion` is the stable Linux family identity (`1`). The
user-space compatibility boundary is expressed by the recorded libc family and
minimum version, ELF-discovered required shared libraries, Python identity, and
CPU architecture/features.

`os.kernelMinimum` is the conservative RCC runtime floor, currently Linux
`3.15`. RCC's Linux artifact lifecycle uses `renameat2` with
`RENAME_NOREPLACE` for atomic no-replace publication; those capabilities were
introduced in Linux 3.15. A worker below that floor is rejected. A newer
producer kernel is not itself a requirement, so an artifact built on (for
example) Linux `7.1.8` can be acquired by a supported worker on Linux `5.14`
when its other recorded requirements are satisfied.

Architecture, native-only translation policy, libc family/version, required
shared libraries, CPU features, filesystem capabilities, Python identity, and
relocation version remain fail-closed compatibility checks.
