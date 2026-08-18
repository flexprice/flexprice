package ui

import (
	"fmt"

	"github.com/charmbracelet/huh"
)

type Option struct {
	Label string
	Value string
}

// Separated from Confirm so the wording stays testable: huh drives a real
// terminal session, which `go test` cannot exercise.
func ConfirmTitle(action, subject string) string {
	return fmt.Sprintf("This will %s %s and cannot be undone.", action, subject)
}

// Refuses rather than prompting when input is unavailable, so scripts fail
// loudly instead of hanging — or, as the old raw y/N prompt did, proceeding to
// destroy something because nobody could be asked.
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

// flagHint names the flag a scripted caller should pass instead, so the
// refusal under --no-input is actionable.
func (u *UI) SelectWithHint(title, flagHint string, opts []Option) (string, error) {
	if len(opts) == 0 {
		return "", fmt.Errorf("no options available for %q", title)
	}
	// One option is not a question; prompting would fail scripted use for
	// no reason.
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

// Prefer SelectWithHint wherever a flag exists: this refusal message is vaguer.
func (u *UI) Select(title string, opts []Option) (string, error) {
	return u.SelectWithHint(title, "the corresponding flag", opts)
}
