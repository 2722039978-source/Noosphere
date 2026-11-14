const { spawn } = require('node:child_process');
const { createRequire } = require('node:module');
const path = require('node:path');
const { pathToFileURL } = require('node:url');
const requireFrontend = createRequire(path.resolve(process.cwd(), 'package.json'));

async function main() {
  const viteEntry = requireFrontend.resolve('vite/package.json');
  const { createServer } = await import(pathToFileURL(path.join(path.dirname(viteEntry), 'dist/node/index.js')).href);
  const vite = await createServer({ server: { host: '127.0.0.1', port: 0, strictPort: true } });
  await vite.listen();
  try {
    const { port } = vite.httpServer.address();
    const code = await new Promise((resolve, reject) => {
      const child = spawn(process.execPath, ['tests/model-selection-ui.cjs'], {
        env: { ...process.env, QA_URL: `http://127.0.0.1:${port}/`, QA_BROWSER: process.env.QA_BROWSER || 'chromium' },
        stdio: 'inherit',
      });
      child.once('error', reject);
      child.once('exit', (status, signal) => signal ? reject(Error(signal)) : resolve(status));
    });
    if (code !== 0) throw Error(`Browser tests exited with ${code}`);
  } finally {
    await vite.close();
  }
}

main().catch(error => { console.error(error); process.exitCode = 1; });
