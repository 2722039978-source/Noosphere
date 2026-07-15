#!/usr/bin/env python3
"""
CodeLens AI - Intelligent Code Understanding Platform

A developer-oriented AI Code Intelligence tool that parses code structure,
builds project knowledge graphs, and combines RAG with Agent technology
for large codebase understanding, Q&A, call chain analysis, and
automatic documentation generation.
"""

import os
import sys
import json
from pathlib import Path

# ---- Lazy import helpers ----

_IMPORT_CACHE = {}

def _try_import(module_name, pip_name=None):
    """Lazy import with helpful error message."""
    if module_name in _IMPORT_CACHE:
        return _IMPORT_CACHE[module_name]
    try:
        mod = __import__(module_name)
        _IMPORT_CACHE[module_name] = mod
        return mod
    except ImportError:
        name = pip_name or module_name
        print(f"[ERROR] Missing dependency: {name}")
        print(f"  Install with: pip install {name}")
        print(f"  Or run: pip install -r requirements.txt")
        return None

def _get_rich_console():
    """Get Rich Console or fallback to plain print."""
    rich = _try_import("rich")
    if rich is None:
        return None
    from rich.console import Console
    return Console()

def _load_config(config_path="config/settings.yaml"):
    """Load YAML config, return empty dict on failure."""
    if not os.path.exists(config_path):
        return {}

    yaml = _try_import("yaml", "pyyaml")
    if yaml is None:
        # Fallback: try JSON config if YAML not available
        json_path = config_path.replace(".yaml", ".json").replace(".yml", ".json")
        if os.path.exists(json_path):
            try:
                with open(json_path, "r", encoding="utf-8") as f:
                    return json.load(f)
            except Exception:
                pass
        return {}

    try:
        with open(config_path, "r", encoding="utf-8") as f:
            return yaml.safe_load(f)
    except Exception as e:
        print(f"[WARN] Failed to load config: {e}")
        return {}

def _check_core_deps():
    """Check if core dependencies are available. Returns list of missing."""
    core_deps = [
        ("tree_sitter", "tree-sitter"),
        ("networkx", "networkx"),
        ("loguru", "loguru"),
    ]
    missing = []
    for mod, pip_name in core_deps:
        try:
            __import__(mod)
        except ImportError:
            missing.append(pip_name)
    return missing


# ---- Banner ----

BANNER = r"""
   ____          _      _
  / ___|___   __| | ___| | ___ _ __  ___
 | |   / _ \ / _` |/ _ \ |/ _ \ '_ \/ __|
 | |__| (_) | (_| |  __/ |  __/ | | \__ \
  \____\___/ \__,_|\___|_|\___|_| |_|___/
    _    ___
   / \  |_ _|
  / _ \  | |
 / ___ \ | |
/_/   \_\___|

  CodeLens AI v1.0.0
  Intelligent Code Understanding Platform
"""

def print_banner(console=None):
    """Print startup banner."""
    if console:
        console.print(BANNER, style="bold cyan")
        console.print("Intelligent Code Understanding Platform\n", style="dim")
    else:
        print(BANNER)
        print("  Intelligent Code Understanding Platform\n")


# ---- CLI Commands ----

def cmd_info():
    """Display system information."""
    print_banner()
    print("System Information:")
    print(f"  Python: {sys.version}")
    print(f"  Platform: {sys.platform}")
    print(f"  Working dir: {os.getcwd()}")

    # Check all dependencies
    all_deps = [
        ("tree_sitter", "tree-sitter", "Multi-language code parsing"),
        ("networkx", "networkx", "Knowledge graph engine"),
        ("yaml", "pyyaml", "YAML config support"),
        ("loguru", "loguru", "Logging"),
        ("rich", "rich", "Terminal UI"),
        ("click", "click", "CLI framework"),
        ("fastapi", "fastapi", "Web API server"),
        ("uvicorn", "uvicorn", "ASGI server"),
        ("chromadb", "chromadb", "Vector storage"),
        ("langchain", "langchain", "RAG framework"),
        ("git", "gitpython", "Git integration"),
        ("sentence_transformers", "sentence-transformers", "Code embeddings"),
    ]

    print("\nDependencies:")
    available = 0
    for mod, pip_name, desc in all_deps:
        try:
            __import__(mod.replace("-", "_"))
            print(f"  [OK] {pip_name} - {desc}")
            available += 1
        except ImportError:
            print(f"  [--] {pip_name} - {desc} (not installed)")

    print(f"\n{available}/{len(all_deps)} dependencies available")
    if available < len(all_deps):
        print("Run: pip install -r requirements.txt")

    # DeepSeek API 配置状态
    try:
        from .config import print_config_status
        print(f"\n{print_config_status()}")
    except ImportError:
        pass


def cmd_index(project=".", languages=None, exclude=None, no_parallel=False, workers=4):
    """Build project index."""
    missing = _check_core_deps()
    if missing:
        print(f"[ERROR] Missing core dependencies: {', '.join(missing)}")
        print("Run: pip install -r requirements.txt")
        sys.exit(1)

    rich = _try_import("rich")
    console = None
    if rich:
        try:
            from rich.console import Console
            from rich.table import Table
            from rich.progress import Progress, SpinnerColumn, TextColumn
            # Force UTF-8 to avoid GBK encoding errors on Windows
            console = Console(force_terminal=True)
        except Exception:
            rich = None

    print_banner(console)

    from .agent.code_agent import CodeLensAgent, AgentConfig, AgentAction

    agent_config = AgentConfig(
        project_root=project,
        languages=list(languages) if languages else None,
        exclude_dirs=list(exclude) if exclude else None,
        parallel=not no_parallel,
        max_workers=workers,
    )

    agent = CodeLensAgent(agent_config)

    # Build index (with or without Rich progress bar)
    try:
        if console:
            with Progress(SpinnerColumn(), TextColumn("[progress.description]{task.description}"), console=console) as progress:
                task = progress.add_task("[cyan]Building project index...", total=None)
                result = agent.execute(AgentAction.INDEX_PROJECT, project_root=project)
                progress.update(task, completed=True)
        else:
            print("Building project index...")
            result = agent.execute(AgentAction.INDEX_PROJECT, project_root=project)
    except (UnicodeEncodeError, UnicodeDecodeError, UnicodeError):
        # Fallback: Windows console can't handle Rich's Unicode spinners
        console = None
        print("Building project index...")
        result = agent.execute(AgentAction.INDEX_PROJECT, project_root=project)

    if result.success:
        summary = agent.get_project_summary()
        try:
            if console:
                table = Table(title="Index Summary")
                table.add_column("Metric", style="cyan")
                table.add_column("Value", style="green")
                table.add_row("Project Root", summary["project_root"])
                table.add_row("Total Files", str(summary["total_files"]))
                table.add_row("Total Entities", str(summary["total_entities"]))
                table.add_row("Languages", ", ".join(summary.get("languages", [])))
                kg_stats = summary.get("kg_stats", {})
                table.add_row("KG Nodes", str(kg_stats.get("total_nodes", 0)))
                table.add_row("KG Edges", str(kg_stats.get("total_edges", 0)))
                table.add_row("Index Time", f"{result.execution_time_ms:.0f}ms")
                console.print(table)
            else:
                raise ValueError("no console")
        except Exception:
            print(f"\nIndex Summary:")
            print(f"  Files: {summary['total_files']}")
            print(f"  Entities: {summary['total_entities']}")
        else:
            print(f"\nIndex Summary:")
            print(f"  Files: {summary['total_files']}")
            print(f"  Entities: {summary['total_entities']}")
            print(f"  Time: {result.execution_time_ms:.0f}ms")
        print("Index built successfully!")
    else:
        print(f"Index failed: {result.error}")
        sys.exit(1)


def cmd_serve(project=".", host="0.0.0.0", port=8765, reload=False, no_browser=False):
    """Start Web API server."""
    missing = _check_core_deps()
    if missing:
        print(f"[ERROR] Missing core dependencies: {', '.join(missing)}")
        print("Run: pip install -r requirements.txt")
        sys.exit(1)

    fastapi = _try_import("fastapi")
    if fastapi is None:
        sys.exit(1)

    rich = _try_import("rich")
    console = None
    if rich:
        from rich.console import Console
        console = Console()

    print_banner(console)

    from .agent.code_agent import CodeLensAgent, AgentConfig, AgentAction
    from .api.server import APIServer

    agent_config = AgentConfig(project_root=project)
    agent = CodeLensAgent(agent_config)

    print("Indexing project...")
    agent.execute(AgentAction.INDEX_PROJECT, project_root=project)

    server = APIServer(agent)

    if not no_browser:
        import webbrowser
        import threading
        url = f"http://localhost:{port}"
        threading.Timer(1.5, lambda: webbrowser.open(url)).start()
        print(f"Opening browser at {url}...")

    server.run(host=host, port=port, reload=reload)


def cmd_ask(project=".", question="", query_type="general"):
    """Code Q&A."""
    missing = _check_core_deps()
    if missing:
        print(f"[ERROR] Missing core dependencies: {', '.join(missing)}")
        print("Run: pip install -r requirements.txt")
        sys.exit(1)

    rich = _try_import("rich")
    console = None
    if rich:
        from rich.console import Console
        console = Console()

    from .agent.code_agent import CodeLensAgent, AgentConfig, AgentAction

    agent_config = AgentConfig(project_root=project)
    agent = CodeLensAgent(agent_config)

    print("Indexing project...")
    agent.execute(AgentAction.INDEX_PROJECT, project_root=project)

    print(f"\nQuestion: {question}\n")
    response = agent.ask(question, query_type=query_type)

    if console:
        from rich.panel import Panel
        console.print(Panel(response.answer, title="Answer", border_style="green"))
    else:
        print(f"Answer:\n{response.answer}")

    if response.sources:
        print("\nSources:")
        for source in response.sources[:5]:
            print(f"  - {source['file']}:{source['line']}")

    print(f"\nRetrieval: {response.retrieval_time_ms:.0f}ms | Generation: {response.generation_time_ms:.0f}ms")


def cmd_trace(project=".", entity="", depth=10):
    """Trace call chain."""
    missing = _check_core_deps()
    if missing:
        print(f"[ERROR] Missing core dependencies: {', '.join(missing)}")
        print("Run: pip install -r requirements.txt")
        sys.exit(1)

    from .agent.code_agent import CodeLensAgent, AgentConfig, AgentAction

    agent_config = AgentConfig(project_root=project)
    agent = CodeLensAgent(agent_config)

    print("Indexing project...")
    agent.execute(AgentAction.INDEX_PROJECT, project_root=project)

    result = agent.execute(AgentAction.TRACE_CALLS, entity_name=entity, max_depth=depth)

    if result.success:
        data = result.data
        if data.get("callers"):
            print(f"\nCalled by ({len(data['callers'])}):")
            for c in data["callers"][:20]:
                print(f"  - {c['name']} ({c['file']}:{c['line']})")
        if data.get("callees"):
            print(f"\nCalls ({len(data['callees'])}):")
            for c in data["callees"][:20]:
                print(f"  - {c['name']} ({c['file']}:{c['line']})")
        if data.get("call_chains"):
            print(f"\nCall Chains ({len(data['call_chains'])} paths):")
            for i, chain in enumerate(data["call_chains"][:5]):
                print(f"  Path {i+1}: {' -> '.join(chain)}")
    else:
        print(f"Error: {result.error}")


def cmd_impact(project=".", entity=""):
    """Impact analysis."""
    missing = _check_core_deps()
    if missing:
        print(f"[ERROR] Missing core dependencies: {', '.join(missing)}")
        print("Run: pip install -r requirements.txt")
        sys.exit(1)

    from .agent.code_agent import CodeLensAgent, AgentConfig, AgentAction

    agent_config = AgentConfig(project_root=project)
    agent = CodeLensAgent(agent_config)

    print("Indexing project...")
    agent.execute(AgentAction.INDEX_PROJECT, project_root=project)

    result = agent.execute(AgentAction.ANALYZE_IMPACT, entity_name=entity)

    if result.success:
        data = result.data
        print(f"\nImpact Analysis: {entity}")
        if data.get("direct"):
            print(f"\nDirectly Affected ({len(data['direct'])}):")
            for item in data["direct"]:
                print(f"  - {item['name']} ({item['type']}) in {item['file']}")
        if data.get("indirect"):
            print(f"\nIndirectly Affected ({len(data['indirect'])}):")
            for item in data["indirect"][:20]:
                print(f"  - {item['name']} ({item['type']}) in {item['file']} [depth {item['depth']}]")
        print(f"\nTotal: {data.get('total', 0)} entities")
    else:
        print(f"Error: {result.error}")


def cmd_docs(project=".", output="./docs/PROJECT_DOCUMENTATION.md"):
    """Generate documentation."""
    missing = _check_core_deps()
    if missing:
        print(f"[ERROR] Missing core dependencies: {', '.join(missing)}")
        print("Run: pip install -r requirements.txt")
        sys.exit(1)

    from .agent.code_agent import CodeLensAgent, AgentConfig, AgentAction

    agent_config = AgentConfig(project_root=project)
    agent = CodeLensAgent(agent_config)

    print("Indexing project...")
    agent.execute(AgentAction.INDEX_PROJECT, project_root=project)

    result = agent.execute(AgentAction.GENERATE_DOCS)

    if result.success:
        Path(output).parent.mkdir(parents=True, exist_ok=True)
        Path(output).write_text(result.data, encoding="utf-8")
        print(f"Documentation saved to: {output}")
    else:
        print(f"Error: {result.error}")


def cmd_diff(project=".", base="HEAD", target="WORKTREE", staged=False):
    """Analyze Git Diff."""
    missing = _check_core_deps()
    if missing:
        print(f"[ERROR] Missing core dependencies: {', '.join(missing)}")
        print("Run: pip install -r requirements.txt")
        sys.exit(1)

    from .agent.code_agent import CodeLensAgent, AgentConfig, AgentAction
    from .agent.git_diff_analyzer import GitDiffAnalyzer

    agent_config = AgentConfig(project_root=project)
    agent = CodeLensAgent(agent_config)

    print("Indexing project...")
    agent.execute(AgentAction.INDEX_PROJECT, project_root=project)

    kg = agent.get_knowledge_graph()
    analyzer = GitDiffAnalyzer(
        parser=agent.parser,
        extractor=agent.extractor,
        knowledge_graph=kg,
        repo_path=project,
    )

    print("Analyzing diff...")
    if staged:
        analysis = analyzer.analyze_staged()
    elif target == "WORKTREE":
        analysis = analyzer.analyze_unstaged()
    else:
        analysis = analyzer.analyze_branches(base, target)

    print(f"\nChanged Files: {len(analysis.changed_files)}")
    print(f"Changed Entities: {len(analysis.changed_entities)}")
    print(f"Overall Risk: {analysis.overall_risk.value.upper()}")
    print(f"\nSummary:\n{analysis.summary}")

    if analysis.recommendations:
        print("\nRecommendations:")
        for rec in analysis.recommendations:
            print(f"  - {rec}")


# ---- Main entry point with argparse fallback ----

def _try_click_cli():
    """Try using Click for CLI. Falls back to argparse if Click is unavailable."""
    click = _try_import("click")
    if click is None:
        return False

    @click.group()
    @click.version_option(version="1.0.0")
    def cli():
        """CodeLens AI - Intelligent Code Understanding Platform"""
        pass

    @cli.command()
    @click.option("--project", "-p", default=".", help="Project root path")
    def info_cmd(project):
        cmd_info()

    @cli.command()
    @click.option("--project", "-p", default=".", help="Project root path")
    @click.option("--languages", "-l", multiple=True, help="Languages to parse")
    @click.option("--exclude", "-e", multiple=True, help="Directories to exclude")
    @click.option("--no-parallel", is_flag=True, help="Disable parallel processing")
    @click.option("--workers", "-w", default=4, help="Number of parallel workers")
    def index(project, languages, exclude, no_parallel, workers):
        cmd_index(project=project, languages=languages, exclude=exclude,
                  no_parallel=no_parallel, workers=workers)

    @cli.command()
    @click.option("--project", "-p", default=".", help="Project root path")
    @click.option("--question", "-q", prompt=True, help="Your question")
    @click.option("--type", "-t", "query_type", default="general", help="Query type")
    def ask(project, question, query_type):
        cmd_ask(project=project, question=question, query_type=query_type)

    @cli.command()
    @click.option("--project", "-p", default=".", help="Project root path")
    @click.option("--entity", "-e", required=True, help="Function/entity name")
    @click.option("--depth", "-d", default=10, help="Max depth")
    def trace(project, entity, depth):
        cmd_trace(project=project, entity=entity, depth=depth)

    @cli.command()
    @click.option("--project", "-p", default=".", help="Project root path")
    @click.option("--entity", "-e", required=True, help="Entity name")
    def impact(project, entity):
        cmd_impact(project=project, entity=entity)

    @cli.command()
    @click.option("--project", "-p", default=".", help="Project root path")
    @click.option("--output", "-o", default="./docs/PROJECT_DOCUMENTATION.md", help="Output path")
    def docs(project, output):
        cmd_docs(project=project, output=output)

    @cli.command()
    @click.option("--project", "-p", default=".", help="Project root path")
    @click.option("--host", "-h", default="0.0.0.0", help="Bind address")
    @click.option("--port", "-P", default=8765, help="Port")
    @click.option("--reload", is_flag=True, help="Hot reload mode")
    @click.option("--no-browser", is_flag=True, help="Don't open browser")
    def serve(project, host, port, reload, no_browser):
        cmd_serve(project=project, host=host, port=port, reload=reload, no_browser=no_browser)

    @cli.command()
    @click.option("--project", "-p", default=".", help="Project root path")
    @click.option("--base", "-b", default="HEAD", help="Base ref")
    @click.option("--target", "-t", default="WORKTREE", help="Target ref")
    @click.option("--staged", is_flag=True, help="Analyze staged changes")
    def diff(project, base, target, staged):
        cmd_diff(project=project, base=base, target=target, staged=staged)

    cli()
    return True


def _argparse_fallback():
    """Fallback CLI using argparse (no external dependencies needed)."""
    import argparse

    parser = argparse.ArgumentParser(
        description="CodeLens AI - Intelligent Code Understanding Platform",
    )
    subparsers = parser.add_subparsers(dest="command", help="Commands")

    # info
    subparsers.add_parser("info", help="Show system information")

    # index
    p_index = subparsers.add_parser("index", help="Build project index")
    p_index.add_argument("--project", "-p", default=".", help="Project root path")
    p_index.add_argument("--languages", "-l", nargs="*", help="Languages to parse")
    p_index.add_argument("--exclude", "-e", nargs="*", help="Directories to exclude")
    p_index.add_argument("--no-parallel", action="store_true", help="Disable parallel")
    p_index.add_argument("--workers", "-w", type=int, default=4, help="Workers")

    # ask
    p_ask = subparsers.add_parser("ask", help="Code Q&A")
    p_ask.add_argument("--project", "-p", default=".", help="Project root path")
    p_ask.add_argument("--question", "-q", required=True, help="Your question")
    p_ask.add_argument("--type", "-t", dest="query_type", default="general", help="Query type")

    # trace
    p_trace = subparsers.add_parser("trace", help="Trace call chain")
    p_trace.add_argument("--project", "-p", default=".", help="Project root path")
    p_trace.add_argument("--entity", "-e", required=True, help="Function/entity name")
    p_trace.add_argument("--depth", "-d", type=int, default=10, help="Max depth")

    # impact
    p_impact = subparsers.add_parser("impact", help="Impact analysis")
    p_impact.add_argument("--project", "-p", default=".", help="Project root path")
    p_impact.add_argument("--entity", "-e", required=True, help="Entity name")

    # docs
    p_docs = subparsers.add_parser("docs", help="Generate documentation")
    p_docs.add_argument("--project", "-p", default=".", help="Project root path")
    p_docs.add_argument("--output", "-o", default="./docs/PROJECT_DOCUMENTATION.md", help="Output path")

    # serve
    p_serve = subparsers.add_parser("serve", help="Start Web server")
    p_serve.add_argument("--project", "-p", default=".", help="Project root path")
    p_serve.add_argument("--host", default="0.0.0.0", help="Bind address")
    p_serve.add_argument("--port", type=int, default=8765, help="Port")
    p_serve.add_argument("--reload", action="store_true", help="Hot reload")
    p_serve.add_argument("--no-browser", action="store_true", help="Don't open browser")

    # diff
    p_diff = subparsers.add_parser("diff", help="Analyze Git diff")
    p_diff.add_argument("--project", "-p", default=".", help="Project root path")
    p_diff.add_argument("--base", "-b", default="HEAD", help="Base ref")
    p_diff.add_argument("--target", "-t", default="WORKTREE", help="Target ref")
    p_diff.add_argument("--staged", action="store_true", help="Analyze staged changes")

    args = parser.parse_args()

    if args.command == "info" or args.command is None:
        cmd_info()
    elif args.command == "index":
        cmd_index(project=args.project, languages=args.languages,
                  exclude=args.exclude, no_parallel=args.no_parallel,
                  workers=args.workers)
    elif args.command == "ask":
        cmd_ask(project=args.project, question=args.question, query_type=args.query_type)
    elif args.command == "trace":
        cmd_trace(project=args.project, entity=args.entity, depth=args.depth)
    elif args.command == "impact":
        cmd_impact(project=args.project, entity=args.entity)
    elif args.command == "docs":
        cmd_docs(project=args.project, output=args.output)
    elif args.command == "serve":
        cmd_serve(project=args.project, host=args.host, port=args.port,
                  reload=args.reload, no_browser=args.no_browser)
    elif args.command == "diff":
        cmd_diff(project=args.project, base=args.base, target=args.target, staged=args.staged)


def main():
    """Main entry point."""
    # Minimal logging setup (no external deps)
    try:
        from loguru import logger
        logger.remove()
        logger.add(sys.stderr, format="<green>{time:HH:mm:ss}</green> | <level>{level}</level> | {message}",
                   level="INFO", colorize=True)
    except ImportError:
        pass  # loguru is optional

    # Try Click first (richer UX), fall back to argparse
    if _try_click_cli():
        return

    _argparse_fallback()


if __name__ == "__main__":
    main()
