# RCC object storage and named heads implementation plan

> For agentic workers: use superpowers:executing-plans to implement this plan task by task. Keep this lane additive and do not create additional workers.

Goal: Add portable, bounded immutable object and expected-parent named-head storage contracts, with an offline-tested S3 HTTP adapter.

Architecture: RCC Blob and Descriptor remain the object identity contract. An additive S3 storage adapter owns protocol validation, bounded staging, conditional writes, and read-only reconciliation. Existing v1 providers and tree-artifact capture/materializers are untouched.

Tech stack: Go 1.26.5 standard library, existing environmentartifact digests, testing/httptest.

Spec: ../specs/2026-10-06-object-storage-cas-design.md

## Global constraints

- Object cap is 64 MiB for this adapter, not a universal tree-artifact format limit.
- No Camp source copying or Camp runtime dependency.
- Strong opaque ETags only; no ETag content-hash interpretation.
- No mutation retries, multipart, CLI/config integration, or live-provider claims.
- GET proves integrity at EOF; Close before verified EOF returns an integrity error.
- Reconciliation uses an independent context bounded to at most 10 seconds.

## Review focus

- Response metadata and actual bytes disagree: reject corruption, oversize and extra trailing data.
- Consumer stops early: close the response and return an unverified-read error.
- A→B→A head digest transitions: unique publication tokens keep old revisions stale.
- Cancellation occurs after remote commit: only bounded readback may establish the outcome.
- 409 and 412 responses: preserve conditional rejection semantics, never retry PUT.

## Task 1: Immutable object transport

Files: artifactprovider/objectstorage.go, artifactprovider/s3storage.go, artifactprovider/s3storage_test.go.

Interfaces: ObjectStorage.PutObject(ctx, Blob) error, GetObject(ctx, Descriptor) (io.ReadCloser, error), HeadObject(ctx, Digest) (StoredObject, error); NewS3Storage(S3StorageOptions) (*S3Storage, error).

- [x] Write HTTP fixture tests for first PUT, exact idempotence, corrupt existing objects, conflicting bytes, invalid descriptors, oversize inputs/responses, bounded staging, cancellation, redirects and malformed metadata.
- [x] Run focused tests and verify feature-missing failures.
- [x] Implement the smallest object transport with bounded one-shot staging, exact hashing, If-None-Match and strong revision validation.
- [x] Verify exact readback after successful or conflicting publication, and streaming GET digest/size checks through EOF with early-close error.
- [x] Run focused package tests.

## Task 2: Named heads and ambiguous outcomes

Files: the same additive implementation/test files.

Interfaces: NamedHeadStorage.ResolveHead(ctx, name) (NamedHead, error), CompareAndSwapHead(ctx, name, expected *NamedHead, next Digest) (NamedHead, error).

- [x] Write failing tests for initial create, expected-parent+revision checks, ABA, concurrent contenders, unknown writes with committed/missing/unreadable state and conditional 409 versus 412.
- [x] Run focused tests and verify feature-missing failures.
- [x] Implement canonical bounded head bodies with a random publication token and atomic If-Match/create-only requests.
- [x] Implement one bounded read-only reconciliation for uncertain writes; retain ErrStorageAmbiguous when exact outcome is unproved.
- [x] Run focused tests and race tests.

## Task 3: Evidence and documentation

Files: scripts/verify-object-storage.sh and docs/object-storage.md; update relevant existing RCC development guidance only with verified reusable knowledge.

- [x] Add a rerunnable low-parallelism focused/race/Windows compile verification script.
- [x] Attempt the repository Go suite after normal generated-asset preparation; report the blocked outbound `https://end` request and unavailable runtime/platform gates.
- [x] Run Windows compile-only and go vet for artifactprovider.
- [x] Document source licensing decision, cap, EOF/Close contract, signer boundary, head CAS and reconciliation rules, and transport-versus-live-provider evidence.
- [x] Review the exact diff and return all evidence to the parent before a draft PR.
