import { copyFile, lstat, mkdir, mkdtemp, readdir, readFile, rename, rm, writeFile } from 'node:fs/promises';
import { basename, dirname, join, resolve } from 'node:path';
import { checksums, exists, files, json, manifest, packageNames, run, sha256, version } from './desktop-platform.mjs';

function validHash(hash) {
  return typeof hash === 'string' && /^[a-f0-9]{64}$/i.test(hash);
}

async function verifyTarget(dir, id, target, revision, formal) {
  const expected = Object.values(packageNames(target));
  const scope = [...expected, 'build-info.json'];
  const manifestPath = join(dir, 'SHA256SUMS.txt');
  for (const name of [...scope, 'SHA256SUMS.txt']) {
    if (!(await lstat(join(dir, name))).isFile()) throw new Error('Non-regular release file: ' + id + '/' + name);
  }
  const info = await json(join(dir, 'build-info.json'));
  if (info.version !== version || info.source_revision !== revision || info.target !== id
    || info.arch !== target.arch || info.os !== target.os || info.build_profile !== 'release'
    || typeof info.source_dirty !== 'boolean' || !validHash(info.source_fingerprint) || !validHash(info.frontend_sha256)
    || !validHash(info.desktop_sha256) || !validHash(info.backend_sha256)) {
    throw new Error('Build identity did not match target or revision for ' + id);
  }
  if (formal && (info.source_dirty || !/^\d+\.\d+\.\d+$/.test(info.version))) {
    throw new Error('Expected a clean numeric release build for ' + id);
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
  return expected;
}

export async function aggregate([input, output, ...flags]) {
  if (!input || !output || (flags.length && (flags.length !== 2 || flags[0] !== '--formal-tag'))) {
    throw new Error('Usage: node scripts/desktop.mjs aggregate <artifact-root> <output> [--formal-tag vVERSION]');
  }
  const formal = flags.length === 2;
  const tag = flags[1];
  let revision = run('git', ['rev-parse', 'HEAD'], { capture: true });
  if (formal) {
    if (!/^\d+\.\d+\.\d+$/.test(version) || tag !== 'v' + version) throw new Error('Tag does not match VERSION');
    const taggedRevision = run('git', ['rev-parse', '--verify', 'refs/tags/' + tag + '^{}'], { capture: true });
    if (taggedRevision !== revision) throw new Error('Current source revision does not match the tag');
    revision = taggedRevision;
  }
  const candidates = (await files(resolve(input))).filter(path => basename(path) === 'build-info.json');
  const buildDirs = new Map();
  for (const path of candidates) {
    if (!(await lstat(path)).isFile()) throw new Error('Unexpected manifest link ' + path);
    const info = await json(path);
    if (!Object.hasOwn(manifest.targets, info.target) || buildDirs.has(info.target)) {
      throw new Error('Unexpected or duplicate target manifest ' + path);
    }
    buildDirs.set(info.target, dirname(path));
  }
  if (buildDirs.size !== Object.keys(manifest.targets).length) throw new Error('Each of the four targets must be present');
  const destination = resolve(output);
  if (await exists(destination)) throw new Error('Aggregation destination already exists ' + destination);
  await mkdir(dirname(destination), { recursive: true });
  const stage = await mkdtemp(join(dirname(destination), '.aggregate-'));
  const all = [];
  try {
    for (const [id, target] of Object.entries(manifest.targets)) {
      const dir = buildDirs.get(id);
      const assets = await verifyTarget(dir, id, target, revision, formal);
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
    await writeFile(join(stage, 'SHA256SUMS.txt'), await checksums(stage, all));
    await rename(stage, destination);
    console.log('Verified four target packages for ' + revision + ' in ' + destination);
  } finally {
    await rm(stage, { recursive: true, force: true });
  }
}
