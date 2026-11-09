import type { ArtifactRecord, ContextCitation } from '@/lib/api';

export const ARTIFACT_REFERENCE_ID = 'A1';

export function artifactCitation(workspaceId: string, artifact: ArtifactRecord): ContextCitation {
  const result = artifact.evidence.normalized_result;
  const fields = artifact.tool === 'inspect-pe-imports'
    ? ['architecture', 'coverage', 'imports', 'delay_imports', 'imphash', 'issues']
    : ['architecture', 'format', 'build_info'];
  const details = JSON.stringify(Object.fromEntries(fields.map(key => [key, result[key]])), null, 2);
  const limit = 5500;
  const excerpt = details.length > limit ? `${details.slice(0, limit)}\n[摘要已截断，完整内容见原始记录]` : details;
  return {
    id: ARTIFACT_REFERENCE_ID,
    kind: 'artifact',
    title: artifact.path,
    excerpt,
    workspace_id: workspaceId,
    source_hash: artifact.source_hash,
    artifact_id: artifact.id,
  };
}

export function artifactPrompt(citation: ContextCitation): string {
  return `[${citation.id}] REA artifact analysis: ${citation.title}\nSHA-256: ${citation.source_hash}\n${citation.excerpt}`;
}
