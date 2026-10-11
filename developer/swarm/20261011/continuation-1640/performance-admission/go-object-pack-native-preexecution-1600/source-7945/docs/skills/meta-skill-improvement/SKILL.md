---
name: meta-skill-improvement
description: Use when turning verified RCC development discoveries into durable repository guidance without documentation churn.
---

# Meta Skill Improvement

Use this skill only when evidence indicates repository guidance may need to change. The goal is to turn verified, reusable findings into better guidance without making documentation churn part of every task.

## When to Use

- Evidence reveals stale, incomplete, or missing repository guidance
- The task directly changes agent guidance
- Closure produces verified durable learning that belongs in this repository
- A command, platform behavior, test boundary, or recovery path was surprising
- Existing guidance caused wasted work or contradicted verified behavior

## When Not to Use

- Trivial formatting or typo-only work
- Read-only work that produced no reusable finding
- A request whose only output is already durable documentation
- Routine non-trivial work that can close with a no-change documentation receipt

## Guidance Review Loop

1. Read `AGENTS.md` and route through [`docs/skills/README.md`](../README.md).
2. Identify the guidance that should already cover the task.
3. Treat current source, tests, command output, and runtime behavior as evidence. Treat old docs, upstream history, and prior agent claims as leads until verified.
4. Note suspected gaps, but do not edit guidance before evidence exists.

## Evidence Threshold

Update guidance only when the learning is:

- verified by code, tests, commands, runtime output, or authoritative repository history;
- reusable beyond the current issue;
- specific enough to change a future action or decision; and
- best owned by this repository.

Do not record speculation, temporary issue state, PR or commit narratives, dates, personal notes, isolated failures, or obvious restatements of code.

## End-Of-Work Loop

1. Compare the verified result with the guidance used at the start.
2. Correct incomplete or stale guidance in its canonical file.
3. Prefer replacing a bad rule over appending an exception.
4. Add a new skill only when the knowledge forms a distinct recurring workflow.
5. Validate links, frontmatter, commands, and formatting.
6. If no durable learning emerged, leave the documentation unchanged and use a no-change receipt.

Open gaps belong in GitHub issues or the task handoff, not in skills. A skill describes how to operate now.

## Documentation Receipt

Close non-trivial work with:

```text
Documentation receipt
- Canonical guidance: <file changed, exact proposed delta, or "no change">
- Durable learning: <reusable fact or "none">
- Evidence: <code, test, command, runtime output, or history>
- Stale guidance removed: <what was replaced or "none">
- Remaining uncertainty: <specific unknown or "none">
```

Read-only work proposes an exact documentation delta rather than mutating files. Mutating work applies an in-scope update when the evidence threshold is met. The receipt is mandatory for non-trivial work even when this skill was not loaded. A no-change receipt is valid and preferred over cosmetic churn.

## Red Flags

- Guidance describes planned behavior as implemented.
- A dated incident or current issue number is becoming a permanent procedure.
- A new skill duplicates an existing guide.
- A receipt says "verified" without naming evidence.
- Documentation changes merely to satisfy the loop.

## Verification

- `python scripts/validate_agent_docs.py`
- `git diff --check`
- Confirm the skill index lists every repository-local skill.
- Confirm the receipt separates verified facts from remaining uncertainty.
