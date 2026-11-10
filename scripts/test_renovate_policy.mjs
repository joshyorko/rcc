// Run against the exact released runner, never a handwritten merge emulator.
// Usage: node scripts/test_renovate_policy.mjs <renovate-package-dir> <inherited-json>
import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import { resolve } from 'node:path';
import { createRequire } from 'node:module';
import { pathToFileURL } from 'node:url';
const [runtime, inherited] = process.argv.slice(2);
assert(runtime && inherited, 'Supply Renovate package directory and exact central policy JSON');
const load = (path) => import(pathToFileURL(resolve(runtime, path)));
const JSON5 = createRequire(resolve(runtime, 'package.json'))('json5');
const { mergeChildConfig } = await load('dist/config/utils.js');
const { applyPackageRules } = await load('dist/util/package-rules/index.js');
const { extractPackageFile } = await load('dist/modules/manager/custom/regex/index.js');
const { extractPackageFile: pythonExtract } = await load('dist/modules/manager/pip_requirements/index.js');
const { extractPackageFile: actionsExtract } = await load('dist/modules/manager/github-actions/index.js');
assert.equal(JSON.parse(await readFile(resolve(runtime, 'package.json'))).version, '43.288.0');
const local = JSON5.parse(await readFile('.github/renovate.json5', 'utf8'));
const central = JSON.parse(await readFile(inherited, 'utf8'));
const { resolveConfigPresets } = await load('dist/config/presets/index.js');
assert.deepEqual(local.extends, ['github>joshyorko/renovate-config:org-inherited-config']);
// Substitute the supplied immutable central snapshot for the GitHub reference;
// resolve every bundled preset using the actual runner.
const { extends: reference, ...localRules } = local;
const { config: resolvedCentral } = await resolveConfigPresets(central);
const effective = mergeChildConfig(resolvedCentral, localRules);
const compiler = (await readFile('go.mod', 'utf8')).match(/^go (\d+\.\d+\.\d+)$/m)[1];
for (const manager of ['gomod', 'custom.regex', 'github-actions', 'pip_requirements', 'dockerfile']) {
  for (const depName of ['go', 'github.com/spf13/cobra', 'github.com/spf13/viper', 'gopkg.in/yaml.v3', 'golang.org/x/sys', 'robotframework', 'actions/checkout']) {
    for (const updateType of ['major', 'minor', 'patch', 'pin', 'pinDigest', 'digest', 'replacement', 'rollback']) {
      const result = await applyPackageRules({ ...effective, repository: 'joshyorko/rcc', manager, depName, packageName: depName, updateType });
      assert.equal(result.automerge, false, `${manager} ${depName} ${updateType}`);
      assert.equal(result.platformAutomerge, false);
      const routine = manager === 'gomod' && ['github.com/spf13/cobra', 'github.com/spf13/viper', 'gopkg.in/yaml.v3'].includes(depName) && updateType === 'patch';
      assert.equal(result.dependencyDashboardApproval, !routine);
    }
  }
}
const yamlV2 = await applyPackageRules({ ...effective, manager: 'gomod', depName: 'gopkg.in/yaml.v2', packageName: 'gopkg.in/yaml.v2', updateType: 'major' });
assert.equal(yamlV2.enabled, false);
assert.equal(effective.prConcurrentLimit, 1);
assert.equal(effective['github-actions'].enabled, true);
assert.equal(effective.pip_requirements.enabled, true);
const custom = local.customManagers[0];
const files = new Map([
  ['go.mod', 1], ['.dagger/main.go', 1], ['.github/workflows/rcc.yaml', 3],
  ['.github/workflows/publish-release.yml', 1], ['.github/workflows/export-release-evidence.yml', 1],
  ['scripts/release_publication.py', 1],
]);
for (const [file, count] of files) {
  assert(custom.managerFilePatterns.some((p) => new RegExp(p.slice(1, -1)).test(file)), file);
  const content = await readFile(file, 'utf8');
  const result = extractPackageFile(content, file, custom);
  assert.equal(result.deps.length, count, file);
  for (const dep of result.deps) {
    assert.equal(dep.depName, 'go');
    assert.equal(dep.datasource, 'golang-version');
    assert.equal(dep.currentValue, compiler);
    // Exercise a realistic regex replacement and re-extract the future pin.
    const upgraded = content.replaceAll(dep.replaceString, dep.replaceString.replace(compiler, '99.99.99'));
    assert(extractPackageFile(upgraded, file, custom).deps.some((d) => d.currentValue === '99.99.99'));
  }
}
assert(!custom.managerFilePatterns.some((p) => new RegExp(p.slice(1, -1)).test('developer/setup.yaml')));
assert(!custom.managerFilePatterns.some((p) => new RegExp(p.slice(1, -1)).test('assets/micromamba_version.txt')));
assert((await pythonExtract(await readFile('robot_requirements.txt', 'utf8'), 'robot_requirements.txt', {})).deps.some((d) => d.depName === 'robotframework'));
assert((await actionsExtract(await readFile('.github/workflows/rcc.yaml', 'utf8'), '.github/workflows/rcc.yaml', {})).deps.some((d) => d.depName === 'actions/checkout'));
console.log('Renovate 43.288.0: effective central + RCC rules, compiler extraction/replacement, Python and Actions extraction pass');
