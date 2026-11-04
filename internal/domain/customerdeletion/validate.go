package customerdeletion

import (
	ierr "github.com/flexprice/flexprice/internal/errors"
)

// CountCategories counts the non-nil category rows.
func CountCategories(categories []*CustomerDeletionCategory) int {
	count := 0
	for _, category := range categories {
		if category != nil {
			count++
		}
	}
	return count
}

// ValidateCategories enforces what the Postgres indexes enforce.
func ValidateCategories(requestID string, categories []*CustomerDeletionCategory) error {
	// unique indexes make a categoryless request unretryable
	if CountCategories(categories) == 0 {
		return ierr.NewError("at least one deletion category is required").
			WithHint("A deletion request must carry at least one touchpoint category").
			WithReportableDetails(map[string]any{
				"request_id": requestID,
			}).
			Mark(ierr.ErrValidation)
	}

	seenID := make(map[string]struct{}, len(categories))
	seenCategory := make(map[string]struct{}, len(categories))

	for _, category := range categories {
		if category == nil {
			continue
		}

		if _, dup := seenID[category.ID]; dup {
			return ierr.NewError("duplicate customer deletion category id").
				WithHintf("Category ID %s appears twice in the same request", category.ID).
				WithReportableDetails(map[string]any{
					"request_id": requestID,
					"id":         category.ID,
				}).
				Mark(ierr.ErrAlreadyExists)
		}
		seenID[category.ID] = struct{}{}

		if _, dup := seenCategory[category.Category]; dup {
			return ierr.NewError("duplicate customer deletion category").
				WithHint("A category row already exists for this request and category").
				WithReportableDetails(map[string]any{
					"request_id": requestID,
					"category":   category.Category,
				}).
				Mark(ierr.ErrAlreadyExists)
		}
		seenCategory[category.Category] = struct{}{}
	}

	return nil
}
