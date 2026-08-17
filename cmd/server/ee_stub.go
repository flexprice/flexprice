//go:build !ee

package main

import "go.uber.org/fx"

// eeOptions returns no extra fx options in a community build.
//
// The `ee` build tag selects ee_enabled.go instead, which imports the ee/
// submodule. Public clones have an empty ee/ directory, so this stub is the
// only version that can compile for them — and main.go, which calls
// eeOptions(), never names the ee package.
func eeOptions() []fx.Option { return nil }
