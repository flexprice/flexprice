package models

import (
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateWalletActionConfig_Validate(t *testing.T) {
	day := types.CreditGrantExpiryDurationUnitDays
	week := types.CreditGrantExpiryDurationUnitWeeks
	invalidUnit := types.CreditGrantExpiryDurationUnit("HOUR")
	duration30 := 30
	duration0 := 0
	durationNeg := -1

	tests := []struct {
		name    string
		cfg     CreateWalletActionConfig
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid currency only",
			cfg: CreateWalletActionConfig{
				Currency: "USD",
			},
			wantErr: false,
		},
		{
			name: "valid prepaid wallet type",
			cfg: CreateWalletActionConfig{
				Currency:   "USD",
				WalletType: types.WalletTypePrePaid,
			},
			wantErr: false,
		},
		{
			name: "valid postpaid wallet type",
			cfg: CreateWalletActionConfig{
				Currency:   "USD",
				WalletType: types.WalletTypePostPaid,
			},
			wantErr: false,
		},
		{
			name: "invalid wallet type",
			cfg: CreateWalletActionConfig{
				Currency:   "USD",
				WalletType: types.WalletType("INVALID"),
			},
			wantErr: true,
			errMsg:  "invalid wallet type",
		},
		{
			name: "valid credits without expiry",
			cfg: CreateWalletActionConfig{
				Currency:             "USD",
				InitialCreditsToLoad: decimal.NewFromInt(100),
			},
			wantErr: false,
		},
		{
			name: "valid credits with duration pair",
			cfg: CreateWalletActionConfig{
				Currency:                             "USD",
				InitialCreditsToLoad:                 decimal.NewFromInt(100),
				InitialCreditsExpirationDuration:     &duration30,
				InitialCreditsExpirationDurationUnit: &day,
			},
			wantErr: false,
		},
		{
			name: "missing currency",
			cfg: CreateWalletActionConfig{
				Currency: "",
			},
			wantErr: true,
			errMsg:  "currency is required",
		},
		{
			name: "negative initial credits",
			cfg: CreateWalletActionConfig{
				Currency:             "USD",
				InitialCreditsToLoad: decimal.NewFromInt(-1),
			},
			wantErr: true,
			errMsg:  "initial_credits_to_load cannot be negative",
		},
		{
			name: "duration without unit",
			cfg: CreateWalletActionConfig{
				Currency:                         "USD",
				InitialCreditsToLoad:             decimal.NewFromInt(100),
				InitialCreditsExpirationDuration: &duration30,
			},
			wantErr: true,
			errMsg:  "expiration_duration and expiration_duration_unit must be set together",
		},
		{
			name: "unit without duration",
			cfg: CreateWalletActionConfig{
				Currency:                             "USD",
				InitialCreditsToLoad:                 decimal.NewFromInt(100),
				InitialCreditsExpirationDurationUnit: &week,
			},
			wantErr: true,
			errMsg:  "expiration_duration and expiration_duration_unit must be set together",
		},
		{
			name: "duration pair without credits",
			cfg: CreateWalletActionConfig{
				Currency:                             "USD",
				InitialCreditsExpirationDuration:     &duration30,
				InitialCreditsExpirationDurationUnit: &day,
			},
			wantErr: true,
			errMsg:  "expiration_duration requires initial_credits_to_load > 0",
		},
		{
			name: "duration pair with zero credits",
			cfg: CreateWalletActionConfig{
				Currency:                             "USD",
				InitialCreditsToLoad:                 decimal.Zero,
				InitialCreditsExpirationDuration:     &duration30,
				InitialCreditsExpirationDurationUnit: &day,
			},
			wantErr: true,
			errMsg:  "expiration_duration requires initial_credits_to_load > 0",
		},
		{
			name: "duration not greater than zero",
			cfg: CreateWalletActionConfig{
				Currency:                             "USD",
				InitialCreditsToLoad:                 decimal.NewFromInt(100),
				InitialCreditsExpirationDuration:     &duration0,
				InitialCreditsExpirationDurationUnit: &day,
			},
			wantErr: true,
			errMsg:  "expiration_duration must be greater than 0",
		},
		{
			name: "negative duration",
			cfg: CreateWalletActionConfig{
				Currency:                             "USD",
				InitialCreditsToLoad:                 decimal.NewFromInt(100),
				InitialCreditsExpirationDuration:     &durationNeg,
				InitialCreditsExpirationDurationUnit: &day,
			},
			wantErr: true,
			errMsg:  "expiration_duration must be greater than 0",
		},
		{
			name: "invalid duration unit",
			cfg: CreateWalletActionConfig{
				Currency:                             "USD",
				InitialCreditsToLoad:                 decimal.NewFromInt(100),
				InitialCreditsExpirationDuration:     &duration30,
				InitialCreditsExpirationDurationUnit: &invalidUnit,
			},
			wantErr: true,
			errMsg:  "invalid credit grant expiry duration unit",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestCreateWalletActionConfig_ToDTO(t *testing.T) {
	day := types.CreditGrantExpiryDurationUnitDays
	month := types.CreditGrantExpiryDurationUnitMonths
	duration30 := 30
	duration2 := 2

	params := &WorkflowActionParams{CustomerID: "cust_123"}

	t.Run("credits with duration maps to InitialCreditsExpiryDateUTC only", func(t *testing.T) {
		before := time.Now().UTC()
		cfg := CreateWalletActionConfig{
			Currency:                             "USD",
			ConversionRate:                       decimal.NewFromInt(1),
			InitialCreditsToLoad:                 decimal.NewFromInt(100),
			InitialCreditsExpirationDuration:     &duration30,
			InitialCreditsExpirationDurationUnit: &day,
		}

		out, err := cfg.ToDTO(params)
		require.NoError(t, err)
		after := time.Now().UTC()

		req, ok := out.(*dto.CreateWalletRequest)
		require.True(t, ok)
		assert.Equal(t, "cust_123", req.CustomerID)
		assert.Equal(t, "USD", req.Currency)
		assert.True(t, req.InitialCreditsToLoad.Equal(decimal.NewFromInt(100)))
		require.NotNil(t, req.InitialCreditsExpiryDateUTC)
		assert.Nil(t, req.InitialCreditsToLoadExpiryDate)

		expectedMin := before.Add(30 * 24 * time.Hour)
		expectedMax := after.Add(30 * 24 * time.Hour)
		assert.False(t, req.InitialCreditsExpiryDateUTC.Before(expectedMin))
		assert.False(t, req.InitialCreditsExpiryDateUTC.After(expectedMax))
	})

	t.Run("credits with month duration uses AddDate", func(t *testing.T) {
		now := time.Now().UTC()
		cfg := CreateWalletActionConfig{
			Currency:                             "USD",
			InitialCreditsToLoad:                 decimal.NewFromInt(50),
			InitialCreditsExpirationDuration:     &duration2,
			InitialCreditsExpirationDurationUnit: &month,
		}

		out, err := cfg.ToDTO(params)
		require.NoError(t, err)

		req := out.(*dto.CreateWalletRequest)
		require.NotNil(t, req.InitialCreditsExpiryDateUTC)
		assert.Nil(t, req.InitialCreditsToLoadExpiryDate)

		expected := now.AddDate(0, 2, 0)
		// Allow small clock skew between now capture and ToDTO
		diff := req.InitialCreditsExpiryDateUTC.Sub(expected)
		assert.LessOrEqual(t, diff.Abs(), 2*time.Second)
	})

	t.Run("credits without duration leaves expiry nil", func(t *testing.T) {
		cfg := CreateWalletActionConfig{
			Currency:             "USD",
			InitialCreditsToLoad: decimal.NewFromInt(100),
		}

		out, err := cfg.ToDTO(params)
		require.NoError(t, err)

		req := out.(*dto.CreateWalletRequest)
		assert.True(t, req.InitialCreditsToLoad.Equal(decimal.NewFromInt(100)))
		assert.Nil(t, req.InitialCreditsExpiryDateUTC)
		assert.Nil(t, req.InitialCreditsToLoadExpiryDate)
	})

	t.Run("no credits leaves amount zero and expiry nil", func(t *testing.T) {
		cfg := CreateWalletActionConfig{
			Currency: "USD",
		}

		out, err := cfg.ToDTO(params)
		require.NoError(t, err)

		req := out.(*dto.CreateWalletRequest)
		assert.True(t, req.InitialCreditsToLoad.IsZero())
		assert.Nil(t, req.InitialCreditsExpiryDateUTC)
		assert.Nil(t, req.InitialCreditsToLoadExpiryDate)
	})

	t.Run("default conversion rate when zero", func(t *testing.T) {
		cfg := CreateWalletActionConfig{
			Currency: "EUR",
		}

		out, err := cfg.ToDTO(params)
		require.NoError(t, err)

		req := out.(*dto.CreateWalletRequest)
		assert.True(t, req.ConversionRate.Equal(decimal.NewFromInt(1)))
		assert.Equal(t, "EUR", req.Currency)
	})

	t.Run("maps wallet type to DTO", func(t *testing.T) {
		cfg := CreateWalletActionConfig{
			Currency:   "USD",
			WalletType: types.WalletTypePostPaid,
		}

		out, err := cfg.ToDTO(params)
		require.NoError(t, err)

		req := out.(*dto.CreateWalletRequest)
		assert.Equal(t, types.WalletTypePostPaid, req.WalletType)
	})

	t.Run("empty wallet type leaves DTO empty for CreateWallet default", func(t *testing.T) {
		cfg := CreateWalletActionConfig{
			Currency: "USD",
		}

		out, err := cfg.ToDTO(params)
		require.NoError(t, err)

		req := out.(*dto.CreateWalletRequest)
		assert.Equal(t, types.WalletType(""), req.WalletType)
	})

	t.Run("invalid params type", func(t *testing.T) {
		cfg := CreateWalletActionConfig{Currency: "USD"}
		_, err := cfg.ToDTO("not-params")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid parameters")
	})
}

