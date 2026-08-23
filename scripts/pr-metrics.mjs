#!/usr/bin/env node

import fs from "node:fs";

const eventPath = process.env.GITHUB_EVENT_PATH;
const event = eventPath && fs.existsSync(eventPath)
  ? JSON.parse(fs.readFileSync(eventPath, "utf8"))
  : {};
const pullRequest = event.pull_request ?? {};
const requestedReviewers = event.pull_request?.requested_reviewers ?? [];

const metrics = {
  schema: 1,
  generated_at: new Date().toISOString(),
  repository: process.env.GITHUB_REPOSITORY ?? null,
  pull_request: pullRequest.number ?? null,
  action: event.action ?? null,
  draft: pullRequest.draft ?? null,
  changed_files: pullRequest.changed_files ?? null,
  additions: pullRequest.additions ?? null,
  deletions: pullRequest.deletions ?? null,
  requested_reviewer_count: Array.isArray(requestedReviewers) ? requestedReviewers.length : null,
};

process.stdout.write(`${JSON.stringify(metrics, null, 2)}\n`);
