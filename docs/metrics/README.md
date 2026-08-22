# Public metrics contract

The public quality surface is limited to CI-derived, non-user data:

- configured test coverage threshold and observed coverage;
- Go test success or failure; and
- pull request metadata emitted by `scripts/pr-metrics.mjs`.

No RCC runtime telemetry or personally identifying data is exported.
