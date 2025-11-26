*** Settings ***
Library             OperatingSystem
Library             supporting.py
Resource            resources.robot
Suite Setup         SBOM test setup

*** Keywords ***
SBOM test setup
  Fire And Forget   build/rcc ht delete 4e67cd8

*** Test cases ***

Goal: Verify SBOM command help is available
  Step        build/rcc holotree sbom --help
  Must Have   Generate Software Bill of Materials
  Must Have   cyclonedx
  Must Have   spdx
  Must Have   --format
  Must Have   --output
  Must Have   --registry
  Must Have   --robot
  Must Have   --json

Goal: List available catalogs when no catalog specified
  Step        build/rcc holotree sbom --controller citests
  Use STDERR
  Must Have   OK.

Goal: Initialize new extended robot into tmp/sbomtest folder.
  Step        build/rcc robot init --controller citests -t extended -d tmp/sbomtest -f
  Use STDERR
  Must Have   OK.

Goal: Build environment for sbom test robot
  Step        build/rcc ht vars -s sbomtest --controller citests -r tmp/sbomtest/robot.yaml
  Must Have   RCC_ENVIRONMENT_HASH=
  Must Have   RCC_INSTALLATION_ID=
  Use STDERR
  Must Have   Progress: 01/15
  Must Have   Progress: 15/15

Goal: Get blueprint hash for sbom test
  ${output}=    Capture Flat Output    build/rcc ht hash --silent tmp/sbomtest/conda.yaml
  Set Suite Variable    ${fingerprint}    ${output}

Goal: Generate CycloneDX SBOM from catalog hash
  Step        build/rcc holotree sbom ${fingerprint} --format cyclonedx --controller citests
  Must Have   bomFormat
  Must Have   CycloneDX
  Must Have   specVersion
  Must Have   components

Goal: Generate SPDX SBOM from catalog hash
  Step        build/rcc holotree sbom ${fingerprint} --format spdx --controller citests
  Must Have   spdxVersion
  Must Have   SPDX-2.3
  Must Have   packages
  Must Have   SPDXRef-DOCUMENT

Goal: Generate SBOM with JSON wrapper format
  Step        build/rcc holotree sbom ${fingerprint} --format cyclonedx --json --controller citests
  Must Have   blueprintHash
  Must Have   format
  Must Have   platform
  Must Have   sbom
  Must Have   bomFormat

Goal: Generate SBOM and write to file
  Step        build/rcc holotree sbom ${fingerprint} --format cyclonedx --output tmp/sbom_test.json --controller citests
  Use STDERR
  Must Have   SBOM written to
  Must Have   OK.
  Must Exist  tmp/sbom_test.json

Goal: Generate SBOM from robot.yaml directly
  Step        build/rcc holotree sbom --robot tmp/sbomtest/robot.yaml --format cyclonedx --controller citests
  Must Have   bomFormat
  Must Have   CycloneDX
  Must Have   components

Goal: Invalid format returns error
  Step        build/rcc holotree sbom ${fingerprint} --format invalid --controller citests    1
  Use STDERR
  Must Have   unsupported SBOM format

Goal: Clean up sbom test space
  Step        build/rcc ht delete 4e67cd8
  Use STDERR
  Must Have   OK
  Remove Directory  tmp/sbomtest  True
  Remove File       tmp/sbom_test.json
