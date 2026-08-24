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
    library.Log To Console    phase=test:start
    Should Not Be Equal    ${A_HOME}    ${B_HOME}
    Environment Artifact Home Should Be Empty    ${B_HOME}
    library.Log To Console    phase=consumer-home-empty:complete

    library.Log To Console    phase=binary-version:start
    ${binary_version_result}=    Run Process    ${RCC}    version
    Should Be Equal As Integers    ${binary_version_result.rc}    0
    ${binary_version}=    Set Variable    ${binary_version_result.stdout}
    library.Log To Console    phase=binary-version:complete

    library.Log To Console    phase=provider-server-start:start
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
    library.Log To Console    phase=provider-server-start:complete
    library.Log To Console    phase=provider-server-json:start
    ${server_json}=    Wait For JSON File    ${SERVER_STDOUT}
    ${provider_url}=    Set Variable    ${server_json}[url]
    Should Be Equal    ${server_json}[root]    ${PROVIDER_ROOT}
    library.Log To Console    phase=provider-server-json:complete

    Set To Dictionary    ${A_ENV}    RCC_TEST_PROVIDER_AUTHORIZATION=Bearer robot-test
    Log To Console    phase=producer-profile:start
    ${a_profile_result}=    Run Process
    ...    ${RCC}    provider    add    office
    ...    --type    http
    ...    --url    ${provider_url}
    ...    --authorization-env    RCC_TEST_PROVIDER_AUTHORIZATION
    ...    --json
    ...    env=${A_ENV}
    Should Be Equal As Integers    ${a_profile_result.rc}    0
    ${a_profile}=    Parse JSON    ${a_profile_result.stdout}
    Should Be Equal    ${a_profile}[name]    office
    Should Be Equal    ${a_profile}[type]    http
    Should Be Equal    ${a_profile}[url]    ${provider_url}
    Should Be Equal    ${a_profile}[authorizationEnv]    RCC_TEST_PROVIDER_AUTHORIZATION
    Should Not Contain    ${a_profile_result.stdout}    Bearer robot-test
    Should Not Contain    ${a_profile_result.stderr}    Bearer robot-test
    Log To Console    phase=producer-profile:complete

    Set To Dictionary    ${B_ENV}    RCC_TEST_PROVIDER_AUTHORIZATION=Bearer robot-test
    Log To Console    phase=consumer-profile:start
    ${b_profile_result}=    Run Process
    ...    ${RCC}    provider    add    office
    ...    --type    http
    ...    --url    ${provider_url}
    ...    --authorization-env    RCC_TEST_PROVIDER_AUTHORIZATION
    ...    --json
    ...    env=${B_ENV}
    Should Be Equal As Integers    ${b_profile_result.rc}    0
    ${b_profile}=    Parse JSON    ${b_profile_result.stdout}
    Should Be Equal    ${b_profile}[name]    office
    Should Be Equal    ${b_profile}[type]    http
    Should Be Equal    ${b_profile}[url]    ${provider_url}
    Should Be Equal    ${b_profile}[authorizationEnv]    RCC_TEST_PROVIDER_AUTHORIZATION
    Should Not Contain    ${b_profile_result.stdout}    Bearer robot-test
    Should Not Contain    ${b_profile_result.stderr}    Bearer robot-test
    Log To Console    phase=consumer-profile:complete

    Log To Console    phase=publish:start
    ${published_result}=    Run Process
    ...    ${RCC}    env    publish
    ...    --robot    ${ROBOT}
    ...    --provider    office
    ...    --json
    ...    env=${A_ENV}
    Log    ${published_result.stderr}
    Should Be Equal As Integers    ${published_result.rc}    0
    Should Not Contain    ${published_result.stdout}    Bearer robot-test
    Should Not Contain    ${published_result.stderr}    Bearer robot-test
    Should Not Be Empty    ${published_result.stderr}
    Log To Console    phase=publish:complete
    ${published}=    Parse JSON    ${published_result.stdout}
    ${artifact}=    Set Variable    ${published}[artifactDigest]
    Should Start With    ${artifact}    sha256:
    Should Be True    ${published}[objectCount] > 0
    Provider Should Contain Manifest    ${PROVIDER_ROOT}    ${artifact}

    Log To Console    phase=cold-acquire:start
    ${cold_result}=    Run Process
    ...    ${RCC}    env    acquire
    ...    --artifact    ${artifact}
    ...    --provider    office
    ...    --trust-carrier    ${TRUST_ROOT}
    ...    --trust-carrier-type    filesystem
    ...    --permissive-local
    ...    --json
    ...    env=${B_ENV}
    Log    ${cold_result.stderr}
    Should Be Equal As Integers    ${cold_result.rc}    0
    Should Not Contain    ${cold_result.stdout}    Bearer robot-test
    Should Not Contain    ${cold_result.stderr}    Bearer robot-test
    ${cold}=    Parse JSON    ${cold_result.stdout}
    Should Be Equal    ${cold}[artifactDigest]    ${artifact}
    Should Be Equal    ${cold}[cacheHit]    provider
    Should Start With    ${cold}[path]    ${B_HOME}${/}holotree${/}
    Should Not Contain    ${cold_result.stderr}    micromamba phase
    Log To Console    phase=cold-acquire:complete

    Log To Console    phase=artifact-exec:start
    ${exec_result}=    Run Process
    ...    ${RCC}    env    exec
    ...    --artifact    ${artifact}
    ...    --trust-carrier    ${TRUST_ROOT}
    ...    --trust-carrier-type    filesystem
    ...    --permissive-local
    ...    --json
    ...    --    python    ${TASK}    ${PROOF_FILE}
    ...    env=${B_ENV}
    Should Be Equal As Integers    ${exec_result.rc}    0
    Log To Console    phase=artifact-exec:complete
    ${executed}=    Parse JSON    ${exec_result.stdout}
    Should Be Equal    ${executed}[artifactDigest]    ${artifact}
    Should Be Equal    ${executed}[materializationId]    ${cold}[materializationId]
    Should Be Equal    ${executed}[cacheHit]    local-materialization
    Should Be Equal As Integers    ${executed}[exitCode]    0
    Should Not Be Empty    ${executed}[leaseId]
    ${proof}=    Read JSON File    ${PROOF_FILE}
    Should Not Be Empty    ${proof}[yamlVersion]
    Should Be Equal    ${proof}[condaOffline]    true
    Should Be Equal    ${proof}[mambaOffline]    true
    Should Be Equal    ${proof}[pipNoIndex]    1
    Should Be Equal    ${proof}[uvNoIndex]    1
    Should Be Equal    ${proof}[nativeImport]    sqlite3
    Should Not Be Empty    ${proof}[nativeExtension]
    Should Not Be Empty    ${proof}[sqliteVersion]
    Package Manager Caches Should Be Empty    ${B_HOME}

    ${source_bundle}=    Set Variable    ${FIXTURE_ROOT}${/}source-only.py
    Log To Console    phase=source-bundle:start
    ${source_bundle_result}=    Run Process
    ...    ${RCC}    robot    bundle
    ...    --robot    ${ROBOT}
    ...    --output    ${source_bundle}
    ...    env=${A_ENV}
    Should Be Equal As Integers    ${source_bundle_result.rc}    0
    Log To Console    phase=source-bundle:complete
    ${source_run_cwd}=    Set Variable    ${FIXTURE_ROOT}${/}source-run
    Create Directory    ${source_run_cwd}
    Log To Console    phase=source-bundle-run:start
    ${source_run_result}=    Run Process
    ...    ${RCC}    robot    run-from-bundle    ${source_bundle}
    ...    --task    proof
    ...    cwd=${source_run_cwd}
    ...    env=${A_ENV}
    Log    ${source_run_result.stderr}
    Should Be Equal As Integers    ${source_run_result.rc}    0
    Log To Console    phase=source-bundle-run:complete

    ${archive}=    Set Variable    ${FIXTURE_ROOT}${/}artifact.rcca
    Log To Console    phase=artifact-export:start
    ${export_result}=    Run Process
    ...    ${RCC}    env    export
    ...    --artifact    ${artifact}
    ...    --provider    office
    ...    --output    ${archive}
    ...    env=${A_ENV}
    Should Be Equal As Integers    ${export_result.rc}    0
    Log To Console    phase=artifact-export:complete
    ${artifact_bundle}=    Set Variable    ${FIXTURE_ROOT}${/}source-artifact.py
    Log To Console    phase=artifact-bundle:start
    ${artifact_bundle_result}=    Run Process
    ...    ${RCC}    robot    bundle
    ...    --robot    ${ROBOT}
    ...    --artifact-archive    ${archive}
    ...    --output    ${artifact_bundle}
    ...    env=${A_ENV}
    Should Be Equal As Integers    ${artifact_bundle_result.rc}    0
    Log To Console    phase=artifact-bundle:complete
    ${artifact_run_cwd}=    Set Variable    ${FIXTURE_ROOT}${/}artifact-run
    Create Directory    ${artifact_run_cwd}
    Log To Console    phase=artifact-bundle-run:start
    ${artifact_run_result}=    Run Process
    ...    ${RCC}    robot    run-from-bundle    ${artifact_bundle}
    ...    --task    proof
    ...    cwd=${artifact_run_cwd}
    ...    env=${B_ENV}
    Log    ${artifact_run_result.stderr}
    Should Be Equal As Integers    ${artifact_run_result.rc}    0
    Log To Console    phase=artifact-bundle-run:complete

    ${platform_index}=    Set Variable    ${FIXTURE_ROOT}${/}platform-index.json
    ${index_path}=    Create Multi Platform Index    ${archive}    ${platform_index}
    ${indexed_bundle}=    Set Variable    ${FIXTURE_ROOT}${/}multi-platform.py
    Log To Console    phase=indexed-bundle:start
    ${indexed_bundle_result}=    Run Process
    ...    ${RCC}    robot    bundle
    ...    --robot    ${ROBOT}
    ...    --artifact-archive    ${archive}
    ...    --artifact-index    ${index_path}
    ...    --output    ${indexed_bundle}
    ...    env=${A_ENV}
    Should Be Equal As Integers    ${indexed_bundle_result.rc}    0
    Log To Console    phase=indexed-bundle:complete
    ${indexed_run_cwd}=    Set Variable    ${FIXTURE_ROOT}${/}indexed-run
    Create Directory    ${indexed_run_cwd}
    Log To Console    phase=indexed-bundle-run:start
    ${indexed_run_result}=    Run Process
    ...    ${RCC}    robot    run-from-bundle    ${indexed_bundle}
    ...    --task    proof
    ...    cwd=${indexed_run_cwd}
    ...    env=${B_ENV}
    Log    ${indexed_run_result.stderr}
    Should Be Equal As Integers    ${indexed_run_result.rc}    0
    Log To Console    phase=indexed-bundle-run:complete

    ${wrong_index}=    Set Variable    ${FIXTURE_ROOT}${/}wrong-platform-index.json
    ${wrong_index_path}=    Create Wrong Platform Index    ${archive}    ${wrong_index}
    ${wrong_bundle}=    Set Variable    ${FIXTURE_ROOT}${/}wrong-platform.py
    Log To Console    phase=wrong-platform-bundle:start
    ${wrong_bundle_result}=    Run Process
    ...    ${RCC}    robot    bundle
    ...    --robot    ${ROBOT}
    ...    --artifact-archive    ${archive}
    ...    --artifact-index    ${wrong_index_path}
    ...    --output    ${wrong_bundle}
    ...    env=${A_ENV}
    Should Be Equal As Integers    ${wrong_bundle_result.rc}    0
    Log To Console    phase=wrong-platform-bundle:complete
    ${wrong_run_cwd}=    Set Variable    ${FIXTURE_ROOT}${/}wrong-run
    Create Directory    ${wrong_run_cwd}
    Log To Console    phase=wrong-platform-run:start
    ${wrong_run_result}=    Run Process
    ...    ${RCC}    robot    run-from-bundle    ${wrong_bundle}
    ...    --task    proof
    ...    cwd=${wrong_run_cwd}
    ...    env=${B_ENV}
    Should Not Be Equal As Integers    ${wrong_run_result.rc}    0
    Should Contain    ${wrong_run_result.stderr}    no exact environment artifact for platform
    Log To Console    phase=wrong-platform-run:complete

    ${stopped}=    Terminate Process    environment-provider
    Should Be Equal As Integers    ${stopped.rc}    0
    Provider Should Be Unreachable    ${provider_url}

    Remove From Dictionary    ${B_ENV}    RCC_TEST_PROVIDER_AUTHORIZATION

    Log To Console    phase=provider-dead-test:start
    ${provider_test_result}=    Run Process
    ...    ${RCC}    provider    test    office    --json
    ...    env=${B_ENV}
    Should Not Be Equal As Integers    ${provider_test_result.rc}    0
    Should Not Contain    ${provider_test_result.stdout}    reachable
    Should Not Contain    ${provider_test_result.stdout}    compatible
    Should Not Contain    ${provider_test_result.stdout}    Bearer robot-test
    Should Contain    ${provider_test_result.stderr}    RCC_TEST_PROVIDER_AUTHORIZATION
    Should Not Contain    ${provider_test_result.stderr}    Bearer robot-test
    Log To Console    phase=provider-dead-test:complete

    Log To Console    phase=provider-dead-warm:start
    ${warm_result}=    Run Process
    ...    ${RCC}    env    acquire
    ...    --artifact    ${artifact}
    ...    --provider    office
    ...    --trust-carrier    ${TRUST_ROOT}
    ...    --trust-carrier-type    filesystem
    ...    --permissive-local
    ...    --json
    ...    env=${B_ENV}
    Should Be Equal As Integers    ${warm_result.rc}    0
    Should Not Contain    ${warm_result.stdout}    Bearer robot-test
    Should Not Contain    ${warm_result.stderr}    Bearer robot-test
    ${warm}=    Parse JSON    ${warm_result.stdout}
    Should Be Equal    ${warm}[artifactDigest]    ${artifact}
    Should Be Equal    ${warm}[materializationId]    ${cold}[materializationId]
    Should Be Equal    ${warm}[path]    ${cold}[path]
    Should Be Equal    ${warm}[cacheHit]    local-materialization
    Should Not Contain    ${warm_result.stderr}    micromamba phase
    Log To Console    phase=provider-dead-warm:complete
    Package Manager Caches Should Be Empty    ${B_HOME}
    Write Native Runtime Robot Evidence    ${RCC}    ${binary_version}    ${artifact}    ${cold}    ${executed}    ${warm}    ${proof}


*** Keywords ***
Prepare Environment Artifact Acceptance
    library.Log To Console    phase=suite-setup:start
    ${native_rcc}=    Native RCC Binary
    Set Suite Variable    ${RCC}    ${native_rcc}
    Set Suite Variable    ${FIXTURE_ROOT}    ${None}
    ${fixture}=    New Environment Artifact Fixture
    Set Suite Variable    ${FIXTURE_ROOT}      ${fixture}[root]
    Set Suite Variable    ${A_HOME}            ${fixture}[aHome]
    Set Suite Variable    ${B_HOME}            ${fixture}[bHome]
    Set Suite Variable    ${TRUST_ROOT}        ${fixture}[bHome]${/}artifacts${/}v1${/}trust
    Set Suite Variable    ${PROVIDER_ROOT}     ${fixture}[providerRoot]
    Set Suite Variable    ${SERVER_STDOUT}     ${fixture}[serverStdout]
    Set Suite Variable    ${SERVER_STDERR}     ${fixture}[serverStderr]
    Set Suite Variable    ${PROOF_FILE}        ${fixture}[proofFile]
    ${a_env}=    Environment Artifact Process Environment    ${A_HOME}    ${False}
    ${b_env}=    Environment Artifact Process Environment    ${B_HOME}    ${True}
    Set Suite Variable    ${A_ENV}    ${a_env}
    Set Suite Variable    ${B_ENV}    ${b_env}
    library.Log To Console    phase=suite-setup:complete

Clean Environment Artifact Acceptance
    Run Keyword And Ignore Error    Terminate All Processes    kill=${True}
    Run Keyword If    $FIXTURE_ROOT is not None    Remove Environment Artifact Fixture    ${FIXTURE_ROOT}
