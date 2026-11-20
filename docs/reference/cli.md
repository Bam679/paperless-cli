# paperless CLI reference

`paperless` version 0.x. `paperless schema` prints the same information as JSON and is always current for the installed version.

## Synopsis

```text
paperless <command> [arguments] [flags]
```

Flags can appear before or after arguments. Every flag has two dashes. `--` ends flag parsing.

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

## Global flags

| Flag | Type | Description |
|---|---|---|
| `--json` | bool | Print one JSON document. |
| `--ndjson` | bool | Print one JSON object per line. |
| `--fields a,b` | string | Select and order fields. Unknown names are a usage error. |
| `--limit N` | int | Maximum number of items. Default 50, at most 1000. |
| `--cursor C` | string | Continue a list from a `next_cursor`. A cursor only works with the same command and flags. |
| `--max-chars N` | int | Cap the size of stdout. Lists drop whole items, single items shorten their longest text field. |
| `--dry-run` | bool | Writes only. Print the plan, change nothing. |
| `--confirm` | bool | Writes only. Apply the change. |
| `--timeout D` | duration | Abort after this time (default `60s`). |
| `--help` | bool | Show help. |

## Output formats

| Format | Lists | Single items |
|---|---|---|
| default | One line per item, tab-separated default fields, `-` for empty values. Trailer lines start with `#`: `# next_cursor=…`, `# truncated: …` | `field: value` lines |
| `--json` | `{"items":[…],"count":N,"next_cursor":"…","truncated":true}` | One object; `"truncated":true` when shortened |
| `--ndjson` | One object per line, then `{"_meta":{"next_cursor":…,"truncated":…}}` when relevant | One object |

Write commands print the changed items with an `op` field. With `--dry-run`, or without `--confirm`, they print the planned items with `"dry_run":true`.

Text output never contains control characters, tabs or newlines inside a value, or bidirectional override characters.

Errors are one JSON line on stderr:

```json
{"error":{"code":"policy_denied","message":"writing to \"Private\" is not allowed by policy","hint":"…","exit":4}}
```

## Exit codes

| Code | Name | Meaning |
|---|---|---|
| 0 | ok | Success. |
| 1 | internal | Unexpected error. |
| 2 | usage | Invalid arguments or flags. |
| 3 | not_found | The item or container does not exist. |
| 4 | policy_denied | The policy does not allow the operation or scope, or the policy file is invalid. No flag overrides this. |
| 5 | confirm_required | A write without `--confirm`. The plan is printed on stdout. |
| 6 | permission_denied | Missing macOS privacy permission, or invalid credentials. |
| 7 | unavailable | The backend did not answer, or the timeout expired. |
| 8 | conflict | An ambiguous name, or the item already exists. |

## Stdin

Write commands that take `-` as an argument read JSON objects from stdin: one object, an array of objects, or one object per line. Input is limited to 4 MiB, and unknown fields are rejected.

## Dates

Date flags accept `YYYY-MM-DD` (a whole day), `YYYY-MM-DDTHH:MM` and `YYYY-MM-DD HH:MM` in local time, RFC 3339, `today`, `tomorrow`, `yesterday`, `+Nd` and `-Nd`.

## Configuration

| Variable | Description |
|---|---|
| `PAPERLESS_URL` | Required. Base URL of the server, for example `https://paperless.example.com`. Plain `http` is accepted for `localhost`, `127.0.0.1` and `::1` only. |
| `PAPERLESS_TOKEN` | API token. If it is not set, the CLI reads the macOS Keychain item with service `paperless-cli` and the normalised `PAPERLESS_URL` as account. |

## Fields

| Field | Description |
|---|---|
| `id` | Paperless document id. |
| `title`, `correspondent`, `document_type`, `tags` | Document metadata (names, not ids). |
| `created`, `added`, `modified` | Dates in RFC 3339. |
| `original_file_name`, `archive_serial_number`, `page_count` | File metadata. |
| `content` | OCR text (`get --content` only). |
| `score`, `rank`, `highlights` | Search relevance (`search` only). |

Run `paperless schema` for the exact field list of each command.

## Scopes

A document's scope strings are `tag:<name>` for each of its tags, or `untagged` when it has none. A document is visible only if every one of its scope strings may be read, so `"read": {"deny": ["tag:medical"]}` hides every document tagged `medical`. A write needs write scope on all of the document's scope strings and on every tag it adds or removes. Uploads need write scope on their target tags, or on `untagged`.

## Behaviour notes

- Tags, correspondents and types accept a name (exact, or unique when compared case-insensitively) or an id. Several `--tag` flags must all match.
- `upload --wait` polls the consumption task and prints the new document id.
- `download` writes the archived PDF, or the original file with `--original`, with mode `0600`, and refuses to overwrite an existing file.
- `delete` moves documents to the Paperless trash.

## Policy file

Path: `~/.config/agent-cli/paperless.json`. The home directory comes from the user database, not from `$HOME`. No flag or environment variable selects a different file.

| Key | Type | Meaning |
|---|---|---|
| `version` | int | Must be `1`. |
| `operations.allow` | string list | `read`, `write` (every write operation), or single operations: `upload`, `update`, `delete`, `tag-create`. |
| `operations.deny` | string list | Denied operations. Deny wins over allow. |
| `scopes.read.allow` | glob list | Containers that may be read. Empty means all. |
| `scopes.read.deny` | glob list | Containers hidden from every read. |
| `scopes.write.allow` | glob list | Containers that may be changed. Empty means none. |
| `scopes.write.deny` | glob list | Containers that may never be changed. |
| `limits.max_write_batch` | int | Maximum number of items one write command may change (default 100). |

Scope strings are document tags as `tag:<name>` (e.g. `tag:invoices`, `tag:agent-*`), or `untagged` for documents without tags. In patterns `*` matches any text, including `/`; matching is case-sensitive.

Without a file, the default policy allows `read` on everything and no writes. The CLI rejects files that are not regular files, that are writable by group or others, that another user owns, or that contain unknown keys or invalid JSON. A rejected file denies every command except `schema`, `policy`, `version`, `help` and `guard`.

## Built-in commands

| Command | Description |
|---|---|
| `schema` | The machine-readable contract as JSON. |
| `policy` | The effective policy and its path. `--example` prints a template, and `--claude-settings` prints recommended Claude Code settings. |
| `guard` | Claude Code `PreToolUse` hook. It reads the hook JSON on stdin and denies tool calls that would modify agent-cli policy files. |
| `version` | Name and version. |
| `help` | Usage. |
