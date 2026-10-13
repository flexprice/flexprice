package admin

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/domain/environment"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewListEnvironmentsResponse(t *testing.T) {
	ist := time.FixedZone("IST", 5*60*60+30*60)
	envs := []*environment.Environment{{
		ID:   "env_1",
		Name: "Production",
		Type: types.EnvironmentProduction,
		BaseModel: types.BaseModel{
			TenantID:  "tenant_1",
			Status:    types.StatusPublished,
			CreatedAt: time.Date(2026, 4, 1, 15, 30, 0, 0, ist),
			UpdatedAt: time.Date(2026, 4, 2, 15, 30, 0, 0, ist),
		},
	}}

	body, err := json.Marshal(NewListEnvironmentsResponse(envs))

	require.NoError(t, err)
	assert.JSONEq(t, `{"items": [{
		"id": "env_1",
		"name": "Production",
		"type": "production",
		"tenant_id": "tenant_1",
		"status": "published",
		"created_at": "2026-04-01T10:00:00Z",
		"updated_at": "2026-04-02T10:00:00Z"
	}]}`, string(body))
}

func TestNewListEnvironmentsResponse_NoEnvironments(t *testing.T) {
	body, err := json.Marshal(NewListEnvironmentsResponse(nil))

	require.NoError(t, err)
	assert.JSONEq(t, `{"items": []}`, string(body))
}
