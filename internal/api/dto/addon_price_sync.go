package dto

// SyncAddonPricesResponse is the response for the addon price sync endpoints.
type SyncAddonPricesResponse struct {
	AddonID string                 `json:"addon_id"`
	Message string                 `json:"message"`
	Summary SyncAddonPricesSummary `json:"summary"`
}

// SyncAddonPricesSummary contains the metrics from an addon price sync run.
type SyncAddonPricesSummary struct {
	LineItemsFoundForCreation int `json:"line_items_found_for_creation"`
	LineItemsCreated          int `json:"line_items_created"`
	LineItemsTerminated       int `json:"line_items_terminated"`
}
