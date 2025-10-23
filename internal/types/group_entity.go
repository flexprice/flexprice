package types

import (
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/samber/lo"
)

// GroupEntityType represents the type of entity that can be grouped
type GroupEntityType string

const (
	GroupEntityTypePrice GroupEntityType = "price"
)

func (e GroupEntityType) String() string {
	return string(e)
}

func (e GroupEntityType) Validate() error {
	allowed := []GroupEntityType{
		GroupEntityTypePrice,
	}
	if !lo.Contains(allowed, e) {
		return ierr.NewError("invalid group entity type").
			WithHint("Unsupported entity type: " + e.String()).
			WithReportableDetails(map[string]interface{}{
				"allowed_types": allowed,
			}).
			Mark(ierr.ErrValidation)
	}
	return nil
}

// GetAllGroupEntityTypes returns all supported group entity types
func GetAllGroupEntityTypes() []GroupEntityType {
	return []GroupEntityType{
		GroupEntityTypePrice,
	}
}

// IsValidGroupEntityType checks if the given string is a valid group entity type
func IsValidGroupEntityType(entityType string) bool {
	groupEntityType := GroupEntityType(entityType)
	return groupEntityType.Validate() == nil
}
