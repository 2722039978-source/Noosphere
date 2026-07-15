"""
API 密钥与配置管理

安全的密钥管理机制：
1. 优先从环境变量读取（生产环境 / 部署后）
2. 其次从 config/.env 文件读取（开发环境）
3. 最后使用 config/.env.example 中的占位符（开源版本）

使用方式:
    from src.config import get_api_key, get_model, get_base_url

    api_key = get_api_key()     # 自动选择安全的获取方式
    model = get_model()          # deepseek-v4-pro
"""

import os
from pathlib import Path
from typing import Optional

# 项目根目录
_PROJECT_ROOT = Path(__file__).parent.parent


def _load_env_file(env_file: str) -> dict:
    """手动解析 .env 文件（零依赖）"""
    result = {}
    env_path = _PROJECT_ROOT / env_file
    if not env_path.exists():
        return result

    with open(env_path, "r", encoding="utf-8") as f:
        for line in f:
            line = line.strip()
            if not line or line.startswith("#"):
                continue
            if "=" in line:
                key, _, value = line.partition("=")
                key = key.strip()
                value = value.strip().strip('"').strip("'")
                result[key] = value
    return result


def _get_setting(key: str, default: str = "") -> str:
    """
    三级安全读取：
    1. 环境变量（最高优先级 — 不会泄露到代码仓库）
    2. config/.env 文件（本地开发，已被 .gitignore 排除）
    3. config/.env.example 模板（开源版本的占位符）
    """
    # Level 1: 环境变量
    value = os.environ.get(key, "")
    if value and "your-api-key" not in value.lower():
        return value

    # Level 2: config/.env
    if not value:
        env_data = _load_env_file("config/.env")
        value = env_data.get(key, "")

    if value and "your-api-key" not in value.lower():
        return value

    # Level 3: config/.env.example 占位符（开源版）
    if not value:
        example_data = _load_env_file("config/.env.example")
        value = example_data.get(key, "")

    return value or default


def get_api_key() -> str:
    """
    获取 DeepSeek API 密钥
    开源后请修改 config/.env 或设置环境变量 DEEPSEEK_API_KEY
    """
    return _get_setting("DEEPSEEK_API_KEY", "")


def get_model() -> str:
    """获取默认模型"""
    return _get_setting("DEEPSEEK_MODEL", "deepseek-v4-pro")


def get_base_url() -> str:
    """获取 API 基础地址"""
    return _get_setting("DEEPSEEK_BASE_URL", "https://api.deepseek.com")


def get_llm_config() -> dict:
    """
    获取完整的 LLM 配置，用于初始化 QA 引擎

    Returns:
        {
            "api_key": "sk-xxx",
            "model": "deepseek-v4-pro",
            "base_url": "https://api.deepseek.com",
            "temperature": 0.1,
            "max_tokens": 4096,
        }
    """
    return {
        "api_key": get_api_key(),
        "model": get_model(),
        "base_url": get_base_url(),
        "temperature": 0.1,
        "max_tokens": 4096,
    }


def is_configured() -> bool:
    """检查 API 密钥是否已正确配置（不是占位符）"""
    key = get_api_key()
    return bool(key) and "your-api-key" not in key.lower() and len(key) > 10


def print_config_status() -> str:
    """打印配置状态（用于 info 命令）"""
    key = get_api_key()
    if not key:
        return "[--] DeepSeek API: 未配置"
    if "your-api-key" in key.lower():
        return "[--] DeepSeek API: 使用占位符，请设置 config/.env 或环境变量"
    masked = key[:8] + "..." + key[-4:]
    return f"[OK] DeepSeek API: {masked} (model: {get_model()})"
