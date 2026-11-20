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
