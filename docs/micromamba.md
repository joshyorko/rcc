# Embedded micromamba

RCC embeds micromamba **2.10.0**, from upstream release build
[`2.10.0-0`](https://github.com/mamba-org/micromamba-releases/releases/tag/2.10.0-0).
The executable is extracted locally; RCC does not download or self-update it
at runtime. The supported targets remain Linux amd64, macOS amd64,
macOS arm64 and Windows amd64.

## Provenance and refresh procedure

`assets/micromamba_version.txt` pins the semantic version.
`blobs/embedded.go` contains the corresponding numeric admission limit.
`tasks.py` binds the exact upstream release build and archive SHA-256 values in
`_get_official_micromamba_fallback`, and executable SHA-256 values in
`MICROMAMBA_BINARY_SHA256`. Release-build suffixes are not runtime versions.

For a pin update:

1. Review the intervening [upstream release notes](https://github.com/mamba-org/mamba/releases).
2. Read the release asset metadata from GitHub's
   `repos/mamba-org/micromamba-releases/releases/tags/<release-build>` API.
   Download all four `.tar.bz2` archives and calculate their SHA-256 values
   independently. Match each to that archive's API `digest` field.
3. Validate the archive members, hash the extracted executable and match it to
   the separate standalone executable's API digest. Do not confuse an archive
   digest, executable digest or checksum-file digest.
4. Update the version asset, admission limit, release build, both sets of
   digests and exact-pin regression expectations together.
5. Run the checked-in toolkit from the repository root:

   ```sh
   rcc run -r developer/toolkit.yaml --dev -t assets
   rcc run -r developer/toolkit.yaml --dev -t unitTests
   rcc run -r developer/toolkit.yaml --dev -t local
   rcc run -r developer/toolkit.yaml --dev -t micromambaAcceptance
   rcc run -r developer/toolkit.yaml -t robot
   ```

Generated gzip assets are reused only when their decompressed executable hash
matches the pin. Stale, corrupt or unrecorded bytes require a verified download.
Compression uses a fixed timestamp and no filename header. Replacement is atomic
per target; a failed download fails the task and does not overwrite the previous
asset. An incomplete asset task is not a qualified build.

`RCC_MICROMAMBA_BASE` overrides the primary **build-time** archive URL, retaining
the same digest requirements and pinned GitHub fallback. A mirror serves
`<base>/<conda-platform>/2.10.0`; runtime endpoint settings do not change the
embedded executable. Archive size limits, member validation and bounded retries
apply to both sources.

## Compatibility checks

- [2.6.0](https://github.com/mamba-org/mamba/releases/tag/2.6.0) enables sharded
  repodata where available and introduces hierarchical package caches.
  [2.7.0](https://github.com/mamba-org/mamba/releases/tag/2.7.0) restores fallback
  to the flat cache. Check reuse with a real pre-upgrade RCC home; do not delete
  caches or alter environment identity to force an upgrade.
- [2.8.0](https://github.com/mamba-org/mamba/releases/tag/2.8.0) rewrites JSON
  output, and [2.9.0](https://github.com/mamba-org/mamba/releases/tag/2.9.0)
  changes logging alongside JSON. RCC's package inventory consumes
  `list --json` in `conda/dependencies.go`. 2.10.0 returns an object containing
  `log_history` and a `packages` array. RCC accepts that wrapper and the legacy
  array while retaining the pip array contract. Missing, null or invalid wrapped
  package lists fail parsing. The native acceptance task checks the actual
  output shape and resulting golden inventory.
- 2.9.0 fixes credential retention for sharded repodata and permissions on
  shared package-cache directories. 2.10.0 includes `cmd.exe` initialization
  and macOS codesign fixes. These are reasons to require native and deployment
  transport checks, not grounds to weaken TLS, proxy or authentication behavior.
- RCC retains its existing `create` flags, strict channel priority, copy mode,
  safety checks, rc-file policy, network configuration and activation scripts.
  Do not change these unless an actual fixture demonstrates an incompatibility.

Executable paths include the semantic version under
`$ROBOCORP_HOME/micromamba/`. Extracting 2.10.0 leaves the 2.5.0 path intact so an
older RCC can continue using its pin. This alone does not prove cache rollback;
exercise both RCC binaries on the same home and compare their resolved prefix,
environment identity and package inventory.

## Native acceptance boundary

`micromambaAcceptance` runs offline download-integrity regressions, executes the
native embedded binary, verifies versioned extraction, then runs a real
conda-forge fixture with pip layering, package JSON, activation, warm reuse and
task exit/output assertions. `RCC_MICROMAMBA_TEST_OUTPUT` selects the Robot
output directory; CI retains its default under `tmp/output/micromamba`.

The warm test uses offline configuration and dead proxies. It is not a network
isolation proof; additionally execute a restored fixture with networking disabled
(for example, a Linux network namespace). Test actual old-home reuse and rollback,
custom CA/mirror/proxy/auth deployments, and Windows long paths separately.

The native CI matrix runs this task on all four supported targets. A configured
job or cross-compiled binary is not a passed native gate. Release acceptance
requires each target's exact candidate SHA, RCC hash, extracted micromamba hash,
test counts, skips and retained evidence. Missing native or compatibility evidence
means NO-GO; merging and publishing require separate owner approval.
