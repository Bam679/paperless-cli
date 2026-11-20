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
