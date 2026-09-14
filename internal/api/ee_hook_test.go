package api

import "testing"

func resetRouteRegistrars(t *testing.T) {
	t.Helper()
	saved := eeRouteRegistrars
	eeRouteRegistrars = nil
	t.Cleanup(func() { eeRouteRegistrars = saved })
}

func TestApplyEERoutes_CommunityNoop(t *testing.T) {
	resetRouteRegistrars(t)
	called := false
	applyEERoutes(EERouteParams{})
	if called {
		t.Fatal("unreachable")
	}
	if EERouteRegistrarCount() != 0 {
		t.Fatalf("expected 0 registrars, got %d", EERouteRegistrarCount())
	}
}

func TestApplyEERoutes_RunsRegistrar(t *testing.T) {
	resetRouteRegistrars(t)
	ran := false
	RegisterEERoutes(func(EERouteParams) { ran = true })
	applyEERoutes(EERouteParams{})
	if !ran {
		t.Fatal("registrar was not invoked")
	}
}
