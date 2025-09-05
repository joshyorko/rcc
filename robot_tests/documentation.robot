*** Settings ***
Resource  resources.robot
Test template  Verify documentation

*** Test cases ***                  DOCUMENTATION      EXPECT

Changelog in documentation          changelog          troubleshooting documentation added as
Features in documentation           features           Incomplete list of rcc features
LICENSE in documentation            license            TERMS AND CONDITIONS FOR USE
Profiles in documentation           profiles           Profile is way to capture
Recipes in documentation            recipes            Tips, tricks, and recipies
Troubleshooting in documentation    troubleshooting    Troubleshooting guidelines and known solutions
Tutorial in documentation           tutorial           Welcome to RCC tutorial
Use-cases in documentation          usecases           Incomplete list of rcc use cases

*** Keywords ***

Verify documentation
  [Arguments]  ${document}  ${expected}
  Step        build/rcc man ${document} --controller citests
  Must Have   ${expected}
