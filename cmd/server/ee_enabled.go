//go:build ee

package main

import (
	"github.com/flexprice/flexprice/ee"

	"go.uber.org/fx"
)

// eeOptions returns the Enterprise Edition feature set.
//
// This file only compiles with `-tags ee`, and only resolves when the ee/
// submodule has been checked out. Building an EE binary therefore requires both
// the tag and access to the private repository.
//
// Importing ee here is also what triggers the init() functions that populate
// the extension registries in internal/temporal, internal/auth, and internal/api.
func eeOptions() []fx.Option { return []fx.Option{ee.Module()} }
