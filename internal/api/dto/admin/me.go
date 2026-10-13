package admin

// MeResponse confirms that the caller's admin secret was accepted.
type MeResponse struct {
	OK bool `json:"ok"`
}
