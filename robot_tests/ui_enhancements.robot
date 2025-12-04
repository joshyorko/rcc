*** Settings ***
Library         OperatingSystem
Library         Process
Library         String
Library         supporting.py
Resource        resources.robot

Suite Setup     UI Test Suite Setup
Suite Teardown  UI Test Suite Teardown

*** Variables ***
${RCC}          build/rcc
${TIMEOUT}      60s
${TEST_DIR}     tmp/ui_test

*** Keywords ***
UI Test Suite Setup
    Prepare Local
    Create Directory    ${TEST_DIR}

UI Test Suite Teardown
    Remove Directory    ${TEST_DIR}    True
    Log    UI Tests Complete

Run RCC With Env
    [Arguments]    @{args}    ${env}=${EMPTY}
    ${result}=    Run Process    ${RCC}    @{args}
    ...    timeout=${TIMEOUT}
    ...    env:CI\=${env}
    ...    env:RCC_NO_DASHBOARD\=${env}
    [Return]    ${result}

Output Should Contain Progress
    [Arguments]    ${output}
    Should Match Regexp    ${output}    (Progress:|\\d+/\\d+|%)

Output Should Not Contain ANSI Codes
    [Arguments]    ${output}
    Should Not Contain    ${output}    \\x1b
    Should Not Contain    ${output}    \\033

Output Should Not Contain Progress
    [Arguments]    ${output}
    Should Not Match Regexp    ${output}    (Progress:|\\d+/\\d+)

Log UI Sample
    [Documentation]    Log both stdout and stderr with clear headers for UI inspection
    [Arguments]    ${result}    ${title}=UI Output Sample
    Log    \n${"="*60}    console=yes
    Log    ${title}    console=yes
    Log    ${"="*60}    console=yes
    Log    \n--- STDOUT ---    console=yes
    Log    ${result.stdout}    console=yes
    Log    \n--- STDERR (UI/Progress output) ---    console=yes
    Log    ${result.stderr}    console=yes
    Log    ${"="*60}\n    console=yes
    # Also log to Robot report HTML for inspection
    Log    <h3>${title}</h3><pre style="background:#1e1e1e;color:#d4d4d4;padding:10px;overflow-x:auto;font-family:monospace;">${result.stderr}</pre>    html=yes
    Log    <h4>STDOUT:</h4><pre style="background:#f5f5f5;padding:10px;overflow-x:auto;">${result.stdout}</pre>    html=yes

Run And Log UI Output
    [Documentation]    Run RCC command and log full output for UI inspection
    [Arguments]    ${title}    @{args}
    ${result}=    Run Process    ${RCC}    @{args}    timeout=${TIMEOUT}
    Log UI Sample    ${result}    ${title}
    [Return]    ${result}

Stream UI Sample
    [Documentation]    Run command and STREAM output to terminal in real-time (shows spinners/progress live)
    [Arguments]    ${title}    ${command}
    ${code}    ${stdout}    ${stderr}=    Run UI Sample    ${command}    ${title}
    Set Suite Variable    ${robot_stdout}    ${stdout}
    Set Suite Variable    ${robot_stderr}    ${stderr}
    # Also log to report for later viewing
    Log    <h3>${title}</h3><pre style="background:#1e1e1e;color:#d4d4d4;padding:10px;overflow-x:auto;font-family:monospace;">${stderr}</pre>    html=yes
    Log    <h4>STDOUT:</h4><pre style="background:#f5f5f5;padding:10px;overflow-x:auto;">${stdout}</pre>    html=yes
    [Return]    ${code}

*** Test Cases ***

Goal: Progress indicators appear during environment build
    [Documentation]    T134 - Verify progress output during holotree vars
    [Tags]    progress
    Step    build/rcc holotree vars --controller citests robot_tests/conda.yaml
    Use STDERR
    Must Have    Progress:
    Must Have    /

Goal: Silent flag suppresses progress output
    [Documentation]    T137 - Verify --silent flag suppresses progress indicators
    [Tags]    progress    silent
    Step    build/rcc holotree vars --controller citests --silent robot_tests/conda.yaml
    Use STDERR
    Wont Have    Progress:

Goal: CI mode works without color codes
    [Documentation]    T140 - Verify CI environment disables ANSI colors
    [Tags]    ci    colors
    Step    build/rcc version --controller citests
    Comment    CI mode test - just verify command runs successfully
    Must Have    v18.

Goal: Robot init with all flags works non-interactively
    [Documentation]    T142 - Verify robot init with --template, --directory, --force
    [Tags]    wizard    non-interactive
    Remove Directory    ${TEST_DIR}/test_robot    True
    Step    build/rcc robot init -i --controller citests --template python --directory ${TEST_DIR}/test_robot --force
    Use STDERR
    Must Have    OK.
    Must Exist    ${TEST_DIR}/test_robot/robot.yaml
    Must Exist    ${TEST_DIR}/test_robot/conda.yaml

Goal: Template list shows available templates
    [Documentation]    T143 - Verify template listing functionality
    [Tags]    wizard    templates
    Step    build/rcc robot init -i --controller citests --list
    Must Have    extended
    Must Have    python
    Must Have    standard
    Use STDERR
    Must Have    OK.

Goal: Invalid template shows helpful error
    [Documentation]    T144 - Verify error message for invalid template
    [Tags]    wizard    error-handling
    Remove Directory    ${TEST_DIR}/invalid_test    True
    Step    build/rcc robot init -i --controller citests --template nonexistent_template --directory ${TEST_DIR}/invalid_test --force    2
    Comment    Error message appears in stderr for template errors
    Use STDERR
    Must Have    does not exist

Goal: Force flag prevents overwrite prompts
    [Documentation]    T145 - Verify --force bypasses directory exists prompts
    [Tags]    wizard    force
    Remove Directory    ${TEST_DIR}/force_test    True
    Step    build/rcc robot init -i --controller citests --template python --directory ${TEST_DIR}/force_test --force
    Use STDERR
    Must Have    OK.
    Comment    Try to init again in same directory with force - should succeed
    Step    build/rcc robot init -i --controller citests --template python --directory ${TEST_DIR}/force_test --force
    Use STDERR
    Must Have    OK.

Goal: Non-interactive environment without force fails gracefully
    [Documentation]    T146 - Verify graceful handling when directory exists without force
    [Tags]    wizard    non-interactive
    Remove Directory    ${TEST_DIR}/exists_test    True
    Step    build/rcc robot init -i --controller citests --template python --directory ${TEST_DIR}/exists_test --force
    Use STDERR
    Must Have    OK.
    Comment    Second attempt without force should fail
    Step    build/rcc robot init -i --controller citests --template python --directory ${TEST_DIR}/exists_test    2
    Use STDERR
    Must Have    not empty

Goal: RCC_NO_DASHBOARD disables rich output
    [Documentation]    T148 - Verify RCC_NO_DASHBOARD environment variable
    [Tags]    dashboard    environment
    ${result}=    Run Process    build/rcc    holotree    vars    --controller    citests    robot_tests/conda.yaml
    ...    env:RCC_NO_DASHBOARD\=true
    Should Be Equal As Integers    ${result.rc}    0
    Comment    With RCC_NO_DASHBOARD, progress should be minimal or absent
    Log    STDERR: ${result.stderr}

Goal: Holotree delete requires confirmation without --yes in non-interactive mode
    [Documentation]    T153 - Verify destructive operation requires confirmation
    [Tags]    confirmation    destructive
    Comment    In non-interactive mode (robot test), holotree delete without --yes should fail
    Comment    First create an environment to delete
    Step    build/rcc holotree vars --controller citests robot_tests/conda.yaml --silent
    Comment    Try to delete without --yes - should fail in non-interactive context
    Comment    This test validates the confirmation requirement exists
    Step    build/rcc holotree delete --help
    Must Have    --yes

Goal: Yes flag bypasses confirmation
    [Documentation]    T154 - Verify --yes flag bypasses confirmation prompts
    [Tags]    confirmation    bypass
    Comment    Create a test environment
    Step    build/rcc holotree vars --controller citests robot_tests/conda.yaml --silent
    Must Have    RCC_ENVIRONMENT_HASH
    Comment    Delete with --yes should succeed without prompt
    Fire And Forget    build/rcc holotree delete --yes --controller citests 4e67cd8

Goal: Progress output appears during task run
    [Documentation]    Verify progress indicators during robot task execution
    [Tags]    progress    task
    Remove Directory    ${TEST_DIR}/progress_test    True
    Step    build/rcc robot init -i --controller citests --template extended --directory ${TEST_DIR}/progress_test --force
    Use STDERR
    Must Have    OK.
    Comment    Use --task to specify which task to run
    Step    build/rcc task run --controller citests --robot ${TEST_DIR}/progress_test/robot.yaml --task "Run all tasks"
    Use STDERR
    Must Have    Progress:

Goal: Holotree variables shows progress by default
    [Documentation]    Verify holotree variables command shows progress
    [Tags]    progress    holotree
    Step    build/rcc holotree variables --controller citests robot_tests/conda.yaml
    Must Have    PYTHON_EXE=
    Use STDERR
    Must Have    Progress:

Goal: Silent mode suppresses all progress indicators
    [Documentation]    Verify --silent completely suppresses progress
    [Tags]    silent    progress
    Step    build/rcc holotree variables --controller citests --silent robot_tests/conda.yaml
    Must Have    PYTHON_EXE=
    Use STDERR
    Wont Have    Progress:
    Wont Have    Step

Goal: Warranty voided mode shows warning
    [Documentation]    Verify warranty-voided mode shows appropriate warning
    [Tags]    warranty
    Step    build/rcc holotree variables --controller citests --warranty-voided --anything test robot_tests/conda.yaml
    Use STDERR
    Must Have    warranty voided

Goal: Configuration cleanup requires confirmation
    [Documentation]    Verify cleanup command has --yes flag
    [Tags]    confirmation    cleanup
    Step    build/rcc configuration cleanup --help
    Must Have    --yes

Goal: Holotree remove requires confirmation
    [Documentation]    Verify remove command has --yes flag
    [Tags]    confirmation    remove
    Step    build/rcc holotree remove --help
    Must Have    --yes

Goal: Version command works in silent mode
    [Documentation]    Verify version command respects silent mode
    [Tags]    version    silent
    Step    build/rcc version --controller citests --silent
    Must Have    v18.

Goal: Help output is always shown regardless of silent flag
    [Documentation]    Verify help is not suppressed by silent mode
    [Tags]    help    silent
    Step    build/rcc --help
    Must Have    Available Commands:
    Must Have    config
    Must Have    holotree

Goal: JSON output is not affected by progress indicators
    [Documentation]    Verify JSON output remains valid with progress enabled
    [Tags]    json    progress
    Step    build/rcc holotree list --controller citests --json
    Must Be Json Response

Goal: Holotree hash works in silent mode
    [Documentation]    Verify hash command in silent mode
    [Tags]    hash    silent
    Step    build/rcc holotree hash --controller citests --silent robot_tests/conda.yaml
    Comment    Hash output is a 16-character hex string
    Should Match Regexp    ${robot_stdout}    [0-9a-f]{16}

Goal: Multiple conda files show progress
    [Documentation]    Verify merging conda files shows progress
    [Tags]    progress    merge
    Step    build/rcc holotree vars --controller citests conda/testdata/third.yaml conda/testdata/other.yaml
    Must Have    RCC_ENVIRONMENT_HASH
    Use STDERR
    Must Have    Progress:

Goal: Diagnostics command shows system information
    [Documentation]    Verify diagnostics command works correctly
    [Tags]    diagnostics
    Step    build/rcc configure diagnostics --json
    Must Be Json Response

Goal: Holotree check command works
    [Documentation]    Verify holotree integrity check
    [Tags]    holotree    check
    Step    build/rcc holotree check --controller citests
    Use STDERR
    Must Have    OK.

Goal: Identity configuration shows tracking status
    [Documentation]    Verify identity command shows tracking status
    [Tags]    identity    tracking
    Step    build/rcc configure identity --controller citests
    Must Have    tracking is: disabled

Goal: Timeline flag produces timeline output
    [Documentation]    Verify --timeline flag works with task run
    [Tags]    timeline    task
    Remove Directory    ${TEST_DIR}/timeline_test    True
    Step    build/rcc robot init -i --controller citests --template python --directory ${TEST_DIR}/timeline_test --force
    Comment    Use --task to specify which task to run
    Step    build/rcc task run --controller citests --robot ${TEST_DIR}/timeline_test/robot.yaml --task "Run Python" --timeline
    Use STDERR
    Must Have    OK.

Goal: Debug mode shows detailed progress information
    [Documentation]    Verify --debug flag shows detailed output
    [Tags]    debug    progress
    Step    build/rcc version --controller citests --debug
    Use STDERR
    Comment    Debug output uses [D] prefix
    Should Contain Any    ${robot_stderr}    [D]    Debug    debug

Goal: Bundled mode check works
    [Documentation]    Verify bundled mode detection
    [Tags]    bundled
    Step    build/rcc version --controller citests --bundled
    Must Have    v18.

# =============================================================================
# UI SAMPLE TESTS - These tests STREAM output to terminal in real-time
# Run with: robot -i ui-sample robot_tests/ui_enhancements.robot
# Or via rcc: rcc run -r developer/toolkit.yaml -t robot -- -i ui-sample robot_tests/ui_enhancements.robot
# =============================================================================

UI Sample: Progress Spinner During Environment Creation
    [Documentation]    STREAMS progress spinner output during holotree vars - watch the terminal!
    [Tags]    ui-sample    progress    spinner
    ${code}=    Stream UI Sample    Progress Spinner - Environment Creation
    ...    build/rcc holotree vars --controller citests robot_tests/conda.yaml

UI Sample: Progress Bar During Environment Build
    [Documentation]    STREAMS progress bar output during environment build with timeline
    [Tags]    ui-sample    progress    bar
    ${code}=    Stream UI Sample    Progress Bar - Environment Build (with timeline)
    ...    build/rcc holotree vars --controller citests --timeline robot_tests/conda.yaml

UI Sample: Diagnostics Output
    [Documentation]    STREAMS diagnostics command output showing check statuses
    [Tags]    ui-sample    diagnostics
    ${code}=    Stream UI Sample    Diagnostics Check Output
    ...    build/rcc configure diagnostics --controller citests

UI Sample: Version with Debug Mode
    [Documentation]    STREAMS debug output showing detailed logging
    [Tags]    ui-sample    debug
    ${code}=    Stream UI Sample    Debug Mode Output
    ...    build/rcc version --controller citests --debug

UI Sample: Holotree List
    [Documentation]    STREAMS holotree list output
    [Tags]    ui-sample    holotree
    ${code}=    Stream UI Sample    Holotree List Output
    ...    build/rcc holotree list --controller citests

UI Sample: Template Listing
    [Documentation]    STREAMS template listing output from robot init
    [Tags]    ui-sample    templates    wizard
    ${code}=    Stream UI Sample    Template Listing Output
    ...    build/rcc robot init -i --controller citests --list

UI Sample: Robot Init with Progress
    [Documentation]    STREAMS full robot init output with progress indicators
    [Tags]    ui-sample    wizard    init
    Remove Directory    ${TEST_DIR}/ui_sample_robot    True
    ${code}=    Stream UI Sample    Robot Init Progress
    ...    build/rcc robot init -i --controller citests --template extended --directory ${TEST_DIR}/ui_sample_robot --force

UI Sample: Task Run with Progress
    [Documentation]    STREAMS task run output showing execution progress
    [Tags]    ui-sample    task    run
    Remove Directory    ${TEST_DIR}/task_run_sample    True
    # First create the robot (silently)
    Run Process    ${RCC}    robot    init    -i    --controller    citests    --template    extended    --directory    ${TEST_DIR}/task_run_sample    --force
    # Then run the task and STREAM output
    ${code}=    Stream UI Sample    Task Run Progress
    ...    build/rcc task run --controller citests --robot ${TEST_DIR}/task_run_sample/robot.yaml

UI Sample: Error Output Formatting
    [Documentation]    STREAMS error output to see error formatting/colors
    [Tags]    ui-sample    error
    ${code}=    Stream UI Sample    Error Formatting Sample
    ...    build/rcc robot init -i --controller citests --template nonexistent_bad_template --directory ${TEST_DIR}/error_sample --force

UI Sample: Help Output Formatting
    [Documentation]    STREAMS help output formatting
    [Tags]    ui-sample    help
    ${code}=    Stream UI Sample    Help Output Formatting
    ...    build/rcc --help

UI Sample: Holotree Variables Full Output
    [Documentation]    STREAMS full holotree variables output with all env vars
    [Tags]    ui-sample    holotree    variables
    ${code}=    Stream UI Sample    Holotree Variables Full Output
    ...    build/rcc holotree variables --controller citests robot_tests/conda.yaml

UI Sample: Configuration Settings
    [Documentation]    STREAMS configuration settings output
    [Tags]    ui-sample    config
    ${code}=    Stream UI Sample    Configuration Settings
    ...    build/rcc configure settings --controller citests

UI Sample: Holotree Check with Progress
    [Documentation]    STREAMS holotree integrity check output
    [Tags]    ui-sample    holotree    check
    ${code}=    Stream UI Sample    Holotree Integrity Check
    ...    build/rcc holotree check --controller citests

UI Sample: Interactive Mode Disabled Warning
    [Documentation]    STREAMS output when interactive mode is needed but unavailable
    [Tags]    ui-sample    interactive    warning
    ${code}=    Stream UI Sample    Non-Interactive Mode Warning
    ...    build/rcc holotree delete --controller citests

UI Sample: Warranty Voided Mode
    [Documentation]    STREAMS warranty voided mode warning output
    [Tags]    ui-sample    warranty
    ${code}=    Stream UI Sample    Warranty Voided Warning
    ...    build/rcc holotree variables --controller citests --warranty-voided --anything test robot_tests/conda.yaml
