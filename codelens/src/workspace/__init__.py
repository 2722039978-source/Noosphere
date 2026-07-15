"""
Noosphere Workspace 模块

提供项目工作区管理、LLM 配置加载、项目扫描与验证功能。

使用示例:
    from src.workspace import WorkspaceManager, LLMValidator

    # 管理工作区
    wm = WorkspaceManager("../../workspace")
    projects = wm.scan_projects()

    # 验证 LLM 连通性
    validator = LLMValidator(wm.load_llm_config())
    result = validator.test_connectivity("deepseek")
"""

from .manager import WorkspaceManager, ProjectInfo
from .validator import LLMValidator, ToolValidator, ValidationResult

__all__ = [
    "WorkspaceManager",
    "ProjectInfo",
    "LLMValidator",
    "ToolValidator",
    "ValidationResult",
]
