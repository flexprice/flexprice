package types

import (
	"context"
	"testing"
)

func TestActorRoundTrip(t *testing.T) {
	ctx := SetActor(context.Background(), Actor{Type: ActorTypeAPIKey, ID: "sec_1", Label: "Billing Sync", UserID: "user_1"})
	got := GetActor(ctx)
	if got.Type != ActorTypeAPIKey || got.ID != "sec_1" || got.UserID != "user_1" {
		t.Fatalf("unexpected actor %+v", got)
	}
}

func TestGetUserIDPrefersActor(t *testing.T) {
	ctx := SetUserID(context.Background(), "legacy")
	ctx = SetActor(ctx, Actor{Type: ActorTypeUser, ID: "user_9", Label: "Alice"})
	if got := GetUserID(ctx); got != "user_9" {
		t.Fatalf("want user_9, got %q", got)
	}
	ctx = SetActor(ctx, Actor{Type: ActorTypeAPIKey, ID: "sec_1", UserID: "user_2"})
	if got := GetUserID(ctx); got != "user_2" {
		t.Fatalf("want owning user user_2, got %q", got)
	}
}

func TestGetUserIDFallsBackToLegacy(t *testing.T) {
	ctx := SetUserID(context.Background(), "legacy")
	if got := GetUserID(ctx); got != "legacy" {
		t.Fatalf("want legacy, got %q", got)
	}
}

func TestSystemActorHasNoUser(t *testing.T) {
	a := SystemActor("DailyDraftAndComputeWorkflow", "Billing workflow")
	if a.Type != ActorTypeSystem || a.UserID != "" {
		t.Fatalf("unexpected %+v", a)
	}
}
