// Real packaged WebView2 / Rust / Go / CodeLens test. The model endpoint is a fixture.
const assert = require('node:assert/strict');
const { spawn, execFileSync } = require('node:child_process');
const { mkdtempSync, mkdirSync, writeFileSync, readFileSync, readdirSync, existsSync,
  openSync, writeSync, closeSync, ftruncateSync } = require('node:fs');
const { createServer } = require('node:http');
const { createRequire } = require('node:module');
const { tmpdir } = require('node:os');
const path = require('node:path');
const { chromium } = createRequire(path.resolve(__dirname, '../frontend/package.json'))('playwright');

const name = process.argv[3] || 'Noosphere.exe';
const localRelease = path.resolve(__dirname, '../release');
const localApp = path.join(localRelease, 'app');
const exe = path.resolve(process.argv[2] || (existsSync(path.join(localApp, name)) ? localApp : localRelease), name);
const root = mkdtempSync(path.join(tmpdir(), 'noosphere delivery '));
const data = path.join(root, '用户 数据');
const project = path.join(root, '测试 项目');
const evidence = process.env.NOOSPHERE_VERIFY_SCREENSHOTS && path.resolve(process.env.NOOSPHERE_VERIFY_SCREENSHOTS);
const listen = server => new Promise((resolve, reject) => {
  server.once('error', reject);
  server.listen(0, '127.0.0.1', () => resolve(server.address().port));
});
const pause = ms => new Promise(resolve => setTimeout(resolve, ms));
async function waitFor(check, label, timeout = 30000, checkProcess) {
  const deadline = Date.now() + timeout;
  let recent;
  while (Date.now() < deadline) {
    checkProcess?.();
    try { const value = await check(); if (value) return value; } catch (error) { recent = error; }
    await pause(200);
  }
  const reason = [recent?.message, recent?.cause?.code].filter(Boolean).join(' ');
  throw Error(`${label} timed out${reason ? ': ' + reason : ''}`);
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

function textPDF(value) {
  const stream = `BT /F1 11 Tf 40 750 Td (${value}) Tj ET`;
  const objects = ['<< /Type /Catalog /Pages 2 0 R >>', '<< /Type /Pages /Kids [3 0 R] /Count 1 >>',
    '<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 4 0 R >> >> /Contents 5 0 R >>',
    '<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>', `<< /Length ${stream.length} >>\nstream\n${stream}\nendstream`];
  let body = '%PDF-1.4\n';
  const offsets = objects.map((object, i) => {
    const offset = body.length;
    body += `${i + 1} 0 obj\n${object}\nendobj\n`;
    return offset;
  });
  const xref = body.length;
  body += `xref\n0 ${offsets.length + 1}\n0000000000 65535 f \n`;
  body += offsets.map(offset => `${String(offset).padStart(10, '0')} 00000 n \n`).join('');
  return Buffer.from(`${body}trailer\n<< /Size ${offsets.length + 1} /Root 1 0 R >>\nstartxref\n${xref}\n%%EOF\n`);
}

function writeLargeLog(file, size) {
  const fd = openSync(file, 'w');
  try {
    const block = Buffer.alloc(1 << 20, 'ordinary fixture line\n');
    for (let offset = 0; offset < size; offset += block.length) {
      const length = Math.min(block.length, size - offset);
      if (offset + length === size) {
        const tail = Buffer.from('\nfile_evidence satori-742\n');
        tail.copy(block, length - tail.length);
      }
      writeSync(fd, block, 0, length);
    }
  } finally {
    closeSync(fd);
  }
}

function writeLargePDF(file, size) {
  const small = textPDF('pdf_evidence paper-853');
  const index = small.indexOf('\nxref\n') + 1;
  const table = small.subarray(index, small.lastIndexOf('startxref\n')).toString('ascii');
  const offset = size - table.length - String(size).length - 'startxref\n\n%%EOF\n'.length;
  const trailer = Buffer.from(table + 'startxref\n' + offset + '\n%%EOF\n');
  assert.equal(offset + trailer.length, size);
  const fd = openSync(file, 'w');
  try {
    writeSync(fd, small.subarray(0, index));
    writeSync(fd, trailer, 0, trailer.length, offset);
  } finally {
    closeSync(fd);
  }
}

async function setDesktopFiles(page, files) {
  // The WebView is local even though Playwright connects over CDP. Have
  // WebView2 open the actual files without a copy across Playwright's channel.
  const session = await page.context().newCDPSession(page);
  try {
    const { root } = await session.send('DOM.getDocument');
    const { nodeId } = await session.send('DOM.querySelector', {
      nodeId: root.nodeId, selector: 'input[aria-label="对话附件"]',
    });
    assert.ok(nodeId, 'chat file input exists');
    await session.send('DOM.setFileInputFiles', { nodeId, files });
  } finally {
    await session.detach();
  }
}

const modelCalls = [];
const upstream = createServer(async (request, response) => {
  try {
    if (request.method === 'GET' && request.url === '/v1/models') {
      response.writeHead(200, { 'Content-Type': 'application/json' });
      response.end(JSON.stringify({ data: [{ id: 'fixture-model' }] }));
      return;
    }
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

async function streamChat(page, requestId, modelId, sessionId, prompt, attachmentTurn) {
  return page.evaluate(({requestId,modelId,sessionId,prompt,attachmentTurn}) => new Promise(async (resolve, reject) => {
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
          requestId, modelId, workspaceId: 'local', sessionId, attachmentTurn,
          messages: [{ role: 'user', content: prompt }], onEvent: '__CHANNEL__:' + id,
        });
      } catch (error) { clearTimeout(timeout); reject(Error(JSON.stringify(error))); }
    }), { requestId, modelId, sessionId, prompt, attachmentTurn });
}

async function verifyImportedWeight(ipc, page, file) {
  const change = change => ipc('modify_model_library', { change });
  const projector = process.env.NOOSPHERE_TEST_PROJECTOR || null;
  const before = performance.now();
  const catalog = await change({ action: 'import', paths: [file] });
  const afterImport = performance.now();
  const repeated = await change({ action: 'import', paths: [file] });
  assert.deepEqual(repeated.models.map(entry => entry.id), catalog.models.map(entry => entry.id));
  console.log('GGUF registration (ms): initial %d, repeat %d', Math.round(afterImport - before), Math.round(performance.now() - afterImport));
  const [model] = catalog.models;
  assert.equal(catalog.models.length, 1);
  assert.equal(model.status, 'runtime_missing');
  const installed = await change({ action: 'install_runtime' });
  const runtime = installed.runtimes.find(item => item.status === 'ready');
  assert.ok(runtime?.version, 'installed runtime reported its version');
  await change({ action: 'configure', id: model.id, name: model.name, runtime_id: runtime.id,
    projector, context: projector ? 2048 : 512, gpu_layers: 0, device: null });
  const engine = await ipc('control_local_engine', { engineId: 'custom_llama', action: 'start', model: model.id });
  assert.ok(engine.managed && engine.models.some(entry => entry.id === model.id && entry.loaded));
  try {
    const config = { id: model.id, name: model.name, model_name: model.id, provider: 'openai_compatible',
      base_url: engine.base_url, enabled: true, max_tokens: 32, supports_temperature: false, protocol_options: { tools: false, vision: Boolean(projector) } };
    await ipc('add_model', { model: config });
    const reply = await ipc('gateway_chat', { modelId: model.id, messages: [{ role: 'user', content: 'Answer in one word. What color is grass?' }] });
    assert.ok(reply.content?.trim());
    assert.equal(reply.model, model.id);
    const events = await streamChat(page, 'real-cpu-stream', model.id, 'real-gguf-session', 'Name one color.');
    assert.ok(events.some(ev => ev.token) && events.at(-1).status === 'completed' && events.at(-1).content?.trim());

    if (projector) {
      assert.equal(engine.models.find(entry => entry.id === model.id).vision, true);
      const imageId = await page.evaluate(async projectPath => {
        const canvas = document.createElement('canvas');
        canvas.width = canvas.height = 128;
        const draw = canvas.getContext('2d');
        draw.fillStyle = 'red';
        draw.fillRect(0, 0, 128, 128);
        const blob = await new Promise(resolve => canvas.toBlob(resolve, 'image/png'));
        const bytes = await blob.arrayBuffer();
        const id = crypto.randomUUID();
        const metadata = { workspace_id: 'local', session_id: 'real-vision-session', project_path: projectPath,
          id, name: 'sample.png', offset: 0, size: bytes.byteLength };
        const reply = await window.__TAURI_INTERNALS__.invoke('stage_chat_attachment', bytes,
          { headers: { 'x-noosphere-attachment': encodeURIComponent(JSON.stringify(metadata)) } });
        if (reply.attachment?.id !== id) throw Error('Image upload was not acknowledged');
        return id;
      }, (await ipc('list_workspaces')).workspaces.find(ws => ws.id === 'local').project_path || '');
      const prompt = 'What color fills this image? Answer in one word.';
      const result = await streamChat(page, 'real-vision-stream', model.id, 'real-vision-session', prompt,
        { message_id: 'vision-user', ids: [imageId], question: prompt });
      assert.equal(result.at(-1)?.status, 'completed', JSON.stringify(result));
      assert.ok(result.at(-1).content?.trim());
      console.log('PASS image streamed through selected projector:', result.at(-1).content.slice(0, 120));
    }
    console.log('PASS real CPU inference through native import, pinned install, owned listener and Go gateway:', runtime.version, reply.content.slice(0, 80));
  } finally {
    await ipc('control_local_engine', { engineId: 'custom_llama', action: 'stop' });
  }
  await ipc('delete_model', { id: model.id });
  assert.equal((await change({ action: 'forget', id: model.id })).models.length, 0);
  assert.ok(existsSync(file));
}

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
  const occupied = [createServer(), createServer()];
  const [serverPort, workerPort] = await Promise.all(occupied.map(listen));
  writeFileSync(path.join(data, 'config.yaml'), JSON.stringify({ server: { port: serverPort },
    modules: { nebula: { embedding: { provider: 'mock' } }, codelens: { port: workerPort } },
  }));
  writeFileSync(path.join(data, 'data-version.json'), JSON.stringify({ schema_version: 1, app_version: '0.6.0' }));
  const configuredPort = process.env.NOOSPHERE_TEST_WEBVIEW_PORT;
  const cdpPort = Number(configuredPort || 0);
  if (configuredPort) assert.ok(Number.isInteger(cdpPort) && cdpPort >= 1024 && cdpPort <= 65535);
  const profile = path.join(root, 'webview');
  const portFile = path.join(profile, 'EBWebView', 'DevToolsActivePort');
  const launchedAt = Date.now();
  const child = spawn(exe, [], {
    cwd: path.dirname(exe), windowsHide: true, stdio: ['ignore', 'ignore', 'inherit'],
    env: { ...process.env,
      PATH: ['System32', '.', 'System32/Wbem'].map(directory => path.resolve(process.env.SystemRoot, directory)).join(';'),
      NOOSPHERE_DATA_DIR: data, NOOSPHERE_RUNTIME_DIAGNOSTICS: '1',
      WEBVIEW2_USER_DATA_FOLDER: profile,
      WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS: `--remote-debugging-port=${cdpPort} --remote-debugging-address=127.0.0.1`,
    },
  });
  let launchError;
  child.on('error', error => { launchError = error; });
  let browser;
  try {
    // On a cold runner the desktop can take over 45 s just to initialize its WebView.
    // Chromium publishes both the allocated port and browser identity in this fresh profile.
    const socket = await waitFor(async () => {
      let port = cdpPort;
      let browserPath;
      if (!configuredPort) {
        const [rawPort, target] = readFileSync(portFile, 'utf8').trim().split(/\r?\n/);
        port = Number(rawPort);
        if (!Number.isInteger(port) || port < 1 || port > 65535 || !/^\/devtools\/browser\/[\w-]+$/.test(target)) return;
        browserPath = target;
      }
      const origin = '127.0.0.1:' + port;
      const response = await fetch('http://' + origin + '/json/version', { signal: AbortSignal.timeout(2000) });
      if (!response.ok) return;
      const info = await response.json();
      const url = new URL(info.webSocketDebuggerUrl);
      if (url.protocol === 'ws:' && url.host === origin && (!browserPath || url.pathname === browserPath)) return url.href;
    }, 'WebView2 CDP', 120000, () => {
      if (launchError || child.exitCode !== null || child.signalCode) throw launchError || Error('Desktop exited: ' + (child.exitCode ?? child.signalCode));
    });
    console.log('WebView2 CDP ready after %d ms', Date.now() - launchedAt);
    browser = await chromium.connectOverCDP(socket);
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
    const diagnostic = await ipc('probe_model', { model: { id: 'missing-check', name: 'Missing fixture', provider: 'openai_compatible', model_name: 'missing', base_url: `http://127.0.0.1:${modelPort}/v1`, enabled: true }, infer: false });
    assert.equal(diagnostic.service, 'online');
    assert.equal(diagnostic.model_discovery, 'missing');
    assert.equal(diagnostic.stage, 'enumeration');
    assert.ok(diagnostic.error && diagnostic.endpoint && diagnostic.code);
    console.log('PASS structured model failure survives native IPC');
    if (evidence) {
      mkdirSync(evidence, { recursive: true });
      await page.setViewportSize({ width: 1585, height: 992 });
      await page.getByRole('heading', { name: '开始对话', exact: true }).waitFor();
      await page.getByText('后端在线', { exact: true }).waitFor({ state: 'attached' });
      await page.screenshot({ path: path.join(evidence, '10-final-windows-workbench.png') });
      const operations = page.getByRole('link', { name: '系统', exact: true });
      await operations.hover();
      await page.getByRole('tooltip', { name: '系统', exact: true }).waitFor();
      await page.screenshot({ path: path.join(evidence, '11-final-windows-icon-hover.png') });
      await page.setViewportSize({ width: 1280, height: 800 });
    }
    const initialRuns = await ipc('list_workspace_runs', { workspaceId: 'local' });
    await page.getByRole('link', { name: '测评', exact: true }).click();
    await page.getByRole('heading', { name: '模型试聊', exact: true }).waitFor();
    await page.getByRole('textbox', { name: '输入试聊问题', exact: true }).fill('Synthetic isolated preview');
    await page.getByRole('button', { name: '发送试聊', exact: true }).click();
    await page.getByLabel('试聊记录', { exact: true }).getByText(/delivery fixture/).waitFor();
    assert.equal((await ipc('list_workspace_runs', { workspaceId: 'local' })).runs.length, initialRuns.runs.length);
    if (evidence) await page.screenshot({ path: path.join(evidence, '14-final-windows-model-check.png') });
    await page.getByRole('link', { name: '对话', exact: true }).click();
    await page.getByRole('textbox', { name: '输入问题', exact: true }).waitFor();
    assert.equal(await page.getByText('Synthetic isolated preview', { exact: true }).count(), 0);
    console.log('PASS isolated native model preview and unchanged main conversation');
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

    const chunks = await streamChat(page, 'delivery-stream', 'fixture', 'delivery-native', 'Synthetic delivery chat');
    const terminal = chunks.at(-1);
    assert.equal(terminal.status, 'completed', JSON.stringify(chunks));
    assert.equal(terminal.content, 'delivery fixture');
    const agent = await run('agent', { goal: 'Propose the specified synthetic change to the fixture file.',
      display_goal: 'Review the fixture file',
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

    await page.reload();
    await page.getByRole('button', { name: 'Delivery fixture', exact: true }).click();
    await page.getByRole('link', { name: '打开任务 Delivery agent', exact: true }).click();
    await page.getByRole('heading', { name: '任务目标', exact: true }).waitFor();
    await page.getByText('Review the fixture file', { exact: true }).waitFor();
    await page.getByRole('heading', { name: '模型结论', exact: true }).waitFor();
    await page.getByText('delivery fixture', { exact: true }).waitFor();
    const approvals = page.getByRole('region', { name: 'Agent 操作审批' });
    await approvals.getByText(/文件修改.*已执行/).waitFor();
    if (evidence) await page.screenshot({ path: path.join(evidence, '12-final-windows-task-record.png') });
    await page.getByRole('button', { name: '继续对话', exact: true }).click();
    await page.getByRole('textbox', { name: '输入问题', exact: true }).waitFor();
    await page.getByRole('link', { name: '新建任务', exact: true }).click();
    await page.getByRole('heading', { name: '新建任务', exact: true }).waitFor();
    await page.setViewportSize({ width: 900, height: 600 });
    const openSidebar = page.getByRole('button', { name: '打开项目侧栏', exact: true });
    await openSidebar.focus();
    await page.keyboard.press('Enter');
    const sidebar = page.getByRole('dialog', { name: '项目与会话' });
    await sidebar.getByRole('link', { name: '新建任务', exact: true }).waitFor();
    if (evidence) await page.screenshot({ path: path.join(evidence, '13-final-windows-narrow-sidebar.png') });
    await page.keyboard.press('Escape');
    await sidebar.waitFor({ state: 'hidden' });
    await page.setViewportSize({ width: 1280, height: 800 });
    await page.getByRole('button', { name: '本地工作区', exact: true }).click();
    await page.getByText('任务 · 只读', { exact: true }).waitFor({ state: 'hidden' });
    console.log('PASS sidebar task history, conversation continuation, and keyboard navigation at 900×600');

    await page.evaluate(() => { window.location.hash = '/chat'; });
    const input = page.getByLabel('对话附件', { exact: true });
    const picker = page.getByRole('button', { name: '选择附件', exact: true });
    await picker.waitFor();
    await waitFor(() => picker.isEnabled(), 'chat composer ready');
    const draft = page.getByTestId('chat-attachments');
    await input.setInputFiles({ name: '图片.png', mimeType: 'image/png', buffer: Buffer.from([137, 80, 78, 71]) });
    await draft.getByText(/无法读取图片/).waitFor();
    await draft.getByRole('button', { name: '移除附件 图片.png' }).click();
    const maxAttachmentBytes = 200 * 1024 * 1024;
    const overflowFile = path.join(root, '超限.txt');
    const overflow = openSync(overflowFile, 'w');
    try { ftruncateSync(overflow, maxAttachmentBytes + 1); } finally { closeSync(overflow); }
    await setDesktopFiles(page, [overflowFile]);
    await draft.getByText(/超过每份 200 MB/).waitFor();
    await draft.getByRole('button', { name: '移除附件 超限.txt' }).click();
    const logPath = path.join(root, '文本证据.txt');
    const pdfPath = path.join(root, '页码证据.pdf');
    writeLargeLog(logPath, maxAttachmentBytes);
    writeLargePDF(pdfPath, maxAttachmentBytes);
    await setDesktopFiles(page, [logPath, pdfPath]);
    await waitFor(async () => {
      if (await draft.getByText('可以发送').count() === 2) return true;
      throw Error(await draft.innerText());
    }, '200 MiB text and PDF staged through native IPC', 120000);
    const question = page.getByRole('textbox', { name: '输入问题' });
    await question.evaluate((target, bytes) => {
      const transfer = new DataTransfer();
      transfer.items.add(new File([new Uint8Array(bytes)], '拖拽证据.pdf', { type: 'application/pdf' }));
      target.dispatchEvent(new DragEvent('drop', { bubbles: true, cancelable: true, dataTransfer: transfer }));
    }, [...textPDF('drop evidence')]);
    await waitFor(async () => await draft.getByText('可以发送').count() === 3, 'PDF drag and drop staged through native IPC');
    await question.fill('Give me the exact file_evidence and pdf_evidence values.');
    await page.getByRole('button', { name: '发送', exact: true }).click();
    await page.getByRole('list', { name: '消息附件' }).getByText('文本证据.txt').waitFor();
    await waitFor(() => modelCalls.find(call => call.stream && call.messages.at(-1)?.content.includes('pdf_evidence paper-853')), 'PDF excerpt reached text-only model');
    const attachedPrompt = modelCalls.find(call => call.stream && call.messages.at(-1)?.content.includes('pdf_evidence paper-853')).messages.at(-1).content;
    assert.match(attachedPrompt, /file_evidence satori-742/);
    assert.match(attachedPrompt, /页码证据\.pdf.*page 1/);
    assert.ok(attachedPrompt.length < 3000, 'entire original file leaked into model call');
    await page.getByRole('button', { name: '重新生成', exact: true }).waitFor();
    await page.getByText(/本次对话的依据/).click();
    await page.getByText(/页码证据\.pdf.*第 1 页/).waitFor();
    const attachmentRun = (await ipc('list_workspace_runs', { workspaceId: 'local' })).runs.find(item => item.kind === 'chat' && item.session_id !== 'delivery-native');
    assert.ok(attachmentRun);
    const attachmentEvent = (await ipc('get_run_events', { id: attachmentRun.id, after: 0 })).events.find(event => event.phase === 'attachments');
    assert.ok(attachmentEvent?.data?.citations?.some(citation => citation.page === 1));
    const turn = { message_id: 'delivery-proof', question: 'file_evidence', ids: [...new Set(attachmentEvent.data.citations.map(citation => citation.attachment_id))] };
    await assert.rejects(() => ipc('prepare_chat_attachments', { workspaceId: other.id, sessionId: attachmentRun.session_id, turn }), /找不到此附件/);
    await assert.rejects(() => ipc('prepare_chat_attachments', { workspaceId: 'local', sessionId: 'foreign-session', turn }), /找不到此附件/);
    console.log('PASS 200 MiB native text/PDF attachments, oversized rejection, drag and drop, page/line provenance, bounded model context and workspace/session isolation');

    if (process.env.NOOSPHERE_TEST_GGUF) await verifyImportedWeight(ipc, page, path.resolve(process.env.NOOSPHERE_TEST_GGUF));

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
    const retained = await ipc('prepare_chat_attachments', { workspaceId: 'local', sessionId: attachmentRun.session_id, turn });
    assert.ok(retained.citations.some(citation => citation.excerpt.includes('satori-742')));
    const newGo = await waitFor(backend, 'new backend child');
    const newPython = await waitFor(() => children(newGo).find(process => /python/i.test(process.Name))?.ProcessId, 'new worker child');
    try { await ipc('plugin:window|close', { label: 'main' }); } catch (error) {
      if (!page.isClosed() && child.exitCode === null) throw error;
    }
    await waitFor(() => child.exitCode !== null && !running(newGo) && !running(newPython), 'normal exit and subprocess cleanup');
    console.log('PASS crash recovery, retained task, graceful desktop close and no owned child left');
    assert.ok(existsSync(path.join(data, 'logs')));
  } catch (error) {
    console.error('Packaged WebView2 launch', { executable: exe, cdpPort, activePortFile: existsSync(portFile), elapsedMs: Date.now() - launchedAt, pid: child.pid, exitCode: child.exitCode });
    try {
      if (child.pid && child.exitCode === null) console.error('Desktop child processes', children(child.pid));
      const services = path.join(data, 'desktop-services.log');
      if (existsSync(services)) console.error('Packaged runtime log\n' + readFileSync(services, 'utf8').slice(-16000));
    } catch (diagnosticError) {
      console.error('Could not collect desktop diagnostics', diagnosticError.message);
    }
    throw error;
  } finally {
    await browser?.close().catch(() => {});
    if (child.exitCode === null && child.pid) child.kill();
    for (const server of occupied) server.close();
    console.log(`Desktop smoke fixture and logs: ${root}`);
  }
}

main().catch(error => { console.error(error); process.exitCode = 1; }).finally(() => upstream.close());
