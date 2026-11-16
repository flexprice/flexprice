package dto

// PublicAssetRequest addresses a public asset by its path segments.
type PublicAssetRequest struct {
	TenantID      string
	EnvironmentID string
	File          string
	IfNoneMatch   string
}

// PublicAsset is a stored asset; NotModified means the caller's cached copy is current.
type PublicAsset struct {
	Data        []byte
	ContentType string
	ETag        string
	NotModified bool
}
