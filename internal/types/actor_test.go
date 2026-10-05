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

func TestWithDerivedSystemActor(t *testing.T) {
	cases := []struct {
		name     string
		in       Actor
		wantType ActorType
		wantUser string
	}{
		{"user keeps owner", Actor{Type: ActorTypeUser, ID: "user_9", Label: "Alice"}, ActorTypeSystem, "user_9"},
		{"api key keeps owning user", Actor{Type: ActorTypeAPIKey, ID: "sec_1", UserID: "user_2"}, ActorTypeSystem, "user_2"},
		{"portal has no owner", Actor{Type: ActorTypeCustomerPortal, ID: "cust_1"}, ActorTypeSystem, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ctx := WithDerivedSystemActor(SetActor(context.Background(), c.in), "subscription_billing", "Subscription billing")
			got := GetActor(ctx)
			if got.Type != c.wantType || got.ID != "subscription_billing" || got.Label != "Subscription billing" || got.UserID != c.wantUser {
				t.Fatalf("unexpected actor %+v", got)
			}
		})
	}
}

func TestWithDerivedSystemActorLeavesSystemAndEmptyAlone(t *testing.T) {
	sys := SetActor(context.Background(), SystemActor("webhook", "Webhook x"))
	if got := GetActor(WithDerivedSystemActor(sys, "subscription_billing", "Subscription billing")); got.ID != "webhook" {
		t.Fatalf("system actor must not be replaced, got %+v", got)
	}
	if got := GetActor(WithDerivedSystemActor(context.Background(), "subscription_billing", "Subscription billing")); got.Type != "" {
		t.Fatalf("no actor must stay no actor, got %+v", got)
	}
}

func TestDerivedSystemActorKeepsCreatedByOwner(t *testing.T) {
	ctx := WithDerivedSystemActor(SetActor(context.Background(), Actor{Type: ActorTypeUser, ID: "user_9"}), "subscription_billing", "Subscription billing")
	if got := GetUserID(ctx); got != "user_9" {
		t.Fatalf("created_by must stay the requesting user, got %q", got)
	}
}
