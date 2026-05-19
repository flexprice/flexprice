package dto

import (
	"fmt"

	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/validator"
)

var supportedSyncEntityTypes = []string{"invoice", "customer"}

type IntegrationSyncRequest struct {
	EntityType string `json:"entity_type" validate:"required"`
	EntityID   string `json:"entity_id" validate:"required,max=255"`
}

func (r *IntegrationSyncRequest) Validate() error {
	if err := validator.ValidateRequest(r); err != nil {
		return err
	}

	for _, t := range supportedSyncEntityTypes {
		if r.EntityType == t {
			return nil
		}
	}

	return ierr.NewError(fmt.Sprintf("unsupported entity_type: %s", r.EntityType)).
		WithHintf("Supported entity types: %v", supportedSyncEntityTypes).
		Mark(ierr.ErrValidation)
}
