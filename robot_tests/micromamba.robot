*** Settings ***
Resource          resources.robot
Library           Process
Suite Setup       Prepare Micromamba Home
Suite Teardown    Restore Micromamba Home
Test Timeout      10 minutes

*** Keywords ***
Prepare Micromamba Home
    ${original}=    Get Environment Variable    ROBOCORP_HOME    ${EMPTY}
    Set Suite Variable    ${original_home}    ${original}
    ${home}=    Evaluate    tempfile.mkdtemp(prefix="rcc-micromamba-")    modules=tempfile
    Set Suite Variable    ${micromamba_home}    ${home}
    Prepare Robocorp Home    ${home}
    ${is_windows}=    Is Windows
    IF    ${is_windows}
        ${executable}=    Set Variable    ${home}/micromamba/v2.10.0/micromamba.exe
    ELSE
        ${executable}=    Set Variable    ${home}/micromamba/v2.10.0/micromamba
    END
    Set Suite Variable    ${micromamba_executable}    ${executable}

Restore Micromamba Home
    Set Environment Variable    ROBOCORP_HOME    ${original_home}
    Remove Directory    ${micromamba_home}    recursive=True

*** Test Cases ***
Cold Conda Build Includes Pip Packages And Activation
    Step    build/rcc run -r robot_tests/micromamba/robot.yaml -t probe --space micromamba-upgrade --controller citests
    Must Have    MICROMAMBA_ACCEPTANCE_OK
    Use Stderr
    Must Have    micromamba v2.10.0
    Wont Have    Golden EE failure
    Wont Have    Activation failure
    ${result}=    Run Process    ${micromamba_executable}    --version    timeout=20s    on_timeout=kill
    Should Be Equal As Integers    ${result.rc}    0
    Should Be Equal As Strings    ${result.stdout.strip()}    2.10.0

List Json Contains Packages And Produces Golden Inventory
    Step    build/rcc ht vars --space micromamba-upgrade --controller citests robot_tests/uv_system_flag/conda.yaml
    ${prefix}=    Extract Env Value    ${robot_stdout}    CONDA_PREFIX
    # Robot 6.1 waits before draining PIPE output; verbose JSON needs file-backed capture.
    ${result}=    Run Process    ${micromamba_executable}    list    --json    --no-rc    -p    ${prefix}    stdout=${micromamba_home}/package-list.json    stderr=${micromamba_home}/package-list.stderr    timeout=30s    on_timeout=kill
    IF    ${result.rc} != 0
        Log To Console    ${result.stderr}
        Log To Console    ${result.stdout}
    END
    Should Be Equal As Integers    ${result.rc}    0
    ${listing}=    Evaluate    json.loads($result.stdout)    modules=json
    ${packages}=    Set Variable    ${listing}[packages]
    Should Be True    isinstance($packages, list)
    ${names}=    Evaluate    [package["name"] for package in $packages]
    Should Contain    ${names}    python
    ${inventory}=    Get File    ${prefix}/golden-ee.yaml
    Should Contain    ${inventory}    name: requests

Warm Restore Executes With Offline Configuration
    # Offline switches and dead proxies exercise restored execution without a
    # dependency fetch; network-namespace isolation remains a separate Linux gate.
    Step    build/rcc run -r robot_tests/micromamba/robot.yaml -t probe --space micromamba-upgrade --controller citests
    Must Have    MICROMAMBA_ACCEPTANCE_OK
    ${saved_network}=    Evaluate    {key: os.environ[key] for key in ("CONDA_OFFLINE", "MAMBA_OFFLINE", "HTTP_PROXY", "HTTPS_PROXY") if key in os.environ}    modules=os
    Set Environment Variable    CONDA_OFFLINE    true
    Set Environment Variable    MAMBA_OFFLINE    true
    Set Environment Variable    HTTP_PROXY    http://127.0.0.1:9
    Set Environment Variable    HTTPS_PROXY    http://127.0.0.1:9
    TRY
        Step    build/rcc run -r robot_tests/micromamba/robot.yaml -t probe --space micromamba-upgrade --controller citests
        Must Have    MICROMAMBA_ACCEPTANCE_OK
        Use Stderr
        Wont Have    Running micromamba phase
    FINALLY
        Remove Environment Variable    CONDA_OFFLINE
        Remove Environment Variable    MAMBA_OFFLINE
        Remove Environment Variable    HTTP_PROXY
        Remove Environment Variable    HTTPS_PROXY
        Evaluate    os.environ.update($saved_network)    modules=os
    END

Task Exit And Error Output Propagate
    Step    build/rcc run -r robot_tests/micromamba/robot.yaml -t fail --space micromamba-upgrade --controller citests    10
    Use Stderr
    Must Have    MICROMAMBA_EXPECTED_ERROR
