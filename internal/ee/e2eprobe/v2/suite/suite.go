// Package suite reads the probe's suite file: the personas, the scenarios bound
// to them, tenant-level expectations and alerting defaults.
package suite

import (
	"bytes"
	_ "embed"
	"fmt"
	"os"
	"time"

	"github.com/flexprice/flexprice/internal/ee/e2eprobe/v2"
	"gopkg.in/yaml.v3"
)

//go:embed default.yaml
var defaultYAML []byte

// Duration is a time.Duration written as a Go duration string in YAML.
type Duration struct{ time.Duration }

func (d *Duration) UnmarshalYAML(value *yaml.Node) error {
	parsed, err := time.ParseDuration(value.Value)
	if err != nil {
		return fmt.Errorf("line %d: %q is not a duration", value.Line, value.Value)
	}
	d.Duration = parsed
	return nil
}

func (d Duration) MarshalYAML() (any, error) {
	return d.Duration.String(), nil
}

type Suite struct {
	Version   int        `yaml:"version"`
	Defaults  Defaults   `yaml:"defaults"`
	Tenant    Tenant     `yaml:"tenant"`
	Personas  []Persona  `yaml:"personas"`
	Scenarios []Scenario `yaml:"scenarios"`
}

type Defaults struct {
	Tick        Duration `yaml:"tick"`
	Concurrency int      `yaml:"concurrency"`
	Alert       Alert    `yaml:"alert"`
	Load        Budget   `yaml:"load"`
	Janitor     Janitor  `yaml:"janitor"`
	Digest      Digest   `yaml:"digest"`
}

// Alert decides when a failing or skipping binding becomes an incident.
type Alert struct {
	AfterFailures int      `yaml:"after_failures"`
	AfterDuration Duration `yaml:"after_duration"`
	RenotifyEvery Duration `yaml:"renotify_every"`
	SkipAfter     Duration `yaml:"skip_after"`
}

type Budget struct {
	MaxEventsPerDay int `yaml:"max_events_per_day"`
}

type Janitor struct {
	MaxAge Duration `yaml:"max_age"`
}

type Digest struct {
	Every Duration `yaml:"every"`
}

// Tenant lists what the probe expects to find configured on the tenant.
type Tenant struct {
	Connections []string                  `yaml:"connections"`
	Settings    map[string]map[string]any `yaml:"settings"`
	Webhooks    Webhooks                  `yaml:"webhooks"`
}

type Webhooks struct {
	SilenceAfter Duration `yaml:"silence_after"`
}

// Persona is a long-lived customer and the traits provisioning guarantees on it.
type Persona struct {
	Name       string           `yaml:"name"`
	ExternalID string           `yaml:"external_id"`
	Traits     []e2eprobe.Trait `yaml:"traits"`
	Events     string           `yaml:"events"`
	Wallet     *Wallet          `yaml:"wallet"`
	Commitment *Commitment      `yaml:"commitment"`
	Payment    *Payment         `yaml:"payment"`
}

type Wallet struct {
	Balance    string      `yaml:"balance"`
	Thresholds *Thresholds `yaml:"thresholds"`
}

type Thresholds struct {
	Info     string `yaml:"info"`
	Warning  string `yaml:"warning"`
	Critical string `yaml:"critical"`
}

type Commitment struct {
	Amount        string `yaml:"amount"`
	OverageFactor string `yaml:"overage_factor"`
}

type Payment struct {
	Provider          string   `yaml:"provider"`
	Currency          string   `yaml:"currency"`
	FixedCustomer     string   `yaml:"fixed_customer"`
	DeclineLast4      string   `yaml:"decline_last4"`
	SettleTimeout     Duration `yaml:"settle_timeout"`
	AssertKnownIssues bool     `yaml:"assert_known_issues"`
}

// Scenario enables one catalog scenario and chooses the personas it runs on.
type Scenario struct {
	Name              string   `yaml:"name"`
	Every             Duration `yaml:"every"`
	Enabled           *bool    `yaml:"enabled"`
	Personas          []string `yaml:"personas"`
	Alert             *Alert   `yaml:"alert"`
	Families          []string `yaml:"families"`
	PerRun            int      `yaml:"per_run"`
	AssertKnownIssues bool     `yaml:"assert_known_issues"`
}

// ScenarioInfo is what the catalog knows about a scenario. No traits means the
// scenario is tenant-level and binds once with no persona.
type ScenarioInfo struct {
	Name   string
	Kind   e2eprobe.Kind
	Traits []e2eprobe.Trait
	Every  time.Duration
	// EventsPerRun is the ingest one run adds on top of the profile batches.
	EventsPerRun int
}

// Catalog answers which scenarios and event profiles exist in the binary.
type Catalog interface {
	Scenario(name string) (ScenarioInfo, bool)
	Profile(name string) (eventsPerTick int, ok bool)
}

// Default is the suite embedded in the binary.
func Default() *Suite {
	s, err := Parse(defaultYAML)
	if err != nil {
		panic("suite: embedded default.yaml: " + err.Error())
	}
	return s
}

// Load reads a suite file; an empty path yields the embedded default.
func Load(path string) (*Suite, error) {
	if path == "" {
		return Default(), nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("suite: read %s: %w", path, err)
	}
	s, err := Parse(data)
	if err != nil {
		return nil, fmt.Errorf("suite: %s: %w", path, err)
	}
	return s, nil
}

// Parse decodes a suite strictly (unknown fields fail) and applies defaults.
func Parse(data []byte) (*Suite, error) {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	s := &Suite{}
	if err := dec.Decode(s); err != nil {
		return nil, err
	}
	applyDefaults(s)
	return s, nil
}

func applyDefaults(s *Suite) {
	setDuration(&s.Defaults.Tick, 10*time.Minute)
	if s.Defaults.Concurrency == 0 {
		s.Defaults.Concurrency = 4
	}
	if s.Defaults.Alert.AfterFailures == 0 {
		s.Defaults.Alert.AfterFailures = 3
	}
	setDuration(&s.Defaults.Alert.AfterDuration, 20*time.Minute)
	setDuration(&s.Defaults.Alert.RenotifyEvery, 6*time.Hour)
	setDuration(&s.Defaults.Alert.SkipAfter, 24*time.Hour)
	if s.Defaults.Load.MaxEventsPerDay == 0 {
		s.Defaults.Load.MaxEventsPerDay = 100000
	}
	setDuration(&s.Defaults.Janitor.MaxAge, time.Hour)
	setDuration(&s.Defaults.Digest.Every, 24*time.Hour)
	setDuration(&s.Tenant.Webhooks.SilenceAfter, 6*time.Hour)
}

func setDuration(d *Duration, def time.Duration) {
	if d.Duration == 0 {
		d.Duration = def
	}
}

func (s *Suite) Persona(name string) (Persona, bool) {
	for _, p := range s.Personas {
		if p.Name == name {
			return p, true
		}
	}
	return Persona{}, false
}

func (s *Suite) ScenarioConfig(name string) (Scenario, bool) {
	for _, sc := range s.Scenarios {
		if sc.Name == name {
			return sc, true
		}
	}
	return Scenario{}, false
}

// AlertPolicy is the suite default overlaid with the scenario entry's override.
// The entry name is the suite key, before family expansion.
func (s *Suite) AlertPolicy(scenario string) Alert {
	policy := s.Defaults.Alert
	sc, ok := s.ScenarioConfig(scenario)
	if !ok || sc.Alert == nil {
		return policy
	}
	if sc.Alert.AfterFailures > 0 {
		policy.AfterFailures = sc.Alert.AfterFailures
	}
	if sc.Alert.AfterDuration.Duration > 0 {
		policy.AfterDuration = sc.Alert.AfterDuration
	}
	if sc.Alert.RenotifyEvery.Duration > 0 {
		policy.RenotifyEvery = sc.Alert.RenotifyEvery
	}
	if sc.Alert.SkipAfter.Duration > 0 {
		policy.SkipAfter = sc.Alert.SkipAfter
	}
	return policy
}

// EventsPerDay is every event the suite schedules: profile batches plus the
// ingest of each enabled scenario binding at its interval.
func (s *Suite) EventsPerDay(cat Catalog) int {
	profiles, flows := s.Ingest(cat)
	return profiles + flows
}

// Ingest splits the daily budget into profile batches and scenario-driven
// ingest; bindings that cannot be resolved count as nothing and fail Validate.
func (s *Suite) Ingest(cat Catalog) (profiles, flows int) {
	const day = 24 * time.Hour
	if tick := s.Defaults.Tick.Duration; tick > 0 {
		for _, p := range s.Personas {
			if p.Events == "" {
				continue
			}
			if size, ok := cat.Profile(p.Events); ok {
				profiles += size * int(day/tick)
			}
		}
	}
	bindings, err := s.Bindings(cat)
	if err != nil {
		return profiles, 0
	}
	for _, b := range bindings {
		info, ok := cat.Scenario(b.Scenario)
		if !ok || info.EventsPerRun == 0 {
			continue
		}
		every := b.Every
		if every <= 0 {
			every = s.Defaults.Tick.Duration
		}
		if every > 0 {
			flows += info.EventsPerRun * int(day/every)
		}
	}
	return profiles, flows
}

// HasTrait includes the events trait, which a profile implies.
func (p Persona) HasTrait(t e2eprobe.Trait) bool {
	if t == e2eprobe.TraitEvents {
		return p.Events != ""
	}
	for _, have := range p.Traits {
		if have == t {
			return true
		}
	}
	return false
}

func (p Persona) ExternalIDOrDefault() string {
	if p.ExternalID != "" {
		return p.ExternalID
	}
	return "e2eprobe-" + p.Name
}

func (sc Scenario) IsEnabled() bool {
	return sc.Enabled == nil || *sc.Enabled
}
