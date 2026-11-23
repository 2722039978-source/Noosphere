import { spawn } from 'node:child_process';
import { chown, mkdir, mkdtemp, readFile, readdir, realpath, rename, rm, stat, symlink } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { basename, isAbsolute, join, resolve } from 'node:path';
import { setTimeout as delay } from 'node:timers/promises';
import { exists, json, nativeTarget, packageNames, run, runtimeEnv, sha256, version } from './desktop-platform.mjs';
import { verifyMacBundleVersions } from './desktop-macos.mjs';

const [directory, mode, firstOption, secondOption] = process.argv.slice(2);
if (!directory) throw new Error('Expected a versioned native package directory');
const target = nativeTarget();
if (target.os === 'windows') throw new Error('Use scripts/verify-desktop.cjs on Windows');
const names = packageNames(target);
const offline = mode === '--linux-netns';
const installed = mode === '--linux-installed';
const wayland = mode === '--linux-wayland';
const appImageFuse = mode === '--linux-appimage-fuse';
const uid = offline ? Number(firstOption) : process.getuid();
const gid = offline ? Number(secondOption) : process.getgid();
if (mode && !offline && !installed && !wayland && !appImageFuse) throw new Error('Unknown native verification mode');
if (appImageFuse && (target.os !== 'linux' || !process.env.DISPLAY || process.env.APPIMAGE_EXTRACT_AND_RUN)) {
  throw new Error('The AppImage FUSE check requires Linux X11 with regular AppImage execution');
}
if (wayland && (target.os !== 'linux' || process.env.DISPLAY || process.env.GDK_BACKEND !== 'wayland'
  || !process.env.WAYLAND_DISPLAY || !process.env.XDG_RUNTIME_DIR)) {
  throw new Error('Wayland verification requires a Linux compositor and Wayland-only GTK session');
}
if (installed && (target.os !== 'linux' || !firstOption || !isAbsolute(firstOption) || !secondOption)) {
  throw new Error('Installed Debian smoke requires a Linux host, an absolute fixture data path and the expected app version');
}
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

async function smoke(label, executable, cwd, state, {
  overrides = {}, hardExit = false, data = join(state, 'user data'), backendVersion, verifyFuse = false,
} = {}) {
  await mkdir(data, { recursive: true });
  await chown(data, uid, gid);
  const log = join(data, 'desktop-services.log');
  const logStart = await stat(log).then(info => info.size, error => {
    if (error.code === 'ENOENT') return 0;
    throw error;
  });
  const env = {
    ...runtimeEnv(), ...overrides, NOOSPHERE_DATA_DIR: data, TMPDIR: state,
    NOOSPHERE_RUNTIME_DIAGNOSTICS: '1',
  };
  if (wayland) env.WAYLAND_DEBUG = 'client';
  if (offline && process.env.SUDO_USER) {
    const home = run('getent', ['passwd', process.env.SUDO_USER], { capture: true }).split(':')[5];
    if (!home) throw new Error('Could not identify desktop account home');
    env.HOME = home;
    env.USER = process.env.SUDO_USER;
    env.LOGNAME = process.env.SUDO_USER;
  }
  const child = spawn(executable, [], { cwd, env, uid, gid, stdio: ['inherit', 'inherit', wayland ? 'pipe' : 'inherit'] });
  const exited = new Promise(resolve => {
    child.once('error', resolve);
    child.once('exit', (code, signal) => resolve(code ?? signal));
  });
  let waylandTrace = '';
  let hasToplevel = false;
  let hasBuffer = false;
  if (wayland) child.stderr.on('data', bytes => {
    waylandTrace = (waylandTrace + bytes.toString('utf8')).slice(-48000);
    hasToplevel ||= /xdg_surface@\d+\.get_toplevel\(/.test(waylandTrace);
    hasBuffer ||= /wl_surface@\d+\.attach\(wl_buffer@\d+/.test(waylandTrace);
  });
  let owned = [];
  try {
    const deadline = Date.now() + 120_000;
    while (true) {
      const earlyExit = await Promise.race([exited, delay(300, false)]);
      if (earlyExit !== false) throw new Error(label + ' exited early: ' + earlyExit);
      const logText = await readFile(log).then(bytes => bytes.subarray(logStart).toString('utf8')).catch(error => {
        if (error.code === 'ENOENT') return '';
        throw error;
      });
      if (logText.includes('Noosphere Backend ready') && logText.includes('[codelens] worker ready')) {
        if (backendVersion && !logText.includes(`=== Noosphere Backend v${backendVersion} ===`)) {
          throw new Error(label + ' started a backend with a different version');
        }
        if (!wayland || hasToplevel && hasBuffer) break;
      }
      if (Date.now() >= deadline) {
        throw new Error(label + (wayland ? ' did not present a Wayland window and healthy private services'
          : ' backend and private CodeLens worker did not become healthy'));
      }
    }
    owned = descendants(child.pid, processSnapshot());
    if (!owned.some(entry => processNamed(entry, 'noosphere-backend'))
      || !owned.some(entry => /^python3(?:\.\d+)?$/.test(basename(entry.executable)))) {
      throw new Error(label + ' did not start the expected private backend and CodeLens process tree');
    }
    if (verifyFuse) {
      const app = owned.find(entry => processNamed(entry, 'noosphere-desktop'));
      if (!app) throw new Error(label + ' lacks the mounted desktop process');
      const program = await realpath('/proc/' + app.pid + '/exe');
      const mountInfo = await readFile('/proc/' + app.pid + '/mountinfo', 'utf8');
      const mountedFromFuse = mountInfo.split('\n').some(line => {
        const [mount, device] = line.split(' - ');
        const point = mount.split(' ')[4]?.replace(/\\([0-7]{3})/g, (_, octal) => String.fromCharCode(parseInt(octal, 8)));
        return point && device?.startsWith('fuse') && program.startsWith(point + '/');
      });
      if (!mountedFromFuse) throw new Error(label + ' did not boot the desktop from a real FUSE mount');
      console.log(label + ': desktop executable is inside a kernel FUSE mount');
    }
    if (hardExit) {
      if (!child.kill('SIGKILL') || await Promise.race([exited, delay(10_000, false)]) === false) {
        throw new Error(label + ' could not stop the isolated desktop host');
      }
    }
  } catch (error) {
    const logText = await readFile(log, 'utf8').catch(problem => `<unavailable: ${problem.code ?? problem.message}>`);
    console.error(label + ' desktop-services.log:\n' + logText.slice(-16000));
    if (wayland) console.error(label + ' Wayland client trace:\n' + waylandTrace.slice(-12000));
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
    + (hardExit ? ' after hard desktop termination' : '')
    + (offline ? ' in a loopback-only network namespace' : '')
    + (wayland ? ' with a Wayland toplevel and surface buffer' : ''));
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

// Tauri rejects executable paths with symlinked ancestors on macOS, including /var.
const scratch = await mkdtemp(join(await realpath(tmpdir()), 'noosphere-native-'));
try {
  await chown(scratch, uid, gid);
  const packages = resolve(directory);
  const build = await json(join(packages, 'build-info.json'));
  if (build.target !== target.id || build.version !== version) {
    throw new Error('Package metadata does not match the native verification target and VERSION');
  }
  console.log('Native host:', run('uname', ['-srvm'], { capture: true }));
  if (!installed) {
    console.log('Build:', build.version, build.source_revision, 'source_dirty=' + build.source_dirty);
    for (const name of Object.values(names)) console.log('SHA-256', await sha256(join(packages, name)), name);
  }
  if (installed) {
    await smoke('Installed deb ' + secondOption, '/usr/bin/noosphere-desktop', scratch,
      await stateDir(scratch, 'installed state'), { data: firstOption, backendVersion: secondOption });
  } else if (target.os === 'linux') {
    if (!appImageFuse) {
      const deb = await stateDir(scratch, 'relocated deb 搬迁');
      run('dpkg-deb', ['--extract', join(packages, names.deb), deb]);
      await smoke('Relocated deb', join(deb, 'usr/bin/noosphere-desktop'), deb, await stateDir(scratch, 'deb state'));
      await smoke('Relocated deb hard exit', join(deb, 'usr/bin/noosphere-desktop'), deb,
        await stateDir(scratch, 'deb crash state'), { hardExit: true });
    }
    // AppImage bootstrap may use filesystem helpers. Its PATH contains neither a host Node nor a host Python.
    const utilityPath = await stateDir(scratch, 'appimage utilities');
    for (const command of ['awk', 'basename', 'bash', 'cat', 'cut', 'dirname', 'env', 'find', 'fusermount', 'fusermount3', 'grep', 'head', 'ln', 'locale',
      'ls', 'mkdir', 'mktemp', 'printenv', 'readlink', 'realpath', 'rm', 'sed', 'sh', 'sort', 'stat', 'tail', 'tr', 'uname', 'which']) {
      const system = join('/usr/bin', command);
      if (await exists(system)) await symlink(system, join(utilityPath, command));
    }
    const image = await stateDir(scratch, 'relocated appimage 搬迁');
    const imageFile = join(image, names.appimage);
    run('cp', ['-p', join(packages, names.appimage), imageFile]);
    const appImageLabel = appImageFuse ? 'AppImage (native FUSE)' : 'Final AppImage (extract-and-run)';
    await smoke(appImageLabel, imageFile, image, await stateDir(scratch, 'image state'), {
      overrides: appImageFuse ? { PATH: utilityPath } : { APPIMAGE_EXTRACT_AND_RUN: '1', PATH: utilityPath },
      verifyFuse: appImageFuse,
    });
  } else {
    const archive = await stateDir(scratch, 'archive extraction');
    run('tar', ['-xzf', join(packages, names.app), '-C', archive]);
    const moved = join(scratch, 'Relocated Noosphere 搬迁.app');
    await rename(await onlyApp(archive), moved);
    verifyMacBundleVersions(moved, version);
    await smoke('Relocated app archive', await appExecutable(moved), scratch, await stateDir(scratch, 'archive state'));
    await smoke('Relocated app hard exit', await appExecutable(moved), scratch, await stateDir(scratch, 'archive crash state'), { hardExit: true });
    const mount = await stateDir(scratch, 'read-only DMG');
    run('hdiutil', ['attach', '-nobrowse', '-readonly', '-mountpoint', mount, join(packages, names.dmg)]);
    try {
      const installed = join(scratch, 'Installed Noosphere 搬迁.app');
      run('ditto', [await onlyApp(mount), installed]);
      verifyMacBundleVersions(installed, version);
      run('codesign', ['--verify', '--deep', '--strict', installed]);
      if (build.signing?.state === 'developer-id-notarized') {
        run('xcrun', ['stapler', 'validate', installed]);
        run('spctl', ['--assess', '--type', 'execute', installed]);
      }
      await smoke('App installed from final DMG', await appExecutable(installed), scratch, await stateDir(scratch, 'DMG state'));
    } finally {
      run('hdiutil', ['detach', mount]);
    }
  }
} finally {
  await rm(scratch, { recursive: true, force: true });
}
