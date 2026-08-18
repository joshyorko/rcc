*** Settings ***
Library         Collections
Library         OperatingSystem
Library         Process
Library         environment_artifacts/library.py
Resource        resources.robot
Suite Setup     Prepare Environment Artifact Acceptance
Suite Teardown  Clean Environment Artifact Acceptance


*** Variables ***
${RCC}          ${CURDIR}/../build/rcc
${ROBOT}        ${CURDIR}/environment_artifacts/robot.yaml
${TASK}         ${CURDIR}/environment_artifacts/task.py


*** Test Cases ***
Portable Environment Is Published Acquired Executed And Reused Offline
    Should Not Be Equal    ${A_HOME}    ${B_HOME}
    Environment Artifact Home Should Be Empty    ${B_HOME}

    ${server}=    Start Process
    ...    ${RCC}    cache    serve
    ...    --root    ${PROVIDER_ROOT}
    ...    --listen    127.0.0.1:0
    ...    --json
    ...    alias=environment-provider
    ...    stdout=${SERVER_STDOUT}
    ...    stderr=${SERVER_STDERR}
    ...    env=${A_ENV}
    Set Suite Variable    ${SERVER}    ${server}
    ${server_json}=    Wait For JSON File    ${SERVER_STDOUT}
    ${provider_url}=    Set Variable    ${server_json}[url]
    Should Be Equal    ${server_json}[root]    ${PROVIDER_ROOT}

    ${published_result}=    Run Process
    ...    ${RCC}    env    publish
    ...    --robot    ${ROBOT}
    ...    --provider    ${provider_url}
    ...    --json
    ...    env=${A_ENV}
    Should Be Equal As Integers    ${published_result.rc}    0
    Should Not Be Empty    ${published_result.stderr}
    ${published}=    Parse JSON    ${published_result.stdout}
    ${artifact}=    Set Variable    ${published}[artifactDigest]
    Should Start With    ${artifact}    sha256:
    Should Be True    ${published}[objectCount] > 0
    Provider Should Contain Manifest    ${PROVIDER_ROOT}    ${artifact}

    ${cold_result}=    Run Process
    ...    ${RCC}    env    acquire
    ...    --artifact    ${artifact}
    ...    --provider    ${provider_url}
    ...    --json
    ...    env=${B_ENV}
    Should Be Equal As Integers    ${cold_result.rc}    0
    ${cold}=    Parse JSON    ${cold_result.stdout}
    Should Be Equal    ${cold}[artifactDigest]    ${artifact}
    Should Be Equal    ${cold}[cacheHit]    provider
    Should Start With    ${cold}[path]    ${B_HOME}${/}holotree${/}
    Should Not Contain    ${cold_result.stderr}    micromamba phase

    ${exec_result}=    Run Process
    ...    ${RCC}    env    exec
    ...    --artifact    ${artifact}
    ...    --json
    ...    --    python    ${TASK}    ${PROOF_FILE}
    ...    env=${B_ENV}
    Should Be Equal As Integers    ${exec_result.rc}    0
    ${executed}=    Parse JSON    ${exec_result.stdout}
    Should Be Equal    ${executed}[artifactDigest]    ${artifact}
    Should Be Equal    ${executed}[materializationId]    ${cold}[materializationId]
    Should Be Equal    ${executed}[cacheHit]    local-materialization
    Should Be Equal As Integers    ${executed}[exitCode]    0
    ${proof}=    Read JSON File    ${PROOF_FILE}
    Should Not Be Empty    ${proof}[yamlVersion]
    Should Be Equal    ${proof}[condaOffline]    true
    Should Be Equal    ${proof}[mambaOffline]    true
    Should Be Equal    ${proof}[pipNoIndex]    1
    Should Be Equal    ${proof}[uvNoIndex]    1
    Package Manager Caches Should Be Empty    ${B_HOME}

    ${stopped}=    Terminate Process    environment-provider
    Should Be Equal As Integers    ${stopped.rc}    0
    Provider Should Be Unreachable    ${provider_url}

    ${warm_result}=    Run Process
    ...    ${RCC}    env    acquire
    ...    --artifact    ${artifact}
    ...    --json
    ...    env=${B_ENV}
    Should Be Equal As Integers    ${warm_result.rc}    0
    ${warm}=    Parse JSON    ${warm_result.stdout}
    Should Be Equal    ${warm}[artifactDigest]    ${artifact}
    Should Be Equal    ${warm}[materializationId]    ${cold}[materializationId]
    Should Be Equal    ${warm}[path]    ${cold}[path]
    Should Be Equal    ${warm}[cacheHit]    local-materialization
    Should Not Contain    ${warm_result.stderr}    micromamba phase
    Package Manager Caches Should Be Empty    ${B_HOME}


*** Keywords ***
Prepare Environment Artifact Acceptance
    Set Suite Variable    ${FIXTURE_ROOT}    ${None}
    ${is_linux}=    Evaluate    sys.platform.startswith("linux")    modules=sys
    Run Keyword If    not ${is_linux}    Skip    Environment Artifact acceptance is Linux-only
    ${fixture}=    New Environment Artifact Fixture
    Set Suite Variable    ${FIXTURE_ROOT}      ${fixture}[root]
    Set Suite Variable    ${A_HOME}            ${fixture}[aHome]
    Set Suite Variable    ${B_HOME}            ${fixture}[bHome]
    Set Suite Variable    ${PROVIDER_ROOT}     ${fixture}[providerRoot]
    Set Suite Variable    ${SERVER_STDOUT}     ${fixture}[serverStdout]
    Set Suite Variable    ${SERVER_STDERR}     ${fixture}[serverStderr]
    Set Suite Variable    ${PROOF_FILE}        ${fixture}[proofFile]
    ${a_env}=    Environment Artifact Process Environment    ${A_HOME}    ${False}
    ${b_env}=    Environment Artifact Process Environment    ${B_HOME}    ${True}
    Set Suite Variable    ${A_ENV}    ${a_env}
    Set Suite Variable    ${B_ENV}    ${b_env}

Clean Environment Artifact Acceptance
    Run Keyword And Ignore Error    Terminate All Processes    kill=${True}
    Run Keyword If    $FIXTURE_ROOT is not None    Remove Environment Artifact Fixture    ${FIXTURE_ROOT}
