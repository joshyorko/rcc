*** Settings ***
Resource  resources.robot
Library   String

*** Test cases ***

Release changelog is tracked and documents the built RCC version.
  Step       build/rcc --version
  ${version}=  Strip String  ${robot_stdout}
  ${escaped_version}=  Regexp Escape  ${version}
  Step       git show HEAD:docs/changelog.md
  Use Stdout
  Should Match Regexp  ${robot_output}  (?m)^## ${escaped_version}(?: .*)?$
