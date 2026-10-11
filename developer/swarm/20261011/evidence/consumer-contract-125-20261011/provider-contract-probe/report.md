# RCC provider and trust-carrier runtime qualification

Both the actual hosted PR #233 binary and released v18.19.5 reproduce the provider-alias/default-carrier failure. This is an existing RCC #118 CLI integration gap, not a demonstrated regression in PR #233. Provider-dead warm failure with the inferred HTTP carrier is a separate consequence of retaining unavailable trust input. It does not prove that local materialization reuse is broken.

## Scope and preserved identities

This RCC-only probe ran 17 recorded commands against a copied existing Artifact, without an environment build or performance measurement. It changed no Actions state, repository source, branch, issue, PR, or release. Root source checkout `build-provenance` was read-only and matched commit `09796bf78ab4d79ef91dc7fa4bbb23be7aa14033`, tree `4f1338a5de382c313edef39cfbc57c2374afea0f`. Eight exact source files and their hashes are in `source-hashes.json`.

- Hosted candidate Linux binary SHA-256 `b5aa13f0921abd7c4b459c12b3042e58fecb8b7220756ad76ed7d651d74dbfd1`; actual version `v18.19.6`; embedded Go 1.26.9, exact revision `09796bf78ab4d79ef91dc7fa4bbb23be7aa14033`, `vcs.modified=false`.
- Released comparison binary SHA-256 `1a617ad7c736fa67c605e20e5ebe3c7d54b02cd548f733e05c809cf49a48db1e`; actual version `v18.19.5`; embedded Go 1.26.5 and revision `d1aec7d0bb897a81274423c7a6bb747233f9c263`, `vcs.modified=true`. That embedded revision alone is not an exact released source-tree identity; the comparison is bound to the actual released bytes supplied and independently hash-checked here.
- Existing Artifact `sha256:900e160d10da65d2f33ed06a240ec78fb67ac049e8c6881954893ce061d194db`, legacy blueprint key `37368f00fc9af086`, v12/gzip/SHA-256 reader, Python 3.11.17. The copied manifest and 5,049-object closure were read from the authorized original fixture; every stored object digest was checked while copying.
- The original unsigned detached provenance/SBOM/revocation fixture was copied into the probe provider's `trust` directory. Nothing was published or written into the original fixture. Before/after checks covered 5,054 original content/trust/audit files and all hashes/sizes were unchanged.
- Provider used the hosted candidate `cache serve --root OWN_COPY --listen 127.0.0.1:0 --json`. Its actual URL was `http://127.0.0.1:44211`. Profile `office` was added through the actual CLI in the private consumer home; `settings.yaml` records that URL. One shared private consumer home was deliberately reused across the two binaries to compare their behavior on the same verified Artifact/materialization. Stable direct-URL warm success establishes that the ready fixture was consumable by the older binary.

## Actual outcomes

All acquisitions included `env acquire --artifact DIGEST --permissive-local --json`.

| Recorded command | Numeric exit | Cache class | Receipt valid |
| --- | ---: | --- | --- |
| 04-candidate-office-default-online | 1 | none | none |
| 05-candidate-url-default-online | 0 | provider | True |
| 06-candidate-office-http-carrier-online | 0 | local-materialization | True |
| 07-candidate-office-filesystem-carrier-online | 0 | local-materialization | True |
| 08-stable-url-default-online | 0 | local-materialization | True |
| 09-stable-office-default-online | 1 | none | none |
| 10-stable-office-http-carrier-online | 0 | local-materialization | True |
| 11-candidate-url-default-provider-dead | 1 | none | none |
| 12-candidate-office-default-provider-dead | 1 | none | none |
| 13-candidate-provider-absent-local | 0 | local-materialization | True |
| 14-candidate-url-retained-filesystem-carrier-provider-dead | 0 | local-materialization | True |
| 15-stable-url-default-provider-dead | 1 | none | none |
| 16-stable-provider-absent-local | 0 | local-materialization | True |
| 17-stable-url-retained-filesystem-carrier-provider-dead | 0 | local-materialization | True |

The numbered stdout/stderr/exit files contain complete actual outputs. `results.json` contains every exact argv, private environment, numeric exit, output hash and compact returned receipt. `trust-decision-history-map.json` associates all 14 acquisitions with their durable verification decisions. Failed CLI calls have no success JSON; their durable history records `valid=false`, code `invalid`, diagnostic `trust attachment could not be decoded`.

The documented named provider is real and correctly configured. Candidate and stable both succeed with `--provider office --trust-carrier URL --trust-carrier-type http` while the server runs. Both fail with `--provider office` when no explicit carrier is provided. All failures exit 1 and print `artifact trust attachment verification failed`; the CLI deliberately suppresses the lower-level cause.

The source explains the controlled differential. [`newProviderReferenceWithDependencies`](https://github.com/joshyorko/rcc/blob/09796bf78ab4d79ef91dc7fa4bbb23be7aa14033/cmd/provider.go#L29) resolves the content-provider alias through settings into its HTTP URL. [`optionalEnvironmentTrustCarrier`](https://github.com/joshyorko/rcc/blob/09796bf78ab4d79ef91dc7fa4bbb23be7aa14033/cmd/environmentTrustPolicy.go#L72) independently treats the unexpanded raw provider reference as an HTTP carrier BaseURL. [`HTTPCarrier.attachmentURL`](https://github.com/joshyorko/rcc/blob/09796bf78ab4d79ef91dc7fa4bbb23be7aa14033/artifacttrust/carrier.go#L128) rejects the bare `office` string because it has no HTTP/HTTPS scheme. [`persistTrustFailure`](https://github.com/joshyorko/rcc/blob/09796bf78ab4d79ef91dc7fa4bbb23be7aa14033/environmentlifecycle/trust.go#L63) discards the lower-level error and emits the generic failure observed. The literal `carrier URL must use HTTP or HTTPS` was read in immutable source; it was not printed by the actual CLI. Successful explicit-carrier controls qualify this cause without modifying the implementation.

After the probe's server was terminated and reaped, retaining `--provider URL` with the inferred HTTP carrier failed in both binaries even though the same ready materialization existed. [`acquireLocked`](https://github.com/joshyorko/rcc/blob/09796bf78ab4d79ef91dc7fa4bbb23be7aa14033/environmentlifecycle/materialize.go#L217) verifies its configured trust input before warm return. [`trustRequestFor`](https://github.com/joshyorko/rcc/blob/09796bf78ab4d79ef91dc7fa4bbb23be7aa14033/environmentlifecycle/trust.go#L13) loads the configured carrier and propagates errors; the actual failed decision records an invalid attachment rather than a missing materialization. Server port `connect_ex` after stop was `111`.

Two successful outage controls have different meanings:

1. Removing the provider flag succeeds with local-materialization in permissive-local mode, but it also removes the inferred carrier. Those decisions contain no provenance/SBOM digest or revocation source. This is a distinct local verification input and does not prove that the original HTTP-carrier contract survived the outage. The policy digest alone stays equal and therefore cannot establish equal trust inputs.
2. Retaining the raw provider URL while explicitly selecting the copied filesystem carrier succeeds in both binaries. These decisions retain the same policy digest `sha256:7252b27a0266abd2c3a688e89326e739aed446c3418a8a2248ef01d1b740b6ae`, provenance digest `sha256:ac6261841c42bb74a04ac31067bb62c0a371b26130bbd7f9bc417e854c51ecb4`, SBOM digest and `rcc-publish` revocation source as the online controls. This proves the declared explicit filesystem-carrier local fixture can reuse verified local materialization while its content provider is unavailable. It does not prove strict-remote expiry/revocation freshness or authenticated production behavior.

No transport-operation counter was collected. Successful offline controls show a stopped provider can coexist with valid local reuse under the stated carrier selection; do not convert them into measured zero-call or performance claims.

## Reproduction and disposition

`probe.py` is the full reproduction. Copy it into a fresh evidence directory and run `python /absolute/fresh-directory/probe.py`; it uses fixed hash-checked binary and read-only original-fixture paths, allocates its own loopback port, creates private homes, copies a bounded closure, records every result and stops only its own server in `finally`. The recorded script is intentionally a fresh-run fixture; it does not delete or overwrite an existing run directory.

Recommendation for root: preserve this as a scoped #118 provider/profile/carrier contract finding. Any future implementation should resolve an inferred trust carrier through the same named provider reference semantics, including authentication/transport requirements where applicable, and retain explicit carrier/policy authority. Do not silently drop trust input after an outage. Review a minimal failure-first regression fixture before changing code. This lane implemented no fix and proposes no automatic release hold; the same defect is reproduced in released v18.19.5, while PR #233's separate release/acceptance decisions remain root-owned.

A documentation-only proposed addition to the provider guide is: "When acquisition uses a named provider, declare the trust carrier explicitly until inferred carrier resolution is supported. Warm content reuse does not waive a configured trust-carrier read. For a provider-free local fixture, state the permissive-local policy and filesystem carrier before the outage; removing a carrier after failure changes the verification input."

The probe finished at `2026-10-11T01:31:25.402355+00:00`. Its server PID `246642` exited `0` and was reaped. Logical probe data was `387959764` bytes, below 1 GiB. Original fixture preservation passed. Source and binary hashes, configuration and receipt hashes, exact command logs and cleanup evidence are retained here. Actual Actions execution, authenticated profiles, strict-remote outage/revocation behavior, zero-provider-call counts and all performance conclusions remain unverified.
