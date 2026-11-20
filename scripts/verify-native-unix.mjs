import { spawn } from 'node:child_process';
import { chown, mkdir, mkdtemp, readFile, readdir, rename, rm, symlink } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { basename, join, resolve } from 'node:path';
import { setTimeout as delay } from 'node:timers/promises';
import { exists, nativeTarget, packageNames, run, runtimeEnv } from './desktop-platform.mjs';

const [directory, mode, uidText, gidText] = process.argv.slice(2);
if (!directory) throw new Error('Expected a versioned native package directory');
const target = nativeTarget();
if (target.os === 'windows') throw new Error('Use scripts/verify-desktop.cjs on Windows');
const names = packageNames(target);
const offline = mode === '--linux-netns';
const uid = offline ? Number(uidText) : process.getuid();
const gid = offline ? Number(gidText) : process.getgid();
if (offline) {
  if (target.os !== 'linux' || process.getuid() !== 0 || !uid || !gid) {
    throw new Error('Offline smoke requires a new Linux network namespace and an unprivileged desktop UID and GID');
  }
  const links = run('/usr/sbin/ip', ['-brief', 'link', 'show'], { capture: true });
  if (!links.startsWith('lo ') || links.includes('\n')) throw new Error('Expected a network namespace with loopback only');
  run('/usr/sbin/ip', ['link', 'set', 'lo', 'up']);
}

function processSnapshot() {
  const entries = run('ps', ['-axo', 'pid=,ppid=,stat=,comm='], { capture: true }).split(/\r?\n/);
  return entries.flatMap(line => {
    const match = line.match(/^\s*(\d+)\s+(\d+)\s+(\S+)\s+(.+)$/);
    return match && !match[3].startsWith('Z') ? [{ pid: Number(match[1]), parent: Number(match[2]), executable: match[4] }] : [];
  });
}

function descendants(pid, snapshot) {
  const children = snapshot.filter(entry => entry.parent === pid);
  return children.flatMap(child => [child, ...descendants(child.pid, snapshot)]);
}

function processNamed(entry, name) {
  const actual = basename(entry.executable);
  return actual === name || (target.os === 'linux' && actual === name.slice(0, 15));
}

async function smoke(label, executable, cwd, state, overrides = {}) {
  const data = join(state, 'user data');
  await mkdir(data);
  await chown(data, uid, gid);
  const log = join(data, 'desktop-services.log');
  const env = {
    ...runtimeEnv(), ...overrides, NOOSPHERE_DATA_DIR: data, TMPDIR: state,
    NOOSPHERE_RUNTIME_DIAGNOSTICS: '1',
  };
  if (offline && process.env.SUDO_USER) {
    const home = run('getent', ['passwd', process.env.SUDO_USER], { capture: true }).split(':')[5];
    if (!home) throw new Error('Could not identify desktop account home');
    env.HOME = home;
    env.USER = process.env.SUDO_USER;
    env.LOGNAME = process.env.SUDO_USER;
  }
  const child = spawn(executable, [], { cwd, env, uid, gid, stdio: 'inherit' });
  const exited = new Promise(resolve => {
    child.once('error', resolve);
    child.once('exit', (code, signal) => resolve(code ?? signal));
  });
  let owned = [];
  try {
    const deadline = Date.now() + 120_000;
    while (true) {
      const earlyExit = await Promise.race([exited, delay(300, false)]);
      if (earlyExit !== false) throw new Error(label + ' exited early: ' + earlyExit);
      const logText = await readFile(log, 'utf8').catch(error => {
        if (error.code === 'ENOENT') return '';
        throw error;
      });
      if (logText.includes('Noosphere Backend ready') && logText.includes('[codelens] worker ready')) break;
      if (Date.now() >= deadline) throw new Error(label + ' backend and private CodeLens worker did not become healthy');
    }
    owned = descendants(child.pid, processSnapshot());
    if (!owned.some(entry => processNamed(entry, 'noosphere-backend'))
      || !owned.some(entry => /^python3(?:\.\d+)?$/.test(basename(entry.executable)))) {
      throw new Error(label + ' did not start the expected private backend and CodeLens process tree');
    }
  } catch (error) {
    const logText = await readFile(log, 'utf8').catch(problem => `<unavailable: ${problem.code ?? problem.message}>`);
    console.error(label + ' desktop-services.log:\n' + logText.slice(-16000));
    if (child.pid) {
      try {
        owned = descendants(child.pid, processSnapshot());
        console.error(label + ' owned processes:', owned);
      } catch (snapshotError) {
        console.error(label + ' process snapshot failed:', snapshotError.message);
      }
    }
    throw error;
  } finally {
    if (child.pid && child.exitCode === null && child.signalCode === null) {
      const imageDesktop = owned.find(entry => processNamed(entry, 'noosphere-desktop'));
      if (imageDesktop) {
        try { process.kill(imageDesktop.pid, 'SIGTERM'); }
        catch (error) { if (error.code !== 'ESRCH') throw error; }
        await Promise.race([exited, delay(4000)]);
      }
      child.kill('SIGTERM');
      if (await Promise.race([exited, delay(10_000, false)]) === false) {
        child.kill('SIGKILL');
        await exited;
      }
    }
  }
  const deadline = Date.now() + 15_000;
  while (true) {
    const alive = processSnapshot().filter(entry =>
      owned.some(previous => entry.pid === previous.pid && entry.executable === previous.executable));
    if (!alive.length) break;
    if (Date.now() >= deadline) throw new Error(label + ' left owned descendants alive: ' + alive.map(entry => entry.pid).join(', '));
    await delay(250);
  }
  console.log(label + ': private backend and CodeLens boot plus descendant cleanup passed'
    + (offline ? ' in a loopback-only network namespace' : ''));
}

async function appExecutable(app) {
  const binary = run('/usr/libexec/PlistBuddy', ['-c', 'Print CFBundleExecutable', join(app, 'Contents/Info.plist')], { capture: true });
  return join(app, 'Contents/MacOS', binary);
}

async function onlyApp(dir) {
  const apps = (await readdir(dir)).filter(name => name.endsWith('.app'));
  if (apps.length !== 1) throw new Error('Expected exactly one app in ' + dir);
  return join(dir, apps[0]);
}

async function stateDir(root, name) {
  const dir = join(root, name);
  await mkdir(dir);
  await chown(dir, uid, gid);
  return dir;
}

const scratch = await mkdtemp(join(tmpdir(), 'noosphere-native-'));
try {
  await chown(scratch, uid, gid);
  const packages = resolve(directory);
  if (target.os === 'linux') {
    const deb = await stateDir(scratch, 'relocated deb');
    run('dpkg-deb', ['--extract', join(packages, names.deb), deb]);
    await smoke('Relocated deb', join(deb, 'usr/bin/noosphere-desktop'), deb, await stateDir(scratch, 'deb state'));
    // AppImage bootstrap may use filesystem helpers. Its PATH contains neither a host Node nor a host Python.
    const utilityPath = await stateDir(scratch, 'appimage utilities');
    for (const command of ['awk', 'basename', 'bash', 'cat', 'cut', 'dirname', 'env', 'find', 'grep', 'head', 'ln', 'locale',
      'ls', 'mkdir', 'mktemp', 'printenv', 'readlink', 'realpath', 'rm', 'sed', 'sh', 'sort', 'stat', 'tr', 'uname', 'which']) {
      const system = join('/usr/bin', command);
      if (await exists(system)) await symlink(system, join(utilityPath, command));
    }
    const image = await stateDir(scratch, 'relocated appimage');
    const imageFile = join(image, names.appimage);
    run('cp', ['-p', join(packages, names.appimage), imageFile]);
    await smoke('Final AppImage (extract-and-run)', imageFile, image, await stateDir(scratch, 'image state'), {
      APPIMAGE_EXTRACT_AND_RUN: '1', PATH: utilityPath,
    });
  } else {
    const archive = await stateDir(scratch, 'archive extraction');
    run('tar', ['-xzf', join(packages, names.app), '-C', archive]);
    const moved = join(scratch, 'Relocated Noosphere 搬迁.app');
    await rename(await onlyApp(archive), moved);
    await smoke('Relocated app archive', await appExecutable(moved), scratch, await stateDir(scratch, 'archive state'));
    const mount = await stateDir(scratch, 'read-only DMG');
    run('hdiutil', ['attach', '-nobrowse', '-readonly', '-mountpoint', mount, join(packages, names.dmg)]);
    try {
      await smoke('Mounted final DMG app', await appExecutable(await onlyApp(mount)), scratch, await stateDir(scratch, 'DMG state'));
    } finally {
      run('hdiutil', ['detach', mount]);
    }
  }
} finally {
  await rm(scratch, { recursive: true, force: true });
}
