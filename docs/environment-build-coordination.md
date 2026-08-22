# Environment build coordination and prewarming

RCC may coordinate cold builds, but coordination is an optimization and capacity
control seam. The verified Environment Artifact remains the only authoritative
result. Local RCC operation must continue to work without a coordinator or
provider, and workers must never require a shared writable Holotree.

## Stable build identity

A build claim is keyed by a canonical, versioned record, not by a mutable package
path, provider URL, or Holotree space name:

```text
build-key-v1(
  environment-spec-digest,
  target-platform-compatibility,
  builder-kind-and-version,
  dependency-resolution-policy-revision,
  trust-and-build-policy-revision,
  required-artifact-schema-and-encoding-features
)
```

The canonical record is hashed with the digest algorithm named by its schema.
Unknown required fields or features fail closed. Operational metadata such as
queue priority and provider URL does not change the key.

## Claim lifecycle

The optional coordinator exposes a compare-and-swap claim with an owner,
monotonic claim epoch, heartbeat, and expiry. RCC treats the claim as a lease to
perform work, never as authorization to trust bytes:

```text
claim(key) -> owner, epoch, heartbeat, expiry
build in worker-local staging
verify complete Artifact closure and policy
publish immutable objects
atomically commit Manifest
complete/release(key, epoch)
```

Builders use a worker-local staging root with reservations for disk and bounded
resource, network, credential, and cancellation policy. Production Action
secrets are excluded from build inputs. Failed staging is removed or quarantined
under the worker's RCC home and can never be mistaken for a committed Artifact.

Publication is idempotent and content-addressed. A Manifest is committed only
after every referenced object is complete, digest-verified, and authorized by
provider/trust policy. A stale epoch cannot replace a committed Manifest. If two
valid builders publish different bytes for one key, the provider records both
Artifact identities and reports nondeterminism; it does not silently choose one.

Coordinator loss is not data loss: a permitted local build proceeds without the
coordinator. A waiter may wait with bounded backoff, decline, build independently,
or use another trusted provider. Existing committed Artifacts always win over
stale claims. Claim ownership is separate from local materialization locks,
Artifact trust, and Actions Run/Attempt fencing.

## Required failure behavior

| Event | Required result |
| --- | --- |
| owner exits before or during resolution/build | claim expires; another owner may take over after the epoch changes |
| owner exits before publication | staging is non-authoritative; a new builder or trusted provider supplies the Artifact |
| partial objects exist without a Manifest | objects are ignored by acquisition and cleaned by bounded GC |
| Manifest commits before owner acknowledgment | committed Artifact remains authoritative; completion is idempotent |
| stale owner publishes conflicting content | publication is rejected or stored as a distinct identity; committed state is unchanged |
| provider/coordinator is unavailable | use a verified local Artifact, permitted local build, another provider, or an explicit failure |
| identical key produces different outputs | mark nondeterministic with both identities and apply policy; never report an equivalent cache hit |

Notifications and heartbeats are advisory. A notification never makes incomplete
content executable, and an expired claim is not proof that a process stopped
using files; local execution leases remain responsible for materialization
lifetime.

## Prewarming

Prewarming is a separate planning operation. Actions supplies desired Artifact or
Environment Specification identities plus worker policy; RCC does not learn
deployment or business semantics. A plan contains the identity, target platform,
worker/cache target, concurrency and disk limits, old-generation retention, and
policy revision.

Execution first checks the worker-local verified cache and does no network or
provider work when the Artifact is ready. Otherwise it acquires or materializes
through the normal verified Artifact lifecycle, bounded by concurrency, disk,
cancellation, and retry policy. It reports `ready`, `failed`, or `degraded` with
Artifact identity, bytes, timings, and reason. Placement and cache affinity are
hints only.

During a rolling update, old and new generations coexist. Existing leases retain
the old generation until they drain; new work routes to the ready new generation.
Capacity failure leaves the old generation usable and reports `degraded` rather
than deleting or mutating either generation.

## Offline test matrix

The implementation must exercise two racing builders, takeover after every
owner-crash boundary, partial publication, stale publication after commit,
provider/coordinator loss, nondeterministic output, and rolling-update prewarming
under disk pressure. Tests use temporary worker/provider roots and fake
clocks/providers; they do not require a shared writable Holotree or live network.

