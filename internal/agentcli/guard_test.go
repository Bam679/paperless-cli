package agentcli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func hook(tool, key, val string) []byte {
	b, _ := json.Marshal(map[string]any{"tool_name": tool, "tool_input": map[string]string{key: val}})
	return b
}

func TestGuardDecision(t *testing.T) {
	deny := []struct{ tool, key, val string }{
		{"Bash", "command", `echo '{}' > ~/.config/agent-cli/apple-mail.json`},
		{"Bash", "command", `cat x > "$HOME/.config/agent-cli/apple-mail.json"`},
		{"Bash", "command", `cp p.json /Users/me/.CONFIG/Agent-CLI/x.json`},
		{"Bash", "command", `sudo tee "/Library/Application Support/agent-cli/x.json"`},
		{"Bash", "command", `sudo tee /Library/Application\ Support/agent-cli/x.json`},
		{"Bash", "command", `cd ~/.config && rm -rf agent-cli`},
		{"Bash", "command", `cd /etc && vi agent-cli/x.json`},
		{"Edit", "file_path", "/Users/me/.config/agent-cli/apple-notes.json"},
		{"Write", "file_path", "/etc/agent-cli/paperless.json"},
		{"MultiEdit", "file_path", "/Users/me/.config/agent-cli/x.json"},
		{"NotebookEdit", "notebook_path", "/Users/me/.config/agent-cli/x.ipynb"},
	}
	for _, d := range deny {
		if ok, _ := GuardDecision(hook(d.tool, d.key, d.val)); !ok {
			t.Errorf("should deny %s %q", d.tool, d.val)
		}
	}
	allow := []struct{ tool, key, val string }{
		{"Bash", "command", "apple-mail list --json"},
		{"Bash", "command", "git clone https://github.com/Bam679/agent-cli-plugins"},
		{"Bash", "command", "apple-reminders policy"},
		{"Edit", "file_path", "/Users/me/src/agent-cli-plugins/README.md"},
		{"Read", "file_path", "/Users/me/.config/agent-cli/x.json"},
		{"Bash", "command", "ls ~/.config"},
	}
	for _, a := range allow {
		if ok, _ := GuardDecision(hook(a.tool, a.key, a.val)); ok {
			t.Errorf("should allow %s %q", a.tool, a.val)
		}
	}
	if ok, _ := GuardDecision([]byte("not json")); ok {
		t.Error("garbage must not deny")
	}
}

func FuzzGuardDecision(f *testing.F) {
	f.Add("Bash", "rm ~/.config/agent-cli/x.json")
	f.Add("Edit", "/tmp/x")
	f.Fuzz(func(t *testing.T, tool, val string) {
		key := "command"
		if tool != "Bash" {
			key = "file_path"
		}
		deny, _ := GuardDecision(hook(tool, key, val))
		if (tool == "Bash" || tool == "Edit" || tool == "Write") && strings.Contains(strings.ToLower(val), ".config/agent-cli") && !deny {
			t.Fatalf("missed %s %q", tool, val)
		}
	})
}

func TestGuardCommand(t *testing.T) {
	f := newFixture()
	code, out, _ := f.run(string(hook("Write", "file_path", "/Users/x/.config/agent-cli/fake.json")), "guard")
	if code != 0 || !strings.Contains(out, `"permissionDecision":"deny"`) {
		t.Fatalf("guard deny: %d %s", code, out)
	}
	code, out, _ = f.run(string(hook("Bash", "command", "fake items")), "guard")
	if code != 0 || out != "" {
		t.Fatalf("guard allow must be silent: %d %q", code, out)
	}
	f.polErr = bytes.ErrTooLarge
	if code, _, _ := f.run(string(hook("Bash", "command", "x")), "guard"); code != 0 {
		t.Fatal("guard must not depend on policy")
	}
}

func TestClaudeSettings(t *testing.T) {
	f := newFixture()
	code, out, _ := f.run("", "policy", "--claude-settings")
	if code != 0 {
		t.Fatal(code)
	}
	var v struct {
		Permissions struct{ Allow, Ask, Deny []string }
	}
	if err := json.Unmarshal([]byte(out), &v); err != nil {
		t.Fatal(err)
	}
	if !contains(v.Permissions.Ask, "Bash(fake add:*)") || !contains(v.Permissions.Allow, "Bash(fake items:*)") || len(v.Permissions.Deny) == 0 {
		t.Fatalf("unexpected settings: %s", out)
	}
	if contains(v.Permissions.Allow, "Bash(fake add:*)") {
		t.Fatal("write command must not be auto-allowed")
	}
}
