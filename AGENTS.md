# paperless-cli: agent guide

Go CLI `paperless` for Paperless-ngx, built for AI agents. Standard library only.

## Layout
- `cmd/paperless/`: main (wiring only)
- `internal/paperless/`: commands (`commands.go`), the bridge interface and its implementation, tests
- `internal/agentcli/`: shared core (flags, output, exit codes, policy, write gate). Synced from agent-cli-plugins/scaffold, so edit it there.
- `plugin/`: the Claude Code plugin (`skills/paperless/` skill, `hooks/` guard hook)
- `docs/`: tutorial, how-to, reference, explanation (Diataxis) and `adr/`. Generated from `.scaffold/` snippets, so edit those.

## Commands
- `make check`: everything CI runs (fmt, vet, lint, race tests, coverage >= 80%, govulncheck, gosec, gitleaks, plugin validate)
- `make agent-check`: fast gate, also run by the Stop hook
- `make integration`: real sandbox tests (`-tags integration`). They create and remove their own data.
- `go test ./internal/paperless/ -update`: rewrite golden files, then review the diff

## Hard rules (CI, hooks and reviews enforce them)
1. Every write is a `WritePlan` returned from `Plan`. Never call bridge write methods from `Run`.
2. Never add flags or env vars that select or widen the policy. The paths are fixed.
3. Never build scripts or shell commands from strings. Input goes in as JSON argv (osa) or typed params.
4. No real personal data in fixtures, goldens, logs or commits. Use synthetic names only.
5. Integration tests touch only sandbox containers named `agent-cli-test-*`. They never send mail.
6. Keep stdout machine-readable. Diagnostics go to stderr as one JSON line.
7. Contract changes (commands, fields, exit codes) must update the `schema` goldens, the skill and the `.scaffold` snippets.
8. Use Conventional Commits. Don't use `--no-verify`, force-pushes, or new dependencies without an ADR.

## Style
- Match the surrounding code. Keep comments sparse and only where the reason is non-obvious.
- Errors: `agentcli.Usagef/NotFoundf/...`. Exit codes are public API, so never renumber them.
