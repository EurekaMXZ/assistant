# Contributing

## Commit Messages

Every commit must use a multiline Conventional Commit message with a subject, a blank line, and a non-empty body:

```text
<type>(<scope>): <summary>

<body>
```

### Subject

- Use a recognized type such as `feat`, `fix`, `refactor`, `test`, `docs`, `perf`, `build`, `ci`, or `chore`.
- Use a short, meaningful scope such as `chat`, `sandbox`, `workflow`, `frontend`, or `storage`.
- Write the summary in the imperative mood, keep it concise, and do not end it with a period.
- Keep the complete subject line at 72 characters or fewer.

### Body

- Separate the body from the subject with exactly one blank line.
- Explain the material behavior changed and why the change is needed. Do not merely repeat the subject.
- Wrap prose at approximately 72 characters when practical.
- Use multiple paragraphs or a flat bullet list when a commit contains several related changes.
- Add issue references or `BREAKING CHANGE:` footers after another blank line when applicable.

A single-line commit message is not acceptable, even for a small change.

### Example

```text
fix(chat): render persistent shell command output

Route sandbox.shell_connect through the terminal timeline presentation while
preserving legacy sandbox.exec support. Add regression coverage for command
output and exit status rendering.
```

Using multiple `-m` arguments creates the required subject and body paragraphs:

```bash
git commit \
  -m "fix(chat): render persistent shell command output" \
  -m "Route sandbox.shell_connect through the terminal timeline presentation while preserving legacy sandbox.exec support. Add regression coverage for command output and exit status rendering."
```
