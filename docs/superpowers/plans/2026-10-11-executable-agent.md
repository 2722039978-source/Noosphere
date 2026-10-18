# Scheme three: executable read-only project Agent

> Execute inline with superpowers:executing-plans and TDD. User selected the previously proposed scheme three; implement its explicitly staged first phase without further approval gates. One independent whole-change review at the end. No Git checkout: preserve user changes/data in place.

**Goal:** A goal submitted from desktop chat or DevOps runs a bounded backend tool loop across project code, confirmed knowledge and system observations, saves evidence/progress/results, and can be cancelled or inspected after navigation/restart.

**Spec:** Original scheme three in the RhineLabUI conversation: structured tool definitions/messages/results; per-task execution boundaries and budgets; verifiable output. First phase explicitly opens search/read/memory/system status. File edits, tests, service mutations and rollback are later stages and are not silently enabled here.

**Architecture:** Add optional structured-tool provider capability to Gateway without changing existing text/stream providers. DeepSeek/OpenAI-compatible non-stream tool turns use actual assistant tool_calls and matching tool messages, never parse prose as commands. New agent runtime owns a scoped registry and emits each tool start/result through durable workspace tasks. Chat uses existing stream lifecycle with a durable-task transport adapter; DevOps and task center show the same persisted evidence.

**Tech:** Existing Go 1.26, SQLite workspace manager, Nebula knowledge service, React/Zustand, Tauri RPC. No new runtime dependencies.

## Constraints and decisions
- Retain desktop styling, ordinary chat, old task results and diagnostic compatibility endpoints. New UI explicitly labels read-only Agent.
- Freeze project root/project ID when queued; choose/copy model at execution start, retain it for all turns; reject workspace rebind between steps and before completing.
- Default/hard caps: 6 model turns, 12 tool calls, 120 seconds overall, 15 seconds/tool, 2 repeated identical failures, 2 repeated identical successes, 2,048 output tokens/turn, bounded input/evidence. No hidden retries or automatic post-restart replay.
- Supported tool protocol: DeepSeek/OpenAI/OpenAI-compatible/Ollama; other providers explicitly fail capability checks without sending a text fallback. Compatibility does not guarantee every upstream model implements tools.
- Tool arguments are strict objects with declared keys and bounds. No arbitrary commands/URLs/write tools. File traversal is rooted and symlink entries are rejected; skip generated/vendor/hidden/credential files, bound scans/file size and redact credential-like values from observations.
- References are produced by executed tools with source hashes/lines/timestamps, not invented from model prose. Final model conclusions remain distinguishable from observed results. Task error preserves earlier event evidence; cancellation never continues into another tool.
- Validation uses temporary projects/configs and local mock model servers. Do not read live credentials or spend real model API credits. Update release exe/backend/resources and verify shortcut targets at completion.

## Tasks
- [x] 1. Structured provider: add tool DTOs/capability and OpenAI implementation; Gateway bounded nonretrying wrapper. Test request/response roundtrip, tool-only output, matching IDs, malformed/truncated payloads, cancellation and unsupported models.
- [x] 2. Registry and loop: new `backend/internal/agent` scoped files/search/context/system tools and bounded execution. Test traversal/symlink/secrets, schema rejection, stale root, cancellation, budgets, repeated failures, evidence and step events.
- [x] 3. Durable integration: add agent task kind, server worker with frozen workspace/model, shared knowledge provider; integration tests through local structured mock provider with persisted events, cancellation and restart behavior.
- [x] 4. Desktop integration: durable transport in chat, read-only Agent toggle, saved task/evidence details, DevOps entry and task-center display; unit/browser tests for delayed submission cancellation, workspace switches, failure evidence, navigation and recovery.
- [x] 5. Review, suites, release: one fresh review, fix proven issues with regressions, frontend/Go/Python/Rust appropriate checks, isolated packaged mock-provider workflow, release/manifest/shortcuts, usage/limits/verification docs.

## Review focus
- Cancel during task registration or model wait; no orphan task or tool after cancellation.
- Tool IDs/parameters/output supplied by a model are untrusted; unknown or malformed calls cannot execute.
- Workspace root changes during a queued/running task; no mixed-project evidence or final result.
- Symlinks, secrets, huge/binary files, repeated failures and output growth stay bounded.
- A closed/reloaded UI retains durable task identity and observations without resubmitting or charging again.

## References
- DeepSeek official tool calls: https://api-docs.deepseek.com/guides/tool_calls/ (checked 2026-10-11): model requests calls; client executes and returns results with matching call IDs. Use standard non-thinking mode, not beta strict mode.

## Execution ledger
- Structured provider, Gateway capability checks and loop passed unit/integration tests. Local mock HTTP verifies tool-only responses, matching call IDs and in-flight cancellation; unsupported providers are rejected before network access.
- Scoped registry tests cover traversal, strict arguments, redaction, binary/oversized inputs, source search, link rejection, model/tool budgets and repeated failures. Stale/missing context sources first failed a regression, then passed after scoped reread and hash checks.
- Durable backend worker saves per-step events and result. New packaged fixture covers cancellation, workspace rebinding and restart without replay, passed on the final published artifact (5 checks).
- Frontend unit tests: 60 passed. Agent browser flows: 5 passed. Existing UI/workspace/knowledge/return and handoff suites passed. Production asset/CSP smoke passed. Go whole suite and vet passed; Python 12, Rust 28, script fixtures 40 passed.
- Fresh independent reviewer found delayed registration losing chat task identity and reload during execution hiding the durable answer. Both fixed. Reload regression observed RED (answer wait timed out), then GREEN after enabling saved answer when chat content is empty. Delayed ACK cancellation now verifies original message links to the cancelled backend record.
- Ruling: keep this plan/ledger as permanent verification history because this project has no Git metadata; do not remove project files as a branch-finishing operation.

- Final release: Windows exe/backend and both shortcuts updated; manifest hashes checked, Cargo desktop artifact identical, 5 Python resources match source. Packaged Agent mock-provider test 5/5 and real Python/workspace test 10/10 passed. Delivery evidence: docs/EXECUTABLE_AGENT_VERIFICATION.md. No Computer Use or paid model API calls were used.
