# ADR-0003: A policy file enforced by the binary, plus Claude Code settings as a second layer

- Status: accepted, revised 2026-11-20 after a review of comparable tools
- Date: 2026-11-20

## Context

Users need to control what an agent may do with each CLI: which operations, and in which lists, calendars, folders, mailboxes or tags. Instructions in a prompt are not a control. Claude Code permission rules match command text, and Anthropic's documentation says Bash rules are not a security boundary. Plugins cannot ship permission rules. Other agents (Codex, Cursor, Gemini CLI, Copilot) have hooks with different formats and semantics; Cursor's hooks fail open by default.

The first version of this design had a root-owned system policy layered over the user policy, a `max_results` limit and read scopes. Before the first release we asked whether this was overkill and whether a hook that each user sets up would be enough. The review compared a dozen agent-facing tools for personal data. Most enforce coarse switches inside the tool: the GitHub MCP server has a read-only mode, gogcli compiles safety profiles into the binary, and several Paperless and Slack servers keep writes off by default behind an environment variable. None of them scopes reads by container, and none has a root-owned policy layer. The [research summary](https://github.com/Bam679/agent-cli-plugins/blob/main/docs/explanation/permission-research.md) lists the sources.

## Decision

The binary enforces a policy, because that is the only layer that works the same under every agent and can check the container an item really belongs to.

- The policy lives at `~/.config/agent-cli/<bin>.json`. The home directory comes from the user database, not from `$HOME`, and no flag or environment variable selects another file.
- Without a file, the CLI can read everything and change nothing.
- The file contains `operations.allow/deny`, `scopes.read` and `scopes.write` (glob patterns over container names) and `limits.max_write_batch`. Deny wins. An empty `scopes.read.allow` means everything, so read scopes are an optional privacy filter.
- A file that is invalid, writable by others or owned by someone else is rejected, and the CLI then denies everything.
- Commands do not write directly. They return a `WritePlan`, and the shared code checks the operation, every touched container and the batch size before it looks at `--dry-run` or `--confirm`. A write without `--confirm` prints its plan and exits with code 5.

Claude Code gets a second layer. Each plugin ships a `PreToolUse` hook (`<bin> guard`) that denies Edit, Write and Bash calls touching the policy directory. This is the only protection a plugin can install without user action, and it is best effort. `<bin> policy --claude-settings` prints recommended settings: allow read commands, ask before write commands, deny reading and editing the policy directory, and enable the Bash sandbox with only this CLI excluded.

The root-owned system layer and `max_results` were removed. `--limit` has a fixed maximum of 1000 instead.

## Consequences

No flag can widen what the policy allows, and the check is the same under every agent. On its own the policy does not stop an agent with an unrestricted shell, which can call `osascript` or `curl` directly or edit the file. With the Claude Code sandbox, the CLI is the only path to the data, so the policy becomes the actual boundary. We tested this on macOS 26: an excluded `apple-reminders` could read through EventKit, while `osascript` inside the sandbox failed with error -600.
