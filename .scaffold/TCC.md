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
