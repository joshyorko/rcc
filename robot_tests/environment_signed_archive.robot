*** Settings ***
Library    environment_signed_archive/library.py


*** Variables ***
${RCC}    ${CURDIR}/../build/linux64/rcc


*** Test Cases ***
Signed Archive Acquires And Executes Offline With Deployment-Owned Trust
    ${evidence}=    Signed Offline Archive Vertical    ${RCC}
    Should Start With    ${evidence}[artifactDigest]    sha256:
    Should Not Be Empty    ${evidence}[archiveSha256]
    Should Not Be Empty    ${evidence}[binarySha256]
    Should Be Equal    ${evidence}[externalCarrier]    passed
    Should Contain    ${evidence}[negativeCases]    tampered
    Should Contain    ${evidence}[negativeCases]    missing
    Should Contain    ${evidence}[negativeCases]    missing-metadata-warm-home
    Should Contain    ${evidence}[negativeCases]    wrong-authority
    Should Contain    ${evidence}[negativeCases]    metadata-rollback
    Should Contain    ${evidence}[negativeCases]    revoked
