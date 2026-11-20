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
