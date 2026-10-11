# RCC quality metrics

RCC publishes only repository and CI-derived signals. It does not collect
user telemetry or send runtime usage data.

The `PR metrics` workflow stores a versioned JSON artifact containing pull
request metadata supplied by GitHub, including the number of requested
reviewers. This is not a count of submitted reviews or approvals. The
`Coverage gate` workflow checks the line threshold in
`.coverage-thresholds.json`. The `Quality report` workflow writes and uploads
the current Go test result as a run artifact and also mirrors it to the GitHub
step summary.

The current 20% line threshold is a temporary, repository-wide baseline floor:
the checkout cannot currently produce a complete baseline because embedded
asset inputs are absent. It is intentionally not presented as a target quality
level; raise it in a reviewed change once a complete baseline is available.

These artifacts are diagnostic evidence, not a claim of production traffic,
agent quality, or merge acceptance. Retention and access are controlled by
GitHub Actions settings.
