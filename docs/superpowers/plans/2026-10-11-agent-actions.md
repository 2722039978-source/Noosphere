# Scheme three stage two: reviewed actions

> Execute inline using superpowers:executing-plans and TDD. User explicitly requested stage two and prohibited Computer Use. Continue implementation without another design approval round; confirmation is a product feature for concrete runtime actions. One fresh final review.

**Goal:** Agent can propose project file edits, fixed test commands, and Windows service start/stop; the desktop shows exact previews, requires a human action for execution, and preserves results and conflict-aware rollback.

**Architecture:** Add a durable action journal alongside workspace storage, separate from the model loop. Proposal tools only create pending records. Authenticated desktop endpoints approve/reject/cancel/rollback one immutable proposal; no approval tool is exposed to the model. Per-action states prevent double execution, journal before side effects, recover uncertain states without replay. Existing read-only mode remains available; a separate reviewed-action mode adds proposal tools.

**Constraints:** No Computer Use. No live paid model calls, real user-project mutations or real service changes in validation. Preserve visual identity and normal chat. No new dependencies. Windows exe/backend/shortcuts updated at end. Max 3 proposals/task, 64 KiB text per file, existing parent directories only; no deletion proposals or arbitrary shell command. Edits use unique exact-text replacement or new files; root/link checks and before/after hashes guard execution and rollback. Tests are fixed go/python/node commands, max 90 seconds, 32 KiB output. Test code can have side effects: the preview says so. Services only start/stop one validated named service, save stable previous state, reject changed state and pending transitions. Service rollback restores saved stable state if unchanged. No auto elevation. Pending proposals survive restart; uncertain in-flight actions become interrupted and require inspection/reconciliation, never replay automatically.

## Tasks
- [x] 1. Durable journal, proposals, rooted atomic file apply/rollback and conflict/restart tests.
- [x] 2. Fixed test runner and service adapter, bounded execution/cancellation, runtime APIs and proposal-tool integration.
- [x] 3. Desktop IPC and reviewed-mode inputs; shared preview/confirm/reject/cancel/rollback UI in chat, DevOps and task center; headless interaction tests.
- [x] 4. Independent review, regressions, build and packaged mock-model/action workflow, manifest/shortcut checks and docs.

## Review focus
- A double click, network retry or restart must not execute a proposal twice.
- Approval must bind to exact saved content and original project; changed files/services must produce a conflict.
- File rollback must preserve external changes; recovery must distinguish an applied file from uncertain execution.
- Cancel tests including spawned children; previews disclose that project test code is executable.
- Model responses and imported chat records cannot approve actions; UI reads authoritative backend records.

## Execution ledger
- Journal and actual file tests passed: proposals leave files unchanged; immutable approval; reject duplicate/foreign decisions; version conflicts protect apply/rollback; restart marks in-flight records interrupted; created-file rollback restores absence.
- Required new_text regression observed failing, then passed after rejecting absent/null replacement values.
- Mock service tests verify initial-state conflicts, start/stop outcome and restoring original state. No real services changed.
- Real Windows helper processes verified bounded logs, local API credential removal, cancellation of test children. Log cap initially failed because embedded bytes.Buffer exposed ReaderFrom and bypassed Write; removed embedding and regression passed. Windows processes now start suspended, enter a kill-on-close Job, then resume.
- Frontend 60 unit tests, Go suite + vet, Rust 28 passed. Headless UI new approval flows 4, prior Agent flows 5, ordinary UI 14 and workspace flows 7 passed. No Computer Use.
- Inline review added authoritative refresh after decision errors and keyed action panels; inspected workspace IDs, exact saved content, task context ownership, shutdown/cancellation and no replay.
- The same independent reviewer resumed after an initial usage-limit interruption and completed review. It found a journal size mismatch: HTML escaping could expand accepted file snapshots beyond the restart reader limit. A regression reproduced the failure. Writer now disables HTML escaping and enforces the reader limit before touching disk; pending records reserve 8 KiB for later status/recovery metadata. Regressions cover markup apply/reopen/rollback, rejected oversized serialization without residual records, and near-limit interrupted recovery.
- Independent reviewer confirmed the targeted journal fix closed, with no remaining important finding. Full Go tests and vet passed after the fix.
- Final release built 2026-10-11T13:59:06.9748389Z. Packaged Agent/actions 9 and workspace/Python 10 passed. Desktop/backend manifest hashes, Cargo artifact and both shortcut targets/directories/icons matched. See docs/AGENT_ACTIONS_VERIFICATION.md for hashes, result files and validation limits.
