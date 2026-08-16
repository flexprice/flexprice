package types

import (
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/samber/lo"
)

type EntityChangeBehaviour string

const (
	EntityChangeBehaviourCarry EntityChangeBehaviour = "carry"
	EntityChangeBehaviourDrop  EntityChangeBehaviour = "drop"
)

var EntityChangeBehaviourValues = []EntityChangeBehaviour{
	EntityChangeBehaviourCarry,
	EntityChangeBehaviourDrop,
}

func (d EntityChangeBehaviour) String() string { return string(d) }

func (d EntityChangeBehaviour) Validate() error {
	if d == "" {
		return nil
	}
	if !lo.Contains(EntityChangeBehaviourValues, d) {
		return ierr.NewError("invalid entity change behaviour").
			WithHint("Behaviour must be one of the allowed values").
			WithReportableDetails(map[string]any{
				"behaviour": string(d),
				"allowed":   EntityChangeBehaviourValues,
			}).
			Mark(ierr.ErrValidation)
	}
	return nil
}
