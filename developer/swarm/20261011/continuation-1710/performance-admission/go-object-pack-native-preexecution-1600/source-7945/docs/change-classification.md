# Change classification

Classify a pull request by the highest-risk behavior it changes. The author
records the class and evidence; maintainers may raise it during review.

| Class | Typical changes | Minimum evidence |
|---|---|---|
| Maintenance | Documentation, tests, formatting, repository metadata | Focused checks and `git diff --check` |
| Behavioral | RCC command behavior, templates, workflows, environment or cache logic | Focused Go tests, affected package tests, and applicable Robot coverage |
| Sensitive | Releases, dependencies, security boundaries, public APIs, credentials, remote endpoints, cross-platform behavior | Behavioral evidence plus explicit compatibility, security, platform, and rollback review |

Generated-asset changes inherit the class of their source inputs. A change is
not lower risk because its diff is small, and an unavailable platform gate must
be reported rather than inferred green.
