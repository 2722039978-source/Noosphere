import assert from 'node:assert/strict';
import { existsSync, readFileSync, writeFileSync } from 'node:fs';
import { mkdtemp, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import test from 'node:test';
import { debianPackageVersion, verifyReleaseRef } from './desktop-release-manifest.mjs';
import { createMacDiskImage, macBundleVersions } from './desktop-macos.mjs';

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
