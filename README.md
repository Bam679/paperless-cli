# paperless

Search, read, download and file Paperless-ngx documents from the command line, built for AI coding agents.

[![ci](https://github.com/Bam679/paperless-cli/actions/workflows/ci.yml/badge.svg)](https://github.com/Bam679/paperless-cli/actions/workflows/ci.yml)
[![OpenSSF Scorecard](https://api.scorecard.dev/projects/github.com/Bam679/paperless-cli/badge)](https://scorecard.dev/viewer/?uri=github.com/Bam679/paperless-cli)
[![release](https://img.shields.io/github/v/release/Bam679/paperless-cli)](https://github.com/Bam679/paperless-cli/releases)
[![licence](https://img.shields.io/badge/licence-MIT-blue.svg)](LICENSE)

## About

`paperless` lets an AI agent search your [Paperless-ngx](https://github.com/paperless-ngx/paperless-ngx) archive, read OCR text, download documents and file new ones. It talks to the REST API with a token from your environment or the macOS Keychain, prints one compact line per document, and stays read-only until you allow writes in a policy file. The checks run inside the binary, so they apply to every agent, whether that is Claude Code, Codex, Cursor or a script.

`paperless` is one of five CLIs that share the same output format, exit codes and permission model: [apple-mail](https://github.com/Bam679/apple-mail-cli), [apple-notes](https://github.com/Bam679/apple-notes-cli), [apple-calendar](https://github.com/Bam679/apple-calendar-cli), [apple-reminders](https://github.com/Bam679/apple-reminders-cli) and [paperless](https://github.com/Bam679/paperless-cli). All of them are available from one Claude Code marketplace, [agent-cli-plugins](https://github.com/Bam679/agent-cli-plugins).

## Why a CLI instead of an MCP server

There are MCP servers for some Apple apps, but each tool call goes back through the model, and the tool definitions fill the context before the agent has done anything. With a CLI, the agent learns the commands from a short skill, filters the output with `jq` or `grep`, and chains several steps in one script, for example turning this week's events into reminders in a single run. That is faster and uses far fewer tokens. Single-purpose CLIs exist for some of these apps, but not as a set with one output format and one permission model for Mail, Notes, Calendar, Reminders and Paperless-ngx, which is what you need to let Claude manage everyday things on a Mac.

## Features

- Document lists with filters for tags, correspondents, types and dates, plus Paperless full-text search with highlights
- OCR text on request, with a size limit, and downloads that never overwrite files
- Uploading, retagging, updating and deleting documents, and creating tags
- Writes stay off until a policy file allows them; tag-based scopes can also hide documents, for example everything tagged `medical`
- The token comes from the environment or the macOS Keychain, never from a flag, and never leaves for another host
- Builds for macOS, Linux and Windows

## Installation

Requirements: macOS, Linux and Windows; amd64 and arm64.

With Homebrew:

```sh
brew install Bam679/tap/paperless
```

From a release archive (`.tar.gz`; Windows builds are `.zip`). Every release has signed checksums and SLSA build provenance, so check both before you install:

```sh
V=0.1.0 OS=darwin ARCH=arm64
base=https://github.com/Bam679/paperless-cli/releases/download/v$V
curl -fsSLO "$base/paperless_${V}_${OS}_${ARCH}.tar.gz"
curl -fsSLO "$base/checksums.txt"
curl -fsSLO "$base/checksums.txt.sigstore.json"
cosign verify-blob --bundle checksums.txt.sigstore.json \
  --certificate-identity-regexp '^https://github.com/Bam679/paperless-cli/' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com checksums.txt
shasum -a 256 --ignore-missing -c checksums.txt
gh attestation verify "paperless_${V}_${OS}_${ARCH}.tar.gz" --repo Bam679/paperless-cli
tar xzf "paperless_${V}_${OS}_${ARCH}.tar.gz" paperless && install -m 755 paperless ~/.local/bin/
```

From source: `go install github.com/Bam679/paperless-cli/cmd/paperless@latest`

### Claude Code plugin

The plugin adds a skill that teaches agents the CLI, plus a hook that stops agents from editing the policy file. Install it from one marketplace only:

```sh
# this CLI on its own
claude plugin marketplace add Bam679/paperless-cli
claude plugin install paperless@paperless-cli

# or the whole family, including the shared agent-cli skill
claude plugin marketplace add Bam679/agent-cli-plugins
claude plugin install paperless@agent-cli-plugins
```

## Usage

```sh
export PAPERLESS_URL=https://paperless.example.com       # token: PAPERLESS_TOKEN or the macOS Keychain
paperless doctor
paperless documents --tag invoices --created-after 2026-01-01 --limit 20
paperless search "insurance renewal" --json --fields id,title,score,highlights
paperless get 42 --content --max-chars 4000 --json
paperless download 42 --out ./invoice-42.pdf
paperless upload ./scan.pdf --tag agent-inbox --wait --dry-run
paperless upload ./scan.pdf --tag agent-inbox --wait --confirm
paperless update 42 --add-tag agent-reviewed --remove-tag agent-inbox --confirm
paperless documents --limit 20 --cursor "$(paperless documents --limit 20 --json | jq -r .next_cursor)"
```

New to the tool? The [getting started tutorial](docs/tutorial/getting-started.md) walks through a first read and a first write.

## Commands

| Command | Kind | Description |
|---|---|---|
| `documents [--query Q] [--tag T]… [--correspondent C] [--type T] [--created-after D] [--created-before D]` | read | documents, newest first (`--tag` repeatable, all must match) |
| `search <text>` | read | Paperless full-text search, best match first, with `score` and `highlights` |
| `get <id> [--content]` | read | one document; the OCR text only with `--content` (cap it with `--max-chars`) |
| `download <id> --out PATH [--original]` | read `download` | save the archived PDF (or the original). Never overwrites, creates the file with mode `0600` |
| `tags`, `correspondents`, `document-types` | read | lookup lists with id, name and document count |
| `doctor` | read | configuration, token source and server reachability (never prints the token) |
| `upload <file> [--title] [--tag]… [--correspondent] [--type] [--wait]` | write `upload` | upload for consumption; `--wait` polls the task and returns the new document id |
| `update <id> [--title] [--add-tag]… [--remove-tag]… [--correspondent] [--type]` | write `update` | change a document |
| `delete <id>…` | write `delete` | delete documents (Paperless moves them to its trash) |
| `tag-create <name>` | write `tag-create` | create a tag |
| `schema`, `policy`, `guard`, `version` | builtin | contract, effective policy, hook guard, version |

Tags, correspondents and types accept a name (exact, or unique case-insensitive) or an id. Dates accept `YYYY-MM-DD`, `today`, `yesterday` and `+Nd`/`-Nd`. Cursors map onto the API's `page`/`page_size`, and documents hidden by read scopes are skipped without breaking pagination.

`paperless <command> --help` shows the flags of one command. `paperless schema` prints the whole contract as JSON: commands, flags, fields, exit codes and the policy format.

## Output contract

The default output has one item per line with tab-separated fields, no header and no colour. `--json` prints one document (`{"items":[…],"count":N,"next_cursor":"…"}` for lists), and `--ndjson` prints one object per line. Errors are a single JSON line on stderr, for example `{"error":{"code":"policy_denied","message":"…","hint":"…","exit":4}}`.

Agents keep their context small with `--fields`, `--limit` (default 50, at most 1000) and `--max-chars`. When a list continues, the output ends with `# next_cursor=…` (or a `next_cursor` key), and the same command with `--cursor` returns the next page.

| Exit | Name | Meaning |
|---|---|---|
| 0 | ok | success |
| 1 | internal | unexpected error |
| 2 | usage | invalid arguments or flags |
| 3 | not_found | the item or container does not exist |
| 4 | policy_denied | the policy file does not allow this; no flag overrides it |
| 5 | confirm_required | a write without `--confirm`; the plan is printed on stdout |
| 6 | permission_denied | missing macOS privacy permission or credentials |
| 7 | unavailable | the backend did not answer in time |
| 8 | conflict | an ambiguous name, or the item already exists |

The full description is in the [CLI reference](docs/reference/cli.md).

## Permissions

`paperless` only reads until you allow writes. A write goes through three checks inside the binary:

1. The policy file `~/.config/agent-cli/paperless.json` must allow the operation and every container it touches.
2. `--dry-run` shows the planned change without making it.
3. `--confirm` applies the change. Without it the command prints the plan and exits with code 5.

A policy that allows a few writes looks like this:

```json
   {
     "version": 1,
     "operations": {
       "allow": [
         "read",
         "upload",
         "update",
         "tag-create"
       ],
       "deny": [
         "delete"
       ]
     },
     "scopes": {
       "read": {
         "allow": [
           "*"
         ],
         "deny": [
           "tag:medical",
           "tag:private*"
         ]
       },
       "write": {
         "allow": [
           "tag:agent-*",
           "tag:inbox"
         ],
         "deny": []
       }
     },
     "limits": {
       "max_write_batch": 10
     }
   }
```

Scopes are glob patterns over document tags as `tag:<name>` (e.g. `tag:invoices`, `tag:agent-*`), or `untagged` for documents without tags. Deny rules win over allow rules, and an empty `scopes.read.allow` means everything may be read. The binary reads the file from a fixed path (the home directory comes from the user database, not `$HOME`), refuses files that other users can write, and treats a broken file as "deny everything". `paperless policy` shows what is currently allowed.

[How to configure permissions](docs/how-to/configure-permissions.md) covers the policy file, Claude Code permission rules and the recommended sandbox setup.

## Configuration and credentials

`paperless` needs no OS privacy permission. It talks to your Paperless-ngx server over its REST API and is configured through two sources only:

| Setting | Source |
|---|---|
| Server | `PAPERLESS_URL` (required), e.g. `https://paperless.example.com` or `https://example.com/paperless`. It must use `https`; plain `http` is accepted only for `localhost`, `127.0.0.1` and `::1`. |
| API token | `PAPERLESS_TOKEN`, or else the macOS Keychain item with service `paperless-cli` and the normalised `PAPERLESS_URL` as account |

Store the token in the Keychain once. `security` prompts for it, so it never lands in your shell history (create the token in Paperless under Profile > API Auth Token):

```sh
security add-generic-password -s paperless-cli -a https://paperless.example.com -w
```

- The account must match the normalised URL: lower-case scheme and host, no default port, no trailing slash. `paperless doctor` shows the URL it uses and where the token came from.
- Because the item is keyed by URL, pointing `PAPERLESS_URL` at another server can never pull your token for this one.
- The token is read with `/usr/bin/security find-generic-password -s paperless-cli -a <url> -w` (fixed argv, no shell). It is sent only as `Authorization: Token …` to `PAPERLESS_URL`. Redirects to another host or scheme are refused, so it never follows a redirect elsewhere.
- There is no flag or argument for the token. It is never printed, logged or echoed in errors.
- Give the CLI a dedicated Paperless user with only the permissions the agent needs. Paperless's own object permissions apply on top of the policy file.
- Missing or rejected credentials exit with code 6, an unreachable server with code 7.

## Threat model

| Threat | Mitigation |
|---|---|
| A prompt-injected agent tries destructive writes | Writes are off by default. The binary checks an operation and scope allowlist, needs `--confirm` and limits batch sizes. |
| The agent edits its own permissions | The policy path is fixed. The plugin hook and the recommended deny rules block edits, and the Claude Code sandbox blocks writes outside the project. |
| Input turns into code | No shell and no scripts. Requests are built with `net/url` and `encoding/json` from typed parameters, and the only subprocess (`/usr/bin/security`, for the Keychain) gets fixed argv elements. |
| Text in the data steers the agent | Output is data only and control characters are stripped. The skill tells agents to treat returned content as untrusted, and read scopes can hide containers. |
| Leaking secrets or personal data | The API token comes from `PAPERLESS_TOKEN` or the macOS Keychain (keyed by the server URL), never from flags or arguments. It is never printed, logged or included in errors, and it is never sent to another host on a redirect. There is no telemetry and no network traffic except HTTPS requests to your own `PAPERLESS_URL`. Errors never contain credentials. |
| Supply chain | No third-party Go dependencies. Actions are pinned to commit SHAs. CI runs govulncheck, gosec, CodeQL, gitleaks, dependency review and OpenSSF Scorecard. Releases ship signed checksums, SBOMs and build provenance. |

The CLI cannot stop an agent that has unrestricted shell access from going around it, for example by calling `osascript` directly. The Claude Code sandbox closes that gap. [Security model](docs/explanation/security-model.md) explains the reasoning and the limits.

## Protecting PII with AI agents

Whatever an agent reads through this CLI is sent to the model provider. You can reduce that:

- Hide sensitive containers with `scopes.read.deny`. Hidden containers never appear in the output.
- Ask for less. `--fields`, `--limit` and `--max-chars` keep personal data out of the context, and the skill tells agents to use them.
- Mask personal data before it reaches the model. [PasqualePillitteri/claude-privacy-tool](https://github.com/PasqualePillitteri/claude-privacy-tool) adds an offline masking hook to Claude Code, and [gregmos/PII-Shield](https://github.com/gregmos/PII-Shield) anonymises PII through an MCP server, a skill or a CLI.
- Scan tool output with a Claude Code `PostToolUse` hook before it reaches the model. Tools that work well for this: [gitleaks/gitleaks](https://github.com/gitleaks/gitleaks) for secrets and tokens, [trufflesecurity/trufflehog](https://github.com/trufflesecurity/trufflehog) for verified secrets, and [data-privacy-stack/presidio](https://github.com/data-privacy-stack/presidio) (formerly microsoft/presidio) for names, email addresses, IBANs and phone numbers.
- Keep real data out of bug reports. Redact output or use synthetic examples.

## Documentation

- [Getting started](docs/tutorial/getting-started.md) (tutorial)
- [How to configure permissions](docs/how-to/configure-permissions.md)
- [CLI reference](docs/reference/cli.md)
- [Security model](docs/explanation/security-model.md)
- [Architecture decisions](docs/adr)

## Contributing

Contributions are welcome. [CONTRIBUTING.md](CONTRIBUTING.md) has the ground rules, and [SECURITY.md](SECURITY.md) explains how to report vulnerabilities privately.

```sh
git clone https://github.com/Bam679/paperless-cli && cd paperless-cli
lefthook install   # git hooks: gofmt, gitleaks, Conventional Commits
make check         # everything CI runs
make integration   # tests against real data in a sandbox (see CONTRIBUTING.md)
```

## Acknowledgements

Ideas, not code, came from [julianfbeck/paperless-cli](https://github.com/julianfbeck/paperless-cli) (task polling for uploads) and [marcelbrueckner/paperless-ngx-cli](https://github.com/marcelbrueckner/paperless-ngx-cli) (adding and removing tags by name or id). Both are MIT licensed.

## Licence

`paperless` is licensed under the MIT Licence. See [LICENSE](LICENSE).
