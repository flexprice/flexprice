# Adding New Settings - Complete Guide

This guide shows you how to add a new setting to the FlexPrice system. The settings system uses a simple, type-safe approach with centralized conversion utilities.

## Quick Overview

Adding a new setting requires **5 simple steps**:

1. Define the setting key constant
2. Create the typed struct with validation
3. Add default values
4. Add validation case
5. Add service layer switch cases (3 places)

The centralized conversion utilities (`ToStruct` and `ToMap`) handle everything else automatically - no complex patterns needed!

---

## Step-by-Step Guide

### Step 1: Define the Setting Key

**File:** `internal/types/settings.go`

Add your new setting key constant:

```go
const (
    SettingKeyInvoiceConfig      SettingKey = "invoice_config"
    SettingKeySubscriptionConfig SettingKey = "subscription_config"
    SettingKeyInvoicePDFConfig   SettingKey = "invoice_pdf_config"
    SettingKeyEnvConfig          SettingKey = "env_config"
    SettingKeyNotificationConfig SettingKey = "notification_config"  // ← NEW
)
```

---

### Step 2: Create the Typed Struct

**File:** `internal/types/settings.go`

Define your configuration struct with validation tags:

```go
// NotificationConfig represents notification settings
type NotificationConfig struct {
    EmailEnabled bool     `json:"email_enabled"`
    SlackEnabled bool     `json:"slack_enabled"`
    WebhookURL   string   `json:"webhook_url" validate:"omitempty,url"`
    RetryCount   int      `json:"retry_count" validate:"required,min=1,max=5"`
}

// Validate implements SettingConfig interface
func (c NotificationConfig) Validate() error {
    return validator.ValidateRequest(c)
}
```

**Key Points:**
- Use `json` tags for field mapping (must match API/DB keys)
- Use `validate` tags for validation rules
- Implement `SettingConfig` interface (Validate method)
- The struct must be exported (starts with capital letter)
- Fields must be exported (capital first letter)

**Common Validation Tags:**

| Tag         | Example                    | Description             |
| ----------- | -------------------------- | ----------------------- |
| `required`  | `validate:"required"`      | Field must be present   |
| `min`       | `validate:"min=1"`         | Minimum value           |
| `max`       | `validate:"max=100"`       | Maximum value           |
| `email`     | `validate:"email"`         | Valid email format      |
| `url`       | `validate:"url"`           | Valid URL format        |
| `oneof`     | `validate:"oneof=a b c"`   | Value must be one of    |
| `omitempty` | `validate:"omitempty,url"` | Skip if empty           |
| `dive`      | `validate:"dive,required"` | Validate slice elements |

---

### Step 3: Add Default Values

**File:** `internal/types/settings.go`

Add your default configuration to `GetDefaultSettings()`:

```go
func GetDefaultSettings() map[SettingKey]DefaultSettingValue {
    return map[SettingKey]DefaultSettingValue{
        // ... existing settings ...
        
        SettingKeyNotificationConfig: {
            Key: SettingKeyNotificationConfig,
            DefaultValue: map[string]interface{}{
                "email_enabled": true,
                "slack_enabled": false,
                "webhook_url":   "",
                "retry_count":   3,
            },
            Description: "Notification preferences and retry configuration",
            Required:    true,
        },
    }
}
```

---

### Step 4: Add Validation Case

**File:** `internal/types/settings.go`

Add your setting to the validation switch in `ValidateSettingValue()`:

```go
func ValidateSettingValue(key string, value map[string]interface{}) error {
    if value == nil {
        return errors.New("value cannot be nil")
    }

    settingKey := SettingKey(key)

    switch settingKey {
    case SettingKeyInvoiceConfig:
        // ... existing cases ...
    
    case SettingKeyNotificationConfig:  // ← NEW
        config, err := convertToStruct[NotificationConfig](value)
        if err != nil {
            return err
        }
        return config.Validate()

    default:
        return ierr.NewErrorf("unknown setting key: %s", key).
            WithHintf("Unknown setting key: %s", key).
            Mark(ierr.ErrValidation)
    }
}
```

---

### Step 5: Add Service Layer Switch Cases

**File:** `internal/service/settings.go`

Add your setting to **three** switch statements:

#### 5a. GetSettingByKey

```go
func (s *settingsService) GetSettingByKey(ctx context.Context, key types.SettingKey) (*dto.SettingResponse, error) {
    switch key {
    case types.SettingKeyInvoiceConfig:
        return getSettingByKey[types.InvoiceConfig](s, ctx, key)
    // ... other cases ...
    case types.SettingKeyNotificationConfig:  // ← NEW
        return getSettingByKey[types.NotificationConfig](s, ctx, key)
    default:
        return nil, ierr.NewErrorf("unknown setting key: %s", key).Mark(ierr.ErrValidation)
    }
}
```

#### 5b. UpdateSettingByKey

```go
func (s *settingsService) UpdateSettingByKey(ctx context.Context, key types.SettingKey, req *dto.UpdateSettingRequest) (*dto.SettingResponse, error) {
    if err := req.Validate(key); err != nil {
        return nil, err
    }

    switch key {
    case types.SettingKeyInvoiceConfig:
        return updateSettingByKey[types.InvoiceConfig](s, ctx, key, req)
    // ... other cases ...
    case types.SettingKeyNotificationConfig:  // ← NEW
        return updateSettingByKey[types.NotificationConfig](s, ctx, key, req)
    default:
        return nil, ierr.NewErrorf("unknown setting key: %s", key).Mark(ierr.ErrValidation)
    }
}
```

#### 5c. DeleteSettingByKey

```go
func (s *settingsService) DeleteSettingByKey(ctx context.Context, key types.SettingKey) error {
    switch key {
    case types.SettingKeyInvoiceConfig:
        return DeleteSetting[types.InvoiceConfig](s, ctx, key)
    // ... other cases ...
    case types.SettingKeyNotificationConfig:  // ← NEW
        return DeleteSetting[types.NotificationConfig](s, ctx, key)
    default:
        return ierr.NewErrorf("unknown setting key: %s", key).Mark(ierr.ErrValidation)
    }
}
```

---

## Complete Example

Here's a complete example of adding a `PaymentConfig` setting:

```go
// internal/types/settings.go

// 1. Add constant
const (
    SettingKeyPaymentConfig SettingKey = "payment_config"
)

// 2. Define struct
type PaymentConfig struct {
    Provider          string   `json:"provider" validate:"required,oneof=stripe razorpay"`
    AutoCaptureEnabled bool    `json:"auto_capture_enabled"`
    TimeoutSeconds    int      `json:"timeout_seconds" validate:"required,min=10,max=300"`
    AllowedCurrencies []string `json:"allowed_currencies" validate:"required,min=1"`
}

func (c PaymentConfig) Validate() error {
    return validator.ValidateRequest(c)
}

// 3. Add to defaults
func GetDefaultSettings() map[SettingKey]DefaultSettingValue {
    return map[SettingKey]DefaultSettingValue{
        SettingKeyPaymentConfig: {
            Key: SettingKeyPaymentConfig,
            DefaultValue: map[string]interface{}{
                "provider":              "stripe",
                "auto_capture_enabled":  true,
                "timeout_seconds":       60,
                "allowed_currencies":    []string{"USD", "EUR"},
            },
            Description: "Payment gateway configuration",
            Required:    true,
        },
    }
}

// 4. Add to validation
func ValidateSettingValue(key string, value map[string]interface{}) error {
    settingKey := SettingKey(key)
    switch settingKey {
    case SettingKeyPaymentConfig:
        config, err := convertToStruct[PaymentConfig](value)
        if err != nil {
            return err
        }
        return config.Validate()
    }
}

// 5. Add to service layer (internal/service/settings.go)
// In GetSettingByKey:
case types.SettingKeyPaymentConfig:
    return getSettingByKey[types.PaymentConfig](s, ctx, key)

// In UpdateSettingByKey:
case types.SettingKeyPaymentConfig:
    return updateSettingByKey[types.PaymentConfig](s, ctx, key, req)

// In DeleteSettingByKey:
case types.SettingKeyPaymentConfig:
    return DeleteSetting[types.PaymentConfig](s, ctx, key)
```

---

## Using Your New Setting

### In Code

```go
// Get the setting (returns typed struct)
paymentConfig, err := service.GetSetting[types.PaymentConfig](ctx, types.SettingKeyPaymentConfig)
if err != nil {
    return err
}

// Use it with type safety
if paymentConfig.AutoCaptureEnabled {
    timeout := paymentConfig.TimeoutSeconds
    provider := paymentConfig.Provider
}

// Update it
paymentConfig.TimeoutSeconds = 120
paymentConfig.Validate()  // Type-safe validation
err = service.UpdateSetting(ctx, types.SettingKeyPaymentConfig, paymentConfig)
```

### Via API

```bash
# Get setting
GET /api/v1/settings/payment_config

# Update setting
PATCH /api/v1/settings/payment_config
{
  "value": {
    "timeout_seconds": 120
  }
}

# Delete setting
DELETE /api/v1/settings/payment_config
```

---

## Testing Your Setting

Create tests to verify conversion works:

```go
// internal/types/settings/conversion_test.go

func TestToStruct_PaymentConfig(t *testing.T) {
    input := map[string]interface{}{
        "provider":              "stripe",
        "auto_capture_enabled":  true,
        "timeout_seconds":       60,
        "allowed_currencies":    []string{"USD", "EUR"},
    }
    
    result, err := ToStruct[types.PaymentConfig](input)
    require.NoError(t, err)
    assert.Equal(t, "stripe", result.Provider)
    assert.True(t, result.AutoCaptureEnabled)
    assert.Equal(t, 60, result.TimeoutSeconds)
}

func TestRoundTrip_PaymentConfig(t *testing.T) {
    original := types.PaymentConfig{
        Provider:           "stripe",
        AutoCaptureEnabled: true,
        TimeoutSeconds:     60,
        AllowedCurrencies:  []string{"USD"},
    }
    
    // Convert to map
    asMap, err := ToMap(original)
    require.NoError(t, err)
    
    // Convert back
    result, err := ToStruct[types.PaymentConfig](asMap)
    require.NoError(t, err)
    
    assert.Equal(t, original, result)
}
```

---

## Checklist

Before submitting your PR, ensure:

- [ ] Setting key constant added
- [ ] Typed struct created with `Validate()` method
- [ ] Default values added to `GetDefaultSettings()`
- [ ] Validation case added to `ValidateSettingValue()`
- [ ] Service layer `GetSettingByKey` updated
- [ ] Service layer `UpdateSettingByKey` updated
- [ ] Service layer `DeleteSettingByKey` updated
- [ ] Tests written and passing
- [ ] No linter errors

---

## Important Notes

### ✅ DO

- Use descriptive struct and field names
- Add appropriate validation tags
- Provide sensible default values
- Document complex fields with comments
- Test your validation rules
- Export structs and fields (capital first letter)

### ❌ DON'T

- Skip validation implementation
- Use generic field names like `value` or `data`
- Forget to add to all 3 service switch statements
- Use nested complex structures (keep it flat when possible)
- Omit default values
- Import `internal/types/settings` from `internal/types` (causes import cycle)

---

## Troubleshooting

### "Unknown setting key" Error

**Cause:** Setting key not added to all required places.

**Solution:** Check these 6 locations:
1. Constant defined in `internal/types/settings.go`
2. Added to `GetDefaultSettings()` 
3. Added to `ValidateSettingValue()` switch
4. Added to `GetSettingByKey()` switch
5. Added to `UpdateSettingByKey()` switch
6. Added to `DeleteSettingByKey()` switch

### Validation Fails

**Cause:** Data doesn't match validation rules.

**Solution:**
1. Ensure `Validate()` method exists and calls `validator.ValidateRequest(c)`
2. Check validation tags match your data types
3. Test validation directly: `config.Validate()`

### Type Conversion Error

**Cause:** Field names or types don't match.

**Solution:**
1. Ensure JSON tags match map keys exactly
2. The `ToStruct` utility handles type coercion automatically (float64 → int)

### Import Cycle Error

**Cause:** Trying to import `internal/types/settings` from `internal/types`.

**Solution:** The `types` package has inline helpers (`convertToStruct`, `ConvertToMap`) to avoid this. Use the utilities from service/repository layers instead.

### Default Values Not Applied

**Cause:** Defaults not set properly.

**Solution:**
1. Check defaults are in `GetDefaultSettings()`
2. Service automatically returns defaults on NotFound

---

## How It Works

The settings system uses centralized, stateless conversion utilities:

- **`ToStruct[T](map) → struct`** - Converts DB map to typed struct
- **`ToMap(struct) → map`** - Converts typed struct to DB map

These utilities automatically handle:
- ✅ Map to struct conversion
- ✅ Struct to map conversion
- ✅ Type coercion (float64 → int)
- ✅ JSON serialization

You only need to:
1. Define your struct
2. Add validation
3. Wire it up in 5 places

**Simple!** 🎯

---

## Files You'll Modify

| File                                         | What to Add                                   |
| -------------------------------------------- | --------------------------------------------- |
| `internal/types/settings.go`                 | Steps 1-4 (key, struct, defaults, validation) |
| `internal/service/settings.go`               | Step 5 (3 switch cases)                       |
| `internal/types/settings/conversion_test.go` | Tests (optional but recommended)              |

---

## Quick Reference

**The 5 Steps:**
1. Define setting key constant
2. Create typed struct with `Validate()` method
3. Add default values to `GetDefaultSettings()`
4. Add validation case to `ValidateSettingValue()`
5. Add 3 switch cases in service layer

**Run Tests:**
```bash
go test ./internal/types/settings/... -v
```

**Verify Compilation:**
```bash
go build ./internal/service/... ./internal/repository/ent/...
```

That's it! The conversion utilities handle the rest automatically. 🚀

