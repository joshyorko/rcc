# Environment Artifact trust

> **v18.19.0 status:** `artifacttrust` is an offline internal seam. RCC does not
> enforce signatures, provenance, SBOM, or revocation during `env acquire` or
> `env exec` in v18.19.0, and it does not emit a stable verification-receipt
> CLI contract. Those policies remain future additive work.

`artifacttrust` is an offline verification boundary for Environment Artifact
v1. Artifact identity remains the manifest digest; provenance, SBOMs, and
signatures are detached attestations and cannot change it.

The package provides deterministic SBOM JSON containing sorted package
components and the exact artifact digest, provenance fields for builder/source
and resolution evidence, and detached Ed25519 signatures over the canonical
artifact digest. Verification does not execute package code and works before a
carrier or provider is selected.

Policies are worker/deployment input. An explicit local policy may allow
unsigned local artifacts. A remote or production policy can require a valid
signature, restrict signer keys, builders, and platforms, and reject revoked
artifact digests or signer key IDs. Revocation records are additive evidence;
they do not rewrite or delete the immutable artifact, its attestations, or
execution records. Running leases are unaffected by this package; callers must
apply revocation when deciding whether to start a new execution.

Credential values, provider URLs, and mutable operational metadata are not
part of the signed message or SBOM model.

Documentation receipt
- Canonical guidance: this file; documents the artifact trust boundary and policy behavior
- Durable learning: trust metadata must remain detached from the manifest identity
- Evidence: `go test ./artifacttrust`
- Stale guidance removed: none
- Remaining uncertainty: execution callers must persist verification receipts and apply revocation before new leases
