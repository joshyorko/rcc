# History of RCC

RCC (Repeatable, Contained Code) is a tool for creating, managing, and
distributing self-contained Python automation packages with isolated environments.

> "Repeatable, movable and isolated Python environments for your automation."

This document traces RCC's journey from its origins solving Python dependency
hell, through its evolution as the backbone of Robocorp's automation platform,
to its current life as a community-maintained open source project.

---

## The Problem RCC Was Built to Solve

Before RCC, Python automation faced a fundamental challenge: **dependency hell**.

- "Works on my machine" was the norm, not the exception
- System Python installations conflicted with project requirements
- Deploying automation to new machines required manual environment setup
- Teams couldn't reliably share automation packages
- Virtual environments helped but weren't portable or self-contained

RCC solved this by providing:

- **No Python required** - RCC embeds everything needed to run
- **Exact reproducibility** - Same Python version, same packages, every time
- **Portable packages** - Move automation between machines without setup
- **Isolated environments** - No conflicts with system or other projects
- **Cross-platform** - Windows, macOS, Linux from same configuration

---

## The Robocorp Era (2019-2024)

### Founding Vision (2019)

Robocorp was founded in 2019 with a mission to bring open source to the
Robotic Process Automation (RPA) industry, which was dominated by expensive,
proprietary tools like UiPath and Automation Anywhere.

The vision: democratize automation by making it accessible to developers
through open source tools and Python—a language they already knew.

### Birth of "Conman" (April 2020)

On April 1, 2020, Juha Pohjalainen made the first commit to a private repo
called "conman" (short for "conda manager"). The goal was simple: manage
conda environments reliably for automation packages.

On May 8, 2020, the project was renamed to **RCC** (Robocorp Control Center,
later "Repeatable, Contained Code").

### Going Open Source (November 2020)

On November 10, 2020, Robocorp open-sourced RCC under the Apache 2.0 license.
The commit message read:

> "Initial commit for open sourced rcc. This is snapshot from original
> development repo. All new development will continue in this new open
> rcc repository."

This was version 5.x, and RCC already had ~120 commits of development behind it.

### The Holotree Revolution (2021)

Version 11.x introduced **Holotree**, a completely new approach to environment
management that became RCC's defining feature:

- Immutable, content-addressed environment storage
- Shared environments across users on the same machine
- Air-gapped deployment via hololib.zip exports
- Dramatic performance improvements for environment creation

Holotree made RCC not just a convenience tool but a production-grade
environment management system.

### The Agentic Pivot (2023-2024)

As AI capabilities exploded, Robocorp recognized a shift in the automation
landscape. Traditional RPA (clicking buttons, scraping screens) was giving
way to AI agents that could reason, plan, and take actions.

In March 2023, Robocorp began developing **Action Server** (github.com/sema4ai/actions),
a platform for creating AI actions and tools. The very first feature commits
included "Vendor rcc into CLI"—RCC became the environment backbone for AI agents.

Action Server allowed developers to:
- Write Python functions decorated with `@action` or `@tool`
- Automatically expose them as APIs for AI agents
- Connect to OpenAI GPTs, LangChain, MCP, and other AI platforms
- All while RCC handled the Python environment complexity

Version 18.x of RCC added dual product strategy support (`--robocorp` and
`--sema4ai` flags), preparing for the transition.

### The Acquisition and Closure (October-November 2024)

In October 2024, Robocorp was acquired by Sema4.ai. robocorp.com now redirects
to sema4.ai.

On November 11, 2024, Sema4.ai re-released RCC under a proprietary EULA,
ending four years of open source development. The last open source version
was **v18.5.0** (October 25, 2024).

---

## The Community Fork Era (2024-Present)

### Preserving the Open Source Legacy

The community maintained access to the open source codebase through forks
created before the closure:

| Fork | Created | Commits | Status |
|------|---------|---------|--------|
| mikaukora/rcc | Nov 2020 | 19 | Original public mirror, 97+ forks |
| admariner/rcc | May 2021 | 540 | Synced through Oct 2024 |
| vjmp/rcc | Jul 2024 | 531 | Original author's personal fork |
| joshyorko/rcc | Sep 2025 | Active | Current community fork |

### The Original Author's Statement

On January 17, 2025, Juha Pohjalainen (@vjmp)—who wrote 495 of the 531 commits
in RCC's history—made a significant commit to his personal fork:

> "Removed feedback, metrics, and process tree (performance improvement)."

The primary author of RCC surgically removed the telemetry code he himself
had written, signaling a clear stance on the direction of the tool.

### Community Fork: v18.6.0+ (2025 onwards)

This fork (joshyorko/rcc → yorko-io/rcc) continues RCC development as a
**vendor-neutral, community-maintained project**:

- **Decoupled by default** - No Robocorp/Sema4.ai infrastructure dependencies
- **Telemetry disabled** - No metrics sent anywhere
- **Community templates** - Served from community GitHub repositories
- **Version checking via GitHub** - No proprietary download servers
- **Configurable** - Users can still point to any control room via env vars

The mission remains unchanged from what Juha wrote in the first open source
commit: provide repeatable, movable, and isolated Python environments for
automation—now including AI agents.

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

## Version History Summary

For the detailed version-by-version breakdown, see below. Here's the high-level arc:

| Era | Versions | Dates | Focus |
|-----|----------|-------|-------|
| Private Development | 0.x-4.x | Apr-Nov 2020 | Core conda/environment management |
| Early Open Source | 5.x-10.x | Nov 2020-Sep 2021 | Community features, stabilization |
| Holotree Era | 11.x-17.x | Sep 2021-May 2024 | Revolutionary environment caching |
| Agentic Pivot | 18.0-18.5 | Jun-Oct 2024 | Sema4.ai integration, AI support |
| Community Fork | 18.6+ | Sep 2025- | Vendor-neutral, decoupled |

## Version 11.x: between Sep 6, 2021 and ...

Version "eleven" is work in progress and has already 100+ commits, and at least
following improvements:

- breaking change: old environment caching (base/live) was fully removed and
  holotree is only solution available
- breaking change: hashing algorithm changed, holotree uses siphash fron now on
- environment section of commands were removed, replacements live in holotree
  section
- environment cleanup changed, since holotree is different from base/live envs
- auto-scaling worker count is now based on number of CPUs minus one, but at
  least two and maximum of 96
- templates can now be automatically updated from Cloud and can also be
  customized using settings.yaml autoupdates section
- added option to do strict environment building, which turns pip warnings
  into actual errors
- added support for speed test, where current machine performance gets scored
- hololib.zip files can now be imported into normal holotree library (allows
  air gapped workflow)
- added more commands around holotree implementation
- added support for preRunScripts, which are executed in similar context that
  actual robot will use, and there can be OS specific scripts only run on
  that specific OS
- added profile support with define, export, import, and switch functionality
- certificate bundle, micromambarc, piprc, and settings can be part of profile
- `settings.yaml` now has layers, so that partial settings are possible, and
  undefined ones use internal default settings
- `docs/` folder has generated "table of content"
- introduced "shared holotree", where multiple users in same computer can
  share resources needed by holotree spaces
- in addition to normal tasks, now robot.yaml can also contain devTasks, which
  can be activated with flag `--dev`
- holotrees can also be imported directly from URLs
- some experimental support for virtual environments (pyvenv.cfg and others)
- moved from "go-bindata" to use new go buildin "embed" module
- holotree now also fully support symbolic links inside created environments
- improved cleanup in relation to new shared holotrees
- individual catalog removal and cleanup is now possible
- prebuild environments can now be forced using "no build" configurations

## Version 10.x: between Jun 15, 2021 and Sep 1, 2021

Version "ten" had 32 commits, and had following improvements:

- breaking change: removed lease support
- listing of dependencies is now part of holotree space (golden-ee.yaml)
- dependency listing is visible before run (to help debugging environment
  changes) and there is also command to list them
- environment definitions can now be "freezed" using freeze file from run output
- supporting multiple environment configurations to enable operating system
  and architecture specific freeze files (within one robot project)
- made environment creation serialization visible when multiple processes are
  involved
- added holotree check command to verify holotree library integrity and remove
  those items that are broken

## Version 9.x: between Jan 15, 2021 and Jun 10, 2021

Version "nine" had 101 commits, and had following improvements:

- breaking change: old "package.yaml" support was fully dropped
- breaking change: new lease option breaks contract of pristine environments in
  cases where one application has already requested long living lease, and
  other wants to use environment with exactly same specification
- new environment leasing options added
- added configuration diagnostics support to identify environment related issues
- diagnostics can also be done to robots, so that robot issues become visible
- experiment: carrier robots as standalone executables
- issue reporting support for applications (with dryrun options)
- removing environments now uses rename/delete pattern (for detecting locking
  issues)
- environment based temporary folder management improvements
- added support for detecting when environment gets corrupted and showing
  differences compared to pristine environment
- added support for execution timeline summary
- assistants environments can be prepared before they are used/needed, and this
  means faster startup time for assistants
- environments are activated once, on creation (stored on `rcc_activate.json`)
- installation plan is also stored as `rcc_plan.log` inside environment and
  there is command to show itMika Kaukoranta

- introduction of `settings.yaml` file for configurable items
- introduced holotree command subtree into source code base
- holotree implementation is build parallel to existing environment management
- holotree now co-exists with old implementation in backward compatible way
- exporting holotrees as hololib.zip files is possible and robot can be executed
  against it
- micromamba download is now done "on demand" only
- result of environment variables command are now directly executable
- execution can now be profiled "on demand" using command line flags
- download index is generated directly from changelog content
- started to use capability set with Cloud authorization
- new environment variable `ROBOCORP_OVERRIDE_SYSTEM_REQUIREMENTS` to make
  skip those system requirements that some users are willing to try
- new environment variable `RCC_VERBOSE_ENVIRONMENT_BUILDING` to make
  environment building more verbose
- for `task run` and `task testrun` there is now possibility to give additional
  arguments from commandline, by using `--` separator between normal rcc
  arguments and those intended for executed robot
- added event journaling support, and command to see them
- added support to run scripts inside task environments

## Version 8.x: between Jan 4, 2021 and Jan 18, 2021

Version "eight" had 14 commits, and had following improvements:

- breaking change: 32-bit support was dropped
- automatic download and installation of micromamba
- fully migrated to micromamba and removed miniconda3
- no more conda commands and also removed some conda variables
- now conda and pip installation steps are clearly separated

## Version 7.x: between Dec 1, 2020 and Jan 4, 2021

Version "seven" had 17 commits, and had following improvements:

- breaking change: switched to use sha256 as hashing algorithm
- changelogs are now held in separate file
- changelogs are embedded inside rcc binary
- started to introduce micromamba into project
- indentity.yaml is saved inside environment
- longpath checking and fixing for Windows introduced
- better cleanup support for items inside `ROBOCORP_HOME`

## Version 6.x: between Nov 16, 2020 and Nov 30, 2020

Version "six" had 24 commits, and had following improvements:

- breaking change: stdout is used for machine readable output, and all error
  messages go to stderr including debug and trace outputs
- introduced postInstallScripts into conda.yaml
- interactive create for creating robots from templates

## Version 5.x: between Nov 4, 2020 and Nov 16, 2020

Version "five" had 28 commits, and had following improvements:

- breaking change: REST API server removed (since it is easier to use just as
  CLI command from applications)
- Open Source repository for rcc created and work continued there (Nov 10)
- using Apache license as OSS license
- detecting interactive use and coloring outputs
- tutorial added as command
- added community pull and tooling support

## Version 4.x: between Oct 20, 2020 and Nov 2, 2020

Version "four" had 12 commits, and had following improvements:

- breaking change related to new assistant encryption scheme
- usability improvements on CLI use
- introduced "controller" concept as toplevel persistent option
- dynamic ephemeral account support introduced

## Version 3.x: between Oct 15, 2020 and Oct 19, 2020

Version "three" had just 6 commits, and had following improvements:

- breaking change was transition from "task" to "robotTaskName" in robot.yaml
- assistant heartbeat introduced
- lockless option introduced and better support for debugging locking support

## Version 2.x: between Sep 16, 2020 and Oct 14, 2020

Version "two" had around 29 commits, and had following improvements:

- URL (breaking) changes in Cloud required Major version upgrade
- added assistant support (list, run, download, upload artifacts)
- added support to execute "anything", no condaConfigFile required
- file locking introduced
- robot cache introduced at `$ROBOCORP_HOME/robots/`

## Version 1.x: between Sep 3, 2020 and Sep 16, 2020

Version "one" had around 13 commits, and had following improvements:

- terminology was changed, so code also needed to be changed
- package.yaml converted to robot.yaml
- packages were renamed to robots
- activities were renamed to tasks
- added support for environment cleanups
- added support for library management

## Version 0.x: between April 1, 2020 and Sep 8, 2020

Even when project started as "conman", it was renamed to "rcc" on May 8, 2020.

Initial "zero" version was around 120 commits and following highlevel things
were developed in that time:

- cross-compiling to Mac, Linux, Windows, and Raspberry Pi
- originally supported were 32 and 64 bit architectures of arm and amd
- delivery as signed/notarized binaries in Mac and Windows
- download and install miniconda3 automatically
- management of separate environments
- using miniconda to manage packages at `ROBOCORP_HOME`
- merge support for multiple conda.yaml files
- initially using miniconda3 to create those environments
- where robots were initially defined in `package.yaml`
- packaging and unpacking of robots to and from zipped activity packages
- running robots (using run and testrun subcommands)
- local conda channels and pip wheels
- sending metrics to cloud
- CLI handling and command hierarchy using Viper and Cobra
- cloud communication using accounts, credentials, and tokens
- `ROBOCORP_HOME` variable as center of universe
- there was server support, and REST API for applications to use
- ignore files support
- support for embedded templates using go-bindata
- originally used locality-sensitive hashing for conda.yaml identity
- both Lab and Worker support

## Birth of "Codename: Conman"

First commit to private conman repo was done on April 1, 2020. And name was
shortening of "conda manager". And it was developer generated name.
