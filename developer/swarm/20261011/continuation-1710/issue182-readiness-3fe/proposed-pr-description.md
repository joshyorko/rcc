Title: test(journal): cover persistence and lifecycle

## What changed

Add deterministic tests for run-journal lifecycle and stop behavior, warranty-voided append suppression, missing-journal errors, and build-event persistence/reload/statistics filtering. Existing append/reload, malformed-line, and unavailable-append tests remain in place. The change is limited to `journal/coverage_test.go`.

## Validation

On source head `36a84430083b3af6f9758408b94054e5197b7f8b` with Go 1.26.9 and CGO enabled:

```sh
CC=/usr/bin/gcc /workspace/work/rcc-swarm/tools/go1.26.9/bin/go test -p 1 -count=1 -cover ./journal
```

Result: exit 0; journal package coverage 82.5%.

## Follow-up

This is a focused journal test checkpoint. Issue #182 remains open until a reproducible repository-wide coverage run succeeds after embedded assets are generated.
