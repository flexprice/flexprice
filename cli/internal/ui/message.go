package ui

import "fmt"

// Info is human commentary: progress, context, next steps. Goes to stderr and
// is silenced by --quiet.
func (u *UI) Info(format string, a ...any) {
	if u.quiet {
		return
	}
	fmt.Fprintf(u.err, format+"\n", a...)
}

// Data is a command's result. It goes to stdout and is NEVER silenced by
// --quiet: --quiet suppresses progress, not the thing that was asked for.
func (u *UI) Data(format string, a ...any) {
	fmt.Fprintf(u.out, format+"\n", a...)
}

// Success reports a completed state change.
func (u *UI) Success(format string, a ...any) {
	if u.quiet {
		return
	}
	fmt.Fprintln(u.err, u.palette.Success(fmt.Sprintf(format, a...)))
}

// Failure reports an error. Deliberately not gated on --quiet: a user who
// asked for less progress output did not ask to have failures hidden, and a
// silent non-zero exit is far worse than an unwanted line.
func (u *UI) Failure(err error) {
	fmt.Fprintln(u.err, u.palette.Error(err.Error()))
}

// Receipt confirms a state change. It is silent when id is empty: without an
// identifier there is nothing trustworthy to report, and a vague "Created
// something" is worse than saying nothing.
//
// Always stderr. The created object itself goes to stdout as normal output, so
// piping to jq is unaffected.
func (u *UI) Receipt(verb, resource, id string) {
	if u.quiet || id == "" {
		return
	}
	fmt.Fprintln(u.err, u.palette.Success(fmt.Sprintf("%s %s %s", verb, resource, id)))
}

// EmptyState reports that a list came back empty and names a way forward.
// clig.dev: suggesting the next command is how people discover a CLI's shape.
func (u *UI) EmptyState(resource string) {
	if u.quiet {
		return
	}
	fmt.Fprintf(u.err, "No %s yet.\n", resource)
	fmt.Fprintf(u.err, "  Create one with: %s\n",
		u.palette.Accent(fmt.Sprintf("flexprice %s create", resource)))
}

// StatusLine renders the dim context footer under table output.
func (u *UI) StatusLine(s string) {
	if u.quiet || s == "" {
		return
	}
	fmt.Fprintln(u.err, u.palette.Dim(s))
}
