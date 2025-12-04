# Feature Specification: UI and Wizard Enhancements

**Feature Branch**: `002-ui-wizard-enhancements`
**Created**: 2025-12-04
**Status**: Draft
**Input**: User description: "UI Enhancement Specifications - Enhancements to RCC's terminal UI and interactive wizard using currently unused utility functions csif and memberValidation"

## Clarifications

### Session 2025-12-04

- Q: What is explicitly out of scope for this feature? → A: New external dependencies, third-party progress libraries, interactive TUI frameworks
- Q: Which specific RCC commands qualify as destructive operations requiring confirmation? → A: holotree delete, holotree remove, configuration cleanup, cache clear operations
- Q: How should progress operations be logged for observability? → A: Log operation start/complete/error at trace level only; no logging of individual progress updates
- Q: What dashboard features are required? → A: Full dashboard layouts (A-F) with box drawing, scroll regions, cursor control, and rich progress visualization

## Out of Scope

- New external dependencies beyond what already exists in the codebase
- Third-party progress bar or spinner libraries
- Interactive TUI frameworks (e.g., bubbletea, tview)
- Changes that would require new Go module dependencies

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Progress Feedback During Long Operations (Priority: P1)

As an RCC user running environment creation or downloads, I want to see clear progress indicators so that I know operations are proceeding and can estimate completion time.

**Why this priority**: Long-running operations without feedback cause user anxiety and lead to premature cancellation or duplicate runs. This directly impacts user experience for the most common RCC workflows.

**Independent Test**: Can be fully tested by running `rcc holotree vars robot.yaml` and observing spinner/progress bar during environment resolution. Delivers immediate visual feedback value.

**Acceptance Scenarios**:

1. **Given** an interactive terminal session, **When** the user runs an environment creation command, **Then** a spinner animation displays with status text showing current operation phase
2. **Given** an interactive terminal session, **When** a file download begins, **Then** a progress bar displays showing percentage complete and estimated time remaining
3. **Given** a non-interactive environment (CI/piped output), **When** any long operation runs, **Then** progress indicators are suppressed and only completion messages appear
4. **Given** progress is being displayed, **When** an error occurs, **Then** the progress indicator stops cleanly and error message is clearly visible

---

### User Story 2 - Validated Wizard Inputs (Priority: P2)

As an RCC user creating a new robot project via the wizard, I want my inputs validated immediately with clear feedback so that I can correct mistakes before proceeding.

**Why this priority**: Input validation prevents downstream errors and wasted time. Using the existing `memberValidation` function improves code quality while enhancing user experience.

**Independent Test**: Can be fully tested by running `rcc create` and entering invalid values at prompts. Delivers immediate error feedback and re-prompting.

**Acceptance Scenarios**:

1. **Given** the wizard prompts for robot type selection, **When** the user enters an invalid option, **Then** an error message displays in red showing valid choices and the prompt repeats
2. **Given** the wizard prompts for a project name, **When** the user enters an invalid name (special characters), **Then** an error message explains naming rules and the prompt repeats
3. **Given** the wizard prompts for a selection from a list, **When** the user presses Enter without input, **Then** the default value is accepted if one exists

---

### User Story 3 - Confirmation Prompts for Destructive Operations (Priority: P2)

As an RCC user performing operations that modify or delete data, I want to be prompted for confirmation so that I can avoid accidental data loss.

**Why this priority**: Preventing accidental data loss is critical for user trust. Operations like cache clearing or environment deletion should require explicit confirmation.

**Independent Test**: Can be fully tested by running `rcc holotree delete` and observing confirmation prompt. Delivers protection against accidental deletion.

**Acceptance Scenarios**:

1. **Given** an interactive terminal, **When** the user initiates a destructive operation (cache clear, environment delete), **Then** a confirmation prompt appears asking "Are you sure? [y/N]"
2. **Given** a confirmation prompt is displayed, **When** the user enters anything other than "y" or "Y", **Then** the operation is cancelled with a message
3. **Given** a destructive operation command, **When** the user includes a `--yes` or `-y` flag, **Then** the confirmation prompt is bypassed
4. **Given** a non-interactive environment, **When** a destructive operation is attempted without `--yes`, **Then** the operation fails with an error message requiring explicit confirmation flag

---

### User Story 4 - Enhanced Color and Formatting (Priority: P3)

As an RCC user viewing terminal output, I want consistent and meaningful use of colors and text styles so that I can quickly distinguish between information types.

**Why this priority**: Visual hierarchy improves scannability but is an enhancement over core functionality. The existing `csif` function enables dynamic formatting capabilities.

**Independent Test**: Can be fully tested by running any RCC command and observing colored output for success (green), warnings (yellow), errors (red), and emphasis (bold/underline).

**Acceptance Scenarios**:

1. **Given** an interactive terminal with color support, **When** a command completes successfully, **Then** success messages display in green with appropriate styling
2. **Given** an interactive terminal, **When** warnings are generated, **Then** warning text displays in yellow with a warning indicator
3. **Given** the `--no-color` flag is set, **When** any output is generated, **Then** all CSI escape sequences are omitted
4. **Given** output contains section headers or important labels, **When** displayed, **Then** bold or underline formatting distinguishes them from body text

---

### User Story 5 - Rich Dashboard Display for Environment Operations (Priority: P1)

As an RCC user building or restoring environments, I want a rich dashboard display showing operation progress with visual hierarchy so that I can monitor the multi-step process at a glance.

**Why this priority**: Environment building is the primary RCC workflow (15+ steps). Users need to see which step is running, what completed, and what's next without scrolling through text.

**Independent Test**: Run `rcc holotree vars robot.yaml --force` and observe the dashboard with box-drawing characters, step indicators, and progress within a fixed viewport.

**Acceptance Scenarios**:

1. **Given** an interactive terminal with sufficient height (≥20 lines), **When** `holotree vars` starts, **Then** a dashboard displays with bordered sections showing: current step with spinner, completed steps list, and overall progress bar
2. **Given** a dashboard is displayed, **When** a step completes, **Then** the dashboard updates in-place (no scroll) showing the step as complete with checkmark
3. **Given** a dashboard is displayed, **When** an error occurs, **Then** the error appears in the status section in red and the dashboard persists for review
4. **Given** terminal height < 20 lines, **When** an operation starts, **Then** fall back to inline progress display instead of dashboard

---

### User Story 6 - Multi-Task Progress Dashboard (Priority: P2)

As an RCC user running batch operations (multiple downloads, parallel restores), I want to see progress for each task simultaneously so I can monitor overall batch status.

**Why this priority**: Batch operations like catalog pulls or multi-file downloads benefit from showing parallel progress rather than sequential updates.

**Independent Test**: Run a command that downloads multiple files and observe multiple progress bars updating simultaneously within a dashboard frame.

**Acceptance Scenarios**:

1. **Given** an interactive terminal, **When** multiple download operations start in parallel, **Then** a dashboard shows up to 5 progress bars with individual file names and percentages
2. **Given** a multi-task dashboard, **When** one task completes, **Then** its row shows completion status and remaining tasks continue updating
3. **Given** more than 5 parallel tasks, **When** displaying progress, **Then** show "and N more..." summary row below the visible tasks

---

### User Story 7 - Robot Run Dashboard (Priority: P2)

As an RCC user running robot automation, I want a dashboard showing robot execution status, output capture, and result summary so I can monitor automation progress.

**Why this priority**: Robot runs are long-lived operations where users need to see what's happening without output scrolling off screen.

**Independent Test**: Run `rcc run -r robot.yaml` and observe dashboard with robot status, recent output, and summary section.

**Acceptance Scenarios**:

1. **Given** an interactive terminal, **When** robot run starts, **Then** dashboard shows: header with robot name, scrolling output section (last N lines), and status footer
2. **Given** robot dashboard is displayed, **When** robot output is generated, **Then** the output section scrolls while header and footer remain fixed
3. **Given** robot run completes, **When** dashboard closes, **Then** final summary shows task count, pass/fail status, and duration

---

### User Story 8 - Wizard Action Selection (Priority: P2)

As an RCC user encountering errors or decision points, I want to choose from available actions with clear descriptions so I can make informed decisions.

**Why this priority**: Error recovery and decision points benefit from presenting options rather than cryptic prompts.

**Independent Test**: Trigger a recoverable error and observe action selection menu with numbered options and descriptions.

**Acceptance Scenarios**:

1. **Given** an error with recovery options, **When** presented to user, **Then** display numbered list with action names and descriptions
2. **Given** action selection displayed, **When** user enters valid number, **Then** execute corresponding action
3. **Given** action selection displayed, **When** user enters 'q' or presses Ctrl+C, **Then** cancel and exit cleanly

---

### Edge Cases

- What happens when terminal width is very narrow (< 40 characters)? Progress bars should adapt or fall back to simpler indicators.
- How does the system handle output when TERM environment variable is unset? Default to non-color mode.
- What happens if the user interrupts (Ctrl+C) during a progress operation? Clean up cursor position and restore terminal state.
- How should validation handle empty input when no default exists? Display error and re-prompt.
- What happens when stdin is a pipe but stdout is a TTY? Use non-interactive input mode but allow visual output.

**Pipe Input + TTY Output Scenario**:
- **Given** stdin is a pipe (e.g., `echo "input" | rcc create`) but stdout is a TTY
- **When** a command requiring user input is executed
- **Then** the command fails gracefully with message "Interactive input required" rather than hanging
- **And** visual output (colors, progress) is still displayed since stdout is a TTY

## Requirements *(mandatory)*

### Functional Requirements

#### Core Progress & Validation (Phase 1 - COMPLETE)

- **FR-001**: System MUST display animated progress indicators (spinner) during operations lasting more than 500ms when running in an interactive terminal ✓
- **FR-002**: System MUST display progress bars with percentage for operations with known duration/size (e.g., file downloads) ✓
- **FR-003**: System MUST suppress all progress animations when output is not connected to an interactive terminal ✓
- **FR-004**: System MUST validate wizard inputs against allowed values using the existing `memberValidation` pattern ✓
- **FR-005**: System MUST display validation error messages in red with clear explanation of valid options ✓
- **FR-006**: System MUST re-prompt after validation failure without losing previous context ✓
- **FR-007**: System MUST prompt for confirmation before destructive operations: `holotree delete`, `holotree remove`, `configuration cleanup` ✓
- **FR-008**: System MUST accept `--yes` or `-y` flag to bypass confirmation prompts ✓
- **FR-009**: System MUST fail destructive operations in non-interactive mode without explicit `--yes` flag ✓
- **FR-010**: System MUST use consistent color coding: green for success, yellow for warnings, red for errors ✓
- **FR-011**: System MUST respect `--no-color` flag and `NO_COLOR` environment variable ✓
- **FR-012**: System MUST clean up terminal state (cursor position, styling) on operation completion or interruption ✓
- **FR-013**: System MUST utilize the existing `csif` function for dynamic ANSI escape sequence generation ✓
- **FR-014**: System MUST utilize the existing `memberValidation` function for list-based input validation ✓
- **FR-015**: System MUST log progress operation start/complete/error events at trace level only (no individual progress update logging) ✓

#### Dashboard Layouts (Phase 2 - NEW)

- **FR-016**: System MUST provide a dashboard framework supporting fixed regions (header, footer, scrollable body)
- **FR-017**: System MUST implement Dashboard Layout A (Environment Build) with 15-step progress tracking using box-drawing characters
- **FR-018**: System MUST implement Dashboard Layout B (Diagnostics) with checklist-style status display
- **FR-019**: System MUST implement Dashboard Layout C (Download Progress) with enhanced single-file download visualization
- **FR-020**: System MUST implement Dashboard Layout D (Multi-Task Progress) showing up to 5 parallel operations
- **FR-021**: System MUST implement Dashboard Layout E (Compact Inline) for minimal terminal height situations
- **FR-022**: System MUST implement Dashboard Layout F (Robot Run) with header, scrolling output, and status footer
- **FR-023**: System MUST detect terminal dimensions and select appropriate layout (dashboard vs inline fallback)
- **FR-024**: System MUST support scroll regions (DECSTBM) for fixed header/footer with scrolling body

#### Cursor Control (Phase 2 - NEW)

- **FR-025**: System MUST provide SaveCursor() function using CSI s or ESC 7
- **FR-026**: System MUST provide RestoreCursor() function using CSI u or ESC 8
- **FR-027**: System MUST provide MoveTo(row, col) function using CSI {row};{col}H
- **FR-028**: System MUST provide MoveUp(n), MoveDown(n), MoveLeft(n), MoveRight(n) cursor movement functions
- **FR-029**: System MUST provide SetScrollRegion(top, bottom) function using CSI {top};{bottom}r
- **FR-030**: System MUST provide ClearScrollRegion() to restore full-screen scrolling
- **FR-031**: System MUST provide ClearLine() and ClearToEnd() functions for line manipulation

#### Advanced Color Support (Phase 2 - NEW)

- **FR-032**: System MUST provide SeverityColor(level) mapping: trace→dim gray, debug→gray, info→white, warning→yellow, error→red, critical→bright red/bold
- **FR-033**: System MUST provide StatusColor(status) mapping: pending→gray, running→cyan, complete→green, failed→red, skipped→dim
- **FR-034**: System MUST provide RGB(r, g, b) function for TrueColor output (CSI 38;2;r;g;b m)
- **FR-035**: System MUST provide BGRGB(r, g, b) function for TrueColor backgrounds (CSI 48;2;r;g;b m)
- **FR-036**: System MUST provide Color256(n) and BGColor256(n) functions for 256-color palette support
- **FR-037**: System MUST detect color capability (NO_COLOR, COLORTERM=truecolor, TERM) and select appropriate color mode

#### Box Drawing (Phase 2 - NEW)

- **FR-038**: System MUST provide box-drawing character sets (Unicode and ASCII fallback)
- **FR-039**: System MUST provide DrawBox(x, y, width, height, style) function for bordered regions
- **FR-040**: System MUST provide horizontal and vertical line drawing functions
- **FR-041**: System MUST support box styles: single line (─│┌┐└┘), double line (═║╔╗╚╝), rounded (─│╭╮╰╯)

#### Wizard Enhancements (Phase 2 - NEW)

- **FR-042**: System MUST provide ChooseAction(prompt, actions) function returning selected action struct
- **FR-043**: System MUST provide AskRecovery(error, options) function for error recovery dialogs
- **FR-044**: System MUST provide ConfirmDangerous(prompt, force) with typed confirmation for critical operations
- **FR-045**: System MUST support keyboard navigation (arrow keys) for selection menus where terminal supports it
- **FR-046**: System MUST provide chooseByName() for template selection with preview display

### Key Entities

- **Progress Indicator**: Represents an ongoing operation's visual feedback state (type: spinner/bar, current progress, status message)
- **Validation Rule**: Defines acceptable input patterns and associated error messages (allowed values, regex pattern, error text)
- **Confirmation Prompt**: Represents a user decision point with accept/reject options and bypass capability
- **Dashboard**: Fixed viewport display with regions (header, body, footer, sidebar) and scroll capabilities
- **DashboardStep**: Individual step in a multi-step operation with status (pending, running, complete, failed, skipped)
- **BoxStyle**: Configuration for box-drawing characters (single, double, rounded) with Unicode/ASCII modes
- **ColorMode**: Active color capability (none, basic 16-color, 256-color, TrueColor)
- **Action**: Selectable option with name, description, and associated handler function

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: Users can identify operation progress status within 1 second of operation start in interactive mode ✓
- **SC-002**: 100% of wizard validation errors display actionable feedback before re-prompting ✓
- **SC-003**: Zero destructive operations execute without explicit user consent (prompt or flag) ✓
- **SC-004**: Terminal output is visually clean with no escape sequence artifacts in non-interactive environments ✓
- **SC-005**: Color output is disabled within 0 additional user actions when `--no-color` or `NO_COLOR=1` is specified ✓
- **SC-006**: Progress indicators do not interfere with piped output or script automation ✓
- **SC-007**: Dashboard displays render within 50ms update cycle for smooth animation
- **SC-008**: Environment build dashboard shows all 15 steps in a single viewport without scrolling
- **SC-009**: Box-drawing characters display correctly on terminals supporting Unicode (ASCII fallback for others)
- **SC-010**: Multi-task progress dashboard shows real-time updates for up to 5 parallel operations

## Assumptions

- The existing `pretty.Interactive` boolean correctly identifies interactive terminal sessions
- The `go-isatty` dependency reliably detects terminal capabilities across supported platforms
- Users have terminals supporting basic ANSI escape sequences when color output is expected
- Default terminal width of 80 characters is assumed when width detection fails
- The `--yes` flag pattern is consistent with Unix CLI conventions users expect
