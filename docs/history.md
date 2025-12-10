# History of rcc

This is quick recap of rcc history. Just major topics and breaking changes.
There has already been 500+ commits, with lots of fixes and minor improvements,
and they are not listed here.

## Community Fork Era: November 2024 onwards

### The Closure

In October 2024, Robocorp was acquired by Sema4.ai. On November 11, 2024,
Sema4.ai re-released RCC under a proprietary EULA, effectively closing the
source code that had been open under Apache 2.0 since November 2020.

The last open source version was **v18.5.0** (October 25, 2024).

robocorp.com now redirects to sema4.ai, and the robocorp/rcc repository
contains only the closed-source EULA-licensed version.

### The Community Response

The community maintained access to the open source codebase through forks:

- **mikaukora/rcc** - Original public mirror (November 2020), 97 forks
- **vjmp/rcc** - Juha Pohjalainen's fork (July 2024), 531 commits of full history
- **admariner/rcc** - Active fork (May 2021), 540 commits synced through October 2024
- **joshyorko/rcc** - Current community fork (September 2025)

On January 17, 2025, Juha Pohjalainen (vjmp), the original primary author of RCC,
removed telemetry and metrics code from his personal fork with the commit:
"Removed feedback, metrics, and process tree (performance improvement)."

### Version 18.12.x: Community Fork (December 2025 onwards)

This fork (joshyorko/rcc, moving to yorko-io/rcc) continues RCC development
as a vendor-neutral, community-maintained project:

- RCC fully decoupled from Robocorp/Sema4.ai infrastructure by default
- All cloud endpoints empty by default (telemetry disabled)
- Templates served from community GitHub repository
- Version checking via GitHub releases
- Users can still configure Robocorp endpoints if needed via environment variables

## Original Contributors (Open Source Era 2020-2024)

The following individuals contributed to RCC during its open source period:

| Contributor | GitHub | Commits | Notable Work |
|-------------|--------|---------|--------------|
| Juha Pohjalainen | @vjmp | 495 | Primary author, created "conman"/rcc |
| Kari Harju | @kariharju | 21 | Initial repo setup, infrastructure |
| Fabio Zadrozny | @fabioz | ~10 | v18.x features, Windows support |
| Antti Karjalainen | @aikarjal | 5 | Documentation, README |
| Raivo Link | @raivolink | 2 | Documentation updates |
| Sampo Ahokas | @sahokas | 1 | Documentation, EULA notice |
| And others | | | cmin764, mchece, jaukia, machafulla, orlof, SoloJacobs |

## Robocorp Era: Version 12.x through 18.5.x (2021-2024)

Versions 12 through 18.5 continued active development under Robocorp with
major features including:

- Version 18.x: Sema4.ai/Robocorp dual product strategy, batch metrics
- Version 17.x: uv support experiments, package.yaml as conda.yaml replacement,
  micromamba upgrades, improved cleanup procedures
- Version 16.x: Various improvements and bug fixes
- Version 15.x: Continued holotree improvements
- Version 14.x: Performance optimizations
- Version 13.x: Stability improvements
- Version 12.x: Post-holotree maturation

For detailed version 12-18.5 changes, see `docs/changelog.md`.

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
