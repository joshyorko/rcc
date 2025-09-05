*** Settings ***
Resource  resources.robot

*** Test cases ***

Has required changes in commit based on development process.
  Step       git show --stat
  Must Have  docs/changelog.md
  Must Have  common/version.go

