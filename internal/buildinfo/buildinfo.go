package buildinfo

// Set at build time via -ldflags -X.
var (
	Version      = "dev"
	GitSHA       = "unknown"
	BuildVariant = "Enterprise"
)
