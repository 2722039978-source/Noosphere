"""
Workspace Manager — 项目工作区管理

管理 workspace/ 目录下的项目发现、LLM 配置加载、项目分析编排。

核心功能:
- 扫描 workspace/projects/ 下的所有项目
- 自动识别项目语言、框架、结构
- 加载和管理多提供商 LLM 配置
- 编排 Nebula + CodeLens 联合分析
"""

import os
import json
import time
from pathlib import Path
from typing import Optional, Dict, Any, List
from dataclasses import dataclass, field
from datetime import datetime

from loguru import logger


# ============================================================
# 数据模型
# ============================================================

@dataclass
class ProjectInfo:
    """项目信息"""
    name: str
    path: str
    languages: List[str] = field(default_factory=list)
    frameworks: List[str] = field(default_factory=list)
    total_files: int = 0
    total_size_kb: float = 0.0
    entry_files: List[str] = field(default_factory=list)
    last_scanned: Optional[str] = None
    scan_status: str = "pending"  # pending / scanning / done / error
    scan_error: Optional[str] = None

    def to_dict(self) -> Dict[str, Any]:
        return {
            "name": self.name,
            "path": self.path,
            "languages": self.languages,
            "frameworks": self.frameworks,
            "total_files": self.total_files,
            "total_size_kb": round(self.total_size_kb, 2),
            "entry_files": self.entry_files,
            "last_scanned": self.last_scanned,
            "scan_status": self.scan_status,
            "scan_error": self.scan_error,
        }


@dataclass
class LLMProviderConfig:
    """单个 LLM 提供商配置"""
    name: str
    provider_type: str  # deepseek / openai / anthropic / openai_compat
    api_key: str
    base_url: str
    model: str
    enabled: bool = True
    temperature: float = 0.1
    max_tokens: int = 4096
    extra: Dict[str, Any] = field(default_factory=dict)

    def to_dict(self) -> Dict[str, Any]:
        return {
            "name": self.name,
            "type": self.provider_type,
            "model": self.model,
            "base_url": self.base_url,
            "enabled": self.enabled,
        }


# ============================================================
# 语言/框架检测
# ============================================================

# 扩展名 → 语言映射
EXTENSION_LANG_MAP = {
    ".py": "Python",
    ".js": "JavaScript",
    ".mjs": "JavaScript",
    ".cjs": "JavaScript",
    ".ts": "TypeScript",
    ".tsx": "TypeScript (React)",
    ".jsx": "JavaScript (React)",
    ".go": "Go",
    ".rs": "Rust",
    ".java": "Java",
    ".kt": "Kotlin",
    ".kts": "Kotlin",
    ".swift": "Swift",
    ".cpp": "C++",
    ".cc": "C++",
    ".cxx": "C++",
    ".c": "C",
    ".h": "C/C++ Header",
    ".hpp": "C++ Header",
    ".cs": "C#",
    ".rb": "Ruby",
    ".php": "PHP",
    ".scala": "Scala",
    ".ex": "Elixir",
    ".exs": "Elixir",
    ".clj": "Clojure",
    ".lua": "Lua",
    ".r": "R",
    ".sql": "SQL",
    ".sh": "Shell",
    ".bash": "Shell",
    ".zsh": "Shell",
    ".ps1": "PowerShell",
    ".yaml": "YAML",
    ".yml": "YAML",
    ".toml": "TOML",
    ".json": "JSON",
    ".xml": "XML",
    ".md": "Markdown",
    ".css": "CSS",
    ".scss": "SCSS",
    ".less": "Less",
    ".html": "HTML",
    ".vue": "Vue",
    ".svelte": "Svelte",
    ".dart": "Dart",
    ".proto": "Protobuf",
}

# 框架检测：文件名 → 框架
FRAMEWORK_INDICATORS = {
    "go.mod": "Go Modules",
    "go.sum": "Go Modules",
    "package.json": "Node.js / npm",
    "package-lock.json": "npm",
    "yarn.lock": "Yarn",
    "pnpm-lock.yaml": "pnpm",
    "tsconfig.json": "TypeScript",
    "requirements.txt": "Python (pip)",
    "Pipfile": "Python (Pipenv)",
    "pyproject.toml": "Python (Poetry/PDM)",
    "setup.py": "Python (setuptools)",
    "setup.cfg": "Python (setuptools)",
    "Cargo.toml": "Rust (Cargo)",
    "Cargo.lock": "Rust (Cargo)",
    "pom.xml": "Java (Maven)",
    "build.gradle": "Java (Gradle)",
    "build.gradle.kts": "Java (Gradle Kotlin DSL)",
    "Gemfile": "Ruby (Bundler)",
    "Rakefile": "Ruby (Rake)",
    "composer.json": "PHP (Composer)",
    "Dockerfile": "Docker",
    "docker-compose.yml": "Docker Compose",
    "docker-compose.yaml": "Docker Compose",
    "Makefile": "Make",
    "CMakeLists.txt": "CMake",
    "next.config.js": "Next.js",
    "next.config.mjs": "Next.js",
    "next.config.ts": "Next.js",
    "tailwind.config.js": "Tailwind CSS",
    "tailwind.config.ts": "Tailwind CSS",
    "vite.config.js": "Vite",
    "vite.config.ts": "Vite",
    "webpack.config.js": "Webpack",
    ".eslintrc.js": "ESLint",
    ".eslintrc.json": "ESLint",
    "eslint.config.js": "ESLint",
    ".prettierrc": "Prettier",
    "prettier.config.js": "Prettier",
}

# 入口文件启发式
ENTRY_FILE_PATTERNS = [
    "main.go", "main.py", "main.rs", "main.cpp", "main.c",
    "app.py", "server.py", "index.py", "run.py", "manage.py",
    "index.js", "server.js", "app.js", "index.ts", "server.ts", "app.ts",
    "Program.cs", "Main.java", "Application.kt",
    "pages/index.tsx", "pages/index.jsx", "app/page.tsx",
    "src/main.py", "src/main.go", "src/index.js", "src/index.ts",
    "cmd/main.go", "cmd/server/main.go",
    "src/App.tsx", "src/App.jsx",
]


# ============================================================
# Workspace Manager
# ============================================================

class WorkspaceManager:
    """
    工作区管理器

    管理 workspace/ 目录，发现和分析用户放入的项目。

    使用示例:
        wm = WorkspaceManager("../../workspace")
        projects = wm.scan_projects()
        for p in projects:
            print(f"{p.name}: {p.languages}")
    """

    def __init__(self, workspace_root: str):
        """
        初始化工作区管理器

        Args:
            workspace_root: workspace 目录路径
        """
        self.workspace_root = Path(workspace_root).resolve()
        self.projects_dir = self.workspace_root / "projects"
        self.llm_config_path = self.workspace_root / "llm_config.yaml"
        self._state_file = self.workspace_root / ".workspace_state.json"

        # 确保必要目录存在
        self.projects_dir.mkdir(parents=True, exist_ok=True)

        logger.info(f"Workspace Manager 初始化: {self.workspace_root}")

    # ─── 项目扫描 ───

    def scan_projects(self) -> List[ProjectInfo]:
        """
        扫描 projects/ 下的所有子目录，识别项目

        Returns:
            项目信息列表
        """
        projects = []

        if not self.projects_dir.exists():
            logger.warning(f"projects 目录不存在: {self.projects_dir}")
            return projects

        for entry in sorted(self.projects_dir.iterdir()):
            if not entry.is_dir():
                continue
            if entry.name.startswith("."):
                continue

            try:
                info = self._analyze_project_dir(entry)
                projects.append(info)
            except Exception as e:
                logger.error(f"扫描项目 {entry.name} 失败: {e}")
                projects.append(ProjectInfo(
                    name=entry.name,
                    path=str(entry),
                    scan_status="error",
                    scan_error=str(e),
                ))

        # 保存状态
        self._save_state(projects)

        logger.info(f"扫描完成: 发现 {len(projects)} 个项目")
        return projects

    def get_project(self, name: str) -> Optional[ProjectInfo]:
        """获取单个项目信息"""
        project_path = self.projects_dir / name
        if not project_path.exists() or not project_path.is_dir():
            return None
        return self._analyze_project_dir(project_path)

    def _analyze_project_dir(self, project_path: Path) -> ProjectInfo:
        """
        分析单个项目目录

        检测语言、框架、入口文件等。
        """
        name = project_path.name
        languages = set()
        frameworks = set()
        entry_files = []
        total_files = 0
        total_size = 0

        # 需要排除的目录
        exclude_dirs = {
            ".git", "node_modules", "__pycache__", "vendor",
            "dist", "build", "target", ".next", ".turbo",
            "venv", ".venv", "env", ".env", "egg-info",
            ".mypy_cache", ".pytest_cache", ".ruff_cache",
            "coverage", ".nyc_output",
        }

        for file_path in project_path.rglob("*"):
            if file_path.is_file():
                # 跳过排除目录中的文件
                parts = file_path.relative_to(project_path).parts
                if any(p in exclude_dirs for p in parts):
                    continue

                total_files += 1
                try:
                    total_size += file_path.stat().st_size
                except OSError:
                    pass

                # 检测语言
                ext = file_path.suffix.lower()
                if ext in EXTENSION_LANG_MAP:
                    languages.add(EXTENSION_LANG_MAP[ext])

                # 检测框架
                if file_path.name in FRAMEWORK_INDICATORS:
                    frameworks.add(FRAMEWORK_INDICATORS[file_path.name])

                # 检测入口文件
                rel_path = str(file_path.relative_to(project_path)).replace("\\", "/")
                for pattern in ENTRY_FILE_PATTERNS:
                    if rel_path == pattern or rel_path.endswith("/" + pattern.split("/")[-1]):
                        if file_path.name == pattern.split("/")[-1]:
                            entry_files.append(rel_path)
                            break

        return ProjectInfo(
            name=name,
            path=str(project_path),
            languages=sorted(languages),
            frameworks=sorted(frameworks),
            total_files=total_files,
            total_size_kb=round(total_size / 1024, 2),
            entry_files=sorted(set(entry_files)),
            last_scanned=datetime.now().isoformat(),
            scan_status="done",
        )

    # ─── LLM 配置加载 ───

    def load_llm_config(self) -> Dict[str, Any]:
        """
        加载 LLM 配置

        优先级:
        1. workspace/llm_config.yaml（用户配置）
        2. 根目录 .env 文件（兼容旧版）
        3. 环境变量

        Returns:
            LLM 配置字典
        """
        config = {
            "default_provider": "deepseek",
            "providers": [],
        }

        # 尝试加载 YAML 配置
        if self.llm_config_path.exists():
            try:
                yaml_config = self._load_yaml_config()
                if yaml_config:
                    config = yaml_config
                    logger.info(f"从 {self.llm_config_path} 加载 LLM 配置")
            except Exception as e:
                logger.warning(f"加载 llm_config.yaml 失败: {e}")

        # 如果没有配置提供商，从环境变量/.env 构建
        if not config.get("providers"):
            config["providers"] = self._build_providers_from_env()

        return config

    def get_provider_config(self, provider_name: str = None) -> Optional[LLMProviderConfig]:
        """获取指定提供商的配置"""
        config = self.load_llm_config()

        if not provider_name:
            provider_name = config.get("default_provider", "deepseek")

        for p in config.get("providers", []):
            if p.get("name") == provider_name and p.get("enabled", True):
                return LLMProviderConfig(
                    name=p["name"],
                    provider_type=p.get("type", "deepseek"),
                    api_key=p.get("api_key", ""),
                    base_url=p.get("base_url", "https://api.deepseek.com"),
                    model=p.get("model", "deepseek-v4-pro"),
                    enabled=p.get("enabled", True),
                    temperature=p.get("temperature", 0.1),
                    max_tokens=p.get("max_tokens", 4096),
                    extra={k: v for k, v in p.items()
                           if k not in ("name", "type", "api_key", "base_url",
                                        "model", "enabled", "temperature", "max_tokens")},
                )

        return None

    def _load_yaml_config(self) -> Optional[Dict]:
        """解析 YAML 配置文件（零依赖实现）"""
        try:
            import yaml
            with open(self.llm_config_path, "r", encoding="utf-8") as f:
                return yaml.safe_load(f)
        except ImportError:
            # 无 PyYAML 时的简单解析（支持基本结构）
            return self._parse_simple_yaml()

    def _parse_simple_yaml(self) -> Optional[Dict]:
        """
        简单 YAML 解析器（仅支持本配置文件的结构）

        不做完整 YAML 解析，只处理我们的配置格式。
        """
        with open(self.llm_config_path, "r", encoding="utf-8") as f:
            content = f.read()

        result = {"providers": []}
        current_provider = None
        default_provider = "deepseek"

        for line in content.split("\n"):
            # 跳过注释和空行
            stripped = line.strip()
            if not stripped or stripped.startswith("#"):
                continue

            # 顶级键
            if line.startswith("default_provider:"):
                default_provider = stripped.split(":", 1)[1].strip()
                result["default_provider"] = default_provider
                continue

            # 新的 provider 条目
            if stripped.startswith("- name:"):
                if current_provider:
                    result["providers"].append(current_provider)
                name = stripped.split(":", 1)[1].strip()
                current_provider = {"name": name}
                continue

            # provider 的子字段（缩进 4 空格）
            if current_provider is not None and line.startswith("    "):
                if ":" in stripped:
                    key, _, value = stripped.partition(":")
                    key = key.strip()
                    value = value.strip().strip('"').strip("'")

                    if value == "true":
                        value = True
                    elif value == "false":
                        value = False
                    elif value.replace(".", "").isdigit():
                        value = float(value) if "." in value else int(value)

                    current_provider[key] = value

        if current_provider:
            result["providers"].append(current_provider)

        return result if result["providers"] else None

    def _build_providers_from_env(self) -> List[Dict]:
        """从环境变量和 .env 文件构建提供商配置（兼容旧版）"""
        # 复用已有的 config 模块
        try:
            from ..config import get_api_key, get_model, get_base_url, is_configured
            api_key = get_api_key()
            model = get_model()
            base_url = get_base_url()

            if is_configured():
                return [{
                    "name": "deepseek",
                    "type": "deepseek",
                    "api_key": api_key,
                    "base_url": base_url,
                    "model": model,
                    "enabled": True,
                    "temperature": 0.1,
                    "max_tokens": 4096,
                }]
        except ImportError:
            pass

        # 直接读环境变量
        api_key = os.environ.get("DEEPSEEK_API_KEY", "")
        if api_key:
            return [{
                "name": "deepseek",
                "type": "deepseek",
                "api_key": api_key,
                "base_url": os.environ.get("DEEPSEEK_BASE_URL", "https://api.deepseek.com"),
                "model": os.environ.get("DEEPSEEK_MODEL", "deepseek-v4-pro"),
                "enabled": True,
                "temperature": 0.1,
                "max_tokens": 4096,
            }]

        return []

    def is_llm_configured(self) -> bool:
        """检查是否至少有一个 LLM 提供商已配置"""
        config = self.load_llm_config()
        for p in config.get("providers", []):
            if p.get("enabled", True) and p.get("api_key", ""):
                # 排除占位符
                key = p["api_key"]
                if key and "your-api-key" not in key.lower() and len(key) > 10:
                    return True
        return False

    # ─── 状态持久化 ───

    def _save_state(self, projects: List[ProjectInfo]):
        """保存工作区状态"""
        try:
            state = {
                "updated_at": datetime.now().isoformat(),
                "projects": [p.to_dict() for p in projects],
            }
            with open(self._state_file, "w", encoding="utf-8") as f:
                json.dump(state, f, indent=2, ensure_ascii=False)
        except Exception as e:
            logger.warning(f"保存状态失败: {e}")

    def load_state(self) -> Optional[Dict]:
        """加载上次的工作区状态"""
        if self._state_file.exists():
            try:
                with open(self._state_file, "r", encoding="utf-8") as f:
                    return json.load(f)
            except Exception:
                pass
        return None
