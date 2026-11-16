package types

// Status is a type for the status of a resource (e.g. meter, event) in the Database
// This is used to track the lifecycle of a resource and to determine if it should be included in queries
// Any changes to this type should be reflected in the database schema by running migrations
type Status string

const (
	// StatusPublished is the status of a resource that is published and visible to users
	StatusPublished Status = "published"

	// StatusDeleted is the status of a resource that is deleted and not in use
	// This is typically used for data that is no longer in use and should be removed from the database
	// These rows should not be returned in queries and should not be visible to users
	StatusDeleted Status = "deleted"

	// StatusArchived is the status of a resource that is archived and not in use
	// This is typically used for data that is no longer in use but we want to keep for historical purposes
	// These rows might be returned in queries and might be visible to users in some cases only
	StatusArchived Status = "archived"

	// Erasure requested; never readable, reversible
	StatusSuppressed Status = "suppressed"
)

// Unset status behaves as published
func CustomerStatusOrDefault(status Status) string {
	if status == "" {
		return string(StatusPublished)
	}
	return string(status)
}

// Single source for production and test doubles
func IsCustomerStatusReadable(status Status) bool {
	switch CustomerStatusOrDefault(status) {
	case string(StatusPublished), string(StatusArchived):
		return true
	default:
		return false
	}
}

// Fresh slice so callers cannot widen the allowlist
func ReadableCustomerStatuses() []string {
	return []string{
		string(StatusPublished),
		string(StatusArchived),
	}
}
