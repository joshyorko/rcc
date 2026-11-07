*** Settings ***
Library    OperatingSystem
Library    Process

*** Test Cases ***
Native Revocation Operator Preserves Artifact And Running Decision
    [Tags]    revocations
    ${binary}=    Get Environment Variable    RCC_ACCEPTANCE_BINARY    ${CURDIR}/../build/rcc
    ${archive}=    Get Environment Variable    RCC_BUNDLE_TEST_ARCHIVE    ${EMPTY}
    Skip If    not $archive    RCC_BUNDLE_TEST_ARCHIVE native fixture is required
    File Should Exist    ${binary}
    File Should Exist    ${archive}
    ${result}=    Run Process
    ...    go    test    ./cmd    -run    ^TestRevocationOperatorRealCLI$    -count\=1    -v
    ...    cwd=${CURDIR}/..
    ...    env:RCC_ACCEPTANCE_BINARY=${binary}
    ...    env:RCC_BUNDLE_TEST_ARCHIVE=${archive}
    Log    ${result.stdout}
    Log    ${result.stderr}
    Should Be Equal As Integers    ${result.rc}    0
