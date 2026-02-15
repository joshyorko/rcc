# RCC & Action Server Installation Guide (Community Fork)

This guide is aligned to the community toolchain:
- RCC: `joshyorko/rcc`
- Action Server: `joshyorko/actions` (`community` branch)

Recommended install order:
1. Homebrew casks (macOS/Linux)
2. Source build from `community` branch (when you need fork-specific behavior)
3. Direct binary downloads (CI/CD, containers, air-gapped staging)
4. PyPI install (`sema4ai-action-server`) for quick compatibility setup

---

## Recommended: Homebrew (macOS & Linux)

### RCC Installation

```bash
# One-liner (recommended)
brew install --cask joshyorko/tools/rcc

# Or tap first, then install
brew tap joshyorko/tools
brew install --cask rcc

# Verify installation
rcc version
```

### Action Server Installation

```bash
# One-liner
brew install --cask joshyorko/tools/action-server

# Or after tapping
brew install --cask action-server

# Verify installation
action-server version
```

### Updates

```bash
brew update
brew upgrade --cask rcc
brew upgrade --cask action-server
```

### Uninstall

```bash
brew uninstall --cask joshyorko/tools/rcc
brew uninstall --cask joshyorko/tools/action-server
```

### Brewfile (for dotfiles/DevContainers)

```ruby
tap "joshyorko/tools"
cask "rcc"
cask "action-server"
```

### Platform Support

| Platform | RCC | Action Server |
|----------|-----|---------------|
| Linux x64 | ✅ Native | ✅ Native |
| macOS Intel | ✅ Native | ✅ Native |
| macOS Apple Silicon | ✅ Native | ✅ Native |

---

## Build from Source (Community Branch)

Use this when you need community-branch frontend behavior or build-time customization.

```bash
# Clone community fork
git clone https://github.com/joshyorko/actions.git
cd actions

# Build action-server binary (community tier)
rcc run -r action_server/developer/toolkit.yaml -t community

# Output binary
action_server/dist/final/action-server
```

Optional frontend-only build:

```bash
cd action_server/frontend
npm ci
inv build-frontend --tier=community
```

---

## Alternative: Direct Binary Download

For CI/CD, Docker, or environments without Homebrew.

### RCC

```bash
# Linux x64
curl -o rcc https://github.com/joshyorko/rcc/releases/latest/download/rcc-linux64
chmod +x rcc && sudo mv rcc /usr/local/bin/

# macOS Intel
curl -o rcc https://github.com/joshyorko/rcc/releases/latest/download/rcc-macos64
chmod +x rcc && sudo mv rcc /usr/local/bin/

# macOS Apple Silicon
curl -o rcc https://github.com/joshyorko/rcc/releases/latest/download/rcc-macosarm64
chmod +x rcc && sudo mv rcc /usr/local/bin/

# Windows (PowerShell)
Invoke-WebRequest -Uri "https://github.com/joshyorko/rcc/releases/latest/download/rcc-windows64.exe" -OutFile "rcc.exe"
Move-Item rcc.exe C:\Windows\System32\
```

### Action Server

```bash
# Linux x64
curl -o action-server https://github.com/joshyorko/actions/releases/latest/download/action-server-linux64
chmod +x action-server && sudo mv action-server /usr/local/bin/

# macOS Intel
curl -o action-server https://github.com/joshyorko/actions/releases/latest/download/action-server-macos64
chmod +x action-server && sudo mv action-server /usr/local/bin/

# macOS Apple Silicon
curl -o action-server https://github.com/joshyorko/actions/releases/latest/download/action-server-macosarm64
chmod +x action-server && sudo mv action-server /usr/local/bin/
```

---

## Alternative: PyPI (Compatibility Path)

```bash
pip install sema4ai-action-server
action-server version
```

Use source/binary installs from `joshyorko/actions` when you specifically need community-fork build behavior.

---

## Optional: Community Work Items Library

Install the published drop-in work-items replacement:

```bash
pip install actions-work-items
```

Import style:

```python
from actions.work_items import inputs, outputs
```

---

## CI/CD Installation

### GitHub Actions

```yaml
- name: Install RCC & Action Server
  run: |
    if ! command -v brew &> /dev/null; then
      /bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"
      echo 'eval "$(/home/linuxbrew/.linuxbrew/bin/brew shellenv)"' >> ~/.bashrc
      eval "$(/home/linuxbrew/.linuxbrew/bin/brew shellenv)"
    fi

    brew install --cask joshyorko/tools/rcc
    brew install --cask joshyorko/tools/action-server
```

Or direct downloads:

```yaml
- name: Install RCC
  run: |
    curl -o rcc https://github.com/joshyorko/rcc/releases/latest/download/rcc-linux64
    chmod +x rcc && sudo mv rcc /usr/local/bin/
    rcc version

- name: Install Action Server
  run: |
    curl -o action-server https://github.com/joshyorko/actions/releases/latest/download/action-server-linux64
    chmod +x action-server && sudo mv action-server /usr/local/bin/
    action-server version
```

### Docker

```dockerfile
RUN curl -o /usr/local/bin/rcc \
    https://github.com/joshyorko/rcc/releases/latest/download/rcc-linux64 && \
    chmod +x /usr/local/bin/rcc

RUN curl -o /usr/local/bin/action-server \
    https://github.com/joshyorko/actions/releases/latest/download/action-server-linux64 && \
    chmod +x /usr/local/bin/action-server
```

---

## Post-Installation Setup

### Verify Installation

```bash
rcc version
action-server version
rcc configure diagnostics
```

### Shell Refresh

```bash
hash -r
```

### Initialize Shared Holotree (Optional, team setups)

```bash
sudo rcc holotree shared --enable
rcc holotree init
```

---

## Troubleshooting

### "Command not found" after install

```bash
hash -r
export PATH="/usr/local/bin:$PATH"
```

### Homebrew cask conflicts

```bash
# Remove upstream casks first if present
brew uninstall --cask robocorp/tools/rcc || true
brew uninstall --cask robocorp/tools/action-server || true

# Install community casks
brew install --cask joshyorko/tools/rcc
brew install --cask joshyorko/tools/action-server
```

### Permission denied on Linux

```bash
chmod +x /usr/local/bin/rcc
chmod +x /usr/local/bin/action-server
sudo chmod -R 777 /opt/robocorp
```

---

## Resources

- **Community Actions Repo**: https://github.com/joshyorko/actions/tree/community
- **Homebrew Tap**: https://github.com/joshyorko/homebrew-tools
- **RCC Releases**: https://github.com/joshyorko/rcc/releases
- **Action Server Releases**: https://github.com/joshyorko/actions/releases
- **actions-work-items (PyPI)**: https://pypi.org/project/actions-work-items/
