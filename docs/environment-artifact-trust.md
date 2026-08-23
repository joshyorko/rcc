# Environment Artifact trust

`artifacttrust` is enforced at the `env acquire` and `env exec` lifecycle
boundary. The command default is `strict-remote`; unsigned local artifacts are
available only with the explicit `--permissive-local` policy. Both modes emit a
machine-readable verification receipt.

`artifacttrust` is an offline verification boundary for Environment Artifact
v1. Artifact identity remains the manifest digest; provenance, SBOMs, and
signatures are detached attestations and cannot change it.

The package provides deterministic SBOM JSON containing sorted package
components and the exact artifact digest, provenance fields for builder/source
and resolution evidence, and detached Ed25519 signatures over the canonical
artifact digest. Signature bundles are artifact-bound envelopes. Verification
does not execute package code and is independent of the provider/carrier; HTTP,
filesystem, and validated offline ZIP carriers use the same attachment names.

Policies are worker/deployment input. An explicit local policy may allow
unsigned local artifacts. A remote or production policy requires a valid
signature, restricts signer keys, builders, platforms, RCC versions, source and
dependency indexes, and can reject revoked artifact digests or signer key IDs.
Verification uses the supplied request time for signature expiry and records a
policy digest, decision ID, signer, provenance/SBOM digests, and revocation
snapshot. Fail-closed revocation mode rejects missing, malformed, future, or
stale snapshots.

Receipts are retained as a latest record plus append-only JSONL history. A new
lease copies the verified decision and links its lease ID into the durable
receipt. Revocation is evaluated for each new acquisition/lease; an already
running lease keeps its recorded decision and is not silently terminated.

Credential values, provider URLs, and mutable operational metadata are not
part of the signed message or SBOM model. Carrier paths, archive members, and
HTTP URLs are validated before traversal, and malformed present attachments
fail closed.

Documentation receipt
- Canonical guidance: this file; documents enforced trust, carrier, receipt, and lease behavior
- Durable learning: trust metadata remains detached from manifest identity while each new lease records the exact verification decision and revocation snapshot
- Evidence: `rcc run -r developer/toolkit.yaml --dev -t artifactFocused`
- Stale guidance removed: v18.19 future-work status and caller-only enforcement claim
- Remaining uncertainty: OCI carrier support remains outside this RCC-owned implementation
