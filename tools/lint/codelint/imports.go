package codelint

import "strings"

const (
	codeLayering    = "CL002"
	codePackageName = "CL006"
)

// gatewaySDKs are provider client libraries whose types stay behind
// internal/integration; the rest of the code talks to provider interfaces.
var gatewaySDKs = []string{
	"github.com/stripe/stripe-go",
	"github.com/razorpay/razorpay-go",
	"github.com/chargebee/chargebee-go",
}

// genericPackageNames say what a package contains, never what it provides.
var genericPackageNames = map[string]bool{
	"utils": true, "util": true, "helpers": true, "helper": true, "common": true, "misc": true, "shared": true,
}

// checkImports keeps each layer talking only to the layer below it, and
// gateway SDK types inside the integration layer.
func (c *checker) checkImports() {
	pkg := c.packagePath()
	for _, file := range c.pass.Files {
		if isGenerated(file) {
			continue
		}
		if c.isTestFile(file) {
			continue
		}
		for _, spec := range file.Imports {
			path := strings.Trim(spec.Path.Value, `"`)
			if problem := layeringProblem(pkg, path); problem != "" {
				c.report(spec.Pos(), codeLayering, "%s", problem)
			}
		}
	}
	c.checkPackageName()
}

func layeringProblem(pkg, imported string) string {
	switch {
	case strings.HasPrefix(pkg, "internal/api/") && (strings.Contains(imported, "internal/repository") || isEntPackage(imported)):
		return "handlers call services only; move the data access into the service that owns it"
	case strings.HasPrefix(pkg, "internal/domain/") && (strings.Contains(imported, "internal/repository") || strings.Contains(imported, "internal/ee/service") || strings.Contains(imported, "internal/api")):
		return "domain packages depend on nothing above them; keep the interface here and the implementation in repository or service"
	case isGatewaySDK(imported) && !strings.HasPrefix(pkg, "internal/integration/") && !strings.HasPrefix(pkg, "internal/testutil"):
		return "gateway SDK types stay inside internal/integration; expose the behaviour through the provider interface instead"
	}
	return ""
}

func isEntPackage(path string) bool {
	return strings.HasSuffix(path, "/ent") || strings.Contains(path, "/ent/")
}

func isGatewaySDK(path string) bool {
	for _, sdk := range gatewaySDKs {
		if strings.HasPrefix(path, sdk) {
			return true
		}
	}
	return false
}

// checkPackageName rejects grab-bag package names on the package clause of
// each file, so adding a file to such a package is what trips the rule.
func (c *checker) checkPackageName() {
	for _, file := range c.pass.Files {
		if isGenerated(file) || c.isTestFile(file) {
			continue
		}
		name := strings.TrimSuffix(file.Name.Name, "_test")
		if genericPackageNames[name] {
			c.report(file.Name.Pos(), codePackageName, "name the package for what it provides, not what it contains; `%s` belongs with the feature that uses it", name)
		}
	}
}
