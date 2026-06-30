package e2eprobe

import (
	"sync"
	"time"
)

type Seeds struct {
	PersistentCustomerIDs []string
	PreFundedCustomerIDs  []string
	PersistentSubIDs      []string
	PlanIDs               []string
	FeatureIDs            []string
	MeterIDs              map[string]string // event_name -> meter ID
}

type EphemeralEntity struct {
	Kind      string
	ID        string
	CreatedAt time.Time
}

type Registry interface {
	LoadSeeds(s Seeds)
	Seeds() Seeds
	RegisterEphemeral(kind, id string, createdAt time.Time)
	Ephemerals(kind string) []EphemeralEntity
	ArchiveEphemeral(kind, id string)
}

func NewRegistry() Registry {
	return &registry{ephemerals: map[string][]EphemeralEntity{}}
}

type registry struct {
	mu         sync.RWMutex
	seeds      Seeds
	ephemerals map[string][]EphemeralEntity
}

func (r *registry) LoadSeeds(s Seeds) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.seeds = Seeds{
		PersistentCustomerIDs: append([]string(nil), s.PersistentCustomerIDs...),
		PreFundedCustomerIDs:  append([]string(nil), s.PreFundedCustomerIDs...),
		PersistentSubIDs:      append([]string(nil), s.PersistentSubIDs...),
		PlanIDs:               append([]string(nil), s.PlanIDs...),
		FeatureIDs:            append([]string(nil), s.FeatureIDs...),
		MeterIDs:              copyStringMap(s.MeterIDs),
	}
}

func (r *registry) Seeds() Seeds {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return Seeds{
		PersistentCustomerIDs: append([]string(nil), r.seeds.PersistentCustomerIDs...),
		PreFundedCustomerIDs:  append([]string(nil), r.seeds.PreFundedCustomerIDs...),
		PersistentSubIDs:      append([]string(nil), r.seeds.PersistentSubIDs...),
		PlanIDs:               append([]string(nil), r.seeds.PlanIDs...),
		FeatureIDs:            append([]string(nil), r.seeds.FeatureIDs...),
		MeterIDs:              copyStringMap(r.seeds.MeterIDs),
	}
}

func (r *registry) RegisterEphemeral(kind, id string, createdAt time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ephemerals[kind] = append(r.ephemerals[kind], EphemeralEntity{Kind: kind, ID: id, CreatedAt: createdAt})
}

func (r *registry) Ephemerals(kind string) []EphemeralEntity {
	r.mu.RLock()
	defer r.mu.RUnlock()
	src := r.ephemerals[kind]
	out := make([]EphemeralEntity, len(src))
	copy(out, src)
	return out
}

func (r *registry) ArchiveEphemeral(kind, id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	src := r.ephemerals[kind]
	out := src[:0]
	for _, e := range src {
		if e.ID != id {
			out = append(out, e)
		}
	}
	r.ephemerals[kind] = out
}

func copyStringMap(in map[string]string) map[string]string {
	if in == nil {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
