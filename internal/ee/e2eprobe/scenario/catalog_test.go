package scenario

import (
	"context"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/ee/e2eprobe"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/suite"
)

func TestCatalog_RegisterDuplicatePanics(t *testing.T) {
	cat := NewCatalog()
	registerStub(cat, "usage", []e2eprobe.Trait{e2eprobe.TraitEvents}, time.Minute, nil)
	defer func() {
		if recover() == nil {
			t.Error("registering the same name twice must panic")
		}
	}()
	registerStub(cat, "usage", nil, time.Minute, nil)
}

func TestCatalog_ImplementsSuiteCatalog(t *testing.T) {
	cat := NewCatalog()
	registerStub(cat, "usage", []e2eprobe.Trait{e2eprobe.TraitEvents}, 15*time.Minute, func(context.Context, *Context) e2eprobe.Result { return e2eprobe.Pass() })
	registerStub(cat, "cancel", []e2eprobe.Trait{e2eprobe.TraitSubscription}, time.Hour, nil)
	var sc suite.Catalog = cat
	info, ok := sc.Scenario("usage")
	if !ok || info.Every != 15*time.Minute || len(info.Traits) != 1 || info.Kind != e2eprobe.KindProbe {
		t.Errorf("info = %+v ok=%v", info, ok)
	}
	if _, ok := sc.Scenario("ghost"); ok {
		t.Error("unknown scenario must not resolve")
	}
	if n, ok := sc.Profile("standard"); !ok || n != 27 {
		t.Errorf("profile standard = %d %v", n, ok)
	}
	if _, ok := sc.Profile("firehose"); ok {
		t.Error("unknown profile must not resolve")
	}
	deps, _ := newDeps(t, false)
	s, err := cat.Build("usage", deps)
	if err != nil || s.Name() != "usage" {
		t.Fatalf("build: %v %v", s, err)
	}
	if _, err := cat.Build("ghost", deps); err == nil {
		t.Error("building an unknown scenario must fail")
	}
	if names := cat.Names(); len(names) != 2 || names[0] != "cancel" || names[1] != "usage" {
		t.Errorf("names = %v", names)
	}
}
