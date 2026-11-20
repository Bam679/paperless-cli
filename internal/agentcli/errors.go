// Package agentcli is the shared, dependency-free core of the agent-cli family:
// argument parsing, the output contract, exit codes, policy enforcement and the
// write gate. It is copied verbatim into every CLI repo from
// github.com/Bam679/agent-cli-plugins/scaffold; edit it there, then sync.
package agentcli

import (
	"errors"
	"fmt"
)

// Exit codes are part of the public contract. Never renumber them.
const (
	ExitOK          = 0 // success
	ExitError       = 1 // internal or unexpected error
	ExitUsage       = 2 // invalid arguments or flags
	ExitNotFound    = 3 // item or container does not exist
	ExitPolicy      = 4 // denied by the policy file (never bypassable by flags)
	ExitConfirm     = 5 // write requested without --confirm (use --dry-run to preview)
	ExitPermission  = 6 // OS permission (TCC) or authentication failure
	ExitUnavailable = 7 // backend not reachable or timed out
	ExitConflict    = 8 // ambiguous name or item already exists
)

// ExitCodes documents every exit code for the schema subcommand.
var ExitCodes = []ExitCode{
	{ExitOK, "ok", "success"},
	{ExitError, "internal", "internal or unexpected error"},
	{ExitUsage, "usage", "invalid arguments or flags"},
	{ExitNotFound, "not_found", "item or container does not exist"},
	{ExitPolicy, "policy_denied", "denied by the policy file; flags cannot override this"},
	{ExitConfirm, "confirm_required", "write requested without --confirm; preview with --dry-run"},
	{ExitPermission, "permission_denied", "OS privacy permission (TCC) or authentication failure"},
	{ExitUnavailable, "unavailable", "backend not reachable or timed out"},
	{ExitConflict, "conflict", "ambiguous name or item already exists"},
}

// ExitCode describes one documented exit code.
type ExitCode struct {
	Code    int    `json:"code"`
	Name    string `json:"name"`
	Meaning string `json:"meaning"`
}

// Error is the structured error printed as one JSON line on stderr.
type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Hint    string `json:"hint,omitempty"`
	Exit    int    `json:"exit"`
}

func (e *Error) Error() string { return e.Code + ": " + e.Message }

func newErr(exit int, code, hint, format string, args ...any) *Error {
	return &Error{Code: code, Message: fmt.Sprintf(format, args...), Hint: hint, Exit: exit}
}

// Usagef reports invalid input.
func Usagef(format string, args ...any) *Error {
	return newErr(ExitUsage, "usage", "run with --help or use the schema subcommand", format, args...)
}

// NotFoundf reports a missing item.
func NotFoundf(format string, args ...any) *Error {
	return newErr(ExitNotFound, "not_found", "", format, args...)
}

// Deniedf reports a policy denial.
func Deniedf(format string, args ...any) *Error {
	return newErr(ExitPolicy, "policy_denied", "the policy file must allow this; agents must not edit it", format, args...)
}

// Permissionf reports an OS permission or auth failure.
func Permissionf(format string, args ...any) *Error {
	return newErr(ExitPermission, "permission_denied", "grant access in System Settings > Privacy & Security, or check credentials", format, args...)
}

// Unavailablef reports an unreachable backend.
func Unavailablef(format string, args ...any) *Error {
	return newErr(ExitUnavailable, "unavailable", "", format, args...)
}

// Conflictf reports an ambiguous or duplicate item.
func Conflictf(format string, args ...any) *Error {
	return newErr(ExitConflict, "conflict", "", format, args...)
}

// Internalf reports an unexpected failure.
func Internalf(format string, args ...any) *Error {
	return newErr(ExitError, "internal", "", format, args...)
}

// AsError converts any error into an *Error, defaulting to internal.
func AsError(err error) *Error {
	if err == nil {
		return nil
	}
	var e *Error
	if errors.As(err, &e) {
		return e
	}
	return Internalf("%s", truncate(err.Error(), 500))
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
