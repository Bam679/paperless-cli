# Contributing

Thanks for helping! Ground rules:

1. **Security first.** Reads are default. Every write must be a `WritePlan` (`internal/agentcli`) so the policy, `--dry-run` and `--confirm` gates apply. Never add flags or env vars that widen permissions.
2. **No string-built scripts.** Apple bridges use static JXA with JSON argv (`internal/osa`). HTTP bridges use typed parameters.
3. **No personal data** in fixtures, tests, issues or commits. Use synthetic data only.
4. **Minimal dependencies.** The standard library is the default. A new module needs a reason in the PR.

## Setup
```sh
brew install go golangci-lint gitleaks lefthook gosec goreleaser
go install golang.org/x/vuln/cmd/govulncheck@latest
lefthook install          # pre-commit: gofmt, gitleaks; commit-msg: Conventional Commits
make check                # everything CI runs
make integration          # real sandbox tests on macOS (creates and removes its own data)
```

## Commits and releases
Use [Conventional Commits](https://www.conventionalcommits.org/). release-please opens the release PR, and merging it tags `vX.Y.Z` and runs GoReleaser (signed checksums, SBOMs, provenance).

## Shared code
`internal/agentcli` (and `internal/osa`) come from [agent-cli-plugins/scaffold](https://github.com/Bam679/agent-cli-plugins/tree/main/scaffold). Change them there first, then sync.
