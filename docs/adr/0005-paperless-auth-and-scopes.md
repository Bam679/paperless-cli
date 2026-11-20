# ADR-0005: Paperless-ngx auth, transport and policy scopes

- Status: accepted
- Date: 2026-11-20

## Context
Unlike the Apple CLIs, `paperless` talks to a network service and needs a credential. Agents must be able to use it without ever seeing, passing or leaking the API token. Paperless has no folders, so the policy needs a different container notion, and documents can carry several tags at once.

## Decision
- **Transport:** `net/http` only. `PAPERLESS_URL` is required and must be `https`, except for `localhost`, `127.0.0.1` and `::1`. Requests send `Authorization: Token …` and `Accept: application/json; version=9` (supported by Paperless-ngx 2.x and 3.x). Cross-host or cross-scheme redirects are refused, so the header can never go elsewhere. Timeouts apply to dial, TLS and response headers, plus `--timeout`. Responses are capped (32 MiB JSON, 1 GiB downloads, 4 KiB error excerpts).
- **Token:** `PAPERLESS_TOKEN`, then the macOS Keychain item (service `paperless-cli`, account = the normalized base URL), read with `/usr/bin/security … -w` using fixed argv. Keying by URL means changing `PAPERLESS_URL` can never pull another server's token. There is no token flag or argument, and the token is redacted from every error. `/api/profile/` is never called because it echoes the token.
- **Scopes:** a document's scope strings are `tag:<name>` per tag, or `untagged`. A read requires *all* of them to be readable, so denying `tag:medical` hides a document even if it has other tags. List commands filter client-side and the cursor stores the raw server offset (mapped onto `page`/`page_size`), so hidden documents never break pagination. A call scans at most 10 server pages and then returns a cursor. Tags the API user cannot see become `tag:#<id>`, which fails closed against allowlists.
- **Writes:** `WritePlan.Scopes` holds all of the document's scopes plus every tag being added or removed, and `untagged` if the result has no tags. Uploads use the target tags, or `untagged`. Write plans also require the document to be readable, so a hidden document cannot be changed blindly. `download` is a read with its own op so it can be denied separately; it never overwrites and creates files with mode 0600.
- **Integration tests** start a pinned (by digest) Paperless-ngx + Redis with Docker Compose on a random loopback port, get a token from `POST /api/token/` with test-only credentials, and always run `down -v`. The Go test owns the compose lifecycle, so the generated `make integration` target needs no change.

## Consequences
+ The policy speaks the user's vocabulary (tags), and one deny rule protects a whole category of documents.
+ An agent can't exfiltrate the token through the CLI or redirect it to another host.
- Paperless matching rules and workflows may add tags after an upload. The policy only governs what the CLI requests.
- Client-side read filtering means a page can hold fewer than `--limit` items while `next_cursor` is still set.
- `title_content` (used by `documents --query`) is deprecated in Paperless 3.x in favour of the Tantivy `text` parameter. Switch when 2.x support is dropped.
