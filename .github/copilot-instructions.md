
# RCC (Robocorp Control Client)

RCC is a Go-based CLI tool for creating, managing, and distributing Python-based automation packages with isolated environments. It uses conda/micromamba for Python environment management and supports cross-platform builds (Linux, Windows, macOS).

**Always reference these instructions first and fallback to search or bash commands only when you encounter unexpected information that does not match the info here.**

## Working Effectively

### Prerequisites and Setup
- Install Go 1.20: `go version` (should show 1.20.x)
- Install Python 3.10 or later: `python3 --version` 
- Install invoke for build automation: `python3 -m pip install invoke`
- Verify tools: `inv -l` (should list available tasks)

### Build Commands
- **NEVER CANCEL**: All builds require patience. Set timeouts to 60+ minutes minimum.
- Full cross-platform build: `inv build` -- takes ~35 seconds but **NEVER CANCEL, SET 60+ MINUTE TIMEOUT**
- Local platform only: `inv local` -- takes ~10 seconds but **NEVER CANCEL, SET 15+ MINUTE TIMEOUT**  
- Build via Go directly: `go build -o build/ ./cmd/...` -- takes ~2-3 seconds but **NEVER CANCEL, SET 15+ MINUTE TIMEOUT**

### Testing Commands
- **NEVER CANCEL**: Test commands can be slow due to environment creation.
- Unit tests: `inv test` -- takes ~0.6 seconds but **NEVER CANCEL, SET 15+ MINUTE TIMEOUT**
- Robot Framework acceptance tests: `inv robot` -- takes ~5-15 minutes **NEVER CANCEL, SET 45+ MINUTE TIMEOUT**
- Specific unit test packages: `GOARCH=amd64 go test ./common ./pathlib ./hamlet ./journal ./set ./settings ./xviper` -- takes ~0.6 seconds

### Development Workflow
- Show project info: `inv what`
- Check tools: `inv tooling`
- Clean build artifacts: `inv clean`
- Update documentation TOC: `inv toc`

## Developer toolkit (developer/)

Use the bundled developer toolkit to bootstrap a consistent env and run common tasks via rcc. This requires an existing rcc binary on your PATH (any recent older rcc works) since it self-bootstraps.

- Quick robot smoke test (writes logs to `tmp/output/log.html`):
    - `rcc run -r developer/toolkit.yaml -t robot`

- Developer tasks (use `--dev`):
    - Unit tests: `rcc run -r developer/toolkit.yaml --dev -t unitTests`
        - Outside of invoke, set `GOARCH=amd64` when using `go test` directly since some tests assume it.
    - Local build (current OS): `rcc run -r developer/toolkit.yaml --dev -t local`
    - Cross-platform build: `rcc run -r developer/toolkit.yaml --dev -t build`
    - Show tools: `rcc run -r developer/toolkit.yaml --dev -t tools`
    - Update docs TOC: `rcc run -r developer/toolkit.yaml --dev -t toc`

- How it’s wired:
    - `developer/toolkit.yaml` defines tasks and devTasks that call `python developer/call_invoke.py <task>`.
    - `developer/call_invoke.py` runs `invoke <task>` in the repo root so you benefit from the same Invoke tasks described below.
    - Environments are declared in `developer/setup.yaml` and pinned to:
        - Python 3.10.15, Invoke 2.2.0
        - Robot Framework 6.1.1 (matches `robot_requirements.txt`)
        - Go 1.20.7, Git 2.46.0

Notes
- If rcc downloads are blocked in your network, you can still use the direct Invoke/Go commands documented in this file.
- The toolkit writes artifacts under `tmp/` and respects `.gitignore` specified in `developer/toolkit.yaml`.

## Critical Build Requirements

### Asset Preparation
RCC requires embedded assets to build. If you encounter "pattern assets/*.py: no matching files found" or similar:

1. **Prepare basic assets**: `inv support` then copy assets manually:
   ```bash
   mkdir -p blobs/assets blobs/assets/man blobs/docs
   cp assets/*.py assets/*.txt assets/*.yaml blobs/assets/ 2>/dev/null || true
   cp assets/man/*.txt blobs/assets/man/
   cp docs/*.md blobs/docs/
   ```

2. **Create template assets** (required for build):
   ```bash
   python3 -c "
   import os, shutil, glob
   from zipfile import ZIP_DEFLATED, ZipFile
   for directory in glob.glob('templates/*/'):
       basename = os.path.basename(os.path.dirname(directory))
       assetname = f'blobs/assets/{basename}.zip'
       if not os.path.exists(assetname):
           with ZipFile(assetname, 'w', ZIP_DEFLATED) as zipf:
               for root, _, files in os.walk(directory):
                   for file in files:
                       file_path = os.path.join(root, file)
                       arcname = os.path.relpath(file_path, directory)
                       zipf.write(file_path, arcname)
   "
   ```

3. **Handle micromamba dependency**: If external downloads fail, create placeholders:
   ```bash
   touch blobs/assets/micromamba.linux_amd64.gz blobs/assets/micromamba.darwin_amd64.gz blobs/assets/micromamba.windows_amd64.gz
   ```

### Network Dependency Issues
- Micromamba downloads from `downloads.robocorp.com` may fail due to network restrictions
- If `inv test` or `inv build` fails with "Could not resolve host: downloads.robocorp.com", use the placeholder workaround above
- **Document this in your changes**: "Note: Micromamba downloads may fail due to network restrictions. Use placeholder files for development builds."

## Validation Scenarios

### Basic Functionality Test
After any changes, always validate:
1. Build succeeds: `inv build` (35 seconds)
2. Binary works: `./build/rcc --help` and `./build/rcc version`
3. Core commands: `./build/rcc create --help` and `./build/rcc man changelog | head -10`

### Development Validation
1. Unit tests pass: `GOARCH=amd64 go test ./common ./pathlib ./hamlet ./journal ./set ./settings ./xviper`
2. Robot Framework setup: `python3 -m pip install -r robot_requirements.txt`
3. Simple Robot test: `robot -L INFO -d tmp/output robot_tests/documentation.robot` (8 tests should pass)

### Full Integration Test
For significant changes, run the complete test suite:
```bash
# This takes 10-30 minutes total - NEVER CANCEL
inv robotsetup  # Install Robot Framework dependencies
inv robot       # Run full acceptance test suite
```

## Repository Structure

### Key Directories
- `cmd/` - CLI command implementations (start here for RCC functionality)
- `blobs/` - Embedded assets and binary data  
- `assets/` - Source files for embedded assets
- `templates/` - Robot project templates (python, standard, extended)
- `robot_tests/` - Robot Framework acceptance tests
- `docs/` - Documentation files
- `build/` - Build output directory

### Key Files  
- `go.mod` - Go module definition (Go 1.20)
- `tasks.py` - Invoke task definitions (build automation)
- `robot_requirements.txt` - Robot Framework version (6.1.1)
- `.github/workflows/rcc.yaml` - CI/CD pipeline

### Common Build Outputs
```
ls -la build/
total 30440
-rwxr-xr-x rcc        # Main RCC binary (~18MB)
-rwxr-xr-x rccremote  # Remote RCC binary (~13MB)
linux64/             # Linux-specific builds
windows64/            # Windows builds (rcc.exe)
macos64/              # macOS builds
```

## Timing Expectations

- **Go dependency download**: 5-15 seconds (first time only)
- **Asset preparation**: 2-5 seconds  
- **Go build**: 2-3 seconds
- **Full invoke build**: 30-45 seconds
- **Unit tests**: 0.5-1 seconds
- **Simple Robot test**: 10-30 seconds  
- **Full Robot test suite**: 5-30 minutes depending on system

**CRITICAL**: Always set timeouts to 2-3x these estimates. **NEVER CANCEL** builds or tests that appear to hang.

## Common Tasks Reference

### View Repository Root
```
ls -la
.github/workflows/    # CI/CD workflows
assets/              # Source assets
blobs/               # Embedded assets  
cmd/                 # CLI commands
common/              # Common utilities
docs/                # Documentation
robot_tests/         # Acceptance tests
templates/           # Project templates
go.mod go.sum        # Go dependencies
tasks.py             # Build tasks
```

### View go.mod
```
module github.com/joshyorko/rcc
go 1.20
require (
    github.com/spf13/cobra v1.7.0
    github.com/spf13/viper v1.17.0
    golang.org/x/sys v0.13.0
    gopkg.in/yaml.v2 v2.4.0
    ...
)
```

### Available Invoke Tasks
```
inv -l
assets        Prepare asset files
build         Build executables  
clean         Remove build directory
local         Build local, operating system specific rcc
robot         Run robot tests on local application
robotsetup    Setup build environment
test          Run tests
what          Show latest HEAD with stats
```

## CI/CD Information

The GitHub Actions workflow (`.github/workflows/rcc.yaml`) builds on:
- Ubuntu (Linux builds and Robot tests)
- Windows (Robot tests)
- Uses Go 1.20 and Python 3.10
- Uploads artifacts for all platforms
- Timeout expectations align with local development

## Troubleshooting

### Build Fails with "pattern assets/*.py: no matching files found"
- Run asset preparation commands in "Critical Build Requirements" section above

### "Could not resolve host: downloads.robocorp.com"  
- Network restrictions blocking micromamba downloads
- Use placeholder file workaround in "Network Dependency Issues" section

### "No idea what 'false' is!"
- invoke parameter parsing issue
- Use `inv local` without parameters, or `go build` directly

### Robot tests fail to start
- Ensure Robot Framework installed: `python3 -m pip install -r robot_requirements.txt`
- Use `robot` command directly, not `python3 -m robot`
- Ensure `tmp/output` directory exists: `mkdir -p tmp/output`

## Never Do This
- **DO NOT** cancel builds that take longer than expected - Go builds can be slow on first run
- **DO NOT** skip asset preparation steps before building 
- **DO NOT** assume network dependencies will always work - have workarounds ready
- **DO NOT** modify core build processes without understanding the embedded asset system
- **DO NOT** use default timeouts for long-running operations
