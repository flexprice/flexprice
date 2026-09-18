//go:build ee

package main

import (
	"github.com/flexprice/flexprice/ee"

	"go.uber.org/fx"
)

// eeOptions returns the Enterprise Edition feature set. Compiles only with
// -tags ee. Importing ee triggers the init()s that populate the extension
// registries in internal/temporal, internal/api, internal/auth.
func eeOptions() []fx.Option { return []fx.Option{ee.Module()} }
