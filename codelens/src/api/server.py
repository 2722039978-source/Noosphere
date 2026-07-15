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
# 统一中文 Web 界面 — 概览 + 工作区（LLM配置·项目管理·验证诊断）全整合
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

/* Tab 导航 */
.tab-nav{display:flex;gap:2px;background:var(--card-border);border-radius:10px;padding:3px;margin-bottom:20px;width:fit-content}
.tab-nav button{padding:8px 20px;border:none;background:transparent;color:var(--text-secondary);font-size:13px;font-weight:600;cursor:pointer;border-radius:7px;transition:.15s;font-family:inherit}
.tab-nav button:hover{color:var(--text)}
.tab-nav button.active{background:var(--card-bg);color:var(--text);box-shadow:var(--shadow-sm)}
.tab-panel{display:none}
.tab-panel.active{display:block}

/* sub-tabs */
.subtabs{display:flex;gap:2px;background:var(--card-border);border-radius:8px;padding:2px;margin-bottom:14px;width:fit-content}
.subtabs button{padding:6px 14px;border:none;background:transparent;color:var(--text-secondary);font-size:11.5px;font-weight:600;cursor:pointer;border-radius:6px;transition:.15s;font-family:inherit}
.subtabs button:hover{color:var(--text)}
.subtabs button.active{background:var(--card-bg);color:var(--text);box-shadow:var(--shadow-sm)}

/* Provider Cards */
.provider-grid{display:grid;grid-template-columns:repeat(auto-fill,minmax(300px,1fr));gap:12px}
.provider-card{background:var(--card-bg);border:2px solid var(--card-border);border-radius:var(--radius);padding:18px;transition:.15s;position:relative}
.provider-card:hover{border-color:var(--primary);box-shadow:0 0 20px rgba(99,102,241,0.1)}
.provider-card.connected{border-color:var(--green)}
.provider-card.error{border-color:var(--red)}
.provider-header{display:flex;align-items:center;gap:10px;margin-bottom:12px}
.provider-icon{width:40px;height:40px;border-radius:10px;display:flex;align-items:center;justify-content:center;font-size:18px;font-weight:800;flex-shrink:0}
.provider-icon.ds{background:linear-gradient(135deg,#4f46e5,#7c3aed);color:#fff}
.provider-icon.oai{background:linear-gradient(135deg,#10a37f,#1a7f64);color:#fff}
.provider-icon.anth{background:linear-gradient(135deg,#d97706,#b45309);color:#fff}
.provider-icon.local{background:linear-gradient(135deg,#6366f1,#8b5cf6);color:#fff}
.provider-name{font-weight:700;font-size:14px}
.provider-type{font-size:10px;color:var(--text-muted);text-transform:uppercase;letter-spacing:0.5px}
.provider-status{position:absolute;top:14px;right:14px;font-size:10px;font-weight:600;padding:3px 8px;border-radius:10px}
.provider-status.connected{background:rgba(34,166,126,0.1);color:var(--green)}
.provider-status.disconnected{background:rgba(217,74,58,0.1);color:var(--red)}
.provider-body{display:flex;flex-direction:column;gap:8px}
.form-group{display:flex;flex-direction:column;gap:3px}
.form-group label{font-size:10px;font-weight:600;color:var(--text-secondary);text-transform:uppercase;letter-spacing:0.5px}
.form-group input,.form-group select{background:var(--bg);border:1px solid var(--card-border);border-radius:6px;padding:7px 10px;font-size:12.5px;color:var(--text);outline:none;transition:.15s;font-family:inherit}
.form-group input:focus,.form-group select:focus{border-color:var(--primary);box-shadow:0 0 0 3px rgba(59,130,196,0.1)}
.form-row{display:grid;grid-template-columns:1fr 1fr;gap:6px}
.provider-actions{display:flex;gap:6px;margin-top:2px}

/* Project items */
.project-list{display:flex;flex-direction:column;gap:6px}
.project-item{display:flex;align-items:center;gap:12px;background:var(--card-bg);border:1px solid var(--card-border);border-radius:var(--radius);padding:12px 14px;transition:.15s}
.project-item:hover{border-color:var(--primary)}
.project-icon-sm{width:36px;height:36px;border-radius:8px;display:flex;align-items:center;justify-content:center;font-size:16px;flex-shrink:0}
.project-icon-sm.go{background:rgba(0,173,216,0.1);color:#00add8}
.project-icon-sm.py{background:rgba(55,118,171,0.1);color:#3776ab}
.project-icon-sm.js{background:rgba(247,223,30,0.1);color:#f7df1e}
.project-icon-sm.other{background:rgba(99,102,241,0.1);color:var(--primary)}
.project-info-sm{flex:1;min-width:0}
.project-name-sm{font-weight:700;font-size:13px}
.project-meta-sm{font-size:10.5px;color:var(--text-muted);display:flex;gap:8px;flex-wrap:wrap;margin-top:1px}
.project-status{font-size:10px;font-weight:600;padding:3px 8px;border-radius:8px;flex-shrink:0}
.status-done{background:rgba(34,166,126,0.1);color:var(--green)}
.status-pending{background:rgba(232,150,47,0.1);color:var(--orange)}
.status-error{background:rgba(217,74,58,0.1);color:var(--red)}

/* Validation */
.val-summary{display:grid;grid-template-columns:repeat(3,1fr);gap:10px;margin-bottom:14px}
.val-stat{background:var(--card-bg);border-radius:var(--radius);border:1px solid var(--card-border);padding:16px;text-align:center}
.val-stat .val-num{font-size:28px;font-weight:800}
.val-stat.pass .val-num{color:var(--green)}
.val-stat.fail .val-num{color:var(--red)}
.val-stat.total .val-num{color:var(--primary)}
.val-stat .val-label{font-size:10px;color:var(--text-secondary);margin-top:2px;text-transform:uppercase;letter-spacing:0.5px}
.result-list{display:flex;flex-direction:column;gap:5px;max-height:400px;overflow-y:auto}
.result-item{display:flex;align-items:center;gap:8px;background:var(--card-bg);border:1px solid var(--card-border);border-radius:var(--radius);padding:10px 12px;font-size:12px}
.result-item .r-name{flex:1;font-weight:500}
.result-item .r-latency{font-size:10px;color:var(--text-muted);font-family:Consolas,monospace}
.result-item .r-status{font-size:10px;font-weight:600;padding:2px 6px;border-radius:6px}
.r-pass{background:rgba(34,166,126,0.1);color:var(--green)}
.r-fail{background:rgba(217,74,58,0.1);color:var(--red)}

/* Toast */
.toast{position:fixed;top:16px;right:16px;z-index:1000;padding:10px 18px;border-radius:var(--radius);font-size:13px;font-weight:600;box-shadow:var(--shadow-lg);animation:slideIn .3s;max-width:360px}
.toast.success{background:var(--green);color:#fff}
.toast.error{background:var(--red);color:#fff}
.toast.info{background:var(--primary);color:#fff}
@keyframes slideIn{from{transform:translateX(120%);opacity:0}to{transform:translateX(0);opacity:1}}

/* spinner override for tabs */
.spinner-sm{width:14px;height:14px;border:2px solid var(--card-border);border-top-color:var(--primary);border-radius:50%;animation:spin .6s linear infinite;display:inline-block}
@keyframes spin{to{transform:rotate(360deg)}}
.btn-outline-sm{background:transparent;color:var(--primary);border:1px solid var(--primary);padding:4px 10px;border-radius:6px;font-size:11px;font-weight:600;cursor:pointer;transition:.15s;font-family:inherit}
.btn-outline-sm:hover{background:var(--primary-light)}

/* 双栏 */
.main-grid{display:grid;grid-template-columns:1fr 1fr;gap:12px;margin-bottom:18px}
@media(max-width:820px){.main-grid,.stats-grid,.val-summary{grid-template-columns:1fr}}

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
<a href="/workspace">🗂 Workspace</a>
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

<!-- ====== Tab 导航 ====== -->
<div class="tab-nav">
<button class="active" onclick="switchMainTab('overview')">🏠 总览</button>
<button onclick="switchMainTab('workspace')">🗂 工作区 · LLM + 项目 + 诊断</button>
</div>

<!-- ====== Tab: 总览 ====== -->
<div class="tab-panel active" id="panel-overview">

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
</div><!-- end panel-overview -->

<!-- ====== Tab: 工作区 ====== -->
<div class="tab-panel" id="panel-workspace">

<!-- 子 Tab: LLM配置 / 项目管理 / 验证诊断 -->
<div class="subtabs">
<button class="active" onclick="switchSubTab('llm')">🔑 LLM 配置</button>
<button onclick="switchSubTab('projects')">📁 项目管理</button>
<button onclick="switchSubTab('validate')">✅ 验证诊断</button>
</div>

<!-- ── LLM 配置子面板 ── -->
<div class="subpanel active" id="sub-llm">
<div class="card">
<div class="card-header"><span class="dot blue"></span>🔑 大模型 API 配置</div>
<div class="card-body">
<p style="font-size:12px;color:var(--text-secondary);margin-bottom:12px">支持多提供商同时配置。填入 API Key → 测试连接 → 自动在分析中可用。</p>
<div class="provider-grid" id="providerGrid"></div>
<div style="margin-top:12px;display:flex;gap:8px">
<button class="btn" style="background:var(--primary);color:#fff;border:none;padding:7px 14px;border-radius:6px;font-size:12px;font-weight:600;cursor:pointer;font-family:inherit" onclick="saveAllConfig()">💾 保存全部</button>
<button class="btn-outline-sm" onclick="loadConfig()">🔄 重新加载</button>
</div>
</div>
</div>
</div>

<!-- ── 项目管理子面板 ── -->
<div class="subpanel" id="sub-projects">
<div class="card">
<div class="card-header"><span class="dot green"></span>📁 项目工作区 <code style="font-size:10px;color:var(--text-muted);font-weight:400;margin-left:6px">workspace/projects/</code></div>
<div class="card-body">
<div style="display:flex;gap:8px;margin-bottom:12px;flex-wrap:wrap;align-items:center">
<button class="btn" style="background:var(--primary);color:#fff;border:none;padding:7px 14px;border-radius:6px;font-size:12px;font-weight:600;cursor:pointer;font-family:inherit" onclick="scanProjects()" id="scanBtn">🔍 扫描项目</button>
<button class="btn-outline-sm" onclick="loadProjects()">🔄 刷新</button>
<span style="font-size:11px;color:var(--text-muted)">将项目文件夹拖入 workspace/projects/ 后扫描</span>
</div>
<div class="project-list" id="projectList">
<div style="text-align:center;padding:24px;color:var(--text-muted)">📂 点击「扫描项目」发现项目</div>
</div>
</div>
</div>
</div>

<!-- ── 验证诊断子面板 ── -->
<div class="subpanel" id="sub-validate">
<div class="card">
<div class="card-header"><span class="dot orange"></span>✅ 系统验证与诊断</div>
<div class="card-body">
<div style="display:flex;gap:6px;margin-bottom:12px;flex-wrap:wrap">
<button class="btn" style="background:var(--primary);color:#fff;border:none;padding:7px 14px;border-radius:6px;font-size:12px;font-weight:600;cursor:pointer;font-family:inherit" onclick="runValidation('llm')">🔗 LLM 连通性</button>
<button class="btn" style="background:var(--primary);color:#fff;border:none;padding:7px 14px;border-radius:6px;font-size:12px;font-weight:600;cursor:pointer;font-family:inherit" onclick="runValidation('tools')">🔧 工具链 I/O</button>
<button class="btn" style="background:var(--green);color:#fff;border:none;padding:7px 14px;border-radius:6px;font-size:12px;font-weight:600;cursor:pointer;font-family:inherit" onclick="runValidation('all')">🚀 全量诊断</button>
</div>
<div class="val-summary">
<div class="val-stat total"><div class="val-num" id="valTotal">--</div><div class="val-label">总测试数</div></div>
<div class="val-stat pass"><div class="val-num" id="valPass">--</div><div class="val-label">通过</div></div>
<div class="val-stat fail"><div class="val-num" id="valFail">--</div><div class="val-label">失败</div></div>
</div>
<div class="result-list" id="resultList">
<div style="text-align:center;padding:20px;color:var(--text-muted)">🔬 点击上方按钮运行诊断</div>
</div>
</div>
</div>
</div>

</div><!-- end panel-workspace -->

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

// ===== Tab切换 =====
function switchMainTab(name){
document.querySelectorAll('.tab-nav button').forEach(b=>b.classList.remove('active'));
document.querySelectorAll('.tab-panel').forEach(p=>p.classList.remove('active'));
event.target.classList.add('active');
document.getElementById('panel-'+name).classList.add('active');
if(name==='workspace'){loadConfig();loadProjects()}
}
function switchSubTab(name){
document.querySelectorAll('.subtabs button').forEach(b=>b.classList.remove('active'));
document.querySelectorAll('.subpanel').forEach(p=>p.classList.remove('active'));
event.target.classList.add('active');
document.getElementById('sub-'+name).classList.add('active');
}

// ===== Workspace: LLM Config =====
const DEFAULTS=[
{name:'deepseek',type:'deepseek',icon:'ds',label:'DeepSeek',model:'deepseek-v4-pro',base_url:'https://api.deepseek.com',api_key:'',enabled:true},
{name:'openai',type:'openai',icon:'oai',label:'OpenAI',model:'gpt-4o',base_url:'https://api.openai.com/v1',api_key:'',enabled:false},
{name:'anthropic',type:'anthropic',icon:'anth',label:'Anthropic',model:'claude-sonnet-5',base_url:'https://api.anthropic.com',api_key:'',enabled:false},
{name:'local',type:'openai_compat',icon:'local',label:'Ollama/本地',model:'qwen3:latest',base_url:'http://localhost:11434/v1',api_key:'ollama',enabled:false}
];
let providers=JSON.parse(JSON.stringify(DEFAULTS));
let providerStatus={};

async function loadConfig(){
try{
const r=await fetch('/api/v1/workspace/config/llm');const d=await r.json();
if(d.providers&&d.providers.length){
d.providers.forEach((p,i)=>{
if(providers[i]){
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
let s='';
if(connected) s='<span class="provider-status connected">✓</span>';
else if(testing) s='<span class="provider-status disconnected"><span class="spinner-sm"></span></span>';
else if(error) s='<span class="provider-status disconnected">✗</span>';
else if(p.api_key_configured) s='<span class="provider-status connected">已配</span>';

return '<div class="provider-card'+(connected?' connected':(error?' error':''))+'">'+s+
'<div class="provider-header"><div class="provider-icon '+p.icon+'">'+p.label[0]+'</div><div><div class="provider-name">'+p.label+'</div><div class="provider-type">'+p.type+'</div></div></div>'+
'<div class="provider-body">'+
'<div class="form-group"><label>API Key</label><input type="password" id="key-'+i+'" placeholder="'+(p.api_key_masked||'sk-...')+'" value="'+(p.api_key||'')+'" onfocus="this.type=\'text\'" onblur="if(!this.value)this.type=\'password\'"></div>'+
'<div class="form-row"><div class="form-group"><label>Base URL</label><input type="text" id="url-'+i+'" value="'+p.base_url+'"></div><div class="form-group"><label>Model</label><input type="text" id="model-'+i+'" value="'+p.model+'"></div></div>'+
'<div class="provider-actions"><button class="btn-outline-sm" onclick="testProvider('+i+')" '+(testing?'disabled':'')+'>'+(testing?'<span class="spinner-sm"></span> ':'')+'🔗 测试</button><label style="display:flex;align-items:center;gap:4px;font-size:11px;color:var(--text-secondary);cursor:pointer;margin-left:6px"><input type="checkbox" id="enabled-'+i+'" '+(p.enabled?'checked':'')+' onchange="providers['+i+'].enabled=this.checked"> 启用</label></div>'+
'<div id="test-result-'+i+'" style="font-size:10px;margin-top:2px"></div>'+
'</div></div>';
}).join('');
}

async function testProvider(idx){
const p=providers[idx];
const keyEl=document.getElementById('key-'+idx);
const apiKey=keyEl.value.trim()||p.api_key||'';
if(!apiKey||apiKey.includes('your-api-key')){alert('请先填入 '+p.label+' 的 API Key');return}
providerStatus[p.name]='testing';renderProviders();
try{
const r=await fetch('/api/v1/workspace/validate/llm',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({provider:p.name})});
const d=await r.json();
const el=document.getElementById('test-result-'+idx);
if(d.result&&d.result.passed){
providerStatus[p.name]='ok';
el.innerHTML='<span style="color:var(--green)">✓ '+Math.round(d.result.latency_ms)+'ms</span>';
}else{
providerStatus[p.name]='error';
el.innerHTML='<span style="color:var(--red)">✗ '+(d.result?d.result.error:'连接失败')+'</span>';
}
}catch(e){providerStatus[p.name]='error';document.getElementById('test-result-'+idx).innerHTML='<span style="color:var(--red)">✗ '+e.message+'</span>'}
renderProviders();
}

async function saveAllConfig(){
let yaml='# Noosphere Workspace - LLM Configuration\n# Saved from Web UI\n\ndefault_provider: '+(providers.find(p=>p.enabled)||providers[0]).name+'\n\nproviders:\n';
providers.forEach((p,i)=>{
const k=document.getElementById('key-'+i);const u=document.getElementById('url-'+i);const m=document.getElementById('model-'+i);
yaml+='  - name: '+p.name+'\n    type: '+p.type+'\n    api_key: "'+(k&&k.value.trim()||p.api_key||'')+'"\n    base_url: '+(u&&u.value.trim()||p.base_url)+'\n    model: '+(m&&m.value.trim()||p.model)+'\n    enabled: '+(p.enabled!==false)+'\n';
});
try{
const r=await fetch('/api/v1/workspace/config/llm',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({yaml_content:yaml})});
if(r.ok){showToast('配置已保存到 workspace/llm_config.yaml','success')}else{showToast('保存失败，请查看控制台','error');console.log(yaml)}
}catch(e){showToast('保存失败: '+e.message,'error')}
}

// ===== Workspace: Projects =====
async function loadProjects(){
try{
const r=await fetch('/api/v1/workspace/projects');const d=await r.json();
const list=document.getElementById('projectList');
const projects=d.projects||[];
if(!projects.length){list.innerHTML='<div style="text-align:center;padding:24px;color:var(--text-muted)">📂 还没有项目 — 将项目文件夹放入 workspace/projects/ 后点击扫描</div>';return}
list.innerHTML=projects.map(p=>{
let icon='other';const langs=(p.languages||[]).map(l=>l.toLowerCase()).join(' ');
if(langs.includes('go')) icon='go';else if(langs.includes('python')) icon='py';else if(langs.includes('javascript')) icon='js';
const statusClass='status-'+(p.scan_status||'pending');
const statusText={pending:'待分析',scanning:'扫描中',done:'已分析',error:'出错'}[p.scan_status]||'待分析';
let tags='';
if(p.languages&&p.languages.length) tags+=p.languages.slice(0,3).map(l=>'<span>💻 '+l+'</span>').join('');
if(p.frameworks&&p.frameworks.length) tags+='<span>📦 '+p.frameworks.slice(0,2).join(', ')+'</span>';
tags+='<span>📄 '+p.total_files+' 文件</span>';
return '<div class="project-item"><div class="project-icon-sm '+icon+'">'+(p.languages&&p.languages[0]?p.languages[0][0].toUpperCase():'?')+'</div><div class="project-info-sm"><div class="project-name-sm">'+p.name+'</div><div class="project-meta-sm">'+tags+'</div></div><span class="project-status '+statusClass+'">'+statusText+'</span><button class="btn-outline-sm" onclick="analyzeProject(\''+p.name+'\')">🔍 分析</button></div>';
}).join('');
}catch(e){document.getElementById('projectList').innerHTML='<div style="text-align:center;padding:24px;color:var(--red)">⚠ 加载失败: '+e.message+'</div>'}
}

async function scanProjects(){
const btn=document.getElementById('scanBtn');btn.innerHTML='<span class="spinner-sm"></span> 扫描中...';btn.disabled=true;
try{
const r=await fetch('/api/v1/workspace/scan',{method:'POST'});const d=await r.json();
showToast('发现 '+d.count+' 个项目','success');loadProjects();
}catch(e){showToast('扫描失败: '+e.message,'error')}
btn.innerHTML='🔍 扫描项目';btn.disabled=false;
}

async function analyzeProject(name){
showToast('正在分析: '+name+'...','info');
try{
const r=await fetch('/api/v1/workspace/projects/'+encodeURIComponent(name)+'/analyze',{method:'POST'});
const d=await r.json();
if(r.ok){showToast('分析完成: '+name,'success');loadProjects();loadStatus()}
else{showToast('分析失败: '+(d.detail||'未知错误'),'error')}
}catch(e){showToast('分析失败: '+e.message,'error')}
}

// ===== Workspace: Validation =====
async function runValidation(type){
const endpoints={llm:'/api/v1/workspace/validate/llm',tools:'/api/v1/workspace/validate/tools',all:'/api/v1/workspace/validate/all'};
const rl=document.getElementById('resultList');
rl.innerHTML='<div style="text-align:center;padding:20px"><span class="spinner-sm"></span> 正在测试...</div>';
try{
const r=await fetch(endpoints[type],{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({})});
const d=await r.json();
document.getElementById('valTotal').textContent=d.total_tests||0;
document.getElementById('valPass').textContent=d.passed_tests||0;
document.getElementById('valFail').textContent=d.failed_tests||0;
let results=[];
if(d.results) results=d.results;
if(d.llm_report&&d.llm_report.results) results=[...results,...d.llm_report.results];
if(d.tool_report&&d.tool_report.results) results=[...results,...d.tool_report.results];
if(d.e2e_report&&d.e2e_report.results) results=[...results,...d.e2e_report.results];
if(!results.length){rl.innerHTML='<div style="text-align:center;padding:20px;color:var(--text-secondary)">✓ '+d.summary+'</div>';return}
rl.innerHTML=results.map(r=>'<div class="result-item"><span>'+(r.passed?'✅':'❌')+'</span><span class="r-name">'+r.test_name+'</span>'+(r.latency_ms?'<span class="r-latency">'+Math.round(r.latency_ms)+'ms</span>':'')+'<span class="r-status '+(r.passed?'r-pass':'r-fail')+'">'+(r.passed?'通过':'失败')+'</span></div>'+(r.error?'<div style="font-size:10px;color:var(--red);margin-left:28px">'+r.error+'</div>':'')).join('');
if(d.overall_status==='ok') showToast('全部验证通过！','success');
else if(d.overall_status==='partial') showToast(d.summary,'info');
}catch(e){rl.innerHTML='<div style="text-align:center;padding:20px;color:var(--red)">❌ '+e.message+'</div>'}
}

// ===== Toast =====
function showToast(msg,type){
const t=document.createElement('div');t.className='toast '+type;t.textContent=msg;
document.body.appendChild(t);
setTimeout(()=>{t.style.opacity='0';t.style.transition='opacity .3s';setTimeout(()=>t.remove(),300)},3000);
}

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

        # Register workspace save endpoint
        from .workspace_page import setup_workspace_routes
        setup_workspace_routes(app)

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
