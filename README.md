# RCC

[![Build (caching)](https://github.com/joshyorko/rcc/actions/workflows/rcc.yaml/badge.svg)](https://github.com/joshyorko/rcc/actions/workflows/rcc.yaml)
[![Lint](https://github.com/joshyorko/rcc/actions/workflows/lint.yml/badge.svg)](https://github.com/joshyorko/rcc/actions/workflows/lint.yml)
[![codecov](https://codecov.io/gh/joshyorko/rcc/branch/main/graph/badge.svg)](https://codecov.io/gh/joshyorko/rcc)
[![Release](https://img.shields.io/github/v/release/joshyorko/rcc)](https://github.com/joshyorko/rcc/releases)

![RCC](/docs/rcc-logo.svg)

RCC allows you to create, manage, and distribute Python-based self-contained automation packages. RCC also allows you to run your automations in isolated Python environments so they can still access the rest of your machine.

**Repeatable, Contained Code** - movable and isolated Python environments for your automation.

Together with [robot.yaml](https://robocorp.com/docs/robot-structure/robot-yaml-format) configuration file, `rcc` is a foundation that allows anyone to build and share automation easily.

RCC is actively maintained by [JoshYorko](https://github.com/joshyorko).


## Why use rcc?

* You do not need to install Python on the target machine
* You can control exactly which version of Python your automation will run on (..and which pip version is used to resolve dependencies)
* You can avoid `Works on my machine`
* No need for `venv`, `pyenv`, ... tooling and knowledge sharing inside your team.
* Define dependencies in `conda.yaml` and automation config in `robot.yaml` and let RCC do the heavy lifting.
* If you have run into "dependency drifts", where once working runtime environment dependencies get updated and break your production system?, RCC can freeze ALL dependencies, pre-build environments, and more.
* RCC will give you a heads-up if your automations have been leaving behind processes after running.

...and much much more.

## Getting Started

**Install rcc**
> [Installation guide](#installing-rcc-from-the-command-line)

**Pull a robot from GitHub:**
> `rcc pull github.com/joshyorko/template-python-browser`

**Run robot**
> `rcc run`

**Create your own robot from templates**
> `rcc create`

For detailed instructions, visit the [RCC documentation](https://robocorp.com/docs/rcc/overview) to get started. To build `rcc` from this repository, see the [Setup Guide](/docs/BUILD.md)

### Environment artifact providers

Named providers are stored per `ROBOCORP_HOME` and are used by the portable
artifact commands:

```sh
rcc provider add office --type http --url http://127.0.0.1:8080 \
  --json
rcc provider list --json
rcc provider inspect office --json
rcc provider test office --json
rcc provider remove office --json
```

For a complete local publish/acquire example, including the explicit local
trust policy, see [Host a local provider](#host-a-local-provider). Production
publish/acquire requires a signing key, deployment-owned trust roots, and a
detached trust carrier; see [Signed artifacts for remote clients](#signed-artifacts-for-remote-clients).

`authorization-env` stores only the environment-variable name. When present,
its value must be the complete HTTP `Authorization` header required by the
provider; RCC does not add an authentication scheme. The secret is never
persisted or printed. Provider URLs must be root-only with no userinfo, query,
or fragment. HTTP is restricted to
explicit loopback hosts (`localhost`, `127.0.0.0/8`, or `::1`); remote endpoints
must use HTTPS. Redirects are rejected.
The built-in `local` provider uses the local provider root; it is distinct from
the RCC cache and local materializations. A warm acquire reuses a complete local
materialization without contacting the provider or rebuilding, even when the
provider authorization variable is absent. Existing direct HTTP(S) `--provider`
URLs remain supported. Provider names select transport only; the immutable
`sha256:` Artifact digest remains the identity. The legacy `rccremote` protocol
is classified as compatibility level A for v18.

#### Host a local provider

Run the provider in one terminal. RCC creates its default storage directory at
`$ROBOCORP_HOME/artifacts/v1/provider` (or under its platform default home when
that variable is unset); the server listens on loopback.
For a fixed local URL, set an explicit loopback port:

```sh
export ROBOCORP_HOME="$HOME/.rcc-provider-demo"
rcc cache serve --listen 127.0.0.1:8080
```

In another terminal, configure a client profile and publish an environment:

```sh
export ROBOCORP_HOME="$HOME/.rcc-provider-demo"
rcc provider add office-local --type http --url http://127.0.0.1:8080 --json
rcc provider test office-local --json
rcc env publish --robot robot.yaml --provider office-local \
  --trust-carrier "$ROBOCORP_HOME/artifacts/v1/trust" \
  --trust-carrier-type filesystem --json
# Copy the full artifactDigest value from the publish result into this variable.
ARTIFACT_DIGEST='sha256:REPLACE_WITH_DIGEST'
rcc env acquire --artifact "$ARTIFACT_DIGEST" --provider office-local \
  --trust-carrier "$ROBOCORP_HOME/artifacts/v1/trust" \
  --trust-carrier-type filesystem --permissive-local --json
```

Set the same `ROBOCORP_HOME` in the server terminal before starting the server,
so the provider profile, local provider store, and trust carrier are available
to the client. This example deliberately uses unsigned artifacts and the
explicit `--permissive-local` policy for a controlled local development flow.
Do not use that policy for remote or production artifacts. By default,
`env acquire` uses `strict-remote` and requires a valid detached signature,
deployment-owned trust roots, and fresh revocation data.

To let RCC choose an available loopback port, run `rcc cache serve` and use
the URL from its startup line. Add `--json` for a machine-readable startup
receipt. See the [provider hosting guide](docs/holotree.md#hosting-an-environment-artifact-provider)
for storage, backend, and lifecycle details.

#### Host for remote clients

Keep `rcc cache serve` on loopback and place a TLS reverse proxy, ingress, or
tunnel in front of it. The edge terminates TLS and enforces authentication;
the RCC server does not configure an authentication database and must not be
bound directly to a public interface.

```text
Remote RCC client
    | HTTPS + Authorization header
    v
TLS/auth reverse proxy (artifacts.example.com:443)
    | HTTP to 127.0.0.1:8787
    v
rcc cache serve --listen 127.0.0.1:8787
    |
    v
$ROBOCORP_HOME/artifacts/v1/provider
```

Start the server on the host, configure the reverse proxy to forward to
`127.0.0.1:8787`, then configure each client with the HTTPS URL and its
Authorization header:

```sh
# Provider host
rcc cache serve --listen 127.0.0.1:8787

# Each client process; this variable contains the complete header value from
# the client's secret store. nginx auth_basic requires Basic credentials;
# other proxy configurations may require a different scheme.
export RCC_PROVIDER_OFFICE_AUTHORIZATION="${RCC_ARTIFACT_AUTHORIZATION}"
rcc provider add office --type http --url https://artifacts.example.com \
  --authorization-env RCC_PROVIDER_OFFICE_AUTHORIZATION --replace --json
rcc provider test office --json
```

`--authorization-env` names a client-side environment variable containing the
complete outgoing `Authorization` header. It does not configure server-side
authentication. The [provider hosting guide](docs/holotree.md#hosting-an-environment-artifact-provider)
includes a complete nginx TLS/auth example and deployment notes. For its
`auth_basic` configuration, the secret store must provide a complete value
such as `Basic <base64-encoded-username-and-password>`; other proxies use the
scheme configured by their operator.

#### Signed artifacts for remote clients

Remote and production consumers use the default `strict-remote` policy. The
publisher signs with an Ed25519 private key held by the publishing system, and
each consumer receives the matching public key through deployment-owned trust
roots. Keep the trust carrier detached from the artifact provider: by default,
`env publish` writes it to RCC's local filesystem trust store, and
`rcc cache serve` serves artifact objects rather than that local trust
directory. Transfer the published carrier to consumers through a trusted
deployment channel, preserving its directory structure. The proxy's HTTP
Authorization credential authenticates transport only; it is not an artifact
signature or trust root.

On the publisher, set `TRUST_CARRIER` to a directory that can be distributed
with the signed attachments and provide the signing key and stable key ID.
Provider profiles are local to each `ROBOCORP_HOME`, so configure the
publisher's HTTPS profile as well. The secret store supplies the complete
Authorization value needed by the TLS proxy:

```sh
export RCC_PROVIDER_OFFICE_AUTHORIZATION="${RCC_ARTIFACT_AUTHORIZATION}"
rcc provider add office --type http --url https://artifacts.example.com \
  --authorization-env RCC_PROVIDER_OFFICE_AUTHORIZATION --replace --json
rcc provider test office --json
TRUST_CARRIER='/var/lib/rcc/artifact-trust'
rcc env publish --robot robot.yaml --provider office \
  --trust-carrier "$TRUST_CARRIER" --trust-carrier-type filesystem \
  --signing-key /secure/path/rcc-ed25519-private.key \
  --signing-key-id org-prod-2026 --json
```

Copy the complete `artifactDigest` from the publish result into the quoted
variable below. On each consumer, deploy the detached carrier directory and a
JSON trust-roots file whose map keys are signer IDs and whose values are
base64-encoded Ed25519 public keys, for example
`{"org-prod-2026":"BASE64_PUBLIC_KEY"}`. Obtain the public key from the
organization's key-management process; do not derive trust from the provider
response. Provider profiles are local to each `ROBOCORP_HOME`, so repeat the
HTTPS profile setup on every consumer. The secret store supplies that
consumer's complete proxy Authorization value.

```sh
ARTIFACT_DIGEST='sha256:REPLACE_WITH_DIGEST'
export RCC_PROVIDER_OFFICE_AUTHORIZATION="${RCC_ARTIFACT_AUTHORIZATION}"
rcc provider add office --type http --url https://artifacts.example.com \
  --authorization-env RCC_PROVIDER_OFFICE_AUTHORIZATION --replace --json
rcc provider test office --json
rcc env acquire --artifact "$ARTIFACT_DIGEST" --provider office \
  --trust-carrier /etc/rcc/artifact-trust --trust-carrier-type filesystem \
  --trust-roots /etc/rcc/trust-roots.json --strict-remote --json
```

The consumer's HTTP credential is still supplied using the complete
Authorization header required by the TLS proxy. It does not replace
`--trust-roots` or the detached carrier. `--permissive-local` is not appropriate
for this remote flow.

## Installing RCC from the command line

> Links to changelog and different versions [available here](https://github.com/joshyorko/rcc/releases)

### Windows

1. Open the command prompt
1. Download: `curl -o rcc.exe https://github.com/joshyorko/rcc/releases/latest/download/rcc-windows64.exe`
1. [Add to system path](https://www.architectryan.com/2018/03/17/add-to-the-path-on-windows-10/): Open Start -> `Edit the system environment variables`
1. Test: `rcc`

### macOS

1. Open the terminal
1. Intel: `curl -o rcc https://github.com/joshyorko/rcc/releases/latest/download/rcc-macos64`
1. Apple Silicon: `curl -o rcc https://github.com/joshyorko/rcc/releases/latest/download/rcc-macosarm64`
1. Make the downloaded file executable: `chmod a+x rcc`
1. Add to path: `sudo mv rcc /usr/local/bin/`
1. Test: `rcc`

### Linux

1. Open the terminal
1. Download: `curl -o rcc https://github.com/joshyorko/rcc/releases/latest/download/rcc-linux64`
1. Make the downloaded file executable: `chmod a+x rcc`
1. Add to path: `sudo mv rcc /usr/local/bin/`
1. Test: `rcc`

### Homebrew (macOS & Linux)

RCC is available via Homebrew for both macOS and Linux:

```bash
brew tap joshyorko/tools
brew install --cask rcc
```

#### Platform Support

| Platform | Status | Binary |
|----------|--------|--------|
| Linux x64 | ✅ Native | rcc-linux64 |
| macOS Intel | ✅ Native | rcc-macos64 |
| macOS Apple Silicon | ✅ Native | rcc-macosarm64 |



#### For Brewfile Users

Add to your Brewfile:

```ruby
tap "joshyorko/tools"
cask "rcc"
```

Or with the full path:

```ruby
cask "joshyorko/tools/rcc"
```

## Documentation

The changelog can be seen [here](/docs/changelog.md). It is also visible inside RCC using the command `rcc docs changelog`.

Some tips, tricks, and recipes can be found [here](/docs/recipes.md).
These are also visible inside RCC using the command: `rcc docs recipes`.

For additional documentation on robot.yaml, conda.yaml, and the broader ecosystem, see the [Robocorp Documentation](https://robocorp.com/docs).

## Telemetry

This fork disables all internal telemetry by default:

- No background metrics are sent and internal metrics are disabled across product modes.
- The installation identifier header is not attached to outbound HTTP requests when telemetry is disabled.
- The `rcc configure identity` output will always report tracking as disabled unless explicitly modified in code; feedback/metric commands are effectively no-ops.

## Custom Endpoints

You can repoint all network endpoints via environment variables or a local `settings.yaml`.

Environment variables (take precedence over builtin settings):
- `RCC_ENDPOINT_CLOUD_API`
- `RCC_ENDPOINT_CLOUD_LINKING`
- `RCC_ENDPOINT_CLOUD_UI`
- `RCC_ENDPOINT_DOWNLOADS`
- `RCC_ENDPOINT_DOCS`
- `RCC_ENDPOINT_TELEMETRY`
- `RCC_ENDPOINT_ISSUES`
- `RCC_ENDPOINT_PYPI`
- `RCC_ENDPOINT_PYPI_TRUSTED`
- `RCC_ENDPOINT_CONDA`
- `RCC_ENDPOINT_UV_RELEASES` - Override the uv binary download URL (default: GitHub releases)
- `RCC_AUTOUPDATES_TEMPLATES` - Override the templates.yaml URL for robot templates
- `RCC_AUTOUPDATES_RCC_INDEX` - Override the index.json URL for version checking

Example (`~/.zshrc`):

```zsh
# Point rcc at your own control plane endpoints
export RCC_ENDPOINT_CLOUD_API="https://api.your-domain.com/"
export RCC_ENDPOINT_CLOUD_UI="https://console.your-domain.com/"
export RCC_ENDPOINT_CLOUD_LINKING="https://console.your-domain.com/link/"

# Optional: switch where generic downloads resolve
export RCC_ENDPOINT_DOWNLOADS="https://downloads.your-domain.com/"

# Optional mirrors for docs, PyPI, and conda
export RCC_ENDPOINT_DOCS="https://docs.your-domain.com/"
export RCC_ENDPOINT_PYPI="https://pypi.org/simple/"
export RCC_ENDPOINT_PYPI_TRUSTED="https://pypi.org/"
export RCC_ENDPOINT_CONDA="https://conda.anaconda.org/"

# Optional: override where rcc checks for updates
# (defaults shown here; replace with your own mirror/private registry if needed)
export RCC_AUTOUPDATES_TEMPLATES="https://github.com/joshyorko/robot-templates/releases/latest/download/templates.yaml"
export RCC_AUTOUPDATES_RCC_INDEX="https://github.com/joshyorko/rcc/releases/latest/download/index.json"

# Validate your overrides
build/rcc configuration diagnostics --quick --json | jq .
```

Local settings file: write a `settings.yaml` to `$ROBOCORP_HOME/settings.yaml` with an `endpoints:` section. See `assets/robocorp_settings.yaml` for the full shape; any key you set there will override the built-in defaults.

### Notes

Micromamba is embedded into `rcc` and extracted locally at runtime; no live download is needed for the conda-forge path. The uv-native path (conda.yaml with no `channels`) downloads uv on-demand and caches it locally. If you rebuild assets yourself, you can change the micromamba download base used during asset preparation via:

```zsh
export RCC_DOWNLOADS_BASE="https://downloads.your-domain.com"
rcc run -r developer/toolkit.yaml --dev -t assets
```

To verify what endpoints are in effect at runtime, run:

```zsh
build/rcc configuration diagnostics --quick --json | jq .
```

## Acknowledgements

RCC was originally developed by the Robocorp team and released as open source under the Apache 2.0 license. This fork continues development independently.

- [Robocorp Documentation](https://robocorp.com/docs) - detailed docs on compatible python libraries and guides.

## License

Apache 2.0
