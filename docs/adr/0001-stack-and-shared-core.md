# ADR-0001: Go, zero dependencies, shared core copied per repo

- Status: accepted
- Date: 2026-11-20

## Context
Five CLIs (Apple Mail, Notes, Calendar, Reminders, Paperless-ngx) must behave identically for AI agents (flags, output, exit codes, policy) while each stays independently installable and auditable.

## Decision
- Go (single static binary, fast start, good fuzzing, `govulncheck`). Standard library only: no cobra/viper, no YAML/TOML; policy files are JSON.
- One shared package `internal/agentcli` (argument parsing, output contract, exit codes, policy, write gate, `schema`, `guard`) and, for the Apple tools, `internal/osa` (JXA runner). The canonical copy lives in `agent-cli-plugins/scaffold/go`; `scripts/sync.sh` copies it into each repo. Each repo vendors the code, so it has no cross-repo module dependency.
- The OS or HTTP bridge sits behind a Go interface per tool. Unit tests use fakes, and integration tests (`-tags integration`) use the real bridge against sandbox data.

## Consequences
+ Zero third-party runtime dependencies, so the supply-chain surface is the Go toolchain.
+ Identical agent contract everywhere, and a `make drift` check detects divergence.
- Fixes to the core must be synced to 5 repos. This is mitigated by the sync script and the identical tests that ship with the core.
