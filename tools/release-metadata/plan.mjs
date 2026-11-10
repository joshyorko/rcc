import { createHash } from 'node:crypto';
import { createRequire } from 'node:module';
const require = createRequire(import.meta.url);
require('release-please');
const { Go } = require('release-please/build/src/strategies/go.js');
const { Generic } = require('release-please/build/src/updaters/generic.js');
const { Version } = require('release-please/build/src/version.js');
const { TagName } = require('release-please/build/src/util/tag-name.js');
const { AlwaysBumpPatch } = require('release-please/build/src/versioning-strategies/always-bump-patch.js');
const { parseConventionalCommits } = require('release-please/build/src/commit.js');
export const BRANCH = 'release-please--branches--main';
export const STATE = '.release-metadata.json';
export const VERSION = 'common/version.go';
export const CHANGELOG = 'docs/changelog.md';
export const CONTROLLER = 'rcc-release-metadata/v1';
export const digest = (value) => createHash('sha256').update(value).digest('hex');
export const check = (value, message) => { if (!value) throw new Error(message); };
export const isSha = (value) => typeof value === 'string' && /^[a-f0-9]{40}$/.test(value);
const semver = /^v(\d+)\.(\d+)\.(\d+)$/;
const versionPattern = /\bVersion = `(v\d+\.\d+\.\d+)`/g;
const start = '<!-- rcc-release-notes:start -->';
const end = '<!-- rcc-release-notes:end -->';
export function sourceVersion(source) {
  const matches = [...source.matchAll(versionPattern)];
  check(matches.length === 1, 'Expected exactly one grouped Go backtick Version');
  return matches[0][1];
}
export function filesDigest(files) {
  return digest(JSON.stringify([files[VERSION], files[CHANGELOG]]));
}
function splitChangelog(text) {
  const pattern = /(?:^<a name="v\d+"><\/a>\n)?^## v\d+\.\d+\.\d+[^\n]*$/gm;
  const matches = [...text.matchAll(pattern)];
  check(matches.length > 0, 'Changelog has no RCC release headings');
  const blocks = matches.map((m, i) => ({
    version: m[0].match(/## (v\d+\.\d+\.\d+)/)[1],
    text: text.slice(m.index, matches[i + 1]?.index ?? text.length),
  }));
  check(new Set(blocks.map((b) => b.version)).size === blocks.length, 'Duplicate changelog version');
  return { prefix: text.slice(0, matches[0].index), blocks };
}
function stateFrom(text) {
  if (!text) return null;
  const state = JSON.parse(text);
  check(state.controller === CONTROLLER && state.schema === 1, 'Unrecognized metadata controller state');
  check(semver.test(state.candidate) && semver.test(state.publishedTag) && isSha(state.publishedSha) && isSha(state.baseSha), 'Malformed metadata state');
  check(/^[a-f0-9]{64}$/.test(state.filesDigest), 'Malformed generated file digest');
  return state;
}
export function verifyGeneratedBranch(files) {
  const state = stateFrom(files[STATE]);
  check(state && state.filesDigest === filesDigest(files), 'Release branch contains reviewer edits; preserve and review them before refreshing');
  return state;
}
export async function plan(snapshot) {
  check(snapshot.repository === 'joshyorko/rcc', 'Only the RCC repository is supported');
  check(isSha(snapshot.mainSha) && isSha(snapshot.published.sha), 'Invalid source identity');
  check(snapshot.ancestryVerified === true && snapshot.historyComplete === true, 'Incomplete or unrelated published history');
  check(snapshot.published.draft === false && snapshot.published.prerelease === false, 'Published baseline must be a stable release');
  const published = snapshot.published.tag;
  const match = published.match(semver);
  check(match, 'Published baseline must have a stable v-prefixed version');
  const next = `v${match[1]}.${match[2]}.${Number(match[3]) + 1}`;
  const files = snapshot.files;
  const current = sourceVersion(files[VERSION]);
  check(current === published || current === next, 'Source version is neither published nor its next patch');
  const { prefix, blocks } = splitChangelog(files[CHANGELOG]);
  check(blocks[0].version === current, 'Source version and first changelog heading disagree');
  check(!snapshot.candidateTagExists, 'Candidate tag already exists without a matching published release');
  const state = stateFrom(files[STATE]);
  if (state) {
    check(state.candidate === current, 'Metadata state disagrees with source version');
    if (current !== published) {
      check(state.publishedTag === published && state.publishedSha === snapshot.published.sha, 'Pending candidate has a different published baseline');
      return { status: 'PENDING_PUBLICATION', version: current, sourceSha: snapshot.mainSha, publication: 'OWNER_APPROVAL_REQUIRED' };
    }
    // A previous metadata candidate is now published; the actual tag is the baseline.
    check(state.candidate === published, 'Publication did not advance to the candidate');
  }
  check(!snapshot.candidateTagExists, 'Candidate tag already exists without a matching published release');
  check(Array.isArray(snapshot.commits) && snapshot.commits.every((c) => isSha(c.sha) && typeof c.message === 'string'), 'Malformed merged history');
  check(new Set(snapshot.commits.map((c) => c.sha)).size === snapshot.commits.length, 'Duplicate history commits');
  if (!snapshot.commits.length) return { status: 'NO_CHANGES' };
  check(/^\d{4}-\d\d-\d\dT/.test(snapshot.headDate), 'Missing source commit date');
  const strategy = new Go({
    github: { repository: { owner: 'joshyorko', repo: 'rcc' } }, targetBranch: 'main',
    includeComponentInTag: false, skipChangelog: true,
    versioningStrategy: new AlwaysBumpPatch(),
    changelogSections: ['feat', 'fix', 'perf', 'deps', 'test', 'chore', 'docs', 'refactor', 'build', 'ci', 'revert', 'security'].map((type) => ({ type, section: type, hidden: false })),
  });
  const normalized = snapshot.commits.map((commit) => {
    if (parseConventionalCommits([commit]).length) return commit;
    return { ...commit, message: `chore: ${commit.message.split('\n')[0]}` };
  });
  const commits = parseConventionalCommits(normalized);
  const latest = { sha: snapshot.published.sha, tag: TagName.parse(published), notes: '' };
  const candidate = await strategy.buildReleasePullRequest(commits, latest);
  check(candidate && `v${candidate.version}` === next, 'Release Please did not select the next patch');
  // Release Please owns category/commit-linked prose, never publication or state.
  const notes = await strategy.buildReleaseNotes(commits, Version.parse(next.slice(1)), TagName.parse(next), latest, snapshot.commits);
  const generated = notes.replace(/^##[^\n]*\n/, '').trim();
  check(generated, 'Release Please produced no source-backed notes');
  let authored = current === next ? blocks.shift().text : '';
  const history = blocks.map((b) => b.text).join('');
  let headingDate = snapshot.headDate.slice(0, 10).split('-').reverse().join('.');
  if (authored) {
    const date = authored.match(/^## v[^\n]+\(date: ([\d.]+)\)/m);
    if (date) headingDate = date[1];
    authored = authored.replace(/^<a name="v\d+"><\/a>\n/, '').replace(/^##[^\n]*\n/, '');
    const starts = authored.split(start).length - 1;
    const ends = authored.split(end).length - 1;
    check(starts === ends && starts <= 1, 'Malformed generated notes markers');
    if (starts) {
      check(authored.indexOf(start) < authored.indexOf(end), 'Generated notes markers are reversed');
      authored = authored.slice(0, authored.indexOf(start)) + authored.slice(authored.indexOf(end) + end.length);
    }
    authored = authored.trim();
  }
  const goMarker = ' // x-release-please-version';
  const marked = files[VERSION].includes('x-release-please-version') ? files[VERSION] : files[VERSION].replace(versionPattern, (m) => m + goMarker);
  const versionFile = new Generic({ version: Version.parse(next.slice(1)) }).updateContent(marked);
  check(sourceVersion(versionFile) === next, 'Generic updater failed the backtick constant contract');
  const result = {
    [VERSION]: versionFile,
    [CHANGELOG]: `${prefix}<a name="${next.replaceAll('.', '')}"></a>\n## ${next} (date: ${headingDate})\n\n${authored ? authored + '\n\n' : ''}${start}\n${generated}\n${end}\n\n${history}`,
  };
  result[STATE] = JSON.stringify({ schema: 1, controller: CONTROLLER, candidate: next,
    publishedTag: published, publishedSha: snapshot.published.sha, baseSha: snapshot.mainSha,
    filesDigest: filesDigest(result), includedCommits: snapshot.commits.map((c) => c.sha),
  }, null, 2) + '\n';
  return { status: 'PROPOSE', version: next, sourceSha: snapshot.mainSha, branch: BRANCH,
    title: `chore(release): prepare RCC ${next}`, files: result, publication: 'OWNER_APPROVAL_REQUIRED' };
}
