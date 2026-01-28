package enterprise

import (
	"github.com/flexprice/flexprice/internal/service"
)

// EnterpriseParams holds dependencies for enterprise-tier services.
type EnterpriseParams struct {
	service.ServiceParams
}

// NewEnterpriseParams builds EnterpriseParams from service params (for fx registration).
func NewEnterpriseParams(p service.ServiceParams) EnterpriseParams {
	return EnterpriseParams{ServiceParams: p}
}
