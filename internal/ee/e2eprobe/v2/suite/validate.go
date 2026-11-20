package suite

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/flexprice/flexprice/internal/ee/e2eprobe/v2"
)

var nameRE = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

var paymentProviders = map[string]bool{"stripe": true, "chargebee": true, "razorpay": true}

// Validate reports every violation in the suite at once.
func Validate(s *Suite, cat Catalog) error {
	var errs []error
	fail := func(format string, args ...any) {
		errs = append(errs, fmt.Errorf("suite: "+format, args...))
	}

	if s.Version != 1 {
		fail("version must be 1, got %d", s.Version)
	}
	d := s.Defaults
	if d.Tick.Duration < time.Minute {
		fail("defaults.tick must be at least 1m, got %s", d.Tick)
	}
	if d.Concurrency < 1 {
		fail("defaults.concurrency must be at least 1")
	}
	validateAlert(&errs, "defaults.alert", d.Alert, true)
	if d.Load.MaxEventsPerDay < 1 {
		fail("defaults.load.max_events_per_day must be positive")
	}

	seen := map[string]bool{}
	seenExt := map[string]string{}
	for i, p := range s.Personas {
		where := fmt.Sprintf("personas[%d]", i)
		if p.Name != "" {
			where = "persona " + p.Name
		}
		switch {
		case p.Name == "":
			fail("%s: name is required", where)
		case !nameRE.MatchString(p.Name):
			fail("%s: name %q must match %s", where, p.Name, nameRE)
		case seen[p.Name]:
			fail("duplicate persona %q", p.Name)
		}
		seen[p.Name] = true
		for _, t := range p.Traits {
			if t == e2eprobe.TraitEvents {
				fail("%s: trait events is derived from the events profile; remove it", where)
			} else if !t.Valid() {
				fail("%s: unknown trait %q", where, t)
			}
		}
		if p.Events != "" {
			if _, ok := cat.Profile(p.Events); !ok {
				fail("%s: unknown events profile %q", where, p.Events)
			}
		}
		if (p.Wallet != nil) != p.HasTrait(e2eprobe.TraitWallet) {
			fail("%s: wallet block and the wallet trait must come together", where)
		}
		if p.HasTrait(e2eprobe.TraitAlerts) {
			if !p.HasTrait(e2eprobe.TraitWallet) {
				fail("%s: trait alerts requires trait wallet", where)
			} else if p.Wallet != nil && p.Wallet.Thresholds == nil {
				fail("%s: trait alerts requires wallet.thresholds", where)
			}
		}
		if (p.Commitment != nil) != p.HasTrait(e2eprobe.TraitCommitment) {
			fail("%s: commitment block and the commitment trait must come together", where)
		}
		if (p.Payment != nil) != p.HasTrait(e2eprobe.TraitPayment) {
			fail("%s: payment block and the payment trait must come together", where)
		}
		if p.Payment != nil && !paymentProviders[p.Payment.Provider] {
			fail("%s: payment provider %q must be stripe, chargebee or razorpay", where, p.Payment.Provider)
		}
		if !strings.HasPrefix(p.ExternalIDOrDefault(), "e2eprobe-") {
			fail("%s: external_id %q must start with e2eprobe-", where, p.ExternalID)
		}
		if ext := p.ExternalIDOrDefault(); seenExt[ext] != "" {
			fail("%s: duplicate external_id %q, already used by persona %s", where, ext, seenExt[ext])
		} else {
			seenExt[ext] = p.Name
		}
	}

	seenScenario := map[string]bool{}
	for _, sc := range s.Scenarios {
		if seenScenario[sc.Name] {
			fail("duplicate scenario %q", sc.Name)
		}
		seenScenario[sc.Name] = true
		if sc.Every.Duration != 0 && sc.Every.Duration < time.Minute {
			fail("scenario %s: every must be at least 1m, got %s", sc.Name, sc.Every)
		}
		if sc.Alert != nil {
			validateAlert(&errs, "scenario "+sc.Name+".alert", *sc.Alert, false)
		}
		for _, name := range sc.names() {
			info, ok := cat.Scenario(name)
			if !ok {
				fail("unknown scenario %q", name)
				continue
			}
			for _, pn := range sc.Personas {
				p, ok := s.Persona(pn)
				if !ok {
					fail("scenario %s: unknown persona %q", name, pn)
					continue
				}
				for _, t := range info.Traits {
					if !p.HasTrait(t) {
						fail("scenario %s: persona %q lacks required trait %s", name, pn, t)
					}
				}
			}
			if len(info.Traits) > 0 && sc.IsEnabled() && len(s.personasFor(sc, info)) == 0 {
				fail("scenario %s: no persona carries traits %v", name, info.Traits)
			}
		}
	}

	if perDay := s.EventsPerDay(cat); perDay > d.Load.MaxEventsPerDay {
		fail("load: %d events/day exceeds %d (defaults.load.max_events_per_day)", perDay, d.Load.MaxEventsPerDay)
	}
	return errors.Join(errs...)
}

// validateAlert checks an alert block; required means every field must be set,
// false means only set fields are checked (a per-scenario override).
func validateAlert(errs *[]error, where string, a Alert, required bool) {
	if required && a.AfterFailures < 1 {
		*errs = append(*errs, fmt.Errorf("suite: %s.after_failures must be at least 1", where))
	}
	if required && a.AfterDuration.Duration <= 0 {
		*errs = append(*errs, fmt.Errorf("suite: %s.after_duration must be positive", where))
	}
	if required && a.SkipAfter.Duration <= 0 {
		*errs = append(*errs, fmt.Errorf("suite: %s.skip_after must be positive", where))
	}
	if a.AfterFailures < 0 || a.AfterDuration.Duration < 0 || a.RenotifyEvery.Duration < 0 || a.SkipAfter.Duration < 0 {
		*errs = append(*errs, fmt.Errorf("suite: %s must not be negative", where))
	}
}
