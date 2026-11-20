import { mkdtemp, rm, writeFile } from 'node:fs/promises';
import { randomBytes } from 'node:crypto';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { node, root, run } from './desktop-platform.mjs';

if (process.platform !== 'darwin' || process.env.GITHUB_EVENT_NAME !== 'workflow_dispatch'
    || !process.env.GITHUB_REF?.startsWith('refs/tags/v')) {
  throw new Error('CI signing is restricted to an explicitly dispatched tagged macOS job');
}
const [target, ...options] = process.argv.slice(2);
const {
  APPLE_CERTIFICATE, APPLE_CERTIFICATE_PASSWORD, APPLE_SIGNING_IDENTITY,
  APPLE_ID, APPLE_PASSWORD, APPLE_TEAM_ID,
} = process.env;
if (![APPLE_CERTIFICATE, APPLE_CERTIFICATE_PASSWORD, APPLE_SIGNING_IDENTITY,
  APPLE_ID, APPLE_PASSWORD, APPLE_TEAM_ID].every(Boolean)) {
  throw new Error('Blocked: Developer ID certificate or Apple notarization credentials are missing');
}

const privateDir = await mkdtemp(join(process.env.RUNNER_TEMP || tmpdir(), 'noosphere-sign-'));
const keychain = join(privateDir, 'signing.keychain-db');
const password = randomBytes(24).toString('hex');
const certificate = join(privateDir, 'developer-id.p12');
const profile = 'noosphere-ci-notary';
let created = false;
try {
  await writeFile(certificate, Buffer.from(APPLE_CERTIFICATE, 'base64'), { mode: 0o600, flag: 'wx' });
  run('security', ['create-keychain', '-p', password, keychain]);
  created = true;
  run('security', ['unlock-keychain', '-p', password, keychain]);
  run('security', ['set-keychain-settings', '-lut', '7200', keychain]);
  run('security', ['import', certificate, '-k', keychain, '-P', APPLE_CERTIFICATE_PASSWORD, '-T', '/usr/bin/codesign']);
  run('security', ['set-key-partition-list', '-S', 'apple-tool:,apple:', '-s', '-k', password, keychain]);
  await rm(certificate);
  run('xcrun', ['notarytool', 'store-credentials', profile, '--keychain', keychain, '--apple-id', APPLE_ID,
    '--password', APPLE_PASSWORD, '--team-id', APPLE_TEAM_ID]);
  const env = { ...process.env, NOOSPHERE_MAC_SIGN_IDENTITY: APPLE_SIGNING_IDENTITY,
    NOOSPHERE_MAC_NOTARY_PROFILE: profile, NOOSPHERE_MAC_KEYCHAIN: keychain };
  for (const key of ['APPLE_CERTIFICATE', 'APPLE_CERTIFICATE_PASSWORD', 'APPLE_SIGNING_IDENTITY',
    'APPLE_ID', 'APPLE_PASSWORD', 'APPLE_TEAM_ID']) delete env[key];
  run(node, [join(root, 'scripts/desktop.mjs'), 'package', target, ...options, '--signed'], { env });
} finally {
  try {
    if (created) run('security', ['delete-keychain', keychain]);
  } finally {
    await rm(privateDir, { recursive: true, force: true });
  }
}
