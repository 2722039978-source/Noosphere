# Workspace and task runtime implementation plan

> **For agentic workers:** Use superpowers:subagent-driven-development for bounded implementation and review. User has authorized scheme one and exe publication; minimize Computer Use.

**Goal:** Backend-owned workspace identities and durable task/event/results for chat, indexing, and diagnostics, surfaced in the existing Windows desktop.

**Architecture:** A local SQLite workspace store plus bounded task manager in Go. Existing Nebula data and IndexedDB chat history remain intact. Tauri IPC exposes workspace CRUD and task observation/cancellation; frontend scopes sessions and project context to a selected workspace. Restart marks unfinished runs interrupted rather than replaying operations.

**Tech Stack:** Go, SQLite (pure Go driver), React/TypeScript, existing Tauri Rust IPC.

**Spec:** User-approved scheme one in this conversation: workspace entity, shared task state/progress/cancellation, durable events, bounded resource scheduling; preserve current UI palette/motion, update release exe and shortcuts.

## Constraints
- No user keys in source, fixtures or logs; no live model calls needed for verification.
- Product is not a Git checkout; edit existing workspace and retain existing user data.
- Do not migrate/delete Nebula or existing conversations; unscoped historic chats belong to local workspace.
- Chat remains one active frontend generation, but run state and result are recorded server-side.
- Index/diagnosis jobs survive page changes; process restart marks pending work interrupted and does not automatically re-execute.
- Use adaptive cursor polling through IPC for durable events in this iteration; no extra desktop listener process.
- No Computer Use needed for automated tests/build/hash verification. Publish updated native executable.

## Review focus
- Workspace switching during generation/indexing must not attach responses or index IDs to another project.
- Cancellation/late completion must not overwrite terminal state or replay work.
- Restart and malformed journal/data must produce honest state, not empty-success data.
- A slow background index must not occupy every interactive slot.
- Publication must match compiled artifacts and preserve local config/data.

## Work packages
- [x] Core: SQLite store, task manager, workspace/run/event HTTP API, restart recovery, scheduling and cancellation tests.
- [x] Integration: register index/diagnostic workers and attach chat stream runs, default workspace model and context identity; HTTP integration tests.
- [x] Desktop IPC and chat scope: expose workspace/run commands; scope local chat sessions, freeze workspace identity at request start, preserve legacy histories.
- [x] UI: workspace selector/editor and task drawer; submit indexing/diagnosis as backend jobs and show progress/results/cancel.
- [x] Verification: isolated Go/Rust/frontend tests, production browser smoke/flows, final manual code review, native release build and SHA256 manifest comparison. Independent review could not complete because of agent usage limits; see `docs/WORKSPACE_RUNTIME_VERIFICATION.md` for evidence and limitations.

## Shared contract
See `workspace-runtime-contract.md` beside this file for exact interfaces and endpoints.
