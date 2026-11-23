import assert from 'node:assert/strict';
import test from 'node:test';
import { debianPackageVersion, verifyReleaseRef } from './desktop-release-manifest.mjs';
import { macBundleVersions } from './desktop-macos.mjs';

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
