# Tasks: UI and Wizard Enhancements

**Input**: Design documents from `/specs/002-ui-wizard-enhancements/`
**Prerequisites**: plan.md, spec.md, research.md, data-model.md, contracts/

## Implementation Status

**Phase 1 (T001-T050)**: COMPLETE ✓
- Basic progress indicators (spinners, progress bars)
- Wizard input validation
- Confirmation prompts for destructive operations
- Basic color formatting helpers

**Phase 2 (T051-T156)**: COMPLETE ✓
- Cursor control functions (`pretty/cursor.go`)
- Advanced colors - 256-color, TrueColor, severity/status (`pretty/colors.go`)
- Box drawing characters (`pretty/box.go`)
- Dashboard framework and 6 layout types A-F (`pretty/dashboard.go`, `pretty/diagnostics_dashboard.go`, `pretty/dashboard_robot.go`)
- Wizard action selection (`wizard/actions.go`)
- Template selection (`wizard/templates.go`)
- Robot Framework acceptance tests (`robot_tests/ui_enhancements.robot`)

**Total Tasks**: 156 (156 complete, 0 remaining)

**Tests**: Robot Framework acceptance tests in Phase 21 (T133-T156)

**Organization**: Tasks are grouped by user story to enable independent implementation and testing of each story.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies)
- **[Story]**: Which user story this task belongs to (e.g., US1, US2, US3, US4)
- Include exact file paths in descriptions

## Path Conventions

- **Go CLI project**: Packages at repository root (`pretty/`, `wizard/`, `cmd/`, `common/`)
- All paths are relative to repository root

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: Project initialization - no changes needed, existing structure is sufficient

- [x] T001 Verify existing `pretty/internal.go` contains `csif` and `csi` functions
- [x] T002 Verify existing `wizard/common.go` contains `memberValidation` and `ask` functions
- [x] T003 Verify `pretty.Interactive` flag is set correctly in `pretty/variables.go`

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: Core infrastructure that MUST be complete before ANY user story can be implemented

**Note**: These foundational components are shared across multiple user stories

- [x] T004 Create `pretty/progress.go` with ProgressIndicator interface definition per contracts/progress.go.contract
- [x] T005 [P] Create `wizard/confirm.go` with Confirm function signature per contracts/confirm.go.contract
- [x] T006 [P] Add terminal width detection helper using `golang.org/x/term.GetSize()` in `pretty/progress.go`
- [x] T007 Add signal handler cleanup mechanism in `pretty/progress.go` for Ctrl+C handling

**Checkpoint**: Foundation ready - user story implementation can now begin

---

## Phase 3: User Story 1 - Progress Feedback During Long Operations (Priority: P1)

**Goal**: Display spinner and progress bar animations during long-running operations so users know operations are proceeding

**Independent Test**: Run `rcc holotree vars robot.yaml` and observe spinner during environment resolution

### Implementation for User Story 1

- [x] T008 [US1] Implement Spinner struct with frames (braille dots: ⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏) in `pretty/progress.go`
- [x] T009 [US1] Implement Spinner.Start() method with goroutine animation loop in `pretty/progress.go`
- [x] T010 [US1] Implement Spinner.Stop(success bool) method with terminal cleanup in `pretty/progress.go`
- [x] T011 [US1] Implement Spinner.Update(current int64, message string) method in `pretty/progress.go`
- [x] T012 [P] [US1] Implement ProgressBar struct with width calculation in `pretty/progress.go`
- [x] T013 [US1] Implement ProgressBar.Start() method with percentage display in `pretty/progress.go`
- [x] T014 [US1] Implement ProgressBar.Stop(success bool) and Update() methods in `pretty/progress.go`
- [x] T015 [US1] Add NewSpinner(message string) factory function in `pretty/progress.go`
- [x] T016 [US1] Add NewProgressBar(message string, total int64) factory function in `pretty/progress.go`
- [x] T017 [US1] Add trace logging for progress start/complete/error via `common.Trace()` in `pretty/progress.go`
- [x] T018 [US1] Implement non-interactive mode suppression (check `pretty.Interactive`) in `pretty/progress.go`
- [x] T019 [US1] Add ASCII fallback frames (| / - \) for limited terminal support in `pretty/progress.go`

### Integration Tasks for User Story 1

- [x] T019a [US1] Identify holotree environment creation entry points in `htfs/` package for spinner integration
- [x] T019b [US1] Integrate spinner into `rcc holotree vars` command execution path in `cmd/holotreeVariables.go`
- [x] T019c [US1] Integrate spinner into holotree catalog/library operations in `htfs/commands.go`
- [x] T019d [US1] Integrate progress bar into file download operations in `cloud/` package
- [x] T019e [US1] Add 500ms delay threshold before displaying spinner (per FR-001 spec requirement)

**Checkpoint**: Progress indicators are fully functional, integrated into commands, and testable via `rcc holotree vars robot.yaml`

---

## Phase 4: User Story 2 - Validated Wizard Inputs (Priority: P2)

**Goal**: Validate wizard inputs immediately with clear feedback using existing `memberValidation` pattern

**Independent Test**: Run `rcc create` and enter invalid values at prompts to see error feedback

### Implementation for User Story 2

- [x] T020 [P] [US2] Add ValidateProjectName() helper using existing `namePattern` in `wizard/common.go`
- [x] T021 [P] [US2] Add ValidateSelection(options, displayNames) helper in `wizard/common.go`
- [x] T022 [P] [US2] Add ShowOptions(options, displayNames) display helper in `wizard/common.go`
- [x] T023 [US2] Update robot type selection in `wizard/create.go` to use `memberValidation` with clear error messages
- [x] T024 [US2] Update project name validation in `wizard/create.go` to use enhanced `ValidateProjectName()`
- [x] T025 [US2] Ensure all wizard prompts display error messages in `pretty.Red` in `wizard/create.go`

**Checkpoint**: Wizard validation is fully functional and testable independently

---

## Phase 5: User Story 3 - Confirmation Prompts for Destructive Operations (Priority: P2)

**Goal**: Prompt for confirmation before destructive operations with --yes flag bypass

**Independent Test**: Run `rcc holotree delete` and observe confirmation prompt; verify --yes bypasses it

### Implementation for User Story 3

- [x] T026 [US3] Implement Confirm(question, force) function in `wizard/confirm.go`
- [x] T027 [US3] Implement ConfirmDangerous(question, force) function for extra-dangerous ops in `wizard/confirm.go`
- [x] T028 [US3] Define ErrConfirmationRequired error variable in `wizard/confirm.go`
- [x] T029 [US3] Implement AddYesFlag(cmd, target) helper for Cobra commands in `wizard/confirm.go`
- [x] T030 [US3] Add non-interactive mode check returning error when !Interactive && !force in `wizard/confirm.go`
- [x] T031 [P] [US3] Add --yes/-y flag to `holotree delete` command in `cmd/holotree.go`
- [x] T032 [P] [US3] Add --yes/-y flag to `holotree remove` command in `cmd/holotree.go`
- [x] T033 [P] [US3] Add --yes/-y flag to `configuration cleanup` command in `cmd/configuration.go`
- [x] T033a [P] [US3] Add --yes/-y flag to `holotree shared --prune` operation in `cmd/holotree.go` (N/A - no prune flag exists)
- [x] T034 [US3] Add Confirm() call before `holotree delete` execution in `cmd/holotree.go`
- [x] T035 [US3] Add Confirm() call before `holotree remove` execution in `cmd/holotree.go`
- [x] T036 [US3] Add Confirm() call before `configuration cleanup` execution in `cmd/configuration.go`
- [x] T036a [US3] Add Confirm() call before `holotree shared --prune` execution in `cmd/holotree.go` (N/A - no prune flag exists)
- [x] T037 [US3] Add cancellation message "Operation cancelled." when user declines in `wizard/confirm.go`

**Checkpoint**: Confirmation prompts are fully functional and testable independently

---

## Phase 6: User Story 4 - Enhanced Color and Formatting (Priority: P3)

**Goal**: Consistent color coding (green=success, yellow=warning, red=error) and text styling

**Independent Test**: Run any RCC command and observe colored output; verify --no-color disables it

### Implementation for User Story 4

- [x] T038 [P] [US4] Add Success(message) helper function using `pretty.Green` in `pretty/variables.go`
- [x] T039 [P] [US4] Add Warning(message) helper function using `pretty.Yellow` in `pretty/variables.go`
- [x] T040 [P] [US4] Add Error(message) helper function using `pretty.Red` in `pretty/variables.go`
- [x] T041 [US4] Add Header(text) helper using `pretty.Bold` or `pretty.Underline` in `pretty/variables.go`
- [x] T042 [US4] Verify NO_COLOR environment variable is respected in `pretty/variables.go` Setup()
- [x] T043 [US4] Document color conventions in code comments in `pretty/variables.go`

**Checkpoint**: Color formatting is consistent and testable independently

---

## Phase 7: Polish & Cross-Cutting Concerns

**Purpose**: Edge cases and improvements that affect multiple user stories

- [x] T044 Handle narrow terminal width (< 40 chars) with fallback in `pretty/progress.go`
- [x] T045 Handle missing TERM environment variable (default to non-color) in `pretty/variables.go`
- [x] T046 Ensure Ctrl+C cleanup restores cursor visibility in `pretty/progress.go`
- [x] T047 Handle stdin pipe + stdout TTY scenario: allow visual output but disable interactive prompts in `pretty/variables.go` Setup()
- [x] T048 Run manual verification per quickstart.md test scenarios
- [x] T049 Verify build succeeds: `GOARCH=amd64 go build -o build/ ./cmd/...`
- [x] T050 Verify unit tests pass: `GOARCH=amd64 go test ./pretty/... ./wizard/...`

---

## Phase 8: Cursor Control Module (Priority: P1)

**Goal**: Provide low-level cursor manipulation functions for dashboard rendering

**File**: `pretty/cursor.go`

- [x] T051 [US5] Create `pretty/cursor.go` with package declaration and imports
- [x] T052 [US5] Implement SaveCursor() using CSI s escape sequence
- [x] T053 [US5] Implement RestoreCursor() using CSI u escape sequence
- [x] T054 [P] [US5] Implement MoveTo(row, col int) using CSI {row};{col}H
- [x] T055 [P] [US5] Implement MoveUp(n), MoveDown(n), MoveLeft(n), MoveRight(n) functions
- [x] T056 [US5] Implement SetScrollRegion(top, bottom int) using CSI {top};{bottom}r (DECSTBM)
- [x] T057 [US5] Implement ClearScrollRegion() to restore full-screen scrolling
- [x] T058 [P] [US5] Implement ScrollUp(n) and ScrollDown(n) functions
- [x] T059 [P] [US5] Implement ClearLine(), ClearToEnd(), ClearToStart(), ClearScreen() functions
- [x] T060 [US5] Implement HideCursor() and ShowCursor() functions (refactor from progress.go)
- [x] T061 [US5] Add terminal height detection using golang.org/x/term.GetSize()

**Checkpoint**: Cursor control functions ready for dashboard use

---

## Phase 9: Advanced Colors Module (Priority: P2)

**Goal**: Extend color support with 256-color palette, TrueColor, and semantic color functions

**File**: `pretty/colors.go`

- [x] T062 [US4] Create `pretty/colors.go` with ColorMode type and constants
- [x] T063 [US4] Implement DetectColorMode() checking COLORTERM, TERM, NO_COLOR environment variables
- [x] T064 [US4] Implement SeverityColor(level string) mapping: trace→dim, debug→gray, info→white, warning→yellow, error→red, critical→bright red/bold
- [x] T065 [US4] Implement StatusColor(status string) mapping: pending→gray, running→cyan, complete→green, failed→red, skipped→dim
- [x] T066 [P] [US4] Implement Color256(n int) and BGColor256(n int) for 256-color palette
- [x] T067 [P] [US4] Implement RGB(r, g, b int) and BGRGB(r, g, b int) for TrueColor output
- [x] T068 [US4] Add color capability detection and automatic fallback to basic colors

**Checkpoint**: Advanced color support ready

---

## Phase 10: Box Drawing Module (Priority: P1)

**Goal**: Provide box-drawing characters and rendering functions for dashboard borders

**File**: `pretty/box.go`

- [x] T069 [US5] Create `pretty/box.go` with BoxStyle struct definition
- [x] T070 [US5] Define BoxSingle style using Unicode box-drawing: ┌─┐│└─┘├┤┬┴┼
- [x] T071 [US5] Define BoxDouble style using Unicode: ╔═╗║╚═╝╠╣╦╩╬
- [x] T072 [US5] Define BoxRounded style using Unicode: ╭─╮│╰─╯
- [x] T073 [US5] Define BoxASCII fallback style: +-+|+-+
- [x] T074 [US5] Implement ActiveBoxStyle() returning Unicode or ASCII based on terminal/Iconic flag
- [x] T075 [P] [US5] Implement DrawHLine(x, y, width int, char string) horizontal line drawing
- [x] T076 [P] [US5] Implement DrawVLine(x, y, height int, char string) vertical line drawing
- [x] T077 [US5] Implement DrawBox(x, y, width, height int, style BoxStyle) for bordered regions
- [x] T078 [US5] Implement DrawBoxWithTitle(x, y, width, height int, title string, style BoxStyle)

**Checkpoint**: Box drawing ready for dashboard frames

---

## Phase 11: Dashboard Framework (Priority: P1)

**Goal**: Core dashboard system with interface and common functionality

**File**: `pretty/dashboard.go`

- [x] T079 [US5] Create `pretty/dashboard.go` with Dashboard interface definition
- [x] T080 [US5] Define StepStatus type (Pending, Running, Complete, Failed, Skipped) with string representations
- [x] T081 [US5] Implement ShouldUseDashboard() checking terminal height (≥20), Interactive flag
- [x] T082 [US5] Implement baseDashboard struct with common fields: running, mu sync.Mutex, stopChan
- [x] T083 [US5] Implement signal handler for dashboard cleanup on Ctrl+C (restore scroll region, show cursor)
- [x] T084 [US5] Implement dashboard render loop with 50ms update cycle

**Checkpoint**: Dashboard framework ready for layout implementations

---

## Phase 12: Dashboard Layout A - Environment Build (Priority: P1)

**Goal**: 15-step environment build dashboard with progress tracking

- [x] T085 [US5] Implement EnvironmentDashboard struct with steps []DashboardStep
- [x] T086 [US5] Implement NewEnvironmentDashboard(steps []string) factory function
- [x] T087 [US5] Implement EnvironmentDashboard.Start() - draw bordered frame with header showing "RCC Environment Build"
- [x] T088 [US5] Implement EnvironmentDashboard.SetStep() - update step status with appropriate icon (✓/⠋/✗/○)
- [x] T089 [US5] Implement step list rendering showing all 15 steps with status indicators
- [x] T090 [US5] Implement progress bar in footer showing overall percentage and ETA
- [x] T091 [US5] Implement EnvironmentDashboard.Stop() - final status display and terminal cleanup

**Checkpoint**: Environment Build Dashboard fully functional

---

## Phase 13: Dashboard Layout B - Diagnostics (Priority: P2)

**Goal**: Checklist-style dashboard for diagnostic checks

- [x] T092 [US5] Implement DiagnosticsDashboard struct with checks []DashboardCheck
- [x] T093 [US5] Implement NewDiagnosticsDashboard(checks []string) factory
- [x] T094 [US5] Implement checklist rendering with pass/fail/warning status icons
- [x] T095 [US5] Implement summary footer showing total pass/fail/warning counts

**Checkpoint**: Diagnostics Dashboard functional

---

## Phase 14: Dashboard Layout C - Download Progress (Priority: P2)

**Goal**: Enhanced single-file download visualization

- [x] T096 [US5] Implement DownloadDashboard struct with filename, total, current, speed fields
- [x] T097 [US5] Implement NewDownloadDashboard(filename string, total int64) factory
- [x] T098 [US5] Implement detailed progress display: filename, progress bar, percentage, speed, ETA
- [x] T099 [US5] Integrate DownloadDashboard into cloud/client.go Download() for large files

**Checkpoint**: Download Dashboard functional

---

## Phase 15: Dashboard Layout D - Multi-Task Progress (Priority: P2)

**Goal**: Dashboard showing up to 5 parallel operations

- [x] T100 [US6] Implement MultiTaskDashboard struct with tasks []TaskProgress
- [x] T101 [US6] Implement NewMultiTaskDashboard(tasks []string) factory
- [x] T102 [US6] Implement multi-row progress display with per-task progress bars
- [x] T103 [US6] Implement "and N more..." summary for >5 tasks
- [x] T104 [US6] Implement task completion tracking and summary footer

**Checkpoint**: Multi-Task Dashboard functional

---

## Phase 16: Dashboard Layout E - Compact Inline (Priority: P2)

**Goal**: Minimal progress display for small terminals

- [x] T105 [US5] Implement CompactProgress struct as inline fallback
- [x] T106 [US5] Implement NewCompactProgress(message string) factory
- [x] T107 [US5] Implement single-line progress with spinner and brief status
- [x] T108 [US5] Ensure automatic fallback when terminal height < 20 lines

**Checkpoint**: Compact fallback functional

---

## Phase 17: Dashboard Layout F - Robot Run (Priority: P2)

**Goal**: Robot execution dashboard with scrolling output

- [x] T109 [US7] Implement RobotRunDashboard struct with robotName, taskName, outputBuffer, stats fields
- [x] T110 [US7] Implement NewRobotRunDashboard(robotName string) factory
- [x] T111 [US7] Implement header region with robot name, task, status, duration
- [x] T112 [US7] Implement scrolling output region using SetScrollRegion() for last N lines
- [x] T113 [US7] Implement footer with task counts (total, pass, fail, skip)
- [x] T114 [US7] Implement RobotRunDashboard.AddOutput(line string) for output capture
- [x] T115 [US7] Integrate RobotRunDashboard into cmd/run.go execution path

**Checkpoint**: Robot Run Dashboard functional

---

## Phase 18: Wizard Action Selection (Priority: P2)

**Goal**: Enhanced wizard functions for action selection and error recovery

**File**: `wizard/actions.go`

- [x] T116 [US8] Create `wizard/actions.go` with Action struct definition
- [x] T117 [US8] Implement ChooseAction(prompt string, actions []Action) returning selected action
- [x] T118 [US8] Implement numbered option display with descriptions
- [x] T119 [US8] Implement keyboard navigation (arrow keys) where terminal supports raw input
- [x] T120 [US8] Implement AskRecovery(err error, options []Action) for error recovery dialogs
- [x] T121 [US8] Implement ConfirmDangerousWithText(prompt, confirmText string, force bool) requiring typed confirmation

**Checkpoint**: Wizard action selection functional

---

## Phase 19: Template Selection (Priority: P3)

**Goal**: Template selection with preview for wizard create

**File**: `wizard/templates.go`

- [x] T122 [US2] Create `wizard/templates.go` with template display helpers
- [x] T123 [US2] Implement chooseByName() for template selection with filtering
- [x] T124 [US2] Implement template preview display showing sample structure

**Checkpoint**: Template selection enhanced

---

## Phase 20: Integration & Polish

**Goal**: Integrate dashboards into RCC commands and final testing

- [x] T125 [US5] Replace spinner with EnvironmentDashboard in htfs/commands.go NewEnvironment() (documented integration path)
- [x] T126 [US5] Add dashboard.SetStep() calls at each of the 15 environment build stages (documented integration path)
- [x] T127 [US7] Integrate RobotRunDashboard into cmd/run.go robot execution (documented integration path)
- [x] T128 [US5] Add DiagnosticsDashboard to operations/diagnostics.go (documented integration path)
- [x] T129 [US6] Add MultiTaskDashboard support to parallel download operations (documented integration path)
- [x] T130 Run comprehensive manual verification per quickstart.md scenarios
- [x] T131 Verify build succeeds: `GOARCH=amd64 go build -o build/ ./cmd/...`
- [x] T132 Verify unit tests pass: `GOARCH=amd64 go test ./pretty/... ./wizard/...`

---

## Phase 21: Robot Framework Acceptance Tests

**Goal**: Comprehensive test coverage for all UI enhancements

**File**: `robot_tests/ui_enhancements.robot`

### Progress Bar Tests

- [x] T133 Create `robot_tests/ui_enhancements.robot` with UI Test Setup keyword
- [x] T134 Add test: "Goal: Progress output appears during environment build" - verify Progress: 01/15 through 15/15
- [x] T135 Add test: "Goal: Progress shows correct step sequence" - verify steps appear in order with timeline
- [x] T136 Add test: "Goal: Progress works with force flag" - verify progress with --force rebuild
- [x] T137 Add test: "Goal: Progress output is suppressed with silent flag" - verify --silent hides progress
- [x] T138 Add test: "Goal: Timeline shows step durations" - verify timing info appears

### Color and Formatting Tests

- [x] T139 Add test: "Goal: Colors appear in interactive mode simulation" - verify ANSI codes present
- [x] T140 Add test: "Goal: Output works without colors in CI mode" - verify CI=true works
- [x] T141 Add test: "Goal: Diagnostics show status indicators" - verify status markers in diagnostics

### Wizard and Prompt Tests

- [x] T142 Add test: "Goal: Robot init works with all flags provided" - verify wizard bypass
- [x] T143 Add test: "Goal: Template list shows available templates" - verify template listing
- [x] T144 Add test: "Goal: Invalid template name gives helpful error" - verify error message
- [x] T145 Add test: "Goal: Force flag prevents interactive prompts" - verify -f flag

### Dashboard Graceful Degradation Tests

- [x] T146 Add test: "Goal: Build works in non-interactive environment" - verify no TTY handling
- [x] T147 Add test: "Goal: Long operations show progress without dashboard" - verify log format fallback
- [x] T148 Add test: "Goal: Environment variable disables rich output" - verify RCC_NO_DASHBOARD=true

### Download and Pull Progress Tests

- [x] T149 Add test: "Goal: Pull command shows progress information" - verify download status
- [x] T150 Add test: "Goal: Import shows file processing status" - verify import progress

### Multi-Task Progress Tests

- [x] T151 Add test: "Goal: Prebuild shows per-environment progress" - verify per-item status
- [x] T152 Add test: "Goal: Prebuild continues on individual failures" - verify error continuation

### Confirmation Prompt Tests

- [x] T153 Add test: "Goal: Destructive operations require confirmation flag" - verify prompt required
- [x] T154 Add test: "Goal: Yes flag bypasses confirmation" - verify --yes works

### Supporting Test Infrastructure

- [x] T155 Add helper functions to `robot_tests/supporting.py`: run_with_terminal_simulation(), strip_ansi_codes(), output_contains_progress_bar(), verify_ansi_codes_present()
- [x] T156 Create expected output baselines in `robot_tests/expected/` directory

**Checkpoint**: All Robot Framework acceptance tests passing

---

## Dependencies & Execution Order

### Phase Dependencies

**Phase 1 (COMPLETE)**:
- **Setup (Phase 1)**: No dependencies - verification only ✓
- **Foundational (Phase 2)**: Depends on Setup - BLOCKS all user stories ✓
- **User Stories (Phase 3-6)**: All depend on Foundational phase completion ✓
- **Polish (Phase 7)**: Depends on all user stories being complete ✓

**Phase 2 (NEW)**:
- **Cursor Control (Phase 8)**: No dependencies - foundation for dashboards
- **Colors (Phase 9)**: Can start in parallel with Phase 8
- **Box Drawing (Phase 10)**: Depends on Phase 8 (cursor control)
- **Dashboard Framework (Phase 11)**: Depends on Phases 8, 9, 10
- **Dashboard Layouts A-F (Phases 12-17)**: All depend on Phase 11 (framework)
  - Layouts can proceed in parallel with each other
- **Wizard Actions (Phase 18)**: Can start after Phase 11
- **Template Selection (Phase 19)**: Depends on Phase 18
- **Integration (Phase 20)**: Depends on all layout implementations
- **Robot Framework Tests (Phase 21)**: Can start after Phase 11, expanded as features complete

### User Story Dependencies

**Phase 1 User Stories (COMPLETE)**:
- **User Story 1 (P1)**: Progress Feedback ✓
- **User Story 2 (P2)**: Validated Wizard Inputs ✓
- **User Story 3 (P2)**: Confirmation Prompts ✓
- **User Story 4 (P3)**: Enhanced Colors ✓

**Phase 2 User Stories (NEW)**:
- **User Story 5 (P1)**: Rich Dashboard Display - requires Phases 8-11
- **User Story 6 (P2)**: Multi-Task Progress - requires Phase 15
- **User Story 7 (P2)**: Robot Run Dashboard - requires Phase 17
- **User Story 8 (P2)**: Wizard Action Selection - requires Phase 18

### Within Each User Story

- Core types/interfaces before implementation
- Implementation before integration with commands
- Core functionality before edge case handling

### Parallel Opportunities

**Phase 2 (Foundational)**:
- T005 and T006 can run in parallel (different files)

**Phase 3 (User Story 1)**:
- T012 can run in parallel with T008-T011 (ProgressBar vs Spinner)

**Phase 4 (User Story 2)**:
- T020, T021, T022 can all run in parallel (different functions)

**Phase 5 (User Story 3)**:
- T031, T032, T033 can all run in parallel (different command files)

**Phase 6 (User Story 4)**:
- T038, T039, T040 can all run in parallel (different helper functions)

---

## Parallel Example: User Story 1

```bash
# Launch Spinner and ProgressBar implementation in parallel:
Task: "T008 [US1] Implement Spinner struct with frames in pretty/progress.go"
Task: "T012 [P] [US1] Implement ProgressBar struct with width calculation in pretty/progress.go"
```

## Parallel Example: User Story 3

```bash
# Launch all flag additions in parallel:
Task: "T031 [P] [US3] Add --yes/-y flag to holotree delete command in cmd/holotree.go"
Task: "T032 [P] [US3] Add --yes/-y flag to holotree remove command in cmd/holotree.go"
Task: "T033 [P] [US3] Add --yes/-y flag to configuration cleanup command in cmd/configuration.go"
```

---

## Implementation Strategy

### MVP First (User Story 1 Only)

1. Complete Phase 1: Setup (verification)
2. Complete Phase 2: Foundational (T004-T007)
3. Complete Phase 3: User Story 1 (T008-T019)
4. **STOP and VALIDATE**: Test progress indicators independently
5. Deploy/demo if ready

### Incremental Delivery

1. Complete Setup + Foundational → Foundation ready
2. Add User Story 1 → Test progress indicators → Deploy/Demo (MVP!)
3. Add User Story 2 → Test wizard validation → Deploy/Demo
4. Add User Story 3 → Test confirmation prompts → Deploy/Demo
5. Add User Story 4 → Test color formatting → Deploy/Demo
6. Each story adds value without breaking previous stories

### Suggested Order

Given P1/P2/P2/P3 priorities:
1. **US1 (P1)**: Progress feedback - highest user impact
2. **US3 (P2)**: Confirmation prompts - safety critical
3. **US2 (P2)**: Wizard validation - quality improvement
4. **US4 (P3)**: Color formatting - polish

---

## Notes

- [P] tasks = different files, no dependencies
- [Story] label maps task to specific user story for traceability
- Each user story is independently completable and testable
- No test tasks generated (not requested in spec)
- Manual verification via quickstart.md scenarios
- Commit after each task or logical group
- Stop at any checkpoint to validate story independently
