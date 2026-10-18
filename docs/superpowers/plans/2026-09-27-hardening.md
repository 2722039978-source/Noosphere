# Noosphere reliability and capability implementation plan

> **For agentic workers:** Use superpowers:executing-plans to implement this plan task-by-task. The user authorized implementation of the preceding project assessment.

**Goal:** Deliver a reproducible desktop build and correct memory, project retrieval, code analysis and parameterized diagnostic workflows.

**Architecture:** Preserve React / Tauri / Go / Python boundaries. Give project code a stable namespace and source references; keep indexes reproducible from authoritative records. Secure both local HTTP boundaries with per-process credentials. Preserve existing user data.

**Tech Stack:** React/TypeScript, Go, Tauri/Rust, FastAPI/Tree-sitter, PowerShell.

**Spec:** User-approved assessment in this conversation, recorded by the tasks below.

## Global constraints

- No changes to Windows security policy, no real paid model calls, no overwrite of user models or data.
- Keep source backups because this checkout has no Git metadata. Do not initialize a repository or invent commits.
- Use npm and its lockfile for reproducible frontend/desktop dependencies.
- Work inline; final fresh-context code review per executing-plans.
- New regression tests exercise real behavior with synthetic projects/data and fake external model responses.

## Review focus

- Chinese-only memories, missing/deleted records, restart and embedding model changes.
- Two projects containing identically named symbols, removed files and repeated indexing.
- Missing credentials, path escapes, slow model requests and cancellation.
- Existing chat records survive storage migration and failed imports.
- Builds from paths containing spaces/non-ASCII, fresh install, no orphan processes.

### Task 1: Restore reproducible tooling and build checks

Files: frontend/package*.json, scripts/check-environment.ps1, docs/DEVELOPMENT.md, build scripts as required.

- [x] Reproduce clean-install/test failures, locate or install project-local toolchains.
- [x] Reconcile npm lock, archive pnpm metadata; document exact supported versions and add preflight.
- [x] Run frontend baseline tests/build and Go baseline suite; attempt Rust build/test.

### Task 2: Correct Nebula indexes and persistence

Files: backend/internal/nebula/engine/{engine.go,engine_test.go,index_persistence.go}, embed/*.go, vector/hnsw.go, service/pipeline.go.

Interfaces: Engine.PutText/Get/Delete/SearchScoped remain compatible; SetEmbedder reports rebuild failure without switching partially.

- [x] Add failing tests for document counts, Chinese matching, text recovery, deletion, dimension changes and corrupt snapshots.
- [x] Share Unicode tokenization, keep indexed text with records, rebuild vectors safely on model changes, remove stale vectors.
- [x] Move external embedding outside index locks; save snapshots atomically and recover them from authoritative records.
- [x] Run Go tests, including restart and concurrent read/write tests.

### Task 3: Project-scoped code retrieval and accurate call graph

Files: backend/python/codelens/{main.py,ast_parser.py,project_index.py,test_project_index.py}, Go CodeLens bridge, frontend code/chat features.

Interfaces: index returns source chunks with project-relative path, symbol and line range; project namespace derives from canonical root. Chat retrieves only the selected project.

- [x] Add failing fixtures for caller ownership, duplicate names, source chunks and deleted files.
- [x] Build scope-aware graph and bounded incremental project index; persist code chunks into Nebula with stable identifiers and replace stale records.
- [x] Wire project-scoped chat context with readable source citations and bounded context budget.
- [x] Run Python, Go and frontend tests; typecheck/build frontend.

### Task 4: Parameterized, bounded diagnostic Agent

Files: backend/internal/devops/{agent,api,tools}, frontend chat/DevOps labels.

Interfaces: structured {thought,action,args} plans; context-aware Loop with bounded steps and validated tool inputs.

- [x] Reproduce missing tool arguments with fake model response and controlled tool.
- [x] Parse/validate structured plans, propagate timeout/cancellation, enforce step cap and unknown-tool rejection.
- [x] Describe chat toggle as planning assistance; retain real execution in DevOps with visible trace.
- [x] Run real plan-to-tool-to-final-answer regression and existing Go/frontend suites.

### Task 5: Local service authentication and storage resilience

Files: backend middleware/bootstrap/server, Python service, desktop runtime/commands, frontend chat storage and backup UI.

Interfaces: desktop generates NOOSPHERE_BACKEND_TOKEN; Go generates/passes a separate worker token; bridges attach credentials, browser origins do not grant authority. Project operations remain restricted to selected registered roots.

- [x] Add unauthorized/authorized/path-escape tests before changes.
- [x] Protect both service boundaries and constrain code paths; preserve standalone developer workflow through documented token configuration.
- [x] Migrate conversation persistence to IndexedDB by session with legacy recovery, JSON export/import and user-visible errors.
- [x] Verify denied requests, allowed operations and migration/reload/import behavior.

### Task 6: Delivery and review

- [x] Run complete relevant suites and build portable EXE/NSIS if toolchain permits.
- [x] Run available isolated desktop process lifecycle smoke; fresh installation and native UI explicitly remain unverified, and mock IPC is not counted as desktop execution.
- [x] Request independent final review; fix important findings with regression coverage.
- [x] Record commands, results, unresolved environment restrictions and changed subsystems in docs/HARDENING_2026-09-27.md.

## Execution ledger

- Initial: source snapshot at .local/backups/hardening-20260927-230754.
- Ruling: execute directly in non-Git checkout with backup; Git-specific skill helpers/commits are inapplicable. Original files remain recoverable.
- Baseline: npm ci fails because lockfile omits three/@types/three; node_modules junctions point to the old E:/project/Noosphere/new path. Go/Rust are absent from PATH; Python lacks FastAPI.
- Task 1: preflight passed; npm ci restored, local Go 1.26.5 / Rust 1.98.1 / Python venv installed; npm is authoritative. scripts/test-scripts.ps1: 40 checks passed.
- Task 2: go test ./... -count=1 passed after durability, Chinese retrieval, restart and vector-cache fixes.
- Task 3: Python unittest 6 passed; real Go/Python integration index -> authenticated scoped retrieval -> removed-file reindex passed; frontend context tests passed.
- Task 3: Ruling: bounded 60-line source windows instead of complete semantic chunks; all source stays addressable, with explicit truncation. Cost: boundaries can split a function.
- Task 3: Ruling: scoped vectors are scored exactly inside the namespace rather than globally ANN-filtered. Cost: more CPU for very large projects, in exchange for recall.
- Task 4: structured tool and cancellation regression passed; browser diagnostic trace fixture passed; no real paid-model calls.
- Task 5: 42 frontend unit tests and typecheck passed; 14 browser checks, production CSP smoke and 7 startup-feedback checks passed. Rust unit suite: 25 passed.
- Task 5: Ruling: authenticated callers choose project roots per operation; canonical containment prevents file escape but is not an OS sandbox. Cost: a trusted local caller may choose other accessible directories.
- Final review: one read-only reviewer completed after its initial quota failure. Five important findings fixed; inconsistent vector-cache dimensions upgraded from minor to important and fixed. No second review dispatched.
- Final: fixed archive hydration/export limits — large persisted snapshot RED->GREEN; failed-commit retry and newer-snapshot regression green; frontend 42/42.
- Final: fixed synchronous remote rebuild and empty metadata embedding — TestSlowEmbeddingRebuildDoesNotBlockModuleReadiness / TestMetadataIsNotSentToEmbeddingProvider RED->GREEN; Go suite green.
- Final: fixed torn WAL tail and SST publication — TestPartialWALTailDoesNotConsumeNextSuccessfulWrite / TestMalformedCompleteWALRecordFailsClosed / TestFailedSSTPublicationLeavesNoFinalPartialFile RED->GREEN; Go suite green.
- Final: fixed inconsistent cache dimensions — TestStructurallyCorruptVectorCacheIsRebuilt RED->GREEN; Go suite green.
- Final: Ruling: remove arbitrary archive/import capacity caps so exported growing histories remain recoverable. Cost: very large JSON backups need memory; streaming import/export remains future work.
- Final: minor (deferred): Python unqualified class-method calls are not a full language scope resolver; ambiguous class/module names can yield an incorrect edge.
- Final: Ruling: no destructive physical power-loss tests or live paid-model tests; validate persistence ordering, failure injection and synthetic provider contracts. Cost: hardware/provider-specific failures remain unverified.
- Final: Ruling: no fresh Windows installation or native UI automation claim until tested. Computer Use skill was read but node_repl is unavailable; Browser plugin absent. Process-level smoke may still verify isolated backend startup/cleanup; installed WebView UI remains unverified.
- Task 6: complete within stated validation boundaries — full portable EXE + NSIS build succeeded, published 2026-09-28 01:05 CST; isolated new desktop launches backend/CodeLens, rejects unsigned health and leaves no backend/worker after forced exit. Browser regression rerun 14/14 and production smoke passed after final storage changes.
- Final environment: finishing-a-development-branch Git integration choices are inapplicable because this checkout has no Git repository; retain files, source backup, plan and ledger. No push, PR, install or destructive cleanup performed.
