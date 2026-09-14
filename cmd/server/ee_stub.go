//go:build !ee

package main

import "go.uber.org/fx"

// eeOptions returns no extra fx options in a community build. The `ee` build tag
// selects ee_enabled.go instead, which imports the ee package. main.go, which
// calls eeOptions(), never names the ee package directly.
func eeOptions() []fx.Option { return nil }
