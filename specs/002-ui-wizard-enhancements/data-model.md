# Data Model: UI and Wizard Enhancements

**Date**: 2025-12-04
**Feature**: 002-ui-wizard-enhancements

## Overview

This feature introduces no persistent data storage. All entities are runtime structures for managing terminal UI state. This document defines the Go types and their relationships.

## Entities

### ProgressIndicator

Represents an ongoing operation's visual feedback state.

**Location**: `pretty/progress.go`

```go
type ProgressType int

const (
    SpinnerType ProgressType = iota
    BarType
)

type ProgressIndicator struct {
    Type        ProgressType  // Spinner or progress bar
    Message     string        // Status text to display
    Total       int64         // Total units (for bar type, 0 for spinner)
    Current     int64         // Current progress (for bar type)
    StartTime   time.Time     // When operation started
    frames      []string      // Animation frames (for spinner)
    frameIndex  int           // Current frame
    stopChan    chan struct{} // Signal to stop animation
    doneChan    chan struct{} // Confirmation that animation stopped
    width       int           // Terminal width for bar sizing
}
```

**State Transitions**:
```
[Created] --Start()--> [Running] --Stop(success)--> [Completed]
                            |
                            +--Stop(error)------> [Failed]
                            |
                            +--<Ctrl+C>--------> [Interrupted]
```

**Validation Rules**:
- `Message` must not be empty
- `Total` must be >= 0 (0 indicates indeterminate/spinner)
- `Current` must be <= `Total` when `Total` > 0

---

### ValidationRule

Defines acceptable input patterns and associated error messages.

**Location**: `wizard/common.go` (extends existing patterns)

```go
type Validator func(string) bool

type ValidationRule struct {
    AllowedValues []string        // For memberValidation
    Pattern       *regexp.Regexp  // For regexpValidation
    ErrorMessage  string          // Displayed on validation failure
}
```

**Factory Functions** (existing):
- `memberValidation(members []string, erratic string) Validator`
- `regexpValidation(validator *regexp.Regexp, erratic string) Validator`

**Validation Rules**:
- At least one of `AllowedValues` or `Pattern` must be set
- `ErrorMessage` must not be empty

---

### ConfirmationPrompt

Represents a user decision point with accept/reject options.

**Location**: `wizard/confirm.go`

```go
type ConfirmationPrompt struct {
    Question     string  // The question to display
    Default      bool    // Default answer (false = No)
    ForceFlag    bool    // If true, bypass prompt (from --yes flag)
    Interactive  bool    // Cached from pretty.Interactive
}

type ConfirmationResult struct {
    Confirmed bool   // User's decision
    Bypassed  bool   // Was prompt bypassed via --yes
    Error     error  // Any error during prompt
}
```

**State Transitions**:
```
[Created] --Show()--> [WaitingInput] --User enters y--> [Confirmed]
               |                      |
               |                      +--User enters n--> [Rejected]
               |                      |
               |                      +--<EOF/Error>----> [Error]
               |
               +--ForceFlag=true--> [Bypassed/Confirmed]
               |
               +--!Interactive----> [Error: requires --yes]
```

**Validation Rules**:
- `Question` must not be empty
- In non-interactive mode, `ForceFlag` must be true or error is returned

---

## Relationships

```
┌─────────────────────────────────────────────────────────────┐
│                        CLI Command                          │
│  (e.g., holotree delete, holotree vars)                    │
└─────────────────────────────────────────────────────────────┘
           │                              │
           │ uses                         │ uses
           ▼                              ▼
┌─────────────────────┐       ┌─────────────────────────────┐
│ ConfirmationPrompt  │       │    ProgressIndicator        │
│                     │       │                             │
│ - Checks --yes flag │       │ - Checks Interactive        │
│ - Validates y/n     │       │ - Uses csif for animation   │
└─────────────────────┘       └─────────────────────────────┘
           │                              │
           │ uses                         │ uses
           ▼                              ▼
┌─────────────────────┐       ┌─────────────────────────────┐
│  ValidationRule     │       │      pretty package         │
│  (memberValidation) │       │  (csif, Interactive, colors)│
└─────────────────────┘       └─────────────────────────────┘
```

## Package Dependencies

```
cmd/
  └── imports wizard/     (for Confirm)
  └── imports pretty/     (for ProgressIndicator)

wizard/
  └── imports pretty/     (for colors, Interactive)
  └── imports common/     (for Stdout, Trace)

pretty/
  └── imports common/     (for Trace logging)
  └── imports go-isatty   (for terminal detection)
  └── imports x/term      (for terminal width)
```

## No Database/Storage Impact

This feature does not introduce any:
- File-based persistence
- Configuration storage changes
- Cache structures
- Network data transmission

All entities are ephemeral runtime structures destroyed when the command completes.
