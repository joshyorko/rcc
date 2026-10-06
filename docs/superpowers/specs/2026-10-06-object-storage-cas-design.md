# RCC immutable object storage and named heads

## Intent and ownership

Add RCC-native object publication and named-head contracts for tree-artifact consumers without changing capture, materialization, CLI wiring, or the existing v1 manifest provider. All implementation files are additive under `artifactprovider`. There is no Camp runtime dependency and no Camp source is copied. RCC's root license is Apache-2.0. The inspected Camp source tree has no root source license; conceptual safety requirements are used without source adaptation.

## Caller contract

`ObjectStorage` supplies `PutObject(context.Context, Blob) error`, `GetObject(context.Context, environmentartifact.Descriptor) (io.ReadCloser, error)`, and `HeadObject(context.Context, environmentartifact.Digest) (StoredObject, error)`.

`NamedHeadStorage` supplies `ResolveHead(context.Context, string) (NamedHead, error)` and `CompareAndSwapHead(context.Context, string, *NamedHead, environmentartifact.Digest) (NamedHead, error)`. Nil parent means create only. A non-nil parent must have the same name, a valid digest, and an opaque revision. CAS checks both the observed parent digest and its revision before issuing `If-Match`. Revision, not an interpreted ETag digest, protects against stale and ABA writes. Each new head includes a random publication token so the same digest cannot recreate an earlier head body/revision.

## S3 transport

`NewS3Storage(S3StorageOptions)` implements both interfaces with the Go standard library and an injected per-request signer. Options contain no credentials. HTTPS is required unless insecure HTTP is explicitly enabled. Validate DNS-compatible buckets, clean optional prefixes, endpoint authority without credentials/path/query/fragment, and path-style policy for dotted buckets and IP literals. Refuse redirects to protect authorization material.

This bounded adapter supports single-object PUT only, at most 64 MiB per object, and publishes no multipart state. It stages and hashes a one-shot Blob before any PUT. Object keys contain only canonical SHA-256 values; head names are bounded safe path segments. Immutable PUT uses `If-None-Match: *`. A duplicate succeeds only after exact size, digest, and stable-revision GET readback. Content hashes and opaque ETags are separate concepts. GET verifies size and SHA-256 through EOF and rejects extra bytes.

HEAD metadata is not integrity proof. After a successful write or a conditional conflict, verify readback. A transport failure, cancellation during PUT, or 5xx may have committed. Use an independent bounded reconciliation context and read the resulting key once, never retry a mutation blindly. Exact expected bytes and publication token prove success. Missing or inconclusive readback preserves a typed ambiguous result. A deterministic 412 with different bytes is a typed conflict. Concurrent immutable identical writes can both succeed; competing named heads have one winner.

## Alternatives

Implementing the existing v1 `Provider` would also require legacy manifest closure verification and would broaden this lane into unrelated compatibility work. It is deferred. Copying Camp's complete multipart adapter is excluded by scope and unresolved source licensing. The additive RCC-native object/head contracts isolate these concerns.

## Verification and limits

Offline HTTP protocol fixtures prove conditional headers, immutable idempotence/conflict, malformed paths and responses, bounded objects/readback, corruption, stale/ABA parent checks, concurrent CAS, lost-response reconciliation, cancellation, and one mutation attempt. Run focused tests, race tests, repository Go tests, and Windows compile-only for the touched package. A signer hook and synthetic S3 HTTP fixtures do not prove live AWS, MinIO, or R2 compatibility. No account credentials, live provider, multipart, CLI, materializer, or native Windows runtime claims are included.
