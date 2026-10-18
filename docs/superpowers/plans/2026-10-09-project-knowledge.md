# Scheme two: project understanding and long-term memory

> Execute inline with superpowers:executing-plans and test-driven-development. User explicitly authorized scheme two and continued exe updates; no extra approval gate. Product has no Git checkout; preserve existing data and work in place. No Computer Use or live model calls.

**Goal:** Retrieve complete, versioned code and confirmed project knowledge, expose the evidence in desktop chat, and avoid stale/cross-workspace facts.

**Architecture:** Python generates symbol-aware stable code chunks and source versions. Nebula publishes only changed records/vectors, skips graph rebuild on no-op/metadata-only updates, and stages changed graphs outside its reader lock before atomic publication. A Go knowledge service owns workspace-scoped memory lifecycle and unified retrieval, validating source hashes before injection. Tauri forwards structured requests; React surfaces memory review and persisted citations without changing archive visuals.

**Tech:** Existing Go/LSM/HNSW, SQLite workspace identity, Python/tree-sitter, Tauri, React. No new runtime dependencies.

**Spec / accepted scope:** Original scheme two in this conversation: symbol/module chunks with file/symbol/call/version context; finer incremental indexing with measured graph cost; distinguish chat from long-term facts; memory provenance, scope, supersession and visible answer references. Hash vectors remain explicitly identified as lexical fallback. No autonomous write/tools (scheme three).

## Decisions and constraints
- Preserve legacy Nebula session records. New curated knowledge uses a dedicated workspace namespace; old chats/memories are never silently promoted to confirmed facts.
- New memory is proposed by default. User may confirm/reject/expire it or explicitly supersede a prior record in the same workspace. Revisions reject concurrent stale edits.
- Source snapshots include hashes. Retrieval rejects changed/deleted/out-of-root file sources and warns to reindex; no continuous filesystem watcher or automatic paid embedding job.
- Code chunks retain complete ordinary functions/classes, split oversized units with explicit parts, and cover module gaps. Stable symbol IDs avoid line-only invalidation.
- Results expose embedding mode and retrieved evidence; provided evidence is not presented as proof that every model statement is correct.
- Use short bounded retrieval with source/content budgets. Model/source errors remain visible, and no user credentials enter fixtures/logs.
- Update release exe, backend and Python resources only after tests; verify hashes and shortcut targets.

## Review focus
- Inserted lines leave unchanged symbol identity/vector reusable while citation lines update.
- Cancelled/failed index publication preserves previous snapshot; deleted chunks disappear without touching another project.
- Changed/deleted source, changed workspace root, expiry and supersession cannot remain active evidence.
- A memory status update must not overwrite newer content; foreign IDs cannot target another workspace.
- Workspace switch during retrieval/save must not transfer results or references to the new workspace.

## Tasks / ledger
- [x] 1. Symbol chunks: create `backend/python/codelens/symbol_chunks.py`; integrate `project_index.py`, improve AST entity coverage; tests for complete functions/classes, long-unit parts, stable IDs and hashes/calls.
- [x] 2. Incremental publication: update `engine/project.go` with context/stats, changed-record detection and staged graph publication; enrich `codelens/index.go` provenance; benchmark unchanged/one-change and test atomic failure/restart/isolation.
- [x] 3. Knowledge service: add `backend/internal/knowledge` models, lifecycle, source validation, unified retrieval and workspace HTTP routes; wire bootstrap. Test confirm/reject/supersede/revision conflicts, expiry, path escape, stale sources, bounded retrieval and restart.
- [x] 4. Desktop integration: add Tauri commands and api types; route chat preparation through backend context, persist citations/status in messages; add memory review/editor in Nebula and save-message-to-proposed action. Keep existing workspace ownership guards.
- [x] 5. Verification and release: targeted red/green tests, Go/Rust/Python/frontend suites, isolated browser flows, actual packaged indexing/knowledge/restart fixture, final review, build and hash/shortcut verification. Document limits and measured indexing results.

## Execution evidence
- Task 1: original fixed-line behavior failed symbol tests; complete symbol chunks, stable IDs, byte-exact source hashes, module coverage and conservative call metadata implemented. Python 12/12 after final review fixes.
- Task 2: context/delta API first failed to compile, then no-op/metadata/cancellation tests passed. Source metadata persists; unchanged CreatedAt permits true no-op publication. 1,000-record benchmark: unchanged 7.40 ms, one change 67.57 ms (small synthetic sample, not end-to-end).
- Task 3: lifecycle tests failed before service existed, then confirmation/supersession/expiry/root/source/HTTP/revision checks passed. Workspaces own the namespace and project binding; sources are read with bounded size and root validation.
- Task 4: workspace chat test failed against old search path, then unified context and persisted evidence passed; malformed citation import test RED→GREEN. Five isolated Playwright knowledge flows passed, no console errors; existing 33 browser checks also passed. Browser plugin not available, used existing Playwright harness; no Computer Use.
- Final review: one fresh reviewer, five Important findings accepted. No deferred minors. Supersedes target revision, invalid-source deactivation, note/chat root provenance, cancellation while waiting for publication lock, and quadratic entity scans each reproduced with failing tests, then fixed. No second review dispatch.
- Task 5 complete: Go 144 + vet, Python 12, frontend 58, Rust 28, browser 38, script fixtures 40, packaged backend/Python checks 10 passed. Release desktop/backend hashes match build-info.json; Python source resources match; desktop and project shortcuts target the new exe. Evidence: docs/PROJECT_KNOWLEDGE_VERIFICATION.md. No Git checkout exists, so in-place source and this ledger are retained rather than deleting a worktree.
