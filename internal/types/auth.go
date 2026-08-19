package types

type AuthProvider string

const (
	AuthProviderFlexprice AuthProvider = "flexprice"
	AuthProviderSupabase  AuthProvider = "supabase"
	AuthProviderSAML      AuthProvider = "saml"
)
