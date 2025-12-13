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
- Go **1.20.7**
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
| Tooling info | `rcc run -r developer/toolkit.yaml --dev -t tools` |

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

- Go **1.20.x** (CI uses `1.20.x`. Mismatched versions lead to mysterious build failures. Ask us how we know.)
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
| `common/`, `pathlib/`, `shell/` | Shared libraries/utilities |
| `assets/` | *Source* assets that get embedded into the binary |
| `blobs/` | Generated/embedded assets (**do not edit by hand**—the build will overwrite your tears) |
| `templates/` | Robot templates zipped into `blobs/assets/*.zip` |
| `robot_tests/` | Robot Framework acceptance tests (HTML logs under `tmp/output/`) |
| `developer/` | The "repeatable, contained" dev environment bootstrap |
| `.dagger/` | Dagger CI module for containerized builds and tests |

---

## Dagger: CI you can run locally (before CI yells at you)

The `.dagger/` directory contains a [Dagger](https://dagger.io) module for running builds and tests in containers. Think of it as "CI on your laptop"—same containers, same isolation, no more "but it passed locally."

*Yes, we use containers to test a tool that creates isolated environments. It's containers all the way down. 🐳*

### Prerequisites

- [Dagger CLI](https://docs.dagger.io/install) installed
- Docker (or a compatible container runtime) running

### Available functions

| Function | What it does |
|----------|--------------|
| `RunRobotTests` | Spins up a Go container, installs RCC from our releases, runs `rcc run -r developer/toolkit.yaml -t robot` |
| `ContainerEcho` | Returns a container that echoes a string (useful for sanity checks) |
| `GrepDir` | Greps through a directory in a container (for when you need to search without polluting your host) |

### Running Dagger locally

From the repo root:

```bash
# Run the robot tests in a container
dagger call run-robot-tests --source .

# Sanity check that Dagger is working
dagger call container-echo --string-arg "Hello from the container"

# Grep through the codebase in a container
dagger call grep-dir --directory-arg . --pattern "TODO"
```

The `RunRobotTests` function:
1. Pulls a `golang:1.22` base image
2. Installs curl, git, and friends
3. Downloads `rcc` from [our releases](https://github.com/joshyorko/rcc/releases)
4. Mounts your source directory
5. Runs the holotree setup and robot tests
6. Caches Go modules and the `.robocorp` home directory (so subsequent runs are faster)

### Why bother?

- **Catch CI failures before pushing.** "Works on my machine" is not a valid excuse when you can run the same container locally.
- **Reproducible builds.** Same Go version, same dependencies, same environment—every time.
- **Cacheable.** Dagger caches aggressively, so subsequent runs are fast.

### Extending the Dagger module

The module lives in `.dagger/main.go`. It's just Go code—add functions, modify the container setup, go wild. Just remember: if you break it, you own the pieces.

```go
// Example: Add a new function to run unit tests
func (m *RccCi) RunUnitTests(ctx context.Context, source *dagger.Directory) (string, error) {
    return dag.Container().
        From("golang:1.22").
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
