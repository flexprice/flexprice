package ui

import (
	"fmt"

	"github.com/charmbracelet/huh"
)

// Option is one choice in a Select.
type Option struct {
	Label string
	Value string
}

// ConfirmTitle is the sentence shown above a destructive confirmation.
//
// Exported and separated from Confirm so it stays testable: huh drives a real
// terminal session, so the interactive path cannot be exercised under `go
// test`. The wording is the part that matters and the part that can regress —
// a prompt that fails to say what will be destroyed is worse than no prompt,
// because the user answers yes to a question they did not read properly.
func ConfirmTitle(action, subject string) string {
	return fmt.Sprintf("This will %s %s and cannot be undone.", action, subject)
}

// Confirm asks before a destructive action. It refuses rather than prompting
// when input is unavailable, so scripts fail loudly with a message naming the
// flag to pass instead of hanging on a prompt nobody can answer.
//
// This is a deliberate behaviour change from the previous raw y/N prompt, which
// returned nil — i.e. proceeded — when stdin was not a terminal. Silently
// destroying something because nobody could be asked is the worse default; a
// script that relied on it now fails until --force is added.
func (u *UI) Confirm(action, subject string) error {
	if u.noInput {
		return fmt.Errorf(
			"refusing to %s %s without confirmation — pass --force to proceed non-interactively",
			action, subject)
	}

	var ok bool
	err := huh.NewConfirm().
		Title(ConfirmTitle(action, subject)).
		Affirmative("Yes, " + action).
		Negative("Cancel").
		Value(&ok).
		Run()
	if err != nil {
		return fmt.Errorf("confirmation cancelled: %w", err)
	}
	if !ok {
		return fmt.Errorf("cancelled")
	}
	return nil
}

// SelectWithHint presents an arrow-key menu. flagHint names the flag a scripted
// caller should pass instead, so the refusal under --no-input is actionable.
func (u *UI) SelectWithHint(title, flagHint string, opts []Option) (string, error) {
	if len(opts) == 0 {
		return "", fmt.Errorf("no options available for %q", title)
	}
	// One option is not a question. Prompting here would make scripted use
	// fail for no reason.
	if len(opts) == 1 {
		return opts[0].Value, nil
	}
	if u.noInput {
		return "", fmt.Errorf("no terminal available — pass %s (for example %s %s)",
			flagHint, flagHint, opts[0].Value)
	}

	huhOpts := make([]huh.Option[string], len(opts))
	for i, o := range opts {
		huhOpts[i] = huh.NewOption(o.Label, o.Value)
	}

	var choice string
	if err := huh.NewSelect[string]().
		Title(title).
		Options(huhOpts...).
		Value(&choice).
		Run(); err != nil {
		return "", fmt.Errorf("%s selection cancelled: %w", title, err)
	}
	return choice, nil
}

// Select is SelectWithHint for callers with no specific flag to point at. The
// refusal message under --no-input is necessarily vaguer, so prefer
// SelectWithHint wherever a flag exists.
func (u *UI) Select(title string, opts []Option) (string, error) {
	return u.SelectWithHint(title, "the corresponding flag", opts)
}
