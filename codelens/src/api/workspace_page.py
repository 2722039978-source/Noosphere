"""
Noosphere Workspace Admin Page

统一的工作区管理页面，提供：
- LLM API 配置（多提供商卡片式管理）
- 项目管理（扫描、分析、状态查看）
- 验证诊断（连通性测试、工具链 I/O 检查）
"""

WORKSPACE_HTML = r"""<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>Noosphere Workspace · 工作区管理</title>
<style>
:root {
  --primary: #6366f1; --primary-hover: #4f46e5; --primary-light: #eef2ff; --primary-glow: rgba(99,102,241,0.15);
  --green: #10b981; --green-bg: #ecfdf5; --orange: #f59e0b; --orange-bg: #fffbeb;
  --red: #ef4444; --red-bg: #fef2f2; --purple: #8b5cf6;
  --bg: #0f1117; --card-bg: #1a1d27; --card-hover: #1f222d; --card-border: #2a2d3a;
  --text: #e4e6f0; --text-secondary: #9098a8; --text-muted: #5c6272;
  --input-bg: #141720; --input-border: #2a2d3a; --input-focus: #6366f1;
  --shadow-sm: 0 1px 2px rgba(0,0,0,0.3); --shadow-md: 0 4px 12px rgba(0,0,0,0.4);
  --shadow-lg: 0 8px 24px rgba(0,0,0,0.5);
  --radius: 10px; --radius-sm: 6px; --radius-lg: 14px;
  --transition: 0.2s cubic-bezier(0.4,0,0.2,1);
}
body.light {
  --bg: #f5f7fa; --card-bg: #fff; --card-hover: #fafbfc; --card-border: #e2e7ef;
  --text: #1a2634; --text-secondary: #5a6b7d; --text-muted: #8899aa;
  --input-bg: #f9fafb; --input-border: #d1d5db; --input-focus: #6366f1;
  --shadow-sm: 0 1px 3px rgba(0,0,0,0.04); --shadow-md: 0 2px 8px rgba(0,0,0,0.06);
  --shadow-lg: 0 4px 16px rgba(0,0,0,0.08);
  --primary-light: #eef2ff; --green-bg: #ecfdf5; --orange-bg: #fffbeb; --red-bg: #fef2f2;
}
*{margin:0;padding:0;box-sizing:border-box}
body{font-family:'Inter','Microsoft YaHei','PingFang SC',-apple-system,sans-serif;background:var(--bg);color:var(--text);min-height:100vh;line-height:1.6;-webkit-font-smoothing:antialiased}

/* Nav */
.nav{position:sticky;top:0;z-index:100;background:rgba(15,17,23,0.85);-webkit-backdrop-filter:blur(20px);backdrop-filter:blur(20px);border-bottom:1px solid var(--card-border);padding:0 24px}
body.light .nav{background:rgba(245,247,250,0.85)}
.nav-inner{max-width:1200px;margin:0 auto;display:flex;align-items:center;height:52px;gap:12px}
.nav-logo{font-weight:800;font-size:15px;color:var(--text);text-decoration:none;display:flex;align-items:center;gap:8px}
.nav-logo svg{flex-shrink:0}
.nav-suite{display:flex;gap:1px;background:var(--card-border);border-radius:8px;padding:2px}
.nav-suite a{padding:5px 14px;font-size:11.5px;border-radius:6px;color:var(--text-secondary);text-decoration:none;font-weight:600;transition:var(--transition);white-space:nowrap}
.nav-suite a:hover{color:var(--text);background:var(--card-bg)}
.nav-suite a.active{background:var(--primary);color:#fff}
.nav-spacer{flex:1}
.nav-status{font-size:12px;color:var(--text-secondary);display:flex;align-items:center;gap:6px}
.nav-dot{width:7px;height:7px;border-radius:50%;background:var(--green);display:inline-block;animation:pulse 2s infinite}
@keyframes pulse{0%,100%{opacity:1}50%{opacity:0.3}}
.nav-btn{width:34px;height:34px;border-radius:8px;border:1px solid var(--card-border);background:var(--card-bg);cursor:pointer;font-size:15px;display:flex;align-items:center;justify-content:center;transition:var(--transition);color:var(--text)}
.nav-btn:hover{background:var(--primary-light);border-color:var(--primary)}

/* Layout */
.app{max-width:1200px;margin:0 auto;padding:24px 24px 60px}

/* Hero */
.hero{padding:24px 0 20px}
.hero h1{font-size:28px;font-weight:800;letter-spacing:-0.5px}
.hero p{font-size:14px;color:var(--text-secondary);margin-top:4px;max-width:600px}

/* Tabs */
.tabs{display:flex;gap:2px;background:var(--card-border);border-radius:10px;padding:3px;margin-bottom:20px;width:fit-content}
.tab-btn{padding:8px 18px;border:none;background:transparent;color:var(--text-secondary);font-size:13px;font-weight:600;cursor:pointer;border-radius:7px;transition:var(--transition);font-family:inherit}
.tab-btn:hover{color:var(--text)}
.tab-btn.active{background:var(--card-bg);color:var(--text);box-shadow:var(--shadow-sm)}
.tab-content{display:none}
.tab-content.active{display:block}

/* Cards */
.card{background:var(--card-bg);border-radius:var(--radius-lg);border:1px solid var(--card-border);overflow:hidden;margin-bottom:16px;transition:var(--transition)}
.card:hover{border-color:#6366f144}
.card-header{padding:14px 18px;border-bottom:1px solid var(--card-border);display:flex;align-items:center;gap:8px;font-weight:700;font-size:13.5px}
.card-body{padding:18px}

/* Provider Cards (like agent-auth pattern) */
.provider-grid{display:grid;grid-template-columns:repeat(auto-fill,minmax(320px,1fr));gap:12px}
.provider-card{background:var(--card-bg);border:2px solid var(--card-border);border-radius:var(--radius-lg);padding:20px;transition:var(--transition);position:relative}
.provider-card:hover{border-color:var(--primary);box-shadow:0 0 20px var(--primary-glow)}
.provider-card.connected{border-color:var(--green)}
.provider-card.error{border-color:var(--red)}
.provider-header{display:flex;align-items:center;gap:12px;margin-bottom:14px}
.provider-icon{width:44px;height:44px;border-radius:12px;display:flex;align-items:center;justify-content:center;font-size:20px;font-weight:800;flex-shrink:0}
.provider-icon.ds{background:linear-gradient(135deg,#4f46e5,#7c3aed);color:#fff}
.provider-icon.oai{background:linear-gradient(135deg,#10a37f,#1a7f64);color:#fff}
.provider-icon.anth{background:linear-gradient(135deg,#d97706,#b45309);color:#fff}
.provider-icon.local{background:linear-gradient(135deg,#6366f1,#8b5cf6);color:#fff}
.provider-name{font-weight:700;font-size:15px}
.provider-type{font-size:11px;color:var(--text-muted);text-transform:uppercase;letter-spacing:0.5px}
.provider-status{position:absolute;top:16px;right:16px;font-size:11px;font-weight:600;padding:3px 10px;border-radius:12px}
.provider-status.connected{background:var(--green-bg);color:var(--green)}
.provider-status.disconnected{background:var(--red-bg);color:var(--red)}
.provider-status.testing{background:var(--orange-bg);color:var(--orange)}
.provider-body{display:flex;flex-direction:column;gap:10px}
.form-group{display:flex;flex-direction:column;gap:4px}
.form-group label{font-size:11px;font-weight:600;color:var(--text-secondary);text-transform:uppercase;letter-spacing:0.5px}
.form-group input,.form-group select{background:var(--input-bg);border:1px solid var(--input-border);border-radius:var(--radius-sm);padding:8px 12px;font-size:13px;color:var(--text);outline:none;transition:var(--transition);font-family:inherit}
.form-group input:focus,.form-group select:focus{border-color:var(--input-focus);box-shadow:0 0 0 3px rgba(99,102,241,0.1)}
.form-row{display:grid;grid-template-columns:1fr 1fr;gap:8px}
.provider-actions{display:flex;gap:8px;margin-top:4px}
.btn{padding:8px 16px;border-radius:var(--radius-sm);font-size:12.5px;font-weight:600;cursor:pointer;border:none;transition:var(--transition);font-family:inherit;display:inline-flex;align-items:center;gap:6px}
.btn-primary{background:var(--primary);color:#fff;border:1px solid var(--primary)}
.btn-primary:hover{background:var(--primary-hover);box-shadow:0 4px 12px var(--primary-glow)}
.btn-outline{background:transparent;color:var(--primary);border:1px solid var(--primary)}
.btn-outline:hover{background:var(--primary-light)}
.btn-green{background:var(--green);color:#fff}
.btn-red{background:var(--red);color:#fff;border:1px solid var(--red)}
.btn-sm{padding:5px 12px;font-size:11px}
.btn:disabled{opacity:0.5;cursor:not-allowed}

/* Project list */
.project-list{display:flex;flex-direction:column;gap:8px}
.project-item{display:flex;align-items:center;gap:14px;background:var(--card-bg);border:1px solid var(--card-border);border-radius:var(--radius);padding:14px 16px;transition:var(--transition)}
.project-item:hover{border-color:var(--primary);background:var(--card-hover)}
.project-icon{width:40px;height:40px;border-radius:10px;display:flex;align-items:center;justify-content:center;font-size:18px;flex-shrink:0}
.project-icon.go{background:rgba(0,173,216,0.1);color:#00add8}
.project-icon.py{background:rgba(55,118,171,0.1);color:#3776ab}
.project-icon.js{background:rgba(247,223,30,0.1);color:#f7df1e}
.project-icon.ts{background:rgba(49,120,198,0.1);color:#3178c6}
.project-icon.other{background:rgba(99,102,241,0.1);color:var(--primary)}
.project-info{flex:1;min-width:0}
.project-name{font-weight:700;font-size:14px}
.project-meta{font-size:11px;color:var(--text-muted);display:flex;gap:10px;flex-wrap:wrap;margin-top:2px}
.project-meta span{display:inline-flex;align-items:center;gap:3px}
.project-status{font-size:11px;font-weight:600;padding:4px 10px;border-radius:10px;flex-shrink:0}
.status-done{background:var(--green-bg);color:var(--green)}
.status-pending{background:var(--orange-bg);color:var(--orange)}
.status-error{background:var(--red-bg);color:var(--red)}
.status-scanning{background:rgba(99,102,241,0.1);color:var(--primary)}
.project-actions{display:flex;gap:6px;flex-shrink:0}

/* Validation results */
.validation-summary{display:grid;grid-template-columns:repeat(3,1fr);gap:12px;margin-bottom:16px}
.val-stat{background:var(--card-bg);border-radius:var(--radius);border:1px solid var(--card-border);padding:20px;text-align:center}
.val-stat .val-num{font-size:32px;font-weight:800}
.val-stat .val-label{font-size:11px;color:var(--text-secondary);margin-top:2px;text-transform:uppercase;letter-spacing:0.5px}
.val-stat.pass .val-num{color:var(--green)}
.val-stat.fail .val-num{color:var(--red)}
.val-stat.total .val-num{color:var(--primary)}
.result-list{display:flex;flex-direction:column;gap:6px}
.result-item{display:flex;align-items:center;gap:10px;background:var(--card-bg);border:1px solid var(--card-border);border-radius:var(--radius);padding:12px 14px;font-size:13px}
.result-item .r-icon{font-size:16px;flex-shrink:0}
.result-item .r-name{flex:1;font-weight:500}
.result-item .r-latency{font-size:11px;color:var(--text-muted);font-family:'JetBrains Mono',Consolas,monospace}
.result-item .r-status{font-size:11px;font-weight:600;padding:2px 8px;border-radius:8px}
.r-pass{background:var(--green-bg);color:var(--green)}
.r-fail{background:var(--red-bg);color:var(--red)}
.result-error{font-size:11px;color:var(--red);margin-top:2px}
.result-suggest{margin-top:4px}
.result-suggest li{font-size:11px;color:var(--text-secondary);margin-left:16px}

/* Toast */
.toast{position:fixed;top:20px;right:20px;z-index:1000;padding:12px 20px;border-radius:var(--radius);font-size:13px;font-weight:600;box-shadow:var(--shadow-lg);animation:slideIn 0.3s ease;max-width:380px}
.toast.success{background:var(--green);color:#fff}
.toast.error{background:var(--red);color:#fff}
.toast.info{background:var(--primary);color:#fff}
@keyframes slideIn{from{transform:translateX(120%);opacity:0}to{transform:translateX(0);opacity:1}}

/* Empty state */
.empty-state{text-align:center;padding:40px 20px;color:var(--text-muted)}
.empty-state .empty-icon{font-size:48px;margin-bottom:12px}
.empty-state h3{font-size:16px;color:var(--text-secondary);margin-bottom:4px}
.empty-state p{font-size:12px;max-width:400px;margin:0 auto 16px}

/* Spinner */
.spinner{width:16px;height:16px;border:2px solid var(--card-border);border-top-color:var(--primary);border-radius:50%;animation:spin 0.6s linear infinite;display:inline-block}
@keyframes spin{to{transform:rotate(360deg)}}

@media(max-width:768px){
  .provider-grid{grid-template-columns:1fr}
  .validation-summary{grid-template-columns:1fr}
  .form-row{grid-template-columns:1fr}
}
</style>
</head>
<body>

<!-- Nav -->
<nav class="nav">
<div class="nav-inner">
<a class="nav-logo" href="/">
<svg width="28" height="28" viewBox="0 0 32 32"><defs><linearGradient id="g" x1="0%" y1="0%" x2="100%" y2="100%"><stop offset="0%" stop-color="#6366f1"/><stop offset="100%" stop-color="#8b5cf6"/></linearGradient></defs><rect width="32" height="32" rx="7" fill="url(#g)"/><text x="16" y="22" text-anchor="middle" fill="white" font-size="16" font-weight="800">N</text></svg>
Noosphere Workspace
</a>
<div class="nav-suite">
<a href="http://localhost:8730">☁ Nebula</a>
<a href="http://localhost:8765">◆ CodeLens</a>
<a href="http://localhost:8740">⎔ DevOps</a>
<a href="/workspace" class="active">🗂 Workspace</a>
</div>
<div class="nav-spacer"></div>
<span class="nav-status"><span class="nav-dot" id="statusDot"></span><span id="navText">就绪</span></span>
<button class="nav-btn" onclick="toggleTheme()" title="切换主题">🌓</button>
</div>
</nav>

<div class="app">
<div class="hero">
<h1>🗂 Workspace 工作区</h1>
<p>配置你的 LLM API、拖入项目文件夹、一键分析验证 —— 零代码操作，全在浏览器完成。</p>
</div>

<!-- Tabs -->
<div class="tabs">
<button class="tab-btn active" onclick="switchTab('llm')">🔑 LLM 配置</button>
<button class="tab-btn" onclick="switchTab('projects')">📁 项目管理</button>
<button class="tab-btn" onclick="switchTab('validate')">✅ 验证诊断</button>
</div>

<!-- ===== LLM 配置 Tab ===== -->
<div class="tab-content active" id="tab-llm">
<div class="card">
<div class="card-header">🔑 大模型 API 配置</div>
<div class="card-body">
<p style="font-size:13px;color:var(--text-secondary);margin-bottom:14px">支持多个 LLM 提供商同时配置。填入 API Key 后点击「测试连接」验证连通性，通过的提供商会自动在分析任务中可用。</p>
<div class="provider-grid" id="providerGrid">
<!-- Dynamically filled -->
</div>
<div style="margin-top:16px;display:flex;gap:10px">
<button class="btn btn-primary" onclick="saveAllConfig()">💾 保存全部配置</button>
<button class="btn btn-outline" onclick="loadConfig()">🔄 重新加载</button>
</div>
</div>
</div>
</div>

<!-- ===== 项目管理 Tab ===== -->
<div class="tab-content" id="tab-projects">
<div class="card">
<div class="card-header">📁 项目工作区 <code style="font-size:11px;color:var(--text-muted);font-weight:400;margin-left:6px">workspace/projects/</code></div>
<div class="card-body">
<div style="display:flex;gap:8px;margin-bottom:14px;flex-wrap:wrap">
<button class="btn btn-primary" onclick="scanProjects()"><span class="spinner" id="scanSpinner" style="display:none"></span>🔍 扫描项目</button>
<button class="btn btn-outline btn-sm" onclick="loadProjects()">🔄 刷新列表</button>
<span style="font-size:12px;color:var(--text-muted);margin-left:8px;display:flex;align-items:center">将项目文件夹拖入 <code style="background:var(--input-bg);padding:2px 6px;border-radius:4px;margin:0 3px">workspace/projects/</code> 后点击扫描</span>
</div>
<div class="project-list" id="projectList">
<div class="empty-state">
<div class="empty-icon">📂</div>
<h3>还没有项目</h3>
<p>将你的项目文件夹复制到 workspace/projects/ 目录，然后点击「扫描项目」</p>
</div>
</div>
</div>
</div>
</div>

<!-- ===== 验证诊断 Tab ===== -->
<div class="tab-content" id="tab-validate">
<div class="card">
<div class="card-header">✅ 系统验证与诊断</div>
<div class="card-body">
<div style="display:flex;gap:8px;margin-bottom:14px;flex-wrap:wrap">
<button class="btn btn-primary" onclick="runValidation('llm')">🔗 测试 LLM 连通性</button>
<button class="btn btn-primary" onclick="runValidation('tools')">🔧 测试工具链 I/O</button>
<button class="btn btn-green" onclick="runValidation('all')">🚀 一键全量诊断</button>
</div>
<div class="validation-summary" id="valSummary">
<div class="val-stat total"><div class="val-num" id="valTotal">--</div><div class="val-label">总测试数</div></div>
<div class="val-stat pass"><div class="val-num" id="valPass">--</div><div class="val-label">通过</div></div>
<div class="val-stat fail"><div class="val-num" id="valFail">--</div><div class="val-label">失败</div></div>
</div>
<div class="result-list" id="resultList">
<div class="empty-state">
<div class="empty-icon">🔬</div>
<h3>等待测试</h3>
<p>点击上方的测试按钮，验证 LLM 连接和工具链是否正常工作</p>
</div>
</div>
</div>
</div>
</div>

</div>

<!-- Toast container -->
<div id="toastContainer"></div>

<script>
// ===== State =====
const DEFAULTS = [
  {name:'deepseek',type:'deepseek',icon:'ds',label:'DeepSeek',model:'deepseek-v4-pro',base_url:'https://api.deepseek.com',api_key:'',enabled:true},
  {name:'openai',type:'openai',icon:'oai',label:'OpenAI',model:'gpt-4o',base_url:'https://api.openai.com/v1',api_key:'',enabled:false},
  {name:'anthropic',type:'anthropic',icon:'anth',label:'Anthropic',model:'claude-sonnet-5',base_url:'https://api.anthropic.com',api_key:'',enabled:false},
  {name:'local',type:'openai_compat',icon:'local',label:'Ollama / 本地',model:'qwen3:latest',base_url:'http://localhost:11434/v1',api_key:'ollama',enabled:false}
];
let providers = JSON.parse(JSON.stringify(DEFAULTS));
let providerStatus = {};

// ===== Init =====
document.addEventListener('DOMContentLoaded',()=>{
  if(localStorage.getItem('ws-light')==='true') document.body.classList.add('light');
  loadConfig();
  loadProjectStatus();
});

function toggleTheme(){document.body.classList.toggle('light');localStorage.setItem('ws-light',document.body.classList.contains('light'))}

// ===== Toast =====
function toast(msg,type='info'){
  const t=document.createElement('div');t.className='toast '+type;t.textContent=msg;
  document.getElementById('toastContainer').appendChild(t);
  setTimeout(()=>{t.style.opacity='0';t.style.transition='opacity 0.3s';setTimeout(()=>t.remove(),300)},3000);
}

// ===== Tabs =====
function switchTab(name){
  document.querySelectorAll('.tab-btn').forEach(b=>b.classList.remove('active'));
  document.querySelectorAll('.tab-content').forEach(c=>c.classList.remove('active'));
  event.target.classList.add('active');
  document.getElementById('tab-'+name).classList.add('active');
  if(name==='projects') loadProjects();
}

// ===== LLM Config =====
async function loadConfig(){
  try{
    const r=await fetch('/api/v1/workspace/config/llm');const d=await r.json();
    if(d.providers&&d.providers.length){
      d.providers.forEach((p,i)=>{
        if(providers[i]){
          providers[i].api_key='';
          providers[i].enabled=p.enabled;
          providers[i].api_key_configured=p.api_key_configured;
          providers[i].api_key_masked=p.api_key_masked;
          if(p.model) providers[i].model=p.model;
          if(p.base_url) providers[i].base_url=p.base_url;
        }
      });
    }
  }catch(e){}
  renderProviders();
}

function renderProviders(){
  const grid=document.getElementById('providerGrid');
  grid.innerHTML=providers.map((p,i)=>{
    const connected=providerStatus[p.name]==='ok';
    const testing=providerStatus[p.name]==='testing';
    const error=providerStatus[p.name]==='error';
    let statusHTML='';
    if(connected) statusHTML='<span class="provider-status connected">✓ 已连接</span>';
    else if(testing) statusHTML='<span class="provider-status testing"><span class="spinner"></span> 测试中</span>';
    else if(error) statusHTML='<span class="provider-status disconnected">✗ 连接失败</span>';
    else if(p.api_key_configured) statusHTML='<span class="provider-status connected">已配置</span>';

    return `<div class="provider-card ${connected?'connected':(error?'error':'')}">
      ${statusHTML}
      <div class="provider-header">
        <div class="provider-icon ${p.icon}">${p.label[0]}</div>
        <div>
          <div class="provider-name">${p.label}</div>
          <div class="provider-type">${p.type}</div>
        </div>
      </div>
      <div class="provider-body">
        <div class="form-group">
          <label>API Key</label>
          <input type="password" id="key-${i}" placeholder="${p.api_key_masked||'sk-...'}" value="${p.api_key||''}" onfocus="this.type='text'" onblur="if(!this.value)this.type='password'">
        </div>
        <div class="form-row">
          <div class="form-group">
            <label>Base URL</label>
            <input type="text" id="url-${i}" value="${p.base_url}" placeholder="https://api.deepseek.com">
          </div>
          <div class="form-group">
            <label>Model</label>
            <input type="text" id="model-${i}" value="${p.model}" placeholder="deepseek-v4-pro">
          </div>
        </div>
        <div class="provider-actions">
          <button class="btn btn-outline btn-sm" onclick="testProvider(${i})" ${testing?'disabled':''}>${testing?'<span class="spinner"></span>':''}🔗 测试连接</button>
          <label style="display:flex;align-items:center;gap:6px;font-size:12px;color:var(--text-secondary);cursor:pointer;margin-left:8px">
            <input type="checkbox" id="enabled-${i}" ${p.enabled?'checked':''} onchange="providers[${i}].enabled=this.checked"> 启用
          </label>
        </div>
        <div id="test-result-${i}" style="font-size:11px;margin-top:4px"></div>
      </div>
    </div>`;
  }).join('');
}

async function testProvider(idx){
  const p=providers[idx];
  const keyEl=document.getElementById('key-'+idx);
  const apiKey=keyEl.value.trim()||p.api_key||'';
  const baseUrl=document.getElementById('url-'+idx).value.trim();
  const model=document.getElementById('model-'+idx).value.trim();

  if(!apiKey||apiKey.includes('your-api-key')){
    toast('请先填入 '+p.label+' 的 API Key','error');
    return;
  }

  providerStatus[p.name]='testing';
  renderProviders();

  try{
    const r=await fetch('/api/v1/workspace/validate/llm',{
      method:'POST',headers:{'Content-Type':'application/json'},
      body:JSON.stringify({provider:p.name})
    });
    const d=await r.json();
    const resultEl=document.getElementById('test-result-'+idx);

    if(d.result&&d.result.passed){
      providerStatus[p.name]='ok';
      resultEl.innerHTML='<span style="color:var(--green)">✓ 连接成功 · 延迟 '+Math.round(d.result.latency_ms)+'ms · 模型 '+model+'</span>';
      toast(p.label+' 连接成功！延迟 '+Math.round(d.result.latency_ms)+'ms','success');
    }else if(d.single_test&&d.result&&d.result.error){
      providerStatus[p.name]='error';
      resultEl.innerHTML='<span style="color:var(--red)">✗ '+d.result.error+'</span>';
      if(d.result.suggestions&&d.result.suggestions.length){
        resultEl.innerHTML+='<ul class="result-suggest">'+d.result.suggestions.map(s=>'<li>'+s+'</li>').join('')+'</ul>';
      }
      toast(p.label+' 连接失败','error');
    }else{
      // multi-provider result
      const found=d.results?d.results.find(r=>r.test_name.includes(p.name)):null;
      if(found&&found.passed){
        providerStatus[p.name]='ok';
        resultEl.innerHTML='<span style="color:var(--green)">✓ 连接成功</span>';
      }else{
        providerStatus[p.name]='error';
        resultEl.innerHTML='<span style="color:var(--red)">✗ '+(found?found.error:'未知错误')+'</span>';
      }
    }
  }catch(e){
    providerStatus[p.name]='error';
    document.getElementById('test-result-'+idx).innerHTML='<span style="color:var(--red)">✗ 请求失败: '+e.message+'</span>';
    toast(p.label+' 请求失败','error');
  }
  renderProviders();
}

async function saveAllConfig(){
  // Build YAML content
  let yaml='# Noosphere Workspace - LLM Configuration\n';
  yaml+='# Auto-generated from Web UI\n\n';
  yaml+='default_provider: '+(providers.find(p=>p.enabled)||providers[0]).name+'\n\n';
  yaml+='providers:\n';
  providers.forEach(p=>{
    const idx=providers.indexOf(p);
    const keyEl=document.getElementById('key-'+idx);
    const urlEl=document.getElementById('url-'+idx);
    const modelEl=document.getElementById('model-'+idx);
    const apiKey=keyEl&&keyEl.value.trim()||p.api_key||'';
    const baseUrl=urlEl&&urlEl.value.trim()||p.base_url;
    const model=modelEl&&modelEl.value.trim()||p.model;
    yaml+='  - name: '+p.name+'\n';
    yaml+='    type: '+p.type+'\n';
    yaml+='    api_key: "'+apiKey+'"\n';
    yaml+='    base_url: '+baseUrl+'\n';
    yaml+='    model: '+model+'\n';
    yaml+='    enabled: '+(p.enabled!==false)+'\n';
  });

  // Save via API - we'll POST to a save endpoint
  try{
    const r=await fetch('/api/v1/workspace/config/llm',{
      method:'POST',headers:{'Content-Type':'application/json'},
      body:JSON.stringify({yaml_content:yaml})
    });
    if(r.ok){
      toast('配置已保存到 workspace/llm_config.yaml','success');
    }else{
      // Fallback: show manual instructions
      toast('请手动将以下内容保存到 workspace/llm_config.yaml','info');
      console.log(yaml);
    }
  }catch(e){
    toast('配置已生成，请查看浏览器控制台','info');
    console.log('=== Save this to workspace/llm_config.yaml ===');
    console.log(yaml);
  }
}

// ===== Project Management =====
async function loadProjects(){
  try{
    const r=await fetch('/api/v1/workspace/projects');const d=await r.json();
    renderProjects(d.projects||[]);
  }catch(e){
    document.getElementById('projectList').innerHTML='<div class="empty-state"><div class="empty-icon">⚠</div><h3>无法加载项目</h3><p>请确认 CodeLens 服务已启动</p></div>';
  }
}

function renderProjects(projects){
  const list=document.getElementById('projectList');
  if(!projects.length){
    list.innerHTML='<div class="empty-state"><div class="empty-icon">📂</div><h3>还没有项目</h3><p>将你的项目文件夹复制到 workspace/projects/ 目录，然后点击「扫描项目」</p><button class="btn btn-primary" onclick="scanProjects()" style="margin-top:8px">🔍 扫描项目</button></div>';
    return;
  }
  list.innerHTML=projects.map(p=>{
    let langIcon='other';
    const langs=(p.languages||[]).map(l=>l.toLowerCase()).join(' ');
    if(langs.includes('go')) langIcon='go';
    else if(langs.includes('python')) langIcon='py';
    else if(langs.includes('typescript')) langIcon='ts';
    else if(langs.includes('javascript')) langIcon='js';

    const statusClass='status-'+(p.scan_status||'pending');
    const statusText={pending:'待分析',scanning:'分析中',done:'已分析',error:'出错'}[p.scan_status]||'待分析';

    let techTags='';
    if(p.languages&&p.languages.length) techTags+=p.languages.slice(0,3).map(l=>'<span>💻 '+l+'</span>').join('');
    if(p.frameworks&&p.frameworks.length) techTags+='<span>📦 '+p.frameworks.slice(0,2).join(', ')+'</span>';
    techTags+='<span>📄 '+p.total_files+' 文件</span>';

    return `<div class="project-item">
      <div class="project-icon ${langIcon}">${p.languages&&p.languages[0]?p.languages[0][0].toUpperCase():'?'}</div>
      <div class="project-info">
        <div class="project-name">${p.name}</div>
        <div class="project-meta">${techTags}</div>
      </div>
      <span class="project-status ${statusClass}">${statusText}</span>
      <div class="project-actions">
        <button class="btn btn-primary btn-sm" onclick="analyzeProject('${p.name}')">🔍 分析</button>
      </div>
    </div>`;
  }).join('');
}

async function scanProjects(){
  document.getElementById('scanSpinner').style.display='inline-block';
  try{
    const r=await fetch('/api/v1/workspace/scan',{method:'POST'});const d=await r.json();
    toast('扫描完成：发现 '+d.count+' 个项目','success');
    renderProjects(d.projects||[]);
  }catch(e){toast('扫描失败：'+e.message,'error')}
  document.getElementById('scanSpinner').style.display='none';
}

async function analyzeProject(name){
  toast('开始分析项目: '+name+'...','info');
  try{
    const r=await fetch('/api/v1/workspace/projects/'+encodeURIComponent(name)+'/analyze',{method:'POST'});
    const d=await r.json();
    if(r.ok){
      toast('项目 "'+name+'" 分析完成！','success');
      loadProjects();
    }else{
      toast('分析失败: '+(d.detail||'未知错误'),'error');
    }
  }catch(e){toast('分析失败: '+e.message,'error')}
}

async function loadProjectStatus(){
  try{
    const r=await fetch('/api/v1/status');const d=await r.json();
    document.getElementById('navText').textContent=d.indexed?'已索引 · 就绪':'工作区已就绪';
  }catch(e){document.getElementById('navText').textContent='离线'}
}

// ===== Validation =====
async function runValidation(type){
  const endpoints={llm:'/api/v1/workspace/validate/llm',tools:'/api/v1/workspace/validate/tools',all:'/api/v1/workspace/validate/all'};
  const url=endpoints[type];
  if(!url) return;

  const resultList=document.getElementById('resultList');
  resultList.innerHTML='<div class="empty-state"><div class="spinner" style="width:24px;height:24px"></div><h3>正在测试...</h3></div>';

  try{
    const r=await fetch(url,{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({})});
    const d=await r.json();

    // Update summary
    document.getElementById('valTotal').textContent=d.total_tests||0;
    document.getElementById('valPass').textContent=d.passed_tests||0;
    document.getElementById('valFail').textContent=d.failed_tests||0;

    // Render results
    let results=[];
    if(d.results) results=d.results;
    else if(d.llm_report&&d.llm_report.results) results=[...d.llm_report.results];
    if(d.tool_report&&d.tool_report.results) results=[...results,...d.tool_report.results];
    if(d.e2e_report&&d.e2e_report.results) results=[...results,...d.e2e_report.results];

    if(!results.length){
      resultList.innerHTML='<div class="empty-state"><div class="empty-icon">✓</div><h3>测试完成</h3><p>'+d.summary+'</p></div>';
      return;
    }

    resultList.innerHTML=results.map(r=>{
      const icon=r.passed?'✅':'❌';
      const cls=r.passed?'r-pass':'r-fail';
      let html='<div class="result-item">';
      html+='<span class="r-icon">'+icon+'</span>';
      html+='<span class="r-name">'+r.test_name+'</span>';
      if(r.latency_ms) html+='<span class="r-latency">'+Math.round(r.latency_ms)+'ms</span>';
      html+='<span class="r-status '+cls+'">'+(r.passed?'通过':'失败')+'</span>';
      html+='</div>';
      if(!r.passed&&r.error) html+='<div class="result-error" style="margin-left:36px">⚠ '+r.error+'</div>';
      if(r.suggestions&&r.suggestions.length) html+='<ul class="result-suggest" style="margin-left:36px">'+r.suggestions.map(s=>'<li>💡 '+s+'</li>').join('')+'</ul>';
      return html;
    }).join('');

    if(d.overall_status==='ok') toast('全部验证通过！','success');
    else if(d.overall_status==='partial') toast(d.summary,'info');
    else toast(d.summary,'error');
  }catch(e){
    resultList.innerHTML='<div class="empty-state"><div class="empty-icon">❌</div><h3>测试失败</h3><p>'+e.message+'</p></div>';
    toast('验证请求失败','error');
  }
}
</script>
</body>
</html>"""


# Also expose a simple config save endpoint
def setup_workspace_routes(app):
    """Add workspace config save endpoint to the FastAPI app."""
    import os
    from pathlib import Path
    from fastapi import Request
    from fastapi.responses import JSONResponse

    @app.post("/api/v1/workspace/config/llm")
    async def save_llm_config(request: Request):
        """Save LLM config to workspace/llm_config.yaml"""
        try:
            body = await request.json()
            yaml_content = body.get("yaml_content", "")

            if not yaml_content:
                return JSONResponse(status_code=400, content={"error": "Empty YAML content"})

            # Determine workspace path
            codelens_root = Path(__file__).parent.parent.parent
            workspace_root = os.environ.get(
                "WORKSPACE_DIR",
                str(codelens_root.parent / "workspace"),
            )
            config_path = Path(workspace_root) / "llm_config.yaml"

            # Write config
            config_path.write_text(yaml_content, encoding="utf-8")

            return {"status": "ok", "saved_to": str(config_path)}
        except Exception as e:
            return JSONResponse(status_code=500, content={"error": str(e)})
