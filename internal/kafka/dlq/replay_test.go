package dlq

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Shopify/sarama"
	watermillKafka "github.com/ThreeDotsLabs/watermill-kafka/v2/pkg/kafka"
	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/ThreeDotsLabs/watermill/message/router/middleware"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/stretchr/testify/require"
)

func TestParseCount(t *testing.T) {
	cases := map[string]int{
		"":     0,
		"0":    0,
		"2":    2,
		"3":    3,
		"-1":   0,
		"abc":  0,
		"  1 ": 0,
	}
	for in, want := range cases {
		if got := parseCount(in); got != want {
			t.Errorf("parseCount(%q) = %d, want %d", in, got, want)
		}
	}
}

func TestTruncate(t *testing.T) {
	if got := truncate("", 5); got != "(no reason)" {
		t.Errorf("empty reason = %q, want (no reason)", got)
	}
	if got := truncate("short", 80); got != "short" {
		t.Errorf("short = %q, want short", got)
	}
	if got := truncate("abcdef", 3); got != "abc" {
		t.Errorf("truncate len = %q, want abc", got)
	}
}

type replayConsumer struct {
	sarama.Consumer
	partition *replayPartitionConsumer
}

func (c *replayConsumer) ConsumePartition(string, int32, int64) (sarama.PartitionConsumer, error) {
	return c.partition, nil
}

type replayPartitionConsumer struct {
	sarama.PartitionConsumer
	messages chan *sarama.ConsumerMessage
}

func (c *replayPartitionConsumer) Messages() <-chan *sarama.ConsumerMessage { return c.messages }
func (c *replayPartitionConsumer) Close() error                             { return nil }

type replayOffsets struct {
	sarama.PartitionOffsetManager
	marked []int64
}

func (o *replayOffsets) MarkOffset(offset int64, _ string) { o.marked = append(o.marked, offset) }

type replayPublisher struct {
	message.Publisher
	messages []*message.Message
	err      error
}

func (p *replayPublisher) Publish(_ string, messages ...*message.Message) error {
	if p.err != nil {
		return p.err
	}
	p.messages = append(p.messages, messages...)
	return nil
}

func poisonedMessage(t *testing.T, offset int64) *sarama.ConsumerMessage {
	t.Helper()
	msg := message.NewMessage("event", []byte(`{"id":"event"}`))
	msg.Metadata.Set(middleware.PoisonedTopicKey, "events")
	msg.Metadata.Set(middleware.ReasonForPoisonedKey, "connection refused")
	msg.Metadata.Set(ReplayCountKey, "1")
	encoded, err := (watermillKafka.DefaultMarshaler{}).Marshal("events_dlq", msg)
	require.NoError(t, err)
	payload, err := encoded.Value.Encode()
	require.NoError(t, err)
	km := &sarama.ConsumerMessage{Offset: offset, Value: payload}
	for i := range encoded.Headers {
		km.Headers = append(km.Headers, &encoded.Headers[i])
	}
	return km
}

func TestDrainPartitionBoundsAndProgress(t *testing.T) {
	for _, tt := range []struct {
		name         string
		offsets      []int64
		dryRun       bool
		publishErr   error
		wantReplayed int
		wantMarked   []int64
	}{
		{name: "last retained offset", offsets: []int64{149, 150}, wantReplayed: 1, wantMarked: []int64{150}},
		{name: "gap crosses boundary", offsets: []int64{148, 151}, wantReplayed: 1, wantMarked: []int64{149}},
		{name: "starts beyond boundary", offsets: []int64{151}},
		{name: "dry run", offsets: []int64{149}, dryRun: true, wantReplayed: 1},
		{name: "failed publish does not advance", offsets: []int64{149}, publishErr: errors.New("publish failed")},
	} {
		t.Run(tt.name, func(t *testing.T) {
			messages := make(chan *sarama.ConsumerMessage, len(tt.offsets))
			for _, offset := range tt.offsets {
				messages <- poisonedMessage(t, offset)
			}
			close(messages)
			consumer := &replayConsumer{partition: &replayPartitionConsumer{messages: messages}}
			offsets := &replayOffsets{}
			publisher := &replayPublisher{err: tt.publishErr}
			sum := &Summary{ByTarget: map[string]int{}, ByReason: map[string]int{}}
			err := drainPartition(context.Background(), consumer, offsets, watermillKafka.DefaultMarshaler{}, publisher, logger.NewNoopLogger(), Options{SourceTopic: "events_dlq", MaxReplays: 3, DryRun: tt.dryRun, idleTimeout: time.Second}, 0, 148, 150, sum)
			if tt.publishErr != nil {
				require.ErrorIs(t, err, tt.publishErr)
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, tt.wantReplayed, sum.Replayed)
			require.Equal(t, tt.wantMarked, offsets.marked)
			if tt.dryRun {
				require.Empty(t, publisher.messages)
			} else {
				require.Len(t, publisher.messages, tt.wantReplayed)
				for _, msg := range publisher.messages {
					require.Empty(t, msg.Metadata.Get(middleware.PoisonedTopicKey))
					require.Empty(t, msg.Metadata.Get(middleware.ReasonForPoisonedKey))
					require.Equal(t, "2", msg.Metadata.Get(ReplayCountKey))
				}
			}
		})
	}
}
