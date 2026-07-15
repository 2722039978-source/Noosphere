"""
API Server - CodeLens AI
FastAPI server + unified Chinese web UI
"""

import uvicorn
from fastapi import FastAPI
from fastapi.middleware.cors import CORSMiddleware
from fastapi.responses import HTMLResponse, JSONResponse
from loguru import logger
from ..agent.code_agent import CodeLensAgent
from .routes import setup_routes


# ============================================================
# 统一中文 Web 界面 - 独立项目 + 联调导航
# ============================================================
INDEX_HTML = r"""<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>CodeLens AI · 智能代码理解</title>
<style>
:root {
  --primary: #3b82c4; --primary-hover: #2563a0; --primary-light: #eef4fb;
  --green: #22a67e; --orange: #e8962f; --red: #d94a3a; --purple: #7c3aed;
  --bg: #f5f7fa; --card-bg: #fff; --card-border: #e2e7ef;
  --text: #1a2634; --text-secondary: #5a6b7d; --text-muted: #8899aa;
  --shadow-sm: 0 1px 3px rgba(0,0,0,0.04); --shadow-md: 0 2px 8px rgba(0,0,0,0.06);
  --shadow-lg: 0 4px 16px rgba(0,0,0,0.08);
  --radius: 8px; --radius-lg: 12px; --radius-xl: 16px;
}
body.dark {
  --bg: #141820; --card-bg: #1c2129; --card-border: #2a3040;
  --text: #e2e6ec; --text-secondary: #8899aa; --text-muted: #667788;
  --shadow-sm: 0 1px 3px rgba(0,0,0,0.25); --shadow-md: 0 2px 8px rgba(0,0,0,0.35);
  --shadow-lg: 0 4px 16px rgba(0,0,0,0.45); --primary-light: #1a2d44;
}
*{margin:0;padding:0;box-sizing:border-box}
body{font-family:'Microsoft YaHei','PingFang SC',-apple-system,sans-serif;background:var(--bg);color:var(--text);min-height:100vh;line-height:1.6;-webkit-font-smoothing:antialiased}

/* 导航 */
.nav{position:sticky;top:0;z-index:100;background:rgba(245,247,250,0.8);-webkit-backdrop-filter:blur(16px);backdrop-filter:blur(16px);border-bottom:1px solid var(--card-border);padding:0 20px}
body.dark .nav{background:rgba(20,24,32,0.8)}
.nav-inner{max-width:1140px;margin:0 auto;display:flex;align-items:center;height:50px;gap:14px}
.nav-logo{display:flex;align-items:center;gap:8px;font-weight:700;font-size:15px;color:var(--text);text-decoration:none}
.nav-spacer{flex:1}
.nav-suite{display:flex;gap:2px;background:var(--card-border);border-radius:8px;padding:2px}
.nav-suite a{padding:5px 13px;font-size:11.5px;border-radius:6px;color:var(--text-secondary);text-decoration:none;font-weight:500;transition:.15s;white-space:nowrap}
.nav-suite a:hover{color:var(--text);background:var(--card-bg)}
.nav-suite a.active{background:var(--primary);color:#fff}
.nav-status{font-size:12px;color:var(--text-secondary);display:flex;align-items:center;gap:6px}
.nav-dot{width:7px;height:7px;border-radius:50%;background:var(--green);display:inline-block;animation:pulse 2s infinite}
@keyframes pulse{0%,100%{opacity:1}50%{opacity:0.4}}
.nav-btn{width:32px;height:32px;border-radius:6px;border:1px solid var(--card-border);background:var(--card-bg);cursor:pointer;font-size:14px;display:flex;align-items:center;justify-content:center;transition:.15s;color:var(--text)}
.nav-btn:hover{background:var(--primary-light);border-color:var(--primary)}

/* 主容器 */
.app{max-width:1140px;margin:0 auto;padding:0 20px 60px}

/* Hero */
.hero{text-align:center;padding:40px 0 24px}
.hero-icon{width:72px;height:72px;border-radius:18px;background:linear-gradient(135deg,#3b82c4,#6366f1);display:inline-flex;align-items:center;justify-content:center;margin-bottom:16px;box-shadow:0 8px 32px rgba(59,130,196,0.25)}
.hero h1{font-size:34px;font-weight:700;margin-bottom:6px}
.hero .tagline{font-size:16px;color:var(--text-secondary);max-width:560px;margin:0 auto}
.hero .sub{font-size:13px;color:var(--text-muted);margin-top:6px}

/* 套件条 */
.suite-bar{display:flex;align-items:center;gap:14px;flex-wrap:wrap;background:linear-gradient(135deg,var(--primary-light),var(--card-bg));border:1px solid var(--card-border);border-radius:var(--radius);padding:14px 18px;margin-bottom:20px}
.suite-bar .tag{font-size:10px;font-weight:700;padding:3px 10px;border-radius:10px;background:var(--primary);color:#fff;white-space:nowrap}
.suite-bar .desc{flex:1;min-width:200px;font-size:13px;color:var(--text-secondary);line-height:1.5}
.suite-bar .desc b{color:var(--text)}
.suite-bar .btns{display:flex;gap:8px}
.suite-bar .btns a{padding:6px 14px;border-radius:16px;border:1px solid var(--primary);color:var(--primary);text-decoration:none;font-size:12px;font-weight:500;transition:.15s}
.suite-bar .btns a:hover{background:var(--primary);color:#fff}

/* 统计 */
.stats-grid{display:grid;grid-template-columns:repeat(4,1fr);gap:10px;margin-bottom:18px}
.stat-card{background:var(--card-bg);border-radius:var(--radius);padding:16px;box-shadow:var(--shadow-sm);border:1px solid var(--card-border);text-align:center;transition:.15s}
.stat-card:hover{transform:translateY(-1px);box-shadow:var(--shadow-md)}
.stat-value{font-size:28px;font-weight:700;color:var(--primary)}
.stat-label{font-size:11px;color:var(--text-secondary);margin-top:3px;font-weight:500}
.stat-icon{font-size:20px;display:block;margin-bottom:2px}

/* 双栏 */
.main-grid{display:grid;grid-template-columns:1fr 1fr;gap:12px;margin-bottom:18px}
@media(max-width:820px){.main-grid,.stats-grid{grid-template-columns:1fr}}

/* 卡片 */
.card{background:var(--card-bg);border-radius:var(--radius);box-shadow:var(--shadow-sm);border:1px solid var(--card-border);overflow:hidden}
.card-header{padding:13px 16px;border-bottom:1px solid var(--card-border);display:flex;align-items:center;gap:7px;font-weight:600;font-size:13.5px}
.card-header .dot{width:7px;height:7px;border-radius:50%;flex-shrink:0}
.dot.blue{background:var(--primary)}.dot.green{background:var(--green)}.dot.orange{background:var(--orange)}.dot.purple{background:var(--purple)}
.card-body{padding:14px 16px}

/* 聊天 */
.chat-wrap{display:flex;flex-direction:column;height:420px}
.chat-msgs{flex:1;overflow-y:auto;padding:12px 16px;display:flex;flex-direction:column;gap:10px;scroll-behavior:smooth}
.chat-msgs::-webkit-scrollbar{width:4px}.chat-msgs::-webkit-scrollbar-thumb{background:var(--card-border);border-radius:4px}
.msg{max-width:88%;padding:10px 14px;border-radius:14px;font-size:13px;line-height:1.5;animation:msgIn .25s}
.msg.user{align-self:flex-end;background:var(--primary);color:#fff;border-bottom-right-radius:3px}
.msg.agent{align-self:flex-start;background:var(--primary-light);color:var(--text);border-bottom-left-radius:3px}
body.dark .msg.agent{background:#1e2d3d}
.msg .by{font-size:10px;font-weight:600;margin-bottom:2px;opacity:0.7}
.msg .tag{display:inline-block;font-size:9.5px;margin-top:5px;padding:2px 7px;border-radius:8px;background:rgba(0,0,0,0.06);color:var(--text-secondary);font-family:Consolas,monospace}
.chat-row{display:flex;gap:8px;padding:10px 16px;border-top:1px solid var(--card-border)}
.chat-input{flex:1;border:1px solid var(--card-border);border-radius:18px;padding:8px 14px;font-size:13.5px;font-family:inherit;background:var(--bg);color:var(--text);outline:none;transition:.15s}
.chat-input:focus{border-color:var(--primary);box-shadow:0 0 0 3px rgba(59,130,196,0.1)}
.chat-btn{background:var(--primary);color:#fff;border:none;padding:8px 16px;border-radius:18px;font-size:13.5px;font-weight:600;cursor:pointer;transition:.15s;font-family:inherit}
.chat-btn:hover{background:var(--primary-hover)}
.chat-btn:disabled{opacity:0.4;cursor:not-allowed}

/* 快捷操作 */
.quick-grid{display:grid;grid-template-columns:1fr 1fr;gap:6px}
.quick-btn{display:flex;align-items:center;gap:8px;padding:10px 12px;border-radius:var(--radius);border:1px solid var(--card-border);background:var(--card-bg);cursor:pointer;font-size:12.5px;color:var(--text);transition:.15s;text-align:left;font-family:inherit}
.quick-btn:hover{background:var(--primary-light);border-color:var(--primary);transform:translateY(-1px)}
.quick-btn .ic{font-size:18px;flex-shrink:0}
.quick-btn .ti{font-weight:600}.quick-btn .hi{font-size:10.5px;color:var(--text-muted)}

/* 功能介绍区 */
.features{margin-bottom:18px}
.features-grid{display:grid;grid-template-columns:repeat(3,1fr);gap:10px}
@media(max-width:820px){.features-grid{grid-template-columns:1fr}}
.feat-card{background:var(--card-bg);border-radius:var(--radius);box-shadow:var(--shadow-sm);border:1px solid var(--card-border);padding:18px;transition:.15s}
.feat-card:hover{transform:translateY(-2px);box-shadow:var(--shadow-md)}
.feat-card .f-icon{font-size:28px;margin-bottom:8px}
.feat-card h3{font-size:14px;font-weight:700;margin-bottom:6px}
.feat-card p{font-size:12px;color:var(--text-secondary);line-height:1.6}
.feat-card .f-tech{font-size:10px;color:var(--text-muted);margin-top:8px;padding:3px 8px;background:var(--bg);border-radius:4px;display:inline-block}

/* 使用步骤 */
.steps{margin-bottom:18px}
.step-row{display:flex;gap:12px;counter-reset:step}
.step-item{flex:1;background:var(--card-bg);border-radius:var(--radius);box-shadow:var(--shadow-sm);border:1px solid var(--card-border);padding:20px;position:relative;transition:.15s}
.step-item:hover{transform:translateY(-2px);box-shadow:var(--shadow-md)}
.step-num{width:28px;height:28px;border-radius:50%;background:var(--primary);color:#fff;display:flex;align-items:center;justify-content:center;font-weight:700;font-size:13px;margin-bottom:10px}
.step-item h4{font-size:13px;margin-bottom:4px}
.step-item p{font-size:11.5px;color:var(--text-secondary);line-height:1.5}
.step-item code{font-size:10.5px;background:var(--bg);padding:1px 5px;border-radius:3px;color:var(--primary)}
@media(max-width:820px){.step-row{flex-direction:column}}

/* API */
.api-section{margin-bottom:18px}
.api-toggle{width:100%;background:var(--card-bg);border:1px solid var(--card-border);border-radius:var(--radius);padding:14px 18px;cursor:pointer;display:flex;align-items:center;gap:10px;font-size:14px;font-weight:600;color:var(--text);transition:.15s;font-family:inherit;text-align:left;box-shadow:var(--shadow-sm)}
.api-toggle:hover{box-shadow:var(--shadow-md)}
.api-toggle .chevron{transition:.2s;font-size:11px;color:var(--text-secondary)}
.api-toggle.open .chevron{transform:rotate(90deg)}
.api-body{display:none;margin-top:8px}
.api-body.open{display:block}
.api-grid{display:grid;grid-template-columns:1fr 1fr;gap:4px}
@media(max-width:820px){.api-grid{grid-template-columns:1fr}}
.api-ep{background:var(--card-bg);border-radius:var(--radius);padding:10px 12px;box-shadow:var(--shadow-sm);border:1px solid var(--card-border);display:flex;align-items:center;gap:8px;transition:.15s;font-size:12px}
.api-ep:hover{box-shadow:var(--shadow-md)}
.api-m{font-size:9px;font-weight:700;padding:2px 6px;border-radius:4px;text-transform:uppercase;flex-shrink:0}
.api-m.get{background:rgba(59,130,196,0.1);color:var(--primary)}
.api-m.post{background:rgba(34,166,126,0.1);color:var(--green)}
.api-m.ws{background:rgba(232,150,47,0.1);color:var(--orange)}
.api-p{font-family:Consolas,monospace;font-size:11px;color:var(--text);flex:1;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.api-d{font-size:10.5px;color:var(--text-muted);flex-shrink:0}

/* 页脚 */
.footer{text-align:center;padding:32px 0 20px;color:var(--text-muted);font-size:11.5px;border-top:1px solid var(--card-border);margin-top:30px}
.footer a{color:var(--primary);text-decoration:none}.footer a:hover{text-decoration:underline}

@keyframes msgIn{from{opacity:0;transform:translateY(6px)}to{opacity:1;transform:translateY(0)}}
</style>
</head>
<body>

<!-- ====== 导航栏（统一联调入口） ====== -->
<nav class="nav">
<div class="nav-inner">
<a class="nav-logo" href="/">
<svg width="26" height="26" viewBox="0 0 32 32"><defs><linearGradient id="g" x1="0%" y1="0%" x2="100%" y2="100%"><stop offset="0%" stop-color="#3b82c4"/><stop offset="100%" stop-color="#6366f1"/></linearGradient></defs><rect width="32" height="32" rx="7" fill="url(#g)"/><text x="16" y="22" text-anchor="middle" fill="white" font-family="Microsoft YaHei" font-size="17" font-weight="700">C</text></svg>
CodeLens AI
</a>
<div class="nav-suite">
<a href="http://localhost:8730">☁ Nebula</a>
<a href="http://localhost:8765" class="active">◆ CodeLens</a>
<a href="http://localhost:8740">⎔ DevOps</a>
</div>
<div class="nav-spacer"></div>
<span class="nav-status"><span class="nav-dot"></span><span id="nav-text">就绪</span></span>
<button class="nav-btn" onclick="toggleDark()" title="切换深色/浅色模式">🌓</button>
</div>
</nav>

<div class="app">

<!-- Hero -->
<div class="hero">
<div class="hero-icon"><svg width="36" height="36" viewBox="0 0 32 32"><rect width="32" height="32" rx="7" fill="rgba(255,255,255,0.15)"/><text x="16" y="22" text-anchor="middle" fill="white" font-family="Microsoft YaHei" font-size="17" font-weight="700">C</text></svg></div>
<h1>CodeLens AI</h1>
<div class="tagline">深入理解你的代码库 —— 解析代码结构、构建知识图谱、智能问答与变更分析</div>
<div class="sub">基于 Tree-sitter AST 解析 + NetworkX 知识图谱 + ChromaDB 向量检索 + DeepSeek 大模型</div>
</div>

<!-- 套件关联 -->
<div class="suite-bar">
<span class="tag">Project 套件</span>
<div class="desc"><b>CodeLens AI</b>（代码结构理解） + <b>Nebula Agent</b>（编码风格学习） + <b>DevOps Agent</b>（运维智能管理）<br>三个独立服务，通过统一导航串联工作。CodeLens 负责回答「代码怎么写的」，Nebula 负责回答「风格是什么」，DevOps 负责回答「系统怎么跑」。</div>
<div class="btns"><a href="http://localhost:8730">☁ Nebula →</a><a href="http://localhost:8740">⎔ DevOps →</a></div>
</div>

<!-- 实时统计 -->
<div class="stats-grid">
<div class="stat-card"><span class="stat-icon">📁</span><div class="stat-value" id="stat-files">--</div><div class="stat-label">已索引文件</div></div>
<div class="stat-card"><span class="stat-icon">🔷</span><div class="stat-value" id="stat-entities">--</div><div class="stat-label">代码实体</div></div>
<div class="stat-card"><span class="stat-icon">🔗</span><div class="stat-value" id="stat-relations">--</div><div class="stat-label">关系边</div></div>
<div class="stat-card"><span class="stat-icon">🌐</span><div class="stat-value" id="stat-langs">--</div><div class="stat-label">编程语言</div></div>
</div>

<!-- 双栏 -->
<div class="main-grid">
<div class="card" style="grid-row:span 2">
<div class="card-header"><span class="dot blue"></span>💬 代码智能问答</div>
<div class="chat-wrap">
<div class="chat-msgs" id="chatMsgs">
<div class="msg agent"><div class="by">🤖 CodeLens AI</div><div>你好！我是代码智能助手，已接入 DeepSeek 大模型。我可以帮你：</div><div style="margin-top:6px;display:flex;flex-wrap:wrap;gap:4px"><span class="tag">💡 分析项目架构</span><span class="tag">🔍 追踪调用链</span><span class="tag">📊 影响分析</span><span class="tag">🐛 定位问题</span><span class="tag">📝 生成文档</span></div></div>
</div>
<div class="chat-row"><input class="chat-input" id="chatInput" placeholder="输入问题，例如：认证模块的调用链是怎样的？" onkeydown="if(event.key==='Enter')sendMsg()" autofocus><button class="chat-btn" onclick="sendMsg()">发送</button></div>
</div>
</div>

<div class="card">
<div class="card-header"><span class="dot orange"></span>⚡ 快捷操作</div>
<div class="card-body"><div class="quick-grid">
<button class="quick-btn" onclick="quickAction('callchain')"><span class="ic">🔗</span><div><div class="ti">调用链追踪</div><div class="hi">追踪函数执行路径</div></div></button>
<button class="quick-btn" onclick="quickAction('impact')"><span class="ic">💥</span><div><div class="ti">影响分析</div><div class="hi">评估代码变更影响</div></div></button>
<button class="quick-btn" onclick="quickAction('structure')"><span class="ic">🗂️</span><div><div class="ti">项目结构概览</div><div class="hi">整体架构一目了然</div></div></button>
<button class="quick-btn" onclick="quickAction('issues')"><span class="ic">🔍</span><div><div class="ti">代码问题检测</div><div class="hi">扫描潜在风险</div></div></button>
<button class="quick-btn" onclick="quickAction('docs')"><span class="ic">📝</span><div><div class="ti">生成项目文档</div><div class="hi">自动输出技术文档</div></div></button>
<button class="quick-btn" onclick="quickAction('diff')"><span class="ic">🔄</span><div><div class="ti">Git 变更分析</div><div class="di">审查代码变动风险</div></div></button>
</div></div>
</div>
</div>

<!-- ====== 功能详解（新增核心内容） ====== -->
<div class="features">
<h2 style="font-size:18px;font-weight:700;margin-bottom:12px">🎯 核心能力</h2>
<div class="features-grid">

<div class="feat-card">
<div class="f-icon">🌳</div>
<h3>多语言 AST 解析</h3>
<p>基于 Tree-sitter 解析 Python、JavaScript、TypeScript、Java、C/C++、Go、Rust 七种语言的代码，自动提取函数、类、变量、接口及依赖关系。</p>
<span class="f-tech">Tree-sitter · 7 种语言 · AST 遍历</span>
</div>

<div class="feat-card">
<div class="f-icon">🕸️</div>
<h3>代码知识图谱</h3>
<p>构建调用关系、继承关系、依赖关系、包含关系四维图谱。支持调用链追踪（谁调用了谁）、影响分析（改了这里会影响到哪里）。</p>
<span class="f-tech">NetworkX · 有向图 · 可导出 HTML/JSON</span>
</div>

<div class="feat-card">
<div class="f-icon">🗄️</div>
<h3>LSM-Tree KV 存储</h3>
<p>采用日志结构合并树存储索引数据，支持高写入吞吐量。自动选择 LevelDB → RocksDB → SQLite 内置实现，零配置即可使用。</p>
<span class="f-tech">LSM Tree · MemTable · SSTable · Bloom Filter</span>
</div>

<div class="feat-card">
<div class="f-icon">🔎</div>
<h3>RAG 混合检索</h3>
<p>语义向量搜索 + 知识图谱结构搜索 + 关键词全文搜索，三重融合排序。检索结果自动扩展上下文，精准定位相关代码。</p>
<span class="f-tech">ChromaDB · Sentence Transformers · RRF 融合</span>
</div>

<div class="feat-card">
<div class="f-icon">🤖</div>
<h3>DeepSeek 大模型问答</h3>
<p>已接入 DeepSeek V4 Pro，理解检索到的代码上下文后生成精准回答。支持调用链解释、架构分析、问题定位、最佳实践建议。</p>
<span class="f-tech">DeepSeek V4 Pro · OpenAI 兼容协议</span>
</div>

<div class="feat-card">
<div class="f-icon">🔄</div>
<h3>Git Diff 变更分析</h3>
<p>AST 级差异检测 + 知识图谱影响追踪，自动识别变更实体、评估四级风险（低/中/高/严重），生成变更摘要和审查建议。</p>
<span class="f-tech">GitPython · AST Diff · 风险评级</span>
</div>

</div>
</div>

<!-- ====== 使用步骤 ====== -->
<div class="steps">
<h2 style="font-size:18px;font-weight:700;margin-bottom:12px">🚀 快速上手</h2>
<div class="step-row">
<div class="step-item">
<div class="step-num">1</div>
<h4>安装依赖</h4>
<p>运行 <code>pip install -r requirements.txt</code> 安装所有 Python 依赖。核心依赖包括 tree-sitter、networkx、chromadb、fastapi。</p>
</div>
<div class="step-item">
<div class="step-num">2</div>
<h4>配置 API 密钥</h4>
<p>复制 <code>config/.env.example</code> 为 <code>config/.env</code>，填入你的 DeepSeek API Key。也可以设置环境变量 <code>DEEPSEEK_API_KEY</code>。</p>
</div>
<div class="step-item">
<div class="step-num">3</div>
<h4>索引项目</h4>
<p>双击 <code>run.bat</code> 选择「索引项目」，或运行 <code>python -m src.main index --project /your/project</code> 构建代码知识图谱。</p>
</div>
<div class="step-item">
<div class="step-num">4</div>
<h4>提问与探索</h4>
<p>启动 Web 服务后，在聊天框输入问题。也可以直接调用 API：<code>POST /api/v1/qa</code>、<code>GET /api/v1/call-chain/{name}</code>。</p>
</div>
</div>
</div>

<!-- API 参考 -->
<div class="api-section">
<button class="api-toggle" onclick="toggleAPI()">
<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><rect x="3" y="3" width="7" height="7" rx="1"/><rect x="14" y="3" width="7" height="7" rx="1"/><rect x="3" y="14" width="7" height="7" rx="1"/><rect x="14" y="14" width="7" height="7" rx="1"/></svg>
API 接口参考（14 个 REST 端点 + WebSocket）
<span style="flex:1"></span><span class="chevron">▶</span>
</button>
<div class="api-body" id="apiBody">
<div class="api-grid">
<div class="api-ep"><span class="api-m get">GET</span><span class="api-p">/api/v1/health</span><span class="api-d">健康检查</span></div>
<div class="api-ep"><span class="api-m get">GET</span><span class="api-p">/api/v1/status</span><span class="api-d">系统状态</span></div>
<div class="api-ep"><span class="api-m post">POST</span><span class="api-p">/api/v1/index</span><span class="api-d">构建索引</span></div>
<div class="api-ep"><span class="api-m post">POST</span><span class="api-p">/api/v1/qa</span><span class="api-d">代码问答</span></div>
<div class="api-ep"><span class="api-m post">POST</span><span class="api-p">/api/v1/search</span><span class="api-d">搜索代码库</span></div>
<div class="api-ep"><span class="api-m get">GET</span><span class="api-p">/api/v1/call-chain/{name}</span><span class="api-d">调用链</span></div>
<div class="api-ep"><span class="api-m get">GET</span><span class="api-p">/api/v1/impact/{name}</span><span class="api-d">影响分析</span></div>
<div class="api-ep"><span class="api-m post">POST</span><span class="api-p">/api/v1/analyze/file</span><span class="api-d">分析文件</span></div>
<div class="api-ep"><span class="api-m post">POST</span><span class="api-p">/api/v1/analyze/explain</span><span class="api-d">解释代码</span></div>
<div class="api-ep"><span class="api-m post">POST</span><span class="api-p">/api/v1/git/diff</span><span class="api-d">Git Diff</span></div>
<div class="api-ep"><span class="api-m post">POST</span><span class="api-p">/api/v1/docs/generate</span><span class="api-d">生成文档</span></div>
<div class="api-ep"><span class="api-m get">GET</span><span class="api-p">/api/v1/knowledge-graph/stats</span><span class="api-d">图谱统计</span></div>
<div class="api-ep"><span class="api-m get">GET</span><span class="api-p">/api/v1/issues</span><span class="api-d">问题检测</span></div>
<div class="api-ep"><span class="api-m ws">WS</span><span class="api-p">/api/v1/ws/chat</span><span class="api-d">实时对话</span></div>
</div>
<div style="text-align:center;margin-top:10px;font-size:12px;color:var(--text-muted)">完整文档：<a href="/docs" style="color:var(--primary);font-weight:500">Swagger UI</a> · <a href="/redoc" style="color:var(--primary);font-weight:500">ReDoc</a></div>
</div>
</div>

<div class="footer">
<b>CodeLens AI</b> v1.0 · 代码智能理解平台 · <a href="http://localhost:8730">Nebula Agent</a> · <a href="http://localhost:8740">DevOps Agent</a>
</div>

</div>

<script>
// ===== 初始化 =====
document.addEventListener('DOMContentLoaded',()=>{loadStatus();setInterval(loadStatus,15000)});
const dp=localStorage.getItem('dark');
if(dp==='true'||(!dp&&matchMedia('(prefers-color-scheme:dark)').matches))document.body.classList.add('dark');
function toggleDark(){document.body.classList.toggle('dark');localStorage.setItem('dark',document.body.classList.contains('dark'))}

// ===== 状态 =====
async function loadStatus(){
try{
const r=await fetch('/api/v1/status');const d=await r.json();
document.getElementById('stat-files').textContent=d.total_files||'--';
document.getElementById('stat-entities').textContent=d.total_entities||'--';
document.getElementById('stat-relations').textContent=d.total_relations||'--';
document.getElementById('stat-langs').textContent=(d.languages||[]).length||'--';
document.getElementById('nav-text').textContent=d.indexed?'已索引 · 就绪':'未索引';
try{
const kg=await fetch('/api/v1/knowledge-graph/stats');const kd=await kg.json();
document.getElementById('stat-relations').textContent=kd.total_edges||d.total_relations||'--';
document.getElementById('stat-langs').textContent=Object.keys(kd.type_distribution||{}).length||'--';
}catch(e){}
}catch(e){document.getElementById('nav-text').textContent='离线'}
}

// ===== 聊天 =====
async function sendMsg(){
const inp=document.getElementById('chatInput');const q=inp.value.trim();if(!q)return;
const msgs=document.getElementById('chatMsgs');
addMsg('user',q);inp.value='';
const lid=addMsg('agent','正在思考<span class="ld">.</span><span class="ld">.</span><span class="ld">.</span>');
try{
const r=await fetch('/api/v1/qa',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({question:q})});
const d=await r.json();removeMsg(lid);
let ans=(d.answer||'暂无回答').replace(/`([^`]+)`/g,'<code style="background:rgba(0,0,0,0.06);padding:1px 4px;border-radius:3px;font-family:Consolas,monospace;font-size:12px">$1</code>').replace(/\n/g,'<br>');
let html='<div class="by">🤖 CodeLens AI</div><div>'+ans+'</div>';
if(d.sources&&d.sources.length){html+='<div style="margin-top:5px;display:flex;flex-wrap:wrap;gap:3px">';d.sources.slice(0,4).forEach(s=>{html+='<span class="tag">📎 '+(s.file||'').split('/').pop()+':'+s.line+'</span>'});html+='</div>'}
addMsgHTML('agent',html);
}catch(e){removeMsg(lid);addMsg('agent','连接失败，请确认服务已启动。')}
}
function addMsg(t,text){const id='m-'+Date.now(),div=document.createElement('div');div.id=id;div.className='msg '+t;div.innerHTML='<div class="by">'+(t==='user'?'👤 你':'🤖 CodeLens AI')+'</div><div>'+text+'</div>';document.getElementById('chatMsgs').appendChild(div);div.scrollIntoView({behavior:'smooth'});return id}
function addMsgHTML(t,html){const id='m-'+Date.now(),div=document.createElement('div');div.id=id;div.className='msg '+t;div.innerHTML=html;document.getElementById('chatMsgs').appendChild(div);div.scrollIntoView({behavior:'smooth'})}
function removeMsg(id){const el=document.getElementById(id);if(el)el.remove()}

// ===== 快捷操作 =====
function quickAction(act){
const prompts={callchain:'请追踪以下函数的完整调用链：',impact:'请分析修改以下实体会影响哪些模块：',structure:'请描述这个项目的整体架构和模块划分',issues:'请扫描项目中的潜在代码问题和改进建议',docs:'请为以下模块生成技术文档：',diff:'请分析最近的 Git 变更及其影响范围'};
const inp=document.getElementById('chatInput');inp.value=prompts[act]||'';inp.focus();if(act==='structure'||act==='issues'||act==='diff')sendMsg()
}
function toggleAPI(){document.getElementById('apiBody').classList.toggle('open');document.querySelector('.api-toggle').classList.toggle('open')}

// 加载动画
const s=document.createElement('style');s.textContent='.ld{animation:b 1.4s infinite;display:inline-block}.ld:nth-child(2){animation-delay:0.2s}.ld:nth-child(3){animation-delay:0.4s}@keyframes b{0%,100%{opacity:1}50%{opacity:0.3}}';document.head.appendChild(s)
</script>
</body>
</html>"""


class APIServer:
    def __init__(self, agent: CodeLensAgent):
        self.agent = agent
        self.app = self._create_app()

    def _create_app(self) -> FastAPI:
        app = FastAPI(title="CodeLens AI · 智能代码理解平台", version="1.0.0",
                      docs_url="/docs", redoc_url="/redoc")
        app.add_middleware(CORSMiddleware, allow_origins=["*"], allow_credentials=True,
                           allow_methods=["*"], allow_headers=["*"])
        router = setup_routes(self.agent)
        app.include_router(router)

        @app.get("/", response_class=HTMLResponse)
        async def index():
            return INDEX_HTML

        @app.exception_handler(Exception)
        async def global_exception_handler(request, exc):
            logger.error(f"Unhandled: {exc}")
            return JSONResponse(status_code=500, content={"error": str(exc)})

        return app

    def run(self, host="0.0.0.0", port=8765, reload=False, **kwargs):
        logger.info(f"CodeLens AI: http://{host}:{port}")
        uvicorn.run(self.app, host=host, port=port, reload=reload, log_level="info", **kwargs)


def create_app(agent: CodeLensAgent) -> FastAPI:
    return APIServer(agent).app
