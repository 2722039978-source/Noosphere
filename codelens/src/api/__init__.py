"""
API 模块 - FastAPI Web 服务
"""

from .server import APIServer, create_app
from .routes import setup_routes

__all__ = [
    "APIServer",
    "create_app",
    "setup_routes",
]
