# RCC

[![Build (caching)](https://github.com/joshyorko/rcc/actions/workflows/rcc.yaml/badge.svg)](https://github.com/joshyorko/rcc/actions/workflows/rcc.yaml)
[![Go Report Card](https://goreportcard.com/badge/github.com/joshyorko/rcc)](https://goreportcard.com/report/github.com/joshyorko/rcc)
[![codecov](https://codecov.io/gh/joshyorko/rcc/branch/master/graph/badge.svg)](https://codecov.io/gh/joshyorko/rcc)
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

## Installing RCC from the command line

> Links to changelog and different versions [available here](https://github.com/joshyorko/rcc/releases)

### Windows

1. Open the command prompt
1. Download: `curl -o rcc.exe https://github.com/joshyorko/rcc/releases/latest/download/rcc-windows64.exe`
1. [Add to system path](https://www.architectryan.com/2018/03/17/add-to-the-path-on-windows-10/): Open Start -> `Edit the system environment variables`
1. Test: `rcc`

### macOS

1. Open the terminal
1. Download: `curl -o rcc https://github.com/joshyorko/rcc/releases/latest/download/rcc-darwin64`
1. Make the downloaded file executable: `chmod a+x rcc`
1. Add to path: `sudo mv rcc /usr/local/bin/`
1. Test: `rcc`

### Linux

1. Open the terminal
1. Download: `curl -o rcc https://github.com/joshyorko/rcc/releases/latest/download/rcc-linux64`
1. Make the downloaded file executable: `chmod a+x rcc`
1. Add to path: `sudo mv rcc /usr/local/bin/`
1. Test: `rcc`

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

Example (zsh):

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

# Validate your overrides
build/rcc configuration diagnostics --quick --json | jq .
```

Local settings file: write a `settings.yaml` to `$RCC_HOME/settings.yaml` with an `endpoints:` section. See `assets/rcc_settings.yaml` for the full shape; any key you set there will override the built-in defaults.

### Notes

Micromamba is embedded into `rcc` and extracted locally at runtime; no live download is needed. If you rebuild assets yourself, you can change the micromamba download base used during asset preparation via:

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
