*** Settings ***
Library    Process
Library    environment_artifacts/library.py


*** Test Cases ***
Native RCC Binary Spelling Executes Version
    ${rcc}=    Native RCC Binary
    ${result}=    Run Process    ${rcc}    version    timeout=10s    on_timeout=terminate
    Log To Console    version-probe rc=${result.rc} stdout=${result.stdout} stderr=${result.stderr}
    Should Be Equal As Integers    ${result.rc}    0
    Should Start With    ${result.stdout}    v18.19.0
    Should Be Empty    ${result.stderr}
