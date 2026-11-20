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
