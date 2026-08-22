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
4. For metric failures, run `node scripts/pr-metrics.mjs` with the same event
   payload and check the JSON schema/version.
5. Record the failure in the pull request before changing a threshold.

Threshold changes require a reviewed pull request and must include focused
tests or a documented reason. Do not add runtime telemetry, secrets, or
external agent orchestration to these workflows.
