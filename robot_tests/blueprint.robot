*** Settings ***
Library     OperatingSystem
Library     supporting.py


*** Test Cases ***
Goal: Check that blueprint command works correctly
    Run And Check Expected Output
    ...    build/rcc holotree blueprint --json --controller citests robot_tests/bare_action/package.yaml
    ...
    ...    robot_tests/expected/blueprint/blueprint_json.txt

    Run And Check Expected Output
    ...    build/rcc holotree blueprint --controller citests robot_tests/bare_action/package.yaml
    ...
    ...    robot_tests/expected/blueprint/blueprint_txt.txt
    ...    use_stream=stderr

Goal: Check that blueprint command works correctly with devdeps
    Run And Check Expected Output
    ...    build/rcc holotree blueprint --json --devdeps --controller citests robot_tests/bare_action/package.yaml
    ...
    ...    robot_tests/expected/blueprint/blueprint_devdeps_json.txt

    Run And Check Expected Output
    ...    build/rcc holotree blueprint --devdeps --controller citests robot_tests/bare_action/package.yaml
    ...
    ...    robot_tests/expected/blueprint/blueprint_devdeps_txt.txt
    ...    use_stream=stderr
