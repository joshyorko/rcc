# Authenticated detached metadata v2 (implementation in progress)

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
fail closed and are never silently truncated/reset. Native process-crash,
concurrency, disk-failure and Windows persistence qualification is outstanding.

## Compatibility and migration

Strict callers now require v2 metadata and an independent retained state path.
They must provision metadata roots/floor, review the old metadata, and obtain a
new authority signature. Historic unsigned metadata is not upgraded merely by
parsing it. `env trust sign-metadata` signs explicit reviewed inputs; it does not
rotate real keys or publish evidence. `--metadata-trust-roots` and
`--metadata-state` are paired inputs for acquire/exec/bundle/verification.
Explicit permissive-local remains the controlled legacy path; it is not an
authenticated-v2 guarantee. Existing strict fixtures/consumers still need their
migration updates before this branch is a release candidate.

## Integration and qualification

The prepared base is #264 -> #265 -> #266 -> #267. #267's original commit is
81e8fdad2b5559e5aac5488c8ab5a7601f1e0c6f; attributed local integration is
0763420. The #253 PR is based on this union, not on an isolated green #267.

Current evidence covers baseline attacks, core envelope mutation rejection,
authority windows, sequence/key rotation, journal reopening and partial-record
failure. This is not completion: full CLI/carrier/archive integration, canonical
migration fixtures, multi-process/crash gates, narrow formal model revision,
N-1 and four-platform exact-head hosted qualification remain required. Old
fc718cd CI is not reused as security qualification. Recommendation: NO-GO.
