import { chmod, mkdir, readFile, readdir, stat, writeFile } from 'node:fs/promises';
import { basename, dirname, join } from 'node:path';
import { pathToFileURL } from 'node:url';
import { backend, exists, frontend, json, nativeTarget, root, run, tauriRoot } from './desktop-platform.mjs';

const legalName = /^(licen[cs]e|copying|copyright|notice|third[_-]party[_-]notices)([._-]|$)/i;

async function copyLegalText(source, destination) {
  // Go's module cache is read-only. Preserve text bytes without propagating
  // its mode, including when replacing an earlier locally staged notice.
  if (await exists(destination)) await chmod(destination, 0o644);
  await writeFile(destination, await readFile(source));
}

async function collect(dir, output) {
  if (!await exists(dir)) return [];
  const names = [];
  for (const entry of await readdir(dir)) {
    if (!legalName.test(entry)) continue;
    const path = join(dir, entry);
    const info = await stat(path);
    if (!info.isFile()) continue;
    await mkdir(output, { recursive: true });
    await copyLegalText(path, join(output, entry));
    names.push(entry);
  }
  return names;
}

function identifier(name, version) {
  return (name + '-' + version).replace(/[^-a-zA-Z0-9._]+/g, '_');
}

async function npmLicenses(dir, output) {
  const result = [];
  const lock = await json(join(dir, 'package-lock.json'));
  for (const [path, pin] of Object.entries(lock.packages)) {
    if (!path || !await exists(join(dir, path, 'package.json'))) continue;
    const pkg = await json(join(dir, path, 'package.json'));
    const dest = identifier(pkg.name, pkg.version);
    result.push({ name: pkg.name, version: pkg.version, license: pkg.license || pin.license,
      resolved: pin.resolved, files: await collect(join(dir, path), join(output, dest)) });
  }
  return result;
}

export async function collectLicenses(target, output, reaRuntime, env, go = 'go', cargo = 'cargo') {
  await mkdir(output, { recursive: true });
  const base = [
    'frontend/public/assets/RhineLabUI-LICENSE.txt',
    'frontend/public/assets/Attachment-Dependencies-LICENSE.txt',
  ];
  await copyLegalText(join(root, 'LICENSES/MIT.txt'), join(output, 'LICENSE'));
  for (const file of base) await copyLegalText(join(root, file), join(output, basename(file)));

  const goRoot = run(go, ['env', 'GOROOT'], { cwd: backend, env, capture: true });
  await collect(goRoot, join(output, 'go/toolchain'));
  const goDirs = run(go, ['list', '-mod=readonly', '-m', '-f', '{{.Path}}|{{.Version}}|{{.Dir}}', 'all'], {
    cwd: backend, env, capture: true,
  });
  const modules = [];
  for (const line of goDirs.split(/\r?\n/)) {
    const [name, version, dir] = line.split('|');
    if (!version || !dir) continue;
    const files = await collect(dir, join(output, 'go', identifier(name, version)));
    modules.push({ name, version, files });
  }

  const metadata = JSON.parse(run(cargo, ['metadata', '--format-version', '1', '--locked',
    '--filter-platform', target.rust_triple], { cwd: tauriRoot, env, capture: true }));
  const crates = [];
  for (const pkg of metadata.packages) {
    if (!pkg.source) continue;
    const dir = dirname(pkg.manifest_path);
    const dest = join(output, 'rust', identifier(pkg.name, pkg.version));
    const files = await collect(dir, dest);
    if (pkg.license_file && !files.includes(pkg.license_file)) {
      await mkdir(dest, { recursive: true });
      await copyLegalText(join(dir, pkg.license_file), join(dest, basename(pkg.license_file)));
      files.push(basename(pkg.license_file));
    }
    crates.push({ name: pkg.name, version: pkg.version, license: pkg.license, source: pkg.source, files });
  }

  const index = {
    target: target.id,
    source_licenses: ['LICENSE', ...base.map(file => basename(file))],
    bundled_runtimes: {
      node: 'rea-runtime/NODE-LICENSE.txt',
      python: 'codelens-runtime upstream archive licenses and wheel .dist-info/licenses directories',
      rea: 'rea-runtime/node_modules/rea-agents/THIRD_PARTY_NOTICES.md and licenses/',
    },
    go_modules: modules,
    rust_crates: crates,
    frontend_packages: await npmLicenses(frontend, join(output, 'frontend')),
    rea_packages: await npmLicenses(reaRuntime, join(output, 'rea')),
  };
  if (target.os === 'linux' && await exists('/usr/share/doc')) {
    const installed = await readdir('/usr/share/doc');
    for (const name of installed) {
      const copyright = join('/usr/share/doc', name, 'copyright');
      if (!await exists(copyright)) continue;
      await mkdir(join(output, 'system', name), { recursive: true });
      await copyLegalText(copyright, join(output, 'system', name, 'copyright'));
    }
    // Ubuntu copyright notices refer to these texts by path. An AppImage
    // must carry them even when the host is a different distribution.
    const common = '/usr/share/common-licenses';
    const texts = join(output, 'system', 'common-licenses');
    await mkdir(texts, { recursive: true });
    const commonLicenses = [];
    for (const name of (await readdir(common)).sort()) {
      const file = join(common, name);
      if (!(await stat(file)).isFile()) continue;
      await copyLegalText(file, join(texts, name));
      commonLicenses.push(name);
    }
    index.linux_common_licenses = commonLicenses;
    index.linux_build_system_notices = 'system/ is a superset of source distribution notices for bundled AppImage libraries';
    const release = await readFile('/etc/os-release', 'utf8');
    await writeFile(join(output, 'system/os-release'), release);
  }
  await writeFile(join(output, 'THIRD-PARTY.json'), JSON.stringify(index, null, 2) + '\n');
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  const [output, reaRuntime, go, cargo] = process.argv.slice(2);
  if (!output || !reaRuntime || !go || !cargo) throw new Error('Windows license staging requires explicit paths');
  await collectLicenses(nativeTarget('windows-x64'), output, reaRuntime, process.env, go, cargo);
}
