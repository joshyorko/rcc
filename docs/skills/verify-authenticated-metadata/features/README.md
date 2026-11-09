# Authenticated metadata feature map

- [Admission, retained state, and publication](metadata.md): independent authority,
  monotonic checkpoints, restart behavior, renewal/revocation, and carrier readback.

This skill owns metadata authority and distribution. Execution policy selection
and receipt propagation belong to `verify-execution-lifecycle`; leases and child
supervision belong to their respective subsystem skills. Coverage is bounded:
operator renewal/revocation currently has runtime coverage, not a dedicated model.
