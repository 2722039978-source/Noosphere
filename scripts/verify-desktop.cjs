// Real packaged WebView2 / Rust / Go / CodeLens test. The model endpoint is a fixture.
const assert = require('node:assert/strict');
const { spawn, execFileSync } = require('node:child_process');
const { mkdtempSync, mkdirSync, writeFileSync, readFileSync, readdirSync, existsSync } = require('node:fs');
const { createServer } = require('node:http');
const { createRequire } = require('node:module');
const { tmpdir } = require('node:os');
const path = require('node:path');
const { chromium } = createRequire(path.resolve(__dirname, '../frontend/package.json'))('playwright');

const exe = path.resolve(process.argv[2] || 'release', process.argv[3] || 'Noosphere.exe');
const root = mkdtempSync(path.join(tmpdir(), 'noosphere delivery '));
const data = path.join(root, '用户 数据');
const project = path.join(root, '测试 项目');
const listen = server => new Promise((resolve, reject) => {
  server.once('error', reject);
  server.listen(0, '127.0.0.1', () => resolve(server.address().port));
});
const pause = ms => new Promise(resolve => setTimeout(resolve, ms));
async function waitFor(check, label, timeout = 30000) {
  const deadline = Date.now() + timeout;
  let recent;
  while (Date.now() < deadline) {
    try { const value = await check(); if (value) return value; } catch (error) { recent = error; }
    await pause(200);
  }
  throw Error(`${label} timed out${recent ? ': ' + recent.message : ''}`);
}

function children(pid) {
  const raw = execFileSync(path.join(process.env.SystemRoot, 'System32/WindowsPowerShell/v1.0/powershell.exe'), [
    '-NoProfile', '-NonInteractive', '-Command',
    `[Console]::OutputEncoding = [Text.Encoding]::UTF8; ConvertTo-Json -Compress -InputObject @(Get-CimInstance Win32_Process -Filter 'ParentProcessId = ${pid}' | Select-Object Name,ProcessId)`,
  ], { encoding: 'utf8', windowsHide: true }).trim();
  return raw ? JSON.parse(raw) : [];
}

function running(pid) {
  try { process.kill(pid, 0); return true; } catch (error) {
    if (error.code === 'ESRCH') return false;
    throw error;
  }
}

const modelCalls = [];
const upstream = createServer(async (request, response) => {
  try {
    assert.equal(request.url, '/v1/chat/completions');
    const data = [];
    for await (const chunk of request) data.push(chunk);
    const body = JSON.parse(Buffer.concat(data));
    assert.equal(body.model, 'fixture-model');
    modelCalls.push(body);
    if (body.stream) {
      response.writeHead(200, { 'Content-Type': 'text/event-stream' });
      response.write('data: {"choices":[{"delta":{"content":"delivery "}}]}\n\n');
      response.end('data: {"choices":[{"delta":{"content":"fixture"}}]}\n\ndata: [DONE]\n\n');
    } else {
      const tool = body.tools?.length && body.messages.at(-1).role !== 'tool';
      const message = tool ? { tool_calls: [{ id: 'fixture-change', type: 'function',
        function: { name: 'propose_file_change', arguments: JSON.stringify({
          path: 'example.py', old_text: 'return "before"', new_text: 'return "after"', reason: 'Test explicit approval',
        }) } }] } : { content: 'delivery fixture' };
      response.writeHead(200, { 'Content-Type': 'application/json' });
      response.end(JSON.stringify({ choices: [{ message, finish_reason: tool ? 'tool_calls' : 'stop' }] }));
    }
  } catch (error) {
    console.error(error);
    response.writeHead(500).end();
  }
});

async function main() {
  assert.ok(existsSync(exe), `Missing packaged desktop: ${exe}`);
  mkdirSync(data); mkdirSync(project);
  const source = path.join(project, 'example.py');
  writeFileSync(source, 'def fixture_greet():\n    return "before"\n');
  const modelPort = await listen(upstream);
  writeFileSync(path.join(data, 'models.yaml'), JSON.stringify({ default_model: 'fixture', providers: [{
    id: 'fixture', name: 'Delivery fixture', type: 'deepseek', model: 'fixture-model',
    base_url: `http://127.0.0.1:${modelPort}/v1`, enabled: true,
  }] }));
  writeFileSync(path.join(data, 'config.yaml'), JSON.stringify({ server: { port: 8080 },
    modules: { nebula: { embedding: { provider: 'mock' } }, codelens: { port: 18765 } },
  }));
  writeFileSync(path.join(data, 'data-version.json'), JSON.stringify({ schema_version: 1, app_version: '0.6.0' }));
  const occupied = [createServer(), createServer()];
  await Promise.all(occupied.map((server, i) => new Promise((resolve, reject) => {
    server.once('error', reject);
    server.listen([8080, 18765][i], '127.0.0.1', resolve);
  })));
  const probe = createServer();
  const cdpPort = await listen(probe);
  await new Promise(resolve => probe.close(resolve));
  const child = spawn(exe, [], {
    cwd: path.dirname(exe), windowsHide: true, stdio: 'ignore',
    env: { ...process.env,
      PATH: ['System32', '.', 'System32/Wbem'].map(directory => path.resolve(process.env.SystemRoot, directory)).join(';'),
      NOOSPHERE_DATA_DIR: data, WEBVIEW2_USER_DATA_FOLDER: path.join(root, 'webview'),
      WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS: `--remote-debugging-port=${cdpPort} --remote-debugging-address=127.0.0.1`,
    },
  });
  let launchError;
  child.on('error', error => { launchError = error; });
  let browser;
  try {
    const endpoint = `http://127.0.0.1:${cdpPort}`;
    await waitFor(async () => {
      if (launchError || child.exitCode !== null) throw launchError || Error(`Desktop exited with ${child.exitCode}`);
      return (await fetch(endpoint + '/json/version')).ok;
    }, 'WebView2 CDP', 45000);
    browser = await chromium.connectOverCDP(endpoint);
    const page = await waitFor(() => browser.contexts().flatMap(context => context.pages())
      .find(page => /^(?:https?:\/\/tauri\.localhost|tauri:\/\/localhost)\//.test(page.url())), 'packaged WebView');
    const ipc = (command, args = {}) => page.evaluate(async ({ command, args }) => {
      try { return await window.__TAURI_INTERNALS__.invoke(command, args); }
      catch (error) { throw Error(JSON.stringify(error)); }
    }, { command, args });
    const healthy = async () => {
      const status = await ipc('get_backend_status');
      if (!status.running) return false;
      assert.equal(path.resolve(status.data_dir).toLowerCase(), data.toLowerCase());
      const health = await ipc('check_health');
      return health.modules.codelens?.state === 'running' && health.modules.workspaces?.state === 'running' && health;
    };
    const first = await waitFor(healthy, 'owned backend and source worker readiness', 60000);
    assert.ok(first.instance_id);
    const state = JSON.parse(readFileSync(path.join(data, 'data-version.json'), 'utf8'));
    assert.equal(state.app_version, first.version);
    const backupRoot = path.join(data, 'upgrade-backups');
    const [backup] = readdirSync(backupRoot).filter(name => !name.startsWith('.'));
    const migration = JSON.parse(readFileSync(path.join(backupRoot, backup, 'manifest.json'), 'utf8'));
    assert.equal(migration.from_version, '0.6.0');
    assert.equal(migration.to_version, first.version);
    assert.equal(readFileSync(path.join(backupRoot, backup, 'models.yaml'), 'utf8'), readFileSync(path.join(data, 'models.yaml'), 'utf8'));
    assert.equal((await ipc('codelens_health')).status, 'ok');
    const ws = await ipc('save_workspace', { workspace: { name: 'Delivery fixture', project_path: project } });
    const other = await ipc('save_workspace', { workspace: { name: 'Isolated delivery fixture' } });
    const run = async (kind, input) => {
      const run = await ipc('submit_workspace_run', { workspaceId: ws.id, kind, title: `Delivery ${kind}`, input });
      return waitFor(async () => {
        const state = await ipc('get_workspace_run', { id: run.id });
        return !['queued', 'running'].includes(state.status) && state;
      }, kind + ' task');
    };
    const indexed = await run('index', {});
    assert.equal(indexed.status, 'completed', JSON.stringify(indexed));
    assert.equal(indexed.result.total_files, 1);
    const memory = await ipc('save_knowledge', { workspaceId: ws.id, memory: {
      content: 'fixture_greet confirms the delivery proof', category: 'decision', source: { kind: 'note' },
    } });
    assert.ok(memory.id);
    await ipc('save_knowledge', { workspaceId: ws.id, memory: { ...memory, status: 'confirmed' } });
    const lookup = await ipc('prepare_context', { workspaceId: ws.id, query: 'fixture_greet delivery proof' });
    assert.ok(lookup.citations.some(citation => citation.memory_id === memory.id));
    const isolated = await ipc('prepare_context', { workspaceId: other.id, query: 'fixture_greet delivery proof' });
    assert.ok(!isolated.citations.some(citation => citation.memory_id === memory.id));
    console.log('PASS native Tauri IPC, dynamic ports, real Tree-sitter index and workspace-separated project memory');

    const chunks = await page.evaluate(() => new Promise(async (resolve, reject) => {
      const callback = window.__TAURI_INTERNALS__;
      const events = [];
      const timeout = setTimeout(() => reject(Error('No SSE terminal event')), 20000);
      const id = callback.transformCallback(data => {
        if (data.message) events.push(data.message);
        if (data.message?.done) {
          clearTimeout(timeout);
          resolve(events);
        }
      });
      try {
        await callback.invoke('gateway_chat_stream', {
          requestId: 'delivery-stream', modelId: 'fixture', workspaceId: 'local', sessionId: 'delivery-native',
          messages: [{ role: 'user', content: 'Synthetic delivery chat' }], onEvent: '__CHANNEL__:' + id,
        });
      } catch (error) { clearTimeout(timeout); reject(Error(JSON.stringify(error))); }
    }));
    const terminal = chunks.at(-1);
    assert.equal(terminal.status, 'completed', JSON.stringify(chunks));
    assert.equal(terminal.content, 'delivery fixture');
    const agent = await run('agent', { goal: 'Propose the specified synthetic change to the fixture file.',
      session_id: 'delivery-agent', reviewed_actions: true, model_id: 'fixture' });
    assert.equal(agent.status, 'completed', JSON.stringify(agent));
    const proposals = await ipc('list_agent_actions', { workspaceId: ws.id, runId: agent.id });
    const [proposal] = proposals.actions;
    assert.ok(proposal);
    assert.match(readFileSync(source, 'utf8'), /"before"/);
    await ipc('decide_agent_action', { workspaceId: ws.id, id: proposal.id, decision: 'approve' });
    await waitFor(() => readFileSync(source, 'utf8').includes('"after"'), 'approved file change');
    assert.ok(modelCalls.some(call => call.stream) && modelCalls.some(call => call.tools?.length));
    console.log('PASS real streaming and Agent proposal/approval through a synthetic loopback model');

    const backend = () => children(child.pid).find(process => process.Name === 'noosphere-backend.exe')?.ProcessId;
    const oldGo = await waitFor(backend, 'desktop child');
    const oldPython = await waitFor(() => children(oldGo).find(process => /python/i.test(process.Name))?.ProcessId, 'packaged Python child');
    const installedPython = execFileSync(path.join(process.env.SystemRoot, 'System32/WindowsPowerShell/v1.0/powershell.exe'), [
      '-NoProfile', '-NonInteractive', '-Command', `[Console]::OutputEncoding = [Text.Encoding]::UTF8; (Get-CimInstance Win32_Process -Filter 'ProcessId = ${oldPython}').ExecutablePath`,
    ], { encoding: 'utf8', windowsHide: true }).trim();
    assert.equal(path.resolve(installedPython).toLowerCase(), path.resolve(path.dirname(exe), 'codelens-runtime/python.exe').toLowerCase());
    process.kill(oldGo);
    await waitFor(() => !running(oldPython), 'crash cleanup of packaged Python');
    await waitFor(async () => {
      const current = await healthy();
      return current && current.instance_id !== first.instance_id;
    }, 'backend restart and worker recovery', 60000);
    assert.equal((await ipc('get_workspace_run', { id: indexed.id })).status, 'completed');
    const newGo = await waitFor(backend, 'new backend child');
    const newPython = await waitFor(() => children(newGo).find(process => /python/i.test(process.Name))?.ProcessId, 'new worker child');
    try { await ipc('plugin:window|close', { label: 'main' }); } catch (error) {
      if (!page.isClosed() && child.exitCode === null) throw error;
    }
    await waitFor(() => child.exitCode !== null && !running(newGo) && !running(newPython), 'normal exit and subprocess cleanup');
    console.log('PASS crash recovery, retained task, graceful desktop close and no owned child left');
    assert.ok(existsSync(path.join(data, 'logs')));
  } finally {
    await browser?.close().catch(() => {});
    if (child.exitCode === null && child.pid) child.kill();
    for (const server of occupied) server.close();
    console.log(`Desktop smoke fixture and logs: ${root}`);
  }
}

main().catch(error => { console.error(error); process.exitCode = 1; }).finally(() => upstream.close());
