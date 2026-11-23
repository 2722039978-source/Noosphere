import { lstat, mkdir, open, rm, symlink } from 'node:fs/promises';
import { basename, dirname, join, relative } from 'node:path';
import { setTimeout as delay } from 'node:timers/promises';
import { exists, files, run, sha256, verifyRuntimes, version } from './desktop-platform.mjs';

const MACH_O = new Set(['feedface', 'feedfacf', 'cefaedfe', 'cffaedfe', 'cafebabe', 'bebafeca', 'cafebabf', 'bfbafeca']);
const NODE_ENTITLEMENTS = new URL('macos-node.entitlements.plist', import.meta.url);

export async function createMacDiskImage(dmgTree, stage, name, { execute = run, wait = delay } = {}) {
  if (name !== basename(name) || !name.endsWith('.dmg')) throw new Error('Invalid staged DMG filename');
  const dmg = join(stage, name);
  if (await exists(dmg)) throw new Error('Staged DMG already exists: ' + name);
  const maxAttempts = 3;
  for (let attempt = 1; attempt <= maxAttempts; attempt++) {
    try {
      const output = execute('hdiutil', ['create', '-volname', 'Noosphere', '-srcfolder', dmgTree, '-format', 'UDZO', dmg], { capture: true });
      if (output) console.log(output);
      return dmg;
    } catch (error) {
      if (attempt === maxAttempts || !/^hdiutil: create failed - Resource busy\s*$/m.test(error?.message)) throw error;
      // The fresh staging path was absent before the first invocation.
      // Remove only a partial image from this build before retrying.
      await rm(dmg, { force: true });
      console.warn(`hdiutil create reported Resource busy. Retrying ${name} (${attempt + 1}/${maxAttempts}).`);
      await wait(attempt * 1000);
    }
  }
}

export function macBundleVersions(buildVersion) {
  const match = /^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(?:-rc\.([1-9]\d*))?$/.exec(buildVersion);
  if (!match) throw new Error('Invalid Noosphere version for a macOS bundle');
  const [, major, minor, patch, candidate] = match;
  // Reserve the last build number in each patch for the stable release.
  // The signed bundle also keeps the exact SemVer used by the application.
  const order = candidate ? BigInt(candidate) : 999n;
  if (candidate && order >= 999n) throw new Error('macOS bundle build number supports rc.1 through rc.998');
  return {
    CFBundleShortVersionString: `${major}.${minor}.${patch}`,
    CFBundleVersion: `${major}.${minor}.${BigInt(patch) * 1000n + order}`,
    NoosphereVersion: buildVersion,
  };
}

const plistPath = app => join(app, 'Contents', 'Info.plist');
const plist = (app, action) => run('/usr/libexec/PlistBuddy', ['-c', action, plistPath(app)], { capture: true });

export function verifyMacBundleVersions(app, buildVersion) {
  for (const [key, value] of Object.entries(macBundleVersions(buildVersion))) {
    if (plist(app, `Print :${key}`) !== value) throw new Error(`macOS ${key} does not match ${buildVersion}`);
  }
  if (buildVersion.includes('-') && !plist(app, 'Print :CFBundleDisplayName').endsWith(`(${buildVersion})`)) {
    throw new Error('macOS bundle display name does not identify the release candidate');
  }
}

function configureMacBundleVersions(app) {
  const versions = macBundleVersions(version);
  plist(app, `Set :CFBundleShortVersionString ${versions.CFBundleShortVersionString}`);
  plist(app, `Set :CFBundleVersion ${versions.CFBundleVersion}`);
  plist(app, `Add :NoosphereVersion string ${versions.NoosphereVersion}`);
  if (version.includes('-')) {
    const name = plist(app, 'Print :CFBundleDisplayName');
    plist(app, `Set :CFBundleDisplayName ${name} (${version})`);
  }
  verifyMacBundleVersions(app, version);
}

function versionGreater(actual, limit) {
  const a = actual.split('.').map(Number);
  const b = limit.split('.').map(Number);
  for (let i = 0; i < Math.max(a.length, b.length); ++i) {
    if ((a[i] || 0) !== (b[i] || 0)) return (a[i] || 0) > (b[i] || 0);
  }
  return false;
}

async function machFiles(app) {
  const binaries = [];
  for (const path of await files(join(app, 'Contents'))) {
    const stat = await lstat(path);
    if (!stat.isFile() || (!(stat.mode & 0o111) && !/\.(node|so|dylib)$/.test(path))) continue;
    const input = await open(path);
    const magic = Buffer.alloc(4);
    try { await input.read(magic); } finally { await input.close(); }
    if (MACH_O.has(magic.toString('hex'))) binaries.push(path);
  }
  return binaries;
}

async function checkMach(target, app, binaries) {
  const arch = target.arch === 'arm64' ? 'arm64' : 'x86_64';
  const inventory = {};
  for (const binary of binaries) {
    const arches = run('lipo', ['-archs', binary], { capture: true }).split(/\s+/);
    if (!arches.includes(arch)) throw new Error(binary + ' is missing architecture ' + arch);
    if (dirname(binary) === join(app, 'Contents/MacOS') && arches.length !== 1) {
      throw new Error('The application executable must have exactly one architecture');
    }
    const commands = run('otool', ['-arch', arch, '-l', binary], { capture: true });
    const minimums = [...commands.matchAll(/\bcmd LC_(?:BUILD_VERSION|VERSION_MIN_MACOSX)\s+cmdsize \d+\s+(?:platform \S+\s+minos|version) ([0-9.]+)/g)]
      .map(match => match[1]);
    if (!minimums.length || minimums.some(minimum => versionGreater(minimum, target.deployment_target))) {
      throw new Error('Could not verify deployment floor ' + target.deployment_target + ' for ' + binary
        + ' (' + minimums.join(', ') + ')');
    }
    inventory[relative(app, binary)] = { sha256: await sha256(binary), architectures: arches, minimum_macos: minimums };
  }
  if (!Object.keys(inventory).some(path => path.startsWith('Contents/MacOS/'))) {
    throw new Error('No macOS application executable found');
  }
  return inventory;
}

export async function buildMacArtifacts(target, app, stage, names, scratch, signed) {
  const identity = signed ? process.env.NOOSPHERE_MAC_SIGN_IDENTITY : '-';
  const profile = process.env.NOOSPHERE_MAC_NOTARY_PROFILE;
  const keychain = process.env.NOOSPHERE_MAC_KEYCHAIN;
  if (signed && (!identity?.startsWith('Developer ID Application: ') || !profile)) {
    throw new Error('Formal macOS packaging requires a Developer ID Application identity and a notarytool keychain profile');
  }
  configureMacBundleVersions(app);
  const resources = join(app, 'Contents/Resources');
  const native = await machFiles(app);
  const componentHashesBeforeSigning = await checkMach(target, app, native);

  const options = ['--force', '--sign', identity, '--options', 'runtime'];
  if (signed) options.push('--timestamp');
  if (keychain) options.push('--keychain', keychain);
  for (const binary of native) {
    if (dirname(binary) === join(app, 'Contents/MacOS')) continue;
    const entitlement = binary === join(resources, target.components.node.entry)
      ? ['--entitlements', decodeURIComponent(NODE_ENTITLEMENTS.pathname)] : [];
    run('codesign', [...options, ...entitlement, binary]);
    run('codesign', ['--verify', '--strict', binary]);
  }
  run('codesign', [...options, app]);
  run('codesign', ['--verify', '--deep', '--strict', app]);

  const submit = async path => {
    const args = ['notarytool', 'submit', path, '--keychain-profile', profile, '--wait', '--output-format', 'json'];
    if (keychain) args.push('--keychain', keychain);
    const result = JSON.parse(run('xcrun', args, { capture: true, timeout: 900_000 }));
    if (result.status !== 'Accepted' || !result.id) throw new Error('Notarization failed for ' + basename(path));
    return result.id;
  };
  const signing = { state: signed ? 'developer-id-notarized' : 'adhoc-preview' };
  if (signed) {
    const submission = join(scratch, 'app-for-notary.zip');
    run('ditto', ['-c', '-k', '--keepParent', app, submission]);
    signing.app_submission_id = await submit(submission);
    run('xcrun', ['stapler', 'staple', app]);
    run('xcrun', ['stapler', 'validate', app]);
    run('spctl', ['--assess', '--type', 'execute', app]);
  }

  const dmgTree = join(scratch, 'disk-image');
  await mkdir(dmgTree);
  run('ditto', [app, join(dmgTree, basename(app))]);
  await symlink('/Applications', join(dmgTree, 'Applications'));
  const dmg = await createMacDiskImage(dmgTree, stage, names.dmg);
  if (signed) {
    run('codesign', [...options.filter(arg => arg !== '--options' && arg !== 'runtime'), dmg]);
    signing.dmg_submission_id = await submit(dmg);
    run('xcrun', ['stapler', 'staple', dmg]);
    run('xcrun', ['stapler', 'validate', dmg]);
    run('spctl', ['--assess', '--type', 'open', '--context', 'context:primary-signature', dmg]);
  }
  run('tar', ['-czf', join(stage, names.app), '-C', dirname(app), basename(app)]);

  const moved = join(scratch, 'app archive 搬迁');
  await mkdir(moved);
  run('tar', ['-xzf', join(stage, names.app), '-C', moved]);
  const movedApp = join(moved, basename(app));
  verifyMacBundleVersions(movedApp, version);
  run('codesign', ['--verify', '--deep', '--strict', movedApp]);
  if (signed) run('xcrun', ['stapler', 'validate', movedApp]);
  await verifyRuntimes(target, join(movedApp, 'Contents/Resources'), moved, true);

  const mount = join(scratch, 'mounted DMG');
  await mkdir(mount);
  run('hdiutil', ['attach', '-nobrowse', '-readonly', '-mountpoint', mount, dmg]);
  try {
    const mountedApp = join(mount, basename(app));
    verifyMacBundleVersions(mountedApp, version);
    run('codesign', ['--verify', '--deep', '--strict', mountedApp]);
    if (signed) run('xcrun', ['stapler', 'validate', mountedApp]);
    await verifyRuntimes(target, join(mountedApp, 'Contents/Resources'), moved, true);
  } finally {
    run('hdiutil', ['detach', mount]);
  }
  return { signing, componentHashesBeforeSigning, finalApp: movedApp };
}
