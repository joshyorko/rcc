*** Settings ***
Library    Process
Library    environment_artifacts/library.py


*** Test Cases ***
Native RCC Binary Spelling Executes Version
    ${rcc}=    Native RCC Binary
    ${expected}=    Source RCC Version
    ${result}=    Run Process Without Group    ${rcc}    version    timeout=10
    Should Be Equal As Integers    ${result.rc}    0
    Should Be Equal    ${result.stdout.strip()}    ${expected}
    Should Be Empty    ${result.stderr}
