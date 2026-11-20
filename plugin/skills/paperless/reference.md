# paperless reference

## Document fields
`id, title, created, added, modified, correspondent, correspondent_id, document_type, document_type_id, tags, archive_serial_number, original_file_name, mime_type, page_count`

- `get --content` adds `content` (the OCR text). `search` adds `score`, `rank` and `highlights` (a plain-text snippet).
- `created` is the document date (`YYYY-MM-DD`). `added` and `modified` are RFC 3339 timestamps.
- `tags`, `correspondent` and `document_type` are names. Objects the API user cannot see appear as `#<id>`.
- `id` is the Paperless document id. Pass it to `get`, `download`, `update` and `delete`.

## Commands
| command | policy op | notes |
|---|---|---|
| `documents` | read | `--query` (text in title or content), `--tag` (repeatable, all must match), `--correspondent`, `--type`, `--created-after` (inclusive), `--created-before` (exclusive) |
| `search <text>` | read | Paperless full-text query syntax, e.g. `invoice AND acme`, `"exact phrase"` |
| `get <id>` | read | `--content` for the OCR text; `--max-chars` shortens it and sets `truncated` |
| `download <id> --out PATH` | download | `--original`; refuses to overwrite; mode 0600 |
| `tags`, `correspondents`, `document-types` | read | read-denied tags are omitted |
| `doctor` | read | URL, token source (`env` or `keychain`), server version |
| `upload <file>` | upload | `--title`, `--tag`…, `--correspondent`, `--type`, `--wait` |
| `update <id>` | update | `--title`, `--add-tag`…, `--remove-tag`…, `--correspondent`, `--type` |
| `delete <id>…` | delete | moves to the Paperless trash |
| `tag-create <name>` | tag-create | conflict if a tag with that name exists (case-insensitive) |

Dates accept `YYYY-MM-DD`, `today`, `yesterday`, `tomorrow` and `+Nd`/`-Nd`.

## Policy scopes (written by the user, read-only for agents)
`~/.config/agent-cli/paperless.json`. A document's scopes are `tag:<name>` for each of its tags, or `untagged`.

- **Read:** a document is visible only if *all* of its scopes are readable. `"read":{"deny":["tag:medical"]}` hides every document tagged `medical`, even if it has other tags.
- **Write:** a write needs *every* scope of the document plus every tag it adds or removes (and `untagged` if the result has no tags) to be write-allowed. Uploads need the target tags, or `untagged`.

Example: the agent may file new scans into `agent-*` tags and tag documents it can already write, but never delete:
```json
{"version":1,"operations":{"allow":["read","upload","update","tag-create"],"deny":["delete"]},"scopes":{"read":{"allow":["*"],"deny":["tag:medical"]},"write":{"allow":["tag:agent-*"]}}}
```
`paperless policy` shows the effective policy and `paperless policy --example` prints a template. Agents must not create or edit this file; ask the user instead.

## Batches
`delete` acts on several ids. `limits.max_write_batch` caps the batch. If a batch fails part-way, the error message starts with `N of M writes succeeded`.
