package ui

import "fmt"

// Info is commentary: progress, context, next steps. stderr, silenced by --quiet.
func (u *UI) Info(format string, a ...any) {
	if u.quiet {
		return
	}
	fmt.Fprintf(u.err, format+"\n", a...)
}

// Data is the command's result. stdout, never silenced: --quiet suppresses
// progress, not the thing that was asked for.
func (u *UI) Data(format string, a ...any) {
	fmt.Fprintf(u.out, format+"\n", a...)
}

func (u *UI) Success(format string, a ...any) {
	if u.quiet {
		return
	}
	fmt.Fprintln(u.err, u.palette.Success(fmt.Sprintf(format, a...)))
}

// Not gated on --quiet: a silent non-zero exit is worse than an unwanted line.
func (u *UI) Failure(err error) {
	fmt.Fprintln(u.err, u.palette.Error(err.Error()))
}

// Silent when id is empty: a vague "Created something" is worse than nothing.
// stderr only, so piping the object itself to jq is unaffected.
func (u *UI) Receipt(verb, resource, id string) {
	if u.quiet || id == "" {
		return
	}
	fmt.Fprintln(u.err, u.palette.Success(fmt.Sprintf("%s %s %s", verb, resource, id)))
}

func (u *UI) EmptyState(resource string) {
	if u.quiet {
		return
	}
	fmt.Fprintf(u.err, "No %s yet.\n", resource)
	fmt.Fprintf(u.err, "  Create one with: %s\n",
		u.palette.Accent(fmt.Sprintf("flexprice %s create", resource)))
}

// StatusLine is the dim context footer under table output.
func (u *UI) StatusLine(s string) {
	if u.quiet || s == "" {
		return
	}
	fmt.Fprintln(u.err, u.palette.Dim(s))
}
