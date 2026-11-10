import test from 'node:test';
import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';
import { readFile } from 'node:fs/promises';
import { plan, verifyGeneratedBranch, BRANCH, STATE, VERSION, CHANGELOG } from './plan.mjs';
import { GitHub, reconcile } from './github.mjs';
const sha = (digit) => digit.repeat(40);
const published = { tag: 'v18.19.10', sha: sha('a'), draft: false, prerelease: false };
const historical = JSON.parse(await readFile(new URL('./fixtures/source-only.json', import.meta.url), 'utf8'));
const source = historical.versionGo;
const history = historical.changelog;
function fixture() { return {
  repository: 'joshyorko/rcc', mainSha: sha('b'), published: { ...published }, ancestryVerified: true, historyComplete: true,
  headDate: '2026-11-10T12:00:00Z', candidateTagExists: false,
  files: { [VERSION]: source, [CHANGELOG]: history, [STATE]: null },
  commits: [{ sha: sha('c'), message: 'feat(deps): upgrade embedded micromamba to 2.10.0 (#280)' }, { sha: sha('d'), message: 'test(gc): preserve ready environments (#282)' }],
}; }
test('actual Release Please migrates grouped backticks, preserves authored notes and complete historical tail exactly once', async () => {
  const snapshot = fixture();
  const proposal = await plan(snapshot);
  assert.equal(proposal.version, 'v18.19.11');
  assert.match(proposal.files[VERSION], /Version = `v18\.19\.11` \/\/ x-release-please-version/);
  assert.match(proposal.files[CHANGELOG], /## v18\.19\.11 \(date: 10\.11\.2026\)/);
  assert.equal(proposal.files[CHANGELOG].split('## v18.19.11').length, 2);
  assert(proposal.files[CHANGELOG].includes(history.slice(history.indexOf('<a name="v181910">'))));
  assert(proposal.files[CHANGELOG].includes('pin embedded micromamba to 2.10.0'));
  assert.match(proposal.files[CHANGELOG], /\/commit\/ccccccc/);
  assert.deepEqual(await plan(snapshot), proposal);
  const refreshed = structuredClone(snapshot); refreshed.files = { ...proposal.files, [STATE]: null };
  assert.deepEqual((await plan(refreshed)).files, proposal.files);
  assert.equal(verifyGeneratedBranch(proposal.files).candidate, 'v18.19.11');
});
test('source-only migration also preserves an already dated candidate heading (#285)', async () => {
  const f = fixture(); f.files[CHANGELOG] = history.replace('## v18.19.11\n', '## v18.19.11 (date: 09.11.2026)\n');
  assert.match((await plan(f)).files[CHANGELOG], /date: 09\.11\.2026/);
});
test('merged metadata holds until publication; then selects .12 from actual published tag', async () => {
  const f = fixture(); const candidate = await plan(f); f.files = candidate.files;
  assert.equal((await plan(f)).status, 'PENDING_PUBLICATION');
  f.commits.push({ sha: sha('e'), message: 'feat!: more work after metadata merge' });
  assert.equal((await plan(f)).status, 'PENDING_PUBLICATION');
  f.published = { ...published, tag: 'v18.19.11', sha: sha('f') };
  const next = await plan(f);
  assert.equal(next.version, 'v18.19.12');
  assert(next.files[CHANGELOG].endsWith(candidate.files[CHANGELOG].slice(candidate.files[CHANGELOG].indexOf('<a name="v181911">'))));
});
test('fresh published source gets one next-patch heading and no duplicate previous heading', async () => {
  const f = fixture(); f.files[VERSION] = source.replace('v18.19.11', 'v18.19.10');
  f.files[CHANGELOG] = '# rcc change log\n## Unreleased\n\n' + history.slice(history.indexOf('<a name="v181910">'));
  const p = await plan(f); assert.equal(p.version, 'v18.19.11');
  assert.equal(p.files[CHANGELOG].split('## v18.19.10').length, 2);
});
test('patch cadence includes breaking feat, chore(deps), tests, and nonconventional merged commits', async () => {
  for (const message of ['feat!: new major-looking feature', 'chore(deps): upgrade module', 'test: add regression', 'Merge reviewed safety fix']) {
    const f = fixture(); f.commits = [{ sha: sha('c'), message }];
    assert.equal((await plan(f)).version, 'v18.19.11', message);
  }
  const f = fixture(); f.commits = []; assert.equal((await plan(f)).status, 'NO_CHANGES');
});
test('malformed, stale, mismatched and collision snapshots deny', async () => {
  const mutations = [
    (f) => { f.repository = 'other/repo'; }, (f) => { f.mainSha = 'main'; },
    (f) => { f.published.draft = true; }, (f) => { f.published.prerelease = true; },
    (f) => { f.published.tag = 'v18.19.9'; }, (f) => { f.published.sha = 'main'; },
    (f) => { f.ancestryVerified = false; }, (f) => { f.historyComplete = false; },
    (f) => { f.candidateTagExists = true; }, (f) => { f.commits.push(f.commits[0]); },
    (f) => { f.files[CHANGELOG] += '\n## v18.19.11\n'; },
    (f) => { f.files[VERSION] += '\nVersion = `v18.19.11`\n'; },
    (f) => { f.files[STATE] = '{}'; },
    (f) => { f.files[CHANGELOG] = f.files[CHANGELOG].replace('## v18.19.11', '## v18.19.12'); },
  ];
  for (const mutate of mutations) { const f = fixture(); mutate(f); await assert.rejects(plan(f)); }
});
test('merged candidate cannot silently bind to a moved published tag', async () => {
  const f = fixture(); f.files = (await plan(f)).files; f.published.sha = sha('f');
  await assert.rejects(plan(f), /different published baseline/);
});
test('reviewer edits are preserved by denying refresh, never overwritten', async () => {
  const p = await plan(fixture()); p.files[CHANGELOG] += '\nReviewed safety note\n';
  assert.throws(() => verifyGeneratedBranch(p.files), /reviewer edits/);
});

// End-to-end lifecycle: real planning library through the production REST client.
// Transport models GitHub refs, objects and PRs and records EVERY mutation.
class Server {
  constructor() { this.f = fixture(); this.branch = null; this.prs = []; this.writes = []; this.objects = new Map(); this.next = 1; this.race = false; }
  async fetch(url, options) {
    const path = new URL(url).pathname.replace('/repos/joshyorko/rcc/', '');
    const method = options.method; const body = options.body ? JSON.parse(options.body) : null;
    const ok = (data, status = 200) => ({ ok: status < 400, status, json: async () => structuredClone(data) });
    if (method !== 'GET') this.writes.push({ method, path, body });
    if (method === 'GET') {
      if (path === 'contents/.github/workflows/patchraptor-release.yml') return ok({ content: Buffer.from('RCC_LEGACY_RELEASE_RETIRED').toString('base64') });
      if (path === 'actions/workflows/patchraptor-release.yml/runs') return ok({ total_count: this.legacyRunning ? 1 : 0 });
      if (path === 'git/ref/heads/main') return ok({ object: { sha: this.f.mainSha } });
      if (path === `git/ref/heads/${BRANCH}`) return this.branch ? ok({ object: { sha: this.branch } }) : ok({}, 404);
      if (path === 'releases') return ok([{ tag_name: this.f.published.tag, draft: false, prerelease: false, published_at: '2026-11-08T00:00:00Z' }]);
      if (path === `git/ref/tags/${this.f.published.tag}`) return ok({ object: { sha: this.f.published.sha, type: 'commit' } });
      if (path.startsWith('git/ref/tags/')) return ok({}, 404);
      if (path.startsWith('compare/')) {
        if (path.endsWith(`...${this.f.mainSha}`)) return ok({ status: 'ahead', merge_base_commit: { sha: this.f.published.sha }, total_commits: this.f.commits.length,
          commits: this.f.commits.map((c) => ({ sha: c.sha, parents: [{ sha: this.f.published.sha }], commit: { message: c.message } })) });
        return ok({ files: Object.keys((this.objects.get(this.branch) ?? this.f.files)).map((filename) => ({ filename, status: 'modified' })) });
      }
      if (path.startsWith('git/commits/')) return ok({ tree: { sha: sha('8') } });
      if (path.startsWith('commits/')) return ok({ commit: { committer: { date: this.f.headDate } } });
      if (path.startsWith('contents/')) {
        const ref = new URL(url).searchParams.get('ref'); const files = ref === this.f.mainSha ? this.f.files : this.objects.get(ref);
        const content = files?.[path.slice(9)]; return content ? ok({ encoding: 'base64', content: Buffer.from(content).toString('base64') }) : ok({}, 404);
      }
      if (path === 'pulls') return ok(this.prs);
    }
    if (path === 'git/trees') { if (this.moveDuringTree) this.f.mainSha = sha('7'); assert.equal(body.base_tree, sha('8')); const id = (this.next++).toString(16).padStart(40, '0'); this.objects.set(id, Object.fromEntries(body.tree.map((x) => [x.path, x.content]))); return ok({ sha: id }); }
    if (path === 'git/commits') { const id = (this.next++).toString(16).padStart(40, '0'); this.objects.set(id, this.objects.get(body.tree)); this.parents = body.parents; return ok({ sha: id }); }
    if (path === 'git/refs' || path === `git/refs/heads/${BRANCH}`) {
      if (this.race) return ok({}, 422);
      this.branch = body.sha; const open = this.prs.find((p) => p.state === 'open'); if (open) open.head.sha = body.sha;
      return ok({ object: { sha: body.sha } });
    }
    if (path === 'pulls' && method === 'POST') { if (this.failPrOnce) { this.failPrOnce = false; return ok({}, 503); } const pr = { number: 99, state: 'open', html_url: 'https://github.com/joshyorko/rcc/pull/99', head: { sha: this.branch } }; this.prs.push(pr); return ok(pr); }
    if (path.startsWith('pulls/') && method === 'PATCH') return ok(this.prs[0]);
    throw new Error(`Unhandled fixture endpoint ${method} ${path}`);
  }
  api() { return new GitHub('test-installation-token', this.fetch.bind(this)); }
}
test('REST lifecycle creates one PR, reruns without writes, refreshes fast-forward, then holds after merge', async () => {
  const server = new Server(); const api = server.api();
  assert.equal((await reconcile(api)).status, 'PREVIEW'); assert.equal(server.writes.length, 0);
  const first = await reconcile(api, { write: true }); assert.equal(first.status, 'PR_UPDATED'); assert.equal(server.prs.length, 1);
  const writes = server.writes.length;
  assert.equal((await reconcile(api, { write: true })).status, 'UNCHANGED'); assert.equal(server.writes.length, writes);
  const old = server.branch; server.f.mainSha = sha('e'); server.f.commits.push({ sha: sha('f'), message: 'fix: new merged fix' });
  assert.equal((await reconcile(api, { write: true })).status, 'PR_UPDATED');
  assert.deepEqual(server.parents, [old, sha('e')]); assert.equal(server.prs.length, 1);
  assert(server.writes.filter((w) => w.path.includes('refs/')).every((w) => w.body.force === false));
  server.f.files = server.objects.get(server.branch); server.f.mainSha = sha('9');
  assert.equal((await reconcile(api, { write: true })).status, 'PENDING_PUBLICATION');
});
test('transport denies main writes, force updates, publication, merges, dispatch, and missing credentials', async () => {
  assert.throws(() => new GitHub(''), /installation token/);
  const api = new Server().api();
  for (const [method, path, body] of [
    ['POST', 'releases', {}], ['POST', 'git/tags', {}], ['PUT', 'pulls/99/merge', {}],
    ['POST', 'actions/workflows/rcc.yaml/dispatches', {}],
    ['PATCH', 'git/refs/heads/main', { force: false }],
    ['POST', 'git/refs', { ref: 'refs/tags/v18.19.11' }],
    ['PATCH', `git/refs/heads/${BRANCH}`, { force: true }],
    ['PATCH', 'pulls/99', { state: 'closed' }],
  ]) await assert.rejects(api.request(method, path, body));
});
test('stale main, PR edits, duplicate PRs, ref race and closed PR deny without overwrite', async () => {
  const stale = new Server(); let reads = 0; const main = stale.api();
  const original = main.main.bind(main); main.main = async () => { const result = await original(); return ++reads > 1 ? sha('e') : result; };
  await assert.rejects(reconcile(main, { write: true }), /Stale main/); assert.equal(stale.writes.length, 0);
  const edited = new Server(); await reconcile(edited.api(), { write: true }); edited.objects.get(edited.branch)[CHANGELOG] += 'Reviewer notes'; edited.writes = [];
  await assert.rejects(reconcile(edited.api(), { write: true }), /reviewer edits/); assert.equal(edited.writes.length, 0);
  const duplicate = new Server(); await reconcile(duplicate.api(), { write: true }); duplicate.prs.push(duplicate.prs[0]);
  await assert.rejects(reconcile(duplicate.api(), { write: true }), /Multiple/);
  const closed = new Server(); await reconcile(closed.api(), { write: true }); closed.prs[0].state = 'closed';
  await assert.rejects(reconcile(closed.api(), { write: true }), /Closed unmerged/);
  const race = new Server(); race.race = true; await assert.rejects(reconcile(race.api(), { write: true }), /422/); assert.equal(race.prs.length, 0);
});

test('active legacy run blocks cutover before any mutation', async () => {
  const server = new Server(); server.legacyRunning = true;
  await assert.rejects(reconcile(server.api(), { write: true }), /Legacy release run is still active/);
  assert.equal(server.writes.length, 0);
});

test('late main race while constructing git objects cannot update refs or PRs', async () => {
  const server = new Server(); server.moveDuringTree = true;
  await assert.rejects(reconcile(server.api(), { write: true }), /Stale main/);
  assert(server.writes.every((w) => ['git/trees', 'git/commits'].includes(w.path)));
  assert.equal(server.branch, null); assert.equal(server.prs.length, 0);
});
test('event source mismatch denies writes', async () => {
  const server = new Server();
  await assert.rejects(reconcile(server.api(), { write: true, expectedSource: sha('e') }), /Event source/);
  assert.equal(server.writes.length, 0);
});

test('interrupted PR creation recovers the recognized singleton branch without a duplicate PR', async () => {
  const server = new Server(); server.failPrOnce = true;
  await assert.rejects(reconcile(server.api(), { write: true }), /503/);
  assert(server.branch); assert.equal(server.prs.length, 0);
  await reconcile(server.api(), { write: true }); assert.equal(server.prs.length, 1);
});
test('published candidate can reuse retained merged singleton branch for next patch', async () => {
  const server = new Server(); await reconcile(server.api(), { write: true });
  server.f.files = server.objects.get(server.branch); server.f.mainSha = sha('9');
  server.prs[0].state = 'closed'; server.prs[0].merged_at = '2026-11-11T00:00:00Z';
  server.f.published = { ...published, tag: 'v18.19.11', sha: sha('6') };
  server.f.commits = [{ sha: sha('f'), message: 'fix: new work after protected publication' }];
  const next = await reconcile(server.api(), { write: true }); assert.equal(next.version, 'v18.19.12');
  assert.equal(server.prs.filter((p) => p.state === 'open').length, 1);
});
test('unrelated branch files cannot be silently discarded', async () => {
  const server = new Server(); await reconcile(server.api(), { write: true });
  server.objects.get(server.branch)['operations/evil.go'] = 'unrelated'; server.writes = [];
  await assert.rejects(reconcile(server.api(), { write: true }), /unrelated changes/);
  assert.equal(server.writes.length, 0);
});
test('truncated or unrelated GitHub history cannot produce a proposal', async () => {
  for (const mutation of [
    (d) => { d.merge_base_commit.sha = sha('f'); },
    (d) => { d.total_commits = 0; },
    (d) => { d.total_commits = 3; d.commits = []; },
  ]) {
    const server = new Server(); const transport = server.fetch.bind(server);
    const api = new GitHub('test-token', async (url, options) => {
      const response = await transport(url, options);
      if (new URL(url).pathname.includes('/compare/')) { const data = await response.json(); mutation(data); return { ...response, json: async () => data }; }
      return response;
    });
    await assert.rejects(reconcile(api, { write: true })); assert.equal(server.writes.length, 0);
  }
});
test('annotated published tags resolve to the actual commit, never target_commitish', async () => {
  const server = new Server(); const transport = server.fetch.bind(server);
  const api = new GitHub('test-token', async (url, options) => {
    if (url.includes('git/tags/')) return { ok: true, status: 200, json: async () => ({ object: { type: 'commit', sha: published.sha } }) };
    const response = await transport(url, options);
    if (url.endsWith('git/ref/tags/v18.19.10')) return { ...response, json: async () => ({ object: { type: 'tag', sha: sha('5') } }) };
    return response;
  });
  assert.equal((await api.snapshot()).published.sha, published.sha);
});
test('candidate tag appearing after object creation denies the ref update', async () => {
  const server = new Server(); const transport = server.fetch.bind(server); let collision = false;
  const api = new GitHub('test-token', async (url, options) => {
    const response = await transport(url, options);
    if (options.method === 'POST' && url.endsWith('git/trees')) collision = true;
    if (collision && url.endsWith('git/ref/tags/v18.19.11')) return { ok: true, status: 200, json: async () => ({ object: { sha: sha('4') } }) };
    return response;
  });
  await assert.rejects(reconcile(api, { write: true }), /tag collision/); assert.equal(server.branch, null);
});

test('checked-in workflow remains PR-only, opt-in, immutable and atomically retires legacy writer', async () => {
  const workflow = await readFile(new URL('../../.github/workflows/release-metadata.yml', import.meta.url), 'utf8');
  const retired = await readFile(new URL('../../.github/workflows/patchraptor-release.yml', import.meta.url), 'utf8');
  assert.match(workflow, /vars\.RCC_RELEASE_METADATA_ENABLED == 'true'/);
  assert.match(workflow, /github\.event_name == 'workflow_dispatch' && inputs\.propose/);
  assert.match(workflow, /secrets\.RCC_RELEASE_APP_PRIVATE_KEY/);
  assert.match(workflow, /RCC_RELEASE_METADATA_TOKEN: \$\{\{ steps\.app\.outputs\.token \}\}/);
  assert.match(workflow, /RCC_RELEASE_EXPECTED_SHA: \$\{\{ github\.sha \}\}/);
  assert(!/HOMEBREW|^\s+contents: write|^\s+pull-requests: write|^\s+actions: write|gh release|git push/m.test(workflow));
  for (const action of workflow.matchAll(/uses: ([^\s]+)@(\S+)/g)) assert.match(action[2], /^[a-f0-9]{40}$/);
  assert.match(retired, /RCC_LEGACY_RELEASE_RETIRED/);
  assert(!/workflow_run:|contents: write|actions: write|git push|gh workflow/.test(retired));
  const lock = JSON.parse(await readFile(new URL('./package-lock.json', import.meta.url)));
  assert.equal(lock.packages['node_modules/release-please'].version, '17.11.2');
});

test('unapproved CLI write stops before authentication/network and malformed notes never lose authored text', async () => {
  const child = spawnSync(process.execPath, [new URL('./run.mjs', import.meta.url).pathname, '--write'], { env: { ...process.env, RCC_RELEASE_METADATA_PROPOSE: '', RCC_RELEASE_METADATA_TOKEN: '' }, encoding: 'utf8' });
  assert.notEqual(child.status, 0); assert.match(child.stderr, /proposal is disabled/);
  const f = fixture(); f.files[CHANGELOG] = f.files[CHANGELOG].replace('## v18.19.11\n', '## v18.19.11\n<!-- rcc-release-notes:end -->\nKeep reviewed safety text\n<!-- rcc-release-notes:start -->\n');
  await assert.rejects(plan(f), /markers are reversed/);
});
