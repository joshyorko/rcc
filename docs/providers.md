# Environment Artifact Providers & Endpoint Configuration

This guide covers:

1. Managing named Environment Artifact provider profiles (`rcc provider`)
2. Hosting a local loopback provider (`rcc cache serve`) and exposing it safely behind an authenticated TLS reverse proxy
3. Publishing and acquiring **signed, strict-remote** artifacts with authenticated v2 metadata (`v18.19.9+`)
4. Overriding RCC network endpoints and `settings.yaml`

For the underlying trust contracts and revocation operations, see
[environment-artifact-trust.md](environment-artifact-trust.md),
[authenticated-metadata-v2.md](authenticated-metadata-v2.md),
[revocation-lifecycle.md](revocation-lifecycle.md), and
[signed-offline-archives.md](signed-offline-archives.md).

---

## Named provider profiles

Named providers are stored per `ROBOCORP_HOME` and select **artifact transport**
for `rcc env publish`, `rcc env acquire`, `rcc env exec`, and `rcc env export`:

```sh
rcc provider add office --type http --url http://127.0.0.1:8080 --json
rcc provider list --json
rcc provider inspect office --json
rcc provider test office --json
rcc provider remove office --json
```

### Transport rules

- **Credentials stay out of RCC storage:** `--authorization-env` stores only the environment-variable **name**, never its value. At runtime, that variable must contain the complete HTTP `Authorization` header required by the edge proxy (for example `Basic <base64>` or `Bearer <token>`); RCC does not add an authentication scheme and never prints the secret (`provider inspect` reports only the variable name and whether it is present).
- **Strict URL rules:** Artifact provider URLs must be root-only with no userinfo, query, or fragment. Plain HTTP is restricted to explicit loopback hosts (`localhost`, `127.0.0.0/8`, or `::1`); remote endpoints must use HTTPS. HTTP redirects are rejected.
- **Identity vs. transport:** Provider names select transport only. The immutable `sha256:` Artifact digest remains the environment identity. Direct HTTP(S) `--provider` URLs remain supported alongside named profiles.
- **Built-in `local` provider & warm reuse:** The built-in `local` provider uses `$ROBOCORP_HOME/artifacts/v1/provider`, distinct from the local cache (`artifacts/v1/content`) and local materializations. A warm acquire reuses a complete local materialization without contacting the provider or rebuilding—even when the provider authorization variable is absent—while still verifying current trust policy and metadata inputs.
- **Legacy `rccremote` compatibility:** The legacy `rccremote` binary and `/parts` + `/delta` protocol remain supported as compatibility level A for v18 ([v18-compatibility.md](v18-compatibility.md)).

---

## Host a local provider

Start the built-in provider in one terminal. RCC creates its default storage
root at `$ROBOCORP_HOME/artifacts/v1/provider` (or under the platform default
home when unset) and binds only to loopback:

```sh
export ROBOCORP_HOME="$HOME/.rcc-provider-demo"
export RCC_HOLOTREE_MODE=private
rcc cache serve --listen 127.0.0.1:8080
```

Pass `--listen 127.0.0.1:0` to let the OS select an ephemeral loopback port, or
add `--json` for a single machine-readable startup receipt (`url`, `root`,
`listen`). Optional flags include `--root`, `--backend` (`filesystem` or
`journal`), and `--max-bytes`.

For a complete, tested local publish/acquire/exec walkthrough using unsigned
local artifacts (`--permissive-local`), see
[quickstart.md](quickstart.md#3-publish-acquire-and-execute-a-local-environment-artifact).

---

## Host for remote clients

Keep `rcc cache serve` bound to loopback and place an authenticated TLS reverse
proxy, ingress, or tunnel in front of it. `rcc cache serve` does not manage a
user database and rejects non-loopback `--listen` addresses.

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

Example nginx site terminating TLS and enforcing HTTP Basic auth:

```nginx
server {
    listen 443 ssl;
    server_name artifacts.example.com;
    ssl_certificate     /etc/letsencrypt/live/artifacts.example.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/artifacts.example.com/privkey.pem;

    auth_basic "RCC artifacts";
    auth_basic_user_file /etc/nginx/.htpasswd;
    location / {
        proxy_pass http://127.0.0.1:8787;
        proxy_set_header Host $host;
        proxy_set_header X-Forwarded-Proto https;
    }
}
```

On the host, start `rcc cache serve` on loopback; on each client process, inject
the complete outgoing `Authorization` header from your secret store (for nginx
`auth_basic`, `Basic <base64(user:pass)>`):

```sh
# Provider host
rcc cache serve --listen 127.0.0.1:8787

# Client process (repeat profile registration per ROBOCORP_HOME)
export RCC_PROVIDER_OFFICE_AUTHORIZATION="${RCC_ARTIFACT_AUTHORIZATION}"
rcc provider add office --type http --url https://artifacts.example.com \
  --authorization-env RCC_PROVIDER_OFFICE_AUTHORIZATION --replace --json
rcc provider test office --json
```

---

## Signed artifacts for remote clients

Remote and production consumers use the default **`--strict-remote`** policy.
Since `v18.19.9`, strict admission enforces **three independent layers**:

1. **Transport authentication** (`--authorization-env`): protects HTTP access to your reverse proxy. It never signs an artifact or establishes trust.
2. **Artifact signing** (`--signing-key` on publisher; `--trust-roots` on consumer): binds the immutable `sha256:` Artifact digest to a deployment-owned Ed25519 public key map (`{"org-artifact-2026":"<base64-pubkey>"}`).
3. **Authenticated detached metadata v2** (`rcc env trust revocations issue` & `distribute` on operator; `--metadata-trust-roots` & `--metadata-state` on consumer): binds provenance, RCC object inventory, and revocations to an independent metadata authority key and a retained per-worker rollback sequence journal.

> **Important:** Do **not** assume the HTTP artifact provider serves detached
> trust files, and never infer trust roots from a provider or archive response.
> Keep `--metadata-state` outside both the trust carrier and disposable RCC
> homes. See [authenticated-metadata-v2.md](authenticated-metadata-v2.md) and
> [revocation-lifecycle.md](revocation-lifecycle.md) for full threat-boundary
> and key-rotation rules.

### 1. Publisher: sign and publish the Artifact

```sh
export RCC_PROVIDER_OFFICE_AUTHORIZATION="${RCC_ARTIFACT_AUTHORIZATION}"
rcc provider add office --type http --url https://artifacts.example.com \
  --authorization-env RCC_PROVIDER_OFFICE_AUTHORIZATION --replace --json
rcc provider test office --json

TRUST_CARRIER='/var/lib/rcc/artifact-trust'
rcc env publish --robot robot.yaml --provider office \
  --trust-carrier "$TRUST_CARRIER" --trust-carrier-type filesystem \
  --signing-key /secure/path/rcc-artifact-ed25519.key \
  --signing-key-id org-artifact-2026 --json
```

### 2. Metadata authority: issue and distribute a signed v2 metadata snapshot

After reviewing the generated provenance, RCC object inventory (flag `--sbom`
for wire compatibility), and explicit revocation array (`[]` for none), sign
and distribute the v2 metadata envelope under an **independent** metadata
authority key (see [revocation-lifecycle.md](revocation-lifecycle.md) for the
complete key-window schema and renewal/revocation commands):

```sh
ARTIFACT_DIGEST='sha256:REPLACE_WITH_DIGEST'

rcc env trust revocations issue \
  --artifact "$ARTIFACT_DIGEST" \
  --metadata-trust-roots /etc/rcc/metadata-roots.json \
  --metadata-state /var/lib/rcc/issuer-journal \
  --metadata-key-id org-metadata-2026 \
  --metadata-signing-key /secure/path/rcc-metadata-ed25519.key \
  --sequence 1 \
  --issued-at 2026-11-19T12:00:00Z \
  --expires-at 2026-11-20T00:00:00Z \
  --provenance /var/lib/rcc/reviewed/provenance.json \
  --sbom /var/lib/rcc/reviewed/inventory.json \
  --revocations /var/lib/rcc/reviewed/revocations.json \
  --output /var/lib/rcc/out/snapshot-1.json --json

rcc env trust revocations distribute \
  --artifact "$ARTIFACT_DIGEST" \
  --metadata /var/lib/rcc/out/snapshot-1.json \
  --metadata-trust-roots /etc/rcc/metadata-roots.json \
  --metadata-state /var/lib/rcc/distributor-journal \
  --trust-carrier "$TRUST_CARRIER" --trust-carrier-type filesystem --json
```

(The example timestamps above are illustrative; choose current RFC3339 times
within the 24-hour maximum snapshot lifetime and the metadata key's authorized
window. This remote operator sequence is documented from the `v18.19.11` CLI
contract and covered by `TestRevocationOperatorRealCLI`; it was not executed
against an external host in this documentation-only change.)

### 3. Consumer: acquire strictly with independent roots and rollback journal

Deliver the detached `$TRUST_CARRIER` directory (or an offline `.rcca` exported
with `--include-trust-attachments`; see [signed-offline-archives.md](signed-offline-archives.md))
plus deployment-owned `trust-roots.json` and `metadata-roots.json` to each
worker:

```sh
ARTIFACT_DIGEST='sha256:REPLACE_WITH_DIGEST'
export RCC_PROVIDER_OFFICE_AUTHORIZATION="${RCC_ARTIFACT_AUTHORIZATION}"
rcc provider add office --type http --url https://artifacts.example.com \
  --authorization-env RCC_PROVIDER_OFFICE_AUTHORIZATION --replace --json
rcc provider test office --json

rcc env acquire --artifact "$ARTIFACT_DIGEST" --provider office \
  --trust-carrier /etc/rcc/artifact-trust --trust-carrier-type filesystem \
  --trust-roots /etc/rcc/trust-roots.json \
  --metadata-trust-roots /etc/rcc/metadata-roots.json \
  --metadata-state /var/lib/rcc/worker-metadata-state \
  --strict-remote --json
```

Do not use `--permissive-local` to bypass a strict verification failure.

---

## Custom Endpoints & `settings.yaml`

You can repoint every external network endpoint via environment variables or a
local `$ROBOCORP_HOME/settings.yaml` file. Environment variables take precedence
over built-in settings (`assets/robocorp_settings.yaml`).

### Supported environment variables

| Variable | Purpose |
| --- | --- |
| `RCC_ENDPOINT_CLOUD_API` | Override legacy Control Room API endpoint (empty by default) |
| `RCC_ENDPOINT_CLOUD_LINKING` | Override legacy Control Room linking endpoint (empty by default) |
| `RCC_ENDPOINT_CLOUD_UI` | Override legacy Control Room UI endpoint (empty by default) |
| `RCC_ENDPOINT_DOWNLOADS` | Override generic downloads base URL |
| `RCC_ENDPOINT_DOCS` | Override docs base URL |
| `RCC_ENDPOINT_TELEMETRY` | Telemetry endpoint (internal telemetry remains disabled by default) |
| `RCC_ENDPOINT_ISSUES` | Issue-reporting endpoint (empty by default) |
| `RCC_ENDPOINT_PYPI` | Override PyPI simple index URL |
| `RCC_ENDPOINT_PYPI_TRUSTED` | Override PyPI trusted host |
| `RCC_ENDPOINT_CONDA` | Override Conda channel base URL |
| `RCC_ENDPOINT_UV_RELEASES` | Override `uv` binary download URL (default: GitHub Releases) |
| `RCC_AUTOUPDATES_TEMPLATES` | Override `templates.yaml` URL (default: `joshyorko/robot-templates`) |
| `RCC_AUTOUPDATES_RCC_INDEX` | Override `index.json` URL for version checks (default: `joshyorko/rcc`) |

Example shell configuration:

```sh
# Optional mirrors for PyPI and Conda
export RCC_ENDPOINT_PYPI="https://pypi.example.com/simple/"
export RCC_ENDPOINT_PYPI_TRUSTED="https://pypi.example.com/"
export RCC_ENDPOINT_CONDA="https://conda.example.com/"

# Optional update/template index mirrors (defaults shown)
export RCC_AUTOUPDATES_TEMPLATES="https://github.com/joshyorko/robot-templates/releases/latest/download/templates.yaml"
export RCC_AUTOUPDATES_RCC_INDEX="https://github.com/joshyorko/rcc/releases/latest/download/index.json"

# Verify effective runtime configuration
rcc configuration diagnostics --quick --json | jq .
```

To persist overrides in `$ROBOCORP_HOME/settings.yaml`, define an `endpoints:`
section matching the schema in [`assets/robocorp_settings.yaml`](../assets/robocorp_settings.yaml).
For multi-location corporate proxies and custom CA bundles, use configuration
profiles ([profile_configuration.md](profile_configuration.md)).

### Build-time micromamba mirror

Micromamba `2.10.0` is embedded into the `rcc` binary and extracted locally at
runtime; no live download occurs when running conda-forge environments. When
rebuilding RCC assets from source behind a mirror, override the **build-time**
download base (SHA-256 verification still applies; see [micromamba.md](micromamba.md)):

```sh
export RCC_MICROMAMBA_BASE="https://mirror.example.com/api/micromamba"
rcc run -r developer/toolkit.yaml --dev -t assets
```
