*** Settings ***
Library         environment_exec_258/library.py
Suite Setup     Prepare Environment Exec 258    ${RCC}
Suite Teardown  Clean Environment Exec 258


*** Variables ***
${RCC}    ${CURDIR}/../build/rcc


*** Test Cases ***
Artifact-Owned Executable Resolution Preserves Delegation And Receipts
    ${unique}=    Execute 258 Inherited    rcc-issue258-unique
    Should Be Equal As Integers    ${unique}[rc]    0
    Should Be Equal    ${unique}[stdoutText]    artifact-stdout
    Should Be Equal    ${unique}[stderrText]    artifact-stderr
    Should Be Equal    ${unique}[receipt][status]    completed
    Should Be True    ${unique}[receipt][leaseReleased]

    ${collision}=    Execute 258 Inherited    rcc-issue258-probe
    Should Be Equal As Integers    ${collision}[rc]    0
    Should Be Equal    ${collision}[stdoutText]    artifact-stdout
    Should Not Contain    ${collision}[stdout]    host-decoy
    Should Be Equal    ${collision}[stderrText]    artifact-stderr
    Should Be True    ${collision}[receipt][leaseReleased]

    ${json}=    Execute 258 JSON    rcc-issue258-probe
    Should Be Equal As Integers    ${json}[rc]    0
    Should Be Equal    ${json}[receipt][status]    completed
    Should Not Contain    ${json}[stdout]    artifact-stdout
    Should Not Contain    ${json}[stderr]    artifact-stderr
    Should Not Contain    ${json}[stdout]    host-decoy
    Should Be Equal    ${json}[receipt][status]    completed
    Should Be True    ${json}[receipt][leaseReleased]

    ${python}=    Execute 258 Inherited    python    -c    print('artifact-python')
    Should Be Equal As Integers    ${python}[rc]    0
    Should Be Equal    ${python}[stdoutText]    artifact-python
    Should Be True    ${python}[receipt][leaseReleased]

    ${relative}=    Execute 258 Inherited    ./bin/rcc-issue258-unique
    Should Be Equal As Integers    ${relative}[rc]    0
    Should Be Equal    ${relative}[stdoutText]    artifact-stdout
    Should Be True    ${relative}[receipt][leaseReleased]

    ${absolute}=    Execute 258 Inherited    ${HOST_DECOY}
    Should Be Equal As Integers    ${absolute}[rc]    0
    Should Be Equal    ${absolute}[stdoutText]    host-decoy
    Should Be True    ${absolute}[receipt][leaseReleased]

    ${missing}=    Execute 258 Inherited    rcc-issue258-missing
    Should Not Be Equal As Integers    ${missing}[rc]    0
    Should Be Empty    ${missing}[stdout]
    Should Not Contain    ${missing}[stderr]    artifact-stderr
    Should Not Contain    ${missing}[stderr]    host-decoy
    Should Contain    ${missing}[stderr]    rcc-issue258-missing
    Should Be Equal    ${missing}[receipt][status]    failed
    Should Be True    ${missing}[receipt][leaseReleased]

    Create 258 Escaped Probe
    ${traversal}=    Execute 258 Inherited    ../rcc258-escaped-probe
    Should Not Be Equal As Integers    ${traversal}[rc]    0
    Should Be Empty    ${traversal}[stdout]
    Should Not Contain    ${traversal}[stderr]    escaped-artifact
    Should Contain    ${traversal}[stderr]    outside the acquired environment
    Should Be True    ${traversal}[receipt][leaseReleased]
