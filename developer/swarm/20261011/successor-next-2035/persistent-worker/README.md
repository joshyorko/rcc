# Persistent-child lifecycle diagnostic

`source/main.go` is a private Linux-only harness for comparing repeated fresh
Python children with six requests handled by one short-lived resident child.
It is source-preparation only; it has not been built or run. It makes no
production provider/format/default changes and does not claim reuse across
separate harness invocations.

The coordinator supplies an already prepared gzip-level-1 fixture/provider,
creates a new empty 0700 consumer home per arm, checks outer/home ownership and
modes, and runs three counterbalanced fresh/resident pairs. Worker flags are:

```text
-mode=worker -arm=fresh|resident -tasks=6 \
-outer=<owned-0700-boundary> -provider-root=<prepared-provider> \
-artifact=<sha256:...> -fixture=<fixture.json> \
-receipt=<new-jsonl-path> -profile=<new-cpu-profile-path>
```

`-mode=control` runs the untimed nonzero-child/lease-release control and uses
`-outer`, `-provider-root`, `-artifact`, `-fixture`, and `-receipt`. Both modes
use a 25-second context so the coordinator's 30-second process bound retains
cleanup margin. The artifact's materialized `python` wrapper is used through
RCC's ordinary `NewAcquirer`/`ExecuteWithStreams` APIs and normal compatibility
checks. The fixture depends on the host CPython interpreter identified when it
was prepared; it is not a self-contained Python environment claim.

Each request reads and hashes all 512 payload files. The resident child does
that work anew for each of its six input lines and validates the active lease
identity while that child remains alive. Worker receipts bind output rows to
execution lease IDs, require six distinct fresh-child PIDs or one resident PID,
check strict lease removal, source immutability, and home/outer 0700 owner/mode
guards. The measured `batchNs` is the sum of `ExecuteWithStreams` call times;
CPU profiling and resource counters begin after cold acquire and include output
decoding and lease checks, but exclude later source/guard verification. RSS is a
post-batch Go-process snapshot, not a child-process peak.

The failure-control command, expected code, and error text all use exit code 73.
Current source SHA-256 is recorded with the source review receipt.
