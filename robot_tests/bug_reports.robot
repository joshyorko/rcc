*** Settings ***
Resource  resources.robot

*** Test cases ***

Goal: Verify initial call with do-not-track (GH#7)
  [Setup]     Remove config    tmp/bug_7.yaml
  Wont Exist  tmp/bug_7.yaml

  Step        build/rcc configure identity --controller citests --do-not-track --config tmp/bug_7.yaml
  Must Have   anonymous health tracking is: disabled

Bug in virtual holotree with gzipped files
  Step        build/rcc holotree blueprint --controller citests robot_tests/spellbug/conda.yaml
  Use STDERR
  Must Have   Blueprint "8b2083d262262cbd" is available: false

  Step        build/rcc run --liveonly --controller citests --robot robot_tests/spellbug/robot.yaml
  Use STDOUT
  Must Have   Bug fixed!

  Step        build/rcc holotree blueprint --controller citests robot_tests/spellbug/conda.yaml
  Use STDERR
  Must Have   Blueprint "8b2083d262262cbd" is available: false

  Step        build/rcc run --controller citests --robot robot_tests/spellbug/robot.yaml
  Use STDOUT
  Must Have   Bug fixed!

  Step        build/rcc holotree blueprint --controller citests robot_tests/spellbug/conda.yaml
  Use STDERR
  Must Have   Blueprint "8b2083d262262cbd" is available: true

Goal: Verify rcc task script command works (GH#32)
  Step        build/rcc task script --controller citests --robot robot_tests/spellbug/robot.yaml -- pip list
  Use STDOUT
  Must Have   pyspellchecker
  Must Have   0.6.2


*** Keywords ***

Remove Config
  [Arguments]  ${filename}
  Remove File  ${filename}

