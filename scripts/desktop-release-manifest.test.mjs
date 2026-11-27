import assert from 'node:assert/strict';
import { existsSync, readFileSync, writeFileSync } from 'node:fs';
import { mkdir, mkdtemp, readdir, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import test from 'node:test';
import { aggregate, deliveryPlan, debianPackageVersion, verifyReleaseRef } from './desktop-release-manifest.mjs';
import { checksums, manifest, packageNames, run, version } from './desktop-platform.mjs';
import { createMacDiskImage, macBundleVersions, notarizeMacArtifact } from './desktop-macos.mjs';

test('a matching RC tag can build preview packages only', () => {
  assert.doesNotThrow(() => verifyReleaseRef('1.0.0-rc.1', 'v1.0.0-rc.1', false));
  assert.throws(() => verifyReleaseRef('1.0.0-rc.1', 'v1.0.0-rc.1', true), /numeric/);
  assert.throws(() => verifyReleaseRef('1.0.0-rc.1', 'v1.0.0', false), /match/);
});

test('formal packaging requires a selected numeric tag exactly matching VERSION', () => {
  assert.doesNotThrow(() => verifyReleaseRef('1.0.0', 'v1.0.0', true));
  assert.throws(() => verifyReleaseRef('1.0.0', '', true), /tag/);
  assert.doesNotThrow(() => verifyReleaseRef('1.0.0-rc.1', '', false));
  assert.throws(() => verifyReleaseRef('1.0.0-beta.1', 'v1.0.0-beta.1', false), /release/);
});

test('Debian metadata orders candidate packages before the stable version', () => {
  assert.equal(debianPackageVersion('1.0.0-rc.1'), '1.0.0~rc.1');
  assert.equal(debianPackageVersion('1.0.0'), '1.0.0');
});

test('macOS requires numeric plist versions while preserving the canonical candidate', () => {
  assert.deepEqual(macBundleVersions('1.0.0-rc.1'), {
    CFBundleShortVersionString: '1.0.0', CFBundleVersion: '1.0.1', NoosphereVersion: '1.0.0-rc.1',
  });
  assert.equal(macBundleVersions('1.0.0-rc.2').CFBundleVersion, '1.0.2');
  assert.equal(macBundleVersions('1.0.0').CFBundleVersion, '1.0.999');
  assert.equal(macBundleVersions('1.0.1-rc.1').CFBundleVersion, '1.0.1001');
  assert.throws(() => macBundleVersions('1.0.0-rc.999'), /macOS bundle build number/);
});

async function imageFixture(t) {
  const stage = await mkdtemp(join(tmpdir(), 'noosphere-hdiutil-test-'));
  t.after(() => rm(stage, { recursive: true, force: true }));
  const name = 'candidate.dmg';
  return { stage, name, image: join(stage, name) };
}

const busyError = () => new Error('hdiutil failed (1)\nhdiutil: create failed - Resource busy');

test('a busy disk image creation retries after removing only its staged partial result', async t => {
  const { stage, name, image } = await imageFixture(t);
  const sibling = join(stage, 'other-package.dmg');
  writeFileSync(sibling, 'untouched');
  const waits = [];
  let executions = 0;
  const saved = await createMacDiskImage('contents', stage, name, {
    wait: async ms => { waits.push(ms); },
    execute: (command, arguments_, options) => {
      assert.equal(command, 'hdiutil');
      assert.equal(arguments_[0], 'create');
      assert.equal(arguments_.at(-1), image);
      assert.equal(options.capture, true);
      assert.equal(existsSync(image), false);
      assert.equal(readFileSync(sibling, 'utf8'), 'untouched');
      writeFileSync(image, 'fixture');
      if (++executions === 1) throw busyError();
    },
  });
  assert.equal(saved, image);
  assert.equal(executions, 2);
  assert.deepEqual(waits, [1000]);
});

test('disk image creation returns permanent errors unchanged and bounds busy retries', async t => {
  const { stage, name } = await imageFixture(t);
  const waits = [];
  const wait = async ms => { waits.push(ms); };
  const permanent = new Error('hdiutil failed (1)\nhdiutil: create failed - Permission denied');
  await assert.rejects(createMacDiskImage('contents', stage, name, {
    wait, execute: () => { throw permanent; },
  }), error => error === permanent);
  assert.deepEqual(waits, []);

  const busy = busyError();
  let executions = 0;
  await assert.rejects(createMacDiskImage('contents', stage, name, {
    wait, execute: () => { executions++; throw busy; },
  }), error => error === busy);
  assert.equal(executions, 3);
  assert.deepEqual(waits, [1000, 2000]);
});

test('disk image creation preserves existing staged output and rejects escaped names', async t => {
  const { stage, name, image } = await imageFixture(t);
  writeFileSync(image, 'previous output');
  const options = { execute: () => assert.fail('hdiutil must not run') };
  await assert.rejects(createMacDiskImage('contents', stage, name, options), /already exists/);
  await assert.rejects(createMacDiskImage('contents', stage, '../outside.dmg', options), /Invalid staged DMG filename/);
  assert.equal(readFileSync(image, 'utf8'), 'previous output');
});

const submissionID = '2efe2717-52ef-43a5-96dc-0797e4ca1041';

test('mac notarization records its submission before waiting and accepts only the corresponding status', () => {
  const calls = [];
  const report = message => calls.push(message);
  const execute = (command, args, options) => {
    assert.equal(command, 'xcrun');
    assert.equal(options.capture, true);
    calls.push(args.slice(1, 3));
    assert.deepEqual(args.slice(3, 7), ['--keychain-profile', 'fixture', '--keychain', 'fixture-keychain']);
    if (args[1] === 'submit') {
      assert.ok(!args.includes('--wait'));
      assert.equal(options.timeout, 600_000);
      return JSON.stringify({ id: submissionID });
    }
    assert.deepEqual(args.slice(-4), ['--output-format', 'json', '--timeout', '60m']);
    assert.equal(options.timeout, 3_660_000);
    return JSON.stringify({ id: submissionID, status: 'Accepted' });
  };
  assert.equal(notarizeMacArtifact('candidate.zip', 'fixture', 'fixture-keychain', { execute, report }), submissionID);
  assert.deepEqual(calls, [['submit', 'candidate.zip'], `Notarization submitted ${submissionID}`,
    ['wait', submissionID], `Notarization ${submissionID}: Accepted`]);
});

test('failed or rejected notarization retains the same submission ID and suppresses raw command output', () => {
  for (const reject of [true, false]) {
    const phases = [];
    const reports = [];
    const execute = (_command, args) => {
      phases.push(args[1]);
      if (args[1] === 'submit') return JSON.stringify({ id: submissionID });
      if (reject) return JSON.stringify({ id: submissionID, status: 'Rejected', message: 'raw output' });
      throw new Error('raw output');
    };
    assert.throws(() => notarizeMacArtifact('candidate.dmg', 'fixture', undefined, {
      execute, report: message => reports.push(message),
    }), error => error.message.includes(submissionID) && !error.message.includes('raw output'));
    assert.deepEqual(phases, ['submit', 'wait']);
    assert.ok(reports[0].includes(submissionID));
    assert.ok(reports.every(message => !message.includes('raw output')));
  }
});

test('dispatch selects native runners and reserves formal packaging for the complete platform set', () => {
  const preview = deliveryPlan('windows-linux');
  assert.deepEqual(preview.targets, ['windows-x64', 'linux-x64']);
  assert.deepEqual(preview.matrix.include, [
    { target: 'linux-x64', runner: 'ubuntu-22.04', arch: 'X64', os: 'Linux', abi: 'jammy-glibc235' },
  ]);
  const all = deliveryPlan();
  assert.deepEqual(all.targets.toSorted(), ['linux-x64', 'macos-arm64', 'macos-x64', 'windows-x64']);
  assert.deepEqual(all.matrix.include.map(row => row.target).toSorted(), ['linux-x64', 'macos-arm64', 'macos-x64']);
  assert.deepEqual(deliveryPlan('all', true), all);
  assert.throws(() => deliveryPlan('windows-linux', true), /Formal packaging requires all platforms/);
  assert.throws(() => deliveryPlan('windows-only'), /Unknown/);
});

async function writeTarget(input, id) {
  const target = manifest.targets[id];
  const dir = join(input, 'noosphere-' + id);
  await mkdir(dir, { recursive: true });
  const source_revision = run('git', ['rev-parse', 'HEAD'], { capture: true });
  const hash = '1'.repeat(64);
  const info = { version, target: id, os: target.os, arch: target.arch, build_profile: 'release',
    source_dirty: false, source_revision, source_fingerprint: hash, frontend_sha256: hash, desktop_sha256: hash,
    backend_sha256: hash, runtime_revision: 2, codelens_runtime: true, rea_runtime: true };
  const names = Object.values(packageNames(target));
  for (const name of names) writeFileSync(join(dir, name), id + ': ' + name);
  writeFileSync(join(dir, 'build-info.json'), JSON.stringify(info));
  writeFileSync(join(dir, 'SHA256SUMS.txt'), await checksums(dir, [...names, 'build-info.json']));
  return dir;
}

test('preview aggregation verifies exactly the requested target manifests and their original checksums', async t => {
  const { stage } = await imageFixture(t);
  const input = join(stage, 'verified'), output = join(stage, 'candidate');
  const limited = ['--windows-linux-preview'];
  const windows = await writeTarget(input, 'windows-x64');
  await assert.rejects(aggregate([input, output, ...limited]), /Missing target manifests: linux-x64/);
  await writeTarget(input, 'linux-x64');
  await assert.rejects(aggregate([input, output]), /Missing target manifests:.*macos/);
  await assert.rejects(aggregate([input, output, ...limited, '--formal-tag', 'v' + version]), /Usage:/);
  await aggregate([input, output, ...limited]);
  const info = JSON.parse(readFileSync(join(output, 'delivery-info.json'), 'utf8'));
  assert.equal(info.mode, 'windows-linux-preview');
  assert.deepEqual(Object.keys(info.targets).sort(), ['linux-x64', 'windows-x64']);
  const names = (await readdir(output)).filter(name => name !== 'SHA256SUMS.txt');
  const actual = readFileSync(join(output, 'SHA256SUMS.txt'), 'utf8').trim().split('\n').sort();
  const expected = (await checksums(output, names)).trim().split('\n').sort();
  assert.deepEqual(actual, expected);
  const asset = Object.values(packageNames(manifest.targets['windows-x64']))[0];
  writeFileSync(join(windows, asset), 'changed');
  await assert.rejects(aggregate([input, join(stage, 'tampered'), ...limited]), /Asset hash mismatch/);
  assert.equal(existsSync(join(stage, 'tampered')), false);
  await writeTarget(input, 'windows-x64');
  await writeTarget(input, 'macos-arm64');
  await assert.rejects(aggregate([input, join(stage, 'extra'), ...limited]), /Unexpected or duplicate target/);
  await writeTarget(input, 'macos-x64');
  await aggregate([input, join(stage, 'full')]);
  const full = JSON.parse(readFileSync(join(stage, 'full', 'delivery-info.json'), 'utf8'));
  assert.equal(full.mode, 'preview');
  assert.deepEqual(Object.keys(full.targets).sort(), Object.keys(manifest.targets).sort());
});
