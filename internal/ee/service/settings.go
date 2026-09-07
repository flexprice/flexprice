package service

import (
	"context"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/settings"
	ierr "github.com/flexprice/flexprice/internal/errors"
	workflowModels "github.com/flexprice/flexprice/internal/temporal/models"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/flexprice/flexprice/internal/utils"
	"github.com/samber/lo"
)

type SettingsService interface {
	// GetSettingByKey returns a setting as a DTO response (for API endpoints)
	// Use this when you need the full Setting object with metadata (ID, timestamps, etc.)
	//
	// Access-checked: keys governing authentication are refused to callers who
	// may not administer them. Server-side code acting with no user in context
	// must use GetSettingByKeyUnchecked instead.
	GetSettingByKey(ctx context.Context, key types.SettingKey) (*dto.SettingResponse, error)

	// GetSettingByKeyUnchecked returns a setting without the caller checks that
	// GetSettingByKey applies.
	//
	// It exists for work the server does on its own behalf, where there is no
	// user to check: the SAML endpoints run before a session exists and must
	// read the tenant's identity provider configuration to serve a login at all.
	// Never call it while handling a request on behalf of a caller — that is
	// what GetSettingByKey is for.
	GetSettingByKeyUnchecked(ctx context.Context, key types.SettingKey) (*dto.SettingResponse, error)

	// UpdateSettingByKey updates a setting with partial values (merges with existing)
	// Use this for API endpoints that accept partial updates
	UpdateSettingByKey(ctx context.Context, key types.SettingKey, req *dto.UpdateSettingRequest) (*dto.SettingResponse, error)

	// DeleteSettingByKey deletes a setting by key
	DeleteSettingByKey(ctx context.Context, key types.SettingKey) error
}

type settingsService struct {
	ServiceParams
}

func NewSettingsService(params ServiceParams) SettingsService {
	return &settingsService{
		ServiceParams: params,
	}
}

// isTenantLevelSetting checks if a setting is tenant-level (no environment_id)
// Tenant-level settings apply across all environments for a tenant
func isTenantLevelSetting(key types.SettingKey) bool {
	// SAML is tenant-level because an identity provider is per-organisation, and
	// because the pre-login endpoints run with no environment in context.
	return key == types.SettingKeyTenantConfig ||
		key == types.SettingKeySAMLConfig
}

// requireSAMLEnabled refuses a saml_config request on a deployment that does not
// offer SAML.
//
// Writes are already restricted to super admins by the router. What the router
// cannot know is whether the feature exists at all: with auth.saml.enabled
// false no SAML routes are served, and a stored configuration could only sit
// there looking as though SSO were set up.
func (s *settingsService) requireSAMLEnabled(key types.SettingKey) error {
	if key != types.SettingKeySAMLConfig {
		return nil
	}

	if s.Config == nil || !s.Config.Auth.SAML.Enabled {
		return ierr.NewError("saml is not enabled for this deployment").
			WithHint("SAML single sign-on is not available on this deployment").
			Mark(ierr.ErrNotFound)
	}
	return nil
}

// requireSuperAdminToReadSAMLConfig keeps the identity provider configuration
// away from callers who may not administer it.
//
// Settings reads carry the ordinary setting:read permission, which most keys
// should: an invoice prefix is configuration the dashboard shows to any member.
// This one is different. It names the identity provider the tenant trusts and
// whether Flexprice has approved it, which is a map of what to attack, and the
// fields most likely to be added next — an SP private key, a directory-sync
// token — would be secret outright. Someone who may not change how people log
// in has no reason to read it either.
//
// Checked here rather than on the route because the route is shared by every
// key, and gating all of them on super_admin would take reads away from the
// settings the dashboard legitimately shows to everyone.
func requireSuperAdminToReadSAMLConfig(ctx context.Context, key types.SettingKey) error {
	if key != types.SettingKeySAMLConfig {
		return nil
	}

	if types.IsServiceAccount(ctx) || !lo.Contains(types.GetRoles(ctx), types.RoleSuperAdmin.String()) {
		return ierr.NewError("saml configuration requires a super admin user").
			WithHint("This action requires a user account with Super Admin access").
			Mark(ierr.ErrPermissionDenied)
	}
	return nil
}

// apiImmutableSettingFields lists the fields of a setting that the settings API
// may never write, whatever the caller's role.
//
// The settings API is generic: it merges whatever keys a request carries into
// the stored value. That is fine for configuration a tenant owns, but not for a
// field that records a Flexprice-side decision about that tenant — the tenant
// would be able to grant it to itself in the same request that sets everything
// else.
//
// Fields listed here are carried over from the stored value on update and are
// changed out of band, directly in the database.
func apiImmutableSettingFields(key types.SettingKey) []string {
	if key == types.SettingKeySAMLConfig {
		// "active" is Flexprice's approval that this tenant may serve SSO at
		// all, granted after its claim to the identity provider is checked.
		// A tenant that could set it would be approving itself.
		return []string{"active"}
	}
	return nil
}

// mergePreservingImmutableFields applies a partial update over the stored value
// while holding the key's API-immutable fields at what is already stored.
//
// A request naming a protected field is ignored rather than rejected: the field
// is not part of the API's contract, so a caller is not expected to know it
// exists — a configuration blob round-tripped through GET and back through PUT
// would otherwise fail on a field the caller never meant to set.
func mergePreservingImmutableFields(key types.SettingKey, stored, update map[string]interface{}) map[string]interface{} {
	protected := map[string]interface{}{}
	for _, field := range apiImmutableSettingFields(key) {
		if v, ok := stored[field]; ok {
			protected[field] = v
		}
	}

	for k, v := range update {
		stored[k] = v
	}

	for field, v := range protected {
		stored[field] = v
	}
	return stored
}

// fetchSetting fetches a setting from the repository
// Handles the distinction between tenant-level and environment-level settings
//
// WHEN TO USE:
//   - Use this helper instead of calling repository methods directly
//   - This ensures consistent handling of tenant-level vs environment-level settings
func (s *settingsService) fetchSetting(ctx context.Context, key types.SettingKey) (*settings.Setting, error) {
	if isTenantLevelSetting(key) {
		return s.SettingsRepo.GetTenantLevelSettingByKey(ctx, key)
	}
	return s.SettingsRepo.GetByKey(ctx, key)
}

// getDefaultValue returns the default value for a setting key as a typed struct
// Returns the default configuration when a setting doesn't exist in the database
func getDefaultValue[T any](key types.SettingKey) (T, error) {
	var zero T

	defaults, err := types.GetDefaultSettings()
	if err != nil {
		return zero, err
	}

	defaultSetting, exists := defaults[key]
	if !exists {
		return zero, ierr.NewErrorf("unknown setting key: %s", key).
			WithHintf("Unknown setting key: %s", key).
			Mark(ierr.ErrValidation)
	}

	return utils.ToStruct[T](defaultSetting.DefaultValue)
}

// resolvedValueMap returns the effective setting value as a map: default (from typed config) with fetched overlaid.
// Uses getDefaultValue[T] + ToMap so default is defined once; callers use the map as-is or ToStruct when they need a struct.
func resolvedValueMap[T any](key types.SettingKey, fetched map[string]interface{}) (map[string]interface{}, error) {
	config, err := getDefaultValue[T](key)
	if err != nil {
		return nil, err
	}
	resolvedSettingMap, err := utils.ToMap(config)
	if err != nil {
		return nil, err
	}
	for k, v := range fetched {
		resolvedSettingMap[k] = v
	}
	return resolvedSettingMap, nil
}

// GetSetting retrieves a setting and returns it as a typed struct
//
// WHEN TO USE:
//   - Use this when you need the setting value as a typed struct in your business logic
//   - Use this in other services (e.g., subscription service needs InvoiceConfig)
//   - Returns default values if setting doesn't exist
//   - Use this for type-safe access to setting values
//
// WHEN NOT TO USE:
//   - Don't use for API responses (use GetSettingByKey instead)
//   - Don't use if you need the Setting object with metadata (ID, timestamps, etc.)
//   - Don't call repository methods directly - always use service methods
//
// Example:
//
//	config, err := service.GetSetting[types.InvoiceConfig](ctx, types.SettingKeyInvoiceConfig)
//	if err != nil {
//	    return err
//	}
//	prefix := config.InvoiceNumberPrefix  // Type-safe access
func GetSetting[T any](s *settingsService, ctx context.Context, key types.SettingKey) (T, error) {
	var zero T

	setting, err := s.fetchSetting(ctx, key)
	if ierr.IsNotFound(err) {
		// Return default value if setting doesn't exist
		return getDefaultValue[T](key)
	}
	if err != nil {
		return zero, err
	}

	valueMap, err := resolvedValueMap[T](key, setting.Value)
	if err != nil {
		return zero, err
	}
	typedValue, err := utils.ToStruct[T](valueMap)
	if err != nil {
		return zero, ierr.WithError(err).
			WithHintf("Failed to convert setting %s", key).
			Mark(ierr.ErrValidation)
	}

	return typedValue, nil
}

// UpdateSetting updates a setting value (creates if doesn't exist)
//
// WHEN TO USE:
//   - Use this when you have a complete typed struct and want to update the setting
//   - Use this in other services when you need to update settings programmatically
//   - Automatically creates the setting if it doesn't exist
//   - Use this for full replacement of setting values
//
// WHEN NOT TO USE:
//   - Don't use for API endpoints with partial updates (use UpdateSettingByKey instead)
//   - Don't use if you need to merge with existing values
//   - Don't call repository methods directly - always use service methods
//
// Example:
//
//	config := types.InvoiceConfig{
//	    InvoiceNumberPrefix: "INV",
//	    InvoiceNumberFormat: types.InvoiceNumberFormatYYYYMM,
//	    ... other fields
//	}
//	err := service.UpdateSetting(ctx, types.SettingKeyInvoiceConfig, config)
func UpdateSetting[T types.SettingConfig](s *settingsService, ctx context.Context, key types.SettingKey, value T) error {
	// Validate the typed struct
	if err := value.Validate(); err != nil {
		return ierr.WithError(err).
			WithHintf("Validation failed for setting %s", key).
			Mark(ierr.ErrValidation)
	}

	// Convert typed struct to map for database storage
	valueMap, err := utils.ToMap(value)
	if err != nil {
		return ierr.WithError(err).
			WithHintf("Failed to convert setting %s", key).
			Mark(ierr.ErrValidation)
	}

	// Fetch existing setting to check if it exists
	setting, err := s.fetchSetting(ctx, key)

	if ierr.IsNotFound(err) {
		// Create new setting
		newSetting := &settings.Setting{
			ID:        types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SETTING),
			BaseModel: types.GetDefaultBaseModel(ctx),
			Key:       key,
			Value:     valueMap,
		}
		// Set environment_id only for environment-level settings
		if !isTenantLevelSetting(key) {
			newSetting.EnvironmentID = types.GetEnvironmentID(ctx)
		}
		return s.SettingsRepo.Create(ctx, newSetting)
	}
	if err != nil {
		return err
	}

	// Update existing setting
	setting.Value = valueMap
	return s.SettingsRepo.Update(ctx, setting)
}

// GetSettingByKey returns a setting as a DTO response for API endpoints
//
// WHEN TO USE:
//   - Use this for API endpoints (GET /api/v1/settings/{key})
//   - Returns the full Setting object with all metadata (ID, timestamps, etc.)
//   - Returns default values if setting doesn't exist (without ID)
//
// WHEN NOT TO USE:
//   - Don't use in business logic if you only need the typed struct (use GetSetting instead)
//   - Don't use if you need to work with the typed config directly
//   - Don't call repository methods directly - always use service methods
func (s *settingsService) GetSettingByKey(ctx context.Context, key types.SettingKey) (*dto.SettingResponse, error) {
	if err := s.requireSAMLEnabled(key); err != nil {
		return nil, err
	}
	if err := requireSuperAdminToReadSAMLConfig(ctx, key); err != nil {
		return nil, err
	}
	return s.GetSettingByKeyUnchecked(ctx, key)
}

// GetSettingByKeyUnchecked is the read itself, with no caller checks. See the
// interface for when it is legitimate to call.
func (s *settingsService) GetSettingByKeyUnchecked(ctx context.Context, key types.SettingKey) (*dto.SettingResponse, error) {
	switch key {
	case types.SettingKeyInvoiceConfig:
		return getSettingByKey[types.InvoiceConfig](s, ctx, key)
	case types.SettingKeySubscriptionConfig:
		return getSettingByKey[types.SubscriptionConfig](s, ctx, key)
	case types.SettingKeyInvoicePDFConfig:
		return getSettingByKey[types.InvoicePDFConfig](s, ctx, key)
	case types.SettingKeyTenantConfig:
		return getSettingByKey[types.TenantConfig](s, ctx, key)
	case types.SettingKeyCustomerOnboarding:
		return getSettingByKey[*workflowModels.WorkflowConfig](s, ctx, key)
	case types.SettingKeyPrepareProcessedEvents:
		return getSettingByKey[*workflowModels.WorkflowConfig](s, ctx, key)
	case types.SettingKeyCustomAnalytics:
		return getSettingByKey[types.CustomAnalyticsConfig](s, ctx, key)
	case types.SettingKeyWalletBalanceAlertConfig:
		return getSettingByKey[types.AlertSettings](s, ctx, key)
	case types.SettingKeySubscriptionAlertConfig, types.SettingKeyEntitlementAlertConfig:
		return getSettingByKey[types.AlertToggleConfig](s, ctx, key)
	case types.SettingKeyCustomerPortalConfig:
		return getSettingByKey[types.CustomerPortalConfig](s, ctx, key)
	case types.SettingKeyEventIngestionFilter:
		return getSettingByKey[types.EventIngestionFilterConfig](s, ctx, key)
	case types.SettingKeyPaymentMandateLimits:
		return getSettingByKey[types.PaymentMandateLimits](s, ctx, key)
	case types.SettingKeyBonusCreditsTopupConfig:
		return getSettingByKey[types.BonusCreditsTopupConfig](s, ctx, key)
	case types.SettingKeyDraftInvoiceRecomputeConfig:
		return getSettingByKey[types.DraftInvoiceRecomputeConfig](s, ctx, key)
	case types.SettingKeySAMLConfig:
		return getSettingByKey[types.SAMLConfig](s, ctx, key)
	case types.SettingKeyWalletTopupConfig:
		return getSettingByKey[types.WalletTopupConfig](s, ctx, key)
	default:
		return nil, ierr.NewErrorf("unknown setting key: %s", key).
			WithHintf("Unknown setting key: %s", key).
			Mark(ierr.ErrValidation)
	}
}

// UpdateSettingByKey updates a setting with partial values (merges with existing)
//
// WHEN TO USE:
//   - Use this for API endpoints (PATCH /api/v1/settings/{key})
//   - Accepts partial updates and merges with existing values
//   - Validates the merged result before saving
//
// WHEN NOT TO USE:
//   - Don't use if you have a complete typed struct (use UpdateSetting instead)
//   - Don't use in business logic if you want to replace the entire setting
//   - Don't call repository methods directly - always use service methods
func (s *settingsService) UpdateSettingByKey(ctx context.Context, key types.SettingKey, req *dto.UpdateSettingRequest) (*dto.SettingResponse, error) {
	if err := s.requireSAMLEnabled(key); err != nil {
		return nil, err
	}

	if err := req.Validate(key); err != nil {
		return nil, err
	}

	switch key {
	case types.SettingKeyInvoiceConfig:
		return updateSettingByKey[types.InvoiceConfig](s, ctx, key, req)
	case types.SettingKeySubscriptionConfig:
		return updateSettingByKey[types.SubscriptionConfig](s, ctx, key, req)
	case types.SettingKeyInvoicePDFConfig:
		return updateSettingByKey[types.InvoicePDFConfig](s, ctx, key, req)
	case types.SettingKeyTenantConfig:
		return updateSettingByKey[types.TenantConfig](s, ctx, key, req)
	case types.SettingKeyCustomerOnboarding:
		return updateSettingByKey[*workflowModels.WorkflowConfig](s, ctx, key, req)
	case types.SettingKeyPrepareProcessedEvents:
		return updateSettingByKey[*workflowModels.WorkflowConfig](s, ctx, key, req)
	case types.SettingKeyCustomAnalytics:
		return updateSettingByKey[types.CustomAnalyticsConfig](s, ctx, key, req)
	case types.SettingKeyWalletBalanceAlertConfig:
		return updateSettingByKey[*types.AlertSettings](s, ctx, key, req)
	case types.SettingKeySubscriptionAlertConfig, types.SettingKeyEntitlementAlertConfig:
		return updateSettingByKey[*types.AlertToggleConfig](s, ctx, key, req)
	case types.SettingKeyCustomerPortalConfig:
		return updateSettingByKey[types.CustomerPortalConfig](s, ctx, key, req)
	case types.SettingKeyEventIngestionFilter:
		return updateSettingByKey[types.EventIngestionFilterConfig](s, ctx, key, req)
	case types.SettingKeyPaymentMandateLimits:
		return updateSettingByKey[types.PaymentMandateLimits](s, ctx, key, req)
	case types.SettingKeyBonusCreditsTopupConfig:
		return updateSettingByKey[types.BonusCreditsTopupConfig](s, ctx, key, req)
	case types.SettingKeyDraftInvoiceRecomputeConfig:
		return updateSettingByKey[types.DraftInvoiceRecomputeConfig](s, ctx, key, req)
	case types.SettingKeySAMLConfig:
		return updateSettingByKey[types.SAMLConfig](s, ctx, key, req)
	case types.SettingKeyWalletTopupConfig:
		return updateSettingByKey[types.WalletTopupConfig](s, ctx, key, req)
	default:
		return nil, ierr.NewErrorf("unknown setting key: %s", key).
			WithHintf("Unknown setting key: %s", key).
			Mark(ierr.ErrValidation)
	}
}

// DeleteSettingByKey deletes a setting by key
//
// WHEN TO USE:
//   - Use this for API endpoints (DELETE /api/v1/settings/{key})
//   - Handles both tenant-level and environment-level settings
//
// WHEN NOT TO USE:
//   - Don't call repository methods directly - always use service methods
func (s *settingsService) DeleteSettingByKey(ctx context.Context, key types.SettingKey) error {
	if err := s.requireSAMLEnabled(key); err != nil {
		return err
	}

	// Check if setting exists
	_, err := s.fetchSetting(ctx, key)
	if ierr.IsNotFound(err) {
		return ierr.NewErrorf("setting with key '%s' not found", key).
			WithHintf("Setting with key %s not found", key).
			Mark(ierr.ErrNotFound)
	}
	if err != nil {
		return err
	}

	// Delete based on setting type (tenant-level vs environment-level)
	if isTenantLevelSetting(key) {
		return s.SettingsRepo.DeleteTenantLevelSettingByKey(ctx, key)
	}
	return s.SettingsRepo.DeleteByKey(ctx, key)
}

// getSettingByKey fetches a setting and returns it as a DTO response.
// If setting does not exist: returns default value.
// If setting exists: returns default merged with fetched (fetched keys overwrite default).
func getSettingByKey[T any](s *settingsService, ctx context.Context, key types.SettingKey) (*dto.SettingResponse, error) {
	setting, err := s.fetchSetting(ctx, key)
	if err != nil && !ierr.IsNotFound(err) {
		return nil, err
	}

	var fetched map[string]interface{}
	if setting != nil {
		fetched = setting.Value
	}
	valueMap, err := resolvedValueMap[T](key, fetched)
	if err != nil {
		return nil, err
	}

	// Persisted setting: include ID, base model, key, value, and environment ID.
	if setting != nil {
		return dto.NewSettingResponse(&settings.Setting{
			ID:            setting.ID,
			BaseModel:     setting.BaseModel,
			Key:           setting.Key,
			Value:         valueMap,
			EnvironmentID: setting.EnvironmentID,
		}), nil
	}
	// Default setting: no ID, base model, key, value, and environment ID (no DB record yet).
	return dto.NewSettingResponse(&settings.Setting{Key: key, Value: valueMap}), nil
}

// updateSettingByKey updates a setting with partial values from a request
// Internal helper used by UpdateSettingByKey to handle type-specific logic
func updateSettingByKey[T types.SettingConfig](s *settingsService, ctx context.Context, key types.SettingKey, req *dto.UpdateSettingRequest) (*dto.SettingResponse, error) {
	// Get current setting as typed struct
	current, err := GetSetting[T](s, ctx, key)
	if err != nil {
		return nil, err
	}

	// Convert current setting to map
	currentMap, err := utils.ToMap(current)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHintf("Failed to convert current setting %s to map", key).
			Mark(ierr.ErrValidation)
	}

	// Merge request values with current values
	currentMap = mergePreservingImmutableFields(key, currentMap, req.Value)

	// Convert merged map back to typed struct for validation
	merged, err := utils.ToStruct[T](currentMap)
	if err != nil {
		return nil, err
	}

	// Update with merged and validated typed struct
	if err := UpdateSetting[T](s, ctx, key, merged); err != nil {
		return nil, err
	}

	// Return updated setting
	return s.GetSettingByKey(ctx, key)
}
