# Incomplete list of rcc features

> **Note:** This list is embedded in `rcc docs features` and preserves historical
> feature notes alongside current capabilities. For the full `v18.19.11` status
> classification (including Environment Artifacts v1, strict trust, and legacy
> compatibility boundaries), see [Capability & Documentation Inventory](capability-inventory.md).

* supported operating systems are Windows (`amd64`), macOS (`amd64`, `arm64`), and Linux (`amd64`)
* publish, acquire, export, and execute immutable `sha256:` Environment Artifacts
  (`rcc env`, `rcc provider`, `rcc cache serve`) with strict Ed25519 signing,
  authenticated detached metadata v2, and operator revocations
* legacy compatibility support for product families (`--robocorp`), `rccremote`,
  and Control Room CLI commands (cloud endpoints empty by default)
* supported sources for environment building are both conda and pypi
* provide repeatable, isolated, and clean environments for automations and
  robots to run on
* automatic environment creation based on declarative conda environment.yaml
  files
* easily run software robots (automations) based on declarative robot.yaml files
* also support environment creation from package.yaml files
* test robots in isolated environments before uploading them to Control Room
* provide commands for Robocorp runtime and developer tools (Worker, Assistant,
  VS Code, ...)
* provides commands to communicate with Robocorp Control Room from command line
* enable caching dormant environments in efficiently and activating them locally
  when required without need to reinstall anything
* diagnose robots and network settings to see if there is something that prevents
  using robots in specific environment
* support multiple configuration profiles for different network locations and
  conditions (remote, office, restricted networks, ...)
* running assistants from command line
* support prebuild environments, where that environment was build elsewhere
  and then just imported for local consumption
* allow "mass" prebuilding environments for delivery to those environments
  where it is not desired to build those locally
* support unmanaged environments, where rcc only initially build environment
  by the spec, but after that, does not do additional management of it
* support for creating self-contained robot bundles (zipapps) that include both code and environment
* support for running tasks directly from bundle files without manual extraction
* uv-native fast path for Python-only environments: omit `channels:` from
  conda.yaml and specify `python` and `uv` versions to bypass micromamba/conda-forge
  entirely, downloading uv at runtime for faster environment creation
