package agentcli

import (
	"encoding/json"
	"io"
	"strings"
)

// hookInput is the subset of a Claude Code PreToolUse payload we inspect.
type hookInput struct {
	ToolName  string `json:"tool_name"`
	ToolInput struct {
		Command      string `json:"command"`
		FilePath     string `json:"file_path"`
		NotebookPath string `json:"notebook_path"`
		Path         string `json:"path"`
	} `json:"tool_input"`
}

// GuardDecision inspects a PreToolUse payload and returns a deny reason if the
// tool call would touch an agent-cli policy file. It is best effort (an agent
// with a shell can obfuscate); the binary-enforced policy together with the
// Claude Code sandbox is the actual boundary.
func GuardDecision(payload []byte) (deny bool, reason string) {
	var in hookInput
	if err := json.Unmarshal(payload, &in); err != nil {
		return false, ""
	}
	var targets []string
	switch in.ToolName {
	case "Bash":
		targets = []string{in.ToolInput.Command}
	case "Edit", "Write", "MultiEdit", "NotebookEdit":
		targets = []string{in.ToolInput.FilePath, in.ToolInput.NotebookPath, in.ToolInput.Path}
	default:
		return false, ""
	}
	for _, t := range targets {
		if touchesPolicy(t) {
			return true, "agent-cli policy files are user-controlled; agents must not modify them. Ask the user to change the policy."
		}
	}
	return false, ""
}

// touchesPolicy normalises quoting, escapes and case (APFS is usually
// case-insensitive) before looking for the policy directories.
func touchesPolicy(s string) bool {
	n := strings.ToLower(s)
	n = strings.NewReplacer(`\`, "", `"`, "", `'`, "", "$home", "~", "${home}", "~").Replace(n)
	n = strings.Join(strings.Fields(n), " ")
	for _, p := range []string{".config/agent-cli", "application support/agent-cli", "/etc/agent-cli"} {
		if strings.Contains(n, p) {
			return true
		}
	}
	if strings.Contains(n, "agent-cli/") || strings.Contains(n, "agent-cli ") || strings.HasSuffix(n, "agent-cli") {
		for _, p := range []string{".config", "application support", "/etc"} {
			if strings.Contains(n, p) {
				return true
			}
		}
	}
	return false
}

// runGuard implements the guard builtin: read a hook payload from stdin and
// print a PreToolUse deny decision, or nothing to defer to normal handling.
func runGuard(stdin io.Reader, stdout io.Writer) error {
	data, err := io.ReadAll(io.LimitReader(stdin, 1<<20))
	if err != nil {
		return nil
	}
	deny, reason := GuardDecision(data)
	if !deny {
		return nil
	}
	out := map[string]any{"hookSpecificOutput": map[string]any{
		"hookEventName":            "PreToolUse",
		"permissionDecision":       "deny",
		"permissionDecisionReason": reason,
	}}
	b, _ := json.Marshal(out)
	_, err = stdout.Write(append(b, '\n'))
	return err
}

// ClaudeSettings returns recommended Claude Code settings for app: read
// commands allowed, write commands ask, policy files denied, and the Bash
// sandbox enabled with only this CLI excluded. Inside the sandbox an agent
// cannot reach Apple Events, the network or the policy file directly, so the
// CLI (and its policy) is the only path to the data.
func ClaudeSettings(a *App) map[string]any {
	var allow, ask []string
	for _, c := range append(append([]*Command{}, a.Commands...), a.builtins()...) {
		if c.Name == "guard" {
			continue
		}
		rule := "Bash(" + a.Name + " " + c.Name + ":*)"
		if c.Kind == Write {
			ask = append(ask, rule)
		} else {
			allow = append(allow, rule)
		}
	}
	return map[string]any{
		"sandbox": map[string]any{
			"enabled":                  true,
			"allowUnsandboxedCommands": false,
			"excludedCommands":         []string{a.Name + " *"},
			"filesystem":               map[string]any{"denyWrite": []string{"~/.config/agent-cli"}},
		},
		"permissions": map[string]any{
			"allow": allow,
			"ask":   ask,
			"deny":  []string{"Read(~/.config/agent-cli/**)", "Edit(~/.config/agent-cli/**)"},
		},
	}
}
