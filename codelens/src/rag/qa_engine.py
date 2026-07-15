"""
代码问答引擎

基于 RAG 架构的代码库问答系统：
1. 检索阶段：从知识图谱和向量存储中检索相关代码
2. 增强阶段：构建包含代码上下文的增强提示
3. 生成阶段：调用 LLM 生成回答

支持：
- 代码库结构问答
- 函数调用链解释
- 问题定位和诊断
- 最佳实践建议
"""

import os
import json
import time
from typing import Optional, Dict, Any, List, Tuple, Callable
from dataclasses import dataclass, field
from enum import Enum

from loguru import logger

from .retriever import CodeRetriever, RetrievalResult, SearchStrategy
from ..indexer.knowledge_graph import KnowledgeGraph


class QueryType(Enum):
    """查询类型"""
    CODE_EXPLANATION = "code_explanation"       # 代码解释
    CALL_CHAIN = "call_chain"                   # 调用链分析
    PROJECT_STRUCTURE = "project_structure"     # 项目结构
    BUG_LOCATION = "bug_location"               # 问题定位
    BEST_PRACTICE = "best_practice"             # 最佳实践
    IMPACT_ANALYSIS = "impact_analysis"         # 影响分析
    DOC_GENERATION = "doc_generation"           # 文档生成
    GENERAL = "general"                         # 通用问答


@dataclass
class QAQuery:
    """问答查询"""
    question: str
    query_type: QueryType = QueryType.GENERAL
    target_file: Optional[str] = None
    target_entity: Optional[str] = None
    context: Optional[str] = None          # 额外的上下文
    max_tokens: int = 2048
    temperature: float = 0.1

    def to_dict(self) -> Dict[str, Any]:
        return {
            "question": self.question,
            "query_type": self.query_type.value,
            "target_file": self.target_file,
            "target_entity": self.target_entity,
        }


@dataclass
class QAResponse:
    """问答响应"""
    question: str
    answer: str
    sources: List[Dict[str, Any]] = field(default_factory=list)
    retrieval_time_ms: float = 0.0
    generation_time_ms: float = 0.0
    tokens_used: int = 0
    confidence: float = 1.0
    query_type: str = "general"
    follow_up_questions: List[str] = field(default_factory=list)

    def to_dict(self) -> Dict[str, Any]:
        return {
            "question": self.question,
            "answer": self.answer,
            "sources": self.sources,
            "retrieval_time_ms": self.retrieval_time_ms,
            "generation_time_ms": self.generation_time_ms,
            "tokens_used": self.tokens_used,
            "confidence": self.confidence,
            "query_type": self.query_type,
            "follow_up_questions": self.follow_up_questions,
        }


class QACodeEngine:
    """
    代码问答引擎

    基于 RAG 架构，结合代码检索和 LLM 实现智能代码问答。

    工作流程：
    1. 分析查询意图，确定查询类型
    2. 执行多策略检索，获取相关代码上下文
    3. 构建增强提示（包含代码上下文和知识图谱信息）
    4. 调用 LLM 生成回答
    5. 返回带来源引用的结构化回答

    使用示例:
        engine = QACodeEngine(retriever, knowledge_graph, llm_client)
        response = engine.ask(QAQuery(
            question="the authentication flow in this project?",
            query_type=QueryType.CALL_CHAIN,
        ))
        print(response.answer)
        for source in response.sources:
            print(f"  Source: {source['file']}:{source['line']}")
    """

    # 查询类型识别模板
    QUERY_PATTERNS = {
        QueryType.CALL_CHAIN: [
            "how does.*work", "what happens when", "call chain",
            "call flow", "execution flow", "调用链", "执行流程",
            "trace", "what calls what", "who calls",
        ],
        QueryType.PROJECT_STRUCTURE: [
            "project structure", "architecture", "how is.*organized",
            "directory structure", "模块结构", "项目结构", "架构",
            "what modules", "components of",
        ],
        QueryType.BUG_LOCATION: [
            "bug", "error", "issue", "problem", "fix", "broken",
            "why does.*fail", "what causes", "crash", "exception",
            "问题", "错误", "bug", "修复",
        ],
        QueryType.CODE_EXPLANATION: [
            "explain", "what does.*do", "how does.*function",
            "describe", "理解", "解释", "说明",
        ],
        QueryType.BEST_PRACTICE: [
            "best practice", "optimize", "improve", "refactor",
            "suggestion", "recommend", "最佳实践", "优化", "改进",
        ],
        QueryType.IMPACT_ANALYSIS: [
            "impact", "what will.*affect", "what depends on",
            "change impact", "影响", "依赖", "改动",
        ],
    }

    def __init__(
        self,
        retriever: CodeRetriever,
        knowledge_graph: KnowledgeGraph,
        llm_client: Optional[Any] = None,
        system_prompt: Optional[str] = None,
    ):
        """
        初始化问答引擎

        Args:
            retriever: 代码检索器
            knowledge_graph: 知识图谱
            llm_client: LLM 客户端（OpenAI、Anthropic 等）
            system_prompt: 自定义系统提示
        """
        self.retriever = retriever
        self.kg = knowledge_graph
        self.llm_client = llm_client
        self.system_prompt = system_prompt or self._default_system_prompt()
        self._conversation_history: List[Dict[str, str]] = []

    def ask(self, query: QAQuery) -> QAResponse:
        """
        执行代码问答

        Args:
            query: 问答查询

        Returns:
            问答响应
        """
        # 1. 识别查询类型（如果未指定）
        if query.query_type == QueryType.GENERAL:
            query.query_type = self._classify_query(query.question)

        logger.info(f"处理查询 [{query.query_type.value}]: {query.question[:100]}...")

        # 2. 检索相关代码
        retrieval_start = time.time()
        retrieval_results = self._retrieve_for_query(query)
        retrieval_time = (time.time() - retrieval_start) * 1000

        logger.debug(f"检索到 {len(retrieval_results)} 个结果 ({retrieval_time:.0f}ms)")

        # 3. 构建增强提示
        augmented_prompt = self._build_augmented_prompt(query, retrieval_results)

        # 4. 调用 LLM 生成回答
        generation_start = time.time()
        answer, tokens = self._generate_answer(augmented_prompt, query)
        generation_time = (time.time() - generation_start) * 1000

        # 5. 构建响应
        sources = [
            {
                "file": r.file_path,
                "line": r.line_number,
                "entity": r.entity_name,
                "type": r.entity_type,
                "source": r.source,
                "score": r.score,
            }
            for r in retrieval_results[:10]
        ]

        response = QAResponse(
            question=query.question,
            answer=answer,
            sources=sources,
            retrieval_time_ms=retrieval_time,
            generation_time_ms=generation_time,
            tokens_used=tokens,
            query_type=query.query_type.value,
            follow_up_questions=self._generate_follow_up_questions(query, answer),
        )

        # 保存对话历史
        self._conversation_history.append({
            "role": "user",
            "content": query.question,
        })
        self._conversation_history.append({
            "role": "assistant",
            "content": answer,
        })

        return response

    def ask_with_stream(self, query: QAQuery) -> Any:
        """
        流式问答（返回生成器）

        Args:
            query: 问答查询

        Yields:
            文本块
        """
        retrieval_results = self._retrieve_for_query(query)
        augmented_prompt = self._build_augmented_prompt(query, retrieval_results)

        if self.llm_client and hasattr(self.llm_client, 'stream'):
            for chunk in self.llm_client.stream(augmented_prompt):
                yield chunk
        else:
            response = self.ask(query)
            yield response.answer

    # ---- 检索方法 ----

    def _retrieve_for_query(self, query: QAQuery) -> List[RetrievalResult]:
        """根据查询类型执行不同的检索策略"""
        strategy_map = {
            QueryType.CALL_CHAIN: SearchStrategy.HYBRID,
            QueryType.PROJECT_STRUCTURE: SearchStrategy.STRUCTURAL,
            QueryType.CODE_EXPLANATION: SearchStrategy.SEMANTIC,
            QueryType.BUG_LOCATION: SearchStrategy.HYBRID,
            QueryType.BEST_PRACTICE: SearchStrategy.SEMANTIC,
            QueryType.IMPACT_ANALYSIS: SearchStrategy.STRUCTURAL,
            QueryType.GENERAL: SearchStrategy.HYBRID,
        }

        strategy = strategy_map.get(query.query_type, SearchStrategy.HYBRID)
        top_k = 15 if query.query_type == QueryType.CALL_CHAIN else 10

        results = self.retriever.retrieve(
            query.question,
            strategy=strategy,
            top_k=top_k,
            file_filter=query.target_file,
        )

        # 如果指定了目标实体，额外获取其调用链
        if query.target_entity:
            entity_results = self.retriever.retrieve_for_entity(
                query.target_entity,
                include_related=(query.query_type == QueryType.CALL_CHAIN),
            )
            # 将实体结果放在前面
            results = entity_results + results

        # 对于调用链查询，添加调用链上下文
        if query.query_type == QueryType.CALL_CHAIN and query.target_entity:
            call_chain_context = self.retriever.retrieve_call_chain_context(
                query.target_entity, max_depth=5
            )
            if call_chain_context:
                results.insert(0, RetrievalResult(
                    content=call_chain_context,
                    score=1.0,
                    source="structural",
                    entity_name=query.target_entity,
                    entity_type="call_chain",
                ))

        return results

    # ---- 提示构建 ----

    def _build_augmented_prompt(
        self,
        query: QAQuery,
        retrieval_results: List[RetrievalResult],
    ) -> str:
        """构建增强提示"""
        parts = []

        # 系统提示
        parts.append(self.system_prompt)

        # 检索到的代码上下文
        if retrieval_results:
            parts.append("\n\n## Retrieved Code Context\n")
            parts.append("The following code sections are relevant to the query:\n")

            total_length = 0
            for i, result in enumerate(retrieval_results):
                context_str = result.to_prompt_context()
                if total_length + len(context_str) > 8000:  # 限制总上下文长度
                    break
                parts.append(f"\n### Source {i + 1}: {result.entity_name or result.file_path}")
                parts.append(f"```\n{context_str}\n```")
                total_length += len(context_str)

        # 项目统计信息
        if self.kg:
            stats = self.kg.stats
            parts.append(f"\n\n## Project Context\n")
            parts.append(f"- Total entities in knowledge graph: {stats.get('total_nodes', 0)}")
            parts.append(f"- Total relationships: {stats.get('total_edges', 0)}")

        # 用户的查询
        parts.append(f"\n\n## Question\n{query.question}")

        # 额外的指令
        parts.append("\n\n## Instructions\n")
        parts.append("Please answer the question based on the provided code context.")
        parts.append("When referencing code, cite the file paths and line numbers.")
        parts.append("If the context is insufficient, explain what additional information would be needed.")
        parts.append("Be specific and technical in your response.")

        if query.query_type == QueryType.CALL_CHAIN:
            parts.append("Focus on explaining the execution flow and call relationships.")
        elif query.query_type == QueryType.BUG_LOCATION:
            parts.append("Help identify potential bug locations and explain why they might cause issues.")
        elif query.query_type == QueryType.IMPACT_ANALYSIS:
            parts.append("Analyze the potential impact of changes to the specified code.")

        return "\n".join(parts)

    # ---- LLM 调用 ----

    def _generate_answer(self, prompt: str, query: QAQuery) -> Tuple[str, int]:
        """调用 LLM 生成回答（优先 DeepSeek，其次外部客户端，最后内置规则）"""
        # 优先级 1：DeepSeek API（内置支持）
        try:
            from ..config import get_api_key, get_model, get_base_url, is_configured
            if is_configured():
                return self._call_deepseek(prompt, query)
        except ImportError:
            pass

        # 优先级 2：外部 LLM 客户端
        if self.llm_client:
            return self._call_external_llm(prompt, query)

        # 优先级 3：内置简单回答
        return self._generate_simple_answer(prompt, query), 0

    def _call_deepseek(self, prompt: str, query: QAQuery) -> Tuple[str, int]:
        """调用 DeepSeek API（OpenAI 兼容协议）"""
        import json
        import urllib.request
        import urllib.error

        from ..config import get_api_key, get_model, get_base_url

        api_key = get_api_key()
        model = get_model()
        base_url = get_base_url().rstrip("/")

        messages = [
            {"role": "system", "content": self.system_prompt},
        ]
        for msg in self._conversation_history[-6:]:
            messages.append({"role": msg["role"], "content": msg["content"]})
        messages.append({"role": "user", "content": prompt})

        body = json.dumps({
            "model": model,
            "messages": messages,
            "max_tokens": query.max_tokens,
            "temperature": query.temperature,
            "stream": False,
        }).encode("utf-8")

        req = urllib.request.Request(
            f"{base_url}/chat/completions",
            data=body,
            headers={
                "Content-Type": "application/json",
                "Authorization": f"Bearer {api_key}",
            },
        )

        try:
            with urllib.request.urlopen(req, timeout=120) as resp:
                data = json.loads(resp.read().decode("utf-8"))
                answer = data["choices"][0]["message"]["content"]
                tokens = data.get("usage", {}).get("total_tokens", 0)
                return answer, tokens
        except urllib.error.HTTPError as e:
            error_body = e.read().decode("utf-8") if e.fp else str(e)
            logger.error(f"DeepSeek API 错误 ({e.code}): {error_body}")
            # 401/403 等认证错误时，回退到简单回答
        except Exception as e:
            logger.error(f"DeepSeek 调用失败: {e}")

        # 回退
        return self._generate_simple_answer(prompt, query), 0

    def _call_external_llm(self, prompt: str, query: QAQuery) -> Tuple[str, int]:
        """调用外部 LLM（OpenAI SDK / Anthropic SDK / 回调函数）"""
        try:
            if callable(self.llm_client):
                answer = self.llm_client(prompt)
                return answer, 0

            if hasattr(self.llm_client, 'chat'):
                if hasattr(self.llm_client.chat, 'completions'):
                    messages = [{"role": "system", "content": self.system_prompt}]
                    for msg in self._conversation_history[-6:]:
                        messages.append(msg)
                    messages.append({"role": "user", "content": prompt})
                    response = self.llm_client.chat.completions.create(
                        model=os.environ.get("OPENAI_MODEL", "gpt-4o"),
                        messages=messages,
                        max_tokens=query.max_tokens,
                        temperature=query.temperature,
                    )
                    answer = response.choices[0].message.content
                    tokens = response.usage.total_tokens
                    return answer, tokens
                else:
                    response = self.llm_client.messages.create(
                        model=os.environ.get("ANTHROPIC_MODEL", "claude-sonnet-5"),
                        max_tokens=query.max_tokens,
                        system=self.system_prompt,
                        messages=[{"role": "user", "content": prompt}],
                    )
                    answer = response.content[0].text
                    tokens = response.usage.input_tokens + response.usage.output_tokens
                    return answer, tokens

        except Exception as e:
            logger.error(f"LLM 调用失败: {e}")

        return self._generate_simple_answer(prompt, query), 0

    def _generate_simple_answer(self, prompt: str, query: QAQuery) -> str:
        """
        简单的规则回答（无需 LLM）

        在没有 LLM 客户端时使用，基于检索结果构建结构化的回答。
        """
        # 从 prompt 中提取检索到的代码上下文
        parts = []

        parts.append(f"# Answer to: {query.question}\n")

        if query.query_type == QueryType.PROJECT_STRUCTURE:
            parts.append(self._build_structure_answer())
        elif query.query_type == QueryType.CALL_CHAIN:
            parts.append(self._build_call_chain_answer(query))
        elif query.query_type == QueryType.IMPACT_ANALYSIS:
            parts.append(self._build_impact_answer(query))
        else:
            parts.append(self._build_general_answer(query))

        return "\n".join(parts)

    def _build_structure_answer(self) -> str:
        """构建项目结构回答"""
        stats = self.kg.stats
        return (
            f"## Project Structure Overview\n\n"
            f"The project knowledge graph contains:\n"
            f"- **{stats.get('total_nodes', 0)}** code entities\n"
            f"- **{stats.get('total_edges', 0)}** relationships\n"
            f"- **{stats.get('files_indexed', 0)}** indexed files\n\n"
            f"### Entity Distribution:\n" +
            "\n".join(
                f"- {etype}: {count}"
                for etype, count in stats.get("type_distribution", {}).items()
            )
        )

    def _build_call_chain_answer(self, query: QAQuery) -> str:
        """构建调用链回答"""
        if not query.target_entity:
            return "Please specify a target function/entity for call chain analysis."

        callers = self.kg.get_callers(query.target_entity)
        callees = self.kg.get_callees(query.target_entity)

        parts = [f"## Call Analysis for `{query.target_entity}`\n"]

        if callers:
            parts.append(f"### Called by ({len(callers)} functions):")
            for caller in callers[:20]:
                parts.append(f"- `{caller.name}` ({caller.file_path}:{caller.start_line})")

        if callees:
            parts.append(f"\n### Calls ({len(callees)} functions):")
            for callee in callees[:20]:
                parts.append(f"- `{callee.name}` ({callee.file_path}:{callee.start_line})")

        return "\n".join(parts)

    def _build_impact_answer(self, query: QAQuery) -> str:
        """构建影响分析回答"""
        if not query.target_entity:
            return "Please specify a target entity for impact analysis."

        impact = self.kg.get_impact_analysis(query.target_entity)

        parts = [f"## Impact Analysis for `{query.target_entity}`\n"]

        if impact["direct"]:
            parts.append(f"### Directly Affected ({len(impact['direct'])} entities):")
            for item in impact["direct"]:
                parts.append(f"- `{item['name']}` ({item['type']}) in {item['file']}")

        if impact["indirect"]:
            parts.append(f"\n### Indirectly Affected ({len(impact['indirect'])} entities):")
            for item in impact["indirect"][:20]:
                parts.append(f"- `{item['name']}` ({item['type']}) in {item['file']} (depth {item['depth']})")

        parts.append(f"\n**Total impacted entities: {impact['total']}**")

        return "\n".join(parts)

    def _build_general_answer(self, query: QAQuery) -> str:
        """构建通用回答"""
        return (
            f"Based on the codebase analysis, here is the relevant information for your query.\n\n"
            f"*Note: For more detailed answers, configure an LLM client "
            f"(OpenAI or Anthropic) by setting the appropriate API key.*\n\n"
            f"Query type: {query.query_type.value}\n"
            f"Knowledge graph contains {self.kg.stats.get('total_nodes', 0)} entities "
            f"across {self.kg.stats.get('files_indexed', 0)} files."
        )

    # ---- 辅助方法 ----

    def _classify_query(self, question: str) -> QueryType:
        """自动识别查询类型"""
        import re
        question_lower = question.lower()

        for query_type, patterns in self.QUERY_PATTERNS.items():
            for pattern in patterns:
                if re.search(pattern, question_lower):
                    return query_type

        return QueryType.GENERAL

    def _generate_follow_up_questions(self, query: QAQuery, answer: str) -> List[str]:
        """生成建议的跟进问题"""
        follow_ups = []

        if query.query_type == QueryType.CALL_CHAIN:
            follow_ups.append("What are the main entry points for this flow?")
            follow_ups.append("Are there any error handling paths I should be aware of?")
        elif query.query_type == QueryType.PROJECT_STRUCTURE:
            follow_ups.append("Which modules have the most dependencies?")
            follow_ups.append("Where is the core business logic located?")
        elif query.query_type == QueryType.BUG_LOCATION:
            follow_ups.append("What tests cover this area?")
            follow_ups.append("When was this code last modified?")
        else:
            follow_ups.append("Can you show me the detailed implementation?")
            follow_ups.append("What are the related components?")

        return follow_ups

    def clear_history(self):
        """清除对话历史"""
        self._conversation_history = []

    @staticmethod
    def _default_system_prompt() -> str:
        """默认系统提示"""
        return (
            "You are CodeLens AI, an expert code analysis assistant. "
            "You help developers understand, navigate, and improve their codebases.\n\n"
            "Your capabilities:\n"
            "- Analyze code structure and architecture\n"
            "- Explain function call chains and execution flows\n"
            "- Identify potential bugs and issues\n"
            "- Suggest best practices and improvements\n"
            "- Generate documentation from code\n\n"
            "Always reference specific files and line numbers when discussing code. "
            "Be precise, technical, and helpful."
        )

    def set_llm_client(self, client: Any):
        """设置 LLM 客户端"""
        self.llm_client = client
        logger.info("LLM 客户端已配置")
