package e2eprobe

import (
	"fmt"
	"math/rand"
	"strconv"
	"sync"
)

type EventDeckOpts struct {
	Customers        []string
	EventNames       []string
	OrphanEventName  string
	OrphanFrequency  int
	Seed             int64
}

type EventDraw struct {
	EventName          string
	ExternalCustomerID string
	Source             string
	Properties         map[string]string
}

var (
	deckSources      = []string{"api", "web", "mobile", "batch"}
	deckRegions      = []string{"us", "eu", "ap"}
	deckAmounts      = []string{"0.1", "1", "10", "100", "1000"}
	deckRandomExtras = []string{"session_id", "endpoint", "status", "method", "tier"}
	deckUserPool     = 50
)

type EventDeck struct {
	opts EventDeckOpts
	mu   sync.Mutex
	rnd  *rand.Rand
	n    int64
}

func NewEventDeck(opts EventDeckOpts) *EventDeck {
	if len(opts.Customers) == 0 {
		panic("e2eprobe: EventDeck requires at least one customer")
	}
	if len(opts.EventNames) == 0 {
		panic("e2eprobe: EventDeck requires at least one event name")
	}
	return &EventDeck{opts: opts, rnd: rand.New(rand.NewSource(opts.Seed))}
}

func (d *EventDeck) Next() EventDraw {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.n++
	n := d.n

	var name string
	if d.opts.OrphanEventName != "" && d.opts.OrphanFrequency > 0 && n%int64(d.opts.OrphanFrequency) == 0 {
		name = d.opts.OrphanEventName
	} else {
		name = d.opts.EventNames[int(n)%len(d.opts.EventNames)]
	}

	cust := d.opts.Customers[int(n)%len(d.opts.Customers)]
	source := deckSources[int(n)%len(deckSources)]
	region := deckRegions[int(n)%len(deckRegions)]
	amount := deckAmounts[int(n)%len(deckAmounts)]
	user := fmt.Sprintf("e2eprobe_user_%d", d.rnd.Intn(deckUserPool))
	durMs := strconv.Itoa(1 + d.rnd.Intn(500))

	props := map[string]string{
		"amount":      amount,
		"region":      region,
		"user_id":     user,
		"duration_ms": durMs,
		"e2eprobe": "true",
	}

	if d.rnd.Float64() < 0.3 {
		extras := 1 + d.rnd.Intn(3)
		used := map[string]bool{}
		for i := 0; i < extras; i++ {
			k := deckRandomExtras[d.rnd.Intn(len(deckRandomExtras))]
			if used[k] {
				continue
			}
			used[k] = true
			props[k] = fmt.Sprintf("v_%d", d.rnd.Intn(1000))
		}
	}

	return EventDraw{
		EventName:          name,
		ExternalCustomerID: cust,
		Source:             source,
		Properties:         props,
	}
}
