## What and why

## Checklist
- [ ] Title follows Conventional Commits (`feat:`, `fix:`, `docs:` …)
- [ ] `make check` passes locally
- [ ] Writes go through `WritePlan` (policy + `--dry-run` + `--confirm`), no new bypass flags
- [ ] No string-built scripts; input only via JSON argv / typed API params
- [ ] Tests (fake bridge + golden) added or updated; no personal data in fixtures
- [ ] `schema`, skill and README updated if the CLI contract changed
