/**
 * DEVOPS AGENT — 运维智能体
 * Apple Design · 环形指标 · 元素联动 · 弹性动画
 * 让每一次故障，都成为经验。
 */

const API_BASE = '/api/v1/devops';
const $ = (sel) => document.querySelector(sel);
const $$ = (sel) => document.querySelectorAll(sel);

// ═══════════════════════════════════════════
// 0. 全局状态
// ═══════════════════════════════════════════
const state = {
  connected: false,
  logs: [],
  currentView: 0,
  viewTimer: null,
  startTime: Date.now(),
  cpuCores: 1,                    // CPU 核心数（从 API 获取）
  prevProcesses: {},              // 上一轮进程 CPU 快照 { pid: { cpuSec, time } }
  lastMetricsTime: 0,             // 上次指标采集时间戳
  firstMetricsLoad: true,          // 是否首次加载（首次不做增量计算）
};

// ═══════════════════════════════════════════
// 1. 联动事件总线
// ═══════════════════════════════════════════
const linkageBus = {
  _events: {},
  on(event, fn) {
    (this._events[event] = this._events[event] || []).push(fn);
    return () => this.off(event, fn);
  },
  off(event, fn) {
    const list = this._events[event];
    if (list) this._events[event] = list.filter(cb => cb !== fn);
  },
  emit(event, data) {
    (this._events[event] || []).forEach(cb => cb(data));
  },
};

// ═══════════════════════════════════════════
// 2. 滚动进度条 + 视口揭示
// ═══════════════════════════════════════════
function initScrollEffects() {
  window.addEventListener('scroll', () => {
    const scrollTop = window.scrollY;
    const docHeight = document.documentElement.scrollHeight - window.innerHeight;
    const progress = Math.min(scrollTop / docHeight, 1);
    const bar = $('#scroll-progress');
    if (bar) bar.style.transform = `scaleX(${progress})`;
  });

  const observer = new IntersectionObserver((entries) => {
    entries.forEach(entry => {
      if (entry.isIntersecting) {
        entry.target.classList.add('revealed');
        observer.unobserve(entry.target);
      }
    });
  }, { threshold: 0.12, rootMargin: '0px 0px -30px 0px' });

  $$('.reveal').forEach(el => observer.observe(el));
}

// ═══════════════════════════════════════════
// 3. 架构视图切换
// ═══════════════════════════════════════════
function initArchViews() {
  const dots = $$('.arch-dot');
  const views = $$('.arch-view');
  const viewport = $('#arch-viewport');
  if (!dots.length) return;

  function switchToView(viewId, idx) {
    views.forEach(v => v.classList.remove('view-active'));
    dots.forEach(d => d.classList.remove('active'));
    const target = document.getElementById(viewId);
    if (target) target.classList.add('view-active');
    dots[idx].classList.add('active');
    state.currentView = idx;
    linkageBus.emit('view:switched', { viewId, index: idx });
  }

  dots.forEach((dot, i) => {
    dot.addEventListener('click', () => { switchToView(dot.dataset.view, i); });
  });

  if (viewport) {
    viewport.addEventListener('mouseenter', () => { if (state.viewTimer) clearInterval(state.viewTimer); });
    viewport.addEventListener('mouseleave', startAutoSwitch);
  }

  function startAutoSwitch() {
    state.viewTimer = setInterval(() => {
      const next = (state.currentView + 1) % dots.length;
      dots[next].click();
    }, 8000);
  }
  startAutoSwitch();
}

// ═══════════════════════════════════════════
// 4. 环形指标图动画
// ═══════════════════════════════════════════
const RING_CIRCUMFERENCE = 2 * Math.PI * 28;

function updateRing(id, percent) {
  const fill = $(`#vital-${id}-fill`);
  const val = $(`#vital-${id}-val`);
  if (!fill) return;

  const pct = Math.min(Math.max(percent, 0), 100);
  const offset = RING_CIRCUMFERENCE * (1 - pct / 100);
  fill.style.strokeDashoffset = offset;

  if (val) val.textContent = Math.round(pct) + '%';

  if (pct > 90) {
    fill.style.stroke = '#FF3B30';
    if (val) val.style.color = '#FF3B30';
  } else if (pct > 70) {
    fill.style.stroke = '#FF9500';
    if (val) val.style.color = '#FF9500';
  } else {
    fill.style.stroke = fill.dataset.color || '#34C759';
    if (val) val.style.color = '';
  }
}

function initRings() {
  const rings = {
    cpu: { color: '#007AFF', el: $('#vital-cpu-fill') },
    mem: { color: '#34C759', el: $('#vital-mem-fill') },
    disk: { color: '#FF9500', el: $('#vital-disk-fill') },
    health: { color: '#34C759', el: $('#vital-health-fill') },
  };
  Object.entries(rings).forEach(([key, { color, el }]) => {
    if (el) { el.dataset.color = color; el.style.stroke = color; }
  });
}

// ═══════════════════════════════════════════
// 5. 监控卡片 — 悬浮联动
// ═══════════════════════════════════════════
function initCardLinkage() {
  const cards = $$('.monitor-card');

  cards.forEach((card, idx) => {
    card.addEventListener('mouseenter', () => {
      cards.forEach((c, i) => {
        if (i !== idx) c.classList.add('is-dimmed');
        else c.classList.add('is-elevated');
      });
      const metric = card.dataset.metric;
      if (metric) {
        linkageBus.emit('card:hover', { metric, cardId: card.id });
        $$(`.arch-svg [data-metric="${metric}"]`).forEach(el => el.classList.add('highlight'));
      }
    });
    card.addEventListener('mouseleave', () => {
      cards.forEach(c => { c.classList.remove('is-dimmed', 'is-elevated'); });
      $$('.arch-svg [data-metric]').forEach(el => el.classList.remove('highlight'));
      linkageBus.emit('card:hover', null);
    });
    card.addEventListener('click', (e) => {
      const ripple = document.createElement('div');
      ripple.className = 'card-ripple';
      const rect = card.getBoundingClientRect();
      ripple.style.left = (e.clientX - rect.left) + 'px';
      ripple.style.top = (e.clientY - rect.top) + 'px';
      card.appendChild(ripple);
      setTimeout(() => ripple.remove(), 600);
      refreshMetrics(); refreshStatus();
      toast('数据已刷新', 'success');
    });
  });

  // Hero 环形图悬浮 → 高亮对应卡片
  $$('.vital-ring-wrap').forEach(wrap => {
    wrap.addEventListener('mouseenter', () => {
      const metric = wrap.dataset.metric;
      const card = document.querySelector(`.monitor-card[data-metric="${metric}"]`);
      if (card) card.classList.add('is-elevated');
    });
    wrap.addEventListener('mouseleave', () => {
      $$('.monitor-card').forEach(c => c.classList.remove('is-elevated'));
    });
  });
}

// ═══════════════════════════════════════════
// 6. 弹性数字动画 + 进度条
// ═══════════════════════════════════════════
function animateValue(id, target, duration = 600, decimals = 0) {
  const el = $('#' + id);
  if (!el) return;
  const current = parseFloat(el.textContent) || 0;
  if (Math.abs(current - target) < 0.1) return;

  const start = performance.now();
  const initial = current;

  function update(now) {
    const elapsed = now - start;
    const p = Math.min(elapsed / duration, 1);
    const eased = p < 1 ? 1 - Math.pow(1 - p, 3) : 1;
    const val = initial + (target - initial) * Math.min(eased, 1);
    el.textContent = decimals > 0 ? val.toFixed(decimals) : Math.round(val);
    if (p < 1) requestAnimationFrame(update);
  }
  requestAnimationFrame(update);
}

function animateBar(id, pct) {
  const bar = $('#' + id);
  if (!bar) return;
  bar.style.width = Math.min(Math.max(pct, 0), 100) + '%';
}

// ═══════════════════════════════════════════
// 7. API 客户端（带超时和重试）
// ═══════════════════════════════════════════
const api = {
  async get(path, timeout = 5000) {
    const ctrl = new AbortController();
    const timer = setTimeout(() => ctrl.abort(), timeout);
    try {
      const r = await fetch(API_BASE + path, { signal: ctrl.signal });
      clearTimeout(timer);
      if (!r.ok) throw new Error(`HTTP ${r.status}`);
      return r.json();
    } catch (e) {
      clearTimeout(timer);
      throw e;
    }
  },
  async post(path, body, timeout = 15000) {
    const ctrl = new AbortController();
    const timer = setTimeout(() => ctrl.abort(), timeout);
    try {
      const r = await fetch(API_BASE + path, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(body),
        signal: ctrl.signal,
      });
      clearTimeout(timer);
      if (!r.ok) throw new Error(`HTTP ${r.status}`);
      return r.json();
    } catch (e) {
      clearTimeout(timer);
      throw e;
    }
  },
};

// ═══════════════════════════════════════════
// 8. 事件日志
// ═══════════════════════════════════════════
function addLog(msg) {
  const time = new Date().toTimeString().slice(0, 8);
  state.logs.unshift({ time, msg });
  if (state.logs.length > 50) state.logs.length = 50;
  renderLogs();
}

function renderLogs() {
  const stream = $('#log-stream');
  if (!stream) return;
  stream.innerHTML = state.logs.map((l, i) =>
    `<div class="log-entry" style="animation-delay:${i * 0.02}s"><span class="log-time">${l.time}</span><span class="log-msg">${l.msg}</span></div>`
  ).join('') || '<div class="log-entry"><span class="log-time">--:--:--</span><span class="log-msg">等待系统事件…</span></div>';
}

// ═══════════════════════════════════════════
// 9. Toast 通知
// ═══════════════════════════════════════════
function toast(msg, type) {
  const container = $('#toast-container');
  if (!container) return;
  const el = document.createElement('div');
  el.className = `toast ${type || ''}`;
  el.textContent = msg;
  container.appendChild(el);
  setTimeout(() => {
    el.style.opacity = '0'; el.style.transform = 'translateY(-10px)';
    el.style.transition = 'all 0.3s ease-in';
    setTimeout(() => el.remove(), 300);
  }, 2800);
}

// ═══════════════════════════════════════════
// 10. 进程 CPU 增量计算引擎
//    后端传来的 cpu_percent 实际是累计 CPU 秒数，
//    需要用两次采集之间的差值来算真实占用率。
// ═══════════════════════════════════════════

/**
 * 根据增量计算每个进程的真实 CPU 占用
 * @param {Array} procs  - 当前进程列表
 * @param {number} cores - CPU 核心数
 * @returns {Array} 附加了 realCpuPct / coreCount / corePct 的进程列表
 */
function calcProcessCPU(procs, cores) {
  const now = Date.now();
  const timeDelta = state.lastMetricsTime
    ? (now - state.lastMetricsTime) / 1000
    : 5; // 首次默认 5 秒间隔
  state.lastMetricsTime = now;

  // 首次加载不做增量计算（没有上一轮数据）
  if (state.firstMetricsLoad) {
    state.firstMetricsLoad = false;
    // 保存首轮快照
    const snap = {};
    procs.forEach(p => { snap[p.pid] = { cpuSec: p.cpu_percent || 0, time: now }; });
    state.prevProcesses = snap;
    // 首轮显示 "—" 等待下次刷新
    return procs.map(p => ({ ...p, realCpuPct: null, coreCount: null, singleCorePct: null }));
  }

  const prev = state.prevProcesses;
  const coresClamped = Math.max(cores, 1);

  const result = procs.map(p => {
    const rawCpu = p.cpu_percent || 0;
    const prevEntry = prev[p.pid];

    // 计算增量占用率
    let realCpuPct = 0;
    if (prevEntry && timeDelta > 0.1) {
      const cpuDelta = rawCpu - prevEntry.cpuSec;
      // 进程可能重启（PID 复用导致 CPU 归零），此时 delta 为负，取 0
      realCpuPct = Math.max(0, (cpuDelta / timeDelta) * 100);
    }

    // 单核等效：占满一个核 = 100%
    const singleCorePct = Math.min(realCpuPct, 100);

    // 占用核数
    const coreCount = realCpuPct / 100;

    return { ...p, realCpuPct, coreCount, singleCorePct };
  });

  // 保存当前快照供下次使用
  const newSnap = {};
  procs.forEach(p => { newSnap[p.pid] = { cpuSec: p.cpu_percent || 0, time: now }; });
  state.prevProcesses = newSnap;

  return result;
}

// ═══════════════════════════════════════════
// 11. 指标刷新（核心数据链路）
// ═══════════════════════════════════════════
async function refreshMetrics() {
  try {
    const data = await api.get('/metrics');
    if (!data || !data.cpu) return;

    // ── 保存核心数 ──
    if (data.cpu.cores) {
      state.cpuCores = data.cpu.cores;
      $('#stat-cores').textContent = data.cpu.cores;
      $('#sys-cores').textContent = data.cpu.cores + ' 核';
    }

    // ── 系统级 CPU / 内存 / 磁盘 卡片 ──
    const metrics = [
      { id: 'stat-cpu', bar: 'bar-cpu', value: data.cpu.usage_percent || 0, delay: 0 },
      { id: 'stat-memory', bar: 'bar-memory', value: data.memory.usage_percent || 0, delay: 120 },
      { id: 'stat-disk', bar: 'bar-disk', value: data.disk.usage_percent || 0, delay: 220 },
    ];

    linkageBus.emit('data:refreshing', { metrics });

    metrics.forEach(({ id, bar, value, delay }) => {
      setTimeout(() => {
        animateValue(id, value);
        if (bar) animateBar(bar, value);
      }, delay);
    });

    // ── 系统信息 ──
    if (data.cpu.model) {
      const osEl = $('#sys-os');
      if (osEl) osEl.textContent = data.cpu.model.split(' ').slice(0, 3).join(' ');
    }
    if (data.memory.used_gb !== undefined) {
      $('#stat-mem-used').textContent = (data.memory.used_gb || 0).toFixed(1);
    }
    if (data.memory.total_gb) {
      $('#sys-total-mem').textContent = data.memory.total_gb.toFixed(1) + ' GB';
    }
    if (data.memory.usage_percent !== undefined) {
      updateRing('mem', data.memory.usage_percent || 0);
    }
    if (data.disk.used_gb !== undefined) {
      $('#stat-disk-used').textContent = (data.disk.used_gb || 0).toFixed(1);
    }
    if (data.disk.usage_percent !== undefined) {
      updateRing('disk', data.disk.usage_percent || 0);
    }
    if (data.cpu.usage_percent !== undefined) {
      updateRing('cpu', data.cpu.usage_percent || 0);
    }
    if (data.system) {
      if (data.system.hostname && $('#sys-hostname')) $('#sys-hostname').textContent = data.system.hostname;
    }

    // ── 进程列表（带增量 CPU 计算）──
    if (data.processes && data.processes.length > 0) {
      const enriched = calcProcessCPU(data.processes, state.cpuCores);
      renderProcesses(enriched, state.cpuCores);
    }

    // ── 首次连接 ──
    if (!state.connected) {
      state.connected = true;
      $('#status-text').textContent = '系统就绪';
      const dot = $('#status-dot');
      if (dot) dot.classList.remove('error');
      addLog('◈ DevOps Agent 已连接 — 运维智能体就绪');
      addLog(`CPU ${state.cpuCores}核 · 内存 ${(data.memory.usage_percent || 0).toFixed(1)}% · 磁盘 ${(data.disk.usage_percent || 0).toFixed(1)}%`);
    }

    // ── 健康环 ──
    const healthOk = (data.cpu.usage_percent || 0) < 90 && (data.memory.usage_percent || 0) < 90;
    updateRing('health', healthOk ? 100 : 60);
    const healthCenter = document.querySelector('#vital-health .vital-ring-center');
    if (healthCenter) {
      healthCenter.textContent = healthOk ? '✓' : '!';
      healthCenter.style.color = healthOk ? '' : '#FF3B30';
    }

    setTimeout(() => { linkageBus.emit('data:refreshed', { metrics, timestamp: Date.now() }); }, 600);
  } catch (_) {
    state.connected = false;
    $('#status-text').textContent = '连接中断';
    const dot = $('#status-dot');
    if (dot) dot.classList.add('error');
  }
}

// ═══════════════════════════════════════════
// 12. 进程列表渲染（单核/多核细分展示）
// ═══════════════════════════════════════════
function renderProcesses(procs, cores) {
  const list = $('#process-list');
  if (!list) return;

  // 按真实 CPU 占用降序排列
  const sorted = procs
    .filter(p => p.realCpuPct !== null)
    .sort((a, b) => (b.realCpuPct || 0) - (a.realCpuPct || 0));

  // 如果没有增量数据（首次加载），显示原始列表
  const display = sorted.length > 0 ? sorted : procs;

  list.innerHTML = display.map(p => {
    const hasData = p.realCpuPct !== null;
    const cpuPct = hasData ? p.realCpuPct : 0;

    // ── 单核占比（用于进度条）──
    const singleCorePct = hasData ? Math.min(cpuPct, 100) : 0;

    // ── 占用核数显示 ──
    let coreLabel, coreClass;
    if (!hasData) {
      coreLabel = '—';
      coreClass = '';
    } else if (cpuPct < 5) {
      coreLabel = '&lt;0.1核';
      coreClass = 'cpu-idle';
    } else if (cpuPct < 100) {
      coreLabel = (cpuPct / 100).toFixed(1) + '核';
      coreClass = cpuPct > 70 ? 'cpu-warn' : 'cpu-ok';
    } else {
      coreLabel = (cpuPct / 100).toFixed(1) + '核';
      coreClass = cpuPct > cores * 0.7 ? 'cpu-hot' : 'cpu-multi';
    }

    // ── 单核百分比（辅助信息）──
    const singleLabel = hasData ? Math.round(singleCorePct) + '%' : '—';

    return `
      <div class="process-item">
        <span class="process-name" title="${escHtml(p.name)} (PID:${p.pid})">${escHtml(p.name)}</span>
        <span class="process-pid">PID ${p.pid}</span>
        <span class="process-core ${coreClass}" title="总CPU占用 / ${state.cpuCores}核">${coreLabel}</span>
        <span class="process-cpu" title="单核等效占用">${singleLabel}</span>
        <span class="process-mem">${(p.memory_mb || 0).toFixed(0)} MB</span>
        <div class="process-bar-wrap">
          <div class="process-bar" style="width:${Math.min(singleCorePct, 100)}%"></div>
        </div>
      </div>`;
  }).join('');

  // 首次加载提示
  const anyData = display.some(p => p.realCpuPct !== null);
  if (!anyData && display.length > 0) {
    addLog('进程列表等待增量数据 — 5秒后显示真实 CPU 占用');
  }
}

// ═══════════════════════════════════════════
// 13. Agent 状态刷新
// ═══════════════════════════════════════════
async function refreshStatus() {
  try {
    const data = await api.get('/status');
    if (data) {
      if (data.tools_count !== undefined) {
        $('#sys-tools-count').textContent = data.tools_count + ' 个';
      }
    }
  } catch (_) {}

  // Agent 运行时长
  const uptime = Math.floor((Date.now() - state.startTime) / 1000);
  const h = Math.floor(uptime / 3600);
  const m = Math.floor((uptime % 3600) / 60);
  const s = uptime % 60;
  const uptimeEl = $('#stat-uptime');
  if (uptimeEl) uptimeEl.textContent = `${String(h).padStart(2, '0')}:${String(m).padStart(2, '0')}:${String(s).padStart(2, '0')}`;

  // 故障统计
  try {
    const faults = await api.get('/faults');
    if (faults) {
      animateValue('stat-faults', faults.total || 0);
      animateBar('bar-faults', Math.min((faults.total || 0) / 50 * 100, 100));
      $('#fs-total').textContent = faults.total || 0;
      $('#fs-critical').textContent = faults.critical || 0;
      $('#fs-high').textContent = faults.high || 0;
      $('#fs-resolved').textContent = faults.resolved || 0;
    }
  } catch (_) {}
}

// ═══════════════════════════════════════════
// 14. 加载工具列表
// ═══════════════════════════════════════════
async function loadTools() {
  try {
    const data = await api.get('/tools');
    const select = $('#tool-select');
    if (!select || !data.tools) return;
    select.innerHTML = '<option value="">-- 请选择运维工具 --</option>' +
      data.tools.map(t => {
        const icons = { system: '🖥️', log: '📋', service: '⚙️', network: '🌐', metrics: '📊', memory: '🧠' };
        const icon = icons[t.category] || '🔧';
        return `<option value="${t.name}">${icon} ${t.name} — ${t.description.slice(0, 50)}…</option>`;
      }).join('');
  } catch (_) {}
}

// ═══════════════════════════════════════════
// 15. 模式切换
// ═══════════════════════════════════════════
function initModeTabs() {
  $$('.segment-btn[data-tab]').forEach(btn => {
    btn.addEventListener('click', () => {
      const tabId = btn.dataset.tab;
      btn.parentElement.querySelectorAll('.segment-btn').forEach(b => b.classList.remove('active'));
      btn.classList.add('active');
      const section = btn.closest('section, .operation-panel, main');
      const container = section || document;
      container.querySelectorAll('.tab-content').forEach(c => c.classList.remove('active'));
      const target = $('#tab-' + tabId);
      if (target) target.classList.add('active');
      linkageBus.emit('tab:switched', { tab: tabId });
    });
  });
}

// ═══════════════════════════════════════════
// 16. 快捷命令预设
// ═══════════════════════════════════════════
window.applyPreset = function () {
  const preset = $('#tool-preset').value;
  const presets = {
    sys_info:      { tool: 'get_system_info',  args: '{}' },
    top_cpu:       { tool: 'list_processes',   args: '{"sort_by":"cpu","top_n":10}' },
    top_mem:       { tool: 'list_processes',   args: '{"sort_by":"memory","top_n":10}' },
    check_8080:    { tool: 'check_port',       args: '{"port":8080}' },
    list_services: { tool: 'list_services',    args: '{"status":"all"}' },
    parse_errors:  { tool: 'parse_error_log',  args: '{"path":"C:\\\\logs\\\\app.log","level":"ERROR"}' },
  };
  if (presets[preset]) {
    $('#tool-select').value = presets[preset].tool;
    $('#tool-args').value = presets[preset].args;
  }
};

// ═══════════════════════════════════════════
// 17. 事件处理器
// ═══════════════════════════════════════════

// 日志模式切换
$('#log-mode')?.addEventListener('change', function () {
  const kwGroup = $('#log-keyword-group');
  if (kwGroup) kwGroup.style.display = this.value === 'search' ? 'block' : 'none';
});

// 工具执行
$('#btn-execute-tool')?.addEventListener('click', async () => {
  const name = $('#tool-select').value;
  let args = {};
  try { args = JSON.parse($('#tool-args').value || '{}'); } catch (_) { toast('参数 JSON 格式错误', 'error'); return; }
  if (!name) { toast('请选择工具', 'error'); return; }

  const outputDiv = $('#tool-output');
  outputDiv.innerHTML = '<span style="color:var(--accent-primary)">> 正在执行 ' + name + '……</span>';

  try {
    const result = await api.post('/tools/execute', { name, args });
    outputDiv.innerHTML = result.success
      ? `<span style="color:var(--color-success)">> ✅ 执行成功 (${(result.duration / 1e6).toFixed(0)}ms)</span>\n\n${escHtml(result.output || '')}`
      : `<span style="color:var(--color-danger)">> ❌ 执行失败</span>\n${escHtml(result.error || '')}\n\n${escHtml(result.output || '')}`;
    addLog(`🔧 ${name} ${result.success ? '✅' : '❌'}`);
    if (result.success) linkageBus.emit('tool:executed', { name, success: true });
  } catch (err) {
    outputDiv.innerHTML = `<span style="color:var(--color-danger)">> ❌ 异常: ${escHtml(err.message)}</span>`;
    toast('工具执行失败', 'error');
  }
});

// 智能诊断
$('#btn-diagnose')?.addEventListener('click', async () => {
  const query = $('#diag-query').value.trim();
  const server = $('#diag-server').value.trim();
  const tags = $('#diag-tags').value.split(',').map(t => t.trim()).filter(Boolean);

  if (!query) { toast('请描述问题', 'error'); return; }

  const outputDiv = $('#diagnosis-output');
  outputDiv.innerHTML = '<span style="color:var(--accent-primary)">> ◈ 正在执行智能诊断……</span>\n> 采集系统指标 + 匹配工具 + 分析故障模式……';

  try {
    const result = await api.post('/diagnose', { query, server_name: server, tags });
    let html = '';

    if (result.metrics_snapshot) {
      const m = result.metrics_snapshot;
      html += '## 📊 实时系统指标\n\n';
      html += '| 指标 | 数值 |\n|------|------|\n';
      html += `| CPU | ${m.cpu?.usage_percent || 0}% (${m.cpu?.cores || '?'}核) |\n`;
      html += `| 内存 | ${m.memory?.usage_percent || 0}% (${m.memory?.used_gb || 0}/${m.memory?.total_gb || 0} GB) |\n`;
      html += `| 磁盘 | ${m.disk?.usage_percent || 0}% (${m.disk?.used_gb || 0}/${m.disk?.total_gb || 0} GB) |\n\n`;
    }

    if (result.diagnosis) html += result.diagnosis + '\n';

    if (result.related_faults && result.related_faults.length > 0) {
      html += '\n## 📚 相关历史故障\n\n';
      result.related_faults.forEach(f => {
        html += `- **[${f.severity}] ${f.title}**: ${(f.summary || '').slice(0, 100)}\n`;
      });
    }

    outputDiv.innerHTML = html || '诊断完成，无异常发现。';
    addLog(`🔍 智能诊断: "${query.slice(0, 40)}..." — 耗时${(result.duration / 1e9).toFixed(1)}s`);
    linkageBus.emit('diagnosis:completed', { query, result });
  } catch (err) {
    outputDiv.innerHTML = `<span style="color:var(--color-danger)">> ❌ 诊断异常: ${escHtml(err.message)}</span>`;
    toast('诊断失败', 'error');
  }
});

// 日志分析
$('#btn-analyze-log')?.addEventListener('click', async () => {
  const path = $('#log-path').value.trim();
  const mode = $('#log-mode').value;
  if (!path) { toast('请输入日志路径', 'error'); return; }

  const outputDiv = $('#log-output');
  outputDiv.innerHTML = '<span style="color:var(--accent-primary)">> 正在分析日志……</span>';

  let endpoint, body;
  if (mode === 'search') {
    endpoint = '/logs/search';
    body = { path, keyword: $('#log-keyword').value.trim(), tail_lines: 500 };
    if (!body.keyword) { toast('请输入搜索关键词', 'error'); return; }
  } else if (mode === 'parse') {
    endpoint = '/logs/analyze';
    body = { path, level: 'ERROR' };
  }

  try {
    const result = await api.post(endpoint, body);
    outputDiv.innerHTML = result.success
      ? `<span style="color:var(--color-success)">> ✅ 分析完成</span>\n\n${escHtml(result.output || '')}`
      : `<span style="color:var(--color-danger)">> ❌ 失败: ${escHtml(result.error || '')}</span>`;
    addLog(`📋 日志分析: ${mode} — ${path}`);
  } catch (err) {
    outputDiv.innerHTML = `<span style="color:var(--color-danger)">> ❌ 异常: ${escHtml(err.message)}</span>`;
  }
});

// 故障搜索
$('#btn-search-faults')?.addEventListener('click', async () => {
  const query = $('#fault-search-query').value.trim();
  if (!query) { toast('请输入搜索关键词', 'error'); return; }

  const listDiv = $('#fault-list');
  listDiv.innerHTML = '<div class="terminal-placeholder">正在搜索……</div>';

  try {
    const result = await api.post('/faults/search', { query, limit: 10 });
    if (!result.faults || result.faults.length === 0) {
      listDiv.innerHTML = '<div class="terminal-placeholder">未找到相关故障记录</div>';
      return;
    }
    listDiv.innerHTML = result.faults.map((f, i) => `
      <div class="fault-item glass-card ${f.severity} ${f.resolved ? 'resolved' : ''}" style="animation-delay:${i * 0.05}s">
        <div class="fault-title">${f.resolved ? '✅ ' : ''}[${f.severity.toUpperCase()}] ${escHtml(f.title || '(无标题)')}</div>
        <div class="fault-meta">
          服务器: ${escHtml(f.server_name || '--')} | 时间: ${f.occurred_at || '--'} | ${f.resolved ? '已解决' : '未解决'}
        </div>
        ${f.solution ? `<div style="margin-top:4px;font-size:11px;color:var(--color-success)">💡 方案: ${escHtml(f.solution.slice(0, 150))}</div>` : ''}
      </div>
    `).join('');
    addLog(`🔍 故障搜索: "${query}" → ${result.faults.length} 条`);
    linkageBus.emit('faults:searched', { query, results: result.faults });
  } catch (err) {
    listDiv.innerHTML = `<div class="terminal-placeholder" style="color:var(--color-danger)">搜索失败: ${escHtml(err.message)}</div>`;
  }
});

// 刷新按钮
$('#btn-refresh')?.addEventListener('click', () => {
  const btn = $('#btn-refresh');
  if (btn) { btn.classList.add('spinning'); setTimeout(() => btn.classList.remove('spinning'), 800); }
  refreshMetrics();
  refreshStatus();
  toast('数据已刷新', 'success');
});

// ═══════════════════════════════════════════
// 18. 工具函数
// ═══════════════════════════════════════════
function escHtml(text) {
  if (!text) return '';
  const div = document.createElement('div');
  div.textContent = String(text);
  return div.innerHTML;
}

// ═══════════════════════════════════════════
// 19. 初始化
// ═══════════════════════════════════════════
async function init() {
  initScrollEffects();
  initArchViews();
  initRings();
  initCardLinkage();
  initModeTabs();

  try {
    const status = await api.get('/status');
    if (status) {
      state.connected = true;
      $('#status-text').textContent = '系统就绪';
      const dot = $('#status-dot');
      if (dot) dot.classList.remove('error');
      addLog('◈ DevOps Agent v0.2.0 — 运维智能体已激活');
      addLog(`已注册 ${status.tools_count || '?'} 个运维工具 · Nebula 记忆引擎就绪`);
      if (status.tools_count !== undefined) {
        $('#sys-tools-count').textContent = status.tools_count + ' 个';
      }
    }
  } catch (_) {
    state.connected = false;
    $('#status-text').textContent = '连接中断';
    const dot = $('#status-dot');
    if (dot) dot.classList.add('error');
    addLog('⚠ Agent 不可达 — 请先启动 devops-server');
  }

  await loadTools();
  await refreshMetrics();
  await refreshStatus();

  setInterval(refreshMetrics, 5000);
  setInterval(refreshStatus, 15000);
}

document.addEventListener('DOMContentLoaded', init);
