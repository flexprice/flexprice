package router

import (
	"context"
	"testing"

	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/flexprice/flexprice/internal/types"
)

func TestWithConsumerActorStampsConsumerGroup(t *testing.T) {
	var gotActor types.Actor
	var gotSource types.Source
	handler := WithConsumerActor("v1_event_processing", func(ctx context.Context, _ *message.Message) error {
		gotActor = types.GetActor(ctx)
		gotSource = types.GetSource(ctx)
		return nil
	})

	if err := handler(context.Background(), message.NewMessage("m1", nil)); err != nil {
		t.Fatal(err)
	}
	want := types.SystemActor("v1_event_processing", "Consumer v1_event_processing")
	if gotActor != want {
		t.Fatalf("actor = %+v, want %+v", gotActor, want)
	}
	if gotSource != types.SourceConsumer {
		t.Fatalf("source = %q, want %q", gotSource, types.SourceConsumer)
	}
}
