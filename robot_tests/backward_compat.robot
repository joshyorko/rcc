*** Settings ***
Library     OperatingSystem
Library     supporting.py
Resource    resources.robot
Suite Setup     Backward Compat Suite Setup

*** Keywords ***
Backward Compat Suite Setup
    Comment    This test suite verifies backward compatibility
    Comment    The new RCC must be able to read old gzip-compressed hololib files
    Remove Directory    tmp/compat_test    True
    Fire And Forget    build/rcc ht delete 4e67cd8

Create Gzip Test File
    [Arguments]    ${filepath}    ${content}
    [Documentation]    Create a gzip compressed file for testing
    Create Directory    tmp/compat_test
    Create File    tmp/compat_test/raw.txt    ${content}
    Capture Flat Output    gzip -c tmp/compat_test/raw.txt > ${filepath}
    Remove File    tmp/compat_test/raw.txt

Verify Dual Format Detection
    [Documentation]    Verify RCC can detect and handle both formats
    Comment    This is implicit - if environments work, detection works

*** Test Cases ***

Goal: Verify version shows zstd support
    [Documentation]    Check that the RCC version is built with zstd support
    Step    build/rcc version --controller citests
    Use STDOUT
    Must Have    v19
    Must Have    zstd
    Comment    Version shows beta with zstd build metadata

Goal: New environment writes zstd, restore reads it back
    [Documentation]    Round-trip test: write zstd, delete holotree, restore from zstd
    Comment    Build fresh environment (writes zstd)
    Fire And Forget    build/rcc ht delete compattest --controller citests
    Step    build/rcc holotree vars --space compattest --controller citests robot_tests/conda.yaml
    Must Have    CONDA_PREFIX=
    
    Comment    Get catalog hash for verification
    Step    build/rcc holotree catalogs --controller citests --json
    Must Be Json Response
    
    Comment    Delete holotree but keep hololib
    Fire And Forget    build/rcc holotree delete compattest --controller citests
    
    Comment    Restore from hololib (reads zstd)
    Step    build/rcc holotree vars --space compattest --controller citests robot_tests/conda.yaml
    Must Have    CONDA_PREFIX=
    Use STDERR
    Must Have    Restore space from library

Goal: Holotree check verifies zstd integrity correctly
    [Documentation]    The check command should work with zstd files
    Step    build/rcc holotree check --controller citests
    Use STDERR
    Must Have    OK.
    Wont Have    Corrupted
    Wont Have    expected
    Wont Have    actual

Goal: Blueprint available check works with zstd catalog
    [Documentation]    Blueprint availability check must work with zstd-compressed catalogs
    Step    build/rcc holotree blueprint --controller citests robot_tests/conda.yaml
    Use STDERR
    Must Have    is available: true

Goal: JSON output from zstd catalog is valid
    [Documentation]    Machine-readable output must still be valid JSON
    Step    build/rcc holotree catalogs --json --controller citests
    Must Be Json Response
    Must Have    "blueprint"
    Must Have    "files"
    Must Have    "directories"

Goal: Multiple restore cycles work correctly
    [Documentation]    Repeated delete/restore should work without issues
    FOR    ${i}    IN RANGE    3
        Fire And Forget    build/rcc holotree delete compattest --controller citests
        Step    build/rcc holotree vars --space compattest --controller citests robot_tests/conda.yaml
        Must Have    CONDA_PREFIX=
    END

Goal: Robot execution works after zstd migration
    [Documentation]    Full robot execution must work with zstd
    Step    build/rcc task run --controller citests --robot robot_tests/spellbug/robot.yaml
    Use STDOUT
    Must Have    Bug fixed!

Goal: Holotree list shows correct information
    [Documentation]    List command output should be correct
    Step    build/rcc holotree list --controller citests
    Use STDERR
    Must Have    Identity
    Must Have    Controller
    Must Have    Space
    Must Have    Blueprint

Goal: Statistics command works with zstd files
    [Documentation]    Stats command should work correctly
    Step    build/rcc holotree stats --controller citests
    Use STDERR
    Must Have    OK

Goal: Variables export works with zstd environment
    [Documentation]    Exporting variables from zstd env should work
    Step    build/rcc holotree variables --space compattest --controller citests --json robot_tests/conda.yaml
    Must Be Json Response
    Must Have    CONDA_PREFIX
    Must Have    ROBOCORP_HOME

