# Executable selection in an acquired environment

`rcc env exec` resolves bare non-Python command names only in
`conda.CondaPaths(materialization.Path)`, in that order. An executable present
only on RCC's launcher PATH is unavailable; a host copy cannot shadow the
Artifact copy. Missing commands fail with the requested name and an
environment-PATH diagnostic. Bare `python`, `python3`, and `python.exe` retain
the materialized-Python mapping.

A relative name containing a path separator is relative to the materialized
environment, not the caller's source directory. Traversal and symlinks resolving
outside that environment are rejected before process startup. Internal symlinks
and symlinked parent directories remain usable. An absolute path, including an
absolute Python path, deliberately selects that executable; it is the explicit
host-delegation mechanism. RCC does not sandbox the task, its interpreter, or
commands subsequently launched by the task.

On Unix, the selected file must be regular and have an executable permission
bit. On Windows, extensionless names follow case-insensitive PATHEXT order
(default `.COM;.EXE;.BAT;.CMD`), without the implicit current-directory or host
PATH search. A name with an extension is selected as written. Selection does
not promise that Windows can directly start every script type; process startup
errors remain explicit. Native Windows/macOS runtime qualification is required
before claiming platform acceptance; cross-compilation is insufficient.

The execution CWD, supervision and lease lifecycle are unchanged. Default CWD
remains the materialization root. `--inherit-streams` sends child stdout/stderr
to their corresponding caller streams and requires `--receipt-file`; `--json`
keeps stdout for the execution receipt and does not forward child output.
Receipts expose exit/status and lease identity, including failed lookup in
inherit-streams mode. No source-CWD extension is included.

## Verification

Run focused Go tests, then the real CLI Robot suite:

```sh
go test ./environmentlifecycle -run 'TestExecutableResolution258|TestLegacyV12Wrap' -count=1
python3 -m robot --variable RCC:"$RCC_BINARY" robot_tests/environment_exec_258.robot
```

`RCC_BINARY` is the absolute built executable. For offline replay, supply
`RCC_258_ARCHIVE` pointing to the native `.rcca` previously produced by this
fixture. Without it, the suite publishes a real Python environment containing
two shell entry scripts installed as package scripts. The suite proves unique
name, host collision, Python, relative/absolute selection, absent command,
traversal, stream/JSON separation and released leases in a private RCC home.
Environment construction may use the network; consumption is offline with only
the fixture loopback provider permitted. Archive export during teardown is
explicitly opt-in with `RCC_258_KEEP_ARCHIVE=1`.

Check memory, swap pressure, bytes and inodes on the actual scratch filesystem
before environment creation or acceptance. Set `TMPDIR` and
`RCC_ISSUE258_TMPDIR` to the same disk-backed task directory; an isolated Git
worktree does not redirect tmpfs. Serialize heavy fixture runs when resources
are constrained. Preserve failure evidence before deleting private test state.

The legacy v12 wrap/import regression delegates its host shell by absolute path;
its former bare `sh` assumption conflicts with the new explicit contract.
N-1 compatibility and native platform results belong in the candidate matrix,
not inferred from these commands.
