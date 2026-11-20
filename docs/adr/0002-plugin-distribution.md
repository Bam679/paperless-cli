# ADR-0002: Plugin distribution: per-CLI plugins + umbrella marketplace with one shared skill

- Status: accepted
- Date: 2026-11-20

## Context
Agents need to learn each CLI, but users may install only some CLIs. Claude Code (>= 2.1.3xx) supports plugins (`.claude-plugin/plugin.json`), marketplaces (`.claude-plugin/marketplace.json`) whose entries can point to other GitHub repos (`{"source":"github","repo":...,"ref":...}`), skills with progressive disclosure (`SKILL.md` + reference files loaded on demand), and plugin hooks (`hooks/hooks.json`). Plugins cannot ship permission rules (see ADR-0003).

## Options
1. One monorepo: a single plugin containing all CLIs. Simple, but it couples releases, and installing one CLI's skill means installing all of them.
2. Per-CLI plugins only. Independent, but there's no single entry point and the shared contract gets duplicated in every skill.
3. **Per-CLI plugins plus an umbrella marketplace that also ships a shared `agent-cli` skill.**

## Decision
Option 3.
- Every CLI repo contains its plugin in `plugin/` (plugin name = binary name, e.g. `apple-reminders`). Keeping it out of the repo root keeps contributor files (`CLAUDE.md`, `.claude/`) out of the plugin. The repo root holds a single-plugin marketplace (`source: "./plugin"`), so each CLI is installable alone:
  `claude plugin marketplace add Bam679/apple-reminders-cli && claude plugin install apple-reminders@apple-reminders-cli`.
  It ships a tool skill (`plugin/skills/<bin>/SKILL.md` under 150 lines, plus `reference.md`) and a `PreToolUse` guard hook (`<bin> guard`).
- `Bam679/agent-cli-plugins` is the umbrella marketplace. It lists all five CLI plugins as `git-subdir` sources (`path: plugin`) pinned to release tags (`ref: vX.Y.Z`), plus the `agent-cli` plugin with one shared skill:
  - `SKILL.md` covers the common contract (output, exit codes, write gate, policy) once.
  - `references/<bin>.md` is one file per CLI and is only read when that CLI is needed (progressive disclosure).
  - A `SessionStart` hook (`scripts/detect.sh`) prints which CLIs are on `PATH` and their versions, so the agent knows deterministically what is installed instead of guessing.
- Versions: `plugin.json` `version` is bumped by release-please together with the binary. The umbrella pins tags and is bumped by PR (`scripts/bump.sh`).
- CI runs `claude plugin validate . --strict` for every plugin and for the marketplace, and an install smoke test (`marketplace add` + `plugin install` from a local checkout).

## Consequences
+ Users can install exactly the CLIs they need, or get everything from one marketplace.
+ The shared contract is documented once. Tool references are synced from each repo's `plugin/skills/<bin>/reference.md` (`scripts/sync-references.sh`).
- If a user installs the same plugin from two marketplaces, the hooks run only once. The docs tell users to pick one source.
