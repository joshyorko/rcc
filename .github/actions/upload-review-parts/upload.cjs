const { execFileSync } = require('node:child_process');
const { readdirSync } = require('node:fs');
const path = require('node:path');

const workspace = process.env.GITHUB_WORKSPACE;
const root = path.resolve(workspace, process.env.INPUT_ROOT);
const uploader = path.join(workspace, 'tmp', 'official-review-uploader', 'dist', 'upload', 'index.js');
for (const entry of readdirSync(root, { withFileTypes: true })) {
  if (!/^(?:native-binary-review|rcc(?:remote)?-(?:linux(?:arm)?64|macos(?:arm)?64|windows64\.exe))-part-\d+$/.test(entry.name)) continue;
  if (!entry.isDirectory()) throw new Error(`Invalid review part: ${entry.name}`);
  const part = path.join(root, entry.name);
  execFileSync('python', ['scripts/release_publication.py', 'check-review-payload', '--root', part],
    { cwd: workspace, stdio: 'inherit' });
  execFileSync(process.execPath, [uploader], {
    cwd: workspace, stdio: 'inherit',
    env: { ...process.env, INPUT_NAME: process.env.INPUT_PREFIX + entry.name, INPUT_PATH: part,
      'INPUT_RETENTION-DAYS': '30', 'INPUT_COMPRESSION-LEVEL': '0', 'INPUT_IF-NO-FILES-FOUND': 'error',
      INPUT_OVERWRITE: 'false', 'INPUT_INCLUDE-HIDDEN-FILES': 'false' },
  });
}
