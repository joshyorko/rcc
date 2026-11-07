# Environment Artifact trust

`artifacttrust` is enforced at the `env acquire` and `env exec` lifecycle
boundary. The command default is `strict-remote` with fail-closed revocation
freshness; unsigned local artifacts are available only with the explicit
`--permissive-local` policy. Both modes emit a machine-readable verification
receipt.

## RCC object inventory and package SBOM

`artifacttrust` is an offline verification boundary for Environment Artifact
v1. Artifact identity remains the manifest digest; provenance, object
inventories, package SBOMs, and signatures are separate from that identity.

`ObjectInventory` is deterministic RCC JSON containing sorted immutable
Hololib object records bound to the exact artifact digest. Its
`application/vnd.rcc.environment.inventory.v1+json` media type is RCC-specific;
it is not SPDX or CycloneDX and does not enumerate installed Conda, uv, or pip
packages, licenses, or CVE coverage. A real package SBOM remains future work.
`env publish` stores generated provenance, an object inventory, and a fresh
revocation snapshot in the local trust carrier; pass `--inventory` for an
explicit RCC inventory. The deprecated `--sbom` flag is an alias and accepts
the same RCC inventory contract, not a standards-format package SBOM. An
Ed25519 key can be supplied for post-manifest signing. Verification does not
execute package code and is independent of the provider/carrier; HTTP,
filesystem, and validated offline ZIP carriers use the same attachment names.
`env acquire` and `env exec` expose explicit filesystem/archive carrier
selection. Strict consumers supply their deployment-owned Ed25519 public keys
at runtime with `--trust-roots`; RCC binds those key IDs into the policy and
never treats a provider signature as its own trust root. Public keys accept
standard Base64 with or without trailing padding, and the decoded Ed25519 key
must be exactly 32 bytes.

Earlier RCC releases used the misleading media type
`application/vnd.rcc.environment.sbom.spdx+json` for this custom inventory.
Readers continue to accept that legacy type and preserve its bytes, including
when carried inside authenticated metadata v2. New inventories use the RCC
media type above. Convert an old record explicitly by reading its components
into `ObjectInventory` and writing it with `ObjectInventoryMediaType`; RCC does
not rewrite signed metadata or relabel authenticated bytes during verification.
The `sbom` field name remains in the v2 signed payload for wire compatibility.
It may contain an RCC object inventory and must not be treated as a package
SBOM by consumers.

Publication stages the required trust set before committing the provider
manifest, so a failed trust write leaves no visible manifest; a later retry can
reuse the staged objects and commit atomically.

Policies are worker/deployment input. An explicit local policy may allow
unsigned local artifacts. A remote or production policy requires a valid
signature, restricts signer keys, builders, platforms, RCC versions, source and
dependency indexes, and can reject revoked artifact digests or signer key IDs.
Verification uses the supplied request time for signature expiry and records a
policy digest, decision ID, signer, provenance/inventory digests, and
revocation snapshot. The receipt retains the historical `sbomDigest` JSON key
for the inventory digest. Fail-closed revocation mode rejects missing,
malformed, future, or stale snapshots.

Receipts are retained as a latest record plus append-only JSONL history. A new
lease re-fetches and re-evaluates its configured carrier, copies the resulting
decision, and links its lease ID into the durable receipt. Revocation is
evaluated for each new acquisition/lease; an already running lease keeps its
recorded decision and is not silently terminated. Malformed carrier input also
creates a failure receipt before the trust decision is returned.

Credential values, provider URLs, and mutable operational metadata are not
part of the signed message or object inventory model. Carrier paths, archive
members, and HTTP URLs are validated before traversal, and malformed present
attachments fail closed.

Documentation receipt
- Canonical guidance: this file, `authenticated-metadata-v2.md`, and `holotree.md`; distinguish the RCC object inventory from package SBOM coverage
- Durable learning: legacy SPDX-labeled custom inventory media types and signed v2 bytes remain readable without relabeling
- Evidence: pre-fix media-type regression reproduced; focused Environment Artifact Go tests and `go vet` passed on Linux/amd64 with cached Go 1.27.3; native CLI accepted `--inventory` and deprecated `--sbom`
- Stale guidance removed: claims that the custom inventory contains installed package components
- Remaining uncertainty: true package SBOM generation and Go 1.26.9 / macOS / Windows / Robot gates remain unverified
