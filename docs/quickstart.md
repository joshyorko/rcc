# RCC Quickstart

This guide takes you from a verified `rcc` binary to a working isolated Python
task and a local Environment Artifact lifecycle—without cloud credentials or a
system Python installation.

Every command in Steps 1–3 was executed against released **`rcc v18.19.11`** on
Linux `amd64` in an isolated private `ROBOCORP_HOME`.

---

## Optional: isolate RCC state before your first command

Running **any** `rcc` command (including `rcc --version`) initializes
`$ROBOCORP_HOME` (default `~/.robocorp` on Unix, `%LOCALAPPDATA%\robocorp` on
Windows) and creates `rcccache.yaml`, lock files, and cache directories. If you
want to try RCC without writing to your default home or any machine-wide shared
Holotree, export a temporary private home **before** installing or running
`rcc --version`:

```sh
export ROBOCORP_HOME="$HOME/.rcc-quickstart"
export RCC_HOLOTREE_MODE=private
```

---

## 1. Verify your installation

Install `rcc` using the checksum-verified staged commands in the
[root README](../README.md#install-rcc-v181911), then confirm the binary
version:

```sh
rcc --version
```

Expected output:

```text
v18.19.11
```

---

## 2. Run a minimal, credential-free task

Create a directory with three files: a dependency spec (`conda.yaml`), a task
definition (`robot.yaml`), and a Python script (`task.py`).

```sh
mkdir -p rcc-hello && cd rcc-hello
```

`conda.yaml`:

```yaml
channels:
  - conda-forge
dependencies:
  - python=3.12.11
```

`robot.yaml`:

```yaml
tasks:
  Hello:
    shell: python task.py
condaConfigFile: conda.yaml
artifactsDir: output
PATH:
  - .
PYTHONPATH:
  - .
```

`task.py`:

```python
import platform
import sys

print(f"Hello from RCC! Python {platform.python_version()} ({sys.executable})")
```

Run the task:

```sh
rcc run
```

### What happens on the first run

1. RCC hashes `conda.yaml` into a deterministic Holotree blueprint (`e64246d31bd41a89` on `linux_amd64`).
2. RCC extracts its embedded `micromamba v2.10.0` binary locally, solves `python=3.12.11` from `conda-forge`, records every file by content hash into `$ROBOCORP_HOME/hololib/`, and materializes an isolated Holotree space.
3. RCC runs `python task.py` inside that space and writes task logs plus a frozen environment descriptor (`output/environment_<platform>_freeze.yaml`).

Observed output excerpt (`rcc v18.19.11`, Linux `amd64`):

```text
####  Progress: 02/15  v18.19.11     0.000s  Holotree blueprint is "e64246d31bd41a89" [linux_amd64 ... from "conda.yaml"].
...
Note: Extracting micromamba binary from inside rcc.
####  Progress: 07/15  v18.19.11     0.174s  Running micromamba phase. (micromamba v2.10.0) [layer: e64246d31bd41a89]
...
####  Progress: 13/15  v18.19.11     0.000s  Record holotree stage to hololib ...
####  Progress: 14/15  v18.19.11     0.212s  Restore space from library ...
...
Hello from RCC! Python 3.12.11 (<ROBOCORP_HOME>/holotree/.../bin/python3.12)
@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@
@@@  From rcc "v18.19.11" (controller: "user") point of view, "actual main robot run" was SUCCESS. @@@
@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@
OK.
```

Run `rcc run` a second time:

```sh
rcc run
```

On the warm run, RCC skips the micromamba build phase (`03/15`–`13/15`) and
restores directly from Hololib (`Progress: 02/15` -> `Progress: 14/15`).
First-build time depends on your network and disk speed; do not assume a fixed
wall-clock target across environments.

### Tip: Python-only `uv`-native mode

If your environment needs only Python and PyPI packages (no conda-forge binary
packages), omit `channels:` and list both `python` and `uv` in `conda.yaml`:

```yaml
dependencies:
  - python=3.12.11
  - uv=0.13.8
  - pip:
      - requests==2.32.3
```

In `uv`-native mode, RCC bypasses micromamba/conda-forge, downloads and caches
`uv` on demand (overrideable via `RCC_ENDPOINT_UV_RELEASES`), and installs
Python and wheels with `uv`.

---

## 3. Publish, acquire, and execute a local Environment Artifact

When you want to **build an environment once** and distribute it as an immutable,
content-addressed `sha256:` artifact (separate from your application code), use
the `rcc cache`, `rcc provider`, and `rcc env` commands.

> **Security boundary:** The walkthrough below uses unsigned local artifacts and
> `--permissive-local` for controlled local development only. Remote and
> production deployments must use the default `--strict-remote` policy with
> Ed25519 signing, deployment-owned `--trust-roots`, authenticated v2 metadata
> (`--metadata-trust-roots`), and a retained rollback journal
> (`--metadata-state`). See [providers.md](providers.md#signed-artifacts-for-remote-clients)
> and [authenticated-metadata-v2.md](authenticated-metadata-v2.md).

### Terminal 1 — Start a loopback artifact provider

```sh
export ROBOCORP_HOME="$HOME/.rcc-quickstart"
export RCC_HOLOTREE_MODE=private
rcc cache serve --listen 127.0.0.1:8080 --json
```

Expected startup receipt:

```json
{"url":"http://127.0.0.1:8080","root":"/home/<user>/.rcc-quickstart/artifacts/v1/provider","listen":"127.0.0.1:8080"}
```

(Pass `--listen 127.0.0.1:0` to let the OS assign an ephemeral loopback port.)

### Terminal 2 — Register the provider, publish, acquire, and execute

Run from the `rcc-hello` directory you created in Step 2:

```sh
export ROBOCORP_HOME="$HOME/.rcc-quickstart"
export RCC_HOLOTREE_MODE=private

# Register and verify the loopback provider profile
rcc provider add local-demo --type http --url http://127.0.0.1:8080 --json
rcc provider test local-demo --json

# Build and publish the immutable Environment Artifact
rcc env publish --robot robot.yaml --provider local-demo \
  --trust-carrier "$ROBOCORP_HOME/artifacts/v1/trust" \
  --trust-carrier-type filesystem --json
```

Observed publish receipt shape (`rcc v18.19.11`, Linux `amd64`):

```json
{
  "artifactDigest": "sha256:<64-hex-chars>",
  "specificationDigest": "sha256:<64-hex-chars>",
  "legacyBlueprintKey": "e64246d31bd41a89",
  "objectCount": 4947,
  "uploadedBytes": 65565014,
  "reusedBytes": 0
}
```

Copy the full `artifactDigest` value from the publish receipt above (no host
Python or JSON tool is required; optionally, if `jq` is installed, you can
capture it automatically with `| jq -r .artifactDigest`), then acquire and
execute:

```sh
ARTIFACT_DIGEST='sha256:REPLACE_WITH_DIGEST_FROM_PUBLISH_JSON'

# Acquire and materialize (explicitly selecting permissive-local for unsigned local dev)
rcc env acquire --artifact "$ARTIFACT_DIGEST" --provider local-demo \
  --trust-carrier "$ROBOCORP_HOME/artifacts/v1/trust" \
  --trust-carrier-type filesystem --permissive-local --json

# Execute with a process-scoped lease (JSON mode keeps child output off stdout)
rcc env exec --artifact "$ARTIFACT_DIGEST" \
  --trust-carrier "$ROBOCORP_HOME/artifacts/v1/trust" \
  --trust-carrier-type filesystem --permissive-local --json -- \
  python -c "import platform; print('artifact-ok', platform.python_version())"
```

Observed `env exec` receipt highlights:

- `"cacheHit": "local-materialization"` (reuses the ready local materialization without contacting the provider)
- `"exitCode": 0`, `"status": "completed"`, and a unique `"leaseId"`
- `"verification.policyMode": "permissive-local"`, `"verification.valid": true`

If you need child stdout/stderr forwarded directly (for interactive or streaming
processes), use `--inherit-streams --receipt-file <path>` instead of `--json`.

Notice that running the same unsigned artifact with `--strict-remote` (or no
policy flag, since `strict-remote` is the default) fails closed as expected:

```text
Error: artifact trust verification failed: metadata-unsigned: strict policy requires authenticated v2 metadata
```

---

## 4. Next steps

- **Interactive project scaffolding:** Run `rcc create` in an interactive terminal to generate a project from built-in or community templates (`joshyorko/robot-templates`, currently `v15.0.0`).
- **Remote providers, TLS proxies, and signed strict-remote flows:** [providers.md](providers.md)
- **Trust model, object inventory vs. package SBOM, and revocation:** [environment-artifact-trust.md](environment-artifact-trust.md), [authenticated-metadata-v2.md](authenticated-metadata-v2.md), [revocation-lifecycle.md](revocation-lifecycle.md)
- **Offline `.rcca` archives:** [signed-offline-archives.md](signed-offline-archives.md)
- **Full documentation index:** [docs/README.md](README.md)
- **Diagnostics & troubleshooting:** Run `rcc configuration diagnostics --quick --json` (or `rcc configuration settings --json` to inspect effective endpoint overrides) and see [troubleshooting.md](troubleshooting.md)
