# Authenticated detached metadata v2

## Threat boundary

The Artifact provider may substitute bytes, which immutable descriptor/Manifest
verification must reject. A carrier adversary may replace, remove or replay any
detached attachment. It cannot sign as an independently provisioned metadata
authority, change deployment roots/floors, modify the retained local journal,
or control the worker's clock/filesystem. Host compromise and restoring an old
trusted-state backup are outside this boundary: deployment recovery must advance
the independently configured minimum sequence before resuming strict use.

#126 established v1 Artifact signatures and detached digest-bound assertions.
#252 propagated explicit policy through acquisition and lease refresh. Neither
authenticated those assertion bytes. Baseline fc718cd reproductions show source
and dependency allowlist bypass in Policy.Verify, plus actual CLI acceptance of
newer-empty and older-empty revocation snapshots after a revoked result. The
CLI has no source allowlist flag, so that specific bypass is an API reproduction.

## Format and authority

One v2 envelope carries a complete snapshot: Artifact digest, scope, sequence,
metadata key ID, issuance, expiry, provenance, SBOM and revocations. Ed25519 signs
the exact canonical decoded payload bytes with a v2 domain separator. The
snapshot cannot be assembled by mixing authenticated and legacy fields.
Artifact identity and v1 Artifact signatures are unchanged.

Metadata roots are a separate deployment-owned v2 file containing scope,
minimum sequence and authorized public keys with validity windows. No key is
inferred from a provider, archive or carrier. Key rotation is authorized through
that independent configuration: overlap old/new windows when required, remove
retired keys, and retain the same scope/Artifact sequence history. Changing key
ID cannot reset the sequence namespace. First use must meet the deployment's
minimum sequence; expiry bounds freshness but does not establish globally latest
state without that independent floor.

## Retained rollback admission

The journal is keyed by scope and Artifact, not signing key or mutable carrier
URL. A lower sequence or equal sequence with different envelope digest fails.
The authenticated checkpoint is flushed before any policy success is returned,
including when its revocations then deny the Artifact. Thus a revoked snapshot
cannot be undone by replaying an earlier valid empty one after restart.
Cross-process locks serialize comparison and append. Partial/corrupt journals
fail closed and are never silently truncated/reset. Failed durability barriers
must complete on retry even when the record is already present. Journals stop
admitting at 8 MiB; recovery requires an independently advanced deployment floor
before a replacement state directory can be provisioned. Never delete state to
make a rejected snapshot pass. The carrier cannot write the journal, its
ancestors, roots, or aliases to those files; separate paths alone do not enforce
that operating-system permission boundary.

Linux/macOS flush the journal and parent directories. Windows uses exclusive
process locking and file flush; the current guarantee is retained state across
process restarts on a trusted filesystem. Power-loss/storage-controller behavior,
malicious network filesystems and host-state rollback are not established by
process tests. Do not treat a temporary RCC home or restored old backup as a
deployment rollback anchor.

## Compatibility and migration

Strict callers now require v2 metadata and an independent retained state path.
They must provision metadata roots/floor, review the old metadata, and obtain a
new authority signature. Historic unsigned metadata is not upgraded merely by
parsing it. `env trust sign-metadata` requires an explicit reviewed revocation
array (including an explicit empty array when appropriate); it does not
rotate real keys or publish evidence. `--metadata-trust-roots` and
`--metadata-state` are paired inputs for acquire/exec/bundle/verification.
Explicit permissive-local remains the controlled legacy path; it is not an
authenticated-v2 guarantee. The legacy `Policy.Evaluate` signature-only API
cannot authorize strict execution. Build coordination separately verifies its
complete signed statement through `Policy.VerifyStatement`, which rejects
detached-metadata policies and cannot issue an execution receipt.

Each receipt records the envelope digest, authority-configuration digest,
scope, sequence and metadata key ID. Changed snapshots or authority roots yield
distinct decisions without changing Artifact identity or rewriting old receipts.
Authority configuration is selected per invocation; carrier metadata is refreshed
at acquisition and each new lease. Retiring a key in deployment configuration
applies to new invocations; running processes retain their admitted decision.

Filesystem, HTTP and separate ZIP trust carriers transport the same envelope.
The lifecycle archive API preserves `attestations/metadata.json` when explicitly
given a trust carrier. The public CLI still requires an independently selected
carrier alongside `.rcca`; it does not infer roots from archive contents or
publish into a remote carrier during acquisition. This does not implement the
broader archive-publication UX in #255 or revocation-management UX in #254.

N-1 clients cannot enforce this format. Passing v2-required flags to v18.19.7
fails as unsupported rather than silently weakening policy. Its legacy v1
signature workflow remains the old security contract. Pin v2-capable clients
for v2-required deployments, and do not share mutable RCC homes/receipt stores
between old and new clients. Immutable Artifacts remain reusable after a new
authority approves their metadata; no Manifest identity migration is needed.

## Integration and qualification

The prepared base is #264 -> #265 -> #266 -> #267. #267's original commit is
81e8fdad2b5559e5aac5488c8ab5a7601f1e0c6f; attributed local integration is
0763420. The #253 PR is based on this union, not on an isolated green #267.

Run the affected Go packages, then the built-CLI security matrix:

```sh
go test ./artifacttrust ./buildcoord ./cmd ./environmentlifecycle
RCC_ACCEPTANCE_BINARY="$PWD/build/rcc" \
  RCC_METADATA_EVIDENCE=/absolute/task-evidence/metadata \
  RCC_N_MINUS_ONE_BINARY=/absolute/released-v18.19.7/rcc \
  go test ./cmd -run '^TestMetadataSecurityRealCLI$' -count=1 -v
RCC_ACCEPTANCE_BINARY="$PWD/build/rcc" \
  RCC_BUNDLE_TEST_ARCHIVE=/absolute/native-fixture.rcca \
  go test ./cmd -run '^TestBundleTrustRealCLI$' -count=1 -v
python3 docs/formal/execution-lifecycle/check.py --model AuthenticatedMetadata
```

Use a disk-backed task TMPDIR and private homes; retain raw output, exits and
binary hashes. The N-1 subtest is skipped without its actual executable. Native
CI runs the security matrix against its exact staged binary on all four targets.
The bounded model and its real-code mutation evidence are in
`docs/formal/execution-lifecycle/evidence/metadata-v2/README.md`. Full five-PR
qualification is tracked in the existing release matrix. No prior fc718cd CI is
reused as proof of this format. Until new exact-head gates complete: NO-GO.
