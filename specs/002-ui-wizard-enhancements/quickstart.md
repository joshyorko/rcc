# Quickstart: UI and Wizard Enhancements

**Feature**: 002-ui-wizard-enhancements
**Date**: 2025-12-04

## Overview

This guide helps developers quickly understand and implement the UI and wizard enhancements for RCC.

## Key Files to Modify

| File | Purpose | Priority |
|------|---------|----------|
| `pretty/progress.go` | NEW: Progress indicator implementation | P1 |
| `wizard/confirm.go` | NEW: Confirmation prompt implementation | P2 |
| `wizard/common.go` | Enhance with new validation helpers | P2 |
| `cmd/holotree.go` | Add --yes flag and confirmations | P2 |
| `cmd/configuration.go` | Add --yes flag and confirmations | P2 |

## Implementation Order

### Step 1: Progress Indicators (P1)

1. Create `pretty/progress.go` with:
   - `Spinner` struct implementing `ProgressIndicator` interface
   - `ProgressBar` struct implementing `ProgressIndicator` interface
   - Signal handler for Ctrl+C cleanup

2. Key patterns to use:
   ```go
   // Use csif for ANSI sequences
   hideCursor := csif("?25l")
   showCursor := csif("?25h")
   clearLine := csif("0K")

   // Check interactivity before animating
   if !Interactive {
       return // No-op in non-interactive mode
   }

   // Trace logging
   common.Trace("Progress started: %s", message)
   ```

### Step 2: Confirmation Prompts (P2)

1. Create `wizard/confirm.go` with:
   - `Confirm(question string, force bool) (bool, error)`
   - `AddYesFlag(cmd *cobra.Command, target *bool)`

2. Key patterns to use:
   ```go
   // Reuse existing ask() and memberValidation()
   validator := memberValidation([]string{"y", "Y", "n", "N"},
       "Please enter y or n")
   response, err := ask(question+" [y/N]", "n", validator)
   ```

### Step 3: Command Integration (P2)

1. Add `--yes` flag to destructive commands:
   ```go
   var yesFlag bool

   func init() {
       deleteCmd.Flags().BoolVarP(&yesFlag, "yes", "y", false,
           "Skip confirmation prompt")
   }
   ```

2. Add confirmation before destructive operations:
   ```go
   confirmed, err := wizard.Confirm("Delete all environments?", yesFlag)
   if err != nil {
       return err
   }
   if !confirmed {
       common.Stdout("Operation cancelled.\n")
       return nil
   }
   ```

## Testing

### Unit Tests

```bash
# Run all unit tests
GOARCH=amd64 go test ./pretty/... ./wizard/...

# Run specific package
GOARCH=amd64 go test -v ./pretty/
```

### Manual Testing

```bash
# Test spinner (should animate in terminal)
./build/rcc holotree vars robot.yaml

# Test confirmation prompt
./build/rcc holotree delete --help  # Should show --yes flag
./build/rcc holotree delete         # Should prompt for confirmation

# Test non-interactive mode
echo "n" | ./build/rcc holotree delete  # Should fail without --yes

# Test --yes bypass
./build/rcc holotree delete --yes   # Should proceed without prompt
```

### Robot Framework Tests

```bash
# Run acceptance tests
python3 -m robot -L DEBUG -d tmp/output robot_tests/
```

## Common Pitfalls

1. **Forgetting to check `Interactive`**: Always check `pretty.Interactive` before displaying animations or prompts.

2. **Not cleaning up terminal state**: Always restore cursor visibility on Stop() or signal handling.

3. **Blocking on non-interactive input**: Return error immediately if `!Interactive && !force`.

4. **Hardcoding terminal width**: Use `term.GetSize()` with 80-char fallback.

## Dependencies

All required dependencies already exist:
- `go-isatty`: Terminal detection
- `golang.org/x/term`: Terminal size
- `os/signal`: Signal handling
- `syscall`: Signal constants

No new dependencies should be added (per out-of-scope constraint).

## Reference Files

- `pretty/internal.go`: `csif()` and `csi()` functions
- `pretty/variables.go`: Color constants and `Interactive` flag
- `wizard/common.go`: `ask()`, `memberValidation()`, `Validator` type
- `.specify/memory/constitution.md`: Project principles to follow
