# Copilot Instructions

See [AGENTS.md](../AGENTS.md) for complete development guidance, architecture, and conventions.

## Quick Reference

```bash
inv assets                     # Generate embedded assets (required before build)
inv local                      # Build for current platform
GOARCH=amd64 go test ./...     # Run unit tests
inv robot                      # Run acceptance tests → tmp/output/log.html
```

## RCC-Specific Patterns

```go
// Error handling — use fail package, not if err != nil
defer fail.Around(&err)
fail.On(condition, "message: %v", err)
fail.Fast(err)

// Logging — use common package, not fmt.Print
common.Log("msg")     // normal
common.Debug("msg")   // --debug
common.Trace("msg")   // --trace

// Testing — use hamlet package
must_be, wont_be := hamlet.Specifications(t)
must_be.Equal("expected", actual)
```

## Key Constraints

1. Never edit `blobs/` or `build/`—regenerate with `inv assets`
2. Never hardcode endpoints—use `RCC_ENDPOINT_*` environment variables
3. No telemetry—this fork has tracking disabled
4. Always set `GOARCH=amd64` for tests
