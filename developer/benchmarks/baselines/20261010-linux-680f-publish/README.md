# Single profiled publication at RCC `680f966`

This is one successful, concurrent-load Linux amd64 publication, not a paired benchmark. `receipt.json` records the exact candidate SHA and binary SHA, fixture inputs/digest, provider startup and publish commands, host context, wall time, waited-child resource use, Artifact receipt, resolved Conda packages, and indexed CAS object histogram. `publish.stdout` and `publish.stderr` are the raw CLI output. The 30 resolved package name/version/build/archive-hash identities match the earlier five-run baseline, but timing is not comparable across these runs.

The profile was produced with the existing RCC root flag, with `ROBOCORP_HOME` and `TMPDIR` under this directory and `RCC_HOLOTREE_MODE=private`:

```sh
/workspace/work/rcc-swarm/integration-release-pin/build/rcc cache serve --backend filesystem --root /workspace/work/rcc-swarm/evidence/performance/profile-680f/provider --listen 127.0.0.1:0 --json
/workspace/work/rcc-swarm/integration-release-pin/build/rcc --pprof /workspace/work/rcc-swarm/evidence/performance/profile-680f/publish.pprof env publish --robot /workspace/work/rcc-swarm/evidence/performance/profile-680f/fixture/robot.yaml --provider http://127.0.0.1:35039 --trust-carrier /workspace/work/rcc-swarm/evidence/performance/profile-680f/trust --trust-carrier-type filesystem --json
/workspace/work/rcc-swarm/tools/go1.26.9/bin/go tool pprof -top -nodecount=25 /workspace/work/rcc-swarm/integration-release-pin/build/rcc /workspace/work/rcc-swarm/evidence/performance/profile-680f/publish.pprof
/workspace/work/rcc-swarm/tools/go1.26.9/bin/go tool pprof -top -cum -nodecount=25 /workspace/work/rcc-swarm/integration-release-pin/build/rcc /workspace/work/rcc-swarm/evidence/performance/profile-680f/publish.pprof
```

The loopback provider port was assigned for this run; use the new server's `url` for a repeat. The generated 1.5 GiB producer home was removed **after** package identities were recorded to return disk space. The immutable provider, `publish.pprof`, receipt and pprof text outputs remain. The profile is Go CPU sampling for the RCC process, while `RUSAGE_CHILDREN` may account for its waited descendants. Neither is a measured network-wire, Python-copy, or Drop-only cost.
