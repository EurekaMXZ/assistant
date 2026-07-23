# AGENTS.md

These instructions apply to every coding agent working in this repository. More specific `AGENTS.md` files, such as `frontend/AGENTS.md`, add rules for their directory trees.

## Required Approval Gate

Before changing code, documentation, configuration, schemas, migrations, dependencies, generated artifacts, or tests, a coding agent must:

1. Read the documentation and local instructions relevant to the requested area.
2. Inspect the existing implementation and tests. For a bug, identify the root cause rather than only the visible symptom. For a feature, develop a concrete implementation approach, including important tradeoffs and affected components.
3. Explain the findings and proposed change to the user clearly enough for the user to evaluate the approach.
4. Ask for and receive explicit user approval before editing files or running mutating commands.

The user's initial bug report or feature request does not replace this approval checkpoint. Approval must follow the agent's explanation of the diagnosis or implementation plan unless the user explicitly waived the checkpoint in advance. If later discoveries materially change the approved approach or scope, stop, explain the change, and obtain approval again.

Before approval, limit work to read-only investigation and non-mutating reproduction or verification. Do not edit files, install or update dependencies, run formatters or generators that write files, apply migrations, or create commits.

## Relevant Documentation

- Always begin with `README.md` for the architecture, project layout, and development environment.
- Read `docs/API.md` before changing HTTP APIs, request or response models, authentication behavior, or streaming contracts.
- Read `docs/STORAGE-PLAN.md` before changing object storage, attachments, artifacts, retention, or related database state.
- Read `frontend/README.md` and `frontend/AGENTS.md` before changing frontend code.
- Read the relevant files under `prompts/` before changing agent behavior or tool-use instructions.
- Inspect the applicable migrations, configuration, tests, and neighboring implementation files for every change.

## Implementation And Verification

After approval, keep the implementation scoped to the approved plan and preserve established repository patterns.

If files are changed, rerun every applicable formatter, linter, and test suite for the affected area. A small change is not a reason to skip verification.

- Go changes: format changed Go files with `gofmt`, then run `go vet ./...` and `go test ./...`.
- Frontend changes: from `frontend/`, format changed files with Prettier, then run `pnpm format:check`, `pnpm lint`, and `pnpm test`. Run `pnpm build` when the change can affect production compilation or rendering.
- Documentation and configuration changes: run `git diff --check` and any formatter or validator that covers the changed files.

Report which checks passed. If an applicable check cannot be run, state that explicitly and explain why; do not present the work as fully verified.
