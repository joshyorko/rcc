# Pull request review rubric

Review the submitted change against RCC's observable behavior and delivery
contract. A checked box or named tool is not evidence by itself.

1. **Correctness:** the change satisfies its linked issue and covers failure
   and recovery behavior where applicable.
2. **Compatibility:** public CLI, configuration, bundle, profile, endpoint,
   and platform contracts remain compatible unless the PR declares and
   justifies a break.
3. **Safety:** filesystem boundaries, credentials, network access, telemetry,
   generated assets, and external side effects are explicit.
4. **Verification:** focused Go tests precede broader checks; Robot or
   platform evidence is included when runtime behavior depends on it.
5. **Delivery truth:** source, build, runtime, push, and release status are
   reported separately, with skipped or unavailable gates named.
6. **Scope:** the diff is cohesive, links its issue, preserves unrelated work,
   and does not mix release ceremony into ordinary implementation.

Approve only evidence demonstrated at the submitted head. Request changes for
missing correctness or safety evidence; use comments for non-blocking follow-up.
