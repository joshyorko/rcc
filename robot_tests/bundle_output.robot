*** Settings ***
Library    Process
Library    OperatingSystem

*** Variables ***
${RCC}    ${CURDIR}${/}..${/}build${/}rcc

*** Test Cases ***
Run From Bundle Finalizes Output Across Modes And Outcomes
    ${root}=    Set Variable    ${OUTPUT DIR}${/}bundle-output
    Create Directory    ${root}
    ${result}=    Run Process
    ...    python3
    ...    ${CURDIR}${/}bundle_output_fixture.py
    ...    --rcc    ${RCC}
    ...    --root    ${root}
    ...    stdout=${root}${/}fixture.stdout
    ...    stderr=${root}${/}fixture.stderr    timeout=3600
    Should Be Equal As Integers    ${result.rc}    0    ${result.stdout}\n${result.stderr}
    Log    ${result.stdout}
