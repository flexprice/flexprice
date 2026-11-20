package inbox

import (
	"context"
	"errors"
	"testing"
	"time"
)

func ev(at time.Time, typ, customer string) Event {
	return Event{ReceivedAt: at, EventType: typ, CustomerID: customer, EntityID: customer}
}

func TestInbox_EvictsBeyondCapacity(t *testing.T) {
	in := New(Options{Capacity: 3})
	at := time.Date(2026, 11, 19, 10, 0, 0, 0, time.UTC)
	for i := 0; i < 5; i++ {
		in.Record(ev(at.Add(time.Duration(i)*time.Second), "customer.created", "c"))
	}
	st := in.Stats()
	if st.Total != 3 || st.ByType["customer.created"] != 3 || !st.OldestAt.Equal(at.Add(2*time.Second)) || !st.LastReceivedAt.Equal(at.Add(4*time.Second)) {
		t.Errorf("stats = %+v", st)
	}
}

func TestInbox_EvictsBeyondRetention(t *testing.T) {
	in := New(Options{Capacity: 10, Retention: time.Hour})
	at := time.Date(2026, 11, 19, 10, 0, 0, 0, time.UTC)
	in.Record(ev(at, "customer.created", "old"))
	in.Record(ev(at.Add(2*time.Hour), "customer.created", "new"))
	if got := in.Find(Match{}); len(got) != 1 || got[0].CustomerID != "new" {
		t.Errorf("retention eviction failed: %+v", got)
	}
}

func TestAwait_ReturnsExistingMatch(t *testing.T) {
	in := New(Options{})
	at := time.Now()
	in.Record(ev(at, "wallet.ongoing_balance.dropped", "c1"))
	got, err := in.Await(context.Background(), Match{EventType: "wallet.ongoing_balance.dropped", CustomerID: "c1"}, time.Second)
	if err != nil || got.CustomerID != "c1" {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestAwait_ReturnsLaterMatch(t *testing.T) {
	in := New(Options{})
	go func() {
		time.Sleep(50 * time.Millisecond)
		in.Record(ev(time.Now(), "customer.created", "c1"))
		in.Record(ev(time.Now(), "customer.created", "c2"))
	}()
	got, err := in.Await(context.Background(), Match{CustomerID: "c2"}, 2*time.Second)
	if err != nil || got.CustomerID != "c2" {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestAwait_TimesOut(t *testing.T) {
	in := New(Options{})
	_, err := in.Await(context.Background(), Match{EventType: "never"}, 30*time.Millisecond)
	if !errors.Is(err, ErrTimeout) {
		t.Fatalf("err = %v, want ErrTimeout", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := in.Await(ctx, Match{EventType: "never"}, time.Minute); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func TestStats_CountsByType(t *testing.T) {
	in := New(Options{})
	at := time.Date(2026, 11, 19, 10, 0, 0, 0, time.UTC)
	in.Record(ev(at, "customer.created", "a"))
	in.Record(ev(at.Add(time.Second), "invoice.update", "b"))
	in.Record(ev(at.Add(2*time.Second), "customer.created", "c"))
	st := in.Stats()
	if st.Total != 3 || st.ByType["customer.created"] != 2 || st.ByType["invoice.update"] != 1 || !in.LastReceived().Equal(at.Add(2*time.Second)) {
		t.Errorf("stats = %+v last = %s", st, in.LastReceived())
	}
	if !New(Options{}).LastReceived().IsZero() {
		t.Error("empty inbox must report a zero last-received time")
	}
}
