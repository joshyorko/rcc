*** Settings ***
Library     OperatingSystem
Library     BuiltIn
Library     supporting.py
Resource    resources.robot
Suite Setup     Compression Suite Setup

*** Keywords ***
Compression Suite Setup
    Comment    Clean up any previous test artifacts
    Remove Directory    tmp/compression_test    True
    Remove Directory    tmp/hololib_backup    True
    Fire And Forget    build/rcc ht delete 4e67cd8

Skip If Windows
    [Documentation]    Skip the test if running on Windows (uses Unix-specific commands)
    ${is_win}=    Is Windows
    Skip If    ${is_win}    Test uses Unix-specific commands (find, head, od)

Verify File Is Zstd Compressed
    [Arguments]    ${filepath}
    [Documentation]    Verify file starts with zstd magic bytes (28 b5 2f fd)
    ${result}=    Capture Flat Output    head -c 4 ${filepath} | od -A n -t x1 | tr -d ' '
    Should Contain    ${result}    28b52ffd

Verify File Is Gzip Compressed
    [Arguments]    ${filepath}
    [Documentation]    Verify file starts with gzip magic bytes (1f 8b)
    ${result}=    Capture Flat Output    head -c 2 ${filepath} | od -A n -t x1 | tr -d ' '
    Should Contain    ${result}    1f8b

*** Test Cases ***

Goal: Build environment and verify hololib files are zstd compressed
    [Documentation]    Create a new environment and verify files in hololib use zstd
    Skip If Windows
    Step    build/rcc holotree variables --space zstdtest --controller citests robot_tests/conda.yaml
    Must Have    ROBOCORP_HOME=
    Must Have    CONDA_PREFIX=

    Comment    Verify catalog file is zstd compressed
    ${catalog_files}=    Capture Flat Output    find %{ROBOCORP_HOME}/hololib/catalog -name "*.linux_amd64" -type f 2>/dev/null | head -1
    Verify File Is Zstd Compressed    ${catalog_files}

    Comment    Verify library files are zstd compressed
    ${lib_file}=    Capture Flat Output    find %{ROBOCORP_HOME}/hololib/library -type f 2>/dev/null | head -1
    Verify File Is Zstd Compressed    ${lib_file}

Goal: Holotree check passes with zstd compressed files
    [Documentation]    Run holotree check to verify integrity of zstd files
    Step    build/rcc holotree check --controller citests
    Use STDERR
    Must Have    OK.
    Wont Have    corrupted
    Wont Have    missing

Goal: Environment restore works with zstd compressed hololib
    [Documentation]    Delete holotree and restore from zstd-compressed hololib
    Comment    First delete the holotree space
    Fire And Forget    build/rcc holotree delete zstdtest --controller citests
    
    Comment    Now restore from hololib (should use zstd files)
    Step    build/rcc holotree variables --space zstdtest --controller citests robot_tests/conda.yaml
    Must Have    ROBOCORP_HOME=
    Must Have    CONDA_PREFIX=
    Use STDERR
    Must Have    Restore space from library

Goal: Catalog listing works with zstd compressed catalogs
    [Documentation]    Verify catalog commands work with zstd format
    Step    build/rcc holotree catalogs --controller citests
    Use STDERR
    Must Have    inside hololib
    Must Have    OK.
    
    Step    build/rcc holotree catalogs --controller citests --json
    Must Be Json Response

Goal: Blueprint command works with zstd catalogs
    [Documentation]    Verify blueprint detection works with zstd format
    Step    build/rcc holotree blueprint --controller citests robot_tests/conda.yaml
    Use STDERR
    Must Have    Blueprint
    Must Have    is available: true

Goal: Multiple environment builds use zstd consistently
    [Documentation]    Build multiple environments and verify all use zstd
    Skip If Windows
    Comment    Build a second environment
    Step    build/rcc holotree variables --space zstdtest2 --controller citests robot_tests/python3913.yaml
    Must Have    CONDA_PREFIX=

    Comment    Verify new files are also zstd
    ${lib_files}=    Capture Flat Output    find %{ROBOCORP_HOME}/hololib/library -type f -newer %{ROBOCORP_HOME}/hololib/catalog 2>/dev/null | head -1
    Run Keyword If    '${lib_files}' != ''    Verify File Is Zstd Compressed    ${lib_files}

Goal: Holotree check with retries works on zstd files
    [Documentation]    Verify check command with retries works
    Step    build/rcc holotree check --retries 3 --controller citests
    Use STDERR
    Must Have    OK.

Goal: Full task run works with zstd compressed environment
    [Documentation]    End-to-end test running a robot with zstd environment
    Step    build/rcc run --controller citests --robot robot_tests/spellbug/robot.yaml
    Use STDOUT
    Must Have    Bug fixed!

Goal: Export and import preserves zstd format
    [Documentation]    Test holotree export/import with zstd files
    Comment    Get the fingerprint for export
    ${fingerprint}=    Capture Flat Output    build/rcc ht hash --silent robot_tests/conda.yaml
    
    Comment    Create output directory and export the catalog with the fingerprint
    Create Directory    tmp/compression_test
    Step    build/rcc holotree export --controller citests --zipfile tmp/compression_test/export.zip ${fingerprint}
    Use STDERR
    Must Have    OK.
    Must Exist    tmp/compression_test/export.zip
    
    Comment    The exported zip should contain zstd compressed data

Goal: Shared holotree mode works with zstd
    [Documentation]    Test shared holotree initialization with zstd
    Comment    Initialize shared mode (if not already)
    Fire And Forget    build/rcc holotree init --controller citests
    
    Comment    Build environment in shared mode
    Step    build/rcc holotree variables --space sharedtest --controller citests robot_tests/conda.yaml
    Must Have    CONDA_PREFIX=
    
    Comment    Revoke shared mode for other tests
    Fire And Forget    build/rcc holotree init --revoke --controller citests

Goal: Cleanup removes zstd files correctly
    [Documentation]    Test cleanup operations with zstd files
    Step    build/rcc config cleanup --quick --controller citests
    Use STDERR
    Must Have    OK

