# Experimental package providers

Both environment formats require the scalar downgrade guard below. RCC v18.19.12 rejects this scalar before solving. Existing manifests retain their legacy decoder and blueprint representation.

```yaml
dependencies: rcc-provider-plan-v1
rcc:
  schema-version: 1
  layers:
    - provider: rattler
      channels: [local]
      packages: [python=3.12.15, pip=25.0.1, zlib=1.3.2]
    - provider: pip
      packages: [requests==2.32.5]
```

The first provider owns the Conda prefix. Later pip or uv layers install into staged Python. Micromamba uses 2.10.0. Pip and uv require installed versions 25.0.1 and 0.9.26 respectively. A different installed version stops installation. RCC takes cumulative snapshots before subsequent layers and runs post-install last. The artifact builder and v12 reader retain their existing names.

The Rattler 0.1.0 protocol accepts the RCC local channel only. Build the Rust helper with the pinned toolchain and Cargo.lock, place it next to the candidate RCC and compile RCC with the linker variable `github.com/joshyorko/rcc/conda.rattlerHelperSHA256` set to that helper's SHA-256. An ordinary build has no digest. Installation verifies a private executable copy before use. The helper requires SHA-256 package records and disables hard links, copy-on-write links and link scripts. Conda activation and golden inventory currently use embedded micromamba. The release pipeline does not distribute a helper.

The equivalent Actions shorthand keeps the dependency mapping inside the guarded section. RCC supports the syntax directly. Actions' current package compiler and executable admission require downstream changes before use.

```yaml
dependencies: rcc-provider-plan-v1
rcc:
  schema-version: 1
  package-managers: {conda: rattler, pypi: pip}
  channels: [local]
  dependencies:
    conda-forge: [python=3.12.15, pip=25.0.1, zlib=1.3.2]
    pypi: [requests==2.32.5]
```

## Qualification boundaries

Linux amd64 local-channel builds ran Python 3.12.15 and zlib 1.3.2, imported the test wheel through pip and uv, and executed OpenSSL 3.6.5. A separate consumer home with only RCC acquired and executed an exported artifact under permissive-local trust. The intermediate artifact digest was `sha256:d40a2d17e91e1614ee83ae33a8f79d0dfdc67c7667866ac1ac7c95954f8ed776`. Strict trust rejected the unsigned archive. The run used offline flags without network isolation.

Remote Rattler channels, enterprise transport, explicit UV-owned runtime, installer file-overlap enforcement, cross-platform runtime parity, positive signed artifact qualification and performance comparisons remain release blockers. See [Actions admission](benchmarks/actions-provider-admission.md) and the [shard traffic receipt](benchmarks/sharded-local.json).
