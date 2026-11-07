# Contributing to RCC

RCC stands for **Repeatable, Contained Code**.

This repo is a Go CLI that builds and runs **movable, isolated Python environments** for automations ("robots"). It reads configuration (like `robot.yaml` and `conda.yaml`), conjures up an environment, caches it, and runs tasks—so you never have to hear "works on my machine" again.

This guide is intentionally practical: copy/paste the commands, get a working dev loop, and ship a PR. We'll go easy on the theory part.

> **Parental advisory**: May contain failed attempts at "humor."

---

## The golden path: a contained, repeatable dev environment

The repo includes a **developer toolkit** under `developer/` which lets you bootstrap a consistent toolchain (Go, Python, Invoke, Robot Framework, Git) using an **existing** `rcc` binary on your `PATH`.

*Yes, we use RCC to develop RCC. It's turtles all the way down. 🐢*

Pinned tool versions live in `developer/setup.yaml`:

- Python **3.10.15**
- Invoke **2.2.0**
- Robot Framework **6.1.1** (matches `robot_requirements.txt`)
- Go **1.26.9** for RCC builds. The toolkit bootstraps Go 1.26.5 from Conda
  Forge, then the `go.mod` minimum and Go's default toolchain auto-selection
  select 1.26.9. CI and Dagger use 1.26.9 directly.
- Git **2.46.0**

### 1) Prerequisites

- `git` (you're reading this, so probably yes)
- An existing `rcc` binary available on your `PATH`
    - Pop quiz: What if you don't have one yet? Grab one from [our releases](https://github.com/joshyorko/rcc/releases) or see the `README.md` for installation options.

### 2) Clone and run tasks (toolkit)

From the repo root:

**Quick acceptance-test smoke run** (no `--dev`):

```bash
rcc run -r developer/toolkit.yaml -t robot
```

HTML logs land in `tmp/output/log.html`. Top-notch reporting right there.

**Developer tasks** (require `--dev`):

| Task | Command |
|------|---------|
| Unit tests | `rcc run -r developer/toolkit.yaml --dev -t unitTests` |
| Local build | `rcc run -r developer/toolkit.yaml --dev -t local` |
| Cross-platform build | `rcc run -r developer/toolkit.yaml --dev -t build` |
| Asset generation | `rcc run -r developer/toolkit.yaml --dev -t assets` |
| Agent guidance structural validation | `rcc run -r developer/toolkit.yaml --dev -t agentDocs` |
| Tooling info | `rcc run -r developer/toolkit.yaml --dev -t tools` |

### Environment artifact gates

The contained toolkit exposes the focused artifact tasks `artifactFocused`,
`artifactRace`, `artifactVertical`, and `artifactRobot`, plus `goVet`,
`binaryInventory`, `selfHost`, and `releaseCandidate`. `goVet` accepts the exact
documented legacy finding set but rejects any new diagnostic. The artifact acceptance gate is Linux-only;
`artifactRace`, `artifactVertical`, `artifactRobot`, `selfHost`, and
`releaseCandidate` therefore require a Linux host; `artifactFocused` is the
portable package check. Standard, Python, and
extended templates need no special artifact-authoring model.

In a fresh checkout, run `local` before `artifactFocused`: its Python task tests
read and hash `build/rcc`, so asset generation alone is insufficient.

The default development and promotion path is installed/released RCC →
`developer/toolkit.yaml` → build/test the candidate, followed by
`build/rcc` → a fresh `ROBOCORP_HOME` → `developer/toolkit.yaml` → build/test
the candidate itself. Direct `go` or `inv` commands are tight diagnostic/TDD
fallbacks, not primary promotion gates when a toolkit task exists.

### How the toolkit works (so you can debug it)

When things go sideways (and they will—this is software), here's what's actually happening:

1. `developer/toolkit.yaml` declares tasks and devTasks.
2. Those tasks call `python developer/call_invoke.py <task>`.
3. `developer/call_invoke.py` runs `invoke <task>` in the repo root, so the actual implementation lives in `tasks.py`.

Think of it as a street address for your automation: toolkit.yaml → call_invoke.py → tasks.py.

---

## Manual development (when you don't want the toolkit)

You can also develop with your system Go/Python. We won't judge. Much.

Sometimes, less (tools) is not more (productivity). But you do you.

### Requirements

- Go **1.26.9** (CI uses the same patch release. Mismatched versions lead to
  mysterious build failures. Ask us how we know.)
- Python **3.10+**
- Invoke (`python -m pip install invoke`)

### Common commands

| Task | Command | Notes |
|------|---------|-------|
| List tasks | `inv -l` | Shows all available tasks |
| Show tooling info | `inv tooling` | |
| Unit tests | `inv test` | Forces `GOARCH=amd64`; set it yourself if running `go test` directly |
| Local build | `inv local` | |
| Cross-platform build | `inv build` | |
| Agent guidance structural validation | `inv agentdocs` | |
| Acceptance tests (setup) | `inv robotsetup` | Run once |
| Acceptance tests (run) | `inv robot` | |
| Update docs TOC | `inv toc` | |
| Clean | `inv clean` | For when you want that fresh start feeling |

---

## Repo map (where to change what)

Your robot, however, lacks both vision and the ability to think. It needs precise instructions to find *anything*. Here's a map:

| Directory | Purpose |
|-----------|---------|
| `cmd/` | CLI commands (Cobra entrypoints & implementations) |
| `operations/` | Higher-level behaviors (auth, bundles, diagnostics, etc.) |
| `conda/` | Python environment creation, activation, and package installation |
| `htfs/` | Holotree filesystem storage and environment materialization |
| `remotree/` | Remote cache protocol, service, and synchronization |
| `environmentartifact/` | Canonical manifests, object indexes, artifact identity, and compatibility contracts |
| `artifactprovider/` | Deferred filesystem, HTTP, and archive artifact transport; secret-safe resolution failures |
| `artifacttrust/` | Detached attestations, trust carriers, signatures, policy, and revocation verification |
| `environmentlifecycle/` | Publish, acquire, materialize, lease, execute, reconcile, and garbage-collect artifacts |
| `buildcoord/` | Build claims, heartbeats, publication, and recovery coordination |
| `settings/` | RCC settings, endpoint, and provider-profile configuration |
| `common/`, `pathlib/`, `shell/` | Shared libraries/utilities |
| `assets/` | *Source* assets that get embedded into the binary |
| `blobs/` | Generated/embedded assets (**do not edit by hand**—the build will overwrite your tears) |
| `templates/` | Robot templates zipped into `blobs/assets/*.zip` |
| `robot_tests/` | Robot Framework acceptance tests (HTML logs under `tmp/output/`) |
| `developer/` | The "repeatable, contained" dev environment bootstrap |
| `.dagger/` | Dagger CI module for containerized builds and tests |

### Environment execution: behavior, ownership, and evidence

| Behavior | Entry point | Critical invariant | Regression coverage |
|----------|-------------|--------------------|---------------------|
| Provider resolution | [`cmd/provider.go`](cmd/provider.go): `newProviderReference`; [`artifactprovider/deferred.go`](artifactprovider/deferred.go) | Construction remains lazy; resolver errors suppress secrets. [`provider inspect`](cmd/providerInspect.go) reports authorization-source name and presence, never its value. | [`cmd/provider_test.go`](cmd/provider_test.go): `TestProviderReferenceDoesNotLoadAtConstruction`, `TestProviderInspectAuthorizationReportsNameAndPresenceWithoutValue`; [`artifactprovider/deferred_test.go`](artifactprovider/deferred_test.go): `TestDeferredProviderStableError`, `TestDeferredProviderDoesNotExposeResolverError` |
| Acquisition and local reuse | [`environmentlifecycle/materialize.go`](environmentlifecycle/materialize.go): `Acquirer.Acquire` | Content transaction precedes artifact transaction. The default materializer uses `materializeLocked` because the public method would reacquire the same non-reentrant lock. Local reuse avoids remote artifact-provider calls, but may still read a trust carrier. | [`environmentlifecycle/materialize_test.go`](environmentlifecycle/materialize_test.go): `TestDefaultAcquisitionMaterializesAndReusesWithinDeadline`, `TestWarmAcquireDoesNotResolveDeferredProvider` |
| Leased execution | [`cmd/environment.go`](cmd/environment.go): `defaultEnvironmentCommandDependencies`; [`cmd/environmentExec.go`](cmd/environmentExec.go): `newEnvironmentExecCommand`; [`environmentlifecycle/execute.go`](environmentlifecycle/execute.go): `Execute`, `ExecuteWithStreams` | Both executors are wired together. Copy the receipt **and** runtime trust inputs into `Materialization`. Release the lease after reaping the child. Preserve mode-specific output and error precedence. | [`cmd/environmentExec_test.go`](cmd/environmentExec_test.go): `TestEnvironmentExecRequiresSelectedExecutor`, `TestEnvironmentExecSelectsExecutorAndPreservesTrust`; [`robot_tests/environment_artifacts/library.py`](robot_tests/environment_artifacts/library.py): `execution_modes_should_preserve_streams_and_receipts` |
| Lease-time trust refresh | [`environmentlifecycle/lease.go`](environmentlifecycle/lease.go): `LocalMaterializer.Lease`; [`environmentlifecycle/trust.go`](environmentlifecycle/trust.go): `refreshMaterializationTrust` | An acquisition receipt does not replace current policy, trust request, or carrier inputs. Refresh revocations for a new lease without changing an already-running lease. | [`environmentlifecycle/lease_test.go`](environmentlifecycle/lease_test.go): `TestNewLeaseRefreshesRevocationsWithoutChangingRunningLease`; [`cmd/environment_test.go`](cmd/environment_test.go): `TestEnvironmentConsumersLoadRuntimeTrustRoots` |

`--json` suppresses child output and emits one result only if execution returns
without an error. `--inherit-streams` requires `--receipt-file`, forwards the
child streams, and attempts the receipt even on an execution error. When both
flags are present, inherited streams take precedence. Receipt-write errors take
precedence over child/execution failures. Context cancellation and a signal
forwarded to the child are different outcomes; keep their classifications.
Receipt replacement and symlink checks are covered in
[`cmd/environment_test.go`](cmd/environment_test.go), under
`TestEnvironmentExecReceiptSafelyReplacesFilesAndRejectsSymlinks` and
`TestEnvironmentExecReceiptPreservesOldFileWhenReplacementFails`.

Keep process guarantees specific: the Linux supervisor's parent-death signal
targets the direct child; Windows uses a job object. Cleanup order alone does
not prove descendant containment. See
[`environmentlifecycle/execute_process_test.go`](environmentlifecycle/execute_process_test.go)
and [`environmentlifecycle/lease_test.go`](environmentlifecycle/lease_test.go).
Native platform tests are required before claiming parity.

The locality benefit is a complete private executor composition point and
adjacent ownership contracts. The command still shows its trust mapping and
mode-specific output in place; the existing lifecycle and provider boundaries
remain. The added navigation cost is a second typed executor field and this
source/test map, not a new dispatch layer. No LLM performance improvement has
been measured.

### Startup conventions and unresolved bootstrap follow-up

Imports currently have effects before command execution:
[`common/variables.go`](common/variables.go): `init` reads arguments/environment,
sets global defaults, and provisions cache directories;
[`common/logger.go`](common/logger.go): `init` starts the logger goroutine;
[`htfs/directory.go`](htfs/directory.go): `init` provisions storage directories.
[`common/exit.go`](common/exit.go): `Exit` panics with `common.ExitCode`;
[`cmd/rcc/main.go`](cmd/rcc/main.go): `ExitProtection` recovers that value at the
executable boundary. Preserve deferred cleanup when changing command errors.

An explicit bootstrap boundary remains **unresolved and separately scoped**;
no startup migration is implemented here. The initial importer inventory is:
`common` is imported directly by `cloud`, `cmd` (including both executables),
`conda`, `environmentartifact`, `environmentlifecycle`, `htfs`, `journal`,
`operations`, `pathlib`, `pretty`, `remotree`, `robot`, `settings`, `shell`,
`wizard`, and `xviper`; `htfs` by `cmd`, `environmentartifact`,
`environmentlifecycle`, `operations`, and `remotree`. Refresh this inventory with
`go list -json ./...` and include external Go consumers before choosing a boundary.

The follow-up should compare CLI-root initialization in both executables with
explicit storage entry points (`htfs.New`, `Acquirer.Acquire`, and direct
materializer users). CLI-root setup gives command authors one visible owner,
but does not cover library/test callers. Storage-entry setup covers those callers,
but needs idempotent, concurrency-safe ownership and a separate decision for
logging and argument-derived globals. Inventory callers and error handling
before selecting either approach; preserve shared/private selection, permissions,
startup failures, and v12 storage compatibility.

Characterize each proposal in fresh subprocesses: import-only probes for
`common` and `htfs`, help/version in both executables, and actual storage entry
points. Set environment **before process start**; `t.Setenv` after import cannot
characterize `init`. Use a disposable container or mount namespace with a
read-only host filesystem and writable task-local homes, caches, and temporary
directories. Contain `HOME`, `ROBOCORP_HOME`, XDG paths, platform home/temp
variables, and the shared holotree mount (Linux defaults to `/opt/robocorp/ht`),
not only `ROBOCORP_HOME`. Test private/shared modes, existing/missing/read-only
roots, and concurrent startup; record writes, permissions, stderr, exit status,
and surviving processes. Keep platform-specific evidence separate. The bounded
acquisition regression uses `--warranty-voided` to suppress shared-directory
initialization in its child; it is not a normal-startup characterization.

---

## Dagger: CI you can run locally (before CI yells at you)

The `.dagger/` directory contains a [Dagger](https://dagger.io) module for running builds and tests in containers.

*Yes, we use containers to test a tool that creates isolated environments. It's containers all the way down. 🐳*

### Why Dagger instead of just running CLI commands?

**The meta-containment angle:** RCC creates isolated Python environments. Dagger wraps *that* in an isolated container. So when you run `dagger call run-robot-tests`, you're testing RCC's environment isolation inside Dagger's container isolation. It's isolation all the way down. If something breaks, you know it's not because your laptop has a weird `~/.bashrc` or a rogue Python in your PATH.

You could just run `go test ./...` or `rcc run` directly. And honestly, for quick iteration, you should. But Dagger solves a different problem.

Dagger describes itself as "a modular, composable platform designed to replace complex systems glued together with artisanal scripts." Translation: it's for when your `Makefile` starts growing `if` statements for different OSes, your CI YAML becomes a nightmare, and "works on my machine" becomes a daily standup punchline.

**What makes it different:**

| Problem | Shell scripts / Makefiles | Dagger |
|---------|---------------------------|--------|
| "Works on my machine" | Runs on your host, inherits your mess | Every function runs in a container—same environment everywhere |
| Caching | DIY or pray Docker layer cache cooperates | Built-in intelligent caching at the function level |
| CI/local parity | Write it twice (local script + CI YAML) | Write once, run anywhere—laptop, GitHub Actions, whatever |
| Reproducibility | "Did you install the right Go version?" | Pinned container images, sandboxed execution |
| Debugging CI failures | Push, wait, read logs, cry, repeat | Run the exact same containers locally, step through failures |
| Composability | Source a bunch of scripts and hope | Type-safe functions you can chain together |


### Prerequisites

- [Dagger CLI](https://docs.dagger.io/install) installed
- Docker (or a compatible container runtime) running

### Available functions

| Function | What it does |
|----------|--------------|
| `RunRobotTests` | Spins up a Go container, installs RCC from our releases, runs the robot tests |
| `ContainerEcho` | Returns a container that echoes a string (sanity check) |
| `GrepDir` | Greps through a directory in a container |

### Running Dagger locally

From the repo root:

```bash
# Run the robot tests in a container (the big one)
dagger call run-robot-tests --source .

# Sanity check that Dagger is working
dagger call container-echo --string-arg "Hello from the container"

# Grep through the codebase in a container
dagger call grep-dir --directory-arg . --pattern "TODO"
```

**What `RunRobotTests` actually does:**

1. Pulls a `golang:1.26.9` base image
2. Installs curl, git, and friends
3. Downloads `rcc` from [our releases](https://github.com/joshyorko/rcc/releases)
4. Mounts your source directory (read-only, nothing gets mutated on your host)
5. Runs the holotree setup and robot tests
6. Caches Go modules and `.robocorp` home (subsequent runs are fast)

First run downloads images and builds caches—grab a coffee. Subsequent runs reuse everything that hasn't changed.

### When to use what

| Scenario | Recommendation |
|----------|----------------|
| Quick iteration while coding | `inv test`, `inv local`, `rcc run` directly |
| Pre-push sanity check | `dagger call run-robot-tests --source .` |
| Debugging "works locally, fails in CI" | Dagger—same containers as CI |
| Experimenting with environment changes | Dagger—won't pollute your host |

### Extending the Dagger module

The module lives in `.dagger/main.go`. It's just Go code with the Dagger SDK—add functions, chain containers, go wild. Just remember: if you break it, you own the pieces.

```go
// Example: Add a new function to run unit tests
func (m *RccCi) RunUnitTests(ctx context.Context, source *dagger.Directory) (string, error) {
    return dag.Container().
        From("golang:1.26.9").
        WithMountedDirectory("/src", source).
        WithWorkdir("/src").
        WithExec([]string{"go", "test", "./..."}).
        Stdout(ctx)
}
```

Then call it:

```bash
dagger call run-unit-tests --source .
```

The Dagger docs are solid: https://docs.dagger.io/quickstart

---

## Embedded assets are part of the build (don't skip this)

RCC embeds assets into the binary (templates, YAML, Python helpers, docs).

If you see errors like `pattern assets/*.py: no matching files found` or builds failing with missing `blobs/assets/*`, your robot is telling you it needs something. Run:

- **Toolkit**: `rcc run -r developer/toolkit.yaml --dev -t assets`
- **Manual**: `inv assets`

Generated outputs include:

- `blobs/assets/*.zip` (zipped `templates/*/`)
- Copies of `assets/*.yaml`, `assets/*.py`, `assets/*.txt`
- `blobs/docs/*.md`

What's that smell? `pattern assets/*.py: no matching files`? Duplication of that error across your terminal? Better deal with it immediately by running `inv assets`.

---

## Micromamba downloads and restricted networks

Some workflows download Micromamba binaries during asset preparation. In a perfect world, this just works. In the real world, corporate firewalls exist.

- In this repo, `tasks.py` downloads from the official source (`micro.mamba.pm`) in task `micromamba`.
- You can override the base URL via `RCC_MICROMAMBA_BASE`.

If you're in a restricted network and downloads fail, you can still do development builds by creating placeholder files (see `.github/copilot-instructions.md`). We've been there. The future you will thank the present you for reading this section before panicking.

---

## Before you open a PR

### Code style

- Run `gofmt` (CI expects it, and CI is relentless)
- Prefer small, composable functions and table-driven tests
- Avoid leaking OS-specific logic across `command_*.go` files
- Don't hardcode endpoints or credentials. Prefer the documented `RCC_ENDPOINT_*` overrides in `README.md`

*Note: You should never commit credentials in your code in a real project. Here we emphasize that because top-notch security is everyone's job.*

### Minimum verification

Before you push, run the tests. CI will run them anyway, and you'll save yourself a round trip.

**Pick one of these flows:**

**Toolkit:**

```bash
rcc run -r developer/toolkit.yaml --dev -t unitTests
rcc run -r developer/toolkit.yaml --dev -t local
rcc run -r developer/toolkit.yaml -t robot
```

**Manual:**

```bash
inv test
inv local
inv robot  # after inv robotsetup
```

**Optional sanity checks** (after building):

```bash
./build/rcc --help
./build/rcc version
```

If both of those work, you're probably in good shape. Probably.

---

## Contribution process

1. **Search existing issues**: https://github.com/joshyorko/rcc/issues
   - Someone may have already documented your particular flavor of pain.
2. **For non-trivial changes**, open an issue first so the approach is agreed.
   - This saves everyone time, especially you.
3. **Create a branch**, make focused commits.
4. **Open a PR** against `main`.
   - Link the issue.
   - Include what you ran (e.g., `inv test`, `inv robot`) and any key output/logs.
   - We appreciate PRs that help us help you get merged.

How do you know how to structure your contribution beforehand? Well, you don't! But you can still have a high-level guess and start with that guess. Refactoring is a normal part of software development.

---

## Good places to contribute

If you want ideas that are *actually actionable*, here are recurring areas where contributions make a real difference:

| Area | What's involved |
|------|-----------------|
| **Go upgrades** | When moving Go versions, update CI (`.github/workflows/rcc.yaml`) and confirm builds/tests. |
| **Micromamba upgrades** | Bump `assets/micromamba_version.txt`, regenerate assets, verify downloads + builds. |
| **Docs & recipes** | Update docs under `docs/`, then run `inv toc`. The world needs more good docs. |
| **Acceptance tests** | Improve Robot Framework suites under `robot_tests/` (especially cross-platform behavior). |

---

## Troubleshooting common issues

### "pattern assets/*.py: no matching files found"

You forgot to generate assets. Run `inv assets` or the toolkit equivalent. Nifty!

### Tests fail with GOARCH mismatch

`tasks.py` forces `GOARCH=amd64`. If you're running `go test` directly, set it yourself:

```bash
GOARCH=amd64 go test ./...
```

### Micromamba download failures

Restricted network? Override with `RCC_MICROMAMBA_BASE` or create placeholders per `.github/copilot-instructions.md`.

### Something else entirely

Gather evidence (logs, console outputs, stack traces, screenshots), and open an issue. We'll figure it out together.

---

## Bravissimo! 👏

Thanks for helping keep RCC repeatable and contained. Now go automate something that no one wants to do manually—and save someone from copy-paste hell.

As a nice bonus, you might even ship a feature that helps others pay their rent, fix their car, and take care of their dog. Can do!
