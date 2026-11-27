import { copyFile, lstat, mkdir, mkdtemp, readdir, readFile, rename, rm, writeFile } from 'node:fs/promises';
import { basename, dirname, join, resolve } from 'node:path';
import { checksums, exists, files, json, manifest, packageNames, run, sha256, version } from './desktop-platform.mjs';

function validHash(hash) {
  return typeof hash === 'string' && /^[a-f0-9]{64}$/i.test(hash);
}

const numeric = /^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)$/;
const candidate = /^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)-rc\.[1-9]\d*$/;

export function verifyReleaseRef(buildVersion, tag, formal) {
  if (formal && (!tag || !numeric.test(buildVersion))) {
    throw new Error('Formal packaging requires a selected numeric release tag');
  }
  if (tag && !numeric.test(buildVersion) && !candidate.test(buildVersion)) {
    throw new Error('A release tag requires a numeric version or an rc.N version');
  }
  if (tag && tag !== 'v' + buildVersion) throw new Error('Tag must exactly match VERSION');
}

export function debianPackageVersion(buildVersion) {
  // In Debian's version order, a hyphen starts the package revision.
  // A tilde makes a pre-release upgrade normally to the stable version.
  return candidate.test(buildVersion) ? buildVersion.replace('-rc.', '~rc.') : buildVersion;
}

function commitTree(revision) {
  if (!/^[a-f0-9]{40}$/.test(revision)) throw new Error('Invalid source commit ' + revision);
  return run('git', ['rev-parse', '--verify', revision + '^{commit}^{tree}'], { capture: true });
}

async function verifyTarget(dir, id, target, revision, tree, formal) {
  const expected = Object.values(packageNames(target));
  const scope = [...expected, 'build-info.json'];
  const manifestPath = join(dir, 'SHA256SUMS.txt');
  for (const name of [...scope, 'SHA256SUMS.txt']) {
    if (!(await lstat(join(dir, name))).isFile()) throw new Error('Non-regular release file: ' + id + '/' + name);
  }
  const info = await json(join(dir, 'build-info.json'));
  if (info.version !== version || info.target !== id
    || info.arch !== target.arch || info.os !== target.os || info.build_profile !== 'release'
    || typeof info.source_dirty !== 'boolean' || !validHash(info.source_fingerprint) || !validHash(info.frontend_sha256)
    || !validHash(info.desktop_sha256) || !validHash(info.backend_sha256)) {
    throw new Error('Build identity did not match target or revision for ' + id);
  }
  if (formal && (info.source_dirty || !/^\d+\.\d+\.\d+$/.test(info.version))) {
    throw new Error('Expected a clean numeric release build for ' + id);
  }
  if (info.source_revision !== revision
    && (!formal || info.source_dirty || commitTree(info.source_revision) !== tree)) {
    throw new Error('Build source does not match the checked out source tree for ' + id);
  }
  if (!info.codelens_runtime || !info.rea_runtime || info.runtime_revision < 2) {
    throw new Error('Missing private runtimes in ' + id);
  }
  if (formal && target.os === 'macos'
    && (info.signing?.state !== 'developer-id-notarized' || !info.signing.app_submission_id || !info.signing.dmg_submission_id)) {
    throw new Error('Missing Developer ID signature, notarization and stapling records for ' + id);
  }
  const seen = new Set();
  for (const line of (await readFile(manifestPath, 'utf8')).trim().split(/\r?\n/)) {
    const match = line.match(/^([a-f0-9]{64})\s+\*?([^/\\]+)$/i);
    if (!match || !scope.includes(match[2]) || seen.has(match[2])) {
      throw new Error('Unexpected or duplicate checksum record in ' + id);
    }
    if (match[1].toLowerCase() !== await sha256(join(dir, match[2]))) throw new Error('Asset hash mismatch in ' + id);
    seen.add(match[2]);
  }
  if (scope.some(name => !seen.has(name))) throw new Error('Missing assets for ' + id);
  const forbidden = (await readdir(dir)).filter(name => !scope.includes(name) && name !== 'SHA256SUMS.txt');
  if (forbidden.length) throw new Error('Unexpected assets for ' + id + ': ' + forbidden.join(', '));
  return { assets: expected, sourceRevision: info.source_revision, fingerprint: info.source_fingerprint };
}

export function deliveryPlan(platforms = 'all', formal = false) {
  if (!['all', 'windows-linux'].includes(platforms)) throw new Error('Unknown delivery platform selection');
  if (formal && platforms !== 'all') throw new Error('Formal packaging requires all platforms');
  const targets = platforms === 'all' ? Object.keys(manifest.targets) : ['windows-x64', 'linux-x64'];
  const include = targets.filter(id => id !== 'windows-x64').map(id => {
    const { runner, os, arch } = manifest.targets[id];
    return { target: id, runner, os: os === 'linux' ? 'Linux' : 'macOS', arch: arch.toUpperCase(),
      abi: os === 'linux' ? 'jammy-glibc235' : 'darwin-13.5' };
  });
  return { targets, matrix: { include } };
}

export async function aggregate([input, output, ...flags]) {
  const formal = flags.length === 2 && flags[0] === '--formal-tag';
  const limited = flags.length === 1 && flags[0] === '--windows-linux-preview';
  if (!input || !output || flags.length && !formal && !limited) {
    throw new Error('Usage: node scripts/desktop.mjs aggregate <artifact-root> <output> [--formal-tag vVERSION | --windows-linux-preview]');
  }
  const { targets } = deliveryPlan(limited ? 'windows-linux' : 'all', formal);
  const tag = flags[1];
  const revision = run('git', ['rev-parse', 'HEAD'], { capture: true });
  const tree = commitTree(revision);
  if (formal) {
    verifyReleaseRef(version, tag, true);
    const taggedRevision = run('git', ['rev-parse', '--verify', 'refs/tags/' + tag + '^{}'], { capture: true });
    if (taggedRevision !== revision) throw new Error('Current source revision does not match the tag');
  }
  const candidates = (await files(resolve(input))).filter(path => basename(path) === 'build-info.json');
  const buildDirs = new Map();
  for (const path of candidates) {
    if (!(await lstat(path)).isFile()) throw new Error('Unexpected manifest link ' + path);
    const info = await json(path);
    if (!targets.includes(info.target) || buildDirs.has(info.target)) {
      throw new Error('Unexpected or duplicate target manifest ' + path);
    }
    buildDirs.set(info.target, dirname(path));
  }
  const missing = targets.filter(id => !buildDirs.has(id));
  if (missing.length) throw new Error('Missing target manifests: ' + missing.join(', '));
  const destination = resolve(output);
  if (await exists(destination)) throw new Error('Aggregation destination already exists ' + destination);
  await mkdir(dirname(destination), { recursive: true });
  const stage = await mkdtemp(join(dirname(destination), '.aggregate-'));
  const all = [];
  const sources = {};
  try {
    for (const id of targets) {
      const target = manifest.targets[id];
      const dir = buildDirs.get(id);
      const { assets, sourceRevision, fingerprint } = await verifyTarget(dir, id, target, revision, tree, formal);
      sources[id] = { source_revision: sourceRevision, source_fingerprint: fingerprint };
      for (const asset of assets) await copyFile(join(dir, asset), join(stage, asset));
      const infoName = 'build-info-' + id + '.json';
      await copyFile(join(dir, 'build-info.json'), join(stage, infoName));
      all.push(...assets, infoName);
      if (id === 'windows-x64') {
        // Keep the legacy Windows manifest name for clients who already read it.
        await copyFile(join(dir, 'build-info.json'), join(stage, 'build-info.json'));
        all.push('build-info.json');
      }
    }
    const releaseInfo = { version, mode: formal ? 'formal-packaging' : limited ? 'windows-linux-preview' : 'preview', source_revision: revision, source_tree: tree,
      ...(formal ? { source_tag: tag } : {}), targets: sources };
    await writeFile(join(stage, 'delivery-info.json'), JSON.stringify(releaseInfo, null, 2) + '\n');
    all.push('delivery-info.json');
    await writeFile(join(stage, 'SHA256SUMS.txt'), await checksums(stage, all));
    await rename(stage, destination);
    console.log('Verified ' + targets.join(', ') + ' packages for ' + revision + ' in ' + destination);
  } finally {
    await rm(stage, { recursive: true, force: true });
  }
}
