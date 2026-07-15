"""
Git Diff 分析 Agent

分析 Git 代码变更，实现：
- 变更代码的 AST 差异分析
- 受影响实体识别
- 调用链影响追踪
- 变更风险评级
- 变更摘要生成

支持分析 unstaged changes, staged changes, 和任意两个 commit 之间的差异。
"""

import os
import re
from pathlib import Path
from typing import Optional, Dict, Any, List, Set, Tuple
from dataclasses import dataclass, field
from enum import Enum

from loguru import logger

from ..parser.tree_sitter_parser import TreeSitterParser, Language, ParseResult
from ..parser.ast_extractor import ASTExtractor, CodeEntity, Relation, EntityType, RelationType
from ..indexer.knowledge_graph import KnowledgeGraph


class ChangeType(Enum):
    """变更类型"""
    ADDED = "added"
    MODIFIED = "modified"
    DELETED = "deleted"
    RENAMED = "renamed"


class RiskLevel(Enum):
    """风险等级"""
    LOW = "low"
    MEDIUM = "medium"
    HIGH = "high"
    CRITICAL = "critical"


@dataclass
class ChangedEntity:
    """变更的代码实体"""
    name: str
    entity_type: EntityType
    change_type: ChangeType
    file_path: str
    old_lines: str = ""
    new_lines: str = ""
    line_start: int = 0
    line_end: int = 0
    diff_context: str = ""

    def to_dict(self) -> Dict[str, Any]:
        return {
            "name": self.name,
            "entity_type": self.entity_type.value,
            "change_type": self.change_type.value,
            "file_path": self.file_path,
            "line_start": self.line_start,
            "line_end": self.line_end,
        }


@dataclass
class ChangeImpact:
    """变更影响"""
    entity: ChangedEntity
    risk_level: RiskLevel
    affected_entities: List[Dict[str, Any]] = field(default_factory=list)
    call_chain_impact: List[List[str]] = field(default_factory=list)
    reasoning: str = ""

    def to_dict(self) -> Dict[str, Any]:
        return {
            "entity": self.entity.to_dict(),
            "risk_level": self.risk_level.value,
            "affected_count": len(self.affected_entities),
            "affected_entities": self.affected_entities[:20],
            "call_chain_impact": self.call_chain_impact[:5],
            "reasoning": self.reasoning,
        }


@dataclass
class DiffAnalysis:
    """Git Diff 分析结果"""
    repo_path: str
    base_ref: str                          # 基准引用
    target_ref: str                        # 目标引用
    changed_files: List[str] = field(default_factory=list)
    changed_entities: List[ChangedEntity] = field(default_factory=list)
    impacts: List[ChangeImpact] = field(default_factory=list)
    overall_risk: RiskLevel = RiskLevel.LOW
    summary: str = ""
    recommendations: List[str] = field(default_factory=list)

    def to_dict(self) -> Dict[str, Any]:
        return {
            "repo_path": self.repo_path,
            "base_ref": self.base_ref,
            "target_ref": self.target_ref,
            "changed_files_count": len(self.changed_files),
            "changed_entities_count": len(self.changed_entities),
            "impacts": [i.to_dict() for i in self.impacts],
            "overall_risk": self.overall_risk.value,
            "summary": self.summary,
            "recommendations": self.recommendations,
        }


class GitDiffAnalyzer:
    """
    Git Diff 分析 Agent

    分析 Git 代码变更的结构和语义影响。

    工作流程：
    1. 获取 Git Diff
    2. 解析变更的文件和代码
    3. 识别变更的实体（函数、类等）
    4. 在知识图谱中追踪影响范围
    5. 评估风险等级
    6. 生成变更摘要和建议

    使用示例:
        analyzer = GitDiffAnalyzer(parser, knowledge_graph)

        # 分析 unstaged changes
        analysis = analyzer.analyze_unstaged()

        # 分析 staged changes
        analysis = analyzer.analyze_staged()

        # 分析两个分支的差异
        analysis = analyzer.analyze_branches("main", "feature/new-api")

        # 检查风险
        if analysis.overall_risk == RiskLevel.HIGH:
            print("Warning: High risk changes detected!")
    """

    def __init__(
        self,
        parser: Optional[TreeSitterParser] = None,
        extractor: Optional[ASTExtractor] = None,
        knowledge_graph: Optional[KnowledgeGraph] = None,
        repo_path: str = ".",
    ):
        """
        初始化 Git Diff 分析器

        Args:
            parser: Tree-sitter 解析器
            extractor: AST 提取器
            knowledge_graph: 知识图谱（用于影响分析）
            repo_path: Git 仓库路径
        """
        self.parser = parser or TreeSitterParser()
        self.extractor = extractor or ASTExtractor()
        self.kg = knowledge_graph
        self.repo_path = repo_path

        self._git_available = self._check_git()

    def analyze_unstaged(self) -> DiffAnalysis:
        """分析未暂存的变更"""
        return self._analyze_diff(
            base_ref="HEAD",
            target_ref="WORKTREE",
            staged=False,
        )

    def analyze_staged(self) -> DiffAnalysis:
        """分析已暂存的变更"""
        return self._analyze_diff(
            base_ref="HEAD",
            target_ref="STAGED",
            staged=True,
        )

    def analyze_branches(self, base_branch: str, target_branch: str) -> DiffAnalysis:
        """分析两个分支之间的差异"""
        return self._analyze_diff(
            base_ref=base_branch,
            target_ref=target_branch,
            staged=False,
        )

    def analyze_commits(self, base_commit: str, target_commit: str) -> DiffAnalysis:
        """分析两个提交之间的差异"""
        return self._analyze_diff(
            base_ref=base_commit,
            target_ref=target_commit,
            staged=False,
        )

    # ---- 核心分析方法 ----

    def _analyze_diff(
        self,
        base_ref: str,
        target_ref: str,
        staged: bool = False,
    ) -> DiffAnalysis:
        """
        执行完整的 Diff 分析

        Args:
            base_ref: 基准引用
            target_ref: 目标引用
            staged: 是否只分析暂存的变更

        Returns:
            DiffAnalysis: 完整的差异分析结果
        """
        analysis = DiffAnalysis(
            repo_path=self.repo_path,
            base_ref=base_ref,
            target_ref=target_ref,
        )

        # 1. 获取变更的文件列表
        changed_files = self._get_changed_files(base_ref, target_ref, staged)
        analysis.changed_files = changed_files
        logger.info(f"检测到 {len(changed_files)} 个变更文件")

        if not changed_files:
            analysis.summary = "No changes detected."
            return analysis

        # 2. 分析每个文件的变更
        for file_path in changed_files:
            # 获取 diff 内容
            diff_content = self._get_file_diff(file_path, base_ref, target_ref, staged)
            if not diff_content:
                continue

            # 解析变更的实体
            entities = self._extract_changed_entities(
                file_path, diff_content, base_ref, target_ref, staged
            )
            analysis.changed_entities.extend(entities)

        logger.info(f"识别到 {len(analysis.changed_entities)} 个变更实体")

        # 3. 影响分析
        if self.kg:
            for entity in analysis.changed_entities:
                impact = self._analyze_entity_impact(entity)
                analysis.impacts.append(impact)
        else:
            # 没有知识图谱时的简化分析
            for entity in analysis.changed_entities:
                analysis.impacts.append(ChangeImpact(
                    entity=entity,
                    risk_level=self._assess_basic_risk(entity),
                    reasoning="Knowledge graph not available for detailed impact analysis.",
                ))

        # 4. 整体风险评估
        analysis.overall_risk = self._assess_overall_risk(analysis.impacts)

        # 5. 生成摘要
        analysis.summary = self._generate_summary(analysis)

        # 6. 生成建议
        analysis.recommendations = self._generate_recommendations(analysis)

        return analysis

    # ---- Git 操作 ----

    def _check_git(self) -> bool:
        """检查 Git 是否可用"""
        try:
            import git
            self._repo = git.Repo(self.repo_path, search_parent_directories=True)
            self.repo_path = self._repo.working_tree_dir
            return True
        except ImportError:
            logger.warning("GitPython 未安装，使用命令行 git")
            return self._check_git_cli()
        except Exception as e:
            logger.warning(f"Git 仓库初始化失败: {e}")
            return self._check_git_cli()

    def _check_git_cli(self) -> bool:
        """检查 git CLI 是否可用"""
        import subprocess
        try:
            result = subprocess.run(
                ["git", "--version"],
                capture_output=True,
                text=True,
                cwd=self.repo_path,
            )
            return result.returncode == 0
        except Exception:
            return False

    def _get_changed_files(
        self,
        base_ref: str,
        target_ref: str,
        staged: bool,
    ) -> List[str]:
        """获取变更文件列表"""
        if not self._git_available:
            return []

        try:
            if hasattr(self, '_repo'):
                # GitPython 方式
                if staged:
                    diff_index = self._repo.index.diff("HEAD")
                elif target_ref == "WORKTREE":
                    diff_index = self._repo.index.diff(None)
                else:
                    base_commit = self._repo.commit(base_ref)
                    target_commit = self._repo.commit(target_ref)
                    diff_index = base_commit.diff(target_commit)

                files = []
                for diff_item in diff_index:
                    if diff_item.a_path:
                        files.append(diff_item.a_path)
                    if diff_item.b_path and diff_item.b_path != diff_item.a_path:
                        files.append(diff_item.b_path)

                return list(set(
                    f for f in files
                    if Language.from_filename(f) != Language.UNKNOWN
                ))
            else:
                # Git CLI 方式
                import subprocess
                if staged:
                    cmd = ["git", "diff", "--cached", "--name-only"]
                elif target_ref == "WORKTREE":
                    cmd = ["git", "diff", "--name-only"]
                else:
                    cmd = ["git", "diff", "--name-only", base_ref, target_ref]

                result = subprocess.run(
                    cmd,
                    capture_output=True,
                    text=True,
                    cwd=self.repo_path,
                )
                if result.returncode == 0:
                    return [
                        f.strip() for f in result.stdout.split("\n")
                        if f.strip() and Language.from_filename(f.strip()) != Language.UNKNOWN
                    ]
        except Exception as e:
            logger.warning(f"获取变更文件失败: {e}")

        return []

    def _get_file_diff(
        self,
        file_path: str,
        base_ref: str,
        target_ref: str,
        staged: bool,
    ) -> str:
        """获取单个文件的 diff 内容"""
        try:
            import subprocess

            if staged:
                cmd = ["git", "diff", "--cached", "--", file_path]
            elif target_ref == "WORKTREE":
                cmd = ["git", "diff", "--", file_path]
            else:
                cmd = ["git", "diff", base_ref, target_ref, "--", file_path]

            result = subprocess.run(
                cmd,
                capture_output=True,
                text=True,
                cwd=self.repo_path,
            )
            if result.returncode == 0:
                return result.stdout
        except Exception as e:
            logger.warning(f"获取文件 diff 失败 {file_path}: {e}")

        return ""

    # ---- 实体提取 ----

    def _extract_changed_entities(
        self,
        file_path: str,
        diff_content: str,
        base_ref: str,
        target_ref: str,
        staged: bool,
    ) -> List[ChangedEntity]:
        """从 diff 中提取变更的代码实体"""
        changed_entities = []

        # 解析 diff 中的 hunk 头部信息
        hunks = self._parse_diff_hunks(diff_content)

        # 获取文件的旧版本和新版本内容
        old_content = self._get_file_content_at_ref(file_path, base_ref)
        new_content = self._get_file_content_at_ref(file_path, target_ref)

        # 分析旧代码和新代码的实体
        old_entities = set()
        new_entities = set()

        if old_content:
            old_result = self.parser.parse_code(old_content, Language.from_filename(file_path))
            if old_result.success:
                old_extracted, _ = self.extractor.extract(old_result)
                old_entities = {e.name for e in old_extracted if e.type in (
                    EntityType.FUNCTION, EntityType.METHOD, EntityType.CLASS
                )}

        if new_content:
            new_result = self.parser.parse_code(new_content, Language.from_filename(file_path))
            if new_result.success:
                new_extracted, _ = self.extractor.extract(new_result)
                new_entities = {e.name for e in new_extracted if e.type in (
                    EntityType.FUNCTION, EntityType.METHOD, EntityType.CLASS
                )}

        # 检测新增的实体
        for name in new_entities - old_entities:
            changed_entities.append(ChangedEntity(
                name=name,
                entity_type=EntityType.FUNCTION,
                change_type=ChangeType.ADDED,
                file_path=file_path,
            ))

        # 检测删除的实体
        for name in old_entities - new_entities:
            changed_entities.append(ChangedEntity(
                name=name,
                entity_type=EntityType.FUNCTION,
                change_type=ChangeType.DELETED,
                file_path=file_path,
            ))

        # 检测修改的实体（同时存在于新旧版本）
        for name in old_entities & new_entities:
            # 检查实体是否真的有变化
            if self._entity_changed(name, old_content, new_content, file_path):
                changed_entities.append(ChangedEntity(
                    name=name,
                    entity_type=EntityType.FUNCTION,
                    change_type=ChangeType.MODIFIED,
                    file_path=file_path,
                ))

        # 使用 hunk 信息补充行号
        for entity in changed_entities:
            for hunk in hunks:
                if entity.line_start == 0:
                    entity.line_start = hunk["new_start"]
                    entity.line_end = hunk["new_start"] + hunk["new_lines"]

        return changed_entities

    def _parse_diff_hunks(self, diff_content: str) -> List[Dict[str, int]]:
        """解析 diff 中的 hunk 头部"""
        hunks = []
        for line in diff_content.split("\n"):
            match = re.match(r'@@ -(\d+),?(\d*) \+(\d+),?(\d*) @@', line)
            if match:
                hunks.append({
                    "old_start": int(match.group(1)),
                    "old_lines": int(match.group(2) or 1),
                    "new_start": int(match.group(3)),
                    "new_lines": int(match.group(4) or 1),
                })
        return hunks

    def _get_file_content_at_ref(self, file_path: str, ref: str) -> Optional[str]:
        """获取指定引用的文件内容"""
        if ref == "WORKTREE":
            full_path = os.path.join(self.repo_path, file_path)
            if os.path.exists(full_path):
                return Path(full_path).read_text(encoding="utf-8", errors="ignore")
            return None

        if ref == "STAGED":
            import subprocess
            try:
                result = subprocess.run(
                    ["git", "show", f":{file_path}"],
                    capture_output=True,
                    text=True,
                    cwd=self.repo_path,
                )
                if result.returncode == 0:
                    return result.stdout
            except Exception:
                pass
            return None

        import subprocess
        try:
            result = subprocess.run(
                ["git", "show", f"{ref}:{file_path}"],
                capture_output=True,
                text=True,
                cwd=self.repo_path,
            )
            if result.returncode == 0:
                return result.stdout
        except Exception:
            pass

        return None

    def _entity_changed(
        self,
        name: str,
        old_content: Optional[str],
        new_content: Optional[str],
        file_path: str,
    ) -> bool:
        """检查实体是否真的发生了变化"""
        if not old_content or not new_content:
            return True  # 无法比较，视为有变更

        # 简单比较：提取实体代码行并比较
        old_lines = old_content.split("\n")
        new_lines = new_content.split("\n")

        # 简单的文本差异检测（更精确的检测需要 AST 级别比较）
        return old_content != new_content

    # ---- 影响分析 ----

    def _analyze_entity_impact(self, entity: ChangedEntity) -> ChangeImpact:
        """分析单个实体的变更影响"""
        if self.kg is None:
            return ChangeImpact(
                entity=entity,
                risk_level=self._assess_basic_risk(entity),
                reasoning="No knowledge graph available",
            )

        # 在知识图谱中查找受影响的实体
        impact = self.kg.get_impact_analysis(entity.name, max_depth=3)

        affected = impact.get("direct", []) + impact.get("indirect", [])
        affected_count = len(affected)

        # 获取调用链影响
        call_chains = self.kg.get_call_chain(entity.name, max_depth=5)

        # 评估风险
        risk = self._assess_entity_risk(entity, affected_count, call_chains)

        reasoning = self._build_risk_reasoning(entity, affected, call_chains)

        return ChangeImpact(
            entity=entity,
            risk_level=risk,
            affected_entities=affected,
            call_chain_impact=call_chains,
            reasoning=reasoning,
        )

    def _assess_entity_risk(
        self,
        entity: ChangedEntity,
        affected_count: int,
        call_chains: List[List[str]],
    ) -> RiskLevel:
        """评估单个实体的变更风险"""
        # 删除操作风险高
        if entity.change_type == ChangeType.DELETED:
            if affected_count > 10:
                return RiskLevel.CRITICAL
            if affected_count > 3:
                return RiskLevel.HIGH
            return RiskLevel.MEDIUM

        # 新增操作风险低
        if entity.change_type == ChangeType.ADDED:
            return RiskLevel.LOW

        # 修改操作根据影响范围评估
        if affected_count > 20:
            return RiskLevel.CRITICAL
        if affected_count > 10:
            return RiskLevel.HIGH
        if affected_count > 3:
            return RiskLevel.MEDIUM
        return RiskLevel.LOW

    def _assess_basic_risk(self, entity: ChangedEntity) -> RiskLevel:
        """基本风险评估（无知识图谱时）"""
        if entity.change_type == ChangeType.DELETED:
            return RiskLevel.HIGH
        if entity.entity_type in (EntityType.CLASS, EntityType.INTERFACE):
            return RiskLevel.MEDIUM
        return RiskLevel.LOW

    def _build_risk_reasoning(
        self,
        entity: ChangedEntity,
        affected: List[Dict[str, Any]],
        call_chains: List[List[str]],
    ) -> str:
        """构建风险分析推理"""
        parts = []
        parts.append(f"Entity '{entity.name}' was {entity.change_type.value} in {entity.file_path}.")
        parts.append(f"Directly affects {len([a for a in affected if a.get('depth', 0) == 1])} entities.")
        parts.append(f"Total propagation reaches {len(affected)} entities.")
        parts.append(f"Found {len(call_chains)} call chains involving this entity.")
        return " ".join(parts)

    def _assess_overall_risk(self, impacts: List[ChangeImpact]) -> RiskLevel:
        """评估整体变更风险"""
        if not impacts:
            return RiskLevel.LOW

        risk_scores = {
            RiskLevel.LOW: 1,
            RiskLevel.MEDIUM: 2,
            RiskLevel.HIGH: 3,
            RiskLevel.CRITICAL: 5,
        }

        # 取最高风险中的最大值
        max_risk = max(impacts, key=lambda i: risk_scores[i.risk_level])
        total_score = sum(risk_scores[i.risk_level] for i in impacts)

        if total_score > 20 or any(i.risk_level == RiskLevel.CRITICAL for i in impacts):
            return RiskLevel.CRITICAL
        if total_score > 10:
            return RiskLevel.HIGH
        if total_score > 3:
            return RiskLevel.MEDIUM
        return RiskLevel.LOW

    def _generate_summary(self, analysis: DiffAnalysis) -> str:
        """生成变更摘要"""
        parts = []
        parts.append(f"## Git Diff Analysis: {analysis.base_ref} → {analysis.target_ref}\n")
        parts.append(f"**{len(analysis.changed_files)}** files changed, "
                     f"**{len(analysis.changed_entities)}** entities affected.\n")

        # 按类型统计
        type_counts = {}
        for entity in analysis.changed_entities:
            key = entity.change_type.value
            type_counts[key] = type_counts.get(key, 0) + 1
        parts.append("### Changes by type:")
        for change_type, count in type_counts.items():
            parts.append(f"- {change_type}: {count}")

        parts.append(f"\n### Overall Risk: **{analysis.overall_risk.value.upper()}**")

        if analysis.impacts:
            parts.append("\n### High Risk Changes:")
            for impact in analysis.impacts:
                if impact.risk_level in (RiskLevel.HIGH, RiskLevel.CRITICAL):
                    parts.append(f"- `{impact.entity.name}` ({impact.entity.change_type.value}) - "
                                f"{impact.reasoning[:100]}")

        return "\n".join(parts)

    def _generate_recommendations(self, analysis: DiffAnalysis) -> List[str]:
        """生成变更建议"""
        recommendations = []

        if analysis.overall_risk in (RiskLevel.HIGH, RiskLevel.CRITICAL):
            recommendations.append("⚠️ High risk changes detected. Thorough code review recommended.")
            recommendations.append("Ensure all dependent modules are tested.")
            recommendations.append("Consider running the full test suite before merging.")

        # 删除相关的建议
        deleted = [e for e in analysis.changed_entities if e.change_type == ChangeType.DELETED]
        if deleted:
            names = ", ".join(f"`{e.name}`" for e in deleted[:5])
            recommendations.append(f"The following entities were deleted: {names}. "
                                 "Verify that all references have been updated.")

        # API 变更建议
        modified = [e for e in analysis.changed_entities if e.change_type == ChangeType.MODIFIED]
        if modified:
            names = ", ".join(f"`{e.name}`" for e in modified[:5])
            recommendations.append(f"Modified entities: {names}. "
                                 "Verify backward compatibility or update callers.")

        if not recommendations:
            recommendations.append("Changes appear low-risk. Standard review process applies.")

        return recommendations

    def get_diff_stats(self) -> Dict[str, Any]:
        """获取当前仓库的 diff 统计"""
        import subprocess

        try:
            result = subprocess.run(
                ["git", "diff", "--stat"],
                capture_output=True,
                text=True,
                cwd=self.repo_path,
            )
            if result.returncode == 0:
                return {"stat": result.stdout.strip()}
        except Exception:
            pass

        return {"stat": "Unable to get diff stats"}
