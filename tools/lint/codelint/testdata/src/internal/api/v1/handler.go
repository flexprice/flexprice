package v1

import (
	"github.com/flexprice/flexprice/ent" // want `CL002: handlers call services only`
	"internal/ee/service"
	"internal/repository" // want `CL002: handlers call services only`
)

type Handler struct {
	svc    *service.Service
	store  *repository.Store
	client *ent.Client
}
