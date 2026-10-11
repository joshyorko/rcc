# Current shipping-source security and consumer contract qualification

Date: 2026-10-11 UTC. Read-only source and GitHub API checks; no source, dependency, alert, release, or issue changes.

## Exact source and toolchain

- Current public `main`: `727c1ff8b679cdbbe12836e53c87734a1edb0071`, tree `cfae5c5ee9df0e5031e8e9ae37040b5c98ac6152`. The existing `/workspace/work/rcc-swarm/final-main-727` worktree is clean.
- This source's `go.mod` selects Go 1.26.9. The exact Go binary reports `go1.26.9 linux/amd64`; the existing checker is govulncheck v1.1.4, SHA-256 `119a9cb89999d79c6932fe65e52f02b7c53276dd0f511da0085be5773f417fe3`, itself built with Go 1.26.9.
- The current source differs from the earlier four-target scan at `e3b693a4d339a16c04218ae437f64dc21934f6df` in the relevant Go inputs as follows:

```text
.dagger/main.go        |   2 +-
 common/version.go      |   2 +-
 conda/uvnative.go      |  11 ++-
 conda/uvnative_test.go | 196 +++++++++++++++++++++++++++++++++++++++++++++++++
 go.mod                 |   2 +-
 5 files changed, 209 insertions(+), 4 deletions(-)
```

The current scans include the Go 1.26.9 floor, RCC version 18.19.6, and UV prefix-alias change. Govulncheck fetched vulnerability data through its configured `https://vuln.go.dev` source on each invocation. Its captured output does not expose the database snapshot's last-modified timestamp, so this report does not assign one.

## Go vulnerability results

Fresh `-show traces,verbose ./...` scans ran against all four supported source targets using the exact main tree:

| Target | CGO | Exit | Symbol-reachable findings |
| --- | ---: | ---: | ---: |
| linux/amd64 | 1 | 0 | 0 |
| darwin/amd64 | 0 | 0 | 0 |
| darwin/arm64 | 0 | 0 | 0 |
| windows/amd64 | 0 | 0 | 0 |


All four runs examined 29 RCC root packages. Each reports `No vulnerabilities found` in symbol results and reports one imported-package advisory and one module-only advisory without a call trace:

- GO-2026-5970: `golang.org/x/text` v0.28.0, fixed in v0.39.0, infinite loop on invalid input. The scanner reports it as an imported-package finding but did not find a call trace in the current RCC package set.
- GO-2026-6629: `golang.org/x/text` v0.28.0, fixed in v0.41.0, panic while parsing crafted input in `x/text/secure/precis`. This is module-only in the root CLI scan.

A separate corrected module scan from the `cmd` package (`-C cmd -scan=module -show verbose`) exited 3 and reported those same two `x/text` module advisories. Exit 3 is govulncheck's findings status; neither is a symbol-reachable result. An initial module scan from the module root had no package pattern and exited 1; its exact diagnostic is retained separately, and the successful `cmd`-scoped scan is the module result used here.

The separate `.dagger` module remains outside source-reachability conclusions: this checkout does not contain generated `.dagger/internal/dagger`, and this lane did not regenerate it. The existing graph-only report remains a tooling dependency inventory, not shipped-source call evidence.

## Dependabot access

`gh auth status` showed an active authenticated GitHub CLI session, but the normal read-only endpoint `GET /repos/joshyorko/rcc/dependabot/alerts?state=open&per_page=100` returned HTTP 403 `Resource not accessible by integration`. The exact response is saved as `dependabot-open.json`. Therefore there is no alert inventory or per-alert severity/package mapping from this lane; no alert was closed or changed.

## CLI, artifact trust, and consumer version boundary

- The source-built binary is `final-main-727/build/rcc`, SHA-256 `daf9143e7caf0fc2dcf59e999396c5168d4148c7d1bae4fa59ccbbe620018998`. `--version` exits 0 and prints `v18.19.6`; `go version -m` identifies Go 1.26.9, Linux/amd64, CGO disabled. Its version is present in this accepted main source, but it is not a published release or tag.
- Fresh `gh release list` still identifies `v18.19.5` as Latest; querying the `v18.19.6` tag ref returned HTTP 404. The candidate binary/source is not evidence of a published v18.19.6 release.
- The current CLI help commands `--help`, `env acquire --help`, `env exec --help`, and `env trust --help` all exited 0. Source tests `TestEnvironmentTrustPolicyDefaultsStrictAndRejectsConflictingModes` and `TestStrictRemoteRejectsUnsignedAndReceiptIsMachineReadable` are in the exercised package suites. `trustPolicyForCommand(false,false)` returns `StrictRemote` with fail-closed revocations; unsigned-local use requires explicit `--permissive-local`, and conflicting explicit modes are rejected. README and `docs/environment-artifact-trust.md` document this strict remote default.
- Fresh focused Go tests passed (`exit 0`) for `./cmd ./artifacttrust ./environmentartifact ./environmentlifecycle`, with Go 1.26.9, `CC=/usr/bin/gcc`, `CGO_ENABLED=1`. The exact final-main receipt also records the full `go test -count=1 ./...` suite exit 0.
- Do not claim the Actions consumer accepts v18.19.6. The retained exact Actions source snapshot at `evidence/performance-steer-20261011/actions-221-source` pins the runtime adapter to exactly v18.19.3 (adapter SHA-256 `8f7642f4b1ee8ff658dbf2414ec34ca28747054833f73f4996fb4210d9e5dde5`). The retained unmodified verifier probe accepts only the v18.19.3 control and rejects both released v18.19.5 and candidate v18.19.6, including when a binary override is supplied; see `evidence/consumer-contract-125-20261011/version-probe-results.json`. No Actions v18.19.6 integration acceptance was run here. This is a blocking fact for an Actions compatibility claim, not for RCC's own CLI/source scan conclusion.

## Disposition and evidence limits

Current RCC root-source scan has no call-traced Go vulnerability on the four targets. The two untraced `x/text` advisories remain visible and unremediated; `.dagger` source reachability and Dependabot alert mapping remain unavailable. The CLI's strict artifact trust default and its unit coverage are intact. Candidate v18.19.6 is not released, and the pinned Actions adapter does not accept it. Do not translate the clean root source call scan into a clean dependency graph or Actions compatibility statement.

Machine-readable scan, module, CLI, source, and test receipts are in this directory. `govulncheck-targets.json` links each target to its raw log and SHA-256; `cli-qualification.json` does the same for CLI help/version checks.
