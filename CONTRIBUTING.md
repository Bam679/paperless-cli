# Contributing to paperless

Thank you for helping. This page covers the rules a change has to follow and how to test it.

## Rules for changes

Every write goes through a `WritePlan` from `internal/agentcli`, so the policy check, `--dry-run` and `--confirm` apply to it automatically. Please don't add flags or environment variables that change which policy is loaded or what it allows.

Input never becomes code. The Apple bridges run static JXA scripts and pass input as one JSON argument; HTTP bridges use typed parameters. If a change needs to build a script or a shell command from strings, open an issue first.

Tests, fixtures and issues contain synthetic data only, never real mail, notes, events or documents.

New dependencies need a reason in the pull request. The code currently uses the Go standard library only.

## Setting up

```sh
brew install go golangci-lint gitleaks lefthook gosec goreleaser
go install golang.org/x/vuln/cmd/govulncheck@latest
lefthook install
```

The git hooks run gofmt and gitleaks before each commit and check the commit message format.

## Testing

```sh
make check         # formatting, vet, lint, race tests, coverage >= 80 %, govulncheck, gosec, gitleaks, plugin validation
make integration   # tests against the real backend on this machine
```

Integration tests use their own sandbox container (named `agent-cli-test-…`) with a policy that allows nothing else, and remove it afterwards. The Paperless tests start a throwaway Paperless-ngx in Docker, run against it and remove it with all its data.

Golden files live in `internal/*/testdata/golden`. After an intended output change, run `go test ./internal/... -update` and review the diff before committing.

## Commits and releases

Commit messages follow [Conventional Commits](https://www.conventionalcommits.org/), for example `feat(list): add --due-before`. release-please collects them into a release pull request. Merging it tags the version, and GoReleaser publishes signed archives with SBOMs and build provenance.

## Shared code and documentation

`internal/agentcli`, `internal/osa`, the CI configuration and most of `docs/` come from the [agent-cli-plugins scaffold](https://github.com/Bam679/agent-cli-plugins/tree/main/scaffold). Change shared code there first. Tool-specific documentation lives in `.scaffold/*.md` in this repository and is rendered into the README and `docs/`.
