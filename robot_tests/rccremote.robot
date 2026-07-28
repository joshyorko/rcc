*** Settings ***
Resource    resources.robot


*** Test Cases ***
Goal: Show rccremote version information.
    Step    build/rccremote -version
    Must Have    v18.

Goal: Reject an unknown rccremote option.
    Step    build/rccremote -invalid-option    2
    Use Stderr
    Must Have    flag provided but not defined
