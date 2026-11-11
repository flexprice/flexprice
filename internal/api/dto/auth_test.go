package dto

import (
	"encoding/json"
	"testing"

	"github.com/gin-gonic/gin/binding"
	"github.com/stretchr/testify/require"
)

func TestAuthRequests_PaddedEmailNormalisedBeforeValidation(t *testing.T) {
	var signup SignUpRequest
	require.NoError(t, json.Unmarshal([]byte(`{"email":"  Alice@Example.com "}`), &signup))
	require.NoError(t, binding.Validator.ValidateStruct(&signup))
	require.Equal(t, "alice@example.com", signup.Email)

	var login LoginRequest
	require.NoError(t, json.Unmarshal([]byte(`{"email":"  Alice@Example.com ","password":"password1"}`), &login))
	require.NoError(t, binding.Validator.ValidateStruct(&login))
	require.Equal(t, "alice@example.com", login.Email)
}
