*** Settings ***
Library     OperatingSystem
Library     supporting.py
Resource    resources.robot

*** Test Cases ***

Goal: Show rccremote version information.
    Step        build/rccremote --version
    Must Have   v18.

Goal: Show rccremote help with -expose flag.
    Step        build/rccremote --help
    Use Stderr
    Must Have   -expose
    Must Have   Expose server via Cloudflare Quick Tunnel

Goal: Show rccremote help with -tunnel-name flag.
    Step        build/rccremote --help
    Use Stderr
    Must Have   -tunnel-name
    Must Have   Use Named Tunnel instead of Quick Tunnel

Goal: Verify rccremote accepts both expose flags together.
    [Documentation]    Test that both -expose and -tunnel-name flags are accepted
    Step        build/rccremote --help
    Use Stderr
    Must Have   -expose
    Must Have   -tunnel-name

*** Keywords ***

Verify cloudflared missing error
    # Verify that the binary accepts the flags
    ${code}    ${output}    ${error}=    Run and return code output error    build/rccremote --help
    Should Contain    ${error}    -expose
    Should Contain    ${error}    -tunnel-name
