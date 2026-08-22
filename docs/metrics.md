# RCC quality metrics

RCC publishes only repository and CI-derived signals. It does not collect
user telemetry or send runtime usage data.

The `PR metrics` workflow stores a versioned JSON artifact containing pull
request metadata supplied by GitHub. The `Coverage gate` workflow checks the
line threshold in `.coverage-thresholds.json`. The `Quality report` workflow
writes the current Go test result to the GitHub step summary.

These artifacts are diagnostic evidence, not a claim of production traffic,
agent quality, or merge acceptance. Retention and access are controlled by
GitHub Actions settings.
