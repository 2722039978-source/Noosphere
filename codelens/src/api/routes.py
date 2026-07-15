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

from typing import Optional, List, Dict, Any
from pathlib import Path

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
