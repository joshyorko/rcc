# Immutable object storage and named heads

`artifactprovider.ObjectStorage` is an additive storage contract for immutable
RCC objects. `S3Storage` implements it and `NamedHeadStorage` through a bounded
S3 HTTP transport. It uses RCC's `Blob`, `environmentartifact.Descriptor`, and
SHA-256 digests. It does not implement the legacy v1 manifest `Provider`, change
provider profiles, or wire a CLI, tree capture, or materializer.

Related architecture: [cloud-native environment engine issue #118](https://github.com/joshyorko/rcc/issues/118).

## Objects

Construct `S3Storage` with a non-secret `S3StorageOptions` value and an optional
runtime `StorageSigner`. The signer receives the final method, URL, conditional
headers, and exact `X-Amz-Content-Sha256` payload hash. Credentials belong only
in the signer. This package does not discover credentials or provide a SigV4
implementation. Anonymous access is possible without a signer.

HTTPS is the default. HTTP requires `AllowInsecureHTTP`; the same option is
rejected on an HTTPS endpoint. Endpoints cannot include credentials, a path
other than `/`, query, or fragment. DNS-compatible bucket names, safe prefix
segments, and complete keys bounded to 1024 bytes are required. Dotted buckets
and IP-literal endpoints require path-style addressing. Redirects are refused,
including redirects returned by an injected HTTP client.

Each upload stages the one-shot reader in a private temporary file, verifies
exact size and SHA-256, and removes the file on success, error, or cancellation.
Staging uses at most the declared size plus one byte to detect excess data.
The adapter supports single PUT objects up to 64 MiB, configurable downward.
This is an adapter limit, not a universal tree-artifact format limit. It does
not create multipart state.

Immutable publication uses `If-None-Match: *`. An existing identical object is
accepted only after verified GET readback at its strong opaque revision.
Different bytes are a conflict. Metadata alone does not prove integrity, and
an ETag is never interpreted as a content hash.

`GetObject` streams verification. Read through EOF, check the read error, and
check `Close`. A partial consumer has not verified the object. Closing before
verified EOF returns `ErrStorageIntegrity` and closes the HTTP body. Missing,
weak, duplicate, malformed, oversized, truncated, trailing, or corrupt content
fails closed. An individual reader is not safe for concurrent Read/Close calls.
Cancellation is checked while staging and reading; a caller-supplied reader
must itself return from Read for cancellation to interrupt that Read.

## Named heads

`CompareAndSwapHead(ctx, name, nil, target)` creates an absent head only.
For an update, supply the `NamedHead` returned by `ResolveHead`. The supplied
parent must match both the target digest and the strong opaque revision.
The write sends `If-Match` for that exact revision.

Every publication includes a fresh random token. An A-to-B-to-A target sequence
therefore creates different head bodies, so an old revision stays stale.
Readback must reproduce the exact proposed body and a new strong revision;
the same target digest alone cannot establish success. The head body is
canonical JSON with a 4096-byte bound. Names are validated before requests.

This contract does not verify the target object or its tree/manifest closure.
A publishing consumer owns that validation and must publish its immutable
closure before advancing the named head. It must retain a losing immutable
candidate when conflict recovery needs it; this adapter performs no deletion.

## Write outcomes

The adapter sends each mutation once. After a transport failure, cancellation
at the write boundary, or 5xx response, it performs read-only reconciliation
under an independent context. That context defaults to five seconds and cannot
exceed ten seconds. The HTTP client has a finite timeout too.

Exact verified object bytes or the unique proposed head body can establish
success after a lost response. Missing, corrupt, unreadable, or superseded
readback keeps `ErrStorageAmbiguous`. It is not evidence of rollback.
A signing failure occurs before transmission and does not trigger
reconciliation. Signer failures and backend diagnostic bodies are not logged
or included in returned errors. Errors from the caller-supplied upload reader
are returned to that caller unchanged; this redaction rule covers backend and
signer diagnostics, not errors authored by the caller.

HTTP 409 and 412 are conditional rejections, rather than uncertain 5xx outcomes.
The adapter can recognize an exact existing immutable object, but otherwise
returns `ErrStorageConflict` and does not retry the PUT. These statuses are not
interchangeable at the backend: an actual S3 implementation may use 409 for
an in-flight competing operation and 412 for a failed condition. The caller
must reconcile and choose its next operation; it must not blindly replay an
unknown mutation.

## Verification boundary

Run from a checkout with Go 1.26.5, a C compiler, Python/Invoke, and canonical
assets prepared with `python -m invoke assets`:

```sh
scripts/verify-object-storage.sh
```

The script runs focused tests, Linux race tests, package vet, Windows amd64
compile-only with bounded parallelism. Output is
under `tmp/object-storage-verification` unless explicitly overridden. The
focused test fixture verifies HTTP conditional headers, concurrent contenders,
immutable conflict/idempotence, stale and ABA parent rejection, bounds,
corruption, early close, staging cleanup, cancellation, redirect refusal,
signer payload hashes, lost-response reconciliation, 5xx, and no mutation retry.

These are offline transport contracts. An injected signer and synthetic HTTP
fixtures do not certify AWS, MinIO, or R2 compatibility. Live provider behavior,
SigV4 credentials, native Windows runtime, multipart, CLI and materializer
acceptance require separate evidence before those claims are made.

Repository-wide `go test ./...` remains a separate verification gate. The
unchanged `operations.TestCanCreateAndDeleteAccount` uses a real client to send
a synthetic account deletion request to `https://end`; it is not an offline
fixture. A denied outbound request must be reported as a blocked full suite,
not routed through another transport or treated as a passing result.

## Source reuse and license

RCC's root `LICENSE` is Apache-2.0. The inspected Camp commit
`44e2dbc9051416500756975a02763d887899494c` had no root/source license file;
`test-camp/LICENSE` is a fixture license and does not establish source licensing.
The RCC implementation is newly authored. Camp's immutable publication and
unknown-outcome requirements informed the design, but no Camp source was
copied and no Camp runtime dependency was introduced.
