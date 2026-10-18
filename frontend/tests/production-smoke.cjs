// Serve built assets with the native CSP (including Tauri's inline-script hashes).
const { chromium } = require(process.env.PLAYWRIGHT_MODULE || 'playwright');
const fs = require('node:fs'); const path = require('node:path'); const http = require('node:http');
const crypto = require('node:crypto'); const assert = require('node:assert/strict');
const root = path.resolve(__dirname, '../dist');
const csp = JSON.parse(fs.readFileSync(path.resolve(__dirname, '../../desktop/src-tauri/tauri.conf.json'))).app.security.csp;
const html = fs.readFileSync(path.join(root, 'index.html'), 'utf8');
for (const match of html.matchAll(/<script(?![^>]*\bsrc=)[^>]*>([\s\S]*?)<\/script>/g)) csp['script-src'] += ` 'sha256-${crypto.createHash('sha256').update(match[1]).digest('base64')}'`;
const policy = Object.entries(csp).map(([key, value]) => `${key} ${value}`).join('; ');
const mime = { '.html': 'text/html', '.js': 'text/javascript', '.css': 'text/css', '.woff2': 'font/woff2', '.png': 'image/png', '.glb': 'model/gltf-binary' };
const server = http.createServer((request, response) => {
  const pathname = decodeURIComponent(new URL(request.url, 'http://localhost').pathname);
  const file = path.resolve(root, '.' + (pathname === '/' ? '/index.html' : pathname));
  if (!file.startsWith(root + path.sep)) { response.writeHead(403).end(); return; }
  fs.readFile(file, (error, data) => { if (error) { response.writeHead(404).end(); return; } response.writeHead(200, { 'content-type': mime[path.extname(file)] || 'application/octet-stream', 'content-security-policy': policy }); response.end(data); });
});
(async () => {
  await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
  const browser = await chromium.launch({ channel: process.env.QA_BROWSER || 'msedge', headless: true });
  try {
    const page = await browser.newPage({ viewport: { width: 1280, height: 800 } }); const errors = [], requests = [];
    page.on('pageerror', error => errors.push(error.message)); page.on('console', message => { if (message.type() === 'error') errors.push(message.text()); }); page.on('request', request => requests.push(request.url()));
    const url = `http://127.0.0.1:${server.address().port}/`;
    await page.goto(url + '#/chat'); await page.getByRole('textbox', { name: '输入问题' }).waitFor();
    assert.ok(!requests.some(url => /ArchiveCanvas|archive-cassette/.test(url)), 'chat must not load the scene');
    await page.getByRole('link', { name: 'Noosphere 工作台', exact: true }).click();
    await page.locator('.ns-full-scene[data-loaded="true"]').waitFor();
    await page.getByRole('button', { name: '下一个档案' }).click(); await page.getByRole('link', { name: /进入CodeLens/ }).click();
    await page.waitForURL('**/#/codelens'); await page.getByRole('heading', { name: 'CodeLens', exact: true }).waitFor(); assert.equal(await page.locator('canvas').count(), 0); assert.deepEqual(errors, []);
    console.log('PASS production assets, native CSP resource fetch, archive navigation, and chat scene isolation');
  } finally { await browser.close(); server.close(); }
})().catch(error => { console.error(error); server.close(); process.exitCode = 1; });
