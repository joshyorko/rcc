# Coordinator admission of the profile hypothesis

The retained profiles support investigating repeated filesystem work, not removing integrity checks. The proposal in `attribution-and-next-prototype.md` has **not** been admitted as an integrity-preserving implementation.

Checking that a cached root FD still matches its pathname only at session end is too late if `CommitManifest` or legacy installation has already made content visible. Holding a directory FD also changes the existing pathname semantics after an ancestor or shard is renamed or replaced. A future prototype must define those semantics, check the required path/FD identity before each visible commit or installation, and independently exercise ancestor/shard replacement, symlink substitution, cancellation and descriptor cleanup. An owned immutable read-only snapshot might permit a simpler contract, but that contract must be explicit and must not be attributed to the current filesystem provider.

No provider checks, production API, format, defaults or shipping source were changed for this diagnosis. All original profiles and the old failed private-home benchmark remain retained; no completed timed wave was replayed.
