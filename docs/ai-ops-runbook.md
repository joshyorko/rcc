# AI and CI observability runbook

## What is measured

Use the `Coverage gate`, `PR metrics`, and `Quality report` workflows. Their
artifacts and step summaries are the source of truth for this repository's
quality signals. A green workflow is evidence for that run only; it is not a
production or user-telemetry claim.

## Triage

1. Open the failed workflow run and preserve its log and artifact URLs.
2. Reproduce locally with `go test ./... -count=1`.
3. For coverage failures, inspect `coverage.out` and the threshold file.
4. For metric failures, run `GITHUB_EVENT_PATH=scripts/fixtures/pr-event.json
   GITHUB_REPOSITORY=joshyorko/rcc node scripts/pr-metrics.mjs` and check the
   JSON schema/version. The fixture demonstrates requested reviewer count; it
   does not represent submitted reviews or approvals.
5. Record the failure in the pull request before changing a threshold.

The 20% coverage value is a temporary baseline floor, not a quality target.
Changing it requires a reviewed pull request, a complete coverage baseline,
and a documented reason. Do not add runtime telemetry, secrets, or
external agent orchestration to these workflows.
