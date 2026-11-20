# paperless

[![ci](https://github.com/Bam679/paperless-cli/actions/workflows/ci.yml/badge.svg)](https://github.com/Bam679/paperless-cli/actions/workflows/ci.yml)
[![OpenSSF Scorecard](https://api.scorecard.dev/projects/github.com/Bam679/paperless-cli/badge)](https://scorecard.dev/viewer/?uri=github.com/Bam679/paperless-cli)
[![release](https://img.shields.io/github/v/release/Bam679/paperless-cli)](https://github.com/Bam679/paperless-cli/releases)

**Paperless-ngx for AI coding agents.** A small, dependency-free Go CLI with compact, machine-readable output. It is **read-only by default**, and writes must be allowed by a policy file that the binary enforces. It also ships as a Claude Code plugin with a skill that teaches agents how to use it.

Part of the agent-cli family: [apple-mail](https://github.com/Bam679/apple-mail-cli) · [apple-notes](https://github.com/Bam679/apple-notes-cli) · [apple-calendar](https://github.com/Bam679/apple-calendar-cli) · [apple-reminders](https://github.com/Bam679/apple-reminders-cli) · [paperless](https://github.com/Bam679/paperless-cli). All of them are available from one marketplace: [agent-cli-plugins](https://github.com/Bam679/agent-cli-plugins).

## Install

**Binary** (macOS, Linux and Windows; amd64 and arm64): download from [Releases](https://github.com/Bam679/paperless-cli/releases) (`.tar.gz`; Windows: `.zip`), verify it, and put it on your `PATH`:

```sh
V=0.1.0; OS=darwin; ARCH=arm64
curl -fsSLO https://github.com/Bam679/paperless-cli/releases/download/v$V/paperless_${V}_${OS}_${ARCH}.tar.gz
curl -fsSLO https://github.com/Bam679/paperless-cli/releases/download/v$V/checksums.txt
curl -fsSLO https://github.com/Bam679/paperless-cli/releases/download/v$V/checksums.txt.sigstore.json
cosign verify-blob --bundle checksums.txt.sigstore.json \
  --certificate-identity-regexp '^https://github.com/Bam679/paperless-cli/' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com checksums.txt
shasum -a 256 --ignore-missing -c checksums.txt
gh attestation verify paperless_${V}_${OS}_${ARCH}.tar.gz --repo Bam679/paperless-cli   # SLSA provenance
tar xzf paperless_${V}_${OS}_${ARCH}.tar.gz paperless && install -m 755 paperless ~/.local/bin/
```

**From source:** `go install github.com/Bam679/paperless-cli/cmd/paperless@latest`

**Claude Code plugin** (skill + guard hook). Install it from one source only:

```sh
# just this CLI
claude plugin marketplace add Bam679/paperless-cli
claude plugin install paperless@paperless-cli
# or everything, including the shared agent-cli skill
claude plugin marketplace add Bam679/agent-cli-plugins
claude plugin install paperless@agent-cli-plugins
```

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

Run `paperless <command> --help` for flags, or `paperless schema` for the full machine-readable contract (commands, flags, fields, exit codes, policy).

## Agent examples

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

## Output contract

| Mode | Shape |
|---|---|
| default | One item per line, tab-separated default fields, `-` for empty values, no header, no color. Trailer lines start with `#` (`# next_cursor=…`, `# truncated: …`). |
| `--json` | Lists: `{"items":[…],"count":N,"next_cursor":"…","truncated":true}`. Single items: one object. |
| `--ndjson` | One object per line, plus an optional final `{"_meta":{"next_cursor":…,"truncated":…}}`. |
| errors | Exactly one JSON line on **stderr**: `{"error":{"code":"policy_denied","message":"…","hint":"…","exit":4}}` |

- `--fields a,b`: choose and order fields (validated). `--limit N` (default 50, capped by policy). `--cursor C` continues from `next_cursor`. Cursors are opaque and bound to their query.
- `--max-chars N` caps stdout. Lists drop whole items; single items shorten their longest text field and set `"truncated":true`.
- IDs are stable native identifiers and can be passed back to `get`, `update` and `delete`.
- Stdin: write commands that show `<input>` accept `-` plus JSON / NDJSON objects (max 4 MiB, unknown fields rejected).
- Control characters and bidi overrides are stripped from text output, so a value can never break the one-line-per-item contract or inject terminal escapes.

| Exit | Name | Meaning |
|---|---|---|
| 0 | ok | success |
| 1 | internal | unexpected error |
| 2 | usage | invalid arguments or flags |
| 3 | not_found | item or container does not exist |
| 4 | policy_denied | denied by the policy file (flags cannot override this) |
| 5 | confirm_required | write without `--confirm` (preview with `--dry-run`) |
| 6 | permission_denied | macOS privacy permission (TCC) or authentication failure |
| 7 | unavailable | backend not reachable or timed out |
| 8 | conflict | ambiguous name or already exists |

## Permissions and policy

Writes pass through **three gates, in this order**. Each one is enforced in the binary.

1. **Policy file.** With no policy file, `paperless` is read-only. To allow writes, create `~/.config/agent-cli/paperless.json` (mode `600`):

   ```json
   {
     "version": 1,
     "operations": { "allow": ["read", "upload", "update", "tag-create"], "deny": ["delete"] },
     "scopes": {
       "read":  { "allow": ["*"], "deny": ["tag:medical", "tag:private*"] },
       "write": { "allow": ["tag:agent-*", "tag:inbox"], "deny": [] }
     },
     "limits": { "max_results": 200, "max_write_batch": 10 }
   }
   ```

   - `operations.allow`: `read`, `write` (all writes) or single ops (`upload`, `update`, `delete`, `tag-create`). `deny` always wins.
   - `scopes.read` / `scopes.write`: glob patterns (`*` wildcard) over document tags as `tag:<name>` (e.g. `tag:invoices`, `tag:agent-*`), or `untagged` for documents without tags. Read-deny hides containers from lists and blocks `get`.
   - `limits.max_results` caps `--limit`. `limits.max_write_batch` caps the number of items per write.
   - **Not bypassable:** the path is fixed. The home directory comes from the user database, not `$HOME`, and no flag or env var selects another file. Invalid, group- or world-writable, or foreign-owned files make the CLI **fail closed** (exit 4).
   - **Admin lock:** `/Library/Application Support/agent-cli/paperless.json` (macOS) or `/etc/agent-cli/paperless.json` must be owned by root. It is intersected with the user file, so it can only restrict further, and an agent running as you cannot change it.
   - `paperless policy` shows the effective layers, and `paperless policy --example` prints a template.
2. **`--dry-run`** prints the planned change (policy-checked as well) and changes nothing.
3. **`--confirm`** is required to change data. Without it you get exit 5.

**Claude Code rules (defence in depth).** The plugin's `PreToolUse` hook (`paperless guard`) blocks Edit, Write and Bash calls that try to modify agent-cli policy files. To get recommended permission rules (read commands allowed, writes ask, policy files denied), run the command below and merge its output into `~/.claude/settings.json`:

```sh
paperless policy --claude-settings
```

## Configuration and credentials

`paperless` needs no OS privacy permission. It talks to your Paperless-ngx server over its REST API and is configured through two sources only:

| Setting | Source |
|---|---|
| Server | `PAPERLESS_URL` (required), e.g. `https://paperless.example.com` or `https://example.com/paperless`. It must use `https`; plain `http` is accepted only for `localhost`, `127.0.0.1` and `::1`. |
| API token | 1. `PAPERLESS_TOKEN`, then 2. the macOS Keychain item with service `paperless-cli` and account = the normalized `PAPERLESS_URL` |

Store the token in the Keychain once. `security` prompts for it, so it never lands in your shell history (create the token in Paperless under *Profile > API Auth Token*):

```sh
security add-generic-password -s paperless-cli -a https://paperless.example.com -w
```

- The account must match the normalized URL: lower-case scheme and host, no default port, no trailing slash. `paperless doctor` shows the URL it uses and where the token came from.
- Because the item is keyed by URL, pointing `PAPERLESS_URL` at another server can never pull your token for this one.
- The token is read with `/usr/bin/security find-generic-password -s paperless-cli -a <url> -w` (fixed argv, no shell). It is sent only as `Authorization: Token …` to `PAPERLESS_URL`. Redirects to another host or scheme are refused, so it never follows a redirect elsewhere.
- There is no flag or argument for the token. It is never printed, logged or echoed in errors.
- Give the CLI a dedicated Paperless user with only the permissions the agent needs. Paperless's own object permissions apply on top of the policy file.
- Missing or rejected credentials exit with code 6, an unreachable server with code 7.

## Threat model

| Threat | Mitigation |
|---|---|
| Prompt-injected agent tries destructive writes | Read-only default. Binary-enforced policy with op and scope allowlists, `--confirm`, batch limits, no bypass flags. |
| Agent edits its own policy | Fixed path ignoring `$HOME`, ownership and permission checks, guard hook, recommended deny rules, root-owned system policy for hard guarantees. |
| Script or command injection through data | No shell and no scripts. Requests are built with `net/url` and `encoding/json` from typed parameters, and the only subprocess (`/usr/bin/security`, for the Keychain) gets fixed argv elements. |
| Malicious content in data steering the agent | Output is data only. Text is sanitized (no control or escape sequences). Read scopes can hide sensitive containers. Treat all returned content as untrusted. |
| Secret or PII leakage | The API token comes from `PAPERLESS_TOKEN` or the macOS Keychain (keyed by the server URL), never from flags or arguments. It is never printed, logged or included in errors, and it is never sent to another host on a redirect. No telemetry, no network calls except HTTPS requests to your own `PAPERLESS_URL`. Errors never echo credentials. |
| Supply chain | Zero third-party Go dependencies. SHA-pinned actions, govulncheck, gosec, CodeQL, gitleaks, dependency review, OpenSSF Scorecard. Releases signed with cosign, with SBOMs and SLSA provenance. |

Out of scope: an agent with unrestricted shell access and your full user privileges can still act outside this CLI (for example by calling `osascript` directly). Use the Claude Code sandbox and permission rules to constrain the agent itself.

## Protecting PII with AI agents

Everything an agent reads through this CLI is sent to the model provider. Reduce what leaves your machine:

- **Scope reads.** Deny sensitive containers in `scopes.read.deny` (for example private folders, health or finance tags). Hidden containers never reach the agent.
- **Keep outputs small.** Prefer `--fields` and `--limit`, and use `--max-chars` instead of dumping full bodies.
- **Add hooks that scan before data leaves.** A Claude Code `PostToolUse` hook can pipe tool output through a detector and block or redact it:
  - [gitleaks/gitleaks](https://github.com/gitleaks/gitleaks): secrets (API keys, tokens) in text and repos
  - [trufflesecurity/trufflehog](https://github.com/trufflesecurity/trufflehog): verified secret detection
  - [data-privacy-stack/presidio](https://github.com/data-privacy-stack/presidio) (formerly microsoft/presidio): PII detection and anonymization (names, emails, IBANs, phone numbers)
- **Never paste outputs into issues.** Bug reports should contain redacted or synthetic data only.
- **Review the policy regularly.** `paperless policy` shows exactly what the agent may touch.

## Development

```sh
make check        # fmt, vet, golangci-lint, race tests + coverage gate, govulncheck, gosec, gitleaks, plugin validate
make integration  # real sandbox tests on this machine
lefthook install  # git hooks (gofmt, gitleaks, Conventional Commits)
```

Decisions are recorded in [docs/adr](docs/adr). See [CONTRIBUTING.md](CONTRIBUTING.md), [SECURITY.md](SECURITY.md) and [AGENTS.md](AGENTS.md).

## License

[MIT](LICENSE)
