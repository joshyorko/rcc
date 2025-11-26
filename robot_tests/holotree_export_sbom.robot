*** Settings ***
Library             OperatingSystem
Library             supporting.py
Resource            resources.robot
Suite Setup         Holotree Export SBOM Setup
Suite Teardown      Holotree Export SBOM Teardown

*** Keywords ***
Holotree Export SBOM Setup
    Remove Directory    tmp/exportsbom    True
    Prepare Robocorp Home    tmp/exportsbom/home
    Fire And Forget    build/rcc ht init --revoke

Holotree Export SBOM Teardown
    Prepare Robocorp Home    tmp/robocorp
    Remove Directory    tmp/exportsbom    True

*** Test cases ***

Goal: Build two catalogs for export SBOM test
    ${fingerprint_one}=    Capture Flat Output    build/rcc ht hash --silent robot_tests/python375.yaml
    ${fingerprint_two}=    Capture Flat Output    build/rcc ht hash --silent robot_tests/python3913.yaml
    Set Suite Variable    ${fingerprint_one}
    Set Suite Variable    ${fingerprint_two}

    Step    build/rcc ht vars -s exportone --controller citests -r robot_tests/python375.yaml
    Use STDERR
    Must Have    OK.

    Step    build/rcc ht vars -s exporttwo --controller citests -r robot_tests/python3913.yaml
    Use STDERR
    Must Have    OK.

Goal: Export both catalogs with SBOM included
    Step    build/rcc ht export --include-sbom -z tmp/exportsbom/hololib.zip ${fingerprint_one} ${fingerprint_two} --controller citests
    Use STDERR
    Must Have    OK.
    Must Exist   tmp/exportsbom/hololib.zip
    Must Exist   tmp/exportsbom/hololib.sbom.json

Goal: SBOM contains data from both catalogs
    Step    cat tmp/exportsbom/hololib.sbom.json
    Must Have   serialNumber
    Must Have   3.7.5
    Must Have   3.9.13
