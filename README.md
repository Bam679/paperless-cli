# paperless

Search, read, download and file Paperless-ngx documents from the command line, built for AI coding agents.

[![ci](https://github.com/Bam679/paperless-cli/actions/workflows/ci.yml/badge.svg)](https://github.com/Bam679/paperless-cli/actions/workflows/ci.yml)
[![release](https://img.shields.io/github/v/release/Bam679/paperless-cli)](https://github.com/Bam679/paperless-cli/releases)
[![OpenSSF Scorecard](https://api.scorecard.dev/projects/github.com/Bam679/paperless-cli/badge)](https://scorecard.dev/viewer/?uri=github.com/Bam679/paperless-cli)
[![licence](https://img.shields.io/badge/licence-MIT-blue.svg)](LICENSE)

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="assets/logo-dark.svg">
  <img alt="paperless logo: the agent-cli bracket mark with the paperless glyph" src="assets/logo.svg" height="56">
</picture>

`paperless` talks to the Paperless-ngx REST API with a token from the environment or the macOS Keychain, and prints one line per document or JSON. It connects to your server and nowhere else. Writes stay off until a policy file allows them, and tag-based scopes can hide whole categories of documents from the agent.



## Why a CLI instead of an MCP server

An MCP server loads its tool definitions into the agent's context in every session, and each result travels back through the model before the agent can act on it. A CLI costs nothing until it is called. The agent learns it from a short skill, filters the output with `jq` or `grep`, and chains several steps in one shell script, for example turning this week's events into reminders in a single run.

`paperless` is one of five CLIs with the same output format, exit codes and permission model: [apple-mail](https://github.com/Bam679/apple-mail-cli), [apple-notes](https://github.com/Bam679/apple-notes-cli), [apple-calendar](https://github.com/Bam679/apple-calendar-cli), [apple-reminders](https://github.com/Bam679/apple-reminders-cli) and [paperless](https://github.com/Bam679/paperless-cli). Together they let an agent manage everyday things on a Mac with one set of rules. If your agent cannot run shell commands, an MCP server is the better fit.

## Install

1. Install the CLI:

   ```sh
   brew install Bam679/tap/paperless
   ```

   Signed release archives and `go install` are described in [How to install and verify a release](docs/how-to/install-and-verify.md).

2. Check that it can reach your Paperless-ngx server:

   ```sh
   paperless doctor
   ```

   It reports the server URL, where the token came from and whether the server answered. It never prints the token. See [Security and permissions](#security-and-permissions).

3. Connect Claude Code. The plugin adds a skill that teaches the agent the CLI, and a hook that stops the agent from editing the policy file:

   ```sh
   claude plugin marketplace add Bam679/paperless-cli
   claude plugin install paperless@paperless-cli
   ```

   To get all five tools and the shared `agent-cli` skill from one place, use the [agent-cli-plugins](https://github.com/Bam679/agent-cli-plugins) marketplace instead. Install each plugin from one marketplace only.

To remove everything: `claude plugin uninstall paperless`, `brew uninstall paperless`, and delete `~/.config/agent-cli/paperless.json` if you created one.

## Quick start

```sh
export PAPERLESS_URL=https://paperless.example.com
security add-generic-password -s paperless-cli -a "$PAPERLESS_URL" -w   # asks for the API token
paperless doctor
```

Without `PAPERLESS_URL`, `doctor` says so instead of guessing:

```text
server	false	PAPERLESS_URL is not set
```

```sh
paperless documents --tag invoices --created-after 2026-01-01 --limit 20
paperless search "insurance renewal" --json --fields id,title,score
paperless get 42 --content --max-chars 4000
paperless download 42 --out ./invoice-42.pdf          # never overwrites, mode 0600
```

The [tutorial](docs/tutorial/getting-started.md) continues from here to a first write in a container made for the exercise.

## Use it with your agent

With the plugin installed, Claude Code picks up the skill on its own. For other agents (Codex, Cursor, Gemini CLI), add this to `AGENTS.md`:

```markdown
Use `paperless` for the user's Paperless-ngx documents. Run `paperless schema` once to learn the commands.
Writes need the user's policy file: preview with `--dry-run`, then run with `--confirm`
only after the user agreed. On exit code 4, stop and tell the user.
```

What an agent can rely on: the default output has one item per line with tab-separated fields; `--json` prints `{"items":[…],"count":N,"next_cursor":"…"}` and `--ndjson` one object per line. `--fields`, `--limit` (default 50, at most 1000) and `--max-chars` keep output small, and `--cursor` continues a list. Errors are one JSON line on stderr, and exit codes are fixed:

| Exit | Meaning |
|---|---|
| 0 | success |
| 1 | unexpected error |
| 2 | invalid arguments or flags |
| 3 | item or container not found |
| 4 | denied by the policy; no flag overrides this |
| 5 | write without `--confirm`; the plan is printed, nothing changed |
| 6 | missing macOS permission or credentials |
| 7 | backend did not answer in time |
| 8 | ambiguous name, or the item already exists |

The [CLI reference](docs/reference/cli.md) has every command, flag, field and the JSON shapes.

## How it works

`paperless` builds every request from typed parameters with Go's `net/http` and `net/url`; there is no shell and no script. Document scopes are the document's tags (`tag:<name>`), so one deny rule such as `tag:medical` hides a whole category from lists, searches and downloads. Every write command first builds a plan. The binary checks the plan against the policy file, then prints it (`--dry-run`, or no `--confirm`) or applies it (`--confirm`). [Security model](docs/explanation/security-model.md) explains why the checks live in the binary.

## When not to use it

- You manage users, permissions, workflows or mail rules in Paperless. The CLI covers documents, tags, correspondents and types only.
- You run bulk edits over thousands of documents. Use the Paperless web interface or its bulk-edit API; the CLI caps write batches.
- Your agent cannot run shell commands. An MCP server fits better there.

## Security and permissions

`paperless` only reads until you allow writes in `~/.config/agent-cli/paperless.json`:

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

Operations are `read`, `write` (all writes) or single operations: `upload`, `update`, `delete`, `tag-create`. Scopes are glob patterns over document tags as `tag:<name>` (e.g. `tag:invoices`, `tag:agent-*`), or `untagged` for documents without tags. Deny wins over allow, and an empty `scopes.read.allow` means everything may be read. `paperless policy` shows what is allowed now.

The binary reads the policy from that fixed path only. It takes the home directory from the user database, not from `$HOME`, and no flag or environment variable points it elsewhere. A file that is invalid, writable by other users or owned by someone else makes every command fail with exit code 4.

`paperless policy --claude-settings` prints Claude Code settings that allow read commands, ask before every write, deny access to the policy directory, and turn on the Bash sandbox with only `paperless` excluded. In the sandbox the agent cannot reach the Paperless API with `curl`, the network or the policy file directly, so `paperless` and its policy are the only way to the data. [How to configure permissions](docs/how-to/configure-permissions.md) walks through it.

### Credentials

`paperless` needs no macOS privacy permission. It reads the server from `PAPERLESS_URL` and the API token from `PAPERLESS_TOKEN`, or on macOS from the Keychain item with service `paperless-cli` and the normalised server URL as account. Because the Keychain item is tied to the URL, pointing `PAPERLESS_URL` at another server can't pull your token for this one. There is no flag or argument for the token, and it never appears in output or errors. Give the CLI a dedicated Paperless user with only the permissions the agent needs; Paperless applies its own object permissions on top of the policy file. Missing or rejected credentials exit with code 6, and an unreachable server with code 7.

### What leaves your machine

`paperless` talks to one host: the server in `PAPERLESS_URL`, over HTTPS (plain HTTP only for `localhost`). It refuses redirects to other hosts, so the token never goes anywhere else. On macOS it also reads the token from the Keychain with `/usr/bin/security`. There is no telemetry and no update check. Whatever an agent reads through `paperless` is sent to the model provider by the agent, like any other tool output.

### Threat model

| Threat | Mitigation |
|---|---|
| A prompt-injected agent tries destructive writes | Writes are off by default. The binary checks an operation and scope allowlist, needs `--confirm` and limits batch sizes. |
| The agent edits its own permissions | The policy path is fixed. The plugin hook and the recommended deny rules block edits, and the sandbox blocks writes outside the project. |
| Input turns into code | No shell and no scripts. Requests are built with `net/url` and `encoding/json` from typed parameters, and the only subprocess (`/usr/bin/security`, for the Keychain) gets fixed argv elements. |
| Text in the data steers the agent | Output is data only and control characters are stripped. The skill tells agents to treat returned content as untrusted, and read scopes can hide containers. |
| Secrets or personal data leak | The API token comes from `PAPERLESS_TOKEN` or the macOS Keychain (keyed by the server URL), never from flags or arguments. It is never printed, logged or included in errors, and it is never sent to another host on a redirect. Errors never contain credentials. |
| Supply chain | No third-party Go dependencies. Actions are pinned to commit SHAs. CI runs govulncheck, gosec, CodeQL, gitleaks, dependency review and OpenSSF Scorecard. Releases ship signed checksums, SBOMs and build provenance. |

An agent with unrestricted shell access can go around any CLI, for example by calling `osascript` or `curl` directly. Only the sandbox closes that gap.

### Protecting PII with AI agents

- Hide sensitive containers with `scopes.read.deny`. Hidden containers never appear in the output.
- Ask for less. `--fields`, `--limit` and `--max-chars` keep personal data out of the context, and the skill tells agents to use them.
- Mask personal data before it reaches the model. [PasqualePillitteri/claude-privacy-tool](https://github.com/PasqualePillitteri/claude-privacy-tool) adds an offline masking hook to Claude Code, and [gregmos/PII-Shield](https://github.com/gregmos/PII-Shield) anonymises PII through an MCP server, a skill or a CLI.
- Scan tool output with a `PostToolUse` hook: [gitleaks/gitleaks](https://github.com/gitleaks/gitleaks) and [trufflesecurity/trufflehog](https://github.com/trufflesecurity/trufflehog) find secrets, and [data-privacy-stack/presidio](https://github.com/data-privacy-stack/presidio) (formerly microsoft/presidio) finds names, addresses, IBANs and phone numbers.
- Keep real data out of bug reports. Redact output or use synthetic examples.

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

## Documentation

| Page | Use it when |
|---|---|
| [Getting started](docs/tutorial/getting-started.md) | you are new and want a first read and a first write |
| [How to configure permissions](docs/how-to/configure-permissions.md) | you want an agent to write, or to hide private data |
| [How to install and verify a release](docs/how-to/install-and-verify.md) | you install without Homebrew or want to check signatures |
| [CLI reference](docs/reference/cli.md) | you need a flag, a field, an exit code or the policy format |
| [Security model](docs/explanation/security-model.md) | you want to know why the checks work this way and where they end |
| [Architecture decisions](docs/adr) | you want the reasoning behind the design |

## Contributing

[CONTRIBUTING.md](CONTRIBUTING.md) has the rules for changes, and [SECURITY.md](SECURITY.md) explains how to report a vulnerability privately.

```sh
git clone https://github.com/Bam679/paperless-cli && cd paperless-cli
lefthook install   # gofmt, gitleaks and commit-message checks
make check         # everything CI runs
make integration   # tests against the real backend on this machine
```

## Acknowledgements

Ideas, not code, came from [julianfbeck/paperless-cli](https://github.com/julianfbeck/paperless-cli) (task polling for uploads) and [marcelbrueckner/paperless-ngx-cli](https://github.com/marcelbrueckner/paperless-ngx-cli) (adding and removing tags by name or id). Both are MIT licensed.

## Licence

MIT. See [LICENSE](LICENSE).
