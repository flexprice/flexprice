// Package spec holds the build-time artefacts the CLI resolves commands from.
// The binary never fetches a spec at runtime.
package spec

import _ "embed"

//go:embed openapi.json
var OpenAPI []byte

//go:embed commands.yaml
var Commands []byte
