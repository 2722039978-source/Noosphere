import { spawnSync } from 'node:child_process';
import { createHash, randomUUID } from 'node:crypto';
import { createReadStream, createWriteStream } from 'node:fs';
import {
  cp, lstat, mkdir, readFile, readdir, rename, writeFile, mkdtemp, rm, realpath,
} from 'node:fs/promises';
import { basename, dirname, isAbsolute, join, relative, resolve, sep } from 'node:path';
import { pipeline } from 'node:stream/promises';
import { fileURLToPath } from 'node:url';

export const root = fileURLToPath(new URL('../', import.meta.url));
export const frontend = join(root, 'frontend');
export const backend = join(root, 'backend');
export const tauriRoot = join(root, 'desktop/src-tauri');
export const tauriCli = join(root, 'desktop/node_modules/@tauri-apps/cli/tauri.js');
export const manifest = await json(join(root, 'scripts/platform-targets.json'));
export const version = (await readFile(join(root, 'VERSION'), 'utf8')).trim();
export const node = process.execPath;
export const npm = process.platform === 'win32'
  ? [node, [join(dirname(node), 'node_modules/npm/bin/npm-cli.js')]] : ['npm', []];

export async function json(path) {
  return JSON.parse(await readFile(path, 'utf8'));
}

export async function exists(path) {
  try { await lstat(path); return true; }
  catch (error) {
    if (error.code === 'ENOENT') return false;
    throw error;
  }
}

export function run(command, args, { cwd = root, env = process.env, capture = false, timeout = 0 } = {}) {
  const result = spawnSync(command, args, {
    cwd, env, encoding: 'utf8', maxBuffer: 64 * 1024 * 1024,
    stdio: capture ? ['ignore', 'pipe', 'pipe'] : 'inherit', timeout,
  });
  if (result.error) throw result.error;
  if (result.status !== 0) throw new Error(
    command + ' failed (' + (result.signal || result.status) + ')'
      + (result.stderr ? '\n' + result.stderr.trim() : ''),
  );
  return result.stdout?.trim() ?? '';
}

export function nativeTarget(id) {
  const hostOS = { win32: 'windows', linux: 'linux', darwin: 'macos' }[process.platform];
  const host = hostOS + '-' + process.arch;
  if (id && host !== id) throw new Error('Target ' + id + ' requires its native host. Found ' + host);
  const target = manifest.targets[host];
  if (!target) throw new Error('Unsupported desktop build host ' + host);
  return { ...target, id: host };
}

export async function sha256(path) {
  const hash = createHash('sha256');
  for await (const chunk of createReadStream(path)) hash.update(chunk);
  return hash.digest('hex');
}

export async function files(dir, filterDirectory = () => true) {
  const entries = [];
  for (const item of await readdir(dir, { withFileTypes: true })) {
    const path = join(dir, item.name);
    if (item.isDirectory() && filterDirectory(item.name)) entries.push(...await files(path, filterDirectory));
    if (item.isFile() || item.isSymbolicLink()) entries.push(path);
  }
  return entries;
}

async function treeHash(dir, names) {
  const records = [];
  for (const name of names) {
    const file = join(dir, name);
    const hash = (await exists(file)) ? (await sha256(file)).toUpperCase() : 'MISSING';
    records.push(name + '\t' + hash);
  }
  return createHash('sha256').update(records.sort().join('\n')).digest('hex').toUpperCase();
}

export const frontendHash = async () => treeHash(join(frontend, 'dist'),
  (await files(join(frontend, 'dist'))).map(path => relative(join(frontend, 'dist'), path).split(sep).join('/')));

export async function sourceIdentity() {
  const revision = run('git', ['rev-parse', 'HEAD'], { capture: true });
  const dirty = Boolean(run('git', ['status', '--porcelain', '--untracked-files=normal'], { capture: true }));
  const listed = run('git', ['ls-files', '-coz', '--exclude-standard'], { capture: true });
  const names = [...new Set(listed.split('\0'))].filter(name =>
    /^(backend\/|frontend\/|desktop\/|scripts\/|\.cargo\/|VERSION$|\.node-version$|\.python-version$|rust-toolchain\.toml$|setup\.bat$|\.gitignore$|\.gitattributes$)/.test(name));
  return { revision, dirty, fingerprint: await treeHash(root, names) };
}

export async function verifiedArchive(component, cache) {
  const path = join(cache, component.archive);
  if (!await exists(path)) {
    const temporary = path + '.' + randomUUID() + '.download';
    try {
      const reply = await fetch(component.url, { signal: AbortSignal.timeout(180_000) });
      if (!reply.ok || !reply.body) throw new Error('Download failed with HTTP ' + reply.status + ' for ' + component.url);
      await pipeline(reply.body, createWriteStream(temporary, { flags: 'wx' }));
      if (await sha256(temporary) !== component.source_sha256) throw new Error('Source hash mismatch for ' + component.archive);
      await rename(temporary, path);
    } finally {
      await rm(temporary, { force: true });
    }
  }
  if (await sha256(path) !== component.source_sha256) throw new Error('Cached archive hash mismatch ' + path);
  return path;
}

function contained(base, path) {
  const rest = relative(base, path);
  return rest !== '..' && !rest.startsWith('..' + sep) && !isAbsolute(rest);
}

export async function checkResourceLinks(base) {
  const canonical = await realpath(base);
  for (const file of await files(base)) {
    const target = await realpath(file);
    if (!contained(canonical, target) || !(await lstat(target)).isFile()) {
      throw new Error('Non-file or escaping runtime resource link ' + file);
    }
  }
}

export function runtimeEnv() {
  const env = { ...process.env, PATH: '', PYTHONNOUSERSITE: '1', PYTHONDONTWRITEBYTECODE: '1', PYTHON_JIT: '0' };
  for (const key of Object.keys(env)) {
    if (/^(PYTHON|DYLD_|LD_|NODE_|NPM_CONFIG_)/i.test(key)
        && !['PYTHONNOUSERSITE', 'PYTHONDONTWRITEBYTECODE', 'PYTHON_JIT'].includes(key)) delete env[key];
  }
  return env;
}

export async function verifyRuntimes(target, resources, cwd = resources, inspectGo = false) {
  const { python, codelens, node: nodeComponent, rea, go } = target.components;
  const env = runtimeEnv();
  run(join(resources, python.entry), [
    '-IB', join(root, 'scripts/codelens-runtime/verify.py'), dirname(join(resources, codelens.entry)), target.id,
  ], { cwd, env });
  const installedNode = join(resources, nodeComponent.entry);
  const arch = run(installedNode, ['-p', 'process.platform+" "+process.arch'], { cwd, env, capture: true });
  if (arch !== target.node_os + ' ' + target.node_arch) throw new Error('Unexpected bundled Node target ' + arch);
  const actualNode = run(installedNode, ['--version'], { cwd, env, capture: true });
  const actualRea = run(installedNode, [join(resources, rea.entry), '--version'], { cwd, env, capture: true });
  if (actualNode !== 'v' + nodeComponent.version || actualRea !== rea.version) {
    throw new Error('Unexpected private Node/REA versions ' + actualNode + ' / ' + actualRea);
  }
  if (inspectGo) {
    const sample = join(resources, go.entry);
    const parsed = JSON.parse(run(installedNode, [join(resources, rea.entry),
      'inspect-go-binary', sample, '--json'], { cwd, env, capture: true, timeout: 30_000 }));
    if (parsed.operation !== 'inspect_go_binary' || parsed.subject?.digest?.sha256 !== await sha256(sample)
        || !parsed.normalized_result) throw new Error('REA did not identify the packaged Go binary');
  }
  console.log('Bundled Node ' + actualNode + ' / REA ' + actualRea + ' verified without host interpreters on PATH');
}

export async function stageUnixResources(target, cache, stage) {
  const { codelens, python, node: nodeComponent, rea } = target.components;
  const [pythonArchive, nodeArchive] = await Promise.all([
    verifiedArchive(python, cache), verifiedArchive(nodeComponent, cache),
  ]);
  const pythonRuntime = join(stage, 'codelens-runtime');
  const reaRuntime = join(stage, 'rea-runtime');
  const pythonSources = dirname(join(stage, codelens.entry));
  const nodeExtract = await mkdtemp(join(cache, 'node-extract-'));

  try {
    await Promise.all([mkdir(pythonRuntime), mkdir(reaRuntime), mkdir(pythonSources)]);
    run('tar', ['-xzf', pythonArchive, '--strip-components', '1', '-C', pythonRuntime]);
    run('tar', ['-xJf', nodeArchive, '--strip-components', '1', '-C', nodeExtract]);
    await checkResourceLinks(pythonRuntime);
    const lock = join(root, python.lock);
    const lockPins = await readFile(lock, 'utf8');
    const requirements = await readFile(join(root, codelens.source, 'requirements.txt'), 'utf8');
    for (const pin of requirements.split(/\r?\n/).map(line => line.trim()).filter(line => line && !line.startsWith('#'))) {
      if (!lockPins.split(/\r?\n/).some(line => line.startsWith(pin + ' --hash='))) {
        throw new Error(target.id + ' wheel lock missing ' + pin);
      }
    }
    const installedPython = join(stage, python.entry);
    const installedVersion = run(installedPython, ['-IB', '-c', 'import platform; print(platform.python_version())'], { capture: true, env: runtimeEnv() });
    if (installedVersion !== python.version) throw new Error('Unexpected bundled CPython version ' + installedVersion);
    const sitePackages = run(installedPython, ['-IB', '-c', 'import sysconfig; print(sysconfig.get_path("purelib"))'], { capture: true, env: runtimeEnv() });
    if (!contained(pythonRuntime, sitePackages)) throw new Error('Private Python points outside its installation');
    const buildEnv = { ...runtimeEnv(), PATH: process.env.PATH };
    run(installedPython, ['-IBm', 'pip', '--isolated', '--disable-pip-version-check', 'install',
      '--index-url', 'https://pypi.org/simple', '--only-binary=:all:', '--require-hashes', '--no-compile',
      '--cache-dir', join(cache, 'pip'), '-r', lock], { env: buildEnv });
    run(installedPython, ['-IBm', 'pip', '--isolated', '--disable-pip-version-check', 'check'], { env: buildEnv });
    for (const file of await readdir(sitePackages)) {
      if (file === 'pip' || /^pip-.*\.dist-info$/.test(file)) {
        await rm(join(sitePackages, file), { recursive: true, force: true });
      }
    }
    for (const file of await readdir(join(pythonRuntime, 'bin'))) {
      if (/^(pip[\d.]*|idle[\d.]*|pydoc[\d.]*|python[\d.]*-config|uvicorn)$/.test(file)) {
        await rm(join(pythonRuntime, 'bin', file), { force: true });
      }
    }
    const sourceRoot = join(root, codelens.source);
    for (const file of await files(sourceRoot, name => !['__pycache__', '.venv', 'venv', '.pytest_cache'].includes(name))) {
      if (basename(file).startsWith('test_') || (!file.endsWith('.py') && basename(file) !== 'requirements.txt')) continue;
      const dest = join(pythonSources, relative(sourceRoot, file));
      await mkdir(dirname(dest), { recursive: true });
      await cp(file, dest);
    }
    const nodeBinary = join(nodeExtract, 'bin/node');
    const reaLock = join(root, rea.lock);
    await cp(reaLock, join(reaRuntime, 'package-lock.json'));
    await cp(join(dirname(reaLock), 'package.json'), join(reaRuntime, 'package.json'));
    run(nodeBinary, [join(nodeExtract, 'lib/node_modules/npm/bin/npm-cli.js'), 'ci',
      '--ignore-scripts', '--omit=optional', '--no-audit', '--no-fund', '--cache', join(cache, 'npm')],
    { cwd: reaRuntime, env: buildEnv });
    // The typed artifact inspectors do not use REA's Windows process addon.
    await rm(join(reaRuntime, 'node_modules/rea-agents/native/windows'), { recursive: true, force: true });
    await cp(nodeBinary, join(stage, nodeComponent.entry));
    await cp(join(nodeExtract, 'LICENSE'), join(reaRuntime, 'NODE-LICENSE.txt'));
    await writeFile(join(pythonRuntime, 'distribution.json'), JSON.stringify({
      python: python.version, archive_sha256: python.source_sha256, lock_sha256: await sha256(lock),
    }));
    await writeFile(join(reaRuntime, 'distribution.json'), JSON.stringify({
      node: 'v' + nodeComponent.version, rea: rea.version, lock_sha256: await sha256(reaLock),
      archive_sha256: nodeComponent.source_sha256,
    }));
    await cp(join(backend, 'configs/config.yaml'), join(stage, 'config.defaults.yaml'));
    await checkResourceLinks(pythonRuntime);
    await verifyRuntimes(target, stage);
  } finally {
    await rm(nodeExtract, { recursive: true, force: true });
  }
}

export function packageNames(target) {
  return Object.fromEntries(Object.entries(target.packages).map(([type, name]) => [
    type, name.replace('{version}', version),
  ]));
}

export async function checksums(dir, names) {
  return (await Promise.all(names.map(async name => (await sha256(join(dir, name))) + '  ' + name))).join('\n') + '\n';
}
