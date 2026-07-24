# Contributing

## Commit Messages

Every commit must use a multiline Conventional Commit message with a subject, a blank line, and a flat unordered-list body:

```text
<type>(<scope>): <summary>

- <change>
```

### Subject

- Use a recognized type such as `feat`, `fix`, `refactor`, `test`, `docs`, `perf`, `build`, `ci`, or `chore`.
- Use a short, meaningful scope such as `chat`, `sandbox`, `workflow`, `frontend`, or `storage`.
- Write the summary in the imperative mood, keep it concise, and do not end it with a period.
- Keep the complete subject line at 72 characters or fewer.

### Body

- Separate the body from the subject with exactly one blank line.
- Use at least one flat unordered-list bullet. Do not use prose paragraphs or nested bullets.
- Explain one material behavior, implementation area, or verification change per bullet. Do not merely repeat the subject.
- Start each bullet with an imperative verb and wrap it at approximately 72 characters when practical.
- Add issue references or `BREAKING CHANGE:` footers after another blank line when applicable.

A single-line commit message is not acceptable, even for a small change.

### Example

```text
fix(chat): render persistent shell command output

- route sandbox.shell_connect through the terminal timeline presentation
- add regression coverage for command output and exit status rendering
```

Using multiple `-m` arguments creates the required subject and bullet-list body:

```bash
git commit \
  -m "fix(chat): render persistent shell command output" \
  -m $'- route sandbox.shell_connect through the terminal timeline presentation\n- add regression coverage for command output and exit status rendering'
```

## Required Checks

Before creating a commit, run the checks that apply to its changed areas:

- For Go changes, format changed files with `gofmt -w`, then run `go vet ./...` and `go test ./...`.
- For frontend changes, from `frontend/`, format changed files with Prettier, then run `pnpm format:check`, `pnpm lint`, and `pnpm test`.
- Run `pnpm build` for frontend changes that can affect production compilation or rendering.
- For documentation or configuration changes, run `git diff --check`.
