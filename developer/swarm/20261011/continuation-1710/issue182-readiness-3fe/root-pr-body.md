Add deterministic journal tests for run lifecycle and stop behavior, warranty-voided append suppression, missing storage errors, and build-statistics persistence, reload and filtering. The current-main diff is one test file with 114 additions. Frozen v18.19.6 source 7945 is unchanged.

Validation: accepted source36a focused Go1.26.9 CGO journal test exit0, coverage82.5%. This head merges current main3fe without changing the accepted test bytes; fresh hosted checks are required before integration.

Refs #182. Keep the issue open pending reproducible repository-wide coverage and its remaining acceptance criteria.
