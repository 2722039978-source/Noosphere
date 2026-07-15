/**
 * NEBULA AGENT — 星云记忆引擎
 * Apple Design · 粒子背景 · 元素联动 · 弹性动画
 * 让每一次对话，都拥有记忆。
 */

const API_BASE = '/api/v1';
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
};

// ═══════════════════════════════════════════
// 1. 联动事件总线 (Linkage Bus)
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
// 2. 粒子背景 (Apple 风格星空)
// ═══════════════════════════════════════════
function initParticles() {
  const canvas = $('#particles-canvas');
  if (!canvas) return;
  const ctx = canvas.getContext('2d');

  let w, h;
  const particles = [];
  const PARTICLE_COUNT = 80;

  function resize() {
    w = canvas.width = window.innerWidth;
    h = canvas.height = window.innerHeight;
  }

  // 创建粒子
  for (let i = 0; i < PARTICLE_COUNT; i++) {
    particles.push({
      x: Math.random() * 2000,
      y: Math.random() * 2000,
      r: Math.random() * 1.5 + 0.5,
      vx: (Math.random() - 0.5) * 0.15,
      vy: (Math.random() - 0.5) * 0.1 - 0.05,
      o: Math.random() * 0.5 + 0.2,
      pulse: Math.random() * Math.PI * 2,
      pulseSpeed: Math.random() * 0.02 + 0.005,
    });
  }

  function animate() {
    ctx.clearRect(0, 0, w, h);

    for (const p of particles) {
      p.x += p.vx;
      p.y += p.vy;
      p.pulse += p.pulseSpeed;

      // 边界循环
      if (p.x < -10) p.x = w + 10;
      if (p.x > w + 10) p.x = -10;
      if (p.y < -10) p.y = h + 10;
      if (p.y > h + 10) p.y = -10;

      const alpha = p.o + Math.sin(p.pulse) * 0.15;

      ctx.beginPath();
      ctx.arc(p.x, p.y, p.r, 0, Math.PI * 2);
      ctx.fillStyle = `rgba(255, 255, 255, ${alpha})`;
      ctx.fill();

      // 偶尔微光
      if (Math.sin(p.pulse) > 0.85) {
        ctx.beginPath();
        ctx.arc(p.x, p.y, p.r * 3, 0, Math.PI * 2);
        ctx.fillStyle = `rgba(139, 120, 255, ${alpha * 0.2})`;
        ctx.fill();
      }
    }

    requestAnimationFrame(animate);
  }

  resize();
  animate();
  window.addEventListener('resize', resize);
}

// ═══════════════════════════════════════════
// 3. 滚动进度条 + 视口触发
// ═══════════════════════════════════════════
function initScrollEffects() {
  // 进度条
  window.addEventListener('scroll', () => {
    const scrollTop = window.scrollY;
    const docHeight = document.documentElement.scrollHeight - window.innerHeight;
    const progress = Math.min(scrollTop / docHeight, 1);
    const bar = $('#scroll-progress');
    if (bar) bar.style.transform = `scaleX(${progress})`;
  });

  // IntersectionObserver 滚动揭示
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
// 4. 架构视图切换
// ═══════════════════════════════════════════
function initArchViewSwitch() {
  const dots = $$('.arch-dot');
  const views = $$('.arch-view');
  const viewport = $('#arch-viewport');
  if (!dots.length || !views.length) return;

  function switchToView(viewId, idx) {
    // 隐藏所有
    views.forEach(v => v.classList.remove('view-active'));
    dots.forEach(d => d.classList.remove('active'));

    // 显示目标
    const target = document.getElementById(viewId);
    if (target) {
      target.classList.add('view-active');
      dots[idx].classList.add('active');
      state.currentView = idx;
    }

    linkageBus.emit('view:switched', { viewId, index: idx });
  }

  // 点击切换
  dots.forEach((dot, i) => {
    dot.addEventListener('click', () => {
      switchToView(dot.dataset.view, i);
      resetAutoSwitch();
    });
  });

  // 能力卡片点击 → 切换到对应架构视图
  $$('.capability-card').forEach(card => {
    card.addEventListener('click', () => {
      const archViewId = card.dataset.archView;
      const targetDot = document.querySelector(`.arch-dot[data-view="${archViewId}"]`);
      if (targetDot) {
        const dotIdx = Array.from(dots).indexOf(targetDot);
        if (dotIdx >= 0) switchToView(archViewId, dotIdx);
        resetAutoSwitch();
      }
    });
  });

  // 悬浮暂停自动轮播
  if (viewport) {
    viewport.addEventListener('mouseenter', stopAutoSwitch);
    viewport.addEventListener('mouseleave', startAutoSwitch);
  }

  function autoSwitch() {
    const next = (state.currentView + 1) % dots.length;
    switchToView(dots[next].dataset.view, next);
  }

  function startAutoSwitch() {
    state.viewTimer = setInterval(autoSwitch, 6000);
  }

  function stopAutoSwitch() {
    if (state.viewTimer) { clearInterval(state.viewTimer); state.viewTimer = null; }
  }

  function resetAutoSwitch() {
    stopAutoSwitch();
    startAutoSwitch();
  }

  startAutoSwitch();
}

// ═══════════════════════════════════════════
// 5. 监控卡片 — 悬浮联动 (邻居变暗 + SVG 高亮)
// ═══════════════════════════════════════════
function initCardLinkage() {
  const cards = $$('.monitor-card');

  cards.forEach((card, idx) => {
    card.addEventListener('mouseenter', () => {
      // 邻居变暗
      cards.forEach((c, i) => {
        if (i !== idx) c.classList.add('is-dimmed');
        else c.classList.add('is-elevated');
      });

      // 高亮对应 SVG 节点
      const metric = card.dataset.metric;
      if (metric) {
        linkageBus.emit('card:hover', { metric, cardId: card.id });
        $$(`.arch-svg [data-metric="${metric}"]`).forEach(el => el.classList.add('highlight'));
      }
    });

    card.addEventListener('mouseleave', () => {
      cards.forEach(c => {
        c.classList.remove('is-dimmed', 'is-elevated');
      });
      $$('.arch-svg [data-metric]').forEach(el => el.classList.remove('highlight'));
      linkageBus.emit('card:hover', null);
    });

    // 点击涟漪 + 刷新
    card.addEventListener('click', (e) => {
      const ripple = document.createElement('div');
      ripple.className = 'card-ripple';
      const rect = card.getBoundingClientRect();
      ripple.style.left = (e.clientX - rect.left) + 'px';
      ripple.style.top = (e.clientY - rect.top) + 'px';
      card.appendChild(ripple);
      setTimeout(() => ripple.remove(), 600);

      refreshStats();
      toast('数据已刷新', 'success');
    });
  });
}

// ═══════════════════════════════════════════
// 6. 能力卡片 — 悬浮高亮记忆类型
// ═══════════════════════════════════════════
function initCapabilityLinkage() {
  $$('.capability-card').forEach(card => {
    card.addEventListener('mouseenter', () => {
      const memType = card.dataset.memoryType;
      linkageBus.emit('capability:hover', { memoryType: memType });
    });
    card.addEventListener('mouseleave', () => {
      linkageBus.emit('capability:hover', null);
    });
  });
}

// ═══════════════════════════════════════════
// 7. 弹性数字动画 + 进度条
// ═══════════════════════════════════════════
function animateValue(id, target, duration = 600) {
  const el = $('#' + id);
  if (!el) return;
  const current = parseInt(el.textContent) || 0;
  if (current === target) return;

  const start = performance.now();
  const initial = current;

  function update(now) {
    const elapsed = now - start;
    const p = Math.min(elapsed / duration, 1);
    // Spring easing: cubic-bezier(0.34, 1.56, 0.64, 1)
    const eased = p < 1
      ? 1 - Math.pow(1 - p, 3)
      : 1 + Math.sin((p - 1) * Math.PI * 2) * Math.exp(-(p - 1) * 8) * 0.08;
    el.textContent = Math.round(initial + (target - initial) * Math.min(eased, 1.04));
    if (p < 1) requestAnimationFrame(update);
  }
  requestAnimationFrame(update);
}

function animateBar(id, pct) {
  const bar = $('#' + id);
  if (!bar) return;
  bar.style.width = Math.min(pct, 100) + '%';
}

function formatBytes(bytes) {
  if (!bytes || bytes === 0) return '0 B';
  const k = 1024;
  const sizes = ['B', 'KB', 'MB', 'GB'];
  const i = Math.floor(Math.log(bytes) / Math.log(k));
  return parseFloat((bytes / Math.pow(k, i)).toFixed(1)) + ' ' + sizes[i];
}

// ═══════════════════════════════════════════
// 8. API 客户端
// ═══════════════════════════════════════════
const api = {
  async request(method, path, body) {
    const res = await fetch(API_BASE + path, {
      method,
      headers: { 'Content-Type': 'application/json' },
      body: body ? JSON.stringify(body) : undefined,
    });
    return res.json();
  },
  health: () => api.request('GET', '/../health'),
  stats: () => api.request('GET', '/stats'),
  storeMemory: (sid, data) => api.request('POST', `/sessions/${sid}/memories`, data),
  search: (sid, data) => api.request('POST', `/sessions/${sid}/search`, data),
};

// ═══════════════════════════════════════════
// 9. 日志系统
// ═══════════════════════════════════════════
function addLog(msg) {
  const time = new Date().toTimeString().slice(0, 8);
  state.logs.unshift({ time, msg });
  if (state.logs.length > 30) state.logs.pop();
  renderLogs();
}

function renderLogs() {
  const stream = $('#log-stream');
  if (!stream) return;
  stream.innerHTML = state.logs.map((l, i) => `
    <div class="log-entry" style="animation-delay:${i * 0.02}s">
      <span class="log-time">${l.time}</span>
      <span class="log-msg">${l.msg}</span>
    </div>
  `).join('') || '<div class="log-entry"><span class="log-time">--:--:--</span><span class="log-msg">等待输入信号……</span></div>';
}

// ═══════════════════════════════════════════
// 10. Toast 通知
// ═══════════════════════════════════════════
function toast(msg, type) {
  const container = $('#toast-container');
  if (!container) return;
  const el = document.createElement('div');
  el.className = `toast ${type || ''}`;
  el.textContent = msg;
  container.appendChild(el);
  setTimeout(() => {
    el.style.opacity = '0';
    el.style.transform = 'translateY(-10px)';
    el.style.transition = 'all 0.3s ease-in';
    setTimeout(() => el.remove(), 300);
  }, 2800);
}

// ═══════════════════════════════════════════
// 11. 数据刷新 — 链式错开更新
// ═══════════════════════════════════════════
async function refreshStats() {
  try {
    const data = await api.stats();
    if (!data || !data.engine_stats) return;

    const s = data.engine_stats;
    const maxMem = Math.max(s.total_memories || 1, 100);

    // 链式错开更新（最重要指标优先）
    const metrics = [
      { id: 'stat-total', bar: 'bar-total', value: s.total_memories || 0, max: maxMem, delay: 0 },
      { id: 'stat-episodic', bar: 'bar-episodic', value: s.episodic_count || 0, max: maxMem, delay: 120 },
      { id: 'stat-semantic', bar: 'bar-semantic', value: s.semantic_count || 0, max: maxMem, delay: 220 },
      { id: 'stat-vector', bar: 'bar-vector', value: s.vector_count || 0, max: maxMem, delay: 320 },
    ];

    linkageBus.emit('data:refreshing', { metrics });

    metrics.forEach(({ id, bar, value, max, delay }) => {
      setTimeout(() => {
        animateValue(id, value);
        if (bar) animateBar(bar, (value / max) * 100);
      }, delay);
    });

    // Hero 区同步更新
    setTimeout(() => {
      animateValue('hero-total', s.total_memories || 0, 800);
      if ($('#sys-uptime')) $('#sys-uptime').textContent = s.uptime || '--';
    }, 100);

    // 缓存命中率
    const hitRate = ((s.cache_hit_rate || 0) * 100).toFixed(1);
    const hitEl = $('#stat-hitrate');
    if (hitEl) hitEl.textContent = hitRate + '%';
    const barHit = $('#bar-hit');
    if (barHit) barHit.style.width = Math.min(hitRate, 100) + '%';

    // 运行时间
    if (s.uptime) {
      const uptimeEl = $('#stat-uptime');
      if (uptimeEl) uptimeEl.textContent = s.uptime;
    }

    // 系统参数
    if ($('#sys-cache')) $('#sys-cache').textContent = `${s.cache_size || 0} / ${s.cache_max_size || 10000}`;
    if ($('#sys-disk')) $('#sys-disk').textContent = formatBytes(s.disk_used_bytes || 0);

    // 首次连接成功
    if (!state.connected) {
      state.connected = true;
      if ($('#status-text')) $('#status-text').textContent = '系统就绪';
      const dot = $('#status-dot');
      if (dot) dot.classList.remove('error');
      addLog('◈ 星云记忆引擎已连接 — 莱茵生命协议已激活');
      addLog('HNSW 向量索引就绪 — 维度: 1536');
    }

    setTimeout(() => {
      linkageBus.emit('data:refreshed', { metrics, timestamp: Date.now() });
    }, 600);
  } catch (_) {
    state.connected = false;
    if ($('#status-text')) $('#status-text').textContent = '连接中断';
    const dot = $('#status-dot');
    if (dot) dot.classList.add('error');
  }
}

// ═══════════════════════════════════════════
// 12. HNSW 拓扑可视化
// ═══════════════════════════════════════════
function initTopologyCanvas() {
  const canvas = $('#topology-canvas');
  const container = $('#topo-wrap');
  const tooltip = $('#topo-tooltip');
  if (!canvas || !container) return;

  const ctx = canvas.getContext('2d');
  let nodes = [], edges = [], angle = 0;
  let mouseX = -200, mouseY = -200;
  let hoverNodeIdx = -1;
  let w = 0, h = 0;
  let rippleWave = 0;

  async function loadMemoryData() {
    try {
      const sid = ($('#store-session')?.value || $('#search-session')?.value || 'demo-agent').trim();
      const resp = await fetch(`/api/v1/sessions/${sid}/search`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ query: '', top_k: 50, strategy: 'keyword', threshold: 0 }),
      });
      const data = await resp.json();
      return (data.results || []).map(r => ({
        id: r.memory?.id || '',
        type: r.memory?.type || 'episodic',
        content: r.memory?.content || '',
        score: r.score || 0,
        tags: r.memory?.tags || [],
        importance: r.memory?.importance || 0.5,
        accessCnt: r.memory?.access_cnt || 0,
        created: r.memory?.created_at || '',
      }));
    } catch (_) {
      return [];
    }
  }

  function resize() {
    const rect = container.getBoundingClientRect();
    const dpr = window.devicePixelRatio || 1;
    w = rect.width;
    h = 520;
    canvas.width = w * dpr;
    canvas.height = h * dpr;
    canvas.style.width = w + 'px';
    canvas.style.height = h + 'px';
    ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
  }

  function buildGraph(memories) {
    nodes = [];
    edges = [];
    const total = Math.max(memories.length, 30);

    for (let i = 0; i < total; i++) {
      const mem = memories[i] || {
        id: 'node-' + i,
        type: ['episodic', 'semantic', 'working', 'procedural'][i % 4],
        content: '示例记忆节点 #' + (i + 1),
        score: Math.random() * 0.5 + 0.5,
        tags: ['demo', 'sample'],
        importance: Math.random(),
        accessCnt: Math.floor(Math.random() * 20),
        created: new Date().toISOString(),
      };

      const layer = mem.type === 'working' ? 3 : mem.type === 'semantic' ? 1 : mem.type === 'procedural' ? 2 : 0;
      const layerCounts = [total * 0.5, total * 0.3, total * 0.15, total * 0.05];
      const layerIdx = [0, 1, 2, 3].indexOf(layer);
      const radius = (w * 0.42) * ((layerIdx + 0.7) / 4);
      const angleOff = (i / Math.max(layerCounts[layerIdx] || 1, 1)) * Math.PI * 2 + (Math.random() - 0.5) * 0.3;

      nodes.push({
        x: 0, y: 0,
        r: 3.5 - layerIdx * 0.6,
        layer: layerIdx,
        baseAngle: angleOff,
        baseRadius: radius,
        idx: i,
        pulse: Math.random() * Math.PI * 2,
        memory: mem,
        orbitSpeed: 0.08 / (layerIdx + 1),
      });
    }

    // 层内边
    for (let i = 0; i < nodes.length; i++) {
      for (let j = i + 1; j < nodes.length; j++) {
        if (nodes[i].layer !== nodes[j].layer) continue;
        const prob = 0.2 + (1 - (Math.abs(i - j) / nodes.length)) * 0.5;
        if (Math.random() < prob * 0.4) {
          edges.push({ from: i, to: j });
        }
      }
    }
    // 跨层边
    for (let i = 0; i < nodes.length; i++) {
      for (let j = i + 1; j < nodes.length; j++) {
        if (Math.abs(nodes[i].layer - nodes[j].layer) === 1 && Math.random() < 0.08) {
          edges.push({ from: i, to: j });
        }
      }
    }

    addLog(`拓扑图就绪 — ${nodes.length} 节点 · ${edges.length} 连接 · 4 层级`);
  }

  // 鼠标交互
  container.addEventListener('mousemove', (e) => {
    const rect = container.getBoundingClientRect();
    mouseX = e.clientX - rect.left;
    mouseY = e.clientY - rect.top;
  });

  container.addEventListener('mouseleave', () => {
    mouseX = -200; mouseY = -200;
    hoverNodeIdx = -1;
    if (tooltip) tooltip.classList.remove('visible');
  });

  function findHoverNode() {
    hoverNodeIdx = -1;
    for (let i = nodes.length - 1; i >= 0; i--) {
      const n = nodes[i];
      const dist = Math.hypot(n.x - mouseX, n.y - mouseY);
      if (dist < n.r * 6) {
        hoverNodeIdx = i;
        break;
      }
    }
  }

  function updateTooltip() {
    if (!tooltip) return;
    if (hoverNodeIdx >= 0) {
      const n = nodes[hoverNodeIdx];
      const m = n.memory;
      const tipX = Math.min(Math.max(n.x, 130), w - 130);
      const tipY = Math.max(n.y - 40, 120);
      tooltip.style.left = tipX + 'px';
      tooltip.style.top = tipY + 'px';

      const typeEl = $('#tooltip-type');
      if (typeEl) {
        typeEl.textContent = m.type.toUpperCase();
        typeEl.className = 'tooltip-type ' + m.type;
      }
      const scoreEl = $('#tooltip-score');
      if (scoreEl) scoreEl.textContent = (m.importance || 0).toFixed(2);
      const contentEl = $('#tooltip-content');
      if (contentEl) contentEl.textContent = (m.content || '').slice(0, 120) || '(无内容)';
      const idEl = $('#tooltip-id');
      if (idEl) idEl.textContent = (m.id || '').slice(0, 18) + '...';
      const accessEl = $('#tooltip-access');
      if (accessEl) accessEl.textContent = m.accessCnt || 0;

      const tagsEl = $('#tooltip-tags');
      if (tagsEl) {
        tagsEl.innerHTML = (m.tags || []).slice(0, 4).map(t =>
          `<span class="tooltip-tag">${escapeHtml(t)}</span>`
        ).join('');
      }

      tooltip.classList.add('visible');
    } else {
      tooltip.classList.remove('visible');
    }
  }

  // 监听记忆写入事件 → 涟漪
  linkageBus.on('memory:stored', () => {
    rippleWave = 1.0;
  });

  function animate() {
    ctx.clearRect(0, 0, w, h);
    angle += 0.002;

    // 背景微光: 中心辐射
    const bgGrad = ctx.createRadialGradient(w / 2, h / 2, 0, w / 2, h / 2, w * 0.5);
    bgGrad.addColorStop(0, 'rgba(139,120,255,0.04)');
    bgGrad.addColorStop(1, 'rgba(0,0,0,0)');
    ctx.fillStyle = bgGrad;
    ctx.fillRect(0, 0, w, h);

    // 涟漪
    if (rippleWave > 0.01) {
      const rippleR = (1 - rippleWave) * w * 0.6;
      ctx.beginPath();
      ctx.arc(w / 2, h / 2, rippleR, 0, Math.PI * 2);
      ctx.strokeStyle = `rgba(139,120,255,${rippleWave * 0.3})`;
      ctx.lineWidth = 2;
      ctx.stroke();
      rippleWave *= 0.96;
    }

    // 轨道
    for (let l = 0; l < 4; l++) {
      const r = w * 0.38 * ((l + 0.7) / 4);
      ctx.beginPath();
      ctx.arc(w / 2, h / 2, r, 0, Math.PI * 2);
      ctx.strokeStyle = `rgba(255,255,255,${0.03 + l * 0.01})`;
      ctx.lineWidth = 0.5;
      ctx.stroke();
    }

    // 更新位置
    for (const node of nodes) {
      node.x = w / 2 + Math.cos(node.baseAngle + angle * node.orbitSpeed) * node.baseRadius;
      node.y = h / 2 + Math.sin(node.baseAngle + angle * node.orbitSpeed) * node.baseRadius * 0.7;
    }

    findHoverNode();
    updateTooltip();

    // 边
    for (const edge of edges) {
      const a = nodes[edge.from], b = nodes[edge.to];
      if (!a || !b) continue;
      let alpha = 0.04, lw = 0.4;
      if (hoverNodeIdx === edge.from || hoverNodeIdx === edge.to) {
        alpha = 0.2; lw = 1;
      }
      ctx.beginPath();
      ctx.moveTo(a.x, a.y);
      ctx.lineTo(b.x, b.y);
      ctx.strokeStyle = `rgba(139,120,255,${alpha})`;
      ctx.lineWidth = lw;
      ctx.stroke();
    }

    // 节点
    const typeColors = { episodic: '#60A5FA', semantic: '#34D399', working: '#64D8FF', procedural: '#FBBF24' };
    for (const node of nodes) {
      const isHover = node.idx === hoverNodeIdx;
      const color = typeColors[node.memory.type] || '#8B78FF';

      // 光晕
      const glowR = isHover ? node.r * 7 : node.r * 2.5;
      const glow = ctx.createRadialGradient(node.x, node.y, 0, node.x, node.y, glowR);
      glow.addColorStop(0, color + '40');
      glow.addColorStop(1, 'rgba(0,0,0,0)');
      ctx.fillStyle = glow;
      ctx.beginPath();
      ctx.arc(node.x, node.y, glowR, 0, Math.PI * 2);
      ctx.fill();

      // 核心
      ctx.fillStyle = isHover ? '#ffffff' : color;
      ctx.beginPath();
      ctx.arc(node.x, node.y, isHover ? node.r * 1.7 : node.r, 0, Math.PI * 2);
      ctx.fill();

      // 选中环
      if (isHover) {
        ctx.strokeStyle = color;
        ctx.lineWidth = 2;
        ctx.beginPath();
        ctx.arc(node.x, node.y, node.r * 3, 0, Math.PI * 2);
        ctx.setLineDash([3, 3]);
        ctx.stroke();
        ctx.setLineDash([]);
      }

      // 脉冲
      const pulse = Math.sin(angle * 3 + node.pulse) * 0.5 + 0.5;
      if (pulse > 0.55 || isHover) {
        ctx.strokeStyle = color + '66';
        ctx.lineWidth = 0.6;
        ctx.beginPath();
        ctx.arc(node.x, node.y, node.r * (1.5 + pulse * 0.7), 0, Math.PI * 2);
        ctx.stroke();
      }
    }

    requestAnimationFrame(animate);
  }

  resize();
  loadMemoryData().then(memories => {
    buildGraph(memories);
    animate();
  });
  window.addEventListener('resize', resize);
}

// ═══════════════════════════════════════════
// 13. 模式标签切换
// ═══════════════════════════════════════════
function initModeTabs() {
  $$('.segment-btn[data-tab]').forEach(btn => {
    btn.addEventListener('click', () => {
      const tabId = btn.dataset.tab;

      // 更新 segment
      btn.parentElement.querySelectorAll('.segment-btn').forEach(b => b.classList.remove('active'));
      btn.classList.add('active');

      // 切换内容
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
// 14. 事件处理器
// ═══════════════════════════════════════════

// 重要性滑块
const impSlider = $('#store-importance');
const impVal = $('#importance-val');
if (impSlider && impVal) {
  impSlider.addEventListener('input', () => { impVal.textContent = impSlider.value; });
}

// 写入记忆
$('#btn-store')?.addEventListener('click', async () => {
  const sid = $('#store-session').value.trim();
  const type = $('#store-type').value;
  const content = $('#store-content').value.trim();
  const imp = parseFloat($('#store-importance').value);
  const tags = $('#store-tags').value.split(',').map(t => t.trim()).filter(Boolean);

  if (!sid || !content) { toast('请输入会话标识和记忆内容', 'error'); return; }

  const resultBox = $('#store-result');
  resultBox.textContent = '> 正在提交至记忆库……';
  resultBox.className = 'terminal-output';

  try {
    const data = await api.storeMemory(sid, { content, type, importance: imp, tags });
    const typeNames = { episodic: '情景记忆', semantic: '语义记忆', working: '工作记忆', procedural: '程序记忆' };
    resultBox.textContent = `> ✓ 写入成功 | 记忆ID: ${data.id} | 类别: ${typeNames[type] || type}`;
    addLog(`记忆写入 [${typeNames[type] || type}] ID:${data.id?.slice(0, 14)}... 内容:"${content.slice(0, 40)}..."`);

    // 联动：通知拓扑图
    linkageBus.emit('memory:stored', { type, content, importance: imp });

    refreshStats();
  } catch (err) {
    resultBox.textContent = `> ✗ 错误: ${err.message}`;
    resultBox.className = 'terminal-output error';
    toast('写入失败', 'error');
  }
});

// 检索记忆
$('#btn-search')?.addEventListener('click', async () => {
  const sid = $('#search-session').value.trim();
  const query = $('#search-query').value.trim();
  const strategy = $('#search-strategy').value;
  const topK = parseInt($('#search-topk').value) || 10;

  if (!sid || !query) { toast('请输入会话标识和搜索关键词', 'error'); return; }

  const resultsDiv = $('#search-results');
  resultsDiv.innerHTML = '<div class="terminal-output">> 正在检索记忆库……</div>';

  try {
    const data = await api.search(sid, { query, top_k: topK, strategy });
    const results = data.results || [];

    if (results.length === 0) {
      resultsDiv.innerHTML = '<div class="terminal-output">> 未找到匹配的记忆</div>';
      linkageBus.emit('memory:searched', { query, results: [], strategy });
      return;
    }

    const strategyNames = { hybrid: '混合检索', vector: '向量检索', keyword: '关键词检索', temporal: '时间衰减' };
    resultsDiv.innerHTML = results.map((r, i) => `
      <div class="search-result-item glass-card" style="animation-delay:${i * 0.05}s">
        <div class="result-header">
          <span class="result-type-badge ${r.memory?.type || 'episodic'}">${(r.memory?.type || '未知').toUpperCase()}</span>
          <span class="result-score">匹配度: ${(r.score * 100).toFixed(1)}%</span>
        </div>
        <div class="result-content">${escapeHtml(r.memory?.content || '(空)')}</div>
        <div class="result-meta">ID:${r.memory?.id?.slice(0, 14)}... | 重要性:${(r.memory?.importance || 0).toFixed(1)} | ${strategyNames[strategy] || strategy}</div>
      </div>
    `).join('');

    addLog(`记忆检索 "${query.slice(0, 30)}..." → ${results.length} 条结果 [${strategyNames[strategy]}]`);
    linkageBus.emit('memory:searched', { query, results, strategy });
  } catch (err) {
    resultsDiv.innerHTML = `<div class="terminal-output error">> 错误: ${err.message}</div>`;
    toast('检索失败', 'error');
  }
});

// 浏览记忆
$('#btn-list')?.addEventListener('click', async () => {
  const sid = $('#list-session').value.trim();
  if (!sid) { toast('请输入会话标识', 'error'); return; }

  const listDiv = $('#memory-list');
  listDiv.innerHTML = '<div class="terminal-output">> 正在扫描记忆库……</div>';

  try {
    const data = await api.search(sid, { query: '', top_k: 50, strategy: 'keyword', threshold: 0 });
    const results = data.results || [];

    if (results.length === 0) {
      listDiv.innerHTML = '<div class="terminal-output">> 此会话中没有记忆记录</div>';
      return;
    }

    listDiv.innerHTML = results.map((r, i) => `
      <div class="memory-item" style="animation-delay:${i * 0.03}s">
        <div>
          <span class="memory-type-tag ${r.memory?.type || ''}">${(r.memory?.type || '?').toUpperCase()}</span>
          ${escapeHtml((r.memory?.content || '').slice(0, 55))}
        </div>
        <span style="color:var(--text-tertiary);font-size:9px;">${r.memory?.created_at || ''}</span>
      </div>
    `).join('');

    addLog(`会话浏览 [${sid}] → 发现 ${results.length} 条记忆记录`);
  } catch (err) {
    listDiv.innerHTML = `<div class="terminal-output error">> 错误: ${err.message}</div>`;
  }
});

// 刷新按钮
$('#btn-refresh')?.addEventListener('click', () => {
  const btn = $('#btn-refresh');
  if (btn) {
    btn.classList.add('spinning');
    setTimeout(() => btn.classList.remove('spinning'), 800);
  }
  refreshStats();
  toast('数据已刷新', 'success');
});

// ═══════════════════════════════════════════
// 15. 工具函数
// ═══════════════════════════════════════════
function escapeHtml(text) {
  if (!text) return '';
  const div = document.createElement('div');
  div.textContent = text;
  return div.innerHTML;
}

// ═══════════════════════════════════════════
// 16. AI 对话 (iMessage 风格)
// ═══════════════════════════════════════════
function initChat() {
  const container = $('#chat-messages');
  const input = $('#chat-input');
  const sendBtn = $('#btn-chat-send');
  const injectCb = $('#chat-inject');
  const langSel = $('#chat-lang');

  if (!container || !input || !sendBtn) return;

  let chatHistory = [];

  function addMessage(role, content, meta) {
    const div = document.createElement('div');
    div.className = `chat-msg chat-msg-${role}`;
    let html = escapeHtml(content)
      .replace(/```(\w*)\n?([\s\S]*?)```/g, '<pre><code>$2</code></pre>')
      .replace(/`([^`]+)`/g, '<code>$1</code>')
      .replace(/\n/g, '<br>');
    div.innerHTML = html;
    if (meta) {
      const metaSpan = document.createElement('span');
      metaSpan.className = 'msg-meta';
      metaSpan.textContent = meta;
      div.appendChild(metaSpan);
    }
    container.appendChild(div);
    container.scrollTop = container.scrollHeight;
    return div;
  }

  function showTyping() {
    const div = document.createElement('div');
    div.className = 'chat-msg-typing';
    div.id = 'typing-indicator';
    div.innerHTML = '<span class="typing-dot"></span><span class="typing-dot"></span><span class="typing-dot"></span>';
    container.appendChild(div);
    container.scrollTop = container.scrollHeight;
  }

  function hideTyping() {
    const el = document.getElementById('typing-indicator');
    if (el) el.remove();
  }

  async function sendMessage() {
    const message = input.value.trim();
    if (!message) return;

    input.value = '';
    input.style.height = 'auto';

    addMessage('user', message);
    chatHistory.push({ role: 'user', content: message });

    showTyping();

    const sessionID = ($('#store-session')?.value || $('#search-session')?.value || 'demo-agent').trim();

    try {
      const resp = await fetch('/api/v1/chat', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          session_id: sessionID,
          message: message,
          language: langSel?.value || 'go',
          history: chatHistory.slice(0, -1),
          inject_context: injectCb?.checked ?? true,
          stream: false,
        }),
      });

      hideTyping();

      const text = await resp.text();
      let data;
      try {
        data = JSON.parse(text);
      } catch (parseErr) {
        addMessage('system', '⚠ 服务器返回异常: ' + text.slice(0, 200));
        addLog('AI 响应解析失败: ' + parseErr.message);
        return;
      }

      if (!resp.ok || data.status === 'error') {
        addMessage('system', '⚠ ' + (data.reply || data.error || '请求失败'));
        addLog('AI 对话失败: ' + (data.reply || ''));
        return;
      }

      const reply = data.reply || '(未收到有效回复)';
      addMessage('assistant', reply, 'DeepSeek V4 · 记忆注入');
      chatHistory.push({ role: 'assistant', content: reply });
      addLog(`AI 对话: "${message.slice(0, 30)}..." → ${reply.length} 字符`);

    } catch (err) {
      hideTyping();
      addMessage('system', '⚠ 网络异常: ' + err.message);
      addLog('Chat 异常: ' + err.message);
    }
  }

  sendBtn.addEventListener('click', sendMessage);

  input.addEventListener('keydown', (e) => {
    if (e.key === 'Enter' && (e.ctrlKey || e.metaKey)) {
      e.preventDefault();
      sendMessage();
    }
  });

  input.addEventListener('input', () => {
    input.style.height = 'auto';
    input.style.height = Math.min(input.scrollHeight, 80) + 'px';
  });

  addLog('DeepSeek V4 对话已就绪 · 支持代码生成 · 项目记忆自动注入');
}

// ═══════════════════════════════════════════
// 17. 初始化
// ═══════════════════════════════════════════
async function init() {
  // 粒子背景
  initParticles();

  // 滚动效果
  initScrollEffects();

  // 架构视图
  initArchViewSwitch();

  // 拓扑图
  initTopologyCanvas();

  // 模式切换
  initModeTabs();

  // 联动系统
  initCardLinkage();
  initCapabilityLinkage();

  // 运行时间
  setInterval(() => {
    const uptime = Math.floor((Date.now() - state.startTime) / 1000);
    const h = Math.floor(uptime / 3600);
    const m = Math.floor((uptime % 3600) / 60);
    const s = uptime % 60;
    const uptimeEl = $('#hero-uptime');
    if (uptimeEl) {
      uptimeEl.textContent = `${String(h).padStart(2, '0')}:${String(m).padStart(2, '0')}:${String(s).padStart(2, '0')}`;
    }
  }, 1000);

  // 健康检查
  try {
    const health = await api.health();
    if (health && health.status === 'ok') {
      state.connected = true;
      if ($('#status-text')) $('#status-text').textContent = '系统就绪';
      if ($('#status-dot')) $('#status-dot').classList.remove('error');
      addLog('◈ 星云智能体 v0.2.0 — 莱茵生命记忆引擎协议已激活');
      addLog('存储引擎: LSM-Tree + 分片KV + HNSW 向量索引');
      addLog('检索策略: 混合检索 (RRF融合) | 向量维度: 1536');
    }
  } catch (_) {
    state.connected = false;
    if ($('#status-text')) $('#status-text').textContent = '连接中断';
    if ($('#status-dot')) $('#status-dot').classList.add('error');
    addLog('⚠ 引擎不可达 — 请先启动 nebula-server');
  }

  refreshStats();
  setInterval(refreshStats, 8000);
}

document.addEventListener('DOMContentLoaded', init);
document.addEventListener('DOMContentLoaded', initChat);
