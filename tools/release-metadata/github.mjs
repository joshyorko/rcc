import { BRANCH, STATE, VERSION, CHANGELOG, check, isSha, plan, verifyGeneratedBranch } from './plan.mjs';

const paths = [VERSION, CHANGELOG, STATE];
const refPath = `git/refs/heads/${BRANCH}`;
export class GitHub {
  constructor(token, transport = fetch) {
    check(token, 'An installation token is required; there is no GITHUB_TOKEN fallback');
    this.token = token;
    this.transport = transport;
  }
  async request(method, path, data, optional = false) {
    // Deliberately excludes release/tag creation, actions dispatch, main updates,
    // merge endpoints, settings, labels and secrets. No Release Please client writes.
    const writeAllowed = method === 'POST' && ['git/trees', 'git/commits', 'git/refs', 'pulls'].includes(path)
      || method === 'PATCH' && (path === refPath || /^pulls\/\d+$/.test(path));
    check(method === 'GET' || writeAllowed, `Forbidden GitHub operation: ${method} ${path}`);
    if (method === 'POST' && path === 'git/refs') check(data.ref === `refs/heads/${BRANCH}`, 'Only singleton branch creation is allowed');
    if (method === 'PATCH' && path === refPath) check(data.force === false, 'Force updates are forbidden');
    if (method === 'POST' && path === 'pulls') check(data.head === BRANCH && data.base === 'main' && data.draft === true, 'Only draft metadata PR creation is allowed');
    if (method === 'PATCH' && /^pulls\//.test(path)) check(Object.keys(data).every((k) => ['title', 'body'].includes(k)), 'Only PR text updates are allowed');
    const response = await this.transport(`https://api.github.com/repos/joshyorko/rcc/${path}`, {
      method, headers: { Authorization: `Bearer ${this.token}`, Accept: 'application/vnd.github+json', 'X-GitHub-Api-Version': '2022-11-28' },
      ...(data ? { body: JSON.stringify(data) } : {}),
    });
    if (optional && response.status === 404) return null;
    check(response.ok, `GitHub ${method} ${path} failed (${response.status})`);
    return response.json();
  }
  async pages(path) {
    const result = [];
    for (let page = 1; page <= 100; page++) {
      const data = await this.request('GET', `${path}${path.includes('?') ? '&' : '?'}per_page=100&page=${page}`);
      check(Array.isArray(data), 'Expected paginated GitHub array');
      result.push(...data);
      if (data.length < 100) return result;
    }
    throw new Error('History exceeds pagination bound');
  }
  async published() {
    const releases = await this.pages('releases');
    const stable = releases.filter((r) => !r.draft && !r.prerelease && /^v\d+\.\d+\.\d+$/.test(r.tag_name));
    check(stable.length, 'No stable published release');
    // Publication order is authoritative; reject out-of-order/version regressions
    // in the state model rather than silently substituting an unpublished tag.
    stable.sort((a, b) => b.published_at.localeCompare(a.published_at));
    const latest = stable[0];
    let object = (await this.request('GET', `git/ref/tags/${latest.tag_name}`)).object;
    for (let depth = 0; object.type === 'tag' && depth < 5; depth++) object = (await this.request('GET', `git/tags/${object.sha}`)).object;
    check(object.type === 'commit' && isSha(object.sha), 'Published tag does not resolve to a commit');
    return { tag: latest.tag_name, sha: object.sha, draft: false, prerelease: false };
  }
  async main() { return (await this.request('GET', 'git/ref/heads/main')).object.sha; }
  async files(ref) {
    const result = {};
    for (const path of paths) {
      const file = await this.request('GET', `contents/${path}?ref=${ref}`, undefined, path === STATE);
      check(!file || file.encoding === 'base64', 'Unexpected GitHub file encoding');
      result[path] = file ? Buffer.from(file.content, 'base64').toString('utf8') : null;
    }
    return result;
  }
  async snapshot() {
    const mainSha = await this.main();
    check(isSha(mainSha), 'Invalid main SHA');
    const published = await this.published();
    const compare = `compare/${published.sha}...${mainSha}`;
    let commits = [];
    let total;
    for (let page = 1; page <= 100; page++) {
      const data = await this.request('GET', `${compare}?per_page=100&page=${page}`);
      check(['ahead', 'identical'].includes(data.status) && data.merge_base_commit?.sha === published.sha, 'Published source is not in main ancestry');
      total ??= data.total_commits;
      check(total === data.total_commits, 'History changed during pagination');
      commits.push(...data.commits);
      if (commits.length >= total) break;
      check(data.commits.length > 0, 'Truncated history');
    }
    check(commits.length === total && new Set(commits.map((c) => c.sha)).size === total, 'Incomplete or duplicated history');
    const head = await this.request('GET', `commits/${mainSha}`);
    const version = published.tag.match(/^v(\d+)\.(\d+)\.(\d+)$/);
    const next = `v${version[1]}.${version[2]}.${Number(version[3]) + 1}`;
    return { repository: 'joshyorko/rcc', mainSha, published, ancestryVerified: true, historyComplete: true,
      headDate: head.commit.committer.date, files: await this.files(mainSha),
      candidateTagExists: !!await this.request('GET', `git/ref/tags/${next}`, undefined, true),
      // Merged PR branch commits are included once; merge aggregate messages are
      // excluded so their summarized child commits cannot generate duplicate notes.
      commits: commits.filter((c) => c.parents.length < 2).map((c) => ({ sha: c.sha, message: c.commit.message })),
    };
  }
  async singleton() {
    const prs = await this.pages(`pulls?state=all&base=main&head=joshyorko:${BRANCH}`);
    const open = prs.filter((p) => p.state === 'open');
    check(open.length <= 1, 'Multiple release-metadata PRs');
    const branch = await this.request('GET', `git/ref/heads/${BRANCH}`, undefined, true);
    check(open.length === 0 || branch?.object.sha === open[0].head.sha, 'PR head does not match singleton branch');
    if (!open.length && prs.some((p) => !p.merged_at)) throw new Error('Closed unmerged metadata PR requires owner review');
    return { pr: open[0] ?? null, sha: branch?.object.sha ?? null };
  }
}

export async function reconcile(api, { write = false, expectedSource } = {}) {
  const snapshot = await api.snapshot();
  if (expectedSource) check(snapshot.mainSha === expectedSource, 'Event source is no longer current main');
  if (write) {
    const legacy = await api.request('GET', `contents/.github/workflows/patchraptor-release.yml?ref=${snapshot.mainSha}`);
    const text = Buffer.from(legacy.content, 'base64').toString('utf8');
    check(text.includes('RCC_LEGACY_RELEASE_RETIRED') && !/contents: write|actions: write|git push/.test(text), 'Legacy version controller is not retired');
    for (const status of ['queued', 'in_progress', 'waiting', 'pending', 'requested']) {
      const runs = await api.request('GET', `actions/workflows/patchraptor-release.yml/runs?status=${status}&per_page=1`);
      check(runs.total_count === 0, 'Legacy release run is still active; owner must drain it before cutover');
    }
  }
  const proposal = await plan(snapshot);
  if (proposal.status !== 'PROPOSE') return proposal;
  const singleton = await api.singleton();
  let oldFiles;
  if (singleton.sha) {
    oldFiles = await api.files(singleton.sha);
    verifyGeneratedBranch(oldFiles);
    const diff = await api.request('GET', `compare/${snapshot.mainSha}...${singleton.sha}`);
    check(diff.files && diff.files.length <= 3 && diff.files.every((f) => paths.includes(f.filename) && f.status !== 'removed'), 'Release branch contains unrelated changes');
    if (singleton.pr && paths.every((path) => oldFiles[path] === proposal.files[path])) return { ...proposal, status: 'UNCHANGED', pr: singleton.pr.html_url };
  }
  if (!write) return { ...proposal, status: 'PREVIEW' };
  const assertFresh = async (expectedBranch) => {
    const fresh = await api.published();
    check(await api.main() === snapshot.mainSha && fresh.tag === snapshot.published.tag && fresh.sha === snapshot.published.sha, 'Stale main or published baseline');
    const freshBranch = await api.request('GET', `git/ref/heads/${BRANCH}`, undefined, true);
    check((freshBranch?.object.sha ?? null) === expectedBranch, 'Stale singleton head');
    check(!await api.request('GET', `git/ref/tags/${proposal.version}`, undefined, true), 'Candidate tag collision');
  };
  await assertFresh(singleton.sha);
  const sourceCommit = await api.request('GET', `git/commits/${snapshot.mainSha}`);
  check(isSha(sourceCommit.tree?.sha), 'Source commit has no valid tree');
  const tree = await api.request('POST', 'git/trees', { base_tree: sourceCommit.tree.sha,
    tree: paths.map((path) => ({ path, mode: '100644', type: 'blob', content: proposal.files[path] })),
  });
  const commit = await api.request('POST', 'git/commits', { message: `${proposal.title}\n\nRefs #286. Metadata only; protected publication remains separate.`, tree: tree.sha,
    parents: singleton.sha ? [...new Set([singleton.sha, snapshot.mainSha])] : [snapshot.mainSha],
  });
  await assertFresh(singleton.sha);
  if (singleton.sha) await api.request('PATCH', refPath, { sha: commit.sha, force: false });
  else await api.request('POST', 'git/refs', { ref: `refs/heads/${BRANCH}`, sha: commit.sha });
  await assertFresh(commit.sha);
  const body = `Refs #286\n\nRelease Please 17.11.2 metadata for ${proposal.version}, published baseline ${snapshot.published.tag} (${snapshot.published.sha}).\nSource main: ${snapshot.mainSha}.\n\nReview authored safety notes and generated commit links. This PR does not authorize publication. Exact-source qualification, independent artifact review, and rcc-production approval remain mandatory.\n\nRefresh fails closed on reviewer edits rather than overwriting them. After merge, metadata holds until the candidate is published.\n`;
  let pr = singleton.pr;
  if (pr) await api.request('PATCH', `pulls/${pr.number}`, { title: proposal.title, body });
  else pr = await api.request('POST', 'pulls', { title: proposal.title, body, head: BRANCH, base: 'main', draft: true, maintainer_can_modify: true });
  await assertFresh(commit.sha);
  return { status: 'PR_UPDATED', version: proposal.version, sourceSha: snapshot.mainSha, headSha: commit.sha, pr: pr.html_url, publication: 'OWNER_APPROVAL_REQUIRED' };
}
