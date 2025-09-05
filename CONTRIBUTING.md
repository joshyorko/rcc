# How to contribute

## Ideas for regular sources of contribution.

1. Is there a need to upgrade the Go language version?
    - see releases from https://go.dev/doc/devel/release
    - do not go to bleeding edge (unless you really have to do so)
    - do not stay too far away behind development
    - when needed, update .github/workflows/rcc.yaml
2. Is there new Micromamba available?
    - see releases from https://anaconda.org/conda-forge/micromamba
    - only use stable versions
3. Where is uv going?
    - important: uv has to come from conda-forge because of enterprise
      firewalls/proxies
    - https://anaconda.org/conda-forge/uv to find out what is available
      on conda-forge
    - https://github.com/astral-sh/uv to see issues and development
4. What is pip doing?
    - check news from https://pip.pypa.io/en/stable/news/

## Additional sources for contribution ideas.

- improve documentation under docs/ directory
- improve acceptance tests written in Robot Framework (inside `robot_tests`
  directory)
    - currently these work fully on Linux only, so if you have Mac or Windows
      and can make these work there, that would also be a nice contribution

## How to proceed with improvements/contributions?

- create an issue in the rcc repository at
  https://github.com/robocorp/rcc/issues
- on that issue, discuss the solution you are proposing
- implementation can proceed only when the solution is clear and accepted
- the solution should be made so that it works on Mac, Windows, and Linux
- when developing, remember to run both unit tests and acceptance tests
  (Robot Framework tests) on your own machine first
- once you have written the code for that solution, create a pull request

## How does rcc build work?

- a good source to understand the build is to see the CI pipeline,
  .github/workflows/rcc.yaml
- also read docs/BUILD.md for tooling requirements and commands to run
