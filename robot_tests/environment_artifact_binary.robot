*** Settings ***
Library    Process
Library    environment_artifacts/library.py


*** Test Cases ***
Native RCC Binary Spelling Executes Version
    ${rcc}=    Native RCC Binary
    ${result}=    Run Process Without Group    ${rcc}    version    timeout=10
    Should Be Equal As Integers    ${result.rc}    0
    Should Start With    ${result.stdout}    v18.19.2
    Should Be Empty    ${result.stderr}
