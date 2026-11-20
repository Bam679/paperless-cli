# ADR-0003: Permission model: binary-enforced policy file + Claude Code rules as defence in depth

- Status: accepted
- Date: 2026-11-20

## Context
Users must control what an agent may do per CLI (operations, accounts, folders, calendars, lists, tags). Prompt instructions are not a control. Claude Code permission rules match command strings and can be sidestepped (aliases, scripts). Plugins cannot ship permission rules. Policy denials must not be bypassable via flags.

## Decision
**Layer 1, enforced in the binary (authoritative):**
- Policy JSON at fixed paths only: system `/Library/Application Support/agent-cli/<bin>.json` (macOS) or `/etc/agent-cli/<bin>.json`, and user `~/.config/agent-cli/<bin>.json`. Home comes from the user database (`os/user`), not `$HOME`. There is no flag or env var to choose another file.
- Without a user file, the built-in default is read-only (`operations.allow=["read"]`, write scopes empty).
- Schema `{version, operations{allow,deny}, scopes{read,write}{allow,deny}, limits{max_results,max_write_batch}}`. Ops are `read`, `write` or concrete op names. Scopes are glob patterns over container names (list, calendar, `Account/Folder`, `Account/Mailbox`, `tag:<name>`).
- Layers intersect, and deny wins. A root-owned system file can only restrict. Files must be regular, owned by the expected uid (root for system) and not group/world-writable. Unknown keys, bad JSON or a bad version reject the policy, and the CLI fails closed (exit 4).
- Write gate in `agentcli.App` (not in commands): commands return a `WritePlan{Scopes,Targets,Apply}`. The App checks op, then every scope, then the batch limit, then `--dry-run` (preview only), then `--confirm`. `--dry-run` is policy-checked too. There is no `--force`, and no command can reach `Apply` past the gate.
- Read scopes filter list output and deny `get` on excluded containers (useful for PII: e.g. `"read":{"deny":["iCloud/Private*","tag:medical"]}`).

**Layer 2, Claude Code (defence in depth):**
- Each plugin ships a `PreToolUse` hook running `<bin> guard`. It denies Edit/Write/Bash calls that touch the policy directories, so the agent cannot rewrite its own policy.
- `<bin> policy --claude-settings` prints recommended `permissions` rules (allow read subcommands, `ask` for write subcommands, deny Read/Edit of the policy dirs) for users to paste into `~/.claude/settings.json`.

## Consequences
+ Flags can never widen permissions. The worst an agent can do is what the policy file allows.
+ Admins can lock policy with a root-owned system file, which an agent running as the user cannot modify.
- An agent with unrestricted shell access as the user can still edit the user policy file through obfuscated commands. The guard hook is best effort, so for hard guarantees use the system file or the Claude Code sandbox.
