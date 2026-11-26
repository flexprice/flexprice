package world

import (
	"sync"
	"time"

	"github.com/flexprice/flexprice/internal/ee/e2eprobe"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/suite"
)

// Persona is a suite persona bound to the live customer, subscription and
// wallet that carry it. Gaps name traits the live state cannot honour.
type Persona struct {
	mu                sync.RWMutex
	spec              suite.Persona
	customerID        string
	subscriptionID    string
	multiCadenceSubID string
	walletID          string
	unfundedWalletID  string // created but its opening balance never landed
	taxActiveFrom     time.Time
	gaps              map[e2eprobe.Trait]string
	ready             bool
}

func newPersona(spec suite.Persona) *Persona {
	return &Persona{spec: spec, gaps: map[e2eprobe.Trait]string{}}
}

func (p *Persona) Name() string              { return p.spec.Name }
func (p *Persona) ExternalID() string        { return p.spec.ExternalIDOrDefault() }
func (p *Persona) Spec() suite.Persona       { return p.spec }
func (p *Persona) Has(t e2eprobe.Trait) bool { return p.spec.HasTrait(t) }

func (p *Persona) CustomerID() string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.customerID
}

func (p *Persona) SubscriptionID() string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.subscriptionID
}

// MultiCadenceSubID is the quarterly subscription of a multi_cadence persona.
func (p *Persona) MultiCadenceSubID() string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.multiCadenceSubID
}

func (p *Persona) WalletID() string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.walletID
}

// TaxActiveFrom is when the shared tax association became active; invoices
// finalized before it carry no tax.
func (p *Persona) TaxActiveFrom() time.Time {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.taxActiveFrom
}

// Gap explains why a declared trait is not fully honoured, or "".
func (p *Persona) Gap(t e2eprobe.Trait) string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.gaps[t]
}

func (p *Persona) Gaps() map[e2eprobe.Trait]string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	out := make(map[e2eprobe.Trait]string, len(p.gaps))
	for k, v := range p.gaps {
		out[k] = v
	}
	return out
}

// Ready reports whether the last provisioning pass completed without error.
func (p *Persona) Ready() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.ready
}

func (p *Persona) setGap(t e2eprobe.Trait, reason string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if reason == "" {
		delete(p.gaps, t)
		return
	}
	p.gaps[t] = reason
}

func (p *Persona) update(fn func(*Persona)) {
	p.mu.Lock()
	defer p.mu.Unlock()
	fn(p)
}

func (p *Persona) unfunded() string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.unfundedWalletID
}

// scratch is the working copy a provisioning pass mutates; adopt publishes it
// only once the whole pass succeeded, so scenarios never see a persona whose
// ids moved while its gaps still describe the old binding.
func (p *Persona) scratch() *Persona {
	p.mu.RLock()
	defer p.mu.RUnlock()
	cp := &Persona{spec: p.spec, customerID: p.customerID, subscriptionID: p.subscriptionID, multiCadenceSubID: p.multiCadenceSubID,
		walletID: p.walletID, unfundedWalletID: p.unfundedWalletID, taxActiveFrom: p.taxActiveFrom, gaps: make(map[e2eprobe.Trait]string, len(p.gaps))}
	for k, v := range p.gaps {
		cp.gaps[k] = v
	}
	return cp
}

func (p *Persona) adopt(s *Persona) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	p.mu.Lock()
	defer p.mu.Unlock()
	p.customerID, p.subscriptionID, p.multiCadenceSubID = s.customerID, s.subscriptionID, s.multiCadenceSubID
	p.walletID, p.unfundedWalletID, p.taxActiveFrom = s.walletID, s.unfundedWalletID, s.taxActiveFrom
	p.gaps = make(map[e2eprobe.Trait]string, len(s.gaps))
	for k, v := range s.gaps {
		p.gaps[k] = v
	}
	p.ready = true
}

// carryRecovery keeps the bookkeeping a failed pass needs next time without
// publishing any of its bindings.
func (p *Persona) carryRecovery(s *Persona) {
	if id := s.unfunded(); id != "" {
		p.update(func(p *Persona) { p.unfundedWalletID = id })
	}
}
