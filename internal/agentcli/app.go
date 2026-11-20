package agentcli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Kind classifies a command for the policy and the write gate.
type Kind string

// Command kinds.
const (
	Read  Kind = OpRead
	Write Kind = OpWrite
)

// FlagType is the value type of a command flag.
type FlagType string

// Flag value types.
const (
	Bool    FlagType = "bool"
	String  FlagType = "string"
	Int     FlagType = "int"
	Strings FlagType = "strings" // repeatable
)

// Flag declares a command flag.
type Flag struct {
	Name     string   `json:"name"`
	Type     FlagType `json:"type"`
	Default  string   `json:"default,omitempty"`
	Usage    string   `json:"usage"`
	Required bool     `json:"required,omitempty"`
	Enum     []string `json:"enum,omitempty"`
}

// Arg declares a positional argument.
type Arg struct {
	Name     string `json:"name"`
	Usage    string `json:"usage"`
	Required bool   `json:"required,omitempty"`
	Variadic bool   `json:"variadic,omitempty"`
}

// WritePlan describes a pending write. The App enforces policy, --dry-run and
// --confirm before calling Apply, so a command cannot forget the write gate.
type WritePlan struct {
	Scopes  []string                                  // containers touched; all must be write-allowed
	Targets []Item                                    // preview of the change (printed on --dry-run)
	Apply   func(ctx context.Context) ([]Item, error) // performs the write
}

// Command is one subcommand.
type Command struct {
	Name      string
	Summary   string
	Kind      Kind
	Op        string // policy operation name; defaults to "read" for reads
	Args      []Arg
	Flags     []Flag
	Fields    []string // default text columns, in order
	AllFields []string // every output field (validates --fields)
	Stdin     string   // description of accepted JSON on stdin ("" = none)
	Example   string
	Run       func(*Ctx) error               // read commands
	Plan      func(*Ctx) (*WritePlan, error) // write commands
}

func (c *Command) op() string {
	if c.Op != "" {
		return c.Op
	}
	return string(c.Kind)
}

// App is a CLI built from commands.
type App struct {
	Name         string
	Version      string
	Summary      string
	ScopeHelp    string // what a policy scope string means for this tool
	ScopeExample string
	Commands     []*Command
	DefaultLimit int
	// DefaultOn lists write operations that are allowed unless the policy
	// denies them explicitly (by name, by the "write" class, or by a
	// scopes.write.deny pattern). Use sparingly for harmless writes.
	DefaultOn []string
	// LoadPolicy is only replaceable in tests (in-process). The shipped
	// binary always uses the fixed path from PolicyPath.
	LoadPolicy func() (Effective, error)
	Now        func() time.Time
}

// MaxLimit caps --limit for every command.
const MaxLimit = 1000

// Global flags shared by every command.
var globalFlags = []Flag{
	{Name: "json", Type: Bool, Usage: "print one JSON document"},
	{Name: "ndjson", Type: Bool, Usage: "print one JSON object per line"},
	{Name: "fields", Type: String, Usage: "comma-separated output fields"},
	{Name: "limit", Type: Int, Usage: "max items to return (default 50)"},
	{Name: "cursor", Type: String, Usage: "continue from a next_cursor"},
	{Name: "max-chars", Type: Int, Usage: "cap stdout size; long text is truncated"},
	{Name: "dry-run", Type: Bool, Usage: "writes: show what would change, change nothing"},
	{Name: "confirm", Type: Bool, Usage: "writes: required to actually change data"},
	{Name: "timeout", Type: String, Default: "60s", Usage: "abort after this duration"},
	{Name: "help", Type: Bool, Usage: "show help"},
}

// Ctx is passed to command handlers.
type Ctx struct {
	context.Context
	App     *App
	Cmd     *Command
	Args    []string
	Out     *Renderer
	Stdin   io.Reader
	Policy  Effective
	Limit   int
	Cursor  string
	DryRun  bool
	Confirm bool
	Now     func() time.Time
	values  map[string][]string
}

// String returns a string flag value (or its default).
func (c *Ctx) String(name string) string {
	if v := c.values[name]; len(v) > 0 {
		return v[len(v)-1]
	}
	if f := c.flag(name); f != nil {
		return f.Default
	}
	return ""
}

// Strings returns all values of a repeatable flag.
func (c *Ctx) Strings(name string) []string { return c.values[name] }

// Bool returns a boolean flag value.
func (c *Ctx) Bool(name string) bool { return c.String(name) == "true" }

// IsSet reports whether the flag was given.
func (c *Ctx) IsSet(name string) bool { return len(c.values[name]) > 0 }

// Int returns an integer flag value.
func (c *Ctx) Int(name string) (int, error) {
	s := c.String(name)
	if s == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, Usagef("--%s must be an integer", name)
	}
	return n, nil
}

func (c *Ctx) flag(name string) *Flag {
	for _, fs := range [][]Flag{c.Cmd.Flags, globalFlags} {
		for i := range fs {
			if fs[i].Name == name {
				return &fs[i]
			}
		}
	}
	return nil
}

// CanRead reports whether the policy allows reading scope.
func (c *Ctx) CanRead(scope string) bool { return c.Policy.AllowsScope(OpRead, scope) }

// CheckRead returns a policy error when scope may not be read.
func (c *Ctx) CheckRead(scope string) error {
	if !c.CanRead(scope) {
		return Deniedf("reading %q is not allowed by policy", scope)
	}
	return nil
}

// List paginates items with --limit/--cursor and renders them.
func (c *Ctx) List(items []Item, keyParts ...string) error {
	key := QueryKey(append([]string{c.Cmd.Name, strconv.Itoa(c.Limit)}, keyParts...)...)
	p, err := Paginate(items, c.Cursor, key, c.Limit)
	if err != nil {
		return err
	}
	return c.Out.List(p, c.Cmd.Fields)
}

// Show renders a single item.
func (c *Ctx) Show(it Item) error { return c.Out.Object(it, c.Cmd.Fields) }

// DecodeInputs reads one JSON object, a JSON array of objects, or NDJSON from
// r into T values. Unknown fields are rejected. Input is capped at 4 MiB.
func DecodeInputs[T any](r io.Reader) ([]T, error) {
	data, err := io.ReadAll(io.LimitReader(r, 4<<20+1))
	if err != nil {
		return nil, Usagef("reading stdin: %v", err)
	}
	if len(data) > 4<<20 {
		return nil, Usagef("stdin larger than 4 MiB")
	}
	data = bytes.TrimSpace(data)
	if len(data) == 0 {
		return nil, Usagef("expected JSON on stdin")
	}
	var out []T
	if data[0] == '[' {
		dec := json.NewDecoder(bytes.NewReader(data))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&out); err != nil {
			return nil, Usagef("invalid JSON input: %v", err)
		}
		return out, nil
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	for dec.More() {
		var v T
		if err := dec.Decode(&v); err != nil {
			return nil, Usagef("invalid JSON input: %v", err)
		}
		out = append(out, v)
	}
	return out, nil
}

// Main runs the CLI and returns the process exit code.
func (a *App) Main(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	err := a.run(ctx, args, stdin, stdout)
	if err == nil {
		return ExitOK
	}
	e := AsError(err)
	if errors.Is(err, context.DeadlineExceeded) {
		e = Unavailablef("timed out")
	}
	enc := json.NewEncoder(stderr)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(map[string]any{"error": e})
	return e.Exit
}

func (a *App) builtins() []*Command {
	return []*Command{
		{Name: "schema", Summary: "print the machine-readable CLI contract as JSON", Kind: Read},
		{Name: "policy", Summary: "show the effective policy and its file paths", Kind: Read,
			Flags: []Flag{{Name: "example", Type: Bool, Usage: "print an example policy file"},
				{Name: "claude-settings", Type: Bool, Usage: "print recommended Claude Code permission rules"}}},
		{Name: "guard", Summary: "Claude Code PreToolUse hook: deny tool calls that modify policy files (reads hook JSON on stdin)", Kind: Read},
		{Name: "version", Summary: "print the version", Kind: Read},
		{Name: "help", Summary: "show help", Kind: Read},
	}
}

func (a *App) find(name string) *Command {
	for _, c := range a.Commands {
		if c.Name == name {
			return c
		}
	}
	for _, c := range a.builtins() {
		if c.Name == name {
			return c
		}
	}
	return nil
}

func (a *App) run(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer) error {
	idx := commandIndex(args)
	name := ""
	if idx >= 0 {
		name = args[idx]
	}
	if name == "" {
		if contains(args, "--help") || contains(args, "-h") {
			return a.writeHelp(stdout, nil)
		}
		if contains(args, "--version") {
			_, err := fmt.Fprintln(stdout, a.Name, a.Version)
			return err
		}
		_ = a.writeHelp(stdout, nil)
		return Usagef("missing command")
	}
	cmd := a.find(name)
	if cmd == nil {
		return Usagef("unknown command %q", name)
	}
	rest := append(append([]string{}, args[:idx]...), args[idx+1:]...)
	values, pos, err := parseFlags(rest, append(append([]Flag{}, cmd.Flags...), globalFlags...))
	if err != nil {
		return err
	}
	c := &Ctx{Context: ctx, App: a, Cmd: cmd, Args: pos, Stdin: stdin, values: values, Now: a.Now}
	if c.Now == nil {
		c.Now = time.Now
	}
	if c.Bool("help") || name == "help" {
		var target *Command
		if name == "help" && len(pos) > 0 {
			target = a.find(pos[0])
		} else if name != "help" {
			target = cmd
		}
		return a.writeHelp(stdout, target)
	}
	switch name {
	case "version":
		_, err := fmt.Fprintln(stdout, a.Name, a.Version)
		return err
	case "schema":
		return a.writeSchema(stdout)
	case "guard":
		return runGuard(stdin, stdout)
	}
	if err := checkArgs(cmd, pos); err != nil {
		return err
	}
	for _, f := range cmd.Flags {
		if f.Required && !c.IsSet(f.Name) {
			return Usagef("missing required flag --%s", f.Name)
		}
	}
	if err := a.setupOutput(c, stdout); err != nil {
		return err
	}
	d, err := time.ParseDuration(c.String("timeout"))
	if err != nil || d <= 0 {
		return Usagef("--timeout must be a positive duration like 30s")
	}
	runCtx, cancel := context.WithTimeout(ctx, d)
	defer cancel()
	c.Context = runCtx

	load := a.LoadPolicy
	if load == nil {
		load = func() (Effective, error) { return LoadPolicy(a.Name) }
	}
	pol, perr := load()
	if name == "policy" {
		return a.showPolicy(c, pol, perr)
	}
	if perr != nil {
		return Deniedf("policy file rejected, failing closed: %v", perr)
	}
	c.Policy = pol
	if !a.allowsOp(pol, cmd) {
		return Deniedf("operation %q is not allowed by policy", cmd.op())
	}
	if c.Limit, err = c.Int("limit"); err != nil {
		return err
	}
	def := a.DefaultLimit
	if def == 0 {
		def = 50
	}
	if c.Limit <= 0 {
		c.Limit = def
	}
	if c.Limit > MaxLimit {
		c.Limit = MaxLimit
	}
	c.Cursor, c.DryRun, c.Confirm = c.String("cursor"), c.Bool("dry-run"), c.Bool("confirm")
	if cmd.Kind == Write {
		return a.runWrite(c)
	}
	if c.DryRun || c.Confirm {
		return Usagef("--dry-run/--confirm only apply to write commands")
	}
	return cmd.Run(c)
}

func (a *App) defaultOn(cmd *Command) bool {
	return cmd.Kind == Write && contains(a.DefaultOn, cmd.op())
}

func (a *App) allowsOp(pol Effective, cmd *Command) bool {
	if a.defaultOn(cmd) {
		return len(pol.Layers) > 0 && !pol.DeniesOp(cmd.op(), string(cmd.Kind))
	}
	return pol.AllowsOp(cmd.op(), string(cmd.Kind))
}

func (a *App) allowsWriteScope(pol Effective, cmd *Command, scope string) bool {
	if a.defaultOn(cmd) {
		return len(pol.Layers) > 0 && !pol.DeniesScope(OpWrite, scope)
	}
	return pol.AllowsScope(OpWrite, scope)
}

func (a *App) runWrite(c *Ctx) error {
	plan, err := c.Cmd.Plan(c)
	if err != nil {
		return err
	}
	if len(plan.Scopes) == 0 {
		return Internalf("write plan without scope")
	}
	for _, s := range plan.Scopes {
		if !a.allowsWriteScope(c.Policy, c.Cmd, s) {
			return Deniedf("writing to %q is not allowed by policy", s)
		}
	}
	if n, max := len(plan.Targets), c.Policy.MaxWriteBatch(100); n > max {
		return Deniedf("batch of %d writes exceeds policy max_write_batch %d", n, max)
	}
	if c.DryRun || !c.Confirm {
		// Without --confirm the plan is printed too, so the agent can show it
		// to the user without a second round trip; nothing is changed.
		items := make([]Item, 0, len(plan.Targets))
		for _, t := range plan.Targets {
			it := Item{"dry_run": true, "op": c.Cmd.op()}
			for k, v := range t {
				it[k] = v
			}
			items = append(items, it)
		}
		if err := c.Out.List(Page{Items: items}, append([]string{"op", "dry_run"}, c.Cmd.Fields...)); err != nil {
			return err
		}
		if c.DryRun {
			return nil
		}
		return &Error{Code: "confirm_required", Exit: ExitConfirm,
			Message: fmt.Sprintf("%s changes data; the plan is on stdout, rerun with --confirm to apply", c.Cmd.Name)}
	}
	res, err := plan.Apply(c)
	if err != nil {
		return err
	}
	for _, it := range res {
		it["op"] = c.Cmd.op()
	}
	return c.Out.List(Page{Items: res}, append([]string{"op"}, c.Cmd.Fields...))
}

func (a *App) setupOutput(c *Ctx, stdout io.Writer) error {
	r := &Renderer{W: stdout}
	switch {
	case c.Bool("json") && c.Bool("ndjson"):
		return Usagef("--json and --ndjson are mutually exclusive")
	case c.Bool("json"):
		r.Format = FormatJSON
	case c.Bool("ndjson"):
		r.Format = FormatNDJSON
	}
	if f := c.String("fields"); f != "" {
		for _, x := range strings.Split(f, ",") {
			x = strings.TrimSpace(x)
			if x == "" {
				continue
			}
			if len(c.Cmd.AllFields) > 0 && !contains(c.Cmd.AllFields, x) {
				return Usagef("unknown field %q; valid: %s", x, strings.Join(c.Cmd.AllFields, ","))
			}
			r.Fields = append(r.Fields, x)
		}
	}
	n, err := c.Int("max-chars")
	if err != nil {
		return err
	}
	if n < 0 {
		return Usagef("--max-chars must not be negative")
	}
	r.MaxChars = n
	c.Out = r
	return nil
}

func (a *App) showPolicy(c *Ctx, pol Effective, perr error) error {
	if c.Bool("claude-settings") {
		enc := json.NewEncoder(c.Out.W)
		enc.SetIndent("", "  ")
		return enc.Encode(ClaudeSettings(a))
	}
	if c.Bool("example") {
		var ops []string
		for _, cmd := range a.Commands {
			if cmd.Kind == Write && !contains(ops, cmd.op()) {
				ops = append(ops, cmd.op())
			}
		}
		_, err := fmt.Fprintln(c.Out.W, ExamplePolicy(ops, a.ScopeExample))
		return err
	}
	userPath, _ := PolicyPath(a.Name)
	it := Item{"tool": a.Name, "user_path": userPath, "scope": a.ScopeHelp}
	if len(a.DefaultOn) > 0 {
		it["default_on"] = a.DefaultOn
	}
	if perr != nil {
		it["error"] = perr.Error()
		it["effective"] = "deny-all (fail closed)"
	} else {
		var layers []any
		for _, l := range pol.Layers {
			layers = append(layers, map[string]any{"source": l.Source, "operations": l.Operations, "scopes": l.Scopes, "limits": l.Limits})
		}
		it["layers"] = layers
	}
	if err := c.Out.Object(it, []string{"tool", "user_path", "scope", "default_on", "error", "effective", "layers"}); err != nil {
		return err
	}
	if perr != nil {
		return Deniedf("policy file rejected: %v", perr)
	}
	return nil
}

// commandIndex returns the index of the first positional token, skipping
// values of global flags given before the command, or -1.
func commandIndex(args []string) int {
	for i := 0; i < len(args); i++ {
		t := args[i]
		if t == "--" {
			return -1
		}
		if t == "-" || !strings.HasPrefix(t, "-") {
			return i
		}
		if strings.HasPrefix(t, "--") && !strings.Contains(t, "=") {
			for _, f := range globalFlags {
				if f.Name == t[2:] && f.Type != Bool {
					i++
				}
			}
		}
	}
	return -1
}

func checkArgs(cmd *Command, pos []string) error {
	required, variadic := 0, false
	for _, a := range cmd.Args {
		if a.Required {
			required++
		}
		variadic = variadic || a.Variadic
	}
	if len(pos) < required {
		return Usagef("%s needs %d argument(s): %s", cmd.Name, required, argNames(cmd.Args))
	}
	if !variadic && len(pos) > len(cmd.Args) {
		return Usagef("%s takes at most %d argument(s): %s", cmd.Name, len(cmd.Args), argNames(cmd.Args))
	}
	return nil
}

func argNames(args []Arg) string {
	var s []string
	for _, a := range args {
		s = append(s, "<"+a.Name+">")
	}
	if len(s) == 0 {
		return "none"
	}
	return strings.Join(s, " ")
}

// parseFlags parses GNU-style flags (--name value, --name=value) anywhere in
// args. "--" ends flag parsing; "-" is a positional (stdin).
func parseFlags(args []string, specs []Flag) (map[string][]string, []string, error) {
	byName := map[string]Flag{}
	for _, f := range specs {
		byName[f.Name] = f
	}
	values := map[string][]string{}
	var pos []string
	for i := 0; i < len(args); i++ {
		t := args[i]
		if t == "--" {
			pos = append(pos, args[i+1:]...)
			break
		}
		if t == "-" || !strings.HasPrefix(t, "-") {
			pos = append(pos, t)
			continue
		}
		if t == "-h" {
			t = "--help"
		}
		if !strings.HasPrefix(t, "--") {
			return nil, nil, Usagef("unknown flag %q (flags use two dashes)", t)
		}
		name, val, hasVal := strings.Cut(t[2:], "=")
		f, ok := byName[name]
		if !ok {
			return nil, nil, Usagef("unknown flag --%s", name)
		}
		if f.Type == Bool {
			if !hasVal {
				val = "true"
			}
			if val != "true" && val != "false" {
				return nil, nil, Usagef("--%s takes no value (or =true/=false)", name)
			}
		} else if !hasVal {
			if i+1 >= len(args) {
				return nil, nil, Usagef("--%s needs a value", name)
			}
			i++
			val = args[i]
		}
		if f.Type != Strings && len(values[name]) > 0 {
			return nil, nil, Usagef("--%s given more than once", name)
		}
		if len(f.Enum) > 0 && !contains(f.Enum, val) {
			return nil, nil, Usagef("--%s must be one of %s", name, strings.Join(f.Enum, ","))
		}
		values[name] = append(values[name], val)
	}
	return values, pos, nil
}

func (a *App) writeHelp(w io.Writer, cmd *Command) error {
	var b strings.Builder
	if cmd == nil {
		fmt.Fprintf(&b, "%s %s - %s\n\nUsage: %s <command> [args] [flags]\n\nCommands:\n", a.Name, a.Version, a.Summary, a.Name)
		all := append(append([]*Command{}, a.Commands...), a.builtins()...)
		for _, c := range all {
			tag := ""
			if c.Kind == Write {
				tag = " [write]"
				if a.defaultOn(c) {
					tag = " [write, on unless the policy denies it]"
				}
			}
			fmt.Fprintf(&b, "  %-16s %s%s\n", c.Name, c.Summary, tag)
		}
		b.WriteString("\nGlobal flags:\n")
		writeFlags(&b, globalFlags)
		b.WriteString("\nWrites need --confirm (or --dry-run) and must be allowed by the policy file.\n")
		fmt.Fprintf(&b, "Run '%s schema' for the full machine-readable contract.\n", a.Name)
	} else {
		fmt.Fprintf(&b, "Usage: %s %s", a.Name, cmd.Name)
		for _, x := range cmd.Args {
			fmt.Fprintf(&b, " <%s>", x.Name)
		}
		fmt.Fprintf(&b, " [flags]\n\n%s", cmd.Summary)
		if cmd.Kind == Write {
			fmt.Fprintf(&b, " [write: policy op %q, needs --confirm]", cmd.op())
		}
		b.WriteString("\n")
		for _, x := range cmd.Args {
			fmt.Fprintf(&b, "  <%s>  %s\n", x.Name, x.Usage)
		}
		if len(cmd.Flags) > 0 {
			b.WriteString("\nFlags:\n")
			writeFlags(&b, cmd.Flags)
		}
		if cmd.Stdin != "" {
			fmt.Fprintf(&b, "\nStdin: %s\n", cmd.Stdin)
		}
		if len(cmd.AllFields) > 0 {
			fmt.Fprintf(&b, "\nFields: %s (default: %s)\n", strings.Join(cmd.AllFields, ","), strings.Join(cmd.Fields, ","))
		}
		if cmd.Example != "" {
			fmt.Fprintf(&b, "\nExample:\n  %s\n", cmd.Example)
		}
	}
	_, err := io.WriteString(w, b.String())
	return err
}

func writeFlags(b *strings.Builder, flags []Flag) {
	for _, f := range flags {
		n := "--" + f.Name
		if f.Type != Bool {
			n += " <" + string(f.Type) + ">"
		}
		extra := ""
		if f.Required {
			extra = " (required)"
		}
		if len(f.Enum) > 0 {
			extra += " {" + strings.Join(f.Enum, "|") + "}"
		}
		fmt.Fprintf(b, "  %-24s %s%s\n", n, f.Usage, extra)
	}
}

type schemaCommand struct {
	Name      string   `json:"name"`
	Summary   string   `json:"summary"`
	Kind      Kind     `json:"kind"`
	Op        string   `json:"op"`
	Args      []Arg    `json:"args"`
	Flags     []Flag   `json:"flags"`
	Fields    []string `json:"default_fields,omitempty"`
	AllFields []string `json:"fields,omitempty"`
	Stdin     string   `json:"stdin,omitempty"`
	Example   string   `json:"example,omitempty"`
}

func nonNilStrings(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func (a *App) writeSchema(w io.Writer) error {
	var cmds []schemaCommand
	for _, c := range append(append([]*Command{}, a.Commands...), a.builtins()...) {
		args, flags := c.Args, c.Flags
		if args == nil {
			args = []Arg{}
		}
		if flags == nil {
			flags = []Flag{}
		}
		cmds = append(cmds, schemaCommand{c.Name, c.Summary, c.Kind, c.op(), args, flags, c.Fields, c.AllFields, c.Stdin, c.Example})
	}
	sort.SliceStable(cmds, func(i, j int) bool { return cmds[i].Name < cmds[j].Name })
	doc := map[string]any{
		"name":         a.Name,
		"version":      a.Version,
		"summary":      a.Summary,
		"commands":     cmds,
		"global_flags": globalFlags,
		"exit_codes":   ExitCodes,
		"output": map[string]string{
			"text":   "one item per line, tab-separated default fields, '-' for empty; trailer lines start with '#'",
			"json":   "lists: {items,count,next_cursor?,truncated?}; single items: object",
			"ndjson": "one object per line; optional final {\"_meta\":{next_cursor,truncated}}",
			"errors": "one JSON line on stderr: {\"error\":{code,message,hint,exit}}",
		},
		"policy": map[string]any{
			"path":       "~/.config/agent-cli/" + a.Name + ".json (home from the user database, not $HOME)",
			"default":    "read-only: operations.allow=[read], no writes",
			"scope":      a.ScopeHelp,
			"rules":      "deny wins; empty scopes.read.allow means all; flags and env vars cannot widen the policy",
			"default_on": nonNilStrings(a.DefaultOn),
		},
	}
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	return enc.Encode(doc)
}
