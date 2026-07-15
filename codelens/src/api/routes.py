"""
API 路由定义

CodeLens AI 的 RESTful API 接口：
- 项目索引管理
- 代码分析
- 问答接口
- 知识图谱查询
- Git Diff 分析
- 文档生成
"""

import os
from typing import Optional, List, Dict, Any
from pathlib import Path
from datetime import datetime

from fastapi import APIRouter, HTTPException, Query, WebSocket, WebSocketDisconnect
from pydantic import BaseModel, Field
from loguru import logger

# ---- 请求/响应模型 ----

class IndexRequest(BaseModel):
    project_root: str = Field(default=".", description="项目根目录")
    languages: Optional[List[str]] = Field(default=None, description="要解析的语言")

class QARequest(BaseModel):
    question: str = Field(..., description="问题")
    query_type: str = Field(default="general", description="查询类型")
    target_file: Optional[str] = Field(default=None)
    target_entity: Optional[str] = Field(default=None)

class SearchRequest(BaseModel):
    query: str = Field(..., description="搜索查询")
    top_k: int = Field(default=10, ge=1, le=100)
    language: Optional[str] = None
    entity_type: Optional[str] = None

class DiffAnalysisRequest(BaseModel):
    base_ref: str = Field(default="HEAD", description="基准引用")
    target_ref: str = Field(default="WORKTREE", description="目标引用")
    staged: bool = Field(default=False)

class DocGenerateRequest(BaseModel):
    project_root: str = Field(default=".")
    output_path: str = Field(default="./docs/PROJECT_DOCUMENTATION.md")
    format: str = Field(default="markdown")

class ProjectSummaryResponse(BaseModel):
    project_root: str
    total_files: int
    total_entities: int
    total_relations: int
    languages: List[str]
    kg_stats: Dict[str, Any]

class IndexStatusResponse(BaseModel):
    indexed: bool
    project_root: str
    total_files: int
    total_entities: int
    total_relations: int = 0
    languages: list = []
    index_time_ms: float = 0.0

class ErrorResponse(BaseModel):
    error: str
    detail: Optional[str] = None


# ---- 工作区请求模型 ----

class WorkspaceValidateLLMRequest(BaseModel):
    provider: Optional[str] = Field(default=None, description="要验证的提供商名称（默认全部）")


class WorkspaceValidateToolsRequest(BaseModel):
    services: Optional[List[str]] = Field(default=None, description="要验证的服务列表")


class WorkspaceAnalyzeRequest(BaseModel):
    deep_analysis: bool = Field(default=True, description="是否深度分析")
    learn_style: bool = Field(default=True, description="是否学习代码风格")


def setup_routes(agent_instance) -> APIRouter:
    """
    创建并配置 API 路由

    Args:
        agent_instance: CodeLensAgent 实例

    Returns:
        配置好的 APIRouter
    """
    router = APIRouter(prefix="/api/v1", tags=["CodeLens AI"])

    # ---- 系统路由 ----

    @router.get("/health")
    async def health_check():
        """健康检查"""
        return {
            "status": "ok",
            "version": "1.0.0",
            "agent_initialized": agent_instance._initialized,
        }

    @router.get("/status", response_model=IndexStatusResponse)
    async def get_status():
        """获取系统状态"""
        summary = agent_instance.get_project_summary()
        kg_stats = summary.get("kg_stats", {})
        return IndexStatusResponse(
            indexed=agent_instance._initialized,
            project_root=summary["project_root"],
            total_files=summary.get("total_files", 0),
            total_entities=summary.get("total_entities", 0),
            total_relations=kg_stats.get("total_edges", 0),
            languages=summary.get("languages", []),
            index_time_ms=0,
        )

    # ---- 索引路由 ----

    @router.post("/index")
    async def index_project(request: IndexRequest):
        """索引项目"""
        from ..agent.code_agent import AgentAction

        result = agent_instance.execute(
            AgentAction.INDEX_PROJECT,
            project_root=request.project_root,
        )

        if not result.success:
            raise HTTPException(status_code=500, detail=result.error)

        summary = agent_instance.get_project_summary()
        return {
            "status": "ok",
            "message": "Project indexed successfully",
            **summary,
        }

    # ---- 代码分析路由 ----

    @router.post("/analyze/file")
    async def analyze_file(file_path: str = Query(..., description="文件路径")):
        """分析单个文件"""
        from ..agent.code_agent import AgentAction

        result = agent_instance.execute(
            AgentAction.ANALYZE_FILE,
            file_path=file_path,
        )

        if not result.success:
            raise HTTPException(status_code=500, detail=result.error)

        return result.data

    @router.post("/analyze/explain")
    async def explain_code(
        code: str = Query(..., description="代码内容"),
        language: str = Query("python", description="编程语言"),
    ):
        """解释代码"""
        from ..agent.code_agent import AgentAction

        result = agent_instance.execute(
            AgentAction.EXPLAIN_CODE,
            code=code,
            language=language,
        )

        if not result.success:
            raise HTTPException(status_code=500, detail=result.error)

        return {"explanation": result.data}

    # ---- 问答路由 ----

    @router.post("/qa")
    async def ask_question(request: QARequest):
        """代码库问答"""
        try:
            response = agent_instance.ask(
                request.question,
                query_type=request.query_type,
                target_file=request.target_file,
                target_entity=request.target_entity,
            )
            return response.to_dict()
        except Exception as e:
            raise HTTPException(status_code=500, detail=str(e))

    # ---- 搜索路由 ----

    @router.post("/search")
    async def search_codebase(request: SearchRequest):
        """搜索代码库"""
        from ..agent.code_agent import AgentAction

        result = agent_instance.execute(
            AgentAction.SEARCH_CODEBASE,
            query=request.query,
            top_k=request.top_k,
        )

        if not result.success:
            raise HTTPException(status_code=500, detail=result.error)

        return {"results": result.data, "query": request.query}

    # ---- 调用链路由 ----

    @router.get("/call-chain/{entity_name}")
    async def get_call_chain(
        entity_name: str,
        max_depth: int = Query(default=10, ge=1, le=50),
    ):
        """获取函数调用链"""
        from ..agent.code_agent import AgentAction

        result = agent_instance.execute(
            AgentAction.TRACE_CALLS,
            entity_name=entity_name,
            max_depth=max_depth,
        )

        if not result.success:
            raise HTTPException(status_code=500, detail=result.error)

        return result.data

    # ---- 影响分析路由 ----

    @router.get("/impact/{entity_name}")
    async def analyze_impact(entity_name: str):
        """影响分析"""
        from ..agent.code_agent import AgentAction

        result = agent_instance.execute(
            AgentAction.ANALYZE_IMPACT,
            entity_name=entity_name,
        )

        if not result.success:
            raise HTTPException(status_code=500, detail=result.error)

        return result.data

    # ---- Git Diff 路由 ----

    @router.post("/git/diff")
    async def analyze_git_diff(request: DiffAnalysisRequest):
        """分析 Git Diff"""
        from ..agent.git_diff_analyzer import GitDiffAnalyzer

        kg = agent_instance.get_knowledge_graph()
        analyzer = GitDiffAnalyzer(
            parser=agent_instance.parser,
            extractor=agent_instance.extractor,
            knowledge_graph=kg,
            repo_path=agent_instance.config.project_root,
        )

        if request.staged:
            analysis = analyzer.analyze_staged()
        elif request.target_ref == "WORKTREE":
            analysis = analyzer.analyze_unstaged()
        else:
            analysis = analyzer.analyze_branches(request.base_ref, request.target_ref)

        return analysis.to_dict()

    # ---- 文档生成路由 ----

    @router.post("/docs/generate")
    async def generate_docs(request: DocGenerateRequest):
        """生成项目文档"""
        from ..agent.code_agent import AgentAction

        result = agent_instance.execute(
            AgentAction.GENERATE_DOCS,
        )

        if not result.success:
            raise HTTPException(status_code=500, detail=result.error)

        # 保存文档
        output_path = request.output_path
        Path(output_path).parent.mkdir(parents=True, exist_ok=True)
        Path(output_path).write_text(result.data, encoding="utf-8")

        return {
            "status": "ok",
            "output_path": output_path,
            "message": "Documentation generated successfully",
        }

    # ---- 知识图谱路由 ----

    @router.get("/knowledge-graph/stats")
    async def get_kg_stats():
        """获取知识图谱统计"""
        kg = agent_instance.get_knowledge_graph()
        if kg is None:
            raise HTTPException(status_code=400, detail="Knowledge graph not initialized")

        return kg.stats

    @router.get("/knowledge-graph/node/{name}")
    async def get_kg_node(name: str):
        """获取知识图谱节点"""
        kg = agent_instance.get_knowledge_graph()
        if kg is None:
            raise HTTPException(status_code=400, detail="Knowledge graph not initialized")

        node = kg.get_node(name)
        if node is None:
            raise HTTPException(status_code=404, detail=f"Node '{name}' not found")

        return node.to_dict()

    @router.get("/knowledge-graph/export")
    async def export_kg(format: str = Query("json", pattern="^(json|html)$")):
        """导出知识图谱"""
        kg = agent_instance.get_knowledge_graph()
        if kg is None:
            raise HTTPException(status_code=400, detail="Knowledge graph not initialized")

        import tempfile
        import os

        suffix = ".json" if format == "json" else ".html"
        with tempfile.NamedTemporaryFile(suffix=suffix, delete=False, mode="w") as f:
            if format == "json":
                f.write(kg.to_json())
            else:
                kg.to_html(f.name)
            return {"export_path": f.name, "format": format}

    # ---- 代码问题检测路由 ----

    @router.get("/issues")
    async def find_issues(file_path: Optional[str] = Query(default=None)):
        """查找代码问题"""
        from ..agent.code_agent import AgentAction

        result = agent_instance.execute(
            AgentAction.FIND_BUGS,
            file_path=file_path,
        )

        if not result.success:
            raise HTTPException(status_code=500, detail=result.error)

        return {"issues": result.data, "count": len(result.data)}

    # ---- 工作区管理路由 ----

    @router.post("/workspace/scan")
    async def scan_workspace():
        """扫描 workspace 目录，发现所有项目"""
        try:
            from ..workspace.manager import WorkspaceManager

            # workspace 目录相对于 codelens 项目根
            codelens_root = Path(__file__).parent.parent.parent
            workspace_root = os.environ.get(
                "WORKSPACE_DIR",
                str(codelens_root.parent / "workspace"),
            )

            wm = WorkspaceManager(workspace_root)
            projects = wm.scan_projects()

            return {
                "status": "ok",
                "workspace_root": workspace_root,
                "projects": [p.to_dict() for p in projects],
                "count": len(projects),
            }
        except Exception as e:
            raise HTTPException(status_code=500, detail=str(e))

    @router.get("/workspace/projects")
    async def list_projects():
        """列出 workspace 中所有已发现的项目"""
        try:
            from ..workspace.manager import WorkspaceManager

            codelens_root = Path(__file__).parent.parent.parent
            workspace_root = os.environ.get(
                "WORKSPACE_DIR",
                str(codelens_root.parent / "workspace"),
            )

            wm = WorkspaceManager(workspace_root)

            # 优先返回缓存的状态
            state = wm.load_state()
            if state and state.get("projects"):
                return {
                    "status": "ok",
                    "workspace_root": workspace_root,
                    "projects": state["projects"],
                    "count": len(state["projects"]),
                    "cached": True,
                    "updated_at": state.get("updated_at"),
                }

            # 没有缓存则实时扫描
            projects = wm.scan_projects()
            return {
                "status": "ok",
                "workspace_root": workspace_root,
                "projects": [p.to_dict() for p in projects],
                "count": len(projects),
                "cached": False,
            }
        except Exception as e:
            raise HTTPException(status_code=500, detail=str(e))

    @router.get("/workspace/projects/{project_name}")
    async def get_project(project_name: str):
        """获取单个项目的详细信息"""
        try:
            from ..workspace.manager import WorkspaceManager

            codelens_root = Path(__file__).parent.parent.parent
            workspace_root = os.environ.get(
                "WORKSPACE_DIR",
                str(codelens_root.parent / "workspace"),
            )

            wm = WorkspaceManager(workspace_root)
            project = wm.get_project(project_name)

            if project is None:
                raise HTTPException(status_code=404, detail=f"项目 '{project_name}' 不存在")

            return {
                "status": "ok",
                "project": project.to_dict(),
            }
        except HTTPException:
            raise
        except Exception as e:
            raise HTTPException(status_code=500, detail=str(e))

    @router.post("/workspace/projects/{project_name}/analyze")
    async def analyze_project(project_name: str, request: WorkspaceAnalyzeRequest):
        """分析指定的项目（索引到 CodeLens + 学习风格）"""
        try:
            from ..workspace.manager import WorkspaceManager
            from ..agent.code_agent import AgentAction

            codelens_root = Path(__file__).parent.parent.parent
            workspace_root = os.environ.get(
                "WORKSPACE_DIR",
                str(codelens_root.parent / "workspace"),
            )

            wm = WorkspaceManager(workspace_root)
            project = wm.get_project(project_name)

            if project is None:
                raise HTTPException(status_code=404, detail=f"项目 '{project_name}' 不存在")

            # 索引项目到 CodeLens
            logger.info(f"开始分析项目: {project_name} ({project.path})")
            result = agent_instance.execute(
                AgentAction.INDEX_PROJECT,
                project_root=project.path,
            )

            if not result.success:
                logger.error(f"项目分析失败: {result.error}")
                raise HTTPException(status_code=500, detail=result.error)

            # 获取分析后的摘要
            summary = agent_instance.get_project_summary()

            # 更新项目状态
            project.scan_status = "done"
            project.last_scanned = datetime.now().isoformat()

            return {
                "status": "ok",
                "project": project.to_dict(),
                "analysis_summary": summary,
                "analysis_time_ms": result.execution_time_ms,
                "message": f"项目 '{project_name}' 分析完成",
            }
        except HTTPException:
            raise
        except Exception as e:
            raise HTTPException(status_code=500, detail=str(e))

    # ---- 验证与诊断路由 ----

    @router.post("/workspace/validate/llm")
    async def validate_llm(request: WorkspaceValidateLLMRequest):
        """验证 LLM 连通性"""
        try:
            from ..workspace.manager import WorkspaceManager
            from ..workspace.validator import LLMValidator

            codelens_root = Path(__file__).parent.parent.parent
            workspace_root = os.environ.get(
                "WORKSPACE_DIR",
                str(codelens_root.parent / "workspace"),
            )

            wm = WorkspaceManager(workspace_root)
            llm_config = wm.load_llm_config()
            validator = LLMValidator()

            if request.provider:
                # 测试指定的提供商
                provider_config = wm.get_provider_config(request.provider)
                if provider_config is None:
                    raise HTTPException(
                        status_code=404,
                        detail=f"提供商 '{request.provider}' 未配置或未启用",
                    )
                result = validator.test_connectivity(provider_config)
                return {
                    "single_test": True,
                    "result": result.to_dict(),
                }
            else:
                # 测试所有提供商
                report = validator.test_all_providers(llm_config)
                return report.to_dict()

        except HTTPException:
            raise
        except Exception as e:
            raise HTTPException(status_code=500, detail=str(e))

    @router.post("/workspace/validate/tools")
    async def validate_tools(request: WorkspaceValidateToolsRequest):
        """验证各服务工具链的输入/输出"""
        try:
            from ..workspace.validator import ToolValidator

            nebula_url = os.environ.get("NEBULA_URL", "http://localhost:8730")
            devops_url = os.environ.get("DEVOPS_URL", "http://localhost:8740")
            codelens_url = os.environ.get("CODELENS_URL", "http://localhost:8765")

            validator = ToolValidator(
                nebula_url=nebula_url,
                devops_url=devops_url,
                codelens_url=codelens_url,
            )

            services = request.services if request.services else None
            report = validator.validate_all(services)
            return report.to_dict()

        except Exception as e:
            raise HTTPException(status_code=500, detail=str(e))

    @router.post("/workspace/validate/all")
    async def validate_all():
        """运行全部验证：LLM + 工具链 + 端到端"""
        try:
            from ..workspace.manager import WorkspaceManager
            from ..workspace.validator import LLMValidator, ToolValidator, EndToEndValidator

            codelens_root = Path(__file__).parent.parent.parent
            workspace_root = os.environ.get(
                "WORKSPACE_DIR",
                str(codelens_root.parent / "workspace"),
            )

            all_results = []
            overall = "ok"

            # 1. LLM 验证
            wm = WorkspaceManager(workspace_root)
            llm_config = wm.load_llm_config()
            llm_validator = LLMValidator()
            llm_report = llm_validator.test_all_providers(llm_config)
            all_results.extend(llm_report.results)
            if llm_report.overall_status != "ok":
                overall = "partial"

            # 2. 工具链验证
            tool_validator = ToolValidator()
            tool_report = tool_validator.validate_all()
            all_results.extend(tool_report.results)
            if tool_report.overall_status == "failed":
                overall = "partial"

            # 3. 端到端验证
            e2e_validator = EndToEndValidator(agent_instance)
            e2e_report = e2e_validator.run_e2e()
            all_results.extend(e2e_report.results)

            # 汇总
            passed = sum(1 for r in all_results if r.passed)
            total = len(all_results)

            if passed == total:
                overall = "ok"
            elif passed == 0:
                overall = "failed"

            return {
                "timestamp": datetime.now().isoformat(),
                "overall_status": overall,
                "total_tests": total,
                "passed_tests": passed,
                "failed_tests": total - passed,
                "llm_report": llm_report.to_dict(),
                "tool_report": tool_report.to_dict(),
                "e2e_report": e2e_report.to_dict(),
                "summary": f"{passed}/{total} 项验证通过",
            }

        except Exception as e:
            raise HTTPException(status_code=500, detail=str(e))

    @router.get("/workspace/config/llm")
    async def get_llm_config_status():
        """获取 LLM 配置状态"""
        try:
            from ..workspace.manager import WorkspaceManager

            codelens_root = Path(__file__).parent.parent.parent
            workspace_root = os.environ.get(
                "WORKSPACE_DIR",
                str(codelens_root.parent / "workspace"),
            )

            wm = WorkspaceManager(workspace_root)
            llm_config = wm.load_llm_config()

            # 返回配置摘要（隐藏 API Key）
            providers_summary = []
            for p in llm_config.get("providers", []):
                key = p.get("api_key", "")
                masked_key = ""
                if key and "your-api-key" not in key.lower():
                    masked_key = key[:8] + "..." + key[-4:] if len(key) > 12 else "***"

                providers_summary.append({
                    "name": p.get("name"),
                    "type": p.get("type"),
                    "model": p.get("model"),
                    "base_url": p.get("base_url"),
                    "enabled": p.get("enabled", True),
                    "api_key_configured": bool(key and "your-api-key" not in key.lower() and len(key) > 10),
                    "api_key_masked": masked_key,
                })

            return {
                "default_provider": llm_config.get("default_provider"),
                "providers": providers_summary,
                "any_configured": wm.is_llm_configured(),
            }

        except Exception as e:
            raise HTTPException(status_code=500, detail=str(e))

    # ---- WebSocket 路由 ----

    @router.websocket("/ws/chat")
    async def websocket_chat(websocket: WebSocket):
        """WebSocket 实时问答"""
        await websocket.accept()
        logger.info("WebSocket 连接已建立")

        try:
            while True:
                data = await websocket.receive_json()

                question = data.get("question", "")
                if not question:
                    await websocket.send_json({"error": "Question is required"})
                    continue

                try:
                    response = agent_instance.ask(question)
                    await websocket.send_json(response.to_dict())
                except Exception as e:
                    await websocket.send_json({"error": str(e)})

        except WebSocketDisconnect:
            logger.info("WebSocket 连接已断开")
        except Exception as e:
            logger.error(f"WebSocket 错误: {e}")

    return router
