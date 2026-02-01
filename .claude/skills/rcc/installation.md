# RCC & Action Server Installation Guide

## Recommended: Homebrew (macOS & Linux)

Homebrew is the **recommended** installation method for both macOS and Linux. It provides automatic updates and easy management.

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
# Update Homebrew and all casks
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

Add to your `Brewfile`:

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

## Alternative: PyPI (Action Server only)

```bash
pip install sema4ai-action-server
action-server version
```

---

## CI/CD Installation

### GitHub Actions

```yaml
- name: Install RCC & Action Server
  run: |
    # Install Homebrew (if not present)
    if ! command -v brew &> /dev/null; then
      /bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"
      echo 'eval "$(/home/linuxbrew/.linuxbrew/bin/brew shellenv)"' >> ~/.bashrc
      eval "$(/home/linuxbrew/.linuxbrew/bin/brew shellenv)"
    fi

    # Install tools
    brew install --cask joshyorko/tools/rcc
    brew install --cask joshyorko/tools/action-server
```

Or direct download (faster in CI):

```yaml
- name: Install RCC
  run: |
    curl -o rcc https://github.com/joshyorko/rcc/releases/latest/download/rcc-linux64
    chmod +x rcc && sudo mv rcc /usr/local/bin/
    rcc version
```

### Docker

```dockerfile
# Using direct download
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
# Check versions
rcc version
action-server version

# Run diagnostics
rcc configure diagnostics
```

### Shell Completion (Optional)

If commands aren't found after installation:

```bash
# Refresh shell hash table
hash -r

# Or start a new terminal session
```

### Initialize Shared Holotree (Optional, for teams)

```bash
# Requires sudo/admin
sudo rcc holotree shared --enable

# Initialize for current user
rcc holotree init
```

---

## Troubleshooting

### "Command not found" after installation

```bash
# Refresh shell
hash -r

# Or add to PATH manually
export PATH="/usr/local/bin:$PATH"
```

### Homebrew cask conflicts

If you have the upstream `robocorp/tools/rcc` installed:

```bash
# Uninstall upstream first
brew uninstall --cask robocorp/tools/rcc

# Install community fork
brew install --cask joshyorko/tools/rcc
```

### Permission denied on Linux

```bash
# Ensure binary is executable
chmod +x /usr/local/bin/rcc

# If using shared holotree
sudo chmod -R 777 /opt/robocorp
```

---

## Resources

- **Homebrew Tap**: https://github.com/joshyorko/homebrew-tools
- **RCC Releases**: https://github.com/joshyorko/rcc/releases
- **Action Server Releases**: https://github.com/joshyorko/actions/releases
