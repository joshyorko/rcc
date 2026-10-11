# Additive finding-level erratum

The earlier report generalized the structured Govulncheck traces too narrowly. I decoded all concatenated JSON objects in each of the eight preserved complete streams. Every binary has 39 findings: 26 GO-2026-5970 and 13 GO-2026-6629. Across all eight, the trace frames comprise 16 module/version-only records, 16 module/package/version records, and 280 module/package/function/receiver/version records. Per binary, that is 2, 2, and 35 respectively.

Each finding has exactly one frame, and each frame names the vulnerable `golang.org/x/text` module. A `function` field names the matched vulnerable dependency symbol; it does not name an RCC caller. No frame names the RCC module or contains a caller position, and no finding provides a multi-frame path. The evidence therefore confirms singleton vulnerable-dependency symbol/reference presence in each binary, but does not establish RCC entrypoint reachability or exploitability.

Per-binary and per-OSV counts, actual frame-key distributions, raw stream hashes, scan metadata, and integrity checks are in `finding-level-erratum.json`. The original `report.md`, `final-scan-summary.json`, raw streams, and initial resource-cap failure evidence were left untouched; their hashes are recorded in the JSON. No scan was rerun.
