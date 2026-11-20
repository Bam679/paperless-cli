# ADR-0002: Per-tool plugins plus an umbrella marketplace with one shared skill

- Status: accepted
- Date: 2026-11-20

## Context

Agents need to learn each CLI, but users may install only some of them. Claude Code supports plugins (`.claude-plugin/plugin.json`), marketplaces whose entries point to other Git repositories, skills that load extra reference files only when needed, and plugin hooks. Plugins cannot ship permission rules (see ADR-0003).

We considered three options. One plugin for all CLIs would be simple, but it couples releases and installs skills for tools the user doesn't have. Plugins per tool without an umbrella would leave no single entry point and repeat the shared contract in five skills. The third option combines both.

## Decision

Each CLI repository keeps its plugin in `plugin/`, named after the binary (for example `apple-reminders`). Keeping it out of the repository root means contributor files such as `CLAUDE.md` and `.claude/` don't end up in the plugin. The repository root holds a marketplace with this single plugin (`"source": "./plugin"`), so each CLI can be installed on its own:

```sh
claude plugin marketplace add Bam679/apple-reminders-cli
claude plugin install apple-reminders@apple-reminders-cli
```

A tool plugin contains a skill (`plugin/skills/<bin>/SKILL.md`, under 150 lines, plus `reference.md`) and a `PreToolUse` hook that runs `<bin> guard`.

`Bam679/agent-cli-plugins` is the umbrella marketplace. It lists the five tool plugins as `git-subdir` sources (`path: plugin`) pinned to release tags, plus the `agent-cli` plugin. That plugin has one shared skill: `SKILL.md` describes the common contract once, and `references/<bin>.md` holds one file per tool, which the agent reads only when it needs that tool. A `SessionStart` hook prints which CLIs are installed and their versions, so the agent does not have to guess.

release-please bumps `version` in `plugin.json` together with the binary. The umbrella marketplace pins tags and is updated by pull request (`scripts/bump.sh`). CI validates every plugin and the marketplaces with `claude plugin validate --strict` and installs all plugins into a temporary configuration directory.

## Consequences

Users can install exactly the CLIs they need, or everything from one marketplace, and the shared contract is written down once. `scripts/sync-references.sh` copies each tool's reference into the shared skill. If a user installs the same plugin from two marketplaces, its hooks run only once, so the documentation asks users to pick one source.
