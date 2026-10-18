# RhineLabUI frontend implementation plan

Goal: full-screen archive as default entry, consistent RhineLabUI visual language with Noosphere identity, performance safeguards and evidenced backend fixes.

Architecture: retain React / Tauri business and persistence layers. Adapt the supplied Three.js scene behind a React lifecycle boundary and a shared real-module registry. Separate immersive archive shell from operational workspace.

Spec: ../specs/2026-10-06-rhinelab-frontend-design.md

User decision: option A confirmed; preserve project character, optimize performance, fix small backend defects encountered. Execute in this chat.

## Constraints and review focus

- Existing directory is not a Git checkout. Backup: `.local/backups/rhine-20261006`; do not initialize Git or alter reference source.
- Five real business modules, with settings accessible from global navigation. No fictional research records or authentication claims.
- Default archive at `/`; `/home` remains an archive alias; AI conversation moves to `/chat`.
- Preserve streams across routes and existing persistence keys.
- Hidden / unmounted canvas, failed WebGL and reduced-motion preferences must not block navigation.
- Real backend data must not be changed by QA; use synthetic IPC and isolated browser storage.

## Tasks

- [x] Registry, routing and preferences: create `src/app/modules.ts`, `src/stores/experience-store.ts`; adapt routes and business links. Test module discovery, five-module navigation and preferences fallback.
- [x] Archive scene: adapt scene, motion, appearance, lighting and cyclic navigation under `features/home/scene`; replace ArchiveCanvas with disposable lazy React adapter. Verify wrap navigation, cleanup and canvas resource load.
- [x] Archive interface: replace HomePage; add searchable module index, selected module details, accessible controls, skippable boot, static fallback and persistent graphics preferences.
- [x] Business shell and design system: update layout/sidebar/PageHeader and shared styles; apply consistent module accents, document-style sections and operational readability throughout seven pages. Preserve existing functional components.
- [x] Backend: inspect relevant endpoints, reproduce a concrete defect with a regression test before repair; run Go tests with existing local toolchain if present.
- [x] Verification: TypeScript + Vite, existing frontend logic/UI/production checks updated for routes, desktop/mobile screenshots and reference comparison, new archive interaction tests. Document actual results and packaging limits.

## Execution ledger

- Design exploration complete. User selected immersive default with Noosphere identity and authorized performance and minor backend fixes.
- Reference has one-shot DOM lifecycle; React adaptation must explicitly dispose scene resources and pending loads.
- Reference's 40 fictional records are replaced by five real module entries; repeated 3D cassettes are visual archive structure, not fabricated user data.

- Ruling: user clarified Windows is the final target; browser previews are QA only. Deliver compiled executable, matching icon and direct native shortcuts.
- Main implementation and regression pass: 44 frontend logic tests; 14 UI scenarios; 5 archive/error/preference scenarios; 7 startup-feedback scenarios; production CSP smoke; full Go suite.
- Fresh review found narrow viewport scroll clipping. Fixed container sizing and independently verified wheel scrolling at 390x844 and 768x600. No remaining P1/P2 review findings.
- Native smoke exposed CodeLens system Python dependency mismatch. Added resource-relative local environment discovery with explicit Python path precedence; new regression and Go suite pass. Final native smoke pending updated build.
- New orbital SVG/ICO retains Noosphere identity. Project and Windows Desktop shortcuts point directly to release/Noosphere.exe. Desktop packaging built with full existing source and retains prior binary backups.
- Updated native smoke passed: Go and CodeLens started from TEMP with isolated NOOSPHERE_DATA_DIR, unsigned health returned 401, and forced desktop exit cleaned both owned services.
- Actual Windows WebView2 UI verified using the native window: default 3D archive, Right to CodeLens, Enter to its business page, sidebar to AI chat. Real backend and CodeLens displayed ready; no real model requests were sent. Normal window close also removed backend processes.
- Final dist changed after the preceding embed began (mobile sizing fix), so a final Cargo build is required. Binary asset-name check caught the older CSS; final artifact must contain HomePage-Cz8oHA6k.css before publication.

- Final release complete: 2026-10-06 22:23 CST. Verified embedded final HomePage-Cz8oHA6k.css; prior CSS absent. Published executable SHA256 40A7B8879308245BDC7DCE8303FA5DA82E7A7B1525DCABCD59CA5C6EF7D1CAA2. Final isolated native startup and Windows job cleanup both PASS. Project and desktop shortcuts refreshed. No NSIS requested; portable release is the delivered artifact.
