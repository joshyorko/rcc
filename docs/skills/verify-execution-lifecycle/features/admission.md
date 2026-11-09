# Artifact execution admission

Source synchronization: `cf7a47e7b9430db4abebae68776f65f3e309624e`.

`cmd/robotBundleTrust.go:bundleTrustOptions.acquireWith` selects policy and checks
matching verification receipts. `materializationForBundledArtifact` carries that
context to `environmentlifecycle/lease.go:LocalMaterializer.Lease`, which refreshes
trust before publishing a lease. `environmentlifecycle/materialize.go:Acquirer.acquireLocked`
retains its legacy nil-policy path; only explicit local opt-in satisfies the modeled
operator contract. Content can enter CAS before detached trust admission.

Drive: `go test ./cmd -run '^TestBundleTrustDefaultsToStrictAndCarriesLeaseRefreshContext$' -count=1`.
Run the fixed/broken/restored [existing checker](../../../formal/execution-lifecycle/check.py).
The deliberate model policy loss is a sensitivity check, not a current product bug.
Real native bundle CLI acceptance needs the built candidate and fixture described
in [bundle trust](../../../bundle-artifact-trust.md); a skip is missing evidence.
