# Revocation operator lifecycle

`rcc env trust revocations issue|renew|revoke|distribute` manages authenticated
v2 metadata for an **unchanged Environment Artifact digest**. It never rebuilds
an environment, uploads Artifact objects, replaces Artifact signatures, or calls
`env publish`. Artifact-signing authority and metadata authority are independent.
The existing `sign-metadata` migration command and v2 wire format remain compatible.

## Authority and ownership

The deployment provisions `--metadata-trust-roots` independently of the carrier:
`schemaVersion: 2`, a stable `scope`, a nonzero `minimumSequence`, and a `keys` map
of metadata key IDs to base64 Ed25519 public keys with RFC3339 `notBefore` and
`notAfter` bounds. These are **metadata** roots, separate from the Artifact signer
map passed to workers as `--trust-roots`. Keys in an attachment cannot authorize
metadata. The issuer's private key never travels with the snapshot.

Keep roots, trusted clock, signing keys, and retained `--metadata-state` directories
outside carrier-writable paths and disposable RCC homes. Use separate retained
journals for the issuer, distributor, and each worker. A distributor needs public
metadata roots and permission to write only detached metadata; it needs neither
an Artifact private key nor an Artifact object-store upload credential. Provision
HTTP carrier write access externally; the generic carrier does not accept
credential-bearing URLs or invent provider authentication. Filesystem ACLs or an
operator-controlled HTTP client/gateway provide transport authorization.

All operators targeting one scope/Artifact must share their corresponding retained
journal and coordinate with other carrier writers. RCC serializes cooperating
issuer/distributor processes using that journal. Distribution holds the lock
through verification, journal admission, write, and exact-envelope readback. An
independent writer using another journal is outside that ordering guarantee;
workers still reject rollback using their own journals. A partially observed
filesystem write fails closed and may be retried; atomic visibility is not claimed.

## Issue and distribute

The following commands run in the operator's Linux environment. Paths are examples
for deployment-owned files, not default trust discovery. Select the already
published canonical `sha256:` digest as `$artifact`. Review complete provenance,
SBOM, and revocation array files first. `[]` explicitly means no revocations;
`null` or an omitted revocation file is rejected. Omit provenance/SBOM only when
the deployment policy permits their absence. Issuance reaffirms all included
revocations at the new issuance timestamp.

```sh
rcc env trust revocations issue \
  --artifact "$artifact" \
  --metadata-trust-roots /deployment/metadata-roots.json \
  --metadata-state /deployment/issuer-journal \
  --metadata-key-id authority-A \
  --metadata-signing-key /deployment/private/authority-A.key \
  --sequence 40 \
  --issued-at 2026-11-07T20:00:00Z --expires-at 2026-11-08T08:00:00Z \
  --provenance /deployment/reviewed/provenance.json \
  --sbom /deployment/reviewed/sbom.json \
  --revocations /deployment/reviewed/revocations.json \
  --output /deployment/out/snapshot-40.json --json

rcc env trust revocations distribute \
  --artifact "$artifact" --metadata /deployment/out/snapshot-40.json \
  --metadata-trust-roots /deployment/metadata-roots.json \
  --metadata-state /deployment/distributor-journal \
  --trust-carrier /srv/rcc-trust --trust-carrier-type filesystem --json
```

Choose times that are current when executing; the literal dates are illustrative.
Sequence and both times are mandatory. Sequences increase per **scope/Artifact**,
not per signing key. Snapshot lifetime cannot exceed 24 hours and must lie inside
the metadata key's authorization window. Future issuance, expired snapshots,
unauthorized keys, old sequences and same-sequence equivocation fail closed.
An identical envelope at the current checkpoint is an idempotent retry.

Distribution verifies authority and freshness and retains the checkpoint **before**
transport. It writes only `<artifact>/metadata.json`, using the existing carrier's
portable filesystem naming. Existing signatures, objects and manifests are not
rewritten. A success receipt requires exact-envelope readback. `--trust-carrier`
accepts filesystem directories or HTTP(S) carriers supporting PUT and GET; archive
carriers are rejected rather than reporting success for an in-memory edit. Readback
proves that endpoint at that moment, not propagation to every replica or worker.

For offline workers, transport the signed envelope as an ordinary file, then run
`distribute` into the worker's detached filesystem carrier using independently
provisioned roots and a retained distributor journal. Provision the initial
sequence floor independently too. Read-only workers consume that carrier; they do
not need the issuer's key. Archive command ergonomics are a separate concern; this
workflow does not silently attach metadata to or rebuild `.rcca` files.

## Renew an identical Artifact

```sh
rcc env trust revocations renew \
  --artifact "$artifact" --metadata /deployment/out/snapshot-40.json \
  --metadata-trust-roots /deployment/metadata-roots.json \
  --metadata-state /deployment/issuer-journal \
  --metadata-key-id authority-A \
  --metadata-signing-key /deployment/private/authority-A.key \
  --sequence 41 \
  --issued-at 2026-11-08T06:00:00Z --expires-at 2026-11-08T18:00:00Z \
  --output /deployment/out/snapshot-41.json --json
```

Run `distribute` for the new output as above. Renewal authenticates the predecessor,
preserves its provenance, SBOM, and complete deny set, refreshes the revocation
entry timestamps, and signs a higher sequence for the same digest/scope. It cannot
clear a revocation or replace policy inputs. The predecessor can have expired:
its signature/window is checked at historical issuance, while the replacement
must be authorized and fresh now. A future predecessor, retired predecessor key,
or predecessor older than the issuer's retained checkpoint is rejected.

## Revoke the Artifact or an Artifact signer

Use `revoke` with the same issuance flags as `renew`, a new higher sequence, and
one or both explicit targets:

```sh
rcc env trust revocations revoke \
  --artifact "$artifact" --metadata /deployment/out/snapshot-41.json \
  --metadata-trust-roots /deployment/metadata-roots.json \
  --metadata-state /deployment/issuer-journal \
  --metadata-key-id authority-A \
  --metadata-signing-key /deployment/private/authority-A.key \
  --sequence 42 \
  --issued-at 2026-11-08T07:00:00Z --expires-at 2026-11-08T19:00:00Z \
  --revoke-artifact --reason 'deployment operator withdrawal' \
  --output /deployment/out/snapshot-42.json --json
```

Use `--revoke-key-id artifact-signer-A` instead of `--revoke-artifact` to deny an
Artifact signer in this snapshot. This is **not** metadata-root retirement, and
it does not automatically broadcast across other Artifacts: issue/distribute an
updated snapshot for every affected digest. An Artifact revoke denies the digest
regardless of which Artifact key signed it. Renew/revoke preserve all earlier
revocations; a deliberate correction requires `issue` with independently reviewed
complete inputs and a higher sequence, with deployment approval outside RCC.

Distribute the revoke before expecting workers to enforce it. New strict
acquisitions and leases then fail with `revoked`, including local-ready reuse.
The authenticated denial advances the worker's rollback checkpoint; replaying an
older allow snapshot cannot undo it. Already-running children finish under their
original immutable lease decision. This is admission control, not a process-kill
or remote invalidation service.

## Rotation, recovery, scheduling and receipts

For normal metadata-key rotation, independently add the successor public key and
its window to deployment roots, keep the old key during overlap, then `renew`
with the new `--metadata-key-id`, private key and a higher sequence. Distribute the
new snapshot; retire the old public key independently after the overlap. For
compromise/recovery, retire the old metadata key in deployment roots, review the
complete deny/provenance/SBOM inputs, and `issue` under the replacement key at a
sequence above the retained checkpoint. Keep scope and all existing journals.
Do not delete, truncate, restore an older journal, or select another scope to
make a rejected snapshot pass. A compromised metadata authority could already
have signed malicious statements; recovery requires independent deployment
judgment and a floor appropriate for all affected workers, not automatic trust
in a carried predecessor.

RCC does not schedule renewal or distribute automatically. The deployment operator
owns an external scheduler and alerts, renewing/distributing comfortably before
signed expiry, the 24-hour maximum age, and key-window end. Keep refreshed deny
snapshots current too. Observe receipts at the carrier and workers; an issuer
receipt alone does not show distribution. Offline withholding is bounded by expiry
and the independently provisioned floor, not knowledge of globally latest unseen
state. Raising the floor can protect newly provisioned workers; retain the
existing journals on established workers.

Operator `--json` receipts report operation, code, digest, scope, sequence, key ID,
validity bounds, metadata digest, predecessor digest for renew/revoke, and completed
`retained`/`distributed` side effects. A transport failure reports
`metadata-distribution` with `retained: true`; retry the **same envelope** against
the same journal. Output files are exclusive and flushed. An output persistence
failure reports `metadata-output`; retain the journal and recover with reviewed
inputs and a higher sequence if the envelope was lost. Never reset state to retry.
Underlying transport errors/key contents are not printed into operator receipts.

Worker verification codes retain their v2 meaning: `metadata-unsigned` (missing),
`metadata-invalid` (malformed/forged), `metadata-authority` (wrong roots/scope/key),
`metadata-expired` (expired/future), `metadata-rollback` (old/equivocal),
`metadata-state` (unavailable/corrupt journal), and `revoked` (authenticated deny).
Use `env trust verify --metadata ... --strict-remote --json` with independent
Artifact/metadata roots and state for an explicit machine-readable decision.
Never work around a failure with `--permissive-local`.

## Deterministic acceptance and scope

```sh
GOARCH=amd64 CGO_ENABLED=0 go test ./artifacttrust ./cmd -run 'TestMetadataOperator|TestRevocationOperatorRequiresExplicitInputs' -count=1
RCC_ACCEPTANCE_BINARY=/absolute/candidate/rcc \
RCC_BUNDLE_TEST_ARCHIVE=/absolute/native-fixture.rcca \
RCC_REVOCATION_EVIDENCE=/absolute/new/evidence \
  go test ./cmd -run '^TestRevocationOperatorRealCLI$' -count=1 -v
```

The real CLI test reuses the native archive harness: initial strict acquisition,
renewed local-ready acquisition and fresh lease, independent digest/signer revokes,
new-lease denial, a real running Python child completing with its original decision,
and missing/forged/unauthorized/old/future/expired negatives. Input/binary hashes,
commands and receipts are captured. Independent-process tests exercise concurrent
publication ordering against one retained journal. These are process/restart and
admission checks, not power-loss, storage-controller durability, host-compromise,
or instantaneous fleet revocation claims. Native Linux execution does not qualify
Windows or macOS; each platform requires its own native binary/archive run.
