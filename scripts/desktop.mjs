import { cp, mkdir, mkdtemp, open, readFile, readdir, rename, rm, writeFile } from 'node:fs/promises';
import { basename, dirname, join, resolve } from 'node:path';
import { platform, release } from 'node:os';
import {
  backend, checksums, exists, files, frontend, frontendHash, json, manifest, nativeTarget, node, npm, packageNames,
  root, run, sha256, sourceIdentity, stageUnixResources, tauriCli, tauriRoot, verifyRuntimes, version,
} from './desktop-platform.mjs';
import { collectLicenses } from './desktop-licenses.mjs';
import { buildMacArtifacts } from './desktop-macos.mjs';
import { aggregate, debianPackageVersion, verifyReleaseRef } from './desktop-release-manifest.mjs';

const cacheRoot = join(root, 'test/cache/desktop');
const stagedNames = ['bin', 'codelens-python', 'codelens-runtime', 'rea-runtime', 'config.defaults.yaml', 'third-party'];
const cargo = 'cargo';

function requiredText(text, pattern, label) {
  const value = text.match(pattern)?.[1];
  if (!value) throw new Error('Could not read ' + label);
  return value;
}

async function pinnedVersions() {
  return {
    go: requiredText(await readFile(join(backend, 'go.mod'), 'utf8'), /^go (\d+\.\d+(?:\.\d+)?)$/m, 'Go version'),
    node: (await readFile(join(root, 'scripts/build-config/.node-version'), 'utf8')).trim(),
    python: (await readFile(join(root, 'scripts/build-config/.python-version'), 'utf8')).trim(),
    rust: requiredText(await readFile(join(root, 'desktop/rust-toolchain.toml'), 'utf8'), /^channel = "([^"]+)"$/m, 'Rust version'),
  };
}

function buildEnv(target) {
  const env = { ...process.env, GOOS: target.go_os, GOARCH: target.go_arch, CGO_ENABLED: '0', GOTOOLCHAIN: 'local' };
  if (target.deployment_target) env.MACOSX_DEPLOYMENT_TARGET = target.deployment_target;
  if (target.os === 'linux') env.APPIMAGE_EXTRACT_AND_RUN = '1';
  env.CARGO_TARGET_DIR = process.env.CARGO_TARGET_DIR
    ? resolve(process.env.CARGO_TARGET_DIR) : join(tauriRoot, 'target');
  return env;
}

async function doctor(target, env) {
  const pinned = await pinnedVersions();
  if (process.versions.node !== pinned.node) throw new Error('Install build Node ' + pinned.node);
  if (target.components.python.version !== pinned.python) throw new Error('.python-version differs from the target lock');
  const goVersion = run('go', ['version'], { capture: true, cwd: backend, env });
  if (!goVersion.endsWith('go' + pinned.go + ' ' + target.go_os + '/' + target.go_arch)) {
    throw new Error('Go version or host mismatch ' + goVersion);
  }
  const cargoVersion = run(cargo, ['-vV'], { capture: true, cwd: tauriRoot, env });
  if (!cargoVersion.includes('release: ' + pinned.rust) || !cargoVersion.includes('host: ' + target.rust_triple)) {
    throw new Error('Cargo version or host mismatch\n' + cargoVersion);
  }
  if (target.os === 'macos' && run('sysctl', ['-in', 'sysctl.proc_translated'], { capture: true }) === '1') {
    throw new Error('A Rosetta-hosted build is not native');
  }
  if (target.os === 'linux') run('pkg-config', ['--modversion', 'webkit2gtk-4.1', 'gtk+-3.0']);
  return {
    go: goVersion, cargo: cargoVersion.split('\n')[0], node: process.version,
    build_target: target.rust_triple, frontend_reused: false,
  };
}

async function checkVersions(target) {
  if (!/^\d+\.\d+\.\d+(?:[-+][A-Za-z0-9.-]+)?$/.test(version)) throw new Error('Invalid VERSION');
  for (const file of ['frontend/package.json', 'frontend/package-lock.json', 'desktop/package.json',
    'desktop/package-lock.json', 'desktop/src-tauri/tauri.conf.json']) {
    const document = await json(join(root, file));
    if (document.version !== version || (document.packages && document.packages['']?.version !== version)) {
      throw new Error(file + ' does not match VERSION');
    }
  }
  const cargoManifest = await readFile(join(tauriRoot, 'Cargo.toml'), 'utf8');
  if (requiredText(cargoManifest, /^version = "([^"]+)"/m, 'desktop Cargo package version') !== version) {
    throw new Error('desktop/src-tauri/Cargo.toml does not match VERSION');
  }
  const tauri = await json(join(tauriRoot, 'tauri.' + target.os + '.conf.json'));
  const expected = target.components.go.entry;
  if (tauri.bundle.resources[expected] !== expected) throw new Error('Tauri backend resource entry differs from the target');
  if (target.deployment_target && tauri.bundle.macOS.minimumSystemVersion !== target.deployment_target) {
    throw new Error('Tauri deployment floor differs from the target');
  }
}

async function prepare(target, env) {
  const cache = join(cacheRoot, target.id);
  await mkdir(cache, { recursive: true });
  const stage = await mkdtemp(join(cache, 'stage-'));
  try {
    await stageUnixResources(target, cache, stage);
    const binary = join(stage, target.components.go.entry);
    await mkdir(dirname(binary));
    run('go', ['build', '-mod=readonly', '-trimpath', '-ldflags', '-X main.Version=' + version + ' -s -w',
      '-o', binary, './cmd/server'], { cwd: backend, env });
    await mkdir(join(stage, 'third-party'));
    await cp(join(root, 'LICENSES/MIT.txt'), join(stage, 'third-party/LICENSE'));

    // Replace generated resources only, never configuration or user data.
    for (const name of stagedNames) {
      await rm(join(tauriRoot, name), { recursive: true, force: true });
      await rename(join(stage, name), join(tauriRoot, name));
    }
  } finally {
    await rm(stage, { recursive: true, force: true });
  }
}

async function setupNpm() {
  for (const cwd of [frontend, join(root, 'desktop')]) {
    run(npm[0], [...npm[1], 'ci', '--no-audit', '--no-fund'], { cwd });
  }
}

async function frontendBuild() {
  run(npm[0], [...npm[1], 'run', 'build'], { cwd: frontend });
}

function buildRust(target, env) {
  run(cargo, ['build', '--locked', '--release', '--target', target.rust_triple, '--bin', 'noosphere-desktop'], {
    cwd: tauriRoot, env,
  });
}

async function tests(target, env) {
  run('go', ['test', '-mod=readonly', '-p', '1', '-count=1', './...'], { cwd: backend, env });
  run('go', ['vet', '-mod=readonly', './...'], { cwd: backend, env });
  run(cargo, ['test', '--locked', '--lib', '--target', target.rust_triple], { cwd: tauriRoot, env });
  await verifyRuntimes(target, tauriRoot, root, true);
  const suite = join(frontend, 'tests');
  if (await exists(suite)) {
    const cases = (await readdir(suite)).filter(name => name.endsWith('.test.mjs')).map(name => join(suite, name));
    if (cases.length) run(node, ['--test', ...cases], { cwd: frontend });
  }
}

async function bundleArtifact(directory, extension) {
  const paths = (await readdir(directory)).filter(name => name.endsWith(extension));
  if (paths.length !== 1) throw new Error('Expected one fresh ' + extension + ' in ' + directory);
  return join(directory, paths[0]);
}

async function configureBundle(target, env, bundleDir) {
  const formats = target.os === 'macos' ? 'app' : 'deb,appimage';
  if (await exists(bundleDir)) {
    throw new Error('Use a clean bundle directory for packaging. Existing output: ' + bundleDir);
  }
  const args = [tauriCli, 'bundle', '--target', target.rust_triple, '--bundles', formats, '--ci'];
  if (target.os === 'macos') {
    args.push('--no-sign');
  } else {
    // linuxdeploy probes each extension with ldd outside the Python interpreter.
    // Resolve those dependencies from the pinned CPython distribution during bundling only.
    const pythonLib = join(dirname(dirname(join(tauriRoot, target.components.python.entry))), 'lib');
    env = { ...env, LD_LIBRARY_PATH: pythonLib, NO_STRIP: '1' };
    args.push('--verbose');
  }
  run(node, args, { cwd: tauriRoot, env });
}

async function verifyLinuxPackages(target, artifacts, names, scratch) {
  const deb = join(artifacts, names.deb);
  const image = join(artifacts, names.appimage);
  if (run('dpkg-deb', ['-f', deb, 'Architecture'], { capture: true }) !== target.go_arch) {
    throw new Error('Debian package architecture mismatch');
  }
  const expectedVersion = debianPackageVersion(version);
  if (run('dpkg-deb', ['-f', deb, 'Version'], { capture: true }) !== expectedVersion) {
    throw new Error('Debian package version mismatch');
  }
  if (expectedVersion !== version) run('dpkg', ['--compare-versions', expectedVersion, 'lt', version.split('-')[0]]);
  const moved = join(scratch, 'Linux packages 搬迁');
  const unpackedDeb = join(moved, 'deb');
  await mkdir(unpackedDeb, { recursive: true });
  run('dpkg-deb', ['--extract', deb, unpackedDeb]);
  run(image, ['--appimage-extract'], { cwd: moved });
  for (const bundle of [unpackedDeb, join(moved, 'squashfs-root')]) {
    const packagedBackend = (await files(bundle))
      .filter(path => path.endsWith('/' + target.components.go.entry));
    if (packagedBackend.length !== 1) throw new Error('Expected one packaged backend in ' + bundle);
    const resources = dirname(dirname(packagedBackend[0]));
    for (const name of ['Apache-2.0', 'GPL-2', 'LGPL-2.1']) {
      if (!await exists(join(resources, 'third-party', 'system', 'common-licenses', name))) {
        throw new Error('Linux package missing license text ' + name);
      }
    }
    await verifyRuntimes(target, resources, moved, true);
  }
}

async function copyDebianPackage(source, destination, scratch) {
  const debVersion = debianPackageVersion(version);
  if (debVersion === version) {
    await cp(source, destination);
    return;
  }
  const unpacked = join(scratch, 'deb-candidate');
  run('dpkg-deb', ['--raw-extract', source, unpacked]);
  const control = join(unpacked, 'DEBIAN/control');
  const text = await readFile(control, 'utf8');
  const fields = [...text.matchAll(/^Version: (.+)$/gm)];
  if (fields.length !== 1 || fields[0][1] !== version) throw new Error('Unexpected Tauri Debian version field');
  await writeFile(control, text.replace(fields[0][0], 'Version: ' + debVersion));
  run('dpkg-deb', ['--root-owner-group', '--build', unpacked, destination]);
}

async function packageTarget(target, env, toolchains, out, signed) {
  const hostRelease = target.os === 'linux'
    ? await readFile('/etc/os-release', 'utf8') : run('sw_vers', ['-productVersion'], { capture: true });
  if (target.os === 'linux' && !(/^ID="?ubuntu"?$/m.test(hostRelease) && /^VERSION_ID="?22\.04"?$/m.test(hostRelease))) {
    throw new Error('Build Linux distributable artifacts on the Ubuntu 22.04 ABI baseline');
  }
  const source = await sourceIdentity();
  if (signed) {
    const tag = run('git', ['rev-parse', '--verify', 'refs/tags/v' + version + '^{}'], { capture: true });
    if (target.os !== 'macos' || source.dirty || source.revision !== tag || !/^\d+\.\d+\.\d+$/.test(version)) {
      throw new Error('Signed macOS artifacts require the clean, matching numeric release tag');
    }
  }
  const outputRoot = join(out, version);
  const destination = join(outputRoot, target.id);
  if (await exists(destination)) throw new Error('Package destination already exists ' + destination);
  await mkdir(outputRoot, { recursive: true });
  const staging = await mkdtemp(join(outputRoot, '.stage-' + target.id + '-'));
  const cache = join(cacheRoot, target.id);
  await mkdir(cache, { recursive: true });
  const scratch = await mkdtemp(join(cache, 'package-'));
  try {
    const packageEnv = { ...env, CARGO_TARGET_DIR: join(cache, 'cargo-package') };
    const bundleDir = join(packageEnv.CARGO_TARGET_DIR, target.rust_triple, 'release/bundle');
    // The named target build lock is held, so only its previous generated bundle is removed.
    await rm(bundleDir, { recursive: true, force: true });
    await prepare(target, packageEnv);
    await frontendBuild();
    await collectLicenses(target, join(tauriRoot, 'third-party'), join(tauriRoot, 'rea-runtime'), packageEnv);
    buildRust(target, packageEnv);
    await configureBundle(target, packageEnv, bundleDir);
    const names = packageNames(target);
    let signing = { state: 'unsigned-linux' };
    let componentHashesBeforeSigning;
    const desktopBinary = join(dirname(bundleDir), 'noosphere-desktop');
    const backendBinary = join(tauriRoot, target.components.go.entry);
    const runtimeSources = Object.fromEntries(await Promise.all(
      Object.entries(target.components).map(async ([id, component]) => [id, {
        ...component, lock_sha256: component.lock ? await sha256(join(root, component.lock)) : undefined,
      }]),
    ));
    let desktopHash = await sha256(desktopBinary);
    let backendHash = await sha256(backendBinary);
    if (target.os === 'macos') {
      const app = await bundleArtifact(join(bundleDir, 'macos'), '.app');
      const built = await buildMacArtifacts(target, app, staging, names, scratch, signed);
      ({ signing, componentHashesBeforeSigning } = built);
      const executable = run('/usr/libexec/PlistBuddy', ['-c', 'Print CFBundleExecutable', join(built.finalApp, 'Contents/Info.plist')], { capture: true });
      desktopHash = await sha256(join(built.finalApp, 'Contents/MacOS', executable));
      backendHash = await sha256(join(built.finalApp, 'Contents/Resources', target.components.go.entry));
    } else {
      await copyDebianPackage(await bundleArtifact(join(bundleDir, 'deb'), '.deb'), join(staging, names.deb), scratch);
      await cp(await bundleArtifact(join(bundleDir, 'appimage'), '.AppImage'), join(staging, names.appimage));
      await verifyLinuxPackages(target, staging, names, scratch);
      componentHashesBeforeSigning = { desktop: desktopHash, backend: backendHash };
    }
    const after = await sourceIdentity();
    if (after.fingerprint !== source.fingerprint || after.revision !== source.revision) {
      throw new Error('Production source changed during the build');
    }
    const info = {
      version, target: target.id, os: target.os, arch: target.arch, minimum_os: target.minimum_os, abi: target.abi,
      built_at_utc: new Date().toISOString(), runtime_revision: 2, source_revision: source.revision,
      source_dirty: source.dirty || after.dirty, source_fingerprint: source.fingerprint,
      source_fingerprint_scope: 'production-v3:backend/,frontend/,desktop/,scripts/,LICENSES/,.cargo/,setup.bat,Start-Noosphere.cmd,.gitignore;git-nonignored',
      build_profile: 'release', frontend_sha256: await frontendHash(), desktop_sha256: desktopHash,
      backend_sha256: backendHash, component_hashes_before_signing: componentHashesBeforeSigning,
      codelens_runtime: await json(join(tauriRoot, 'codelens-runtime/distribution.json')),
      rea_runtime: await json(join(tauriRoot, 'rea-runtime/distribution.json')), runtime_sources: runtimeSources, signing,
      toolchains: { ...toolchains, tauri_cli: run(node, [tauriCli, '--version'], { capture: true }) },
      build_host: { platform: platform(), release: release(), os_release: hostRelease },
      artifacts: names,
      verification: {
        packaged_private_runtime_parse: 'passed', packaged_private_rea_inspection: 'passed',
        resource_relocation: 'passed', host_interpreter_path: 'removed',
        native_gui: 'pending', offline_native_startup: 'pending', installation_upgrade_restore: 'pending', real_model: 'pending',
      },
    };
    await writeFile(join(staging, 'build-info.json'), JSON.stringify(info, null, 2) + '\n');
    await writeFile(join(staging, 'SHA256SUMS.txt'), await checksums(staging, [...Object.values(names), 'build-info.json']));
    await rename(staging, destination);
    console.log('Verified package artifacts: ' + destination);
  } finally {
    await rm(staging, { recursive: true, force: true });
    await rm(scratch, { recursive: true, force: true });
  }
}

async function main(args) {
  const [command, ...options] = args;
  if (command === 'targets') {
    if (options.length) throw new Error('targets takes no arguments');
    console.log(JSON.stringify(manifest.targets, null, 2));
    return;
  }
  if (command === 'aggregate') {
    await aggregate(options);
    return;
  }
  if (command === 'check-release-ref') {
    if (options.length) throw new Error('Release ref is read from GITHUB_REF and GITHUB_REF_NAME');
    const tag = process.env.GITHUB_REF?.startsWith('refs/tags/') ? process.env.GITHUB_REF.slice('refs/tags/'.length) : '';
    verifyReleaseRef(version, tag, process.env.FORMAL === 'true');
    return;
  }
  if (!['doctor', 'setup', 'prepare', 'dev', 'build', 'test', 'package'].includes(command)) {
    throw new Error('Usage: node scripts/desktop.mjs <targets|doctor|setup|prepare|dev|build|test|package|aggregate> [target] [--out directory] [--signed]');
  }
  const explicitTarget = options[0] && !options[0].startsWith('--') ? options.shift() : undefined;
  const target = nativeTarget(explicitTarget);
  const signed = options.includes('--signed');
  let out = join(root, 'artifacts');
  for (let i = 0; i < options.length; ++i) {
    if (options[i] === '--out' && command === 'package' && options[i + 1]) out = resolve(options[++i]);
    else if (options[i] !== '--signed' || command !== 'package') throw new Error('Unexpected argument ' + options[i]);
  }
  if (target.os === 'windows') {
    const commands = { doctor: 'doctor.ps1', setup: 'setup.ps1', build: 'build.ps1', dev: 'dev.ps1', test: 'test.ps1', package: 'package.ps1' };
    if (signed || !commands[command]) throw new Error('Use the Windows PowerShell entry points for this command');
    run('pwsh', ['-NoProfile', '-File', join(root, 'scripts', commands[command]),
      ...(command === 'package' ? ['-OutputRoot', out] : [])]);
    return;
  }

  const env = buildEnv(target);
  const toolchains = await doctor(target, env);
  await checkVersions(target);
  if (command === 'doctor') {
    console.log(JSON.stringify(toolchains, null, 2));
    return;
  }
  if (command === 'setup') {
    await setupNpm();
    return;
  }
  if (!await exists(tauriCli)) throw new Error('Install the frontend and desktop dependencies with the setup command');

  await mkdir(cacheRoot, { recursive: true });
  const lockPath = join(cacheRoot, target.id + '.lock');
  const lock = await open(lockPath, 'wx');
  try {
    await lock.writeFile(String(process.pid) + '\n');
    if (command === 'package') {
      await packageTarget(target, env, toolchains, out, signed);
      return;
    }
    await prepare(target, env);
    if (command === 'prepare') return;
    if (command === 'dev') {
      run(node, [tauriCli, 'dev', '--target', target.rust_triple], { cwd: tauriRoot, env });
      return;
    }
    await frontendBuild();
    if (command === 'test') await tests(target, env);
    if (command === 'build') buildRust(target, env);
  } finally {
    await lock.close();
    await rm(lockPath, { force: true });
  }
}

await main(process.argv.slice(2)).catch(error => {
  console.error(error.message);
  process.exitCode = 1;
});
