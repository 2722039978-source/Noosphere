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

const exe = path.resolve(process.argv[2] || 'release', process.argv[3] || 'Noosphere.exe');
const root = mkdtempSync(path.join(tmpdir(), 'noosphere delivery '));
const data = path.join(root, '用户 数据');
const project = path.join(root, '测试 项目');
const evidence = process.env.NOOSPHERE_VERIFY_SCREENSHOTS && path.resolve(process.env.NOOSPHERE_VERIFY_SCREENSHOTS);
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
  const configuredPort = process.env.NOOSPHERE_TEST_WEBVIEW_PORT;
  const probe = createServer();
  const cdpPort = configuredPort ? Number(configuredPort) : await listen(probe);
  if (configuredPort) assert.ok(Number.isInteger(cdpPort) && cdpPort >= 1024 && cdpPort <= 65535);
  else await new Promise(resolve => probe.close(resolve));
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
    await page.getByRole('heading', { name: '任务结果', exact: true }).waitFor();
    await page.getByRole('region', { name: 'Agent 操作审批' }).waitFor();
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
    await page.getByText(/本次提供给模型的依据/).click();
    await page.getByText(/页码证据\.pdf.*第 1 页/).waitFor();
    const attachmentRun = (await ipc('list_workspace_runs', { workspaceId: 'local' })).runs.find(item => item.kind === 'chat' && item.session_id !== 'delivery-native');
    assert.ok(attachmentRun);
    const attachmentEvent = (await ipc('get_run_events', { id: attachmentRun.id, after: 0 })).events.find(event => event.phase === 'attachments');
    assert.ok(attachmentEvent?.data?.citations?.some(citation => citation.page === 1));
    const turn = { message_id: 'delivery-proof', question: 'file_evidence', ids: [...new Set(attachmentEvent.data.citations.map(citation => citation.attachment_id))] };
    await assert.rejects(() => ipc('prepare_chat_attachments', { workspaceId: other.id, sessionId: attachmentRun.session_id, turn }), /找不到此附件/);
    await assert.rejects(() => ipc('prepare_chat_attachments', { workspaceId: 'local', sessionId: 'foreign-session', turn }), /找不到此附件/);
    console.log('PASS 200 MiB native text/PDF attachments, oversized rejection, drag and drop, page/line provenance, bounded model context and workspace/session isolation');

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
  } finally {
    await browser?.close().catch(() => {});
    if (child.exitCode === null && child.pid) child.kill();
    for (const server of occupied) server.close();
    console.log(`Desktop smoke fixture and logs: ${root}`);
  }
}

main().catch(error => { console.error(error); process.exitCode = 1; }).finally(() => upstream.close());
