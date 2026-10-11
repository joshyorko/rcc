# RCC Roadmap

This document outlines the development direction for the community fork of RCC.

The goal isn't to recreate Robocorp or compete with Sema4.ai. It's to **finish
the architectural story** they started: RCC + Holotree + rccremote as an open,
vendor-neutral foundation for automation infrastructure.

---

## Guiding Principles

1. **RCC is infrastructure, not product.** Keep RCC focused on environments.
   Control rooms, orchestration, and UIs are layers that sit on top.

2. **Environments are data.** Holotree's content-addressed architecture is the
   innovation. Protect and extend it.

3. **rccremote is undersold.** The peer-to-peer distribution fabric exists but
   is poorly documented and hard to use. Fix that.

4. **Vendor-neutral by default.** No hard-coded endpoints, no telemetry, no
   phone-home. Configure everything or configure nothing.

5. **Agents are clients, not the core.** AI agents need reliable Python
   environments. RCC provides those. Don't pivot RCC into an agent framework.

---

## Phase 1: Stabilization (Current)

**Status: In Progress**

Decouple from Robocorp/Sema4.ai infrastructure and establish the fork as a
standalone, maintainable project.

### Completed

- [x] Remove telemetry code paths (not just config, actual removal)
- [x] Empty all cloud endpoints by default
- [x] Templates served from community GitHub repositories
- [x] Version checking via GitHub releases
- [x] All endpoints configurable via `RCC_ENDPOINT_*` environment variables
- [x] Document the decoupling in changelog and history

### Remaining

- [ ] Audit remaining Robocorp/Sema4.ai references in codebase
- [ ] Update all embedded documentation (`rcc man *` commands)
- [ ] Verify robot tests pass in CI with decoupled defaults
- [ ] Tag and release v18.12.0

---

## Phase 2: rccremote UX Revamp

**Status: Planning**

Make rccremote feel like `docker login` + `docker pull` for environments.

### What Already Exists

RCC already has the core remote functionality:

- **`rccremote` binary** — separate server binary with `--hostname`, `--port`, `--domain`, `--hold` flags
- **`rcc holotree pull`** — pull catalogs from remote with `--origin` flag
- **`RCC_REMOTE_ORIGIN`** — env var to set default remote URL
- **`RCC_REMOTE_AUTHORIZATION`** — env var for token-based auth

The functionality works. The problem is discoverability and documentation.

### Goals

The "aha moment" should be:

```bash
# On server (IT team builds environments once)
rccremote --hostname 0.0.0.0 --port 4653

# On client (developers get instant environments)
export RCC_REMOTE_ORIGIN=http://rcc-server:4653
rcc holotree pull -r robot.yaml
rcc run  # instant, no 10-minute build
```

### Tasks

- [ ] **Integrate rccremote into main RCC CLI** (optional, for discoverability)
  - `rcc remote serve` — wrapper around rccremote binary
  - `rcc remote status` — show configured remote, test connectivity
  - `rcc remote catalogs` — list available catalogs on remote

- [ ] **Documentation** (priority)
  - `rcc man remote` — complete guide to rccremote
  - Quick start: localhost development workflow
  - Production: reverse proxy patterns (nginx/Caddy/Traefik)
  - Workflow examples: "cold build 10m → with remote 10s"
  - Troubleshooting guide for TLS, firewalls, proxies

- [ ] **Simple default configurations**
  - Example `settings.yaml` for clients
  - Example systemd unit file for rccremote server
  - Docker compose example for rccremote

- [ ] **Better error messages**
  - Clear guidance when `RCC_REMOTE_ORIGIN` not set
  - Connection failure diagnostics

---

## Phase 3: "Git for Environments" Mental Model

**Status: Future**

Anchor RCC's UX around the Git analogy. Holotree already works like Git's
object database—make the commands reflect that.

### What Already Exists

Holotree has substantial functionality, just not Git-like naming:

| Existing Command | What It Does |
|------------------|--------------|
| `rcc ht list` | List holotree spaces (like `git branch`) |
| `rcc ht catalogs` | List available catalogs with metadata |
| `rcc ht statistics` | Build/runtime stats over time |
| `rcc ht check` | Verify library integrity |
| `rcc ht export` | Export catalog to hololib.zip (local) |
| `rcc ht pull` | Download catalog from remote |
| `rcc ht hash` | Calculate blueprint hash |
| `rcc ht blueprint` | Verify blueprint exists in library |
| `rcc configuration cleanup` | Remove old environments/caches |

### Proposed Additions

```bash
rcc ht status          # Summary: spaces, catalogs, remote config
rcc ht diff            # Compare two blueprints (show what changed)
rcc ht push            # Upload catalog to remote (inverse of pull)
rcc ht gc              # Alias for cleanup focused on holotree
```

### Vocabulary Alignment

| Git | RCC Equivalent |
|-----|----------------|
| repository | holotree |
| commit | catalog/blueprint |
| object | hololib layer |
| remote | rccremote server |
| clone | `rcc ht pull` + `rcc ht vars` |
| push | `rcc ht export` → upload (proposed) |
| log | `rcc ht statistics` |
| status | `rcc ht list` + `rcc ht catalogs` |

### Documentation Updates

- Rewrite `rcc man holotree` around this mental model
- Add "RCC for Git users" guide
- Update error messages to use consistent terminology

---

## Phase 4: Control Room Foundation

**Status: Future**

RCC shouldn't *be* a control room, but it should be easy to build one on top.

### What RCC Already Provides

| Feature | Commands |
|---------|----------|
| Environment management | `rcc ht vars`, `rcc ht list`, `rcc ht catalogs` |
| Environment distribution | `rccremote`, `rcc ht pull`, `rcc ht export` |
| Robot execution | `rcc run`, `rcc task run`, `rcc task testrun` |
| Assistant mode | `rcc assistant run`, `rcc assistant list` |
| Diagnostics | `rcc diagnostics`, `rcc netdiagnostics` |
| Shell access | `rcc task shell`, `rcc task script` |

### What a Control Room Adds

- Worker registration and heartbeats
- Job queuing and scheduling
- Work item management
- Run history and artifacts storage
- Web UI for operations

### Proposed RCC Enhancements

- [ ] **Structured run output** — `--json` flag for machine-parseable run results
- [ ] **Webhook/callback support** — `--on-complete <url>` to notify control room
- [ ] **Worker mode** — `rcc worker start --control-room <url>` that polls for jobs
- [ ] **Health endpoint** — `rcc worker health` for load balancer checks
- [ ] **Clean API boundaries** — document which commands are stable for automation

The goal is: anyone can build a control room on top of RCC without forking
or monkey-patching. The integration points are documented and stable.

---

## Phase 5: Agent Integration

**Status: Future**

AI agents need Python environments. RCC provides those. Keep the integration
clean and don't turn RCC into an agent framework.

### Integration Patterns

- **MCP Server** — expose RCC operations as Model Context Protocol tools
- **Action semantics** — support `@action`-style decorators (like Sema4.ai)
- **LangGraph/LangChain** — RCC as an environment tool in agent workflows
- **OpenAI function calling** — RCC operations as callable functions

### What RCC Should NOT Do

- Don't build an agent runtime into RCC
- Don't add LLM dependencies to the binary
- Don't pivot the project toward "AI-first"

Agents are a use case, not the identity. RCC is environment infrastructure.

---

## Non-Goals

Things this fork will **not** pursue:

1. **Competing SaaS offering** — We're not building Robocorp Cloud 2.0

2. **Proprietary features** — Everything stays Apache 2.0

3. **Agent framework** — RCC provides environments for agents, not agent logic

4. **Backward compatibility with Sema4.ai** — If you need their control room,
   use their RCC. This fork is for self-hosted, vendor-neutral deployments.

5. **Windows-first development** — We'll maintain Windows support, but Linux
   is the primary development and testing platform.

---

## Contributing

See [CONTRIBUTING.md](../CONTRIBUTING.md) for development setup.

Priority areas where contributions are welcome:

1. **Documentation** — rccremote especially needs better docs
2. **Testing** — More robot tests for edge cases
3. **Platform support** — ARM64 binaries, container images
4. **Control room integrations** — Reference implementations welcome

---

## Version Timeline

| Version | Focus | Status |
|---------|-------|--------|
| v18.12.0 | Decoupling complete, vendor-neutral defaults | In Progress |
| v18.13.0 | rccremote UX improvements | Planning |
| v19.0.0 | "Git for environments" UX, breaking changes OK | Future |
| v20.0.0 | Control room foundation APIs | Future |
