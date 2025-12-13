# History of RCC

> "Repeatable, movable and isolated Python environments for your automation."

RCC is a single binary that creates, manages, and distributes self-contained
Python environments. No Python installation required. No dependency conflicts.
No "works on my machine." Just repeatable automation, anywhere.

This document traces RCC's journey from solving a fundamental engineering
problem, through its evolution as the backbone of an automation platform,
to its current life as a community-maintained open source project.

---

## The Problem: Python's Beautiful Mess

Python is the most accessible programming language in the world. It's also
a dependency management nightmare.

Every Python developer has experienced this:

```
$ pip install automation-package
ERROR: package-x requires numpy>=1.20
ERROR: package-y requires numpy<1.19
```

Virtual environments help, but they're not portable. Containers help, but
they're heavy. And when you need to run automation on 50 machines across
3 operating systems? Good luck.

The RPA (Robotic Process Automation) industry's answer was proprietary tools
that cost $10,000+ per bot per year. UiPath. Automation Anywhere. Blue Prism.
Closed ecosystems that locked organizations into vendor dependency.

RCC was built to prove there's a better way.

---

## The Architecture: What Makes RCC Beautiful

RCC's elegance lies in a simple insight: **environments are data**.

Instead of recreating environments from scratch each time (slow, unreliable,
network-dependent), RCC treats environments as immutable, content-addressed
artifacts—like Git commits for your Python dependencies.

### The Holotree

The **Holotree** is RCC's crown jewel. Introduced in version 11.x (2021), it's
a content-addressed storage system for Python environments:

```
~/.robocorp/hololib/
├── library/          # Immutable environment layers (content-addressed)
│   ├── a1b2c3d4/    # Each directory is a hash of its contents
│   ├── e5f6g7h8/
│   └── ...
└── catalog/          # Environment blueprints (what layers to combine)
```

**Why this matters:**

1. **Instant environment creation** - If the layers exist, just symlink them
2. **Shared across users** - Multiple users on same machine share the library
3. **Air-gapped deployment** - Export as hololib.zip, import anywhere
4. **Bandwidth efficient** - Only download missing layers, ever

A 2GB Python environment that takes 10 minutes to create from scratch?
With Holotree, it restores in seconds.

### RCCRemote: The Missing Piece

RCC includes `rccremote`, a peer-to-peer server for sharing Holotree catalogs
across networks. It's the foundation for building a **Control Room**—a
central hub that:

- Serves pre-built environment catalogs to RCC clients
- Enables environment creation in isolated/air-gapped networks
- Reduces bandwidth by only transferring missing layers
- Coordinates automation execution across distributed workers

Robocorp built their proprietary Control Room on top of this. The open source
`rccremote` remains in the codebase, waiting for someone to build the rest.

---

## The Robocorp Era (2019-2024)

### The Vision (2019)

Robocorp was founded with a radical idea: **open source RPA**.

In an industry dominated by expensive proprietary tools, they bet that
developers would choose Python and open source over drag-and-drop GUIs
and vendor lock-in.

They were right. But building a business on open source is hard.

### Birth of RCC (2020)

On April 1, 2020, Juha Pohjalainen made the first commit to a private repo
called "conman" (conda manager). By May, it was renamed **RCC**.

On November 10, 2020, Robocorp open-sourced RCC under Apache 2.0:

> "Initial commit for open sourced rcc. This is snapshot from original
> development repo. All new development will continue in this new open
> rcc repository."

The commit contained ~120 commits of development. Version 5.0 was already
production-ready.

### The Holotree Revolution (2021)

Version 11.x introduced Holotree, transforming RCC from "conda wrapper"
to "production-grade environment management system."

Key innovations:
- Content-addressed storage (environments as immutable data)
- Shared libraries across users
- Air-gapped deployment via hololib.zip
- 10-100x faster environment restoration

This wasn't just an improvement—it was a fundamental rearchitecture that
made enterprise-scale automation possible.

### The Agentic Pivot (2023-2024)

By 2023, the automation landscape was shifting. ChatGPT had changed
everything. Traditional RPA (clicking buttons, scraping screens) was
giving way to AI agents that could reason, plan, and act.

Robocorp recognized this. In March 2023, they began building **Action Server**
(now github.com/sema4ai/actions)—a platform for exposing Python functions
as AI agent tools. The first feature commits included:

> "Vendor rcc into CLI"

RCC became the environment backbone for AI agents. Write a Python function,
decorate it with `@action`, and Action Server handles the rest—powered by
RCC's Holotree underneath.

Version 18.x added `--sema4ai` and `--robocorp` flags, preparing for what
came next.

### The Closure (October-November 2024)

In October 2024, Robocorp was acquired by Sema4.ai.

On November 11, 2024, Sema4.ai re-released RCC under a proprietary EULA.
Four years of open source development ended. The last open source version
was **v18.5.0** (October 25, 2024).

robocorp.com now redirects to sema4.ai. The vision of open source RPA
became closed source AI agents.

---

## The Community Fork Era (2024-Present)

### Why Fork?

RCC is too important to disappear behind a corporate EULA.

It's not just a tool—it's an architecture. Holotree solves a fundamental
problem in a beautiful way. The `rccremote` foundation is there, waiting
for someone to build an open source Control Room on top of it.

The AI agent future that Sema4.ai is building? It still needs reliable
Python environments. RCC is still the best solution. It should remain open.

### The Forks

The community preserved the open source codebase through forks created
before the closure:

| Fork | Created | Commits | Status |
|------|---------|---------|--------|
| mikaukora/rcc | Nov 2020 | 19 | Original public mirror, 97+ forks |
| admariner/rcc | May 2021 | 540 | Synced through Oct 2024 |
| vjmp/rcc | Jul 2024 | 531 | Original author's personal fork |
| joshyorko/rcc | Sep 2025 | Active | Current community fork |

### Community Fork: The Road Ahead

This fork (joshyorko/rcc → yorko-io/rcc) continues RCC development as a
**vendor-neutral, community-maintained project**.

**What we've done (v18.6.0+):**
- Decoupled from Robocorp/Sema4.ai infrastructure by default
- Telemetry fully disabled (no metrics sent anywhere)
- Templates served from community GitHub repositories
- Version checking via GitHub releases
- Configurable endpoints for any control room implementation

**Where we're going:**

RCC is the foundation. Holotree is the innovation. `rccremote` is the
starting point. What's missing is the Control Room—the orchestration
layer that coordinates automation at scale.

The pieces are all here. The architecture is proven. The code is open.

Someone just needs to build it.

---

## Original Contributors (Open Source Era 2020-2024)

The following individuals contributed to RCC during its open source period:

| Contributor | GitHub | Commits | Notable Work |
|-------------|--------|---------|--------------|
| Juha Pohjalainen | @vjmp | 495 | Primary author, architect of holotree |
| Kari Harju | @kariharju | 21 | Initial repo setup, infrastructure |
| Fabio Zadrozny | @fabioz | ~10 | v18.x features, Windows support |
| Antti Karjalainen | @aikarjal | 5 | Documentation, README |
| Raivo Link | @raivolink | 2 | Documentation updates |
| Sampo Ahokas | @sahokas | 1 | Documentation |
| And others | | | cmin764, mchece, jaukia, machafulla, orlof, SoloJacobs |

---

## The Evolution: A Technical Journey

### Era 1: The Foundation (v0.x–v4.x, April–November 2020)

**~180 commits in private development**

RCC began life on April 1, 2020 as "conman"—short for "conda manager." Within
five weeks it was renamed RCC, and by November it was production-ready.

The foundational architecture established patterns that persist today:

- **Cross-platform from day one.** The team built for Mac, Linux, and Windows
  simultaneously, with signed/notarized binaries for each. Even 32-bit ARM
  (Raspberry Pi) was supported initially.

- **Miniconda as the engine.** RCC wrapped miniconda3 to create isolated Python
  environments, downloading and installing it automatically. No system Python
  required.

- **The `ROBOCORP_HOME` paradigm.** All environments, caches, and state live
  under one directory (`~/.robocorp` by default). Move this folder, move
  everything.

- **Declarative configuration.** Robots were defined in `package.yaml` (later
  `robot.yaml`), with dependencies in `conda.yaml`. Merge multiple conda files
  together. Package robots as zips. Run them anywhere.

The CLI structure using Cobra and Viper established the command hierarchy that
still exists. Cloud authentication, file locking, environment cleanup—all the
plumbing was laid in these first 180 commits.

---

### Era 2: Going Open Source (v5.x–v8.x, November 2020–January 2021)

**~83 commits across 4 versions**

On November 10, 2020, Robocorp open-sourced RCC under Apache 2.0. Version 5.0
shipped with the first public commit.

This era focused on community adoption and a critical infrastructure change:

**The Micromamba Migration (v7.x–v8.x)**

Miniconda3 was heavy. Download sizes were large. Installation was slow.
Micromamba—a lightweight, standalone conda implementation—offered a better path.

Version 7.x introduced micromamba alongside miniconda. Version 8.x completed the
migration, removing miniconda entirely. This wasn't just a swap—it was a
philosophical shift toward smaller, faster, more portable tooling. The 32-bit
architectures were dropped (no micromamba support), but the tradeoff was worth it.

**Community Features**

- Interactive robot creation from templates
- Post-install scripts in `conda.yaml`
- Machine-readable output on stdout, human messages on stderr
- Windows long path detection and fixes

---

### Era 3: Building Toward Holotree (v9.x–v10.x, January–September 2021)

**~133 commits across 2 versions**

Version 9 was RCC's most feature-rich release before Holotree. With 101 commits,
it laid the groundwork for what was coming.

**The `settings.yaml` Revolution**

Configuration became externalized and layerable. Network proxies, certificate
bundles, pip indexes—all configurable without rebuilding. This pattern would
prove essential for enterprise deployment.

**Holotree Genesis**

The Holotree command subtree appeared in v9.x, running parallel to the existing
base/live environment system. You could export environments as `hololib.zip`
files and run robots against them. The architecture was being tested in
production while the old system remained the default.

**Diagnostics and Reliability**

RCC gained the ability to detect environment corruption, show installation
timelines, and report issues back to the cloud. Environments were activated
once at creation time (stored in `rcc_activate.json`), with full installation
plans logged to `rcc_plan.log`.

Version 10.x refined these systems: dependency freezing, integrity checks,
multi-architecture support. The stage was set.

---

### Era 4: The Holotree Era (v11.x–v17.x, September 2021–May 2024)

**~200+ commits across 7 major versions**

Version 11 was a breaking change—and RCC's most important release.

The old base/live environment caching was removed entirely. Holotree became
the only way. The hashing algorithm switched to SipHash. The `rcc environment`
commands disappeared, replaced by `rcc holotree`.

**What Made Holotree Revolutionary**

Content-addressed storage transformed environment management:

1. **Immutable layers.** Each environment component is stored by its content
   hash. Change a file? New hash. Old version stays intact.

2. **Shared libraries.** Multiple users on the same machine share the hololib.
   Build once, use everywhere.

3. **Air-gapped deployment.** Export as `hololib.zip`, import on isolated
   networks. No internet required after the first build.

4. **Instant restoration.** A 2GB environment that takes 10 minutes to build
   from scratch? Holotree restores it in seconds by symlinking cached layers.

**Enterprise Features (v12.x–v17.x)**

The following versions refined Holotree for production:

- **Shared holotree** (v11.x): Multiple users share one hololib via `rcc holotree shared --enable`
- **Profile system**: Export/import complete configurations including certificates, pip settings, and endpoints
- **Prebuild environments**: IT teams can build once, distribute everywhere
- **Integrity verification**: `rcc holotree check` validates the library and removes corrupted entries
- **Delta exports**: Transfer only missing layers between machines
- **rccremote**: Peer-to-peer catalog sharing across networks

Version 17.x made Holotree layered by default and embedded micromamba directly
in the RCC binary. The tool had matured from "conda wrapper" to "enterprise
environment management system."

---

### Era 5: The Agentic Pivot (v18.x, June–October 2024)

**~20 commits before closure**

By 2024, Robocorp was pivoting to AI agents. Version 18.x reflected this shift.

**Dual Product Strategy**

The `--sema4ai` and `--robocorp` flags appeared, allowing RCC to serve two
product lines with different branding, endpoints, and default configurations.
Each "strategy" had its own `settings.yaml`, download URLs, and telemetry
destinations.

**Action Server Integration**

RCC became the environment backbone for Action Server (now `sema4ai/actions`),
where Python functions decorated with `@action` become tools for AI agents.
Holotree's instant environment restoration made agent execution fast enough
for real-time AI workflows.

**The Last Open Source Version**

v18.5.0 shipped on October 25, 2024 with stdin support for `rcc holotree hash`
and dev-dependencies in `package.yaml`. Seventeen days later, Sema4.ai
re-released RCC under a proprietary license.

---

### Era 6: Community Fork (v18.6.0+, 2025–Present)

**Active development**

This fork continues RCC as vendor-neutral open source.

**What Changed**

- All Robocorp/Sema4.ai endpoints removed from defaults
- Telemetry fully disabled (code paths removed, not just configured off)
- Templates served from community GitHub repositories
- Version checking via GitHub releases
- All endpoints configurable via environment variables or `settings.yaml`

**The Mission**

RCC's architecture—Holotree, rccremote, content-addressed environments—is too
valuable to disappear behind a corporate license. The foundation for an open
source Control Room exists in this codebase. The community fork keeps it
available for whoever wants to build on it.

---

## This Fork's Thesis

This fork exists for one reason:

> **Keep RCC and Holotree open, first-class, and powerful enough to serve as
> the foundation of any control room—not just one company's SaaS.**

The original authors proved the architecture works:

- **Holotree** makes environments fast, portable, and reproducible
- **rccremote** can distribute those environments across networks
- **RCC** can be embedded under CLIs, robots, and action servers

This fork's job is to:

1. **Preserve that work** under a permissive Apache 2.0 license
2. **Strip out hard-coded dependencies** and telemetry
3. **Make rccremote and Holotree actually usable** and documented
4. **Leave the door open** for any self-hosted control room to sit on top

Where others pivoted to closed, agent-as-a-service platforms, this fork stays
focused on the base layer: repeatable, movable, isolated Python environments—
for robots, for agents, for whatever comes next.

**The mental model:**

| Concept | Analogy |
|---------|---------|
| RCC | Git |
| Holotree | Object database |
| rccremote | Origin remote |
| Control Room | GitHub/GitLab built on top |

RCC is "Git for environments." Holotree is the content-addressed object store.
rccremote is how you push and pull between machines. A Control Room is the UI
and orchestration layer that coordinates it all—but it's not RCC's job to be
that layer. RCC's job is to be the reliable foundation underneath.

See [ROADMAP.md](./roadmap.md) for where we're headed.
