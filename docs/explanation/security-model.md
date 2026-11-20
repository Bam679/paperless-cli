# The security model of paperless

`paperless` gives an AI agent access to personal data: the documents in your Paperless-ngx archive. This page explains how the CLI decides what an agent may do, why the checks live in the binary, and where their protection ends.

## Read-only by default

Most agent tasks only need to read: find a message, summarise a note, check a date. Writes are where mistakes cost something, and a confused agent writes to the wrong place more often than a malicious one attacks. So a fresh installation can read everything and change nothing. Allowing writes is a deliberate step that the user takes in a file the agent is not supposed to touch.

## Why the binary enforces the policy

Claude Code has permission rules and hooks, and other agents have similar mechanisms. They are useful, but they have two limits. They work only in one agent, and each agent has its own format. They also match the text of a command, not what it does, and Anthropic's documentation says that Bash rules are not a security boundary.

The policy file is checked inside `paperless`, after the CLI has resolved which container an item really belongs to. An agent cannot widen it with a flag, an environment variable or a different `$HOME`, and the check works the same under every agent. Comparable tools do the same: the GitHub MCP server has a read-only mode, and gogcli compiles its safety profiles into the binary. [ADR-0003](../adr/0003-permission-model.md) records the decision and the research behind it.

Every write is planned before it runs. The binary checks the operation, every container the plan touches and the batch size. Only then does it look at `--dry-run` or `--confirm`. A dry run is therefore a reliable way to learn whether a write would be allowed.

`--confirm` is not a human approval, because the agent types it. It makes writes explicit in the command line and in logs, and it lets Claude Code's `ask` rules prompt the user at the right moment.

## Where the protection ends

An agent that runs with your user account and an unrestricted shell can go around any CLI: it can call `osascript`, `curl` or a Python script directly, or edit the policy file. Neither this CLI nor a hook can prevent that.

The Claude Code sandbox can. In the sandbox, Bash commands cannot send Apple Events, reach the network or write outside the project, and `paperless` runs as the only excluded command. The CLI then becomes the only way to the data, and its policy becomes the real boundary. `paperless policy --claude-settings` prints that setup, and [How to configure permissions](../how-to/configure-permissions.md) shows how to apply it.

## Untrusted content

Data that comes back from Paperless-ngx was written by other people, so it can contain instructions aimed at the agent. The CLI strips control characters so output cannot fake extra lines or terminal escapes. It cannot know which text is an instruction, though, so the skill tells agents to treat everything they read as data. Read scopes help further: content in a hidden container never reaches the model.

## What was left out on purpose

An earlier design had a second, root-owned policy file and a result-count limit in the policy. No comparable tool has these, and they doubled the documentation for little gain once the sandbox recipe existed, so they were removed. Read scopes stayed, but as an optional privacy filter.
