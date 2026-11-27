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

For native or polyglot dependencies, start with micromamba 2.10.0 or Rattler. Subsequent pip or uv layers target the staged interpreter. Pip defaults to 25.0.1 and uv to 0.9.26 in explicit plans. A UV tool lock selects a verified executable independently of the UV package visible to tasks. RCC checks existing file ownership before recording a PyPI layer. RCC takes cumulative snapshots before subsequent layers and runs post-install last. The artifact builder and v12 reader retain their existing names.

The Rattler helper implements protocol 1. It supports the RCC local channel and HTTPS channels through RCC’s configured alias. For a development sidecar, build the Rust helper with the pinned toolchain and Cargo.lock, place it next to the candidate RCC and compile RCC with the linker variable `github.com/joshyorko/rcc/conda.rattlerHelperSHA256` set to that helper's SHA-256. An ordinary build has no digest. Installation verifies a private executable copy before use. The helper requires SHA-256 package records and disables hard links, copy-on-write links and link scripts. Rattler uses its shell activation implementation and installed prefix records. Native candidate CI embeds the locked helper, micromamba 2.10.0 and UV 0.9.26. Release preparation requires all five target payloads.

Renovate discovers official micromamba and UV release versions as one dependency-dashboard review group. Approval only allows update PR creation; automerge remains disabled. Version discovery does not update micromamba archive digests, UV per-platform archive hashes, generated embedded payloads, or the Rattler git revision/Cargo.lock. Those values are coupled to integrity checks and helper qualification, so update them manually as a reviewed, coordinated change; Renovate discovery is not a safe hash or lock updater.

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

## UV-managed Python

For a Python/PyPI-only environment, use the existing implicit UV-native manifest or start an explicit plan with a `uv-runtime` layer. The latter uses embedded UV by default and takes one exact Python package such as `python=3.12.11`. Use `rcc holotree uv-lock --version latest --output uv-lock.json` to select a new stable UV tool. An exact version can replace `latest`. Copy the resulting JSON object into the layer’s `tool-lock` field. A subsequent `uv` layer inherits this lock. The managed Python version and any task dependency on UV have separate pins.

Lock creation checks the official release tag and SHA-256 metadata over verified TLS and downloads the archive using the configured UV release mirror. A build with a populated verified tool cache can reuse that lock offline. Retain previous locks for rollback. For UV 0.6.1, RCC ships the five historical archive checksums and can bootstrap directly from the archive mirror. Other versions require authenticated metadata when creating a lock. Set RCC_ENDPOINT_UV_METADATA to an approved HTTPS mirror of the release metadata API. Organizations that block the metadata endpoint can prepare explicit locks and distribute the verified tool cache ahead of time.

## Qualification boundaries

Linux amd64 local-channel builds ran Python 3.12.15 and zlib 1.3.2, imported the test wheel through pip and uv, and executed OpenSSL 3.6.5. A separate consumer home with only RCC acquired and executed an exported artifact under permissive-local trust. The intermediate artifact digest was `sha256:d40a2d17e91e1614ee83ae33a8f79d0dfdc67c7667866ac1ac7c95954f8ed776`. Strict trust rejected the unsigned archive. The run used offline flags without network isolation.

On Linux, the synthetic HTTPS channel served metadata and archives through a CONNECT proxy with Basic authentication. Missing credentials and an untrusted CA both failed. Cross-platform runtime parity and complete signed artifact and performance qualification remain open. See [Actions admission](benchmarks/actions-provider-admission.md) and the [shard traffic receipt](benchmarks/sharded-local.json).


## Operator-selected native tools

An explicit micromamba or Rattler layer accepts `native-tool-pin` containing `version`, `target` (for example `linux_amd64`) and a lowercase `sha256` of the executable. Rattler additionally requires `protocol: "1"`. Operators preseed the approved binary under `ROBOCORP_HOME/provider-tools/TOOL/VERSION/TARGET/SHA256/` with its native name or provide the exact source through `RCC_MICROMAMBA_SOURCE` or `RCC_RATTLER_SOURCE`. RCC verifies the content and executes exact version and protocol probes. An operator-selected binary outside the native qualification matrix carries no RCC compatibility guarantee. External Rattler assets must implement RCC’s helper interface. There is no separate helper distribution service.
