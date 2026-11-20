# ADR-0001: Go, no dependencies, and a shared core copied into each repo

- Status: accepted
- Date: 2026-11-20

## Context

Five CLIs (Apple Mail, Notes, Calendar, Reminders and Paperless-ngx) have to behave the same way for AI agents: the same flags, output, exit codes and permission checks. Each one should still be installable and auditable on its own.

## Decision

The CLIs are written in Go, which gives a single static binary, fast start-up, built-in fuzzing and `govulncheck`. They use only the standard library: no cobra or viper, no YAML or TOML. Policy files are JSON.

The shared code lives in two packages. `internal/agentcli` handles argument parsing, the output format, exit codes, the policy, the write gate, `schema` and `guard`. `internal/osa` runs JXA scripts for the Apple tools. The canonical copy is in `agent-cli-plugins/scaffold/go`, and `scaffold/render.py` copies it into each repository together with CI, release and documentation templates. No repository depends on another one at build time.

Each tool talks to its backend through a Go interface. Unit tests use a fake implementation; integration tests (`-tags integration`) use the real one against sandbox data.

## Consequences

The only supply-chain dependency is the Go toolchain, and every tool behaves the same for an agent. In return, a fix to the shared code has to be rendered into five repositories. `render.py --check` (`make drift`) finds repositories that have fallen behind. A shared Go module would remove the copies; it is listed as a follow-up.
